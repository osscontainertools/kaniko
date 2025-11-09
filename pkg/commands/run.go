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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	kConfig "github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

type RunCommand struct {
	BaseCommand
	cmd      *instructions.RunCommand
	shdCache bool
}

// for testing
var (
	userLookup = util.LookupUser
)

func (r *RunCommand) IsArgsEnvsRequiredInCache() bool {
	return true
}

func (r *RunCommand) ExecuteCommand(config *v1.Config, buildArgs *dockerfile.BuildArgs) error {
	return runCommandWithFlags(config, buildArgs, r.cmd)
}

func runCommandWithFlags(config *v1.Config, buildArgs *dockerfile.BuildArgs, cmdRun *instructions.RunCommand) (reterr error) {
	ff_cache := kConfig.EnvBoolDefault("FF_KANIKO_RUN_MOUNT_CACHE", true)
	for _, f := range cmdRun.FlagsUsed {
		if !(ff_cache && f == "mount") {
			logrus.Warnf("#969 kaniko does not support '--%s' flags in RUN statements - relying on unsupported flags can lead to invalid builds", f)
		}
	}
	if ff_cache && len(cmdRun.FlagsUsed) > 0 {
		replacementEnvs := buildArgs.ReplacementEnvs(config.Env)
		expand := func(word string) (string, error) {
			return util.ResolveEnvironmentReplacement(word, replacementEnvs, false)
		}
		err := cmdRun.Expand(expand)
		if err != nil {
			return err
		}
		for _, m := range instructions.GetMounts(cmdRun) {
			switch m.Type {
			case instructions.MountTypeCache:
				normalized := filepath.Clean(m.Target)
				h := sha256.Sum256([]byte(normalized))
				targetHash := hex.EncodeToString(h[:])
				cacheDir := filepath.Join(kConfig.KanikoCacheDir, targetHash)
				err = os.MkdirAll(cacheDir, 0755)
				if err != nil {
					return err
				}
				created, err := ensureDir(m.Target)
				if err != nil {
					return err
				}
				if created != "" {
					defer func() {
						err := os.RemoveAll(created)
						if err != nil {
							reterr = err
						}
					}()
				}
				err = swapDir(cacheDir, m.Target)
				if err != nil {
					return err
				}
				defer func() {
					err := swapDir(m.Target, cacheDir)
					if err != nil {
						reterr = err
					}
				}()
			default:
				logrus.Warnf("Kaniko does not support '--mount=type=%s' flags in RUN statements - relying on unsupported flags can lead to invalid builds", m.Type)
			}

		}
	}
	return runCommandInExec(config, buildArgs, cmdRun)
}

