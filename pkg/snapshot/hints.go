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

package snapshot

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/osscontainertools/kaniko/pkg/hint"
)

var cacheDirs = []string{
	".cache/pip",
	".cache/go-build",
	".cache/yarn",
	".npm",
	".m2/repository",
	".gradle/caches",
	".cargo/registry",
	"usr/local/cargo/registry",
	"var/cache/apt/archives",
	"var/lib/apt/lists",
	"var/cache/apk",
}

var vcsDirs = []string{".git", ".hg", ".svn"}

type dirUsage struct {
	bytes int64
	files int
}

func reportHints(files []string) {
	caches := map[string]*dirUsage{}
	vcs := map[string]*dirUsage{}
	for _, file := range files {
		addUsage(caches, file, cacheDirs)
		addUsage(vcs, file, vcsDirs)
	}
	for _, dir := range slices.Sorted(maps.Keys(caches)) {
		u := caches[dir]
		if u.bytes > 0 {
			hint.Report("SnapshotCacheDir", "%s in %d files under %s, use RUN --mount=type=cache or remove it in the same RUN", formatBytes(u.bytes), u.files, dir)
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(vcs)) {
		u := vcs[dir]
		hint.Report("SnapshotVCSDir", "%s in %d files under %s, add it to .dockerignore", formatBytes(u.bytes), u.files, dir)
	}
}

func formatBytes(b int64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(b)/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(b)/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.1f kB", float64(b)/1e3)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func addUsage(usage map[string]*dirUsage, file string, dirs []string) {
	for _, dir := range dirs {
		i := strings.Index(file+"/", "/"+dir+"/")
		if i >= 0 {
			fi, err := os.Lstat(file)
			if err == nil && fi.Mode().IsRegular() {
				prefix := file[:i+1+len(dir)]
				u := usage[prefix]
				if u == nil {
					u = &dirUsage{}
					usage[prefix] = u
				}
				u.bytes += fi.Size()
				u.files++
			}
			return
		}
	}
}
