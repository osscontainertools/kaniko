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

package dockerfile

import (
	"fmt"
	"strings"

	"github.com/GoogleContainerTools/kaniko/pkg/config"
	"github.com/containerd/platforms"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
)

type BuildArgs struct {
	buildArgs map[string]*string
	metaArgs  map[string]*string
}

func NewBuildArgs(args []string) *BuildArgs {
	res := BuildArgs{}
	for _, a := range args {
		s := strings.SplitN(a, "=", 2)
		if len(s) == 1 {
			res.buildArgs[s[0]] = nil
		} else {
			res.buildArgs[s[0]] = &s[1]
		}
	}
	return &res
}

func (b *BuildArgs) Clone() *BuildArgs {
	res := BuildArgs{}
	for k, v := range b.buildArgs {
		res.buildArgs[k] = v
	}
	for k, v := range b.metaArgs {
		res.metaArgs[k] = v
	}
	return &res
}

// ReplacementEnvs returns a list of filtered environment variables
func (b *BuildArgs) ReplacementEnvs(envs []string) []string {
	// Ensure that we operate on a new array and do not modify the underlying array
	resultEnv := make([]string, len(envs))
	copy(resultEnv, envs)
	// TODO
	filtered := []string{}
	//filtered := b.FilterAllowed(envs)
	// Disable makezero linter, since the previous make is paired with a same sized copy
	return append(resultEnv, filtered...) //nolint:makezero
}

// AddMetaArgs adds the supplied args map to b's allowedMetaArgs
func (b *BuildArgs) AddMetaArgs(metaArgs []instructions.ArgCommand) {
	for _, marg := range metaArgs {
		for _, arg := range marg.Args {
			b.metaArgs[arg.Key] = arg.Value
		}
	}
}

// AddMetaArg adds a new meta arg that can be used by FROM directives
func (b *BuildArgs) AddMetaArg(key string, value *string) {
	b.metaArgs[key] = value
}

func (b *BuildArgs) AddArg(key string, value *string) {
	b.buildArgs[key] = value
}

// GetAllAllowed returns a mapping with all the allowed args
func (b *BuildArgs) GetAllAllowed() map[string]string {
	return b.getAllFromMapping(b.buildArgs)
}

// GetAllMeta returns a mapping with all the meta args
func (b *BuildArgs) GetAllMeta() map[string]string {
	return b.getAllFromMapping(b.metaArgs)
}

func (b *BuildArgs) getAllFromMapping(source map[string]*string) map[string]string {
	m := make(map[string]string)
	for k, v := range source {
		m[k] = *v
	}
	return m
}

// Initialize predefined build args s.a.: TARGETOS, TARGETARCH, BUILDPLATFORM, TARGETPLATFORM ...
func PredefinedBuildArgs(opts *config.KanikoOptions, lastStage *config.KanikoStage) ([]string, error) {
	buildSpec := platforms.Normalize(platforms.DefaultSpec())
	build := platforms.Format(buildSpec)

	var target = build
	var targetSpec = buildSpec
	var err error
	if opts.CustomPlatform != "" {
		target = opts.CustomPlatform
		targetSpec, err = platforms.Parse(opts.CustomPlatform)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse target platform %q: %v", opts.CustomPlatform, err)
		}
	}

	var targetStage = "default"
	if lastStage.Stage.Name != "" {
		targetStage = lastStage.Stage.Name
	}

	return []string{
		"BUILDPLATFORM=" + build,
		"BUILDOS=" + buildSpec.OS,
		"BUILDOSVERSION=" + buildSpec.OSVersion,
		"BUILDARCH=" + buildSpec.Architecture,
		"BUILDVARIANT=" + buildSpec.Variant,
		"TARGETPLATFORM=" + target,
		"TARGETOS=" + targetSpec.OS,
		"TARGETOSVERSION=" + targetSpec.OSVersion,
		"TARGETARCH=" + targetSpec.Architecture,
		"TARGETVARIANT=" + targetSpec.Variant,
		"TARGETSTAGE=" + targetStage,
	}, nil
}