func runCommandInExec(config *v1.Config, buildArgs *dockerfile.BuildArgs, cmdRun *instructions.RunCommand) error {
	var newCommand []string
	if cmdRun.PrependShell {
		// This is the default shell on Linux
		var shell []string
		if len(config.Shell) > 0 {
			shell = config.Shell
		} else {
			shell = append(shell, "/bin/sh", "-c")
		}

		cmd := strings.Join(cmdRun.CmdLine, " ")

		// Heredocs
		if len(cmdRun.Files) == 1 && cmd == fmt.Sprintf("<<%s", cmdRun.Files[0].Name) {
			// 1713: if we encounter a line like 'RUN <<EOF',
			// we implicitly want the file body to be executed as a script
			cmd += " sh"
		}
		for _, h := range cmdRun.Files {
			cmd += "\n" + h.Data + h.Name
		}

		newCommand = append(shell, cmd)
	} else {
		if len(cmdRun.Files) > 0 {
			// https://github.com/GoogleContainerTools/kaniko/issues/1713
			logrus.Warnf("#1713 kaniko does not support heredoc syntax in 'RUN [\"<command>\", ...]' (Exec Form) statements: %v", cmdRun.Files[0].Name)
		}
		newCommand = cmdRun.CmdLine
		// Find and set absolute path of executable by setting PATH temporary
		replacementEnvs := buildArgs.ReplacementEnvs(config.Env)
		for _, v := range replacementEnvs {
			entry := strings.SplitN(v, "=", 2)
			if entry[0] != "PATH" {
				continue
			}
			oldPath := os.Getenv("PATH")
			defer os.Setenv("PATH", oldPath)
			os.Setenv("PATH", entry[1])
			path, err := exec.LookPath(newCommand[0])
			if err == nil {
				newCommand[0] = path
			}
		}
	}

	logrus.Infof("Cmd: %s", newCommand[0])
	logrus.Infof("Args: %s", newCommand[1:])

	cmd := exec.Command(newCommand[0], newCommand[1:]...)

	cmd.Dir = setWorkDirIfExists(config.WorkingDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	replacementEnvs := buildArgs.ReplacementEnvs(config.Env)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	u := config.User
	userAndGroup := strings.Split(u, ":")
	userStr, err := util.ResolveEnvironmentReplacement(userAndGroup[0], replacementEnvs, false)
	if err != nil {
		return fmt.Errorf("resolving user %s: %w", userAndGroup[0], err)
	}

	// If specified, run the command as a specific user
	if userStr != "" {
		cmd.SysProcAttr.Credential, err = util.SyscallCredentials(userStr)
		if err != nil {
			return fmt.Errorf("credentials: %w", err)
		}
	}

	env, err := addDefaultHOME(userStr, replacementEnvs)
	if err != nil {
		return fmt.Errorf("adding default HOME variable: %w", err)
	}

	cmd.Env = env

	logrus.Infof("Running: %s", cmd.Args)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting command: %w", err)
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return fmt.Errorf("getting group id for process: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("waiting for process to exit: %w", err)
	}

	//it's not an error if there are no grandchildren
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && err.Error() != "no such process" {
		return err
	}
	return nil
}

// addDefaultHOME adds the default value for HOME if it isn't already set
func addDefaultHOME(u string, envs []string) ([]string, error) {
	for _, env := range envs {
		split := strings.SplitN(env, "=", 2)
		if split[0] == constants.HOME {
			return envs, nil
		}
	}

	// If user isn't set, set default value of HOME
	if u == "" || u == constants.RootUser {
		return append(envs, fmt.Sprintf("%s=%s", constants.HOME, constants.DefaultHOMEValue)), nil
	}

	// If user is set to username, set value of HOME to /home/${user}
	// Otherwise the user is set to uid and HOME is /
	userObj, err := userLookup(u)
	if err != nil {
		return nil, fmt.Errorf("lookup user %v: %w", u, err)
	}

	return append(envs, fmt.Sprintf("%s=%s", constants.HOME, userObj.HomeDir)), nil
}

// String returns some information about the command for the image config
func (r *RunCommand) String() string {
	return r.cmd.String()
}

func (r *RunCommand) FilesToSnapshot() []string {
	return nil
}

func (r *RunCommand) ProvidesFilesToSnapshot() bool {
	return false
}

// CacheCommand returns true since this command should be cached
func (r *RunCommand) CacheCommand(img v1.Image) DockerCommand {

	return &CachingRunCommand{
		img:       img,
		cmd:       r.cmd,
		extractFn: util.ExtractFile,
	}
}

func (r *RunCommand) MetadataOnly() bool {
	return false
}

func (r *RunCommand) RequiresUnpackedFS() bool {
	return true
}

func (r *RunCommand) ShouldCacheOutput() bool {
	return r.shdCache
}

type CachingRunCommand struct {
	BaseCommand
	caching
	img            v1.Image
	extractedFiles []string
	cmd            *instructions.RunCommand
	extractFn      util.ExtractFunction
}

func (cr *CachingRunCommand) IsArgsEnvsRequiredInCache() bool {
	return true
}

func (cr *CachingRunCommand) ExecuteCommand(config *v1.Config, buildArgs *dockerfile.BuildArgs) error {
	logrus.Infof("Found cached layer, extracting to filesystem")
	var err error

	if cr.img == nil {
		return errors.New(fmt.Sprintf("command image is nil %v", cr.String()))
	}

	layers, err := cr.img.Layers()
	if err != nil {
		return fmt.Errorf("retrieving image layers: %w", err)
	}

	if len(layers) != 1 {
		return errors.New(fmt.Sprintf("expected %d layers but got %d", 1, len(layers)))
	}

	cr.layer = layers[0]

	cr.extractedFiles, err = util.GetFSFromLayers(
		kConfig.RootDir,
		layers,
		util.ExtractFunc(cr.extractFn),
		util.IncludeWhiteout(),
	)
	if err != nil {
		return fmt.Errorf("extracting fs from image: %w", err)
	}

	return nil
}

func (cr *CachingRunCommand) FilesToSnapshot() []string {
	f := cr.extractedFiles
	logrus.Debugf("%d files extracted by caching run command", len(f))
	logrus.Tracef("Extracted files: %s", f)

	return f
}

func (cr *CachingRunCommand) String() string {
	if cr.cmd == nil {
		return "nil command"
	}
	return cr.cmd.String()
}

func (cr *CachingRunCommand) MetadataOnly() bool {
	return false
}

// todo: this should create the workdir if it doesn't exist, atleast this is what docker does
func setWorkDirIfExists(workdir string) string {
	if _, err := os.Lstat(workdir); err == nil {
		return workdir
	}
	return ""
}

func swapDir(pathA, pathB string) (err error) {
	if pathA == "" || pathB == "" {
		return fmt.Errorf("paths must not be empty")
	}
	tmp := kConfig.KanikoSwapDir

	err = os.Rename(pathA, tmp)
	if err != nil {
		return fmt.Errorf("failed to rename (1) %s -> %s: %w", pathA, tmp, err)
	}

	err = os.Rename(pathB, pathA)
	if err != nil {
		return fmt.Errorf("failed to rename (2) %s -> %s: %w", pathB, pathA, err)
	}

	err = os.Rename(tmp, pathB)
	if err != nil {
		return fmt.Errorf("failed to rename (3) %s -> %s: %w", tmp, pathB, err)
	}

	return nil
}

func ensureDir(target string) (string, error) {
	var firstCreated = ""
	curr := target
	for {
		_, err := os.Stat(curr)
		if !os.IsNotExist(err) {
			break
		}
		firstCreated = curr
		curr = filepath.Dir(curr)
	}

	if firstCreated == "" {
		return "", nil
	}

	err := os.MkdirAll(target, 0755)
	if err != nil {
		return "", err
	}

	return firstCreated, nil
}
