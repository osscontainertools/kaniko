/*
Copyright 2020 Google LLC

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
	"fmt"
	"os"

	"github.com/osscontainertools/kaniko/pkg/constants"
)

var RootDir string

// KanikoDir is the path to the Kaniko directory
var KanikoDir = func() string {
	if kd, ok := os.LookupEnv("KANIKO_DIR"); ok {
		return kd
	}
	return constants.DefaultKanikoPath
}()

// DockerfilePath is the path the Dockerfile is copied to
var DockerfilePath = fmt.Sprintf("%s/Dockerfile", KanikoDir)

// BuildContextDir is the directory a build context will be unpacked into,
// for example, a tarball from a GCS bucket will be unpacked here
var BuildContextDir = fmt.Sprintf("%s/buildcontext/", KanikoDir)

// KanikoIntermediateStagesDir is where we will store intermediate stages
// as tarballs in case they are needed later on
var KanikoIntermediateStagesDir = fmt.Sprintf("%s/stages/", KanikoDir)

// KanikoInsterStageDir is where we will store inter-stage dependencies
// Contents are stored as-is.
var KanikoInterStageDepsDir = func() string {
	if EnvBoolDefault("FF_KANIKO_NEW_CACHE_LAYOUT", true) {
		return fmt.Sprintf("%s/deps/", KanikoDir)
	}
	return KanikoDir
}()

// KanikoLayersDir is where we will store layers as tarballs
var KanikoLayersDir = func() string {
	if EnvBoolDefault("FF_KANIKO_NEW_CACHE_LAYOUT", true) {
		return fmt.Sprintf("%s/layers/", KanikoDir)
	}
	return KanikoDir
}()

// KanikoCacheDir is where we will store cache mount directories, ie.
// RUN --mount=type=cache,target=/var/lib/apt/lists/
// Contents are stored as-is.
var KanikoCacheDir = fmt.Sprintf("%s/caches/", KanikoDir)

// KanikoSwapDir is a temporary directory used to swap out cache
// and target directories
var KanikoSwapDir = fmt.Sprintf("%s/swap/", KanikoDir)

// DockerConfigDir is a where registry credentials are stored
var DockerConfigDir = fmt.Sprintf("%s/.docker/", KanikoDir)

var MountInfoPath string

func init() {
	RootDir = constants.RootDir
	MountInfoPath = constants.MountInfoPath
}
