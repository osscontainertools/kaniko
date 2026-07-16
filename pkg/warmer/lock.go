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

package warmer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/osscontainertools/kaniko/pkg/assert"
	"golang.org/x/sys/unix"
)

// warmerLockDir is the subdirectory inside cacheDir used to hold per-key
// lock files. It is intentionally a dotfile so it sorts away from the
// digest-named cache entries and is unlikely to collide with anything else.
const warmerLockDir = ".warmer-locks"

// acquireCacheLock takes an exclusive flock on cacheDir/.warmer-locks/<key>.lock
// and returns a cacheLock. It is used by warmToFile and ociWarmToFile to
// serialize the cache write for the same digest across processes sharing
// the same cache volume.
type cacheLock struct {
	f        *os.File
	released bool
}

func acquireCacheLock(cacheDir, key string) (*cacheLock, error) {
	lockDir := filepath.Join(cacheDir, warmerLockDir)
	err := os.MkdirAll(lockDir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("creating warmer lock dir %s: %w", lockDir, err)
	}

	lockPath := filepath.Join(lockDir, key+".lock")
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening warmer lock %s: %w", lockPath, err)
	}

	err = unix.Flock(int(f.Fd()), unix.LOCK_EX)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("acquiring exclusive lock on %s: %w", lockPath, err)
	}

	return &cacheLock{f: f}, nil
}

// Release unlocks the flock and closes the underlying fd. It must be called
// exactly once per successful acquireCacheLock.
func (l *cacheLock) Release() {
	assert.Assert("warmer.cache-lock.single-release", !l.released, "Release() must not be called twice")
	l.released = true
	_ = unix.Flock(int(l.f.Fd()), unix.LOCK_UN)
	_ = l.f.Close()
}
