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
	"fmt"

	"github.com/osscontainertools/kaniko/pkg/bake"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/logging"
	"github.com/spf13/cobra"
)

var bakeSet []string

func init() {
	AddBakeFlags(bakeCmd, opts, &bakeSet)
	addHiddenFlags(bakeCmd)
	RootCmd.AddCommand(bakeCmd)
}

func AddBakeFlags(cmd *cobra.Command, opts *config.KanikoOptions, set *[]string) {
	AddSharedBuildFlags(cmd, opts)
	cmd.Flags().StringArrayVar(set, "set", nil, "Override a bakefile target field: <target>.<field>=<value>. Set it repeatedly for multiple overrides.")
}

func ConfigureFromBakefile(opts *config.KanikoOptions, path string, selection, set []string) ([]bake.ResolvedTarget, error) {
	bakefile, err := bake.Parse(path)
	if err != nil {
		return nil, err
	}
	targets, err := bakefile.Resolve(selection)
	if err != nil {
		return nil, err
	}
	overrides := make([]bake.Override, 0, len(set))
	for _, s := range set {
		o, err := bake.ParseOverride(s)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, o)
	}
	if err := bake.ApplyOverrides(targets, overrides); err != nil {
		return nil, err
	}
	for _, t := range targets {
		if !opts.NoPush && len(t.Destination) == 0 {
			return nil, fmt.Errorf("target %q has no destination, set one in the bakefile or use --no-push", t.ID)
		}
	}
	if len(targets) > 1 {
		for _, f := range []struct{ name, value string }{
			{"--digest-file", opts.DigestFile},
			{"--image-name-with-digest-file", opts.ImageNameDigestFile},
			{"--image-name-tag-with-digest-file", opts.ImageNameTagDigestFile},
			{"--tar-path", opts.TarPath},
			{"--oci-layout-path", opts.OCILayoutPath},
		} {
			if f.value != "" {
				return nil, fmt.Errorf("%s writes a single file and cannot be used when building several targets, name one target to build", f.name)
			}
		}
	}
	return targets, nil
}

// ApplyTarget points opts at a single target. A target with no stage keeps whatever
// --target was given.
func ApplyTarget(opts *config.KanikoOptions, target bake.ResolvedTarget) {
	if target.Stage != "" {
		opts.Target = []string{target.Stage}
	}
	opts.Destinations = target.Destination
}

var bakeCmd = &cobra.Command{
	Use:   "bake <bakefile> [target]",
	Short: "Build a target defined in a bakefile",
	Long: `Build a target defined in a bakefile. The bakefile may define several
targets, name the one to build (it may be omitted when there is only one). The
target's stage and push destination come from the bakefile. Context, dockerfile,
build args and other settings come from the usual flags.

The bakefile is HCL and looks like this:

    target "app" {
      target      = "app"
      destination = ["registry.example.com/app:latest"]
    }

This is not a docker-bake.hcl. Variables, functions, groups and inherits are
not supported.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := logging.Configure(logLevel, logFormat, logTimestamp); err != nil {
			return err
		}
		targets, err := ConfigureFromBakefile(opts, args[0], args[1:], bakeSet)
		if err != nil {
			return err
		}
		return runBuildTargets(opts, targets)
	},
}
