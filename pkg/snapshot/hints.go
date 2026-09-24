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
	"maps"
	"os"
	"slices"
	"strings"

	units "github.com/docker/go-units"
	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/hint"
)

const (
	cacheAdvice = "use RUN --mount=type=cache or remove it in the same RUN"
	gitAdvice   = "exclude it in .dockerignore for COPY, use ADD <git url>, or git clone --depth 1 and remove it in the same RUN"
	vcsAdvice   = "exclude it in .dockerignore for COPY or remove it in the same RUN"
)

// anchored rules match from the filesystem root, the others under any directory.
type pathRule struct {
	rule     hint.Rule
	dir      string
	anchored bool
	advice   string
}

var pathRules = []pathRule{
	{hint.SnapshotCacheDir, "/.cache/pip/", false, cacheAdvice},
	{hint.SnapshotCacheDir, "/.cache/go-build/", false, cacheAdvice},
	{hint.SnapshotCacheDir, "/.cache/yarn/", false, cacheAdvice},
	{hint.SnapshotCacheDir, "/.npm/", false, cacheAdvice},
	{hint.SnapshotCacheDir, "/.m2/repository/", false, cacheAdvice},
	{hint.SnapshotCacheDir, "/.gradle/caches/", false, cacheAdvice},
	{hint.SnapshotCacheDir, "/.cargo/registry/", false, cacheAdvice},
	{hint.SnapshotCacheDir, "/usr/local/cargo/registry/", true, cacheAdvice},
	{hint.SnapshotCacheDir, "/var/cache/apt/archives/", true, cacheAdvice},
	{hint.SnapshotCacheDir, "/var/lib/apt/lists/", true, cacheAdvice},
	{hint.SnapshotCacheDir, "/var/cache/apk/", true, cacheAdvice},
	{hint.SnapshotVCSDir, "/.git/", false, gitAdvice},
	{hint.SnapshotVCSDir, "/.hg/", false, vcsAdvice},
	{hint.SnapshotVCSDir, "/.svn/", false, vcsAdvice},
}

type dirUsage struct {
	rule  *pathRule
	bytes int64
	files int
}

func reportHints(files []string) {
	if !config.FF.LayerHints {
		return
	}
	usage := map[string]*dirUsage{}
	for _, file := range files {
		for i := range pathRules {
			r := &pathRules[i]
			dir := matchDir(file, r)
			if dir != "" {
				fi, err := os.Lstat(file)
				if err == nil && fi.Mode().IsRegular() {
					u := usage[dir]
					if u == nil {
						u = &dirUsage{rule: r}
						usage[dir] = u
					}
					u.bytes += fi.Size()
					u.files++
				}
			}
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(usage)) {
		u := usage[dir]
		if u.bytes > 0 {
			hint.Report(u.rule.rule, "%s in %d files under %s, %s", units.HumanSize(float64(u.bytes)), u.files, dir, u.rule.advice)
		}
	}
}

func matchDir(file string, r *pathRule) string {
	if r.anchored {
		if strings.HasPrefix(file, r.dir) {
			return strings.TrimSuffix(r.dir, "/")
		}
		return ""
	}
	i := strings.Index(file, r.dir)
	if i < 0 {
		return ""
	}
	return file[:i+len(r.dir)-1]
}
