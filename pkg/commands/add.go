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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/moby/buildkit/frontend/dockerfile/instructions"
	"github.com/opencontainers/go-digest"
	"github.com/osscontainertools/kaniko/pkg/assert"
	kConfig "github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/dockerfile"
	"github.com/osscontainertools/kaniko/pkg/util"
	"github.com/sirupsen/logrus"
)

type AddCommand struct {
	BaseCommand
	cmd           *instructions.AddCommand
	fileContext   util.FileContext
	snapshotFiles []string
	shdCache      bool
}

// ExecuteCommand executes the ADD command
// Special stuff about ADD:
//  1. If <src> is a remote file URL:
//     - destination will have permissions of 0600
//     - If remote file has HTTP Last-Modified header, we set the mtime of the file to that timestamp
//     - If dest doesn't end with a slash, the filepath is inferred to be <dest>/<filename>
//  2. If <src> is a local tar archive:
//     - it is unpacked at the dest, as 'tar -x' would
func (a *AddCommand) ExecuteCommand(config *v1.Config, buildArgs *dockerfile.BuildArgs) error {
	replacementEnvs := buildArgs.ReplacementEnvs(config.Env)

	chmod, _, err := util.GetChmod(a.cmd.Chmod, replacementEnvs)
	if err != nil {
		return fmt.Errorf("getting permissions from chmod: %w", err)
	}

	var chownOwner *util.Owner
	if kConfig.FF.AddChownTar && a.cmd.Chown != "" {
		chownOwner, err = util.GetUserGroup(a.cmd.Chown, replacementEnvs)
		if err != nil {
			return fmt.Errorf("getting user group from chown: %w", err)
		}
	}

	var owner *util.Owner
	if kConfig.FF.CopyLink && a.cmd.Link {
		chown := a.cmd.Chown
		if chown == "" {
			chown = config.User
			if kConfig.FF.CopyAsRoot {
				chown = "0:0"
			}
		}
		owner, err = util.GetNumericUserGroup(chown, replacementEnvs)
		if err != nil && a.cmd.Chown == "" {
			return fmt.Errorf("ADD --link needs a numeric USER or --chown, got USER %q: %w", config.User, err)
		}
		if err != nil {
			return fmt.Errorf("getting user group from chown: %w", err)
		}
		if owner == nil {
			owner = &util.Owner{}
		}
	} else {
		user := config.User
		if kConfig.FF.CopyAsRoot {
			// According to spec: https://docs.docker.com/reference/dockerfile/#add---chown
			//   Without this flag, files are created with UID and GID of 0.
			// But this is a breaking change so we keep it optional for now
			user = "0:0"
		}
		owner, err = util.GetActiveUserGroup(user, a.cmd.Chown, replacementEnvs)
		if err != nil {
			return fmt.Errorf("getting user group from chown: %w", err)
		}
	}

	srcs, dest, err := util.ResolveEnvAndWildcards(a.cmd.SourcesAndDest, a.fileContext, replacementEnvs)
	if err != nil {
		return err
	}

	var checksum digest.Digest
	if a.cmd.Checksum != "" && kConfig.FF.AddChecksum {
		if len(srcs) > 1 {
			return errors.New("checksum can't be specified for multiple sources")
		}
		resolved, err := util.ResolveEnvironmentReplacement(a.cmd.Checksum, replacementEnvs, false)
		if err != nil {
			return fmt.Errorf("resolving checksum: %w", err)
		}
		checksum, err = digest.Parse(resolved)
		if err != nil {
			return fmt.Errorf("invalid checksum digest format: %w", err)
		}
		for _, src := range srcs {
			if !util.IsSrcRemoteFileURL(src) {
				return fmt.Errorf("checksum requires HTTP(S) sources, got %s", src)
			}
		}
	}

	// a local archive is unpacked and a download is not, --unpack overrides both
	unpackLocal, unpackRemote := true, false
	if kConfig.FF.AddUnpack && a.cmd.Unpack != nil {
		unpackLocal, unpackRemote = *a.cmd.Unpack, *a.cmd.Unpack
	}

	// an archive is downloaded outside the rootfs, only its contents belong in the image
	staging := ""
	if unpackRemote {
		var err error
		staging, err = os.MkdirTemp(kConfig.KanikoDir, "add-unpack-")
		if err != nil {
			return fmt.Errorf("creating staging dir for remote sources: %w", err)
		}
		defer os.RemoveAll(staging)
	}

	var unresolvedSrcs []string
	// If any of the sources are local tar archives:
	// 	1. Unpack them to the specified destination
	// If any of the sources is a remote file URL:
	//	1. Download and copy it to the specified dest
	// Else, add to the list of unresolved sources
	for _, src := range srcs {
		fullPath := filepath.Join(a.fileContext.Root, src)
		if util.IsSrcRemoteFileURL(src) {
			urlDest, err := util.URLDestinationFilepath(src, dest, config.WorkingDir, replacementEnvs)
			if err != nil {
				return err
			}
			download := urlDest
			if unpackRemote {
				download = filepath.Join(staging, filepath.Base(urlDest))
			} else if kConfig.FF.CopyLink && a.cmd.Link {
				err = materializeLinkDest(urlDest)
				if err != nil {
					return fmt.Errorf("materializing link destination: %w", err)
				}
			}
			logrus.Infof("Adding remote URL %s to %s", src, urlDest)
			if err := util.DownloadFileToDest(src, download, owner, chmod.Apply(0o600), checksum); err != nil {
				return fmt.Errorf("downloading remote source file: %w", err)
			}

			switch {
			case !unpackRemote:
				a.snapshotFiles = append(a.snapshotFiles, urlDest)
			case util.IsFileTarArchive(download):
				tarDest, err := util.DestinationFilepath("", dest, config.WorkingDir)
				if err != nil {
					return fmt.Errorf("determining dest for tar: %w", err)
				}
				if kConfig.FF.CopyLink && a.cmd.Link {
					err = materializeLinkDest(tarDest)
					if err != nil {
						return fmt.Errorf("materializing link destination: %w", err)
					}
				}
				if chownOwner != nil {
					if err := util.MkdirAllWithPermissions(tarDest, 0o755, *chownOwner, false); err != nil {
						return fmt.Errorf("creating tar destination directory: %w", err)
					}
					a.snapshotFiles = append(a.snapshotFiles, tarDest)
				}
				logrus.Infof("Unpacking remote archive %s to %s", src, tarDest)
				extractedFiles, err := util.UnpackLocalTarArchive(download, tarDest, chownOwner)
				if err != nil {
					return fmt.Errorf("unpacking remote archive: %w", err)
				}
				logrus.Debugf("Added %v from remote archive %s", extractedFiles, src)
				a.snapshotFiles = append(a.snapshotFiles, extractedFiles...)
			default:
				// docker places a download that turns out not to be an archive, --unpack is not an assertion
				// an existing parent is left alone, MkdirAllWithPermissions would replace a symlink with a directory
				if kConfig.FF.CopyLink && a.cmd.Link {
					err = materializeLinkDest(urlDest)
					if err != nil {
						return fmt.Errorf("materializing link destination: %w", err)
					}
				}
				parent := filepath.Dir(urlDest)
				_, err := os.Lstat(parent)
				if os.IsNotExist(err) {
					dirOwner := util.Owner{}
					if owner != nil {
						dirOwner = *owner
					}
					if err := util.MkdirAllWithPermissions(parent, 0o755, dirOwner, false); err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
				if err := util.MoveDir(download, urlDest); err != nil {
					return fmt.Errorf("placing remote source file: %w", err)
				}
				a.snapshotFiles = append(a.snapshotFiles, urlDest)
			}
		} else if unpackLocal && util.IsFileLocalTarArchive(fullPath) {
			if kConfig.FF.ConfineCopySource {
				resolved, err := filepath.EvalSymlinks(fullPath)
				if err != nil {
					return err
				}
				err = util.CheckSource(resolved, a.fileContext)
				if err != nil {
					return err
				}
			}
			tarDest, err := util.DestinationFilepath("", dest, config.WorkingDir)
			if err != nil {
				return fmt.Errorf("determining dest for tar: %w", err)
			}
			if kConfig.FF.CopyLink && a.cmd.Link {
				err = materializeLinkDest(tarDest)
				if err != nil {
					return fmt.Errorf("materializing link destination: %w", err)
				}
			}
			if chownOwner != nil {
				if err := util.MkdirAllWithPermissions(tarDest, 0o755, *chownOwner, false); err != nil {
					return fmt.Errorf("creating tar destination directory: %w", err)
				}
				a.snapshotFiles = append(a.snapshotFiles, tarDest)
			}
			logrus.Infof("Unpacking local tar archive %s to %s", src, tarDest)
			extractedFiles, err := util.UnpackLocalTarArchive(fullPath, tarDest, chownOwner)
			if err != nil {
				return fmt.Errorf("unpacking local tar: %w", err)
			}
			logrus.Debugf("Added %v from local tar archive %s", extractedFiles, src)
			a.snapshotFiles = append(a.snapshotFiles, extractedFiles...)
		} else {
			unresolvedSrcs = append(unresolvedSrcs, src)
		}
	}
	// With the remaining "normal" sources, create and execute a standard copy command
	heredocs := a.cmd.SourceContents
	if len(unresolvedSrcs) == 0 && len(heredocs) == 0 {
		return nil
	}

	copyCmd := CopyCommand{
		cmd: &instructions.CopyCommand{
			SourcesAndDest: instructions.SourcesAndDest{SourcePaths: unresolvedSrcs, DestPath: dest, SourceContents: heredocs},
			Chown:          a.cmd.Chown,
			Chmod:          a.cmd.Chmod,
			Link:           a.cmd.Link,
		},
		fileContext: a.fileContext,
	}

	if err := copyCmd.ExecuteCommand(config, buildArgs); err != nil {
		return fmt.Errorf("executing copy command: %w", err)
	}
	a.snapshotFiles = append(a.snapshotFiles, copyCmd.snapshotFiles...)
	return nil
}

// FilesToSnapshot should return an empty array if still nil; no files were changed
func (a *AddCommand) FilesToSnapshot() []string {
	return a.snapshotFiles
}

// String returns some information about the command for the image config
func (a *AddCommand) String() string {
	return a.cmd.String()
}

func (a *AddCommand) CacheKey(replacementEnvs []string) (string, error) {
	return resolvedCacheKey(a.cmd.String(), a.cmd.SourceContents, replacementEnvs)
}

func (a *AddCommand) FilesUsedFromContext(config *v1.Config, buildArgs *dockerfile.BuildArgs) ([]string, error) {
	return addCmdFilesUsedFromContext(config, buildArgs, a.cmd, a.fileContext)
}

func (a *AddCommand) MetadataOnly() bool {
	return false
}

func (a *AddCommand) RequiresUnpackedFS() bool {
	return true
}

func (a *AddCommand) ShouldCacheOutput() bool {
	return a.shdCache
}

func (a *AddCommand) HasIndependentCacheKey() bool {
	if !kConfig.FF.CopyLink || !a.cmd.Link {
		return false
	}
	if !kConfig.FF.ResolveCacheKey {
		return !namesVariables(a.cmd.String(), a.cmd.SourceContents)
	}
	return true
}

// CacheCommand returns true since this command should be cached
func (a *AddCommand) CacheCommand(img v1.Image) DockerCommand {
	return &CachingAddCommand{
		caching:     caching{img: img},
		cmd:         a.cmd,
		fileContext: a.fileContext,
	}
}

type CachingAddCommand struct {
	BaseCommand
	caching
	cmd         *instructions.AddCommand
	fileContext util.FileContext
}

func (ca *CachingAddCommand) FilesUsedFromContext(config *v1.Config, buildArgs *dockerfile.BuildArgs) ([]string, error) {
	return addCmdFilesUsedFromContext(config, buildArgs, ca.cmd, ca.fileContext)
}

func (ca *CachingAddCommand) MetadataOnly() bool {
	return false
}

func (ca *CachingAddCommand) String() string {
	if ca.cmd == nil {
		return "nil command"
	}
	return ca.cmd.String()
}

func (ca *CachingAddCommand) CacheKey(replacementEnvs []string) (string, error) {
	return resolvedCacheKey(ca.cmd.String(), ca.cmd.SourceContents, replacementEnvs)
}

func (ca *CachingAddCommand) HasIndependentCacheKey() bool {
	if !kConfig.FF.CopyLink || !ca.cmd.Link {
		return false
	}
	if !kConfig.FF.ResolveCacheKey {
		return !namesVariables(ca.cmd.String(), ca.cmd.SourceContents)
	}
	return true
}

func addCmdFilesUsedFromContext(config *v1.Config, buildArgs *dockerfile.BuildArgs, cmd *instructions.AddCommand,
	fileContext util.FileContext,
) ([]string, error) {
	replacementEnvs := buildArgs.ReplacementEnvs(config.Env)

	srcs, _, err := util.ResolveEnvAndWildcards(cmd.SourcesAndDest, fileContext, replacementEnvs)
	if err != nil {
		return nil, err
	}

	files := []string{}
	for _, src := range srcs {
		if util.IsSrcRemoteFileURL(src) {
			continue
		}
		if util.IsFileLocalTarArchive(src) {
			continue
		}
		fullPath := filepath.Join(fileContext.Root, src)
		files = append(files, fullPath)
	}

	// Remote URLs and tar archives are filtered out, so the result cannot exceed the source count.
	assert.Assert("add.files-count", len(files) <= len(srcs), "addCmdFilesUsedFromContext: result exceeds source count (srcs=%d, files=%d)", len(srcs), len(files))
	logrus.Infof("Using files from context: %v", files)
	return files, nil
}
