/*
Copyright 2022 Google LLC

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
	"io"
	"os"
	"strings"

	ecr "github.com/awslabs/amazon-ecr-credential-helper/ecr-login"
	"github.com/chrismellard/docker-credential-acr-env/pkg/credhelper"
	gitlab "github.com/ePirat/docker-credential-gitlabci/pkg/credhelper"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/google"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

// GetKeychain returns a keychain for accessing container registries.
func GetKeychain(opts *config.RegistryOptions) authn.Keychain {
	var helpers []string
	var prios []string

	_, ok := os.LookupEnv("DOCKER_AUTH_CONFIG")
	if ok {
		prios = append(prios, "env:DOCKER_AUTH_CONFIG")
	}

	cf := util.DockerConfLocation()
	_, err := os.Lstat(cf)
	if err == nil {
		prios = append(prios, "file:"+cf)
	}

	if len(opts.CredentialHelpers) == 0 {
		helpers = []string{"env", "google", "ecr", "acr", "gitlab"}
	} else {
		helpers = opts.CredentialHelpers
	}
	prios = append(prios, helpers...)

	keychains := []authn.Keychain{authn.DefaultKeychain}
	for _, source := range helpers {
		switch source {
		case "":
			logrus.Info("all credential helpers disabled")
		case "env":
			keychains = append(keychains,
				authn.NewKeychainFromHelper(EnvCredentialsHelper),
			)
		case "google":
			keychains = append(keychains, google.Keychain)
		case "ecr":
			keychains = append(keychains,
				authn.NewKeychainFromHelper(
					ecr.NewECRHelper(ecr.WithLogger(io.Discard)),
				),
			)
		case "acr":
			keychains = append(keychains,
				authn.NewKeychainFromHelper(
					credhelper.NewACRCredentialsHelper(),
				),
			)
		case "gitlab":
			keychains = append(keychains,
				authn.NewKeychainFromHelper(
					gitlab.NewGitLabCredentialsHelper(),
				),
			)
		default:
			logrus.Warnf("Unknown cred-source %q, skipping.", source)
		}
	}

	logrus.Infof("credential providers by priority: [%s]", strings.Join(prios, ", "))
	return authn.NewMultiKeychain(keychains...)
}
