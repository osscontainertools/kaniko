/*
Copyright 2025 Martin Zihlmann

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Copied from vendor/github.com/google/go-containerregistry/pkg/authn/keychain.go
package creds

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/mitchellh/go-homedir"
)

// newDefaultKeychain implements Keychain with the semantics of the standard Docker
// credential keychain. But it stores the credentials internally rather than retrieve
// them every time.
type newDefaultKeychain struct {
	mu sync.Mutex
	cf *configfile.ConfigFile
}

func (dk *newDefaultKeychain) Load() error {
	dk.mu.Lock()
	defer dk.mu.Unlock()

	// Podman users may have their container registry auth configured in a
	// different location, that Docker packages aren't aware of.
	// If the Docker config file isn't found, we'll fallback to look where
	// Podman configures it, and parse that as a Docker auth config instead.

	// First, check $HOME/.docker/config.json
	foundDockerConfig := false
	home, err := homedir.Dir()
	if err == nil {
		foundDockerConfig = fileExists(filepath.Join(home, ".docker/config.json"))
	}
	// If $HOME/.docker/config.json isn't found, check $DOCKER_CONFIG (if set)
	if !foundDockerConfig && os.Getenv("DOCKER_CONFIG") != "" {
		foundDockerConfig = fileExists(filepath.Join(os.Getenv("DOCKER_CONFIG"), "config.json"))
	}
	// If either of those locations are found, load it using Docker's
	// config.Load, which may fail if the config can't be parsed.
	//
	// If neither was found, look for Podman's auth at
	// $REGISTRY_AUTH_FILE or $XDG_RUNTIME_DIR/containers/auth.json
	// and attempt to load it as a Docker config.
	//
	// If neither are found, fallback to Anonymous.
	var cf *configfile.ConfigFile
	if foundDockerConfig {
		cf, err = config.Load(os.Getenv("DOCKER_CONFIG"))
		if err != nil {
			return err
		}
	} else if fileExists(os.Getenv("REGISTRY_AUTH_FILE")) {
		f, err := os.Open(os.Getenv("REGISTRY_AUTH_FILE"))
		if err != nil {
			return err
		}
		defer f.Close()
		cf, err = config.LoadFromReader(f)
		if err != nil {
			return err
		}
	} else if fileExists(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "containers/auth.json")) {
		f, err := os.Open(filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "containers/auth.json"))
		if err != nil {
			return err
		}
		defer f.Close()
		cf, err = config.LoadFromReader(f)
		if err != nil {
			return err
		}
	}
	dk.cf = cf
	return nil
}

func (dk *newDefaultKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	return dk.ResolveContext(context.Background(), target)
}

func (dk *newDefaultKeychain) ResolveContext(_ context.Context, target authn.Resource) (authn.Authenticator, error) {
	dk.mu.Lock()
	defer dk.mu.Unlock()
	if dk.cf == nil {
		return authn.Anonymous, nil
	}
	var cfg, empty types.AuthConfig
	for _, key := range []string{
		target.String(),
		target.RegistryStr(),
	} {
		if key == name.DefaultRegistry {
			key = authn.DefaultAuthKey
		}

		cfg, err := dk.cf.GetAuthConfig(key)
		if err != nil {
			return nil, err
		}
		// cf.GetAuthConfig automatically sets the ServerAddress attribute. Since
		// we don't make use of it, clear the value for a proper "is-empty" test.
		// See: https://github.com/google/go-containerregistry/issues/1510
		cfg.ServerAddress = ""
		if cfg != empty {
			break
		}
	}
	if cfg == empty {
		return authn.Anonymous, nil
	}

	return authn.FromConfig(authn.AuthConfig{
		Username:      cfg.Username,
		Password:      cfg.Password,
		Auth:          cfg.Auth,
		IdentityToken: cfg.IdentityToken,
		RegistryToken: cfg.RegistryToken,
	}), nil
}

// fileExists returns true if the given path exists and is not a directory.
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
