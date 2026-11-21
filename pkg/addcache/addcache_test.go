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

package addcache

import (
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
)

func digestOf(data []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data))
}

func TestKeyWithChecksum(t *testing.T) {
	checksum := "sha256:" + strings.Repeat("a", 64)
	if got, want := Key("https://example.com/file", digest.Digest(checksum)), "sha256-"+strings.Repeat("a", 64); got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
}

func TestKeyWithoutChecksumIsStable(t *testing.T) {
	first := Key(" HTTPS://Example.COM/a?b=c#fragment ", "")
	second := Key("https://example.com/a?b=c", "")
	if first != second {
		t.Fatalf("Key() = %q and %q, want the same normalized key", first, second)
	}
	if first == Key("https://example.com/other", "") {
		t.Fatal("different URLs produced the same key")
	}
}

func TestStoreAndLookup(t *testing.T) {
	body := []byte("cached content")
	checksum := digest.Digest(digestOf(body))
	var gets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gets++
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		_, _ = w.Write(body)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	path, err := Store(cacheDir, server.URL, checksum)
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(body) {
		t.Fatalf("cached content = %q, %v; want %q", got, err, body)
	}

	lookedUp, ok, err := Lookup(cacheDir, server.URL, checksum)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if !ok || lookedUp != path {
		t.Fatalf("Lookup() = %q, %v; want %q, true", lookedUp, ok, path)
	}

	if _, err := Store(cacheDir, server.URL, checksum); err != nil {
		t.Fatalf("second Store() error = %v", err)
	}
	if gets != 1 {
		t.Fatalf("server received %d requests, want 1", gets)
	}
}

func TestStoreChecksumMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("actual"))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	wrong := digest.Digest(digestOf([]byte("expected")))
	if _, err := Store(cacheDir, server.URL, wrong); err == nil {
		t.Fatal("Store() expected a digest mismatch error")
	}
	if _, ok, err := Lookup(cacheDir, server.URL, wrong); err != nil || ok {
		t.Fatalf("Lookup() = %v, %v; want false, nil", ok, err)
	}
}

func TestStoreSha512Checksum(t *testing.T) {
	body := []byte("sha512 content")
	checksum := digest.Digest(fmt.Sprintf("sha512:%x", sha512.Sum512(body)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	path, err := Store(cacheDir, server.URL, checksum)
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(body) {
		t.Fatalf("cached content = %q, %v; want %q", got, err, body)
	}
}

func TestLookupMiss(t *testing.T) {
	path, ok, err := Lookup(t.TempDir(), "https://example.com/missing", "")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if ok || path != "" {
		t.Fatalf("Lookup() = %q, %v; want empty, false", path, ok)
	}
	if filepath.Dir(Path("cache", "https://example.com/missing", "")) != filepath.Join("cache", DirName) {
		t.Fatal("Path() did not place the entry under the add-cache directory")
	}
}
