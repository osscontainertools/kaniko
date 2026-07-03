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

package config

import (
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/sirupsen/logrus"
)

type FeatureFlags struct {
	BuildkitArgEnvPrecedence       bool
	CacheLookahead                 bool
	CacheProbeAfterMiss            bool
	ChownOnImplicitDirs            bool
	CleanKanikoDir                 bool
	CopyAsRoot                     bool
	CopyChmodOnImplicitDirs        bool
	DeprecateInterStageRestore     bool
	DisableHTTP2                   bool
	ExpandHeredoc                  bool
	HashDirFraming                 bool
	IgnoreCachedManifest           bool
	InferCrossStageCacheKey        bool
	NoPropagateAnnotations         bool
	OCIScratchBase                 bool
	OCIWarmer                      bool
	PrecompileDockerignore         bool
	PreserveHardlinks              bool
	PreserveMountedPaths           bool
	ReproduciblePreserveBaseLayers bool
	ResolveCacheKey                bool
	RollingCacheKey                bool
	RunHonorGroup                  bool
	RunMountBind                   bool
	RunViaTini                     bool
	ScopedDockerignore             bool
	SecurejoinExtraction           bool
	SkipRelabelRecompress          bool
	SkipWriteWhiteouts             bool
	UntarSkipRoot                  bool
	VolumeSkipMkdir                bool
	WarmerCacheLock                bool
}

var FF FeatureFlags

var (
	knownFeatureFlags     []string
	activeFeatureFlags    []string
	redundantFeatureFlags []string
	disabledFeatureFlags  []string
)

func featureFlag(name string, def bool) bool {
	value := EnvBoolDefault(name, def)
	_, explicit := os.LookupEnv(name)
	knownFeatureFlags = append(knownFeatureFlags, name)
	if value {
		activeFeatureFlags = append(activeFeatureFlags, name)
	}
	if def && explicit {
		if value {
			redundantFeatureFlags = append(redundantFeatureFlags, name)
		} else {
			disabledFeatureFlags = append(disabledFeatureFlags, name)
		}
	}
	return value
}

func InitFeatureFlags() {
	knownFeatureFlags = nil
	activeFeatureFlags = nil
	redundantFeatureFlags = nil
	disabledFeatureFlags = nil

	FF = FeatureFlags{
		BuildkitArgEnvPrecedence:       featureFlag("FF_KANIKO_BUILDKIT_ARG_ENV_PRECEDENCE", true),
		CacheLookahead:                 featureFlag("FF_KANIKO_CACHE_LOOKAHEAD", false),
		CacheProbeAfterMiss:            featureFlag("FF_KANIKO_CACHE_PROBE_AFTER_MISS", false),
		ChownOnImplicitDirs:            featureFlag("FF_KANIKO_CHOWN_ON_IMPLICIT_DIRS", false),
		CleanKanikoDir:                 featureFlag("FF_KANIKO_CLEAN_KANIKO_DIR", true),
		CopyAsRoot:                     featureFlag("FF_KANIKO_COPY_AS_ROOT", false),
		CopyChmodOnImplicitDirs:        featureFlag("FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS", false),
		DeprecateInterStageRestore:     featureFlag("FF_KANIKO_DEPRECATE_INTER_STAGE_RESTORE", true),
		DisableHTTP2:                   featureFlag("FF_KANIKO_DISABLE_HTTP2", false),
		ExpandHeredoc:                  featureFlag("FF_KANIKO_EXPAND_HEREDOC", false),
		HashDirFraming:                 featureFlag("FF_KANIKO_HASH_DIR_FRAMING", false),
		IgnoreCachedManifest:           featureFlag("FF_KANIKO_IGNORE_CACHED_MANIFEST", false),
		InferCrossStageCacheKey:        featureFlag("FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY", false),
		NoPropagateAnnotations:         featureFlag("FF_KANIKO_NO_PROPAGATE_ANNOTATIONS", true),
		OCIScratchBase:                 featureFlag("FF_KANIKO_OCI_SCRATCH_BASE", false),
		OCIWarmer:                      featureFlag("FF_KANIKO_OCI_WARMER", true),
		PrecompileDockerignore:         featureFlag("FF_KANIKO_PRECOMPILE_DOCKERIGNORE", false),
		PreserveHardlinks:              featureFlag("FF_KANIKO_PRESERVE_HARDLINKS", true),
		PreserveMountedPaths:           featureFlag("FF_KANIKO_PRESERVE_MOUNTED_PATHS", true),
		ReproduciblePreserveBaseLayers: featureFlag("FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS", false),
		ResolveCacheKey:                featureFlag("FF_KANIKO_RESOLVE_CACHE_KEY", false),
		RollingCacheKey:                featureFlag("FF_KANIKO_ROLLING_CACHE_KEY", false),
		RunHonorGroup:                  featureFlag("FF_KANIKO_RUN_HONOR_GROUP", false),
		RunMountBind:                   featureFlag("FF_KANIKO_RUN_MOUNT_BIND", true),
		RunViaTini:                     featureFlag("FF_KANIKO_RUN_VIA_TINI", false),
		ScopedDockerignore:             featureFlag("FF_KANIKO_SCOPED_DOCKERIGNORE", false),
		SecurejoinExtraction:           featureFlag("FF_KANIKO_SECUREJOIN_EXTRACTION", true),
		SkipRelabelRecompress:          featureFlag("FF_KANIKO_SKIP_RELABEL_RECOMPRESS", false),
		SkipWriteWhiteouts:             featureFlag("FF_KANIKO_SKIP_WRITE_WHITEOUTS", false),
		UntarSkipRoot:                  featureFlag("FF_KANIKO_UNTAR_SKIP_ROOT", false),
		VolumeSkipMkdir:                featureFlag("FF_KANIKO_VOLUME_SKIP_MKDIR", true),
		WarmerCacheLock:                featureFlag("FF_KANIKO_WARMER_CACHE_LOCK", true),
	}

	fields := reflect.TypeFor[FeatureFlags]().NumField()
	unique := len(slices.Compact(slices.Sorted(slices.Values(knownFeatureFlags))))
	assert.Assert("config.featureflags.unique", unique == len(knownFeatureFlags), "FeatureFlags registered %d flags but only %d are unique; each flag name must be registered once", len(knownFeatureFlags), unique)
	assert.Assert("config.featureflags.complete", fields == len(knownFeatureFlags), "FeatureFlags has %d fields but %d are initialized; every field must be set via featureFlag", fields, len(knownFeatureFlags))
}

func init() {
	InitFeatureFlags()
}

func LogFeatureFlags() {
	if len(activeFeatureFlags) > 0 {
		logrus.Infof("active feature flags: %s", strings.Join(activeFeatureFlags, ", "))
	}
	if len(redundantFeatureFlags) > 0 {
		logrus.Infof("feature flags enabled by default, setting them explicitly is no longer necessary: %s", strings.Join(redundantFeatureFlags, ", "))
	}
	if len(disabledFeatureFlags) > 0 {
		logrus.Warnf("feature flags explicitly disabled, please create an issue for your use-case: %s", strings.Join(disabledFeatureFlags, ", "))
	}
	var unknown []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "FF_KANIKO_") && !slices.Contains(knownFeatureFlags, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		logrus.Warnf("unknown feature flags set but not recognised by this version of kaniko: %s", strings.Join(unknown, ", "))
	}
}
