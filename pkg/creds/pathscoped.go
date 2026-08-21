/*
Copyright 2026 OSS Container Tools

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

package creds

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

// PathScopedKeychain implements authn.Keychain like authn.DefaultKeychain, but walks the
// intermediate repository paths, so "registry.example.com/org-a" covers
// "registry.example.com/org-a/project/image", and it never lets an "auths" entry answer
// for a repository it does not cover.
var PathScopedKeychain authn.Keychain = pathScopedKeychain{}

type pathScopedKeychain struct{}

func (pathScopedKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	cf, err := loadDockerConfig()
	if err != nil {
		return nil, err
	}
	if cf == nil {
		// docker/cli still resolves DOCKER_AUTH_CONFIG without an auth file
		cf = configfile.New("")
	}

	// credential helpers are registry-scoped, so intermediate paths only ever see inline auths
	scoped := *cf
	scoped.CredentialsStore = ""
	scoped.CredentialHelpers = nil

	keys := authKeyLookupOrder(target)
	for i, key := range keys {
		if key == name.DefaultRegistry {
			key = authn.DefaultAuthKey
		}

		lookup := &scoped
		if i == 0 || i == len(keys)-1 {
			// the exact resource and the bare host keep DefaultKeychain's credsStore/credHelpers lookup
			lookup = cf
		}
		cfg, err := lookup.GetAuthConfig(key)
		if err != nil {
			return nil, err
		}
		// the credential store answers a bare-host lookup with any entry whose host matches and
		// reports the matched entry in ServerAddress, where a scheme means a legacy registry URL
		if cfg.ServerAddress != "" && cfg.ServerAddress != key && !strings.Contains(cfg.ServerAddress, "://") {
			continue
		}
		// GetAuthConfig sets ServerAddress even for an entry that holds no credential
		cfg.ServerAddress = ""
		if cfg != (types.AuthConfig{}) {
			return authn.FromConfig(authn.AuthConfig{
				Username:      cfg.Username,
				Password:      cfg.Password,
				Auth:          cfg.Auth,
				IdentityToken: cfg.IdentityToken,
				RegistryToken: cfg.RegistryToken,
			}), nil
		}
	}

	return authn.Anonymous, nil
}

// authKeyLookupOrder returns the auth-file lookup keys for target, most specific first.
// Parent paths drop whole slash-delimited segments, so "registry/org" never matches "registry/org-admin".
func authKeyLookupOrder(target authn.Resource) []string {
	keys := []string{target.String()}

	repo, ok := target.(name.Repository)
	if ok {
		segments := strings.Split(repo.RepositoryStr(), "/")
		for i := len(segments) - 1; i > 0; i-- {
			keys = append(keys, repo.RegistryStr()+"/"+strings.Join(segments[:i], "/"))
		}
	}

	host := target.RegistryStr()
	if keys[len(keys)-1] != host {
		keys = append(keys, host)
	}

	return keys
}

// loadDockerConfig has to stay in sync with the file discovery of the vendored
// authn.DefaultKeychain, and returns (nil, nil) where that one falls back to anonymous.
// https://github.com/google/go-containerregistry/blob/main/pkg/authn/keychain.go
func loadDockerConfig() (*configfile.ConfigFile, error) {
	foundDockerConfig := false
	home, err := os.UserHomeDir()
	if err == nil {
		foundDockerConfig = fileExists(filepath.Join(home, ".docker/config.json"))
	}
	if !foundDockerConfig && os.Getenv("DOCKER_CONFIG") != "" {
		foundDockerConfig = fileExists(filepath.Join(os.Getenv("DOCKER_CONFIG"), "config.json"))
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" && home != "" {
		configDir = filepath.Join(home, ".config")
	}
	podmanAuth := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "containers/auth.json")
	if (os.Getenv("XDG_RUNTIME_DIR") == "" || !fileExists(podmanAuth)) && configDir != "" {
		podmanAuth = filepath.Join(configDir, "containers/auth.json")
	}

	var cf *configfile.ConfigFile
	if foundDockerConfig {
		cf, err = config.Load(os.Getenv("DOCKER_CONFIG"))
		if err != nil {
			return nil, err
		}
	} else if path := filepath.Clean(os.Getenv("REGISTRY_AUTH_FILE")); fileExists(path) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		cf, err = config.LoadFromReader(f)
		if err != nil {
			return nil, err
		}
	} else if path := filepath.Clean(podmanAuth); fileExists(path) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		cf, err = config.LoadFromReader(f)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, nil
	}

	return cf, nil
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
