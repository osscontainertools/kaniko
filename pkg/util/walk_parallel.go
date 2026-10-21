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
	"io/fs"
	"math"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/osscontainertools/kaniko/pkg/metrics"
	"github.com/sirupsen/logrus"
)

// maxHashWorkers caps the pool of gowalkDirParallel: beyond it, more readers
// mostly contend for the disk and the locks changeFunc takes per file.
const maxHashWorkers = 8

// hashWorkers is the size of the pool of gowalkDirParallel. GOMAXPROCS follows
// the CPU quota of the container's cgroup.
func hashWorkers() int {
	return min(max(runtime.GOMAXPROCS(0), 1), maxHashWorkers)
}

// gowalkDirParallel is gowalkDir with changeFunc run on hashWorkers goroutines.
// The walk itself stays sequential and does what gowalkDir does before calling
// changeFunc (ignore list, deletedFiles, visited), in the order of fs.WalkDir,
// recording each path at its index. The pool then calls changeFunc on every
// recorded path; foundPaths keep the walk order.
//
// Errors match gowalkDir, which stops at the first one in walk order: a failing
// changeFunc at the lowest index wins. A walk error comes after every path
// recorded before it, since gowalkDir would have called changeFunc on all of
// them first, so it only counts when none of those failed.
func gowalkDirParallel(dir string, existingPaths map[string]struct{}, changeFunc func(string) (bool, error)) ([]string, map[string]struct{}, error) {
	deletedFiles := existingPaths // Make a reference.
	start := metrics.Now()
	var visited int64
	paths := make([]string, 0)

	callback := func(path string, info fs.DirEntry, err error) error {
		logrus.Tracef("Analyzing path '%s'", path)
		if err != nil {
			return err
		}

		if IsInIgnoreList(path) {
			if IsDestDir(path) && info.IsDir() {
				logrus.Tracef("Skipping paths under '%s', as it is an ignored directory", path)
				return filepath.SkipDir
			}
			return nil
		}
		visited++

		// File is existing on disk, remove it from deleted files.
		delete(deletedFiles, path)

		paths = append(paths, path)
		return nil
	}

	walkErr := fs.WalkDir(FSys, dir, callback)
	changed, err := runChangeFunc(paths, changeFunc, hashWorkers())
	if err != nil {
		return nil, nil, err
	}
	if walkErr != nil {
		return nil, nil, walkErr
	}

	foundPaths := make([]string, 0)
	for i, path := range paths {
		if changed[i] {
			foundPaths = append(foundPaths, path)
		}
	}
	metrics.SnapshotWalk.Observe(start, 0, visited)

	return foundPaths, deletedFiles, nil
}

// runChangeFunc calls changeFunc on every path with workers goroutines, which
// take the indices in increasing order. It returns, per index, whether the path
// changed, or the error of the lowest failing index. Once an index fails, no
// higher index is started: every lower one has been handed out already and
// still runs, so the lowest failing index is found whatever the timing.
func runChangeFunc(paths []string, changeFunc func(string) (bool, error), workers int) ([]bool, error) {
	changed := make([]bool, len(paths))
	var (
		next     atomic.Int64
		firstIdx atomic.Int64 // lowest failed index so far
		mu       sync.Mutex   // orders updates of firstIdx and firstErr
		firstErr error
		wg       sync.WaitGroup
	)
	firstIdx.Store(math.MaxInt64)
	for range min(workers, len(paths)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := next.Add(1) - 1
				if i >= int64(len(paths)) || i > firstIdx.Load() {
					return
				}
				isChanged, err := changeFunc(paths[i])
				if err != nil {
					mu.Lock()
					if i < firstIdx.Load() {
						firstIdx.Store(i)
						firstErr = err
					}
					mu.Unlock()
					return
				}
				changed[i] = isChanged
			}
		}()
	}
	wg.Wait()
	return changed, firstErr
}
