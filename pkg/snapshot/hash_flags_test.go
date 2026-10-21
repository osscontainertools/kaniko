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
	"archive/tar"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/util"
	"golang.org/x/sys/unix"
)

// hashFlags is one combination of FF_KANIKO_STAT_HASH_CACHE and FF_KANIKO_PARALLEL_HASH.
type hashFlags struct{ stat, parallel bool }

func (f hashFlags) String() string { return fmt.Sprintf("stat=%v,parallel=%v", f.stat, f.parallel) }

var allHashFlags = []hashFlags{{false, false}, {true, false}, {false, true}, {true, true}}

// set turns the flags on as given and returns the restore function. The stat
// cache is read when the hasher is built, the parallel walk on every scan.
func (f hashFlags) set() func() {
	stat, parallel := config.FF.StatHashCache, config.FF.ParallelHash
	config.FF.StatHashCache, config.FF.ParallelHash = f.stat, f.parallel
	return func() { config.FF.StatHashCache, config.FF.ParallelHash = stat, parallel }
}

func newHasher(stat bool) func(string) (string, error) {
	defer hashFlags{stat: stat}.set()()
	return util.Hasher()
}

// openCountingFS counts the regular files opened through util.FSys.
type openCountingFS struct {
	fs.FS
	regular atomic.Int64
}

func (c *openCountingFS) Open(name string) (fs.File, error) {
	if fi, err := os.Lstat(name); err == nil && fi.Mode().IsRegular() {
		c.regular.Add(1)
	}
	return c.FS.Open(name)
}

// tarEntries describes each tar entry without atime and ctime, which reading
// the tree may move between two snapshots of the same state. Of the ancestors
// of root, such as /tmp, only the name counts: other tests change them.
func tarEntries(t *testing.T, path, root string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if name := "/" + strings.TrimSuffix(hdr.Name, "/"); strings.HasPrefix(root, name+"/") || name == "/" {
			out = append(out, hdr.Name+" (ancestor of root)")
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		var pax []string
		for k, v := range hdr.PAXRecords {
			if k != "atime" && k != "ctime" {
				pax = append(pax, k+"="+v)
			}
		}
		sort.Strings(pax)
		out = append(out, fmt.Sprintf("%s type=%c mode=%o uid=%d gid=%d size=%d mtime=%d link=%q pax=%v sha=%x",
			hdr.Name, hdr.Typeflag, hdr.Mode, hdr.Uid, hdr.Gid, hdr.Size, hdr.ModTime.UnixNano(), hdr.Linkname, pax, sha256.Sum256(content)))
	}
}

