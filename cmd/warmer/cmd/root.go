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
)

func init() {
	RootCmd.Flags().StringVarP(&logLevel, "verbosity", "v", logging.DefaultLevel, "Log level (trace, debug, info, warn, error, fatal, panic)")
	RootCmd.Flags().StringVar(&logFormat, "log-format", logging.FormatColor, "Log format (text, color, json)")
	RootCmd.Flags().BoolVar(&logTimestamp, "log-timestamp", logging.DefaultLogTimestamp, "Timestamp in log output")

	addKanikoOptionsFlags()
	addHiddenFlags()
}

var RootCmd = &cobra.Command{
	Use: "cache warmer",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := logging.Configure(logLevel, logFormat, logTimestamp); err != nil {
			return err
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

		if len(opts.Images) == 0 && opts.DockerfilePath == "" {
			return errors.New("you must select at least one image to cache or a dockerfilepath to parse")
		}

		if opts.DockerfilePath != "" {
			if err := validateDockerfilePath(); err != nil {
				return fmt.Errorf("error validating dockerfile path: %w", err)
			}
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		if _, err := os.Stat(opts.CacheDir); os.IsNotExist(err) {
			err = os.MkdirAll(opts.CacheDir, 0755)
			if err != nil {
				exit(fmt.Errorf("failed to create cache directory: %w", err))
			}
		}
		if err := warmer.WarmCache(opts); err != nil {
			exit(fmt.Errorf("failed warming cache: %w", err))
		}

	},
}

// addKanikoOptionsFlags configures opts
func addKanikoOptionsFlags() {
	RootCmd.Flags().VarP(&opts.Images, "image", "i", "Image to cache. Set it repeatedly for multiple images.")
	RootCmd.Flags().StringVarP(&opts.CacheDir, "cache-dir", "c", "/cache", "Directory of the cache.")
	RootCmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "Force cache overwriting.")
	RootCmd.Flags().DurationVarP(&opts.CacheTTL, "cache-ttl", "", time.Hour*336, "Cache timeout in hours. Defaults to two weeks.")
	RootCmd.Flags().BoolVarP(&opts.InsecurePull, "insecure-pull", "", false, "Pull from insecure registry using plain HTTP")
	RootCmd.Flags().BoolVarP(&opts.SkipTLSVerifyPull, "skip-tls-verify-pull", "", false, "Pull from insecure registry ignoring TLS verify")
	RootCmd.Flags().VarP(&opts.InsecureRegistries, "insecure-registry", "", "Insecure registry using plain HTTP to pull. Set it repeatedly for multiple registries.")
	RootCmd.Flags().VarP(&opts.SkipTLSVerifyRegistries, "skip-tls-verify-registry", "", "Insecure registry ignoring TLS verify to pull. Set it repeatedly for multiple registries.")
	opts.RegistriesCertificates = make(map[string]string)
	RootCmd.Flags().VarP(&opts.RegistriesCertificates, "registry-certificate", "", "Use the provided certificate for TLS communication with the given registry. Expected format is 'my.registry.url=/path/to/the/server/certificate'.")
	opts.RegistriesClientCertificates = make(map[string]string)
	RootCmd.Flags().VarP(&opts.RegistriesClientCertificates, "registry-client-cert", "", "Use the provided client certificate for mutual TLS (mTLS) communication with the given registry. Expected format is 'my.registry.url=/path/to/client/cert,/path/to/client/key'.")
	opts.RegistryMaps = make(map[string][]string)
	RootCmd.Flags().VarP(&opts.RegistryMaps, "registry-map", "", "Registry map of mirror to use as pull-through cache instead. Expected format is 'orignal.registry=new.registry;other-original.registry=other-remap.registry'")
	RootCmd.Flags().VarP(&opts.RegistryMirrors, "registry-mirror", "", "Registry mirror to use as pull-through cache instead of docker.io. Set it repeatedly for multiple mirrors.")
	RootCmd.Flags().BoolVarP(&opts.SkipDefaultRegistryFallback, "skip-default-registry-fallback", "", false, "If an image is not found on any mirrors (defined with registry-mirror) do not fallback to the default registry. If registry-mirror is not defined, this flag is ignored.")
	RootCmd.Flags().StringVarP(&opts.CustomPlatform, "customPlatform", "", "", "Specify the build platform if different from the current host")
	RootCmd.Flags().StringVarP(&opts.DockerfilePath, "dockerfile", "d", "", "Path to the dockerfile to be cached. The kaniko warmer will parse and write out each stage's base image layers to the cache-dir. Using the same dockerfile path as what you plan to build in the kaniko executor is the expected usage.")
	RootCmd.Flags().VarP(&opts.BuildArgs, "build-arg", "", "This flag should be used in conjunction with the dockerfile flag for scenarios where dynamic replacement of the base image is required.")

	// Default the custom platform flag to our current platform, and validate it.
	if opts.CustomPlatform == "" {
		opts.CustomPlatform = platforms.Format(platforms.Normalize(platforms.DefaultSpec()))
	}
	if _, err := v1.ParsePlatform(opts.CustomPlatform); err != nil {
		logrus.Fatalf("Invalid platform %q: %v", opts.CustomPlatform, err)
	}
}

// addHiddenFlags marks certain flags as hidden from the executor help text
func addHiddenFlags() {
	RootCmd.Flags().MarkHidden("azure-container-registry-config")
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

func exit(err error) {
	fmt.Println(err)
	os.Exit(1)
}
