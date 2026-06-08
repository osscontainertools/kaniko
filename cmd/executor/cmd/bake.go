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
	"strings"

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

func ConfigureFromBakefile(opts *config.KanikoOptions, path string, selection, set []string) error {
	bakefile, err := bake.Parse(path)
	if err != nil {
		return err
	}
	targets, err := bakefile.Resolve(selection)
	if err != nil {
		return err
	}
	overrides := make([]bake.Override, 0, len(set))
	for _, s := range set {
		o, err := bake.ParseOverride(s)
		if err != nil {
			return err
		}
		overrides = append(overrides, o)
	}
	if err := bake.ApplyOverrides(targets, overrides); err != nil {
		return err
	}
	if len(targets) != 1 {
		ids := make([]string, len(targets))
		for i, t := range targets {
			ids[i] = t.ID
		}
		return fmt.Errorf("bakefile defines multiple targets, name one to build: %s", strings.Join(ids, ", "))
	}
	target := targets[0]
	if !opts.NoPush && len(target.Destination) == 0 {
		return fmt.Errorf("target %q has no destination, set one in the bakefile or use --no-push", target.ID)
	}
	opts.Target = []string{target.Stage}
	for _, d := range target.Destination {
		if err := opts.Destinations.Set(d); err != nil {
			return err
		}
	}
	return nil
}

var bakeCmd = &cobra.Command{
	Use:   "bake <bakefile> [target]",
	Short: "Build a target defined in a JSON bakefile",
	Long: `Build a target defined in a JSON bakefile. The bakefile may define several
targets; name the one to build (it may be omitted when there is only one). The
target's stage and push destination come from the bakefile. Context, dockerfile,
build args and other settings come from the usual flags.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(_ *cobra.Command, args []string) error {
		if err := logging.Configure(logLevel, logFormat, logTimestamp); err != nil {
			return err
		}
		if err := ConfigureFromBakefile(opts, args[0], args[1:], bakeSet); err != nil {
			return err
		}
		return runBuild(opts)
	},
}
