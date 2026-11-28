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

// Package addcache stores remote ADD sources in a directory shared by the
// warmer and the executor.
package addcache

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/osscontainertools/kaniko/pkg/assert"
	"github.com/sirupsen/logrus"
)

// DirName is the subdirectory of the cache directory that holds ADD sources.
const DirName = "add"

// Source is a remote ADD source that can be cached before a build.
type Source struct {
	URL      string
	Checksum digest.Digest
}

func Path(cacheDir string, checksum digest.Digest) string {
	return filepath.Join(cacheDir, DirName, checksum.String())
}

// Lookup returns the cached file for checksum. The second result is false
// when the cache has no entry. An entry whose content does not match checksum
// is reported as an error.
func Lookup(cacheDir string, checksum digest.Digest) (string, bool, error) {
	// DownloadFileToDest only ever verifies sha256, so no other algorithm may hit.
	assert.Assert("addcache.lookup.sha256", checksum.Algorithm() == digest.SHA256, "ADD cache lookup with %s checksum", checksum.Algorithm())
	cachePath := Path(cacheDir, checksum)
	fi, err := os.Stat(cachePath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if fi.IsDir() {
		return "", false, fmt.Errorf("ADD cache entry %s is a directory", cachePath)
	}
	f, err := os.Open(cachePath)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	got, err := digest.Canonical.FromReader(f)
	if err != nil {
		return "", false, err
	}
	if got != checksum {
		return "", false, fmt.Errorf("cached digest mismatch for %s: got %s, want %s", cachePath, got, checksum)
	}
	return cachePath, true, nil
}

// Store fetches src into the cache entry for checksum and returns its path.
// The file is written to a temporary file and renamed so concurrent readers
// never observe a partial entry. When force is true, an existing entry is
// re-fetched and overwritten.
func Store(cacheDir, src string, checksum digest.Digest, force bool) (string, error) {
	// DownloadFileToDest only ever verifies sha256, so no other algorithm may be stored.
	assert.Assert("addcache.store.sha256", checksum.Algorithm() == digest.SHA256, "ADD cache store with %s checksum", checksum.Algorithm())
	if !force {
		cached, ok, err := Lookup(cacheDir, checksum)
		if err != nil {
			logrus.Warnf("Replacing ADD cache entry for %s: %v", src, err)
		} else if ok {
			return cached, nil
		}
	}

	cachePath := Path(cacheDir, checksum)
	err := os.MkdirAll(filepath.Dir(cachePath), 0o755)
	if err != nil {
		return "", fmt.Errorf("creating ADD cache directory: %w", err)
	}

	resp, err := http.Get(src) //nolint:noctx
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("invalid response status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(cachePath), ".add-")
	if err != nil {
		return "", fmt.Errorf("creating temporary ADD cache file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	digester := digest.Canonical.Digester()
	_, err = io.Copy(tmp, io.TeeReader(resp.Body, digester.Hash()))
	if err != nil {
		tmp.Close()
		return "", fmt.Errorf("writing ADD cache file: %w", err)
	}
	err = tmp.Close()
	if err != nil {
		return "", fmt.Errorf("closing ADD cache file: %w", err)
	}

	got := digester.Digest()
	if got != checksum {
		return "", fmt.Errorf("digest mismatch %s: %s", got, checksum)
	}

	err = os.Chmod(tmpName, 0o600)
	if err != nil {
		return "", fmt.Errorf("setting ADD cache file permissions: %w", err)
	}

	mTime := time.Time{}
	lastMod := resp.Header.Get("Last-Modified")
	if lastMod != "" {
		parsedMTime, err := http.ParseTime(lastMod)
		if err == nil {
			mTime = parsedMTime
		}
	}
	err = os.Chtimes(tmpName, mTime, mTime)
	if err != nil {
		return "", fmt.Errorf("setting ADD cache file times: %w", err)
	}

	err = os.Rename(tmpName, cachePath)
	if err != nil {
		return "", fmt.Errorf("storing ADD cache file: %w", err)
	}
	return cachePath, nil
}
