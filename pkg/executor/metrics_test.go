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

package executor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/osscontainertools/kaniko/pkg/config"
	"github.com/osscontainertools/kaniko/pkg/constants"
	"github.com/osscontainertools/kaniko/pkg/metrics"
	"github.com/osscontainertools/kaniko/testutil"
)

// TestDoBuildWithMetrics runs a multi-stage build with metrics on, offline on
// scratch, and checks the report has the stages, commands and snapshots of it.
// Metrics stay on for the tests of the package that run after this one.
func TestDoBuildWithMetrics(t *testing.T) {
	if metrics.Enabled() {
		t.Skip("metrics already on in this test binary")
	}
	testDir, fn := setupMultistageTests(t)
	defer fn()
	report := filepath.Join(t.TempDir(), "metrics.json")
	t.Setenv(metrics.FileEnv, report)
	metrics.Start()

	ran := filepath.Join(testDir, "workspace", "ran.txt")
	dockerFile := fmt.Sprintf(`
FROM scratch as first
COPY foo/bam.txt copied/
ENV test test
RUN echo hi > %s

FROM scratch as second
COPY --from=first copied/bam.txt output/bam.txt`, ran)
	if err := os.WriteFile(filepath.Join(testDir, "workspace", "Dockerfile"), []byte(dockerFile), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := &config.KanikoOptions{
		DockerfilePath: filepath.Join(testDir, "workspace", "Dockerfile"),
		SrcContext:     filepath.Join(testDir, "workspace"),
		SnapshotMode:   constants.SnapshotModeFull,
	}
	_, err := DoBuild(opts)
	testutil.CheckNoError(t, err)
	metrics.Finish(nil, 0)

	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Exit      map[string]any            `json:"exit"`
		Phases    map[string]metrics.Totals `json:"phases"`
		Stages    []metrics.Record          `json:"stages"`
		Commands  []metrics.Record          `json:"commands"`
		Snapshots []metrics.Record          `json:"snapshots"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("report is not JSON: %v", err)
	}
	if out.Exit["status"] != "success" {
		t.Errorf("exit = %v", out.Exit)
	}
	if len(out.Stages) != 2 {
		t.Errorf("want 2 stages, got %d", len(out.Stages))
	}
	for _, r := range append(append(append([]metrics.Record{}, out.Stages...), out.Commands...), out.Snapshots...) {
		if r.Incomplete {
			t.Errorf("record left open: %+v", r)
		}
	}
	kinds := map[string]metrics.Record{}
	for _, c := range out.Commands {
		kinds[fmt.Sprintf("%d/%s", c.Stage, c.Kind)] = c
	}
	for _, key := range []string{"0/COPY", "0/RUN", "1/COPY"} {
		c, ok := kinds[key]
		if !ok {
			t.Errorf("command %s missing from %v", key, out.Commands)
			continue
		}
		if c.WallNs <= 0 || c.Phases["command.exec"].Count != 1 {
			t.Errorf("command %s not timed: %+v", key, c)
		}
		if c.Phases["snapshot.command"].Count != 1 || c.Phases["command.layer"].Count != 1 {
			t.Errorf("command %s has no snapshot or layer: %+v", key, c.Phases)
		}
	}
	if run := kinds["0/RUN"]; run.Values["child_count"] != 1 || run.Values["child_exit_code"] != 0 {
		t.Errorf("RUN child not recorded: %+v", run.Values)
	} else if _, ok := run.Values["overhead_ns"]; !ok {
		t.Errorf("RUN overhead missing: %+v", run.Values)
	}
	if out.Phases["layer.compress"].Count < 1 || out.Phases["snapshot.init"].Count < 1 || out.Phases["snapshot.hash"].Items < 1 {
		t.Errorf("phases = %+v", out.Phases)
	}
	var kindsSeen []string
	for _, s := range out.Snapshots {
		kindsSeen = append(kindsSeen, s.Kind)
	}
	testutil.CheckDeepEqual(t, true, len(out.Snapshots) >= 3)
	t.Logf("snapshots: %v", kindsSeen)
}
