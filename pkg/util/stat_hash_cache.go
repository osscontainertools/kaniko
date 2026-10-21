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
	"hash/maphash"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/osscontainertools/kaniko/pkg/metrics"
)

// StatHashCacheRacyWindow is how much older than the moment its hash began a
// file's ctime and mtime must both be for the hash to be cached
// (FF_KANIKO_STAT_HASH_CACHE).
//
// The kernel stamps inodes from a clock coarser than time.Now (one tick, a few
// milliseconds), and some filesystems truncate further, to the second or to 2 s.
// Two writes within one tick leave the same ctime and mtime behind, so a hash
// taken between them would be cached under a key that the second write does not
// change. As with git's "racily clean" entries, a file changed that recently is
// hashed but not cached. With the window at least as wide as the timestamp
// granularity, any change made after the hash began gets a later ctime than every
// cached one, so its key cannot match.
const StatHashCacheRacyWindow = 2 * time.Second

// statHashCacheShards splits the cache into maps with a lock each, so that the
// workers of FF_KANIKO_PARALLEL_HASH rarely wait for each other.
const statHashCacheShards = 64

// statKey holds every input of the full hash that Lstat provides (st_mode, from
// which fi.Mode() derives, the fi.ModTime() that is hashed, uid and gid), plus
// what reveals any other change: dev and ino (another inode at this path), size,
// and ctime. The kernel sets ctime to the current time on every write, truncate,
// chmod, chown, utimes, setxattr and link of the inode, and user space cannot
// set it. Content and the security.capability xattr, the remaining inputs, only
// change with ctime.
type statKey struct {
	dev, ino            uint64
	size                int64
	mtimeSec, mtimeNsec int64
	ctimeSec, ctimeNsec int64
	mode                uint32
	uid, gid            uint32
}

func statKeyOf(fi os.FileInfo) (statKey, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return statKey{}, false
	}
	mtime := fi.ModTime()
	ctimeSec, ctimeNsec := st.Ctim.Unix()
	return statKey{
		dev:       st.Dev,
		ino:       st.Ino,
		size:      fi.Size(),
		mtimeSec:  mtime.Unix(),
		mtimeNsec: int64(mtime.Nanosecond()),
		ctimeSec:  ctimeSec,
		ctimeNsec: ctimeNsec,
		mode:      st.Mode,
		uid:       st.Uid,
		gid:       st.Gid,
	}, true
}

// racy reports whether the file of k changed too close to start, the time
// taken before its Lstat, for its hash to be cached. A future ctime or mtime
// (a clock step, touch -d) counts as recent too.
func (k statKey) racy(start time.Time, window time.Duration) bool {
	limit := start.Add(-window)
	return !time.Unix(k.ctimeSec, k.ctimeNsec).Before(limit) ||
		!time.Unix(k.mtimeSec, k.mtimeNsec).Before(limit)
}

type statHashEntry struct {
	key  statKey
	hash string
}

type statHashShard struct {
	mu sync.Mutex
	m  map[string]statHashEntry
	_  [48]byte // a shard per cache line, so neighbouring locks do not share one
}

// statHashCache wraps the full hasher (FF_KANIKO_STAT_HASH_CACHE). It keeps the
// hash of each regular file under its statKey and returns it, without opening
// the file, while a fresh Lstat gives the same key. Because the key covers every
// input of the hash, a hit returns what hashing the file again would.
//
// It lives as long as its Hasher, one per build target (makeSnapshotter), across
// all stages. It holds at most one entry per path ever hashed, and drops the
// entry of a path that stops being a regular file or fails to hash.
type statHashCache struct {
	hash   func(p string, fi os.FileInfo) (string, error)
	lstat  func(p string) (os.FileInfo, error)
	now    func() time.Time
	window time.Duration
	seed   maphash.Seed
	shards [statHashCacheShards]statHashShard
}

func newStatHashCache(hash func(string, os.FileInfo) (string, error), now func() time.Time, window time.Duration) *statHashCache {
	c := &statHashCache{hash: hash, lstat: os.Lstat, now: now, window: window, seed: maphash.MakeSeed()}
	for i := range c.shards {
		c.shards[i].m = map[string]statHashEntry{}
	}
	return c
}

// hashOutcome says how statHashCache.lookup came to its hash.
type hashOutcome int

const (
	hashUncached hashOutcome = iota // not a regular file, or an error: hashed and not cached
	hashHit                         // the stored hash, the file was not read
	hashMiss                        // the file was read and its hash stored
	hashSkipRacy                    // the file was read, too recently changed to store
)

// Hash is the full hasher with the cache in front. It is safe for concurrent use.
func (c *statHashCache) Hash(p string) (string, error) {
	h, outcome, err := c.lookup(p)
	switch outcome {
	case hashHit:
		metrics.SnapshotHashCacheHit.Add(0, 1)
	case hashMiss:
		metrics.SnapshotHashCacheMiss.Add(0, 1)
	case hashSkipRacy:
		metrics.SnapshotHashCacheMiss.Add(0, 1)
		metrics.SnapshotHashCacheSkipRacy.Add(0, 1)
	}
	return h, err
}

func (c *statHashCache) shard(p string) *statHashShard {
	return &c.shards[maphash.String(c.seed, p)%statHashCacheShards]
}

func (c *statHashCache) lookup(p string) (string, hashOutcome, error) {
	// before the Lstat: a change after this moment gets a later ctime
	start := c.now()
	fi, err := c.lstat(p)
	if err != nil {
		c.forget(p)
		return "", hashUncached, err
	}
	key, ok := statKeyOf(fi)
	if !ok || !fi.Mode().IsRegular() {
		// directories and symlinks are not read, hashing them stays cheap
		c.forget(p)
		h, err := c.hash(p, fi)
		return h, hashUncached, err
	}

	sh := c.shard(p)
	sh.mu.Lock()
	e, found := sh.m[p]
	sh.mu.Unlock()
	if found && e.key == key {
		// counted like a hash, so snapshot.hash.items stays the number of files asked for
		metrics.SnapshotHash.Add(0, 1)
		return e.hash, hashHit, nil
	}

	// e is stale or absent; the hash below uses the same fi as key, so a change
	// of the file while it is read leaves the stored key behind for good
	h, err := c.hash(p, fi)
	if err != nil {
		if found {
			c.forget(p)
		}
		return "", hashUncached, err
	}
	if key.racy(start, c.window) {
		if found {
			c.forget(p)
		}
		return h, hashSkipRacy, nil
	}
	sh.mu.Lock()
	sh.m[p] = statHashEntry{key: key, hash: h}
	sh.mu.Unlock()
	return h, hashMiss, nil
}

func (c *statHashCache) forget(p string) {
	sh := c.shard(p)
	sh.mu.Lock()
	delete(sh.m, p)
	sh.mu.Unlock()
}

// len is the number of entries, for tests.
func (c *statHashCache) len() int {
	n := 0
	for i := range c.shards {
		c.shards[i].mu.Lock()
		n += len(c.shards[i].m)
		c.shards[i].mu.Unlock()
	}
	return n
}
