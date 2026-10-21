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
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/minio/highwayhash"
	"github.com/osscontainertools/kaniko/pkg/config"
	"golang.org/x/sys/unix"
)

// settle waits until files written before it are old enough to be cached.
func settle() { time.Sleep(StatHashCacheRacyWindow + 500*time.Millisecond) }

// hasherWith builds Hasher with FF_KANIKO_STAT_HASH_CACHE set as given, the
// flag being read once, at construction.
func hasherWith(statCache bool) func(string) (string, error) {
	orig := config.FF.StatHashCache
	config.FF.StatHashCache = statCache
	defer func() { config.FF.StatHashCache = orig }()
	return Hasher()
}

func newTestStatHashCache(window time.Duration) *statHashCache {
	pool := sync.Pool{New: func() any {
		b := make([]byte, highwayhash.Size*10*1024)
		return &b
	}}
	key := make([]byte, highwayhash.Size)
	return newStatHashCache(func(p string, fi os.FileInfo) (string, error) {
		return hashFileInfo(p, fi, key, &pool)
	}, time.Now, window)
}

// referenceHasher is Hasher as of 34ba9eadf, before hashFileInfo was split out.
func referenceHasher() func(string) (string, error) {
	pool := sync.Pool{
		New: func() any {
			b := make([]byte, highwayhash.Size*10*1024)
			return &b
		},
	}
	key := make([]byte, highwayhash.Size)
	hasher := func(p string) (string, error) {
		h, _ := highwayhash.New(key)
		fi, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		h.Write([]byte(fi.Mode().String()))
		h.Write([]byte(fi.ModTime().String()))

		h.Write([]byte(strconv.FormatUint(uint64(fi.Sys().(*syscall.Stat_t).Uid), 36)))
		h.Write([]byte(","))
		h.Write([]byte(strconv.FormatUint(uint64(fi.Sys().(*syscall.Stat_t).Gid), 36)))

		if fi.Mode().IsRegular() {
			capability, _ := Lgetxattr(p, "security.capability")
			if capability != nil {
				h.Write(capability)
			}
			f, err := FSys.Open(p)
			if err != nil {
				return "", err
			}
			defer f.Close()
			buf := pool.Get().(*[]byte)
			defer pool.Put(buf)
			if _, err := io.CopyBuffer(h, f, *buf); err != nil {
				return "", err
			}
		} else if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
			linkPath, err := os.Readlink(p)
			if err != nil {
				return "", err
			}
			h.Write([]byte(linkPath))
		}

		return hex.EncodeToString(h.Sum(nil)), nil
	}
	return hasher
}

// capV2 is a valid security.capability value: revision 2, effective,
// permitted CAP_NET_BIND_SERVICE.
var capV2 = []byte{0x01, 0, 0, 0x02, 0x00, 0x04, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mtimeOf(t *testing.T, p string) time.Time {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime()
}

func keyOf(t *testing.T, p string) statKey {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	k, ok := statKeyOf(fi)
	if !ok {
		t.Fatalf("no stat key for %s", p)
	}
	return k
}

func isPermission(err error) bool {
	return errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES) || errors.Is(err, unix.ENOTSUP)
}

