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

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/sirupsen/logrus"
)

// A mount can be neither unlinked nor renamed, but a directory holding one can be renamed and the
// mount travels with it. That frees the name for the base image symlink, and since the two are one
// directory the mount stays reachable at the path the runtime gave it.
func relocateMountedPaths(dest, path, linkname string) (bool, error) {
	name := linkname
	if !filepath.IsAbs(linkname) {
		rel, err := filepath.Rel(dest, filepath.Dir(path))
		if err != nil {
			return false, err
		}
		name = filepath.Join(rel, linkname)
	}
	target, err := securejoin.SecureJoin(dest, name)
	if err != nil {
		return false, fmt.Errorf("cannot restore symlink %s -> %s: %w", path, linkname, err)
	}
	if target == path {
		return false, nil
	}
	logrus.Debugf("Moving the mounts under %s to %s, %s is the name the base image ships as a symlink", path, target, path)
	if err := moveMounts(path, target); err != nil {
		return false, err
	}
	if err := InitIgnoreList(); err != nil {
		return false, fmt.Errorf("refreshing the ignore list after moving the mounts under %s: %w", path, err)
	}
	return true, nil
}

// everything the ignore list does not pin is already gone from src, so what is left to move is the
// mounts and the directories holding them
func moveMounts(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	dstInfo, err := os.Stat(dst)
	if err == nil && srcInfo.IsDir() && dstInfo.IsDir() {
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			err := moveMounts(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name()))
			if err != nil {
				return err
			}
		}
		return os.Remove(src)
	}
	if err == nil {
		return fmt.Errorf("cannot move the mounted path %s to %s: the base image ships a file there", src, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("cannot move the mounted path %s to %s: %w", src, dst, err)
	}
	return nil
}
