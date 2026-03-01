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
	"strings"

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
var DockerfilePath = KanikoDir + "/Dockerfile"

// BuildContextDir is the directory a build context will be unpacked into,
// for example, a tarball from a GCS bucket will be unpacked here
var BuildContextDir = KanikoDir + "/buildcontext/"

// KanikoIntermediateStagesDir is where we will store intermediate stages
// as tarballs in case they are needed later on
var KanikoIntermediateStagesDir = KanikoDir + "/stages/"

// KanikoInsterStageDir is where we will store inter-stage dependencies
// Contents are stored as-is.
var KanikoInterStageDepsDir = func() string {
	if EnvBoolDefault("FF_KANIKO_NEW_CACHE_LAYOUT", true) {
		return KanikoDir + "/deps/"
	}
	return KanikoDir
}()

// KanikoLayersDir is where we will store layers as tarballs
var KanikoLayersDir = func() string {
	if EnvBoolDefault("FF_KANIKO_NEW_CACHE_LAYOUT", true) {
		return KanikoDir + "/layers/"
	}
	return KanikoDir
}()

// KanikoCacheDir is where we will store cache mount directories, ie.
// RUN --mount=type=cache,target=/var/lib/apt/lists/
// Contents are stored as-is.
var KanikoCacheDir = KanikoDir + "/caches/"

// KanikoSwapDir is a temporary directory used to swap out cache
// and target directories
var KanikoSwapDir = KanikoDir + "/swap/"

// DockerConfigDir is a where registry credentials are stored
var DockerConfigDir = KanikoDir + "/.docker/"

// KanikoSecretsDir is a where user defined secrets are stored
var KanikoSecretsDir = KanikoDir + "/secrets/"

var TiniExec = KanikoDir + "/tini"

var MountInfoPath string

func init() {
	RootDir = constants.RootDir
	MountInfoPath = constants.MountInfoPath
}

// Same as os.RemoveAll, but asserts that we don't delete / or /kaniko.
// This should be impossible at runtime and would indicate a programming mistake.
func safeRemove(target string) error {
	targetInfo, err := os.Stat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to stat %q: %w", target, err)
	}
	rootInfo, err := os.Stat(RootDir)
	if err != nil {
		return fmt.Errorf("failed to stat %q: %w", RootDir, err)
	}
	kanikoInfo, err := os.Stat(KanikoDir)
	if err != nil {
		return fmt.Errorf("failed to stat %q: %w", KanikoDir, err)
	}
	if os.SameFile(targetInfo, rootInfo) {
		logrus.Fatalf("refusing to remove /")
	}
	if os.SameFile(targetInfo, kanikoInfo) {
		logrus.Fatalf("refusing to remove %q", KanikoDir)
	}
	if !strings.HasPrefix(target, KanikoDir+"/") {
		logrus.Fatalf("refusing to remove %q outside %q", target, KanikoDir)
	}
	return os.RemoveAll(target)
}

func Cleanup() error {
	err := safeRemove(DockerfilePath)
	if err != nil {
		return err
	}
	err = safeRemove(KanikoIntermediateStagesDir)
	if err != nil {
		return err
	}
	err = safeRemove(BuildContextDir)
	if err != nil {
		return err
	}
	if EnvBoolDefault("FF_KANIKO_NEW_CACHE_LAYOUT", true) {
		err = safeRemove(KanikoInterStageDepsDir)
		if err != nil {
			return err
		}
		err = safeRemove(KanikoLayersDir)
		if err != nil {
			return err
		}
	}
	err = safeRemove(KanikoSecretsDir)
	if err != nil {
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