// TestStatHashCacheMatchesFresh applies every kind of change to files whose
// hashes are cached, and expects the cache to agree with a fresh hasher each
// time, and to have read the changed files again.
func TestStatHashCacheMatchesFresh(t *testing.T) {
	dir := t.TempDir()
	staging := t.TempDir()
	const (
		a = "aaaaaaaaaaaaaaaa"
		b = "bbbbbbbbbbbbbbbb"
	)
	type scenario struct {
		name    string
		regular bool // a regular file when the cache is primed
		setup   func(t *testing.T, p string)
		mutate  func(t *testing.T, p string) // nil: left unchanged
		// what the change must leave alone, so that only the rest of the key
		// can reveal it
		sameSize, sameMtime bool
	}
	scenarios := []scenario{
		{name: "unchanged", regular: true, setup: func(t *testing.T, p string) { mustWrite(t, p, a) }},
		{
			name: "content-same-size", regular: true, sameSize: true,
			setup:  func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) { mustWrite(t, p, b) },
		},
		{
			name: "content-same-size-mtime-restored", regular: true, sameSize: true, sameMtime: true,
			setup: func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) {
				mt := mtimeOf(t, p)
				mustWrite(t, p, b)
				if err := os.Chtimes(p, mt, mt); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mtime-only", regular: true, sameSize: true,
			setup: func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) {
				mt := mtimeOf(t, p).Add(-time.Hour)
				if err := os.Chtimes(p, mt, mt); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "chmod", regular: true, sameSize: true, sameMtime: true,
			setup: func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) {
				if err := os.Chmod(p, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "chown", regular: true, sameSize: true, sameMtime: true,
			setup: func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) {
				if err := os.Lchown(p, 4321, 4321); err != nil {
					if isPermission(err) {
						t.Skip("chown needs root")
					}
					t.Fatal(err)
				}
			},
		},
		{
			name: "rename-over-same-size-mtime", regular: true, sameSize: true, sameMtime: true,
			setup: func(t *testing.T, p string) {
				mustWrite(t, p, a)
				other := filepath.Join(staging, "rename-src")
				mustWrite(t, other, b)
				mt := mtimeOf(t, p)
				if err := os.Chtimes(other, mt, mt); err != nil {
					t.Fatal(err)
				}
			},
			mutate: func(t *testing.T, p string) {
				if err := os.Rename(filepath.Join(staging, "rename-src"), p); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "file-to-symlink", regular: true,
			setup: func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("unchanged", p); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink-to-file", regular: false,
			setup: func(t *testing.T, p string) {
				if err := os.Symlink("unchanged", p); err != nil {
					t.Fatal(err)
				}
			},
			mutate: func(t *testing.T, p string) {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, p, a)
			},
		},
		{
			name: "delete-recreate-same-size-mtime", regular: true, sameSize: true, sameMtime: true,
			setup: func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) {
				mt := mtimeOf(t, p)
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, p, b)
				if err := os.Chtimes(p, mt, mt); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "write-through-second-hardlink", regular: true, sameSize: true, sameMtime: true,
			setup: func(t *testing.T, p string) {
				mustWrite(t, p, a)
				if err := os.Link(p, filepath.Join(staging, "hardlink")); err != nil {
					t.Fatal(err)
				}
			},
			mutate: func(t *testing.T, p string) {
				other := filepath.Join(staging, "hardlink")
				mt := mtimeOf(t, other)
				f, err := os.OpenFile(other, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.WriteAt([]byte(b), 0); err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(other, mt, mt); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "security-capability-xattr", regular: true, sameSize: true, sameMtime: true,
			setup: func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) {
				if err := Lsetxattr(p, securityCapabilityXattr, capV2, 0); err != nil {
					if isPermission(err) {
						t.Skipf("setting %s: %v", securityCapabilityXattr, err)
					}
					t.Fatal(err)
				}
				if got, _ := Lgetxattr(p, securityCapabilityXattr); !bytes.Equal(got, capV2) {
					t.Skipf("%s did not take: %x", securityCapabilityXattr, got)
				}
			},
		},
		{
			name: "deleted", regular: true,
			setup: func(t *testing.T, p string) { mustWrite(t, p, a) },
			mutate: func(t *testing.T, p string) {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	fresh := hasherWith(false)
	c := newTestStatHashCache(StatHashCacheRacyWindow)
	for _, s := range scenarios {
		s.setup(t, filepath.Join(dir, s.name))
	}
	settle()

	// prime: every regular file is old enough to be stored, then served
	before := map[string]string{}
	keys := map[string]statKey{}
	for _, s := range scenarios {
		p := filepath.Join(dir, s.name)
		want, err := fresh(p)
		if err != nil {
			t.Fatal(err)
		}
		wantOutcome := hashUncached
		if s.regular {
			wantOutcome = hashMiss
		}
		for _, w := range []hashOutcome{wantOutcome, hashHit} {
			if !s.regular && w == hashHit {
				w = hashUncached
			}
			got, outcome, err := c.lookup(p)
			if err != nil || got != want || outcome != w {
				t.Fatalf("%s primed: got %s %v %v, want %s %v", s.name, got, outcome, err, want, w)
			}
		}
		before[s.name] = want
		if s.regular {
			keys[s.name] = keyOf(t, p)
		}
	}

	for _, s := range scenarios {
		if s.mutate == nil {
			continue
		}
		t.Run(s.name, func(t *testing.T) {
			p := filepath.Join(dir, s.name)
			s.mutate(t, p) // may skip

			want, wantErr := fresh(p)
			got, outcome, err := c.lookup(p)
			if wantErr != nil || err != nil {
				if wantErr == nil || err == nil || wantErr.Error() != err.Error() {
					t.Fatalf("errors differ: cached %v, fresh %v", err, wantErr)
				}
				return
			}
			if got != want {
				t.Fatalf("cached %s, fresh %s", got, want)
			}
			if outcome == hashHit {
				t.Fatalf("change not detected, served from the cache")
			}
			if want == before[s.name] {
				t.Fatalf("the change left the hash as it was, the case tests nothing")
			}
			if s.sameSize || s.sameMtime {
				// only the rest of the key can have caught it
				now := keyOf(t, p)
				old := keys[s.name]
				if s.sameSize && now.size != old.size {
					t.Errorf("size changed: %d -> %d", old.size, now.size)
				}
				if s.sameMtime && (now.mtimeSec != old.mtimeSec || now.mtimeNsec != old.mtimeNsec) {
					t.Errorf("mtime changed")
				}
				t.Logf("key %+v -> %+v", old, now)
			}
		})
	}

	// once they are old enough, the changed files are cached again
	settle()
	regular := 0
	for _, s := range scenarios {
		p := filepath.Join(dir, s.name)
		want, wantErr := fresh(p)
		if wantErr != nil {
			if _, _, err := c.lookup(p); err == nil {
				t.Errorf("%s: no error from the cache, fresh %v", s.name, wantErr)
			}
			continue
		}
		fi, _ := os.Lstat(p)
		// changed files are stored now; a skipped change leaves a hit
		first, second := []hashOutcome{hashMiss, hashHit}, hashHit
		if !fi.Mode().IsRegular() {
			first, second = []hashOutcome{hashUncached}, hashUncached
		} else {
			regular++
		}
		if s.mutate == nil {
			first = []hashOutcome{hashHit}
		}
		got, outcome, err := c.lookup(p)
		if err != nil || got != want || !slices.Contains(first, outcome) {
			t.Errorf("%s settled: got %s %v %v, want %s %v", s.name, got, outcome, err, want, first)
		}
		got, outcome, err = c.lookup(p)
		if err != nil || got != want || outcome != second {
			t.Errorf("%s settled, again: got %s %v %v, want %s %v", s.name, got, outcome, err, want, second)
		}
	}
	if n := c.len(); n != regular {
		t.Errorf("%d entries cached, %d regular files exist", n, regular)
	}
}

// TestHardlinkWriteChangesCtime checks the kernel behaviour the cache relies on
// for hardlinks: a write through one name moves the ctime that Lstat of the
// other name reports, as both names share the inode.
func TestHardlinkWriteChangesCtime(t *testing.T) {
	dir := t.TempDir()
	p, other := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	mustWrite(t, p, "aaaa")
	if err := os.Link(p, other); err != nil {
		t.Fatal(err)
	}
	old := keyOf(t, p)
	time.Sleep(20 * time.Millisecond) // past a coarse timestamp tick
	f, err := os.OpenFile(other, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("bbbb"), 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	now := keyOf(t, p)
	if now.ino != old.ino {
		t.Fatalf("not the same inode")
	}
	if now.ctimeSec == old.ctimeSec && now.ctimeNsec == old.ctimeNsec {
		t.Fatalf("ctime of %s did not move after a write through %s", p, other)
	}
}

// TestStatHashCacheRacy writes twice in quick succession: the first hash must
// not be cached, so the second write cannot be served the first hash.
func TestStatHashCacheRacy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	fresh := hasherWith(false)

	t.Run("real-clock", func(t *testing.T) {
		c := newTestStatHashCache(StatHashCacheRacyWindow)
		mustWrite(t, p, "aaaaaaaaaaaaaaaa")
		h1, out1, err := c.lookup(p)
		if err != nil || out1 != hashSkipRacy {
			t.Fatalf("first: %v %v, want %v", out1, err, hashSkipRacy)
		}
		mt := mtimeOf(t, p)
		mustWrite(t, p, "bbbbbbbbbbbbbbbb")
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		h2, out2, err := c.lookup(p)
		want, _ := fresh(p)
		if err != nil || out2 != hashSkipRacy || h2 != want || h2 == h1 {
			t.Fatalf("second: %s %v %v, want %s %v (first %s)", h2, out2, err, want, hashSkipRacy, h1)
		}
		if c.len() != 0 {
			t.Fatalf("racy entries were cached")
		}
	})

	// A filesystem that keeps whole seconds, simulated by truncating the
	// timestamps of Lstat: two writes within a second leave the same key. Without
	// the window the second lookup is served the stale hash; with it, never.
	coarse := func(p string) (os.FileInfo, error) {
		fi, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		st := *fi.Sys().(*syscall.Stat_t)
		st.Ctim.Nsec, st.Mtim.Nsec = 0, 0
		return coarseInfo{fi, &st}, nil
	}
	for _, window := range []time.Duration{0, StatHashCacheRacyWindow} {
		t.Run(fmt.Sprintf("coarse-window-%s", window), func(t *testing.T) {
			for attempt := 0; ; attempt++ {
				if attempt == 5 {
					t.Fatal("could not write twice within one second")
				}
				c := newTestStatHashCache(window)
				c.lstat = coarse
				// start right after a second boundary
				time.Sleep(time.Second - time.Duration(time.Now().Nanosecond()) + 10*time.Millisecond)
				second := time.Now().Unix()
				mustWrite(t, p, "aaaaaaaaaaaaaaaa")
				h1, out1, err1 := c.lookup(p)
				mustWrite(t, p, "bbbbbbbbbbbbbbbb")
				h2, out2, err2 := c.lookup(p)
				if time.Now().Unix() != second {
					continue
				}
				if err1 != nil || err2 != nil {
					t.Fatal(err1, err2)
				}
				fi, _ := coarse(p)
				want, err := c.hash(p, fi)
				if err != nil {
					t.Fatal(err)
				}
				if window == 0 {
					// the hazard the window exists for
					if out1 != hashMiss || out2 != hashHit || h2 != h1 || h2 == want {
						t.Fatalf("without a window: %v %v, stale %v; expected a stale hit", out1, out2, h2 != want)
					}
					return
				}
				if out1 != hashSkipRacy || out2 != hashSkipRacy || h2 != want {
					t.Fatalf("with the window: %v %v, got %s want %s", out1, out2, h2, want)
				}
				return
			}
		})
	}
}

type coarseInfo struct {
	os.FileInfo
	st *syscall.Stat_t
}

func (c coarseInfo) ModTime() time.Time {
	sec, _ := c.st.Mtim.Unix()
	return time.Unix(sec, 0)
}

func (c coarseInfo) Sys() any { return c.st }

func TestStatKeyRacy(t *testing.T) {
	start := time.Unix(1000, 0)
	w := 2 * time.Second
	for _, tc := range []struct {
		name         string
		ctime, mtime time.Time
		racy         bool
	}{
		{"both old", time.Unix(997, 999_999_999), time.Unix(900, 0), false},
		{"ctime at the limit", time.Unix(998, 0), time.Unix(900, 0), true},
		{"ctime recent", time.Unix(999, 500), time.Unix(900, 0), true},
		{"mtime recent", time.Unix(990, 0), time.Unix(999, 0), true},
		{"mtime in the future", time.Unix(990, 0), time.Unix(5000, 0), true},
		{"ctime in the future", time.Unix(5000, 0), time.Unix(900, 0), true},
	} {
		k := statKey{
			ctimeSec: tc.ctime.Unix(), ctimeNsec: int64(tc.ctime.Nanosecond()),
			mtimeSec: tc.mtime.Unix(), mtimeNsec: int64(tc.mtime.Nanosecond()),
		}
		if got := k.racy(start, w); got != tc.racy {
			t.Errorf("%s: racy %v, want %v", tc.name, got, tc.racy)
		}
	}
}

type countingFS struct {
	fs.FS
	opens atomic.Int64
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.opens.Add(1)
	return c.FS.Open(name)
}

// TestHasherFlags: Hasher gives the hashes it gave before the stat cache with
// the flag off and on, and with the flag off reads every file on every call.
func TestHasherFlags(t *testing.T) {
	dir := t.TempDir()
	files := map[string]func(p string) error{
		"regular": func(p string) error { return os.WriteFile(p, []byte("content"), 0o644) },
		"empty":   func(p string) error { return os.WriteFile(p, nil, 0o600) },
		"large": func(p string) error {
			return os.WriteFile(p, bytes.Repeat([]byte("0123456789abcdef"), 1<<16), 0o755) // 1 MiB, past the 320 KiB buffer
		},
		"capability": func(p string) error {
			if err := os.WriteFile(p, []byte("binary"), 0o755); err != nil {
				return err
			}
			if err := Lsetxattr(p, securityCapabilityXattr, capV2, 0); err != nil && !isPermission(err) {
				return err
			}
			return nil
		},
		"symlink": func(p string) error { return os.Symlink("regular", p) },
		"dir":     func(p string) error { return os.Mkdir(p, 0o750) },
		"fifo":    func(p string) error { return unix.Mkfifo(p, 0o644) },
	}
	for name, mk := range files {
		if err := mk(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	ref := referenceHasher()
	off := hasherWith(false)
	on := hasherWith(true)
	check := func() {
		for name := range files {
			p := filepath.Join(dir, name)
			want, err := ref(p)
			if err != nil {
				t.Fatal(err)
			}
			for flag, h := range map[string]func(string) (string, error){"off": off, "on": on} {
				got, err := h(p)
				if err != nil || got != want {
					t.Errorf("%s with the flag %s: %s %v, want %s", name, flag, got, err, want)
				}
			}
		}
	}
	check()
	_, missing := ref(filepath.Join(dir, "missing"))
	for _, h := range []func(string) (string, error){off, on} {
		if _, err := h(filepath.Join(dir, "missing")); err == nil || err.Error() != missing.Error() {
			t.Errorf("missing file: %v, want %v", err, missing)
		}
	}

	settle()
	check()
	orig := FSys
	counting := &countingFS{FS: orig}
	FSys = counting
	defer func() { FSys = orig }()
	p := filepath.Join(dir, "regular")
	for _, tc := range []struct {
		name  string
		h     func(string) (string, error)
		opens int64
	}{
		{"off", off, 3},
		{"on, cached by check()", on, 0},
	} {
		counting.opens.Store(0)
		for range 3 {
			if _, err := tc.h(p); err != nil {
				t.Fatal(err)
			}
		}
		if got := counting.opens.Load(); got != tc.opens {
			t.Errorf("flag %s: %d opens for 3 hashes, want %d", tc.name, got, tc.opens)
		}
	}
}

// TestStatHashCacheConcurrent hashes one tree from 8 goroutines under -race.
func TestStatHashCacheConcurrent(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for i := range 200 {
		p := filepath.Join(dir, fmt.Sprintf("f%03d", i))
		mustWrite(t, p, fmt.Sprintf("content %d", i))
		paths = append(paths, p)
	}
	fresh := hasherWith(false)
	want := map[string]string{}
	for _, p := range paths {
		want[p], _ = fresh(p)
	}
	c := newTestStatHashCache(StatHashCacheRacyWindow)
	// every file counts as old, so entries are stored and served concurrently
	c.now = func() time.Time { return time.Now().Add(time.Hour) }
	var wg sync.WaitGroup
	var hits atomic.Int64
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range 5 {
				for i := range paths {
					p := paths[(i+g*25+round)%len(paths)]
					got, outcome, err := c.lookup(p)
					if err != nil || got != want[p] {
						t.Errorf("%s: %s %v, want %s", p, got, err, want[p])
					}
					if outcome == hashHit {
						hits.Add(1)
					}
				}
			}
		}()
	}
	wg.Wait()
	if hits.Load() == 0 || c.len() != len(paths) {
		t.Errorf("%d hits, %d entries", hits.Load(), c.len())
	}
}
