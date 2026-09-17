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

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/sirupsen/logrus"
)

// A directory and a symlink to it are two names for one directory, and which of the two
// the base image gave the content to carries no information. When that name holds an
// ignored path, and so has to stay a directory, the content goes there instead and the
// symlink moves to the free name. dirAliases maps the moved symlink back, so snapshots
// still record what the base image declared.
var dirAliases = map[string]string{}

func isDirAlias(path string) bool {
	for _, dir := range dirAliases {
		if path == dir {
			return true
		}
	}
	return false
}

// both names can hold a directory of the same name, so the move descends until it reaches
// a name only one of them has
func mergeInto(root, src, dest string) error {
	// a mount shadows whatever the base image puts under it, and cannot be renamed over
	if CheckCleanedPathAgainstIgnoreList(dest) {
		return os.RemoveAll(src)
	}
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	destInfo, err := os.Lstat(dest)
	if srcInfo.Mode()&os.ModeSymlink != 0 && err == nil && destInfo.IsDir() {
		resolved, err := filepath.EvalSymlinks(src)
		if err != nil || resolved != dest {
			// dest has to stay a directory, so the name the link names takes the link instead
			linkname, err := os.Readlink(src)
			if err != nil {
				return err
			}
			if err := preserveMountedSymlink(root, dest, linkname); err != nil {
				return err
			}
		}
		// both names now reach one directory, so the link is redundant rather than moved
		return os.Remove(src)
	}
	if err != nil || !destInfo.IsDir() || !srcInfo.IsDir() {
		return MoveDir(src, dest)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		err := mergeInto(root, filepath.Join(src, entry.Name()), filepath.Join(dest, entry.Name()))
		if err != nil {
			return err
		}
	}
	return os.Remove(src)
}

func preserveMountedSymlink(dest, path, linkname string) error {
	parent := filepath.Dir(path)
	if filepath.IsAbs(linkname) {
		parent = dest
	}
	target := filepath.Join(parent, linkname)
	if target == path {
		return nil
	}
	// a link text of enough ".." would otherwise put the content outside the image
	outside, err := filepath.Rel(dest, target)
	if err != nil || outside == ".." || strings.HasPrefix(outside, "../") {
		return fmt.Errorf("cannot restore symlink %s -> %s: target leaves %s", path, target, dest)
	}
	if childDirInIgnoreList(target) {
		return fmt.Errorf("cannot restore symlink %s -> %s: both paths contain an ignored path", path, target)
	}
	if FilepathExists(target) {
		if err := mergeInto(dest, target, path); err != nil {
			return err
		}
	}
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}
	rel, err := filepath.Rel(targetDir, path)
	if err != nil {
		return err
	}
	logrus.Debugf("Keeping %s a directory and pointing %s at it, %s contains an ignored path", path, target, path)
	if err := os.Symlink(rel, target); err != nil {
		return err
	}
	dirAliases[target] = path
	return nil
}

// a RUN is free to delete or replace the symlink, which leaves the pair no longer aliased
func pruneDirAliases() {
	// preserveMountedSymlink is the only writer and runs behind the flag, the readers are ungated
	if !config.FF.PreserveMountedSymlinks {
		assert.Assert("util.diraliases.gated", len(dirAliases) == 0,
			"dir aliases recorded with FF_KANIKO_PRESERVE_MOUNTED_SYMLINKS off")
	}
	for alias, dir := range dirAliases {
		resolved, err := filepath.EvalSymlinks(alias)
		if err != nil || resolved != dir {
			delete(dirAliases, alias)
		}
	}
}

func logicalPath(path string) string {
	// aliases nest, and the innermost one is the name the base image gave this path
	longest, target := "", ""
	for alias, dir := range dirAliases {
		if HasFilepathPrefix(path, dir, false) && len(dir) > len(longest) {
			longest, target = dir, alias
		}
	}
	if longest == "" {
		return path
	}
	return filepath.Join(target, strings.TrimPrefix(path, longest))
}

func physicalPath(path string) string {
	longest, target := "", ""
	for alias, dir := range dirAliases {
		if HasFilepathPrefix(path, alias, false) && len(alias) > len(longest) {
			longest, target = alias, dir
		}
	}
	if longest == "" {
		return path
	}
	return filepath.Join(target, strings.TrimPrefix(path, longest))
}

// A swap gives a path a physical parent chain that differs from its name's, in both directions,
// so the parents worth naming are the ones the name has, each mapped back to where it lives.
func LogicalParents(path string) []string {
	if len(dirAliases) == 0 {
		return ParentDirectories(path)
	}
	parents := ParentDirectories(logicalPath(path))
	physical := make([]string, 0, len(parents))
	for _, parent := range parents {
		physical = append(physical, physicalPath(parent))
	}
	return physical
}
