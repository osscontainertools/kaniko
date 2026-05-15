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

	"golang.org/x/sys/unix"
)

// warmerLockDir is the subdirectory inside cacheDir used to hold per-key
// lock files. It is intentionally a dotfile so it sorts away from the
// digest-named cache entries and is unlikely to collide with anything else.
const warmerLockDir = ".warmer-locks"

// acquireCacheLock takes an exclusive flock on cacheDir/.warmer-locks/<key>.lock
// and returns a release closure. It is used by warmToFile and ociWarmToFile to
// serialize the final-rename step for the same digest across processes sharing
// the same cache volume.
//
// The lock is held only around the destination recheck and rename, not across
// the image download itself. Concurrent warmers fetching the same image will
// each pay the download cost; the lock simply guarantees that only one of
// them moves the result into place. This avoids ENOTEMPTY rename failures
// (FF_KANIKO_OCI_WARMER, which renames a directory) and silent overwrites
// (the legacy tarball path, which renames a file).
func acquireCacheLock(cacheDir, key string) (func(), error) {
	lockDir := filepath.Join(cacheDir, warmerLockDir)
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating warmer lock dir %s: %w", lockDir, err)
	}

	lockPath := filepath.Join(lockDir, key+".lock")
	f, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening warmer lock %s: %w", lockPath, err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("acquiring exclusive lock on %s: %w", lockPath, err)
	}

	released := false
	return func() {
		if released {
			return
		}
		released = true
		// Best effort: log nothing here, callers don't have a logger handle
		// and a failure to unlock is not something they can act on. Closing
		// the fd will release the flock regardless via kernel cleanup.
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
