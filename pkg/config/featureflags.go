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
	AddChownTar                bool
	AddFollowSymlink           bool
	CacheCopyFrom              bool
	CacheImageIndex            bool
	CacheProbeAfterMiss        bool
	ChownOnImplicitDirs        bool
	ConfineHardlinkTarget      bool
	CopyAsRoot                 bool
	CopyChmodOnImplicitDirs    bool
	CopyKeepDestOwner          bool
	CopyLink                   bool
	CopySubdirMode             bool
	DeferCachePush             bool
	DisableHTTP2               bool
	FormatIndependentCacheKey  bool
	FromPredefinedArgs         bool
	IsolateCachePush           bool
	LocalUnpackRequiresTar     bool
	NativeInit                 bool
	OCIScratchBase             bool
	ParallelGunzip             bool
	ParallelHash               bool
	RunCrossStageBindMount     bool
	RunHeredocScript           bool
	RunMountBindRelative       bool
	RunMountBindResolveSymlink bool
	RunMountTmpfs              bool
	RunViaTini                 bool
	ScopedDockerignore         bool
	SnapshotTrackCachedLayers  bool
	UnpackXz                   bool
	WarmerBuildArgs            bool
	WarmerFailOnError          bool
	WarmerPredefinedArgs       bool
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
		AddChownTar:                featureFlag("FF_KANIKO_ADD_CHOWN_TAR", true),
		AddFollowSymlink:           featureFlag("FF_KANIKO_ADD_FOLLOW_SYMLINK", true),
		CacheCopyFrom:              featureFlag("FF_KANIKO_CACHE_COPY_FROM", true),
		CacheImageIndex:            featureFlag("FF_KANIKO_CACHE_IMAGE_INDEX", true),
		CacheProbeAfterMiss:        featureFlag("FF_KANIKO_CACHE_PROBE_AFTER_MISS", false),
		ChownOnImplicitDirs:        featureFlag("FF_KANIKO_CHOWN_ON_IMPLICIT_DIRS", false),
		ConfineHardlinkTarget:      featureFlag("FF_KANIKO_CONFINE_HARDLINK_TARGET", true),
		CopyAsRoot:                 featureFlag("FF_KANIKO_COPY_AS_ROOT", false),
		CopyChmodOnImplicitDirs:    featureFlag("FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS", false),
		CopyKeepDestOwner:          featureFlag("FF_KANIKO_COPY_KEEP_DEST_OWNER", true),
		CopyLink:                   featureFlag("FF_KANIKO_COPY_LINK", true),
		CopySubdirMode:             featureFlag("FF_KANIKO_COPY_SUBDIR_MODE", true),
		DeferCachePush:             featureFlag("FF_KANIKO_DEFER_CACHE_PUSH", true),
		DisableHTTP2:               featureFlag("FF_KANIKO_DISABLE_HTTP2", false),
		FormatIndependentCacheKey:  featureFlag("FF_KANIKO_FORMAT_INDEPENDENT_CACHE_KEY", true),
		FromPredefinedArgs:         featureFlag("FF_KANIKO_FROM_PREDEFINED_ARGS", true),
		IsolateCachePush:           featureFlag("FF_KANIKO_ISOLATE_CACHE_PUSH", true),
		LocalUnpackRequiresTar:     featureFlag("FF_KANIKO_LOCAL_UNPACK_REQUIRES_TAR", true),
		NativeInit:                 featureFlag("FF_KANIKO_NATIVE_INIT", true),
		OCIScratchBase:             featureFlag("FF_KANIKO_OCI_SCRATCH_BASE", false),
		ParallelGunzip:             featureFlag("FF_KANIKO_PARALLEL_GUNZIP", true),
		ParallelHash:               featureFlag("FF_KANIKO_PARALLEL_HASH", true),
		RunCrossStageBindMount:     featureFlag("FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT", true),
		RunHeredocScript:           featureFlag("FF_KANIKO_RUN_HEREDOC_SCRIPT", true),
		RunMountBindRelative:       featureFlag("FF_KANIKO_RUN_MOUNT_BIND_RELATIVE", true),
		RunMountBindResolveSymlink: featureFlag("FF_KANIKO_RUN_MOUNT_BIND_RESOLVE_SYMLINK", true),
		RunMountTmpfs:              featureFlag("FF_KANIKO_RUN_MOUNT_TMPFS", true),
		RunViaTini:                 featureFlag("FF_KANIKO_RUN_VIA_TINI", false),
		ScopedDockerignore:         featureFlag("FF_KANIKO_SCOPED_DOCKERIGNORE", false),
		SnapshotTrackCachedLayers:  featureFlag("FF_KANIKO_SNAPSHOT_TRACK_CACHED_LAYERS", true),
		UnpackXz:                   featureFlag("FF_KANIKO_UNPACK_XZ", false),
		WarmerBuildArgs:            featureFlag("FF_KANIKO_WARMER_BUILD_ARGS", true),
		WarmerFailOnError:          featureFlag("FF_KANIKO_WARMER_FAIL_ON_ERROR", true),
		WarmerPredefinedArgs:       featureFlag("FF_KANIKO_WARMER_PREDEFINED_ARGS", true),
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