// TestScanUnchangedByHashFlags runs one snapshotter per flag combination in
// lockstep over the same tree through a series of changes, and expects the
// same layers from all of them.
func TestScanUnchangedByHashFlags(t *testing.T) {
	root := t.TempDir()
	staging := t.TempDir()
	snapshotPathPrefix = t.TempDir()
	original := config.KanikoLayersDir
	config.KanikoLayersDir = t.TempDir()
	t.Cleanup(func() { config.KanikoLayersDir = original })

	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// same size, mtime put back: only ctime tells
	rewrite := func(rel, content string) {
		p := filepath.Join(root, rel)
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		write(rel, content)
		if err := os.Chtimes(p, fi.ModTime(), fi.ModTime()); err != nil {
			t.Fatal(err)
		}
	}
	for d := range 4 {
		for f := range 40 {
			write(fmt.Sprintf("d%d/f%02d", d, f), fmt.Sprintf("file %d %02d", d, f))
		}
		if err := os.Symlink("f00", filepath.Join(root, fmt.Sprintf("d%d/link", d))); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Link(filepath.Join(root, "d0/f01"), filepath.Join(staging, "alias")); err != nil {
		t.Fatal(err)
	}

	snapshotters := map[hashFlags]*Snapshotter{}
	for _, f := range allHashFlags {
		snapshotters[f] = NewSnapshotter(NewLayeredMap(newHasher(f.stat)), root)
	}
	orig := util.FSys
	counting := &openCountingFS{FS: orig}
	util.FSys = counting
	t.Cleanup(func() { util.FSys = orig })

	opens := map[hashFlags]int64{}
	each := func(step string, fn func(s *Snapshotter) (string, []string, []string, error)) {
		t.Helper()
		var want []string
		for _, f := range allHashFlags {
			restore := f.set()
			counting.regular.Store(0)
			tarPath, added, whiteouts, err := fn(snapshotters[f])
			opens[f] = counting.regular.Load()
			restore()
			if err != nil {
				t.Fatalf("%s %v: %v", step, f, err)
			}
			got := []string{fmt.Sprintf("added=%v", added), fmt.Sprintf("whiteouts=%v", whiteouts)}
			if tarPath != "" {
				got = append(got, tarEntries(t, tarPath, root)...)
			}
			if want == nil {
				want = got
				continue
			}
			if !slices.Equal(got, want) {
				t.Fatalf("%s: %v differs from %v:\n%v\n%v", step, f, allHashFlags[0], got, want)
			}
		}
	}
	scan := func(s *Snapshotter) (string, []string, []string, error) { return s.TakeSnapshotFS() }

	each("init", func(s *Snapshotter) (string, []string, []string, error) { return "", nil, nil, s.Init() })
	time.Sleep(util.StatHashCacheRacyWindow + 500*time.Millisecond)
	each("unchanged, priming the cache", scan)
	each("unchanged, cached", scan)
	if opens[hashFlags{true, false}] != 0 || opens[hashFlags{true, true}] != 0 || opens[hashFlags{false, false}] < 160 {
		t.Fatalf("regular files opened per flags: %v; the cache is not in use", opens)
	}

	// changes to files whose hash is cached
	rewrite("d0/f02", "FILE 0 02")
	if err := os.Chmod(filepath.Join(root, "d0/f03"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "d0/f04"), time.Unix(1e9, 0), time.Unix(1e9, 0)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "d1/f05")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "d1/f06")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("f00", filepath.Join(root, "d1/f06")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "d3")); err != nil {
		t.Fatal(err)
	}
	write("d4/new", "new file")
	alias := filepath.Join(staging, "alias")
	fi, _ := os.Lstat(alias)
	if err := os.WriteFile(alias, []byte("FILE 0 01"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(alias, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := util.Lsetxattr(filepath.Join(root, "d2/f07"), "security.capability",
		[]byte{0x01, 0, 0, 0x02, 0x00, 0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 0); err != nil &&
		!errors.Is(err, unix.EPERM) && !errors.Is(err, unix.ENOTSUP) {
		t.Fatal(err)
	}
	each("changed", scan)

	// changes right after a snapshot, while the files are too recent to cache
	time.Sleep(util.StatHashCacheRacyWindow + 500*time.Millisecond)
	each("unchanged, cached again", scan)
	rewrite("d2/f08", "FILE 2 08")
	each("racy, first write", scan)
	rewrite("d2/f08", "file 2 08")
	each("racy, second write", scan)
	rewrite("d2/f08", "FiLe 2 08")
	each("racy, third write", scan)

	// a listed snapshot, as for COPY, after a rename over a cached file
	time.Sleep(util.StatHashCacheRacyWindow + 500*time.Millisecond)
	each("unchanged before COPY", scan)
	write("d0/src", "FILE 0 09")
	src := filepath.Join(root, "d0/src")
	fi, _ = os.Lstat(filepath.Join(root, "d0/f09"))
	if err := os.Chtimes(src, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, filepath.Join(root, "d0/f09")); err != nil {
		t.Fatal(err)
	}
	each("listed", func(s *Snapshotter) (string, []string, []string, error) {
		return s.TakeSnapshot([]string{filepath.Join(root, "d0/f09")}, true)
	})
	each("after listed", scan)
}

// TestCheckFileChangeConcurrent calls CheckFileChange from 8 goroutines, as
// FF_KANIKO_PARALLEL_HASH does, and expects the answers and the layer hash
// cache of sequential calls. Run with -race.
func TestCheckFileChangeConcurrent(t *testing.T) {
	root := t.TempDir()
	var paths []string
	for i := range 300 {
		p := filepath.Join(root, fmt.Sprintf("f%03d", i))
		if err := os.WriteFile(p, fmt.Appendf(nil, "file %d", i), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	for _, stat := range []bool{false, true} {
		build := func() *LayeredMap {
			l := NewLayeredMap(newHasher(stat))
			l.Snapshot()
			for i, p := range paths {
				if i%2 == 0 {
					if err := l.Add(p); err != nil {
						t.Fatal(err)
					}
				}
			}
			l.Snapshot()
			return l
		}
		ref, l := build(), build()
		for i, p := range paths {
			if i%5 == 0 {
				if err := os.WriteFile(p, fmt.Appendf(nil, "changed %d", i), 0o644); err != nil {
					t.Fatal(err)
				}
			}
		}
		want := map[string]bool{}
		for _, p := range paths {
			changed, err := ref.CheckFileChange(p)
			if err != nil {
				t.Fatal(err)
			}
			want[p] = changed
		}

		var wg sync.WaitGroup
		for g := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range paths {
					p := paths[(i+g*37)%len(paths)]
					changed, err := l.CheckFileChange(p)
					if err != nil || changed != want[p] {
						t.Errorf("stat %v: %s changed %v %v, want %v", stat, p, changed, err, want[p])
					}
				}
			}()
		}
		wg.Wait()
		if len(l.layerHashCache) != len(paths) {
			t.Fatalf("%d hashes kept, want %d", len(l.layerHashCache), len(paths))
		}
		for _, p := range paths {
			if l.layerHashCache[p] != ref.layerHashCache[p] {
				t.Errorf("stat %v: hash of %s differs", stat, p)
			}
		}
	}
}
