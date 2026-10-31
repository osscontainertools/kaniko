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
	AddChecksum                    bool
	AddUnpack                      bool
	CacheCopyFrom                  bool
	CacheHashBlake3                bool
	CacheImageIndex                bool
	CacheLookahead                 bool
	CacheProbeAfterMiss            bool
	ChownOnImplicitDirs            bool
	ConfineCopySource              bool
	CopyAsRoot                     bool
	CopyChmodOnImplicitDirs        bool
	CopyKeepDestOwner              bool
	CopyLink                       bool
	CopySkipSpecialFiles           bool
	CopySubdirMode                 bool
	CrossRepoMount                 bool
	DeferCachePush                 bool
	DeprecateLayerlessCacheEntries bool
	DisableHTTP2                   bool
	ExpandHeredoc                  bool
	FormatIndependentCacheKey      bool
	HashDirFraming                 bool
	ImageStages                    bool
	InferCrossStageCacheKey        bool
	IsolateCachePush               bool
	LayerHints                     bool
	NativeCopy                     bool
	NativeInit                     bool
	OCIScratchBase                 bool
	PathScopedRegistryAuth         bool
	PeekArchiveHeader              bool
	PlatformCacheKey               bool
	PoolRegistryConnections        bool
	PrecompileDockerignore         bool
	PreserveMountedSymlinks        bool
	ReproduciblePreserveBaseLayers bool
	ReproduciblePreserveFormat     bool
	ResolveCacheKey                bool
	RollingCacheKey                bool
	RunHeredocScript               bool
	RunHonorGroup                  bool
	RunViaTini                     bool
	ScopedDockerignore             bool
	ScopedRegistryCertificates     bool
	SharedBaseCache                bool
	SkipCachedStages               bool
	SkipRelabelRecompress          bool
	SkipWriteWhiteouts             bool
	UnpackXz                       bool
	UnpackZstd                     bool
	UntarSkipRoot                  bool
	WarmerBuildArgs                bool
	WarmerPredefinedArgs           bool
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
		AddChecksum:                    featureFlag("FF_KANIKO_ADD_CHECKSUM", true),
		AddUnpack:                      featureFlag("FF_KANIKO_ADD_UNPACK", true),
		CacheCopyFrom:                  featureFlag("FF_KANIKO_CACHE_COPY_FROM", true),
		CacheHashBlake3:                featureFlag("FF_KANIKO_CACHE_HASH_BLAKE3", true),
		CacheImageIndex:                featureFlag("FF_KANIKO_CACHE_IMAGE_INDEX", false),
		CacheLookahead:                 featureFlag("FF_KANIKO_CACHE_LOOKAHEAD", true),
		CacheProbeAfterMiss:            featureFlag("FF_KANIKO_CACHE_PROBE_AFTER_MISS", false),
		ChownOnImplicitDirs:            featureFlag("FF_KANIKO_CHOWN_ON_IMPLICIT_DIRS", false),
		ConfineCopySource:              featureFlag("FF_KANIKO_CONFINE_COPY_SOURCE", true),
		CopyAsRoot:                     featureFlag("FF_KANIKO_COPY_AS_ROOT", false),
		CopyChmodOnImplicitDirs:        featureFlag("FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS", false),
		CopyKeepDestOwner:              featureFlag("FF_KANIKO_COPY_KEEP_DEST_OWNER", false),
		CopyLink:                       featureFlag("FF_KANIKO_COPY_LINK", false),
		CopySkipSpecialFiles:           featureFlag("FF_KANIKO_COPY_SKIP_SPECIAL_FILES", true),
		CopySubdirMode:                 featureFlag("FF_KANIKO_COPY_SUBDIR_MODE", false),
		CrossRepoMount:                 featureFlag("FF_KANIKO_CROSS_REPO_MOUNT", true),
		DeferCachePush:                 featureFlag("FF_KANIKO_DEFER_CACHE_PUSH", false),
		DeprecateLayerlessCacheEntries: featureFlag("FF_KANIKO_DEPRECATE_LAYERLESS_CACHE_ENTRIES", true),
		DisableHTTP2:                   featureFlag("FF_KANIKO_DISABLE_HTTP2", false),
		ExpandHeredoc:                  featureFlag("FF_KANIKO_EXPAND_HEREDOC", true),
		FormatIndependentCacheKey:      featureFlag("FF_KANIKO_FORMAT_INDEPENDENT_CACHE_KEY", false),
		HashDirFraming:                 featureFlag("FF_KANIKO_HASH_DIR_FRAMING", true),
		ImageStages:                    featureFlag("FF_KANIKO_IMAGE_STAGES", true),
		InferCrossStageCacheKey:        featureFlag("FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY", true),
		IsolateCachePush:               featureFlag("FF_KANIKO_ISOLATE_CACHE_PUSH", false),
		LayerHints:                     featureFlag("FF_KANIKO_LAYER_HINTS", true),
		NativeCopy:                     featureFlag("FF_KANIKO_NATIVE_COPY", true),
		NativeInit:                     featureFlag("FF_KANIKO_NATIVE_INIT", false),
		OCIScratchBase:                 featureFlag("FF_KANIKO_OCI_SCRATCH_BASE", false),
		PathScopedRegistryAuth:         featureFlag("FF_KANIKO_PATH_SCOPED_REGISTRY_AUTH", true),
		PeekArchiveHeader:              featureFlag("FF_KANIKO_PEEK_ARCHIVE_HEADER", true),
		PlatformCacheKey:               featureFlag("FF_KANIKO_PLATFORM_CACHE_KEY", true),
		PoolRegistryConnections:        featureFlag("FF_KANIKO_POOL_REGISTRY_CONNECTIONS", true),
		PrecompileDockerignore:         featureFlag("FF_KANIKO_PRECOMPILE_DOCKERIGNORE", true),
		PreserveMountedSymlinks:        featureFlag("FF_KANIKO_PRESERVE_MOUNTED_SYMLINKS", true),
		ReproduciblePreserveBaseLayers: featureFlag("FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS", true),
		ReproduciblePreserveFormat:     featureFlag("FF_KANIKO_REPRODUCIBLE_PRESERVE_FORMAT", true),
		ResolveCacheKey:                featureFlag("FF_KANIKO_RESOLVE_CACHE_KEY", true),
		RollingCacheKey:                featureFlag("FF_KANIKO_ROLLING_CACHE_KEY", true),
		RunHeredocScript:               featureFlag("FF_KANIKO_RUN_HEREDOC_SCRIPT", false),
		RunHonorGroup:                  featureFlag("FF_KANIKO_RUN_HONOR_GROUP", true),
		RunViaTini:                     featureFlag("FF_KANIKO_RUN_VIA_TINI", false),
		ScopedDockerignore:             featureFlag("FF_KANIKO_SCOPED_DOCKERIGNORE", false),
		ScopedRegistryCertificates:     featureFlag("FF_KANIKO_SCOPED_REGISTRY_CERTIFICATES", true),
		SharedBaseCache:                featureFlag("FF_KANIKO_SHARED_BASE_CACHE", true),
		SkipCachedStages:               featureFlag("FF_KANIKO_SKIP_CACHED_STAGES", true),
		SkipRelabelRecompress:          featureFlag("FF_KANIKO_SKIP_RELABEL_RECOMPRESS", true),
		SkipWriteWhiteouts:             featureFlag("FF_KANIKO_SKIP_WRITE_WHITEOUTS", true),
		UnpackXz:                       featureFlag("FF_KANIKO_UNPACK_XZ", false),
		UnpackZstd:                     featureFlag("FF_KANIKO_UNPACK_ZSTD", true),
		UntarSkipRoot:                  featureFlag("FF_KANIKO_UNTAR_SKIP_ROOT", true),
		WarmerBuildArgs:                featureFlag("FF_KANIKO_WARMER_BUILD_ARGS", false),
		WarmerPredefinedArgs:           featureFlag("FF_KANIKO_WARMER_PREDEFINED_ARGS", false),
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
