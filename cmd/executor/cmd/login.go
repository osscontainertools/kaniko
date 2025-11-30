/*
Copyright 2025 OSSContainerTools

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

package cmd

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/types"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/spf13/cobra"
)

var (
	username      string
	password      string
	passwordStdin bool
)

func init() {
	loginCmd.Flags().StringVarP(&username, "username", "u", "", "Username for the registry")
	loginCmd.Flags().StringVarP(&password, "password", "p", "", "Password for the registry")
	loginCmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "Accept the registry password via stdin")

	RootCmd.AddCommand(loginCmd)
}

var loginCmd = &cobra.Command{
	Use:   "login <registry>",
	Short: "Log into a container registry",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		if username == "" {
			return errors.New("username required")
		}

		if password == "" && !passwordStdin {
			return errors.New("a password must be supplied via --password or --password-stdin")
		}

		if password != "" && passwordStdin {
			return errors.New("only one of --password or --password-stdin may be specified")
		}

		if passwordStdin {
			contents, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}

			password = strings.TrimSuffix(string(contents), "\n")
			password = strings.TrimSuffix(password, "\r")
			if password == "" {
				return errors.New("password is blank")
			}
		}

		cf, err := config.Load(os.Getenv("DOCKER_CONFIG"))
		if err != nil {
			return err
		}

		registry := args[0]

		creds := cf.GetCredentialsStore(registry)

		if registry == name.DefaultRegistry {
			registry = authn.DefaultAuthKey
		}

		if err := creds.Store(types.AuthConfig{
			ServerAddress: registry,
			Username:      username,
			Password:      password,
		}); err != nil {
			return err
		}

		return cf.Save()
	},
}
