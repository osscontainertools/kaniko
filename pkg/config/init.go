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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/sirupsen/logrus"
)

var RootDir string

// KanikoDir is the path to the Kaniko directory
var KanikoExeDir = func() string {
	exePath, err := os.Executable()
	if err != nil {
		logrus.Fatalf("couldn't determine location of kaniko exe")
	}
	return filepath.Dir(exePath)
}()

var KanikoDir = func() string {
	if kd, ok := os.LookupEnv("KANIKO_DIR"); ok {
		return kd
	}
	return KanikoExeDir
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

// KanikoSecretsDir is a where user defined secrets are stored
var KanikoSecretsDir = fmt.Sprintf("%s/secrets/", KanikoDir)

var TiniExec = fmt.Sprintf("%s/tini", KanikoDir)

var MountInfoPath string

func init() {
	RootDir = constants.RootDir
	MountInfoPath = constants.MountInfoPath
}

func Cleanup() error {
	err := os.Remove(DockerfilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	err = os.RemoveAll(KanikoIntermediateStagesDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	err = os.RemoveAll(BuildContextDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if EnvBoolDefault("FF_KANIKO_NEW_CACHE_LAYOUT", true) {
		err = os.RemoveAll(KanikoInterStageDepsDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		err = os.RemoveAll(KanikoLayersDir)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	err = os.RemoveAll(KanikoSecretsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, err = os.Stat(KanikoSwapDir)
	if err == nil {
		return fmt.Errorf("expected directory %q to not exist, but it does", KanikoSwapDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to stat %q: %w", KanikoSwapDir, err)
	}
	return nil
}
