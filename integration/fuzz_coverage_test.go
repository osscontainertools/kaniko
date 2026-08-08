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

package integration

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// coverageTracker accumulates the set of executor code blocks reached across a
// campaign. It reads the GOCOVERDIR data the -cover executor writes per build,
// which is the same data CI feeds to `go tool covdata`. An input that reaches a
// block not seen before is admitted to the corpus, steering the search into
// unexercised executor code. It is inert unless the run collects coverage, which
// requires `go test -coverage-dir=...` so the executor image is built with -cover.
type coverageTracker struct {
	enabled bool
	mu      sync.Mutex // guards blocks against concurrent workers
	blocks  map[string]bool
}

func newCoverageTracker() *coverageTracker {
	return &coverageTracker{enabled: coverageDir != "", blocks: map[string]bool{}}
}

// observe reads the coverage a single build wrote into caseDir and returns the
// count of blocks reached for the first time this campaign.
func (c *coverageTracker) observe(caseDir string) (int, error) {
	if !c.enabled {
		return 0, nil
	}
	profile := filepath.Join(caseDir, "profile.txt")
	cmd := exec.Command("go", "tool", "covdata", "textfmt", "-i="+caseDir, "-o="+profile)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("covdata textfmt: %w: %s", err, out)
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fresh := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		// Coverage profile rows are "path.go:l.c,l.c numstmt count"; the first
		// field identifies the block and the last is its execution count.
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[2] == "0" {
			continue
		}
		if !c.blocks[fields[0]] {
			c.blocks[fields[0]] = true
			fresh++
		}
	}
	return fresh, nil
}

func (c *coverageTracker) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.blocks)
}

// seedBytes produces the deterministic byte stream for a bare seed.
func seedBytes(seed int64) []byte {
	buf := make([]byte, 256)
	rand.New(rand.NewSource(seed)).Read(buf)
	return buf
}

// mutateInput derives a new input from an admitted parent by flipping a few
// bytes. It is deterministic in seed so a finding stays reproducible.
func mutateInput(parent []byte, seed int64) []byte {
	out := make([]byte, len(parent))
	copy(out, parent)
	r := rand.New(rand.NewSource(seed))
	flips := 1 + r.Intn(8)
	for k := 0; k < flips; k++ {
		out[r.Intn(len(out))] = byte(r.Intn(256))
	}
	return out
}
