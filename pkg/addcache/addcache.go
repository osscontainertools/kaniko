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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
)

// DirName is the subdirectory of the cache directory that holds ADD sources.
const DirName = "add-cache"

// httpClient is a shared HTTP client for downloading ADD sources.
// Reusing a client allows connection pooling and proper resource management.
var httpClient = &http.Client{
	Transport: &http.Transport{
		// Proxy respects environment proxy settings ($HTTP_PROXY, $HTTPS_PROXY, $NO_PROXY).
		// Requests connect directly when no proxy applies.
		Proxy: http.ProxyFromEnvironment,
		// DialContext bounds connection establishment and TLS handshake to prevent indefinite blocking.
		DialContext: (&net.Dialer{
			Timeout: 30 * time.Second,
		}).DialContext,
		// ResponseHeaderTimeout bounds header waiting without limiting response body transfer.
		ResponseHeaderTimeout: 30 * time.Second,
	},
}

// Key returns the cache key for a remote ADD source. A checksum uniquely
// identifies the content, otherwise the normalized URL is hashed.
func Key(src string, checksum digest.Digest) string {
	if checksum != "" {
		return sanitize(checksum.Algorithm().String() + "-" + checksum.Encoded())
	}
	sum := sha256.Sum256([]byte(normalizeURL(src)))
	return hex.EncodeToString(sum[:])
}

// Path returns the cache path for a remote ADD source.
func Path(cacheDir, src string, checksum digest.Digest) string {
	return filepath.Join(cacheDir, DirName, Key(src, checksum))
}

// metadataPath returns the path to metadata file for a specific source URL.
// Metadata is stored per URL so identical content from different URLs retains each URL's timestamp.
func metadataPath(cacheDir, src string, checksum digest.Digest) string {
	// Use a separate metadata file per source URL, keyed by source URL's hash
	srcKey := sanitize("meta-" + Key(src, ""))
	return filepath.Join(cacheDir, DirName, Key(src, checksum)+"_"+srcKey+".meta")
}

// loadMetadata retrieves the Last-Modified timestamp for a specific source URL.
func loadMetadata(cacheDir, src string, checksum digest.Digest) (time.Time, error) {
	metaFile := metadataPath(cacheDir, src, checksum)
	data, err := os.ReadFile(metaFile)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	mTime, err := time.Parse(time.RFC3339, string(data))
	if err != nil {
		return time.Time{}, nil // Silently ignore parse errors, use zero time
	}
	return mTime, nil
}

// saveMetadata stores the Last-Modified timestamp for a specific source URL.
func saveMetadata(cacheDir, src string, checksum digest.Digest, mTime time.Time) error {
	metaFile := metadataPath(cacheDir, src, checksum)
	return os.WriteFile(metaFile, []byte(mTime.Format(time.RFC3339)), 0o600)
}

// Lookup returns the cached file for a remote ADD source. The second result is
// false when the cache has no entry. A checksum mismatch is reported as an
// error so callers can fall back to fetching the source again.
func Lookup(cacheDir, src string, checksum digest.Digest) (string, bool, error) {
	cachePath := Path(cacheDir, src, checksum)
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
	if checksum != "" {
		got, err := fileDigest(cachePath, checksum)
		if err != nil {
			return "", false, err
		}
		if got != checksum {
			return "", false, fmt.Errorf("cached digest mismatch for %s: got %s, want %s", cachePath, got, checksum)
		}
	}
	return cachePath, true, nil
}

// LookupWithMetadata returns the cached file for a remote ADD source along with its metadata.
// This allows the caller to apply the correct Last-Modified timestamp for the requested source URL.
func LookupWithMetadata(cacheDir, src string, checksum digest.Digest) (string, time.Time, bool, error) {
	cachePath, ok, err := Lookup(cacheDir, src, checksum)
	if !ok || err != nil {
		return "", time.Time{}, ok, err
	}
	mTime, err := loadMetadata(cacheDir, src, checksum)
	if err != nil {
		return "", time.Time{}, false, err
	}
	return cachePath, mTime, true, nil
}

// Store fetches a remote ADD source into the cache and returns its path. The
// file is written to a temporary file and renamed so concurrent readers never
// observe a partial entry. When force is true, existing cache entries are
// bypassed and the source is re-fetched and overwritten.
func Store(cacheDir, src string, checksum digest.Digest, force ...bool) (string, error) {
	forceRefresh := false
	if len(force) > 0 {
		forceRefresh = force[0]
	}

	if !forceRefresh {
		if cachePath, ok, err := Lookup(cacheDir, src, checksum); err == nil && ok {
			return cachePath, nil
		}
	}

	cachePath := Path(cacheDir, src, checksum)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", fmt.Errorf("creating ADD cache directory: %w", err)
	}

	resp, err := httpClient.Get(src)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("invalid response status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(cachePath), ".add-cache-")
	if err != nil {
		return "", fmt.Errorf("creating temporary ADD cache file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	body := io.Reader(resp.Body)
	var digester digest.Digester
	if checksum != "" {
		digester = checksum.Algorithm().Digester()
		if digester == nil {
			return "", fmt.Errorf("unsupported checksum algorithm %s", checksum.Algorithm())
		}
		body = io.TeeReader(resp.Body, digester.Hash())
	}

	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return "", fmt.Errorf("writing ADD cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("closing ADD cache file: %w", err)
	}

	if digester != nil {
		got := digester.Digest()
		if got != checksum {
			return "", fmt.Errorf("digest mismatch %s: %s", got, checksum)
		}
	}

	if err := os.Chmod(tmpName, 0o600); err != nil {
		return "", fmt.Errorf("setting ADD cache file permissions: %w", err)
	}

	mTime := time.Time{}
	if lastMod := resp.Header.Get("Last-Modified"); lastMod != "" {
		if parsedMTime, err := http.ParseTime(lastMod); err == nil {
			mTime = parsedMTime
		}
	}
	if err := os.Chtimes(tmpName, mTime, mTime); err != nil {
		return "", fmt.Errorf("setting ADD cache file times: %w", err)
	}

	if err := os.Rename(tmpName, cachePath); err != nil {
		return "", fmt.Errorf("storing ADD cache file: %w", err)
	}

	// Save metadata per source URL so identical content from different URLs retains each URL's timestamp
	if mTime != (time.Time{}) {
		if err := saveMetadata(cacheDir, src, checksum, mTime); err != nil {
			logrus.Debugf("Failed to save metadata for %s: %v", src, err)
		}
	}

	return cachePath, nil
}

func fileDigest(path string, checksum digest.Digest) (digest.Digest, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	digester := checksum.Algorithm().Digester()
	if digester == nil {
		return "", fmt.Errorf("unsupported checksum algorithm %s", checksum.Algorithm())
	}
	if _, err := io.Copy(digester.Hash(), f); err != nil {
		return "", err
	}
	return digester.Digest(), nil
}

func normalizeURL(rawurl string) string {
	rawurl = strings.TrimSpace(rawurl)
	u, err := url.Parse(rawurl)
	if err != nil {
		return rawurl
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	return u.String()
}

func sanitize(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, key)
}
