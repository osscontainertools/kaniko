/*
Copyright 2018 Google LLC

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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/containerd/platforms"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/osscontainertools/kaniko/pkg/bake"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/logging"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/osscontainertools/kaniko/pkg/warmer"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	opts         = &config.WarmerOptions{}
	logLevel     string
	logFormat    string
	logTimestamp bool

	bakeSet       []string
	bakePrint     bool
	bakeCacheFrom []string
)

func init() {
	RootCmd.PersistentFlags().StringVarP(&logLevel, "verbosity", "v", logging.DefaultLevel, "Log level (trace, debug, info, warn, error, fatal, panic)")
	RootCmd.PersistentFlags().StringVar(&logFormat, "log-format", logging.FormatColor, "Log format (text, color, json)")
	RootCmd.PersistentFlags().BoolVar(&logTimestamp, "log-timestamp", logging.DefaultLogTimestamp, "Timestamp in log output")

	AddWarmerFlags(RootCmd, opts)
	addHiddenFlags(RootCmd)

	AddWarmerBakeFlags(bakeCmd)
	RootCmd.AddCommand(bakeCmd)
}

var RootCmd = &cobra.Command{
	Use: "cache warmer",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		return configureWarmerCommon(opts)
	},
	RunE: func(_ *cobra.Command, _ []string) error {
		if len(opts.Images) == 0 && opts.DockerfilePath == "" {
			return errors.New("you must select at least one image to cache or a dockerfilepath to parse")
		}

		if opts.DockerfilePath != "" {
			if err := validateDockerfilePath(); err != nil {
				return fmt.Errorf("error validating dockerfile path: %w", err)
			}
		}

		if !opts.Dryrun {
			if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
				return fmt.Errorf("failed to create cache directory: %w", err)
			}
		}
		if err := warmer.WarmCache(opts); err != nil {
			return fmt.Errorf("failed warming cache: %w", err)
		}
		util.LogRegistryConnections()
		return nil
	},
}

func configureWarmerCommon(opts *config.WarmerOptions) error {
	if err := logging.Configure(logLevel, logFormat, logTimestamp); err != nil {
		return err
	}
	config.LogFeatureFlags()
	if opts.CustomPlatform == "" {
		opts.CustomPlatform = platforms.Format(platforms.Normalize(platforms.DefaultSpec()))
	}

	// Allow setting --registry-maps using an environment variable.
	// some users use warmer with --regisry-mirror before v1.21.0
	// TODO may need all executors validation in here

	if val, ok := os.LookupEnv("KANIKO_REGISTRY_MAP"); ok {
		err := opts.RegistryMaps.Set(val)
		if err != nil {
			return err
		}
	}

	for _, target := range opts.RegistryMirrors {
		err := opts.RegistryMaps.Set(fmt.Sprintf("%s=%s", name.DefaultRegistry, target))
		if err != nil {
			return err
		}
	}

	if len(opts.RegistryMaps) > 0 {
		for src, dsts := range opts.RegistryMaps {
			logrus.Debugf("registry-map remaps %s to %s.", src, strings.Join(dsts, ", "))
		}
	}

	if config.FF.WarmerBuildArgs {
		for i, arg := range opts.BuildArgs {
			if !strings.Contains(arg, "=") {
				opts.BuildArgs[i] = fmt.Sprintf("%s=%s", arg, os.Getenv(arg))
			}
		}
	}

	return nil
}

type bakeTargetPlan struct {
	ID         string   `json:"id"`
	CacheDir   string   `json:"cache_dir"`
	Dockerfile string   `json:"dockerfile"`
	Images     []string `json:"images"`
}

var bakeCmd = &cobra.Command{
	Use:   "bake <bakefile> [target]",
	Short: "Warm base image caches for targets defined in a bakefile",
	Long: `Warm base image caches for targets defined in a bakefile. The bakefile may define several
targets; name the one to warm (it may be omitted when there is only one). Each target writes
base images to its own cache directory when it sets an oci:-prefixed cache_from entry, or to
the global --cache-dir when it does not. Across targets warmed in a single invocation, either
every target must have an oci: cache_from entry or none may.`,
	Args: cobra.RangeArgs(1, 2),
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		return configureWarmerCommon(opts)
	},
	RunE: func(_ *cobra.Command, args []string) error {
		bakefile, err := bake.Parse(args[0])
		if err != nil {
			return err
		}
		selection := args[1:]
		targets, err := bakefile.Resolve(selection)
		if err != nil {
			return err
		}
		overrides := make([]bake.Override, 0, len(bakeSet))
		for _, s := range bakeSet {
			o, err := bake.ParseOverride(s)
			if err != nil {
				return err
			}
			overrides = append(overrides, o)
		}
		if err := bake.ApplyOverrides(targets, overrides); err != nil {
			return err
		}

		if err := validateBakeCacheConsistency(targets); err != nil {
			return err
		}

		globalCacheDir := opts.CacheDir
		if ociDir, ok, err := globalOCICacheDirFromFlags(bakeCacheFrom); err != nil {
			return err
		} else if ok {
			globalCacheDir = ociDir
		}

		plans := make([]bakeTargetPlan, 0, len(targets))
		type targetWork struct {
			cacheDir string
			images   []string
			opts     *config.WarmerOptions
		}
		works := make([]targetWork, 0, len(targets))

		for _, t := range targets {
			cacheDir := warmer.TargetCacheDir(&t, globalCacheDir)
			dockerfilePath := t.Dockerfile
			if dockerfilePath == "" {
				return fmt.Errorf("target %q has no dockerfile", t.ID)
			}
			if !filepath.IsAbs(dockerfilePath) {
				if t.Context != "" {
					ctx := t.Context
					if !filepath.IsAbs(ctx) && !strings.Contains(ctx, "://") {
						ctx = filepath.Join(bakefile.Dir, ctx)
					}
					if !strings.Contains(ctx, "://") {
						dockerfilePath = filepath.Join(ctx, dockerfilePath)
					} else {
						// Dockerfile relative to remote context is not supported for warmer
						dockerfilePath = filepath.Join(bakefile.Dir, dockerfilePath)
					}
				} else {
					dockerfilePath = filepath.Join(bakefile.Dir, dockerfilePath)
				}
			}

			topts := *opts
			topts.DockerfilePath = dockerfilePath
			topts.CacheDir = cacheDir
			topts.Images = nil
			topts.Dryrun = false
			images, err := warmer.ParseDockerfile(&topts)
			if err != nil {
				return fmt.Errorf("target %q: parsing dockerfile %s: %w", t.ID, dockerfilePath, err)
			}
			if opts.RequireDigest {
				for _, img := range images {
					if _, err := name.NewDigest(img); err != nil {
						return fmt.Errorf("--require-digest: %w", err)
					}
				}
			}
			plans = append(plans, bakeTargetPlan{
				ID:         t.ID,
				CacheDir:   cacheDir,
				Dockerfile: dockerfilePath,
				Images:     images,
			})
			works = append(works, targetWork{cacheDir: cacheDir, images: images, opts: &topts})
		}

		if bakePrint {
			out, err := json.MarshalIndent(map[string]any{"target": plansToJSON(targets, plans)}, "", "  ")
			if err != nil {
				return err
			}
			_, err = fmt.Println(string(out))
			return err
		}

		if opts.Dryrun || config.EnvBool("KANIKO_PRINT_PLAN") {
			for i, p := range plans {
				_, err := fmt.Fprintf(os.Stdout, "TARGET %s\n", p.ID)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(os.Stdout, "  CACHE_DIR %s\n", p.CacheDir)
				if err != nil {
					return err
				}
				if err := warmer.RenderPlan(os.Stdout, works[i].images, works[i].opts); err != nil {
					return err
				}
			}
			if opts.Dryrun {
				return nil
			}
		}

		for _, w := range works {
			if err := os.MkdirAll(w.cacheDir, 0o755); err != nil {
				return fmt.Errorf("failed to create cache directory %s: %w", w.cacheDir, err)
			}
			if err := warmer.WarmImagesInto(w.cacheDir, w.images, w.opts); err != nil {
				return err
			}
		}
		util.LogRegistryConnections()
		return nil
	},
}

func plansToJSON(targets []bake.ResolvedTarget, plans []bakeTargetPlan) map[string]any {
	// Return resolved target info enriched with computed cache_dir for --print
	byID := make(map[string]any, len(targets))
	for i, t := range targets {
		m := map[string]any{}
		if t.Context != "" {
			m["context"] = t.Context
		}
		if t.ContextSubPath != "" {
			m["context_sub_path"] = t.ContextSubPath
		}
		if t.Dockerfile != "" {
			m["dockerfile"] = t.Dockerfile
		}
		if t.Stage != "" {
			m["target"] = t.Stage
		}
		if len(t.Destination) > 0 {
			m["destination"] = t.Destination
		}
		if len(t.CacheFrom) > 0 {
			m["cache_from"] = t.CacheFrom
		}
		if len(t.CacheTo) > 0 {
			m["cache_to"] = t.CacheTo
		}
		m["resolved_cache_dir"] = plans[i].CacheDir
		byID[t.ID] = m
	}
	return byID
}

func globalOCICacheDirFromFlags(cacheFrom []string) (string, bool, error) {
	var ociDir string
	for _, ref := range cacheFrom {
		if !strings.HasPrefix(ref, "oci:") {
			return "", false, fmt.Errorf("warmer bake only supports oci: cache-from (got %q)", ref)
		}
		dir := strings.TrimPrefix(ref, "oci:")
		if dir == "" {
			return "", false, errors.New("oci: cache-from requires a directory path")
		}
		if ociDir != "" && ociDir != dir {
			return "", false, errors.New("multiple oci: cache-from values are not supported")
		}
		ociDir = dir
	}
	if ociDir == "" {
		return "", false, nil
	}
	return ociDir, true, nil
}

func validateBakeCacheConsistency(targets []bake.ResolvedTarget) error {
	hasOci := false
	hasNonOci := false
	for _, t := range targets {
		found := false
		for _, ref := range t.CacheFrom {
			if strings.HasPrefix(ref, "oci:") {
				found = true
				break
			}
		}
		if found {
			hasOci = true
		} else {
			hasNonOci = true
		}
	}
	if hasOci && hasNonOci {
		return errors.New("mixing per-target oci caches with shared --cache-dir is not supported")
	}
	return nil
}

// AddWarmerBakeFlags configures bake-specific flags
func AddWarmerBakeFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&bakeSet, "set", nil, "Override a bakefile target field: <target>.<field>=<value>. Set it repeatedly for multiple overrides.")
	cmd.Flags().BoolVar(&bakePrint, "print", false, "Print the resolved targets as JSON and exit without warming")
	cmd.Flags().StringArrayVar(&bakeCacheFrom, "cache-from", nil, "OCI layout cache directory to warm into as an alternative to --cache-dir for targets without per-target oci: cache_from (e.g. oci:/cache).")
}

// AddWarmerFlags configures opts
func AddWarmerFlags(cmd *cobra.Command, opts *config.WarmerOptions) {
	cmd.Flags().VarP(&opts.Images, "image", "i", "Image to cache. Set it repeatedly for multiple images.")
	cmd.Flags().StringVarP(&opts.CacheDir, "cache-dir", "c", "/cache", "Directory of the cache.")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "Force cache overwriting.")
	cmd.Flags().DurationVarP(&opts.CacheTTL, "cache-ttl", "", time.Hour*336, "Cache timeout in hours. Defaults to two weeks.")
	cmd.Flags().BoolVarP(&opts.InsecurePull, "insecure-pull", "", false, "Pull from insecure registry using plain HTTP")
	cmd.Flags().BoolVarP(&opts.SkipTLSVerifyPull, "skip-tls-verify-pull", "", false, "Pull from insecure registry ignoring TLS verify")
	cmd.Flags().VarP(&opts.InsecureRegistries, "insecure-registry", "", "Insecure registry using plain HTTP to pull. Set it repeatedly for multiple registries.")
	cmd.Flags().VarP(&opts.SkipTLSVerifyRegistries, "skip-tls-verify-registry", "", "Insecure registry ignoring TLS verify to pull. Set it repeatedly for multiple registries.")
	opts.RegistriesCertificates = make(map[string]string)
	cmd.Flags().VarP(&opts.RegistriesCertificates, "registry-certificate", "", "Use the provided certificate for TLS communication with the given registry. Expected format is 'my.registry.url=/path/to/the/server/certificate'.")
	opts.RegistriesClientCertificates = make(map[string]string)
	cmd.Flags().VarP(&opts.RegistriesClientCertificates, "registry-client-cert", "", "Use the provided client certificate for mutual TLS (mTLS) communication with the given registry. Expected format is 'my.registry.url=/path/to/client/cert,/path/to/client/key'.")
	opts.RegistryMaps = make(map[string][]string)
	cmd.Flags().VarP(&opts.RegistryMaps, "registry-map", "", "Registry map of mirror to use as pull-through cache instead. Expected format is 'orignal.registry=new.registry;other-original.registry=other-remap.registry'")
	cmd.Flags().VarP(&opts.RegistryMirrors, "registry-mirror", "", "Registry mirror to use as pull-through cache instead of docker.io. Set it repeatedly for multiple mirrors.")
	cmd.Flags().BoolVarP(&opts.SkipDefaultRegistryFallback, "skip-default-registry-fallback", "", false, "If an image is not found on any mirrors (defined with registry-mirror) do not fallback to the default registry. If registry-mirror is not defined, this flag is ignored.")
	cmd.Flags().StringVarP(&opts.CustomPlatform, "customPlatform", "", "", "Specify the build platform if different from the current host")
	cmd.Flags().StringVarP(&opts.DockerfilePath, "dockerfile", "d", "", "Path to the dockerfile to be cached. The kaniko warmer will parse and write out each stage's base image layers to the cache-dir. Using the same dockerfile path as what you plan to build in the kaniko executor is the expected usage.")
	cmd.Flags().VarP(&opts.BuildArgs, "build-arg", "", "This flag should be used in conjunction with the dockerfile flag for scenarios where dynamic replacement of the base image is required.")
	cmd.Flags().BoolVarP(&opts.Dryrun, "dryrun", "", false, "Whether to only print the images that would be cached")
	cmd.Flags().BoolVarP(&opts.RequireDigest, "require-digest", "", false, "Fail if a base image is not pinned by digest")

	// Default the custom platform flag to our current platform, and validate it.
	if opts.CustomPlatform == "" {
		opts.CustomPlatform = platforms.Format(platforms.Normalize(platforms.DefaultSpec()))
	}
	if _, err := v1.ParsePlatform(opts.CustomPlatform); err != nil {
		logrus.Fatalf("Invalid platform %q: %v", opts.CustomPlatform, err)
	}
}

// addHiddenFlags marks certain flags as hidden from the help text
func addHiddenFlags(cmd *cobra.Command) {
	cmd.Flags().MarkHidden("azure-container-registry-config")
}

func validateDockerfilePath() error {
	if isURL(opts.DockerfilePath) {
		return nil
	}
	if util.FilepathExists(opts.DockerfilePath) {
		abs, err := filepath.Abs(opts.DockerfilePath)
		if err != nil {
			return fmt.Errorf("getting absolute path for dockerfile: %w", err)
		}
		opts.DockerfilePath = abs
		return nil
	}

	return errors.New("please provide a valid path to a Dockerfile within the build context with --dockerfile")
}

func isURL(path string) bool {
	if match, _ := regexp.MatchString("^https?://", path); match {
		return true
	}
	return false
}
