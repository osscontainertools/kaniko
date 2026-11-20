/*
Copyright 2026 Google LLC

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
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
)

func writeDockerfile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Dockerfile")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseAddSources(t *testing.T) {
	checksum := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("archive")))
	dockerfile := fmt.Sprintf(`FROM scratch
ARG URL=https://example.com/archive.tar.gz
ADD --checksum=%s $URL /tmp/
ADD local.txt /tmp/
`, checksum)

	opts := &config.WarmerOptions{DockerfilePath: writeDockerfile(t, dockerfile), AddCache: true}
	sources, err := ParseAddSources(opts)
	if err != nil {
		t.Fatalf("ParseAddSources() error = %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 ADD source, got %d: %+v", len(sources), sources)
	}
	if sources[0].Source != "https://example.com/archive.tar.gz" {
		t.Fatalf("source = %q, want %q", sources[0].Source, "https://example.com/archive.tar.gz")
	}
	if sources[0].Checksum.String() != checksum {
		t.Fatalf("checksum = %q, want %q", sources[0].Checksum, checksum)
	}

	sources, err = ParseAddSources(&config.WarmerOptions{DockerfilePath: opts.DockerfilePath})
	if err != nil {
		t.Fatalf("ParseAddSources() second error = %v", err)
	}
	if len(sources) != 1 || sources[0].Checksum.String() != checksum {
		t.Fatalf("sources = %+v, want one source with checksum", sources)
	}
}

func TestParseAddSourcesResolvesEnv(t *testing.T) {
	dockerfile := `FROM scratch
ENV BASE=https://example.com FILE=archive.tar.gz
ADD $BASE/$FILE /tmp/
`
	sources, err := ParseAddSources(&config.WarmerOptions{DockerfilePath: writeDockerfile(t, dockerfile)})
	if err != nil {
		t.Fatalf("ParseAddSources() error = %v", err)
	}
	if len(sources) != 1 || sources[0].Source != "https://example.com/archive.tar.gz" {
		t.Fatalf("sources = %+v, want the resolved remote URL", sources)
	}
}

func TestParseAddSourcesDeduplicates(t *testing.T) {
	dockerfile := `FROM scratch
ADD https://example.com/archive.tar.gz /one/
ADD https://example.com/archive.tar.gz /two/
`
	sources, err := ParseAddSources(&config.WarmerOptions{DockerfilePath: writeDockerfile(t, dockerfile)})
	if err != nil {
		t.Fatalf("ParseAddSources() error = %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 deduplicated source, got %d", len(sources))
	}
}

func TestParseAddSourcesRequireChecksum(t *testing.T) {
	dockerfile := `FROM scratch
ADD https://example.com/archive.tar.gz /tmp/
`
	_, err := ParseAddSources(&config.WarmerOptions{
		DockerfilePath:     writeDockerfile(t, dockerfile),
		AddCache:           true,
		RequireAddChecksum: true,
	})
	if err == nil || !strings.Contains(err.Error(), "--require-add-checksum") {
		t.Fatalf("ParseAddSources() error = %v, want a --require-add-checksum error", err)
	}
}
