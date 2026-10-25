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

package metrics

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"
)

// reset puts the package back into its state before Start.
func reset() {
	if cpuProfile != nil {
		pprof.StopCPUProfile()
	}
	enabled.Store(false)
	active.Store(false)
	startTime = time.Time{}
	outPath, pprofDir, cpuProfile = "", "", nil
	meta = map[string]any{}
	providers = map[string]func() any{}
	startState = nil
	build = -1
	stages, commands, snapshots = nil, nil, nil
	cmdByKey = map[cmdKey]*Record{}
	curStage, curCmd = nil, nil
	finishOnce = sync.Once{}
	for _, s := range stats {
		s.count.Store(0)
		s.totalNs.Store(0)
		s.maxNs.Store(0)
		s.bytes.Store(0)
		s.items.Store(0)
	}
}

func startForTest(t *testing.T, file, profDir string) {
	t.Helper()
	reset()
	t.Cleanup(reset)
	start(file, profDir)
}

func readReport(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading report: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, data)
	}
	return out
}

func TestUnsetIsNoop(t *testing.T) {
	reset()
	t.Cleanup(reset)
	t.Setenv(FileEnv, "")
	t.Setenv(PprofDirEnv, "")
	goroutines := runtime.NumGoroutine()
	dir := t.TempDir()
	t.Chdir(dir)

	Start()
	if Enabled() || Active() {
		t.Fatal("metrics must stay off without KANIKO_METRICS_FILE and KANIKO_PPROF_DIR")
	}
	if !Now().IsZero() {
		t.Fatal("Now must return the zero time when off")
	}
	SnapshotWalk.Since(Now())
	SnapshotHash.Add(100, 1)
	BeginBuild()
	r := BeginCommand(0, 0, 1, "RUN true", false)
	if r != nil {
		t.Fatal("BeginCommand must return nil when off")
	}
	// every record method is safe on the nil record
	r.Since("x", time.Now())
	r.Add("x", 1)
	r.Lookup(time.Now(), true)
	r.End()
	BeginSnapshot("command").EndSnapshot("", 1, 1)
	BeginStage(0, "", "scratch").End()
	CommandAt(0, 0, "RUN true").End()
	ObserveChild(Now(), nil)
	SetMeta("k", "v")
	Attach("network", func() any { return nil })
	Finish(errors.New("boom"), 1)

	if got := SnapshotWalk.load(); got != (Totals{}) {
		t.Errorf("stat changed while off: %+v", got)
	}
	if got := SnapshotHash.load(); got != (Totals{}) {
		t.Errorf("stat changed while off: %+v", got)
	}
	if len(commands) != 0 || len(snapshots) != 0 || len(stages) != 0 || len(meta) != 0 || len(providers) != 0 {
		t.Error("records kept while off")
	}
	if n := runtime.NumGoroutine(); n > goroutines {
		t.Errorf("goroutines grew from %d to %d while off", goroutines, n)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("files written while off: %v", entries)
	}
}

func TestStatObserve(t *testing.T) {
	startForTest(t, filepath.Join(t.TempDir(), "m.json"), "")
	s := newStat("test.observe")
	t.Cleanup(func() { stats = stats[:len(stats)-1] })

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Observe(time.Now().Add(-time.Duration(i+1)*time.Millisecond), 10, 1)
			s.Add(5, 2)
		}()
	}
	wg.Wait()
	got := s.load()
	if got.Count != 8 || got.Bytes != 8*15 || got.Items != 8*3 {
		t.Errorf("unexpected totals %+v", got)
	}
	if got.MaxNs < int64(8*time.Millisecond) || got.TotalNs < int64(36*time.Millisecond) {
		t.Errorf("unexpected timings %+v", got)
	}
	if s.Since(time.Time{}) != 0 || s.load().Count != 8 {
		t.Error("a zero start must not record")
	}
}

