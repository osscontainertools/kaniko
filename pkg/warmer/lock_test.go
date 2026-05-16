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
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Each os.OpenFile call returns a distinct open file description, and
// flock(LOCK_EX) on Linux synchronizes per-OFD — so two goroutines using
// acquireCacheLock against the same lock file serialize against each
// other exactly as two separate processes would.

func TestAcquireCacheLock_CreatesLockDir(t *testing.T) {
	cacheDir := t.TempDir()

	release, err := acquireCacheLock(cacheDir, "sha256:abcd")
	if err != nil {
		t.Fatalf("acquireCacheLock returned error: %v", err)
	}
	defer release()

	lockDir := filepath.Join(cacheDir, warmerLockDir)
	info, err := os.Stat(lockDir)
	if err != nil {
		t.Fatalf("expected lock dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", lockDir)
	}

	lockFile := filepath.Join(lockDir, "sha256:abcd.lock")
	if _, err := os.Stat(lockFile); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}
}

func TestAcquireCacheLock_MutualExclusionSameKey(t *testing.T) {
	cacheDir := t.TempDir()
	const key = "sha256:contend"

	// inCritical tracks the number of goroutines currently holding the lock.
	// If acquireCacheLock is correct, this value never exceeds 1.
	var inCritical int32
	var maxObserved int32

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			release, err := acquireCacheLock(cacheDir, key)
			if err != nil {
				t.Errorf("acquireCacheLock returned error: %v", err)
				return
			}
			defer release()

			cur := atomic.AddInt32(&inCritical, 1)
			for {
				prev := atomic.LoadInt32(&maxObserved)
				if cur <= prev || atomic.CompareAndSwapInt32(&maxObserved, prev, cur) {
					break
				}
			}
			// Hold the critical section briefly so any racers have a
			// chance to be seen if mutual exclusion is broken.
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt32(&inCritical, -1)
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxObserved); got != 1 {
		t.Errorf("max goroutines observed holding the lock concurrently: got %d, want 1", got)
	}
}

func TestAcquireCacheLock_DifferentKeysDoNotBlock(t *testing.T) {
	cacheDir := t.TempDir()

	releaseA, err := acquireCacheLock(cacheDir, "sha256:aaa")
	if err != nil {
		t.Fatalf("first acquire failed: %v", err)
	}
	defer releaseA()

	// Acquiring a different key from the same process must succeed without
	// blocking. We use a tight timeout via a goroutine to avoid hanging the
	// test if the assumption breaks.
	done := make(chan error, 1)
	go func() {
		release, err := acquireCacheLock(cacheDir, "sha256:bbb")
		if err == nil {
			release()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("acquire of independent key returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire of independent key blocked unexpectedly")
	}
}

func TestAcquireCacheLock_ReleaseIsIdempotent(t *testing.T) {
	cacheDir := t.TempDir()

	release, err := acquireCacheLock(cacheDir, "sha256:once")
	if err != nil {
		t.Fatalf("acquireCacheLock returned error: %v", err)
	}
	release()
	release() // must not panic, must not error
}
