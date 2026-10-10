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

package commands

import (
	"fmt"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

type WorkdirCommand struct {
	BaseCommand
	cmd           *instructions.WorkdirCommand
	snapshotFiles []string
	shdCache      bool
}

func ToAbsPath(path string, workdir string) string {
	var result string
	if filepath.IsAbs(path) {
		result = path
	} else {
		if !filepath.IsAbs(workdir) {
			workdir = "/" + workdir
		}
		result = filepath.Join(workdir, path)
	}
	assert.Assert("workdir.abs-path", filepath.IsAbs(result), "ToAbsPath must return an absolute path, got %q (path=%q, workdir=%q)", result, path, workdir)
	return result
}

// For testing
var mkdirAllWithPermissions = util.MkdirAllWithPermissions

func (w *WorkdirCommand) ExecuteCommand(config *v1.Config, buildArgs *dockerfile.BuildArgs) error {
	logrus.Info("Cmd: workdir")
	workdirPath := w.cmd.Path
	replacementEnvs := buildArgs.ReplacementEnvs(config.Env)
	resolvedWorkingDir, err := util.ResolveEnvironmentReplacement(workdirPath, replacementEnvs, true)
	if err != nil {
		return err
	}
	config.WorkingDir = ToAbsPath(resolvedWorkingDir, config.WorkingDir)
	logrus.Infof("Changed working directory to %s", config.WorkingDir)

	// Only create and snapshot the dir if it didn't exist already
	w.snapshotFiles = []string{}
	if _, err := os.Stat(config.WorkingDir); os.IsNotExist(err) {
		uid, gid, err := util.GetActiveUserGroup(config.User, "", replacementEnvs)
		if err != nil {
			return fmt.Errorf("getting user group: %w", err)
		}

		logrus.Infof("Creating directory %s with uid %d and gid %d", config.WorkingDir, uid, gid)
		w.snapshotFiles = append(w.snapshotFiles, config.WorkingDir)
		if err := mkdirAllWithPermissions(config.WorkingDir, 0o755, uid, gid); err != nil {
			return fmt.Errorf("creating workdir %s: %w", config.WorkingDir, err)
		}
	}
	return nil
}

// FilesToSnapshot returns the workingdir, which should have been created if it didn't already exist
func (w *WorkdirCommand) FilesToSnapshot() []string {
	return w.snapshotFiles
}

// String returns some information about the command for the image config history
func (w *WorkdirCommand) String() string {
	return w.cmd.String()
}

func (w *WorkdirCommand) CacheKey(replacementEnvs []string) (string, error) {
	return util.ResolveVariables(w.cmd.String(), replacementEnvs)
}

// CacheCommand returns true since this command should be cached
func (w *WorkdirCommand) CacheCommand(img v1.Image) DockerCommand {
	return &CachingWorkdirCommand{
		caching: caching{img: img, allowEmpty: true},
		cmd:     w.cmd,
	}
}

func (w *WorkdirCommand) MetadataOnly() bool {
	return w.cmd.Path == "/"
}

func (w *WorkdirCommand) RequiresUnpackedFS() bool {
	return w.cmd.Path != "/"
}

func (w *WorkdirCommand) ShouldCacheOutput() bool {
	return w.shdCache
}

type CachingWorkdirCommand struct {
	BaseCommand
	caching
	cmd *instructions.WorkdirCommand
}

func (wr *CachingWorkdirCommand) ExecuteCommand(config *v1.Config, buildArgs *dockerfile.BuildArgs) error {
	logrus.Info("Cmd: workdir")
	replacementEnvs := buildArgs.ReplacementEnvs(config.Env)
	resolvedWorkingDir, err := util.ResolveEnvironmentReplacement(wr.cmd.Path, replacementEnvs, true)
	if err != nil {
		return err
	}
	config.WorkingDir = ToAbsPath(resolvedWorkingDir, config.WorkingDir)
	logrus.Infof("Changed working directory to %s", config.WorkingDir)
	return nil
}

// String returns some information about the command for the image config history
func (wr *CachingWorkdirCommand) String() string {
	if wr.cmd == nil {
		return "nil command"
	}
	return wr.cmd.String()
}

func (wr *CachingWorkdirCommand) CacheKey(replacementEnvs []string) (string, error) {
	return util.ResolveVariables(wr.String(), replacementEnvs)
}

func (wr *CachingWorkdirCommand) MetadataOnly() bool {
	return false
}
