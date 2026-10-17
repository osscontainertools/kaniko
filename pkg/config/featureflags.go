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
	CacheImageIndex         bool
	CacheProbeAfterMiss     bool
	ChownOnImplicitDirs     bool
	CopyAsRoot              bool
	CopyChmodOnImplicitDirs bool
	CopyLink                bool
	DeferCachePush          bool
	DisableHTTP2            bool
	OCIScratchBase          bool
	RunViaTini              bool
	ScopedDockerignore      bool
	UnpackXz                bool
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
		CacheImageIndex:         featureFlag("FF_KANIKO_CACHE_IMAGE_INDEX", true),
		CacheProbeAfterMiss:     featureFlag("FF_KANIKO_CACHE_PROBE_AFTER_MISS", false),
		ChownOnImplicitDirs:     featureFlag("FF_KANIKO_CHOWN_ON_IMPLICIT_DIRS", false),
		CopyAsRoot:              featureFlag("FF_KANIKO_COPY_AS_ROOT", false),
		CopyChmodOnImplicitDirs: featureFlag("FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS", false),
		CopyLink:                featureFlag("FF_KANIKO_COPY_LINK", true),
		DeferCachePush:          featureFlag("FF_KANIKO_DEFER_CACHE_PUSH", true),
		DisableHTTP2:            featureFlag("FF_KANIKO_DISABLE_HTTP2", false),
		OCIScratchBase:          featureFlag("FF_KANIKO_OCI_SCRATCH_BASE", false),
		RunViaTini:              featureFlag("FF_KANIKO_RUN_VIA_TINI", false),
		ScopedDockerignore:      featureFlag("FF_KANIKO_SCOPED_DOCKERIGNORE", false),
		UnpackXz:                featureFlag("FF_KANIKO_UNPACK_XZ", false),
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