func TestReport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out", "metrics.json")
	startForTest(t, path, "")
	SetMeta("kaniko_version", "test")
	Attach("network", func() any { return map[string]int{"requests": 3} })

	BeginBuild()
	stage := BeginStage(0, "builder", "busybox")
	CommandAt(0, 0, "RUN true").Lookup(time.Now().Add(-time.Millisecond), false)
	cmd := BeginCommand(0, 0, 3, "RUN   sh -c \n 'exit 3'", false)
	child := Now()
	c := exec.Command("sh", "-c", "exit 3")
	if err := c.Run(); err == nil {
		t.Fatal("expected the child to fail")
	}
	ObserveChild(child, c.ProcessState)

	tarPath := filepath.Join(dir, "layer.tar")
	if err := os.WriteFile(tarPath, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := BeginSnapshot("command")
	SnapshotHash.Add(4096, 2)
	SnapshotWalk.Observe(Now(), 0, 7)
	snap.EndSnapshot(tarPath, 2, 1)
	cmd.End()
	cmd.End() // a second End keeps the first
	stage.End()

	Finish(nil, 0)

	out := readReport(t, path)
	if out["schema"] != schemaVersion {
		t.Errorf("schema = %v", out["schema"])
	}
	if exit := out["exit"].(map[string]any); exit["status"] != "success" || exit["code"].(float64) != 0 {
		t.Errorf("exit = %v", exit)
	}
	m := out["meta"].(map[string]any)
	if m["kaniko_version"] != "test" || m["goarch"] != runtime.GOARCH {
		t.Errorf("meta = %v", m)
	}
	if out["network"].(map[string]any)["requests"].(float64) != 3 {
		t.Errorf("network = %v", out["network"])
	}

	cmds := out["commands"].([]any)
	if len(cmds) != 1 {
		t.Fatalf("commands = %v", cmds)
	}
	rec := cmds[0].(map[string]any)
	if rec["kind"] != "RUN" || rec["text"] != "RUN sh -c 'exit 3'" || rec["line"].(float64) != 3 {
		t.Errorf("command = %v", rec)
	}
	values := rec["values"].(map[string]any)
	if values["child_count"].(float64) != 1 || values["child_exit_code"].(float64) != 3 || values["lookups"].(float64) != 1 {
		t.Errorf("values = %v", values)
	}
	if _, ok := values["overhead_ns"]; !ok {
		t.Errorf("overhead_ns missing: %v", values)
	}
	phases := rec["phases"].(map[string]any)
	hash := phases["snapshot.hash"].(map[string]any)
	if hash["bytes"].(float64) != 4096 || hash["items"].(float64) != 2 {
		t.Errorf("command phases = %v", phases)
	}
	if _, ok := phases["run.child"]; !ok {
		t.Errorf("run.child missing from command phases: %v", phases)
	}

	snaps := out["snapshots"].([]any)
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %v", snaps)
	}
	sv := snaps[0].(map[string]any)["values"].(map[string]any)
	if sv["tar_bytes"].(float64) != 2048 || sv["files_added"].(float64) != 2 || sv["whiteouts"].(float64) != 1 {
		t.Errorf("snapshot values = %v", sv)
	}
	total := out["phases"].(map[string]any)["snapshot.command"].(map[string]any)
	if total["count"].(float64) != 1 || total["bytes"].(float64) != 2048 || total["items"].(float64) != 3 {
		t.Errorf("snapshot.command = %v", total)
	}
	if stages := out["stages"].([]any); len(stages) != 1 || stages[0].(map[string]any)["name"] != "builder" {
		t.Errorf("stages = %v", stages)
	}
	res := out["resources"].(map[string]any)
	for _, k := range []string{"rusage_self", "rusage_children", "go_memstats", "start", "end"} {
		if _, ok := res[k]; !ok {
			t.Errorf("resources.%s missing", k)
		}
	}

	// only the report is left, the temporary file was renamed
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "metrics.json" {
		t.Errorf("directory holds %v", entries)
	}
}

func TestReportOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	startForTest(t, path, "")
	BeginBuild()
	BeginStage(1, "", "scratch")
	BeginCommand(1, 2, 7, "COPY . /src", false)

	Finish(errors.New("error building image: "+strings.Repeat("x", 1000)), 3)
	// only the first Finish writes
	Finish(nil, 0)

	out := readReport(t, path)
	exit := out["exit"].(map[string]any)
	if exit["status"] != "error" || exit["code"].(float64) != 3 {
		t.Errorf("exit = %v", exit)
	}
	if msg := exit["message"].(string); len(msg) != maxMessageLen || !strings.HasPrefix(msg, "error building image") {
		t.Errorf("message not truncated: %d chars", len(msg))
	}
	rec := out["commands"].([]any)[0].(map[string]any)
	if rec["incomplete"] != true || rec["wall_ns"].(float64) <= 0 {
		t.Errorf("open command not closed as incomplete: %v", rec)
	}
	if st := out["stages"].([]any)[0].(map[string]any); st["incomplete"] != true {
		t.Errorf("open stage not closed as incomplete: %v", st)
	}
}

func TestPprof(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pprof")
	startForTest(t, "", dir)
	if Enabled() || !Active() {
		t.Fatal("KANIKO_PPROF_DIR alone must profile without collecting the report")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("nothing may be written before Finish")
	}
	Finish(nil, 0)
	for _, name := range []string{"cpu.pprof", "heap.pprof"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || fi.Size() == 0 {
			t.Errorf("%s not written: %v", name, err)
		}
	}
}

func TestShorten(t *testing.T) {
	long := "RUN " + strings.Repeat("a", 200)
	if got := shorten(long); len(got) != maxTextLen || !strings.HasSuffix(got, "...") {
		t.Errorf("shorten(long) = %q", got)
	}
	if got := shorten("RUN <<EOF\necho hi\nEOF"); got != "RUN <<EOF echo hi EOF" {
		t.Errorf("shorten(heredoc) = %q", got)
	}
	if got := kindOf("copy --from=0 / /"); got != "COPY" {
		t.Errorf("kindOf = %q", got)
	}
}

func TestReadCgroupFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if v, err := readCgroupFile(write("memory.peak", "1234\n")); err != nil || v != int64(1234) {
		t.Errorf("single = %v, %v", v, err)
	}
	if v, err := readCgroupFile(write("memory.max", "max\n")); err != nil || v != "max" {
		t.Errorf("max = %v, %v", v, err)
	}
	v, err := readCgroupFile(write("cpu.stat", "usage_usec 10\nuser_usec 7\nsystem_usec 3\n"))
	if flat, ok := v.(map[string]int64); err != nil || !ok || flat["user_usec"] != 7 {
		t.Errorf("flat = %v, %v", v, err)
	}
	v, err = readCgroupFile(write("io.stat", "8:0 rbytes=100 wbytes=200 rios=1 wios=2\n"))
	if nested, ok := v.(map[string]map[string]int64); err != nil || !ok || nested["8:0"]["wbytes"] != 200 {
		t.Errorf("nested = %v, %v", v, err)
	}
}
