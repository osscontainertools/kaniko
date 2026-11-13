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
	"os"
	"path/filepath"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/bake"
	"github.com/osscontainertools/kaniko/pkg/buildenv"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/logging"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/spf13/cobra"
)

var (
	bakeSet   []string
	bakePrint bool
)

func init() {
	AddBakeFlags(bakeCmd, opts, &bakeSet)
	bakeCmd.Flags().BoolVar(&bakePrint, "print", false, "Print the resolved targets as JSON and exit without building")
	addHiddenFlags(bakeCmd)
	RootCmd.AddCommand(bakeCmd)
}

func AddBakeFlags(cmd *cobra.Command, opts *config.KanikoOptions, set *[]string) {
	AddSharedBuildFlags(cmd, opts)
	cmd.Flags().StringArrayVar(set, "set", nil, "Override a bakefile target field: <target>.<field>=<value>. Set it repeatedly for multiple overrides.")
}

func ConfigureFromBakefile(opts *config.KanikoOptions, path string, selection, set []string) ([]bake.ResolvedTarget, error) {
	return bake.Load(path, selection, set)
}

func ValidateBakeBuild(opts *config.KanikoOptions, targets []bake.ResolvedTarget) error {
	if err := validateOutputFlags(opts); err != nil {
		return err
	}
	for _, t := range targets {
		if !opts.NoPush && len(t.Destination) == 0 {
			return fmt.Errorf("target %q has no destination, set one in the bakefile or use --no-push", t.ID)
		}
	}
	if len(targets) > 1 {
		for _, f := range []struct{ name, value string }{
			{"--digest-file", opts.DigestFile},
			{"--image-name-with-digest-file", opts.ImageNameDigestFile},
			{"--image-name-tag-with-digest-file", opts.ImageNameTagDigestFile},
			{"--index-file", opts.IndexFile},
			{"--tar-path", opts.TarPath},
			{"--oci-layout-path", opts.OCILayoutPath},
		} {
			if f.value != "" {
				return fmt.Errorf("%s writes a single file and cannot be used when building several targets, name one target to build", f.name)
			}
		}
	}
	return nil
}

var bakeCmd = &cobra.Command{
	Use:   "bake <bakefile> [target]",
	Short: "Build a target defined in a bakefile",
	Long: `Build a target defined in a bakefile. The bakefile may define several
targets, name the one to build (it may be omitted when there is only one). The
target's stage and push destination come from the bakefile. Context and
dockerfile come from the usual flags unless the target sets its own. Build args
and other settings come from the usual flags.

The bakefile is HCL and looks like this:

    target "app" {
      target      = "app"
      destination = ["registry.example.com/app:latest"]
    }

It is the analogue of a docker-bake.hcl, expressed in kaniko's commands rather
than buildx's. A docker-bake.hcl will not parse. Variables, functions, groups
and inherits are not supported.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := logging.Configure(logLevel, logFormat, logTimestamp); err != nil {
			return err
		}
		config.LogFeatureFlags()
		util.ProbeOpenat2()
		opts.CompressionLevelSet = cmd.Flags().Changed("compression-level")
		if opts.CompressionLevelSet {
			err := validateCompressionLevel(opts.Compression, opts.CompressionLevel)
			if err != nil {
				return err
			}
		}
		targets, err := ConfigureFromBakefile(opts, args[0], args[1:], bakeSet)
		if err != nil {
			return err
		}
		if bakePrint {
			out, err := bake.Print(targets)
			if err != nil {
				return err
			}
			_, err = fmt.Println(string(out))
			return err
		}
		if err := ValidateBakeBuild(opts, targets); err != nil {
			return err
		}
		if err := prepareBuild(opts, targets); err != nil {
			return err
		}
		// The filesystem is deleted between targets, so with several targets the inputs live in the kaniko dir.
		store := len(targets) > 1
		if store {
			if err := os.MkdirAll(config.KanikoBuildContextsDir, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", config.KanikoBuildContextsDir, err)
			}
		}
		type resolvedContext struct {
			inputs         targetInputs
			dockerfileBase string
		}
		contexts := map[string]resolvedContext{}
		inputs := make([]targetInputs, len(targets))
		for i, target := range targets {
			topts := *opts
			if err := ApplyTarget(&topts, target); err != nil {
				return err
			}
			srcContext := topts.SrcContext
			key := strings.Join([]string{srcContext, topts.Bucket, topts.ContextSubPath}, "\x00")
			c, seen := contexts[key]
			if !seen {
				if err := resolveContext(&topts); err != nil {
					return err
				}
				c = resolvedContext{inputs: targetInputs{srcContext: topts.SrcContext, source: topts.ContextSource, ci: topts.CI}, dockerfileBase: topts.SrcContext}
				local := srcContext != "" && !strings.Contains(srcContext, "://")
				switch {
				case store && local:
					c.inputs.stored = filepath.Join(config.KanikoBuildContextsDir, fmt.Sprintf("context-%d", len(contexts)))
					if err := util.CopyTree(topts.SrcContext, c.inputs.stored, util.FileContext{}, true); err != nil {
						return fmt.Errorf("storing context %s: %w", srcContext, err)
					}
					c.inputs.srcContext = filepath.Clean(config.BuildContextDir)
				case store:
					c.inputs.stored = filepath.Join(config.KanikoBuildContextsDir, fmt.Sprintf("context-%d", len(contexts)))
					rel, err := filepath.Rel(config.BuildContextDir, topts.SrcContext)
					if err != nil {
						return fmt.Errorf("locating context in %s: %w", config.BuildContextDir, err)
					}
					if err := os.Rename(filepath.Clean(config.BuildContextDir), c.inputs.stored); err != nil {
						return fmt.Errorf("storing context %s: %w", srcContext, err)
					}
					c.dockerfileBase = filepath.Join(c.inputs.stored, rel)
					c.inputs.srcContext = filepath.Join(config.BuildContextDir, rel)
				case local:
					abs, err := filepath.Abs(topts.SrcContext)
					if err != nil {
						return fmt.Errorf("getting absolute path for context: %w", err)
					}
					c.inputs.srcContext = abs
				}
				contexts[key] = c
			}
			topts.SrcContext = c.dockerfileBase
			if err := resolveDockerfilePath(&topts); err != nil {
				return fmt.Errorf("error resolving dockerfile path: %w", err)
			}
			in := c.inputs
			in.dockerfile = topts.DockerfilePath
			if store && !isURL(in.dockerfile) {
				in.dockerfile = filepath.Join(config.KanikoBuildContextsDir, fmt.Sprintf("Dockerfile-%d", i))
				if err := os.Rename(config.DockerfilePath, in.dockerfile); err != nil {
					return fmt.Errorf("storing dockerfile of target %q: %w", target.ID, err)
				}
				ignore := config.DockerfilePath + ".dockerignore"
				if util.FilepathExists(ignore) {
					if err := os.Rename(ignore, in.dockerfile+".dockerignore"); err != nil {
						return fmt.Errorf("storing dockerignore of target %q: %w", target.ID, err)
					}
				}
			}
			inputs[i] = in
		}
		// tracing reads the first target's Dockerfile through opts when it exports.
		opts.SrcContext, opts.DockerfilePath, opts.ContextSource, opts.CI = inputs[0].srcContext, inputs[0].dockerfile, inputs[0].source, inputs[0].ci
		return runBuildTargets(opts, targets, inputs)
	},
}

type targetInputs struct {
	stored     string
	srcContext string
	dockerfile string
	source     buildenv.Source
	ci         buildenv.CI
}
