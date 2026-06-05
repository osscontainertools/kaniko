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

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/executor"
	"github.com/osscontainertools/kaniko/pkg/logging"
	"github.com/spf13/cobra"
)

var pushOpts = &config.KanikoOptions{}

func init() {
	AddRegistryOptionsFlags(pushCmd, &pushOpts.RegistryOptions)
	pushCmd.Flags().VarP(&pushOpts.Destinations, "destination", "d", "Registry the image should be pushed to. Set repeatedly for multiple destinations.")
	pushCmd.Flags().BoolVar(&pushOpts.SkipPushPermissionCheck, "skip-push-permission-check", false, "Skip check of the push permission")
	RootCmd.AddCommand(pushCmd)
}

var pushCmd = &cobra.Command{
	Use:   "push <path>",
	Short: "Push a pre-built image to a registry",
	Long: `Push a pre-built image to one or more registries. The path may point at a
docker-save format tarball (regular file) or at an OCI image layout (directory).`,
	Args: cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		err := logging.Configure(logLevel, logFormat, logTimestamp)
		if err != nil {
			return err
		}
		if len(pushOpts.Destinations) == 0 {
			return errors.New("at least one --destination is required")
		}

		path, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}
		_, err = os.Stat(path)
		if err != nil {
			return fmt.Errorf("path %s not readable: %w", path, err)
		}

		err = executor.CheckPushPermissions(pushOpts)
		if err != nil {
			return fmt.Errorf("checking push permissions: %w", err)
		}

		image, err := executor.LoadImage(path)
		if err != nil {
			return fmt.Errorf("loading image: %w", err)
		}

		err = executor.DoPush(image, pushOpts)
		if err != nil {
			return fmt.Errorf("pushing image: %w", err)
		}
		return nil
	},
}
