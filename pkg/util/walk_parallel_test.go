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
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"syscall"
	"testing"
	"time"
)

type walkFunc func(string, map[string]struct{}, func(string) (bool, error)) ([]string, map[string]struct{}, error)

type walkResult struct {
	found   []string
	deleted map[string]struct{}
	err     error
}

func runWalk(walk walkFunc, dir string, existing map[string]struct{}, changeFunc func(string) (bool, error)) walkResult {
	found, deleted, err := walk(dir, maps.Clone(existing), changeFunc)
	return walkResult{found, deleted, err}
}

// makeWalkTree writes 6x6 directories of 50 files and a symlink each, and an
// empty directory per top-level one.
func makeWalkTree(t *testing.T, root string) {
	t.Helper()
	for d := range 6 {
		for s := range 6 {
			dir := filepath.Join(root, fmt.Sprintf("d%d", d), fmt.Sprintf("s%d", s))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for f := range 50 {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d", f)), fmt.Appendf(nil, "%d/%d/%d", d, s, f), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("f00", filepath.Join(dir, "link")); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Join(root, fmt.Sprintf("d%d", d), "empty"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// walkOrder lists the paths in the order the walk visits them.
func walkOrder(t *testing.T, root string) []string {
	t.Helper()
	var order []string
	if _, _, err := gowalkDir(root, map[string]struct{}{}, func(p string) (bool, error) {
		order = append(order, p)
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	return order
}

// existingFor is a layered map's view: most walked paths, plus some gone.
func existingFor(order []string, root string) map[string]struct{} {
	existing := map[string]struct{}{}
	for i, p := range order {
		if i%7 != 3 {
			existing[p] = struct{}{}
		}
	}
	for i := range 20 {
		existing[filepath.Join(root, "gone", fmt.Sprintf("x%d", i))] = struct{}{}
	}
	return existing
}

// jitter shuffles the order in which the workers finish.
func jitter() {
	if rand.IntN(32) == 0 {
		time.Sleep(time.Duration(rand.IntN(200)) * time.Microsecond)
		return
	}
	runtime.Gosched()
}

func sameResult(t *testing.T, label string, seq, par walkResult) {
	t.Helper()
	switch {
	case seq.err != nil || par.err != nil:
		if seq.err == nil || par.err == nil || (seq.err != par.err && seq.err.Error() != par.err.Error()) {
			t.Fatalf("%s: errors differ: sequential %v, parallel %v", label, seq.err, par.err)
		}
		if par.found != nil || par.deleted != nil {
			t.Fatalf("%s: results returned with the error", label)
		}
	case !slices.Equal(seq.found, par.found):
		t.Fatalf("%s: found paths differ (%d vs %d)", label, len(seq.found), len(par.found))
	case !maps.Equal(seq.deleted, par.deleted):
		t.Fatalf("%s: deleted files differ (%d vs %d)", label, len(seq.deleted), len(par.deleted))
	}
}

func TestWalkParallelMatchesSequential(t *testing.T) {
	root := t.TempDir()
	makeWalkTree(t, root)
	order := walkOrder(t, root)
	existing := existingFor(order, root)

	byName := func(p string) (bool, error) {
		jitter()
		h := fnv.New32a()
		h.Write([]byte(p))
		return h.Sum32()%3 == 0, nil
	}
	for i := range 10 {
		seq := runWalk(gowalkDir, root, existing, byName)
		par := runWalk(gowalkDirParallel, root, existing, byName)
		if seq.err != nil || len(seq.found) == 0 || len(seq.deleted) != 20 {
			t.Fatalf("sequential walk: %d found, %d deleted, %v", len(seq.found), len(seq.deleted), seq.err)
		}
		sameResult(t, fmt.Sprintf("round %d", i), seq, par)
	}

	// through the exported entry points, with the timeout wrapper
	exp := func(walk func(string, map[string]struct{}, func(string) (bool, error)) ([]string, map[string]struct{}, error)) walkResult {
		return runWalk(walk, root, existing, byName)
	}
	sameResult(t, "WalkFS", exp(WalkFS), exp(WalkFSParallel))

	// changed by content, as a layered map sees it, with both hashers
	for _, statCache := range []bool{false, true} {
		hasher := hasherWith(statCache)
		old := map[string]string{}
		for _, p := range order {
			old[p], _ = hasher(p)
		}
		for i, p := range order {
			if fi, _ := os.Lstat(p); i%11 == 0 && fi.Mode().IsRegular() {
				mustWrite(t, p, fmt.Sprintf("changed %d", i))
			}
		}
		byHash := func(p string) (bool, error) {
			h, err := hasher(p)
			if err != nil {
				return false, err
			}
			return h != old[p], nil
		}
		seq := runWalk(gowalkDir, root, existing, byHash)
		par := runWalk(gowalkDirParallel, root, existing, byHash)
		if len(seq.found) == 0 {
			t.Fatal("no change found")
		}
		sameResult(t, fmt.Sprintf("hash, stat cache %v", statCache), seq, par)
	}
}

func TestWalkParallelChangeFuncErrors(t *testing.T) {
	root := t.TempDir()
	makeWalkTree(t, root)
	order := walkOrder(t, root)
	existing := existingFor(order, root)

	for _, failing := range [][]int{{0}, {100}, {1500, 900, 100}, {len(order) - 1, 5}, {7, 8, 9}} {
		errs := map[string]error{}
		lowest := slices.Min(failing)
		for _, i := range failing {
			errs[order[i]] = fmt.Errorf("fail %d", i)
		}
		changeFunc := func(p string) (bool, error) {
			if err, ok := errs[p]; ok {
				if p == order[lowest] {
					// let the later failures happen first
					time.Sleep(3 * time.Millisecond)
				}
				return false, err
			}
			jitter()
			return true, nil
		}
		for round := range 30 {
			seq := runWalk(gowalkDir, root, existing, changeFunc)
			par := runWalk(gowalkDirParallel, root, existing, changeFunc)
			if seq.err != errs[order[lowest]] {
				t.Fatalf("sequential: %v, want %v", seq.err, errs[order[lowest]])
			}
			sameResult(t, fmt.Sprintf("failing %v round %d", failing, round), seq, par)
		}
	}
}

func TestRunChangeFunc(t *testing.T) {
	paths := make([]string, 500)
	for i := range paths {
		paths[i] = fmt.Sprintf("p%03d", i)
	}
	errs := map[string]error{"p042": errors.New("42"), "p317": errors.New("317"), "p499": errors.New("499")}
	for _, workers := range []int{1, 2, 3, 8, 64, 1000} {
		changed, err := runChangeFunc(paths, func(p string) (bool, error) {
			jitter()
			return p[3]%2 == 0, nil
		}, workers)
		if err != nil {
			t.Fatal(err)
		}
		for i, p := range paths {
			if changed[i] != (p[3]%2 == 0) {
				t.Fatalf("workers %d: %s changed %v", workers, p, changed[i])
			}
		}
		for round := range 20 {
			_, err := runChangeFunc(paths, func(p string) (bool, error) {
				if p == "p042" {
					time.Sleep(2 * time.Millisecond)
				}
				if err, ok := errs[p]; ok {
					return false, err
				}
				return true, nil
			}, workers)
			if err != errs["p042"] {
				t.Fatalf("workers %d round %d: %v, want 42", workers, round, err)
			}
		}
	}
	if changed, err := runChangeFunc(nil, nil, 8); err != nil || len(changed) != 0 {
		t.Fatalf("no paths: %v %v", changed, err)
	}
}

// failOpenFS fails Open of one path, like an unreadable directory or file.
type failOpenFS struct {
	fs.FS
	bad string
}

func (f failOpenFS) Open(name string) (fs.File, error) {
	if name == f.bad {
		return nil, &fs.PathError{Op: "open", Path: name, Err: syscall.EACCES}
	}
	return f.FS.Open(name)
}

func TestWalkParallelReadErrors(t *testing.T) {
	root := t.TempDir()
	makeWalkTree(t, root)
	order := walkOrder(t, root)
	existing := existingFor(order, root)
	orig := FSys
	defer func() { FSys = orig }()

	badDir := filepath.Join(root, "d3", "s2")
	FSys = failOpenFS{FS: orig, bad: badDir}
	errs := map[string]error{}
	for _, p := range []string{filepath.Join(root, "d1", "s0", "f00"), badDir, filepath.Join(root, "d5", "s0", "f00")} {
		errs[p] = fmt.Errorf("fail %s", p)
	}
	for _, tc := range []struct {
		name    string
		failing string
		want    func(error) bool
	}{
		{"walk error only", "", func(err error) bool { return errors.Is(err, syscall.EACCES) }},
		{"changeFunc fails before the directory", filepath.Join(root, "d1", "s0", "f00"), nil},
		{"changeFunc fails on the directory", badDir, nil},
		{"changeFunc fails after the directory", filepath.Join(root, "d5", "s0", "f00"), func(err error) bool { return errors.Is(err, syscall.EACCES) }},
	} {
		changeFunc := func(p string) (bool, error) {
			if p == tc.failing {
				return false, errs[p]
			}
			jitter()
			return true, nil
		}
		for round := range 5 {
			seq := runWalk(gowalkDir, root, existing, changeFunc)
			par := runWalk(gowalkDirParallel, root, existing, changeFunc)
			if tc.want == nil && seq.err != errs[tc.failing] || tc.want != nil && !tc.want(seq.err) {
				t.Fatalf("%s: sequential %v", tc.name, seq.err)
			}
			sameResult(t, fmt.Sprintf("%s round %d", tc.name, round), seq, par)
		}
	}

	// an unreadable file fails the hash in both
	badFile := filepath.Join(root, "d2", "s4", "f17")
	FSys = failOpenFS{FS: orig, bad: badFile}
	for _, statCache := range []bool{false, true} {
		hasher := hasherWith(statCache)
		changeFunc := func(p string) (bool, error) {
			_, err := hasher(p)
			return true, err
		}
		seq := runWalk(gowalkDir, root, existing, changeFunc)
		par := runWalk(gowalkDirParallel, root, existing, changeFunc)
		if !errors.Is(seq.err, syscall.EACCES) {
			t.Fatalf("unreadable file, stat cache %v: sequential %v", statCache, seq.err)
		}
		sameResult(t, fmt.Sprintf("unreadable file, stat cache %v", statCache), seq, par)
	}
}

// TestWalkParallelVanishedFile removes a listed file just before its hash, as a
// file that disappears between the walk and the hash.
func TestWalkParallelVanishedFile(t *testing.T) {
	root := t.TempDir()
	makeWalkTree(t, root)
	order := walkOrder(t, root)
	existing := existingFor(order, root)
	victim := filepath.Join(root, "d4", "s1", "f33")
	for _, statCache := range []bool{false, true} {
		hasher := hasherWith(statCache)
		changeFunc := func(p string) (bool, error) {
			if p == victim {
				if err := os.Remove(p); err != nil {
					return false, err
				}
			}
			jitter()
			_, err := hasher(p)
			return true, err
		}
		var results []walkResult
		for _, walk := range []walkFunc{gowalkDir, gowalkDirParallel} {
			mustWrite(t, victim, "victim")
			results = append(results, runWalk(walk, root, existing, changeFunc))
		}
		if !errors.Is(results[0].err, os.ErrNotExist) {
			t.Fatalf("stat cache %v: sequential %v", statCache, results[0].err)
		}
		sameResult(t, fmt.Sprintf("vanished, stat cache %v", statCache), results[0], results[1])
	}
}

func TestHashWorkers(t *testing.T) {
	orig := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(orig)
	for procs, want := range map[int]int{1: 1, 3: 3, 8: 8, 9: 8, 64: 8} {
		runtime.GOMAXPROCS(procs)
		if got := hashWorkers(); got != want {
			t.Errorf("GOMAXPROCS %d: %d workers, want %d", procs, got, want)
		}
	}
}
