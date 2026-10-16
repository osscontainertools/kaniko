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

package snapshot

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/metrics"
	"github.com/osscontainertools/kaniko/pkg/util"
)

// TestSnapshotUnchangedByMetrics takes the same snapshots with metrics off and
// on and expects identical tars. It runs before the other tests of the
// package, which then run with metrics on as well.
func TestSnapshotUnchangedByMetrics(t *testing.T) {
	if metrics.Enabled() {
		t.Skip("metrics already on in this test binary")
	}
	testDir, err := setUpTestDir(t)
	if err != nil {
		t.Fatal(err)
	}
	snapshotPathPrefix = t.TempDir()
	original := config.KanikoLayersDir
	config.KanikoLayersDir = t.TempDir()
	t.Cleanup(func() { config.KanikoLayersDir = original })
	// made up front, a new temp dir would change the mtime of a parent in the tar
	report := filepath.Join(t.TempDir(), "metrics.json")

	take := func() ([]byte, []byte) {
		// no initial snapshot, so the full scan finds every file changed
		full, _, _, err := NewSnapshotter(NewLayeredMap(util.Hasher()), testDir).TakeSnapshotFS()
		if err != nil {
			t.Fatal(err)
		}
		files := []string{filepath.Join(testDir, "foo"), filepath.Join(testDir, "bar/bat")}
		listed, _, _, err := NewSnapshotter(NewLayeredMap(util.Hasher()), testDir).TakeSnapshot(files, true)
		if err != nil {
			t.Fatal(err)
		}
		return readFile(t, full), readFile(t, listed)
	}

	// the tars carry PAX atimes, so let the first reads settle them
	take()
	offFull, offListed := take()
	t.Setenv(metrics.FileEnv, report)
	metrics.Start()
	if !metrics.Enabled() {
		t.Fatal("metrics did not turn on")
	}
	onFull, onListed := take()

	if !bytes.Equal(offFull, onFull) {
		t.Errorf("full snapshot tar differs with metrics on:\n%s", diffTars(t, offFull, onFull))
	}
	if !bytes.Equal(offListed, onListed) {
		t.Errorf("listed snapshot tar differs with metrics on:\n%s", diffTars(t, offListed, onListed))
	}

	metrics.Finish(nil, 0)
	var out struct {
		Phases map[string]metrics.Totals `json:"phases"`
	}
	if err := json.Unmarshal(readFile(t, report), &out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"snapshot.scan_full", "snapshot.syncfs", "snapshot.walk", "snapshot.resolve", "snapshot.add", "snapshot.tar"} {
		if out.Phases[name].Count == 0 {
			t.Errorf("%s not recorded: %+v", name, out.Phases[name])
		}
	}
	if hash := out.Phases["snapshot.hash"]; hash.Items == 0 || hash.Bytes == 0 {
		t.Errorf("snapshot.hash not counted: %+v", hash)
	}
}

// diffTars lists the entries whose header or content differs.
func diffTars(t *testing.T, a, b []byte) string {
	t.Helper()
	entries := func(data []byte) []string {
		var out []string
		tr := tar.NewReader(bytes.NewReader(data))
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				return out
			}
			if err != nil {
				t.Fatal(err)
			}
			content, _ := io.ReadAll(tr)
			out = append(out, fmt.Sprintf("%s mode=%o size=%d mtime=%v atime=%v ctime=%v pax=%v content=%x",
				hdr.Name, hdr.Mode, hdr.Size, hdr.ModTime.UnixNano(), hdr.AccessTime.UnixNano(), hdr.ChangeTime.UnixNano(), hdr.PAXRecords, sha256.Sum256(content)))
		}
	}
	ea, eb := entries(a), entries(b)
	var diff strings.Builder
	for i := range max(len(ea), len(eb)) {
		var x, y string
		if i < len(ea) {
			x = ea[i]
		}
		if i < len(eb) {
			y = eb[i]
		}
		if x != y {
			fmt.Fprintf(&diff, "- %s\n+ %s\n", x, y)
		}
	}
	return diff.String()
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
