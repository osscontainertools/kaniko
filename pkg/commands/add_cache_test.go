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

package commands

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
)

func TestDownloadRemoteSourcePopulatesAndUsesCache(t *testing.T) {
	body := []byte("remote ADD content")
	checksum := digest.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(body)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))

	cacheDir := t.TempDir()
	a := &AddCommand{addCache: AddSourceCache{Enabled: true, Dir: cacheDir}}

	first := filepath.Join(t.TempDir(), "first")
	if err := a.downloadRemoteSource(server.URL, first, nil, 0o600, checksum); err != nil {
		t.Fatalf("downloadRemoteSource() error = %v", err)
	}
	if got, err := os.ReadFile(first); err != nil || string(got) != string(body) {
		t.Fatalf("downloaded content = %q, %v; want %q", got, err, body)
	}

	server.Close()

	second := filepath.Join(t.TempDir(), "second")
	if err := a.downloadRemoteSource(server.URL, second, nil, 0o600, checksum); err != nil {
		t.Fatalf("downloadRemoteSource() from cache error = %v", err)
	}
	if got, err := os.ReadFile(second); err != nil || string(got) != string(body) {
		t.Fatalf("cached content = %q, %v; want %q", got, err, body)
	}
}
