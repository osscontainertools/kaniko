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

// Package metrics totals where a build spends its time and resources and
// writes them as one JSON file at exit, opt-in via KANIKO_METRICS_FILE.
// KANIKO_PPROF_DIR adds a CPU and a heap profile. With both unset every hook
// is a single atomic load and kaniko behaves exactly as without this package.
//
// Only aggregates are kept: per-file hooks add to atomic counters and never
// start a timer, so the cost stays flat however many files a snapshot walks.
// Per-command and per-snapshot figures are the difference of those counters
// between the start and the end of the record.
//
// The package imports only the standard library, so every other package can
// hook into it without an import cycle.
package metrics

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// FileEnv names the JSON report written at exit. Unset, metrics are off.
	FileEnv = "KANIKO_METRICS_FILE"
	// PprofDirEnv names the directory cpu.pprof and heap.pprof are written to at exit.
	PprofDirEnv = "KANIKO_PPROF_DIR"

	// maxTextLen bounds the command text kept per command record.
	maxTextLen = 120
)

var (
	enabled atomic.Bool // KANIKO_METRICS_FILE is set
	active  atomic.Bool // the report or a profile has to be written at exit

	mu         sync.Mutex
	startTime  time.Time
	outPath    string
	pprofDir   string
	cpuProfile *bytes.Buffer
	meta       = map[string]any{}
	providers  = map[string]func() any{}
	startState map[string]any
	build      = -1
	stages     []*Record
	commands   []*Record
	cmdByKey   = map[cmdKey]*Record{}
	snapshots  []*Record
	curStage   *Record
	curCmd     *Record
	finishOnce sync.Once
)

// Start turns collection on when KANIKO_METRICS_FILE or KANIKO_PPROF_DIR is
// set and does nothing otherwise. Call it once, first thing in main.
func Start() {
	start(os.Getenv(FileEnv), os.Getenv(PprofDirEnv))
}

func start(file, profDir string) {
	if file == "" && profDir == "" {
		return
	}
	startTime = time.Now()
	// absolute now, the executor changes its working directory to /
	if profDir != "" {
		if abs, err := filepath.Abs(profDir); err == nil {
			profDir = abs
		}
		pprofDir = profDir
		// buffered in memory, so no file exists in the build's rootfs before exit
		cpuProfile = &bytes.Buffer{}
		if err := pprof.StartCPUProfile(cpuProfile); err != nil {
			warnf("cpu profile not started: %v", err)
			cpuProfile = nil
		}
	}
	if file != "" {
		if abs, err := filepath.Abs(file); err == nil {
			file = abs
		}
		outPath = file
		startState = sampleState()
		enabled.Store(true)
	}
	active.Store(true)
}

// Enabled reports whether the JSON report is being collected.
func Enabled() bool {
	return enabled.Load()
}

// Active reports whether Finish has a report or a profile to write.
func Active() bool {
	return active.Load()
}

// Now returns the current time, or the zero time when metrics are off so that
// the matching Since does nothing.
func Now() time.Time {
	if !enabled.Load() {
		return time.Time{}
	}
	return time.Now()
}

// SetMeta adds a value to the report's meta section.
func SetMeta(key string, value any) {
	if !enabled.Load() {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	meta[key] = value
}

// Attach adds a top-level report section computed by fn at exit. It keeps
// packages this one must not import, such as connstats, out of its imports.
func Attach(name string, fn func() any) {
	if !enabled.Load() {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	providers[name] = fn
}

// Stat totals one phase across the whole run: how often it ran, for how long
// in sum and at most, and the bytes and items (files, entries, layers) it
// handled. Every field is atomic, so goroutines can share a Stat.
type Stat struct {
	name string
	// concurrent stats run beside the build, on goroutines of their own, so a
	// before/after difference on the build goroutine would misattribute them
	concurrent bool
	count      atomic.Int64
	totalNs    atomic.Int64
	maxNs      atomic.Int64
	bytes      atomic.Int64
	items      atomic.Int64
}

var stats []*Stat

func newStat(name string) *Stat {
	s := &Stat{name: name}
	stats = append(stats, s)
	return s
}

func newConcurrentStat(name string) *Stat {
	s := newStat(name)
	s.concurrent = true
	return s
}

// Since records one run that began at start. A zero start, from Now with
// metrics off, records nothing.
func (s *Stat) Since(start time.Time) time.Duration {
	return s.Observe(start, 0, 0)
}

// Observe records one run that began at start, with the bytes and items it handled.
func (s *Stat) Observe(start time.Time, bytes, items int64) time.Duration {
	if start.IsZero() {
		return 0
	}
	d := time.Since(start)
	s.count.Add(1)
	s.totalNs.Add(int64(d))
	for {
		peak := s.maxNs.Load()
		if int64(d) <= peak || s.maxNs.CompareAndSwap(peak, int64(d)) {
			break
		}
	}
	s.add(bytes, items)
	return d
}

// Add counts bytes and items without a timing. It is meant for per-file hooks,
// which must stay a couple of atomic adds.
func (s *Stat) Add(bytes, items int64) {
	if !enabled.Load() {
		return
	}
	s.add(bytes, items)
}

func (s *Stat) add(bytes, items int64) {
	if bytes != 0 {
		s.bytes.Add(bytes)
	}
	if items != 0 {
		s.items.Add(items)
	}
}

// Time runs fn and records it.
func (s *Stat) Time(fn func() error) error {
	start := Now()
	err := fn()
	s.Since(start)
	return err
}

// Totals is a Stat read out, or the part of it that fell inside a record.
type Totals struct {
	Count   int64 `json:"count"`
	TotalNs int64 `json:"total_ns"`
	MaxNs   int64 `json:"max_ns,omitempty"`
	Bytes   int64 `json:"bytes,omitempty"`
	Items   int64 `json:"items,omitempty"`
}

func (s *Stat) load() Totals {
	return Totals{
		Count:   s.count.Load(),
		TotalNs: s.totalNs.Load(),
		MaxNs:   s.maxNs.Load(),
		Bytes:   s.bytes.Load(),
		Items:   s.items.Load(),
	}
}

func loadAll() []Totals {
	out := make([]Totals, len(stats))
	for i, s := range stats {
		out[i] = s.load()
	}
	return out
}

// deltas returns what each non-concurrent stat gained since base. A maximum
// cannot be split that way, so it is left out.
func deltas(base []Totals) map[string]Totals {
	out := map[string]Totals{}
	for i, s := range stats {
		if s.concurrent || i >= len(base) {
			continue
		}
		now := s.load()
		d := Totals{
			Count:   now.Count - base[i].Count,
			TotalNs: now.TotalNs - base[i].TotalNs,
			Bytes:   now.Bytes - base[i].Bytes,
			Items:   now.Items - base[i].Items,
		}
		if d.Count != 0 || d.Bytes != 0 || d.Items != 0 {
			out[s.name] = d
		}
	}
	return out
}

type cmdKey struct {
	build, stage, index int
}

// Record is one stage, command or snapshot. Its phases are the stats gained
// while it was open; values hold what only it knows, such as the rusage of a
// RUN's child process.
type Record struct {
	Build      int               `json:"build"`
	Stage      int               `json:"stage"`
	Index      int               `json:"index"`
	Line       int               `json:"line,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Name       string            `json:"name,omitempty"`
	Text       string            `json:"text,omitempty"`
	CacheHit   bool              `json:"cache_hit,omitempty"`
	StartNs    int64             `json:"start_ns"`
	WallNs     int64             `json:"wall_ns"`
	Incomplete bool              `json:"incomplete,omitempty"`
	Values     map[string]int64  `json:"values,omitempty"`
	Phases     map[string]Totals `json:"phases,omitempty"`

	begun time.Time
	base  []Totals
	open  bool
}

// newRecord expects mu to be held.
func newRecord(stage, index int) *Record {
	return &Record{Build: build, Stage: stage, Index: index, Values: map[string]int64{}}
}

// begin expects mu to be held.
func (r *Record) begin(now time.Time) {
	r.begun = now
	r.StartNs = int64(now.Sub(startTime))
	r.base = loadAll()
	r.open = true
}

// endAt expects mu to be held.
func (r *Record) endAt(now time.Time, incomplete bool) {
	if !r.open {
		return
	}
	r.open = false
	r.WallNs = int64(now.Sub(r.begun))
	r.Incomplete = incomplete
	r.Phases = deltas(r.base)
	r.base = nil
	if child, ok := r.Values["child_wall_ns"]; ok {
		// what kaniko itself spent on a RUN, beside the process it ran
		r.Values["overhead_ns"] = r.WallNs - child
	}
	if curCmd == r {
		curCmd = nil
	}
	if curStage == r {
		curStage = nil
	}
}

// End closes the record. Later calls, and calls on a nil record, do nothing.
func (r *Record) End() {
	if r == nil {
		return
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	r.endAt(now, false)
}

// Since adds the time elapsed since start to the named value.
func (r *Record) Since(key string, start time.Time) {
	if r == nil || start.IsZero() {
		return
	}
	r.Add(key, int64(time.Since(start)))
}

// Add adds v to the named value.
func (r *Record) Add(key string, v int64) {
	if r == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	r.Values[key] += v
}

// Lookup charges one cache lookup that began at start to the record.
func (r *Record) Lookup(start time.Time, hit bool) {
	if r == nil || start.IsZero() {
		return
	}
	d := time.Since(start)
	mu.Lock()
	defer mu.Unlock()
	r.Values["lookup_ns"] += int64(d)
	r.Values["lookups"]++
	if hit {
		r.Values["lookup_hits"]++
	}
}

// BeginBuild starts the next build target. Records opened after it carry its
// ordinal, as bake builds several targets that reuse stage indexes.
func BeginBuild() {
	if !enabled.Load() {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	build++
}

// BeginStage opens the record of a stage, until End.
func BeginStage(index int, name, base string) *Record {
	if !enabled.Load() {
		return nil
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	r := newRecord(index, -1)
	r.Kind = "stage"
	r.Name = name
	r.Text = shorten(base)
	r.begin(now)
	stages = append(stages, r)
	curStage = r
	return r
}

// CommandAt returns the record of a command, creating it if needed but not
// opening it, so the cache probe ahead of the build can charge the command.
func CommandAt(stage, index int, text string) *Record {
	if !enabled.Load() {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	r := commandAt(stage, index)
	if r.Text == "" {
		r.Text = shorten(text)
		r.Kind = kindOf(text)
	}
	return r
}

func commandAt(stage, index int) *Record {
	k := cmdKey{build, stage, index}
	r := cmdByKey[k]
	if r == nil {
		r = newRecord(stage, index)
		cmdByKey[k] = r
		commands = append(commands, r)
	}
	return r
}

// BeginCommand opens the record of a command, until End. It becomes the
// command that child processes and snapshots are charged to.
func BeginCommand(stage, index, line int, text string, cacheHit bool) *Record {
	if !enabled.Load() {
		return nil
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	r := commandAt(stage, index)
	r.Line = line
	r.Text = shorten(text)
	r.Kind = kindOf(text)
	r.CacheHit = cacheHit
	r.begin(now)
	curCmd = r
	return r
}

var snapshotStats = map[string]*Stat{
	"init":    SnapshotInit,
	"command": SnapshotCommand,
	"context": SnapshotContext,
}

// BeginSnapshot opens the record of a snapshot of the given kind (init,
// command or context), charged to the current stage and command.
func BeginSnapshot(kind string) *Record {
	if !enabled.Load() {
		return nil
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	stage, index := -1, -1
	if curStage != nil {
		stage = curStage.Stage
	}
	if curCmd != nil {
		stage, index = curCmd.Stage, curCmd.Index
	}
	r := newRecord(stage, index)
	r.Kind = kind
	r.begin(now)
	snapshots = append(snapshots, r)
	return r
}

// EndSnapshot closes a snapshot record with the size of the tar it wrote and
// the number of paths it added and whited out.
func (r *Record) EndSnapshot(tarPath string, added, whiteouts int) {
	if r == nil {
		return
	}
	var size int64
	if tarPath != "" {
		if fi, err := os.Stat(tarPath); err == nil {
			size = fi.Size()
		}
	}
	if s := snapshotStats[r.Kind]; s != nil {
		s.Observe(r.begun, size, int64(added+whiteouts))
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	r.Values["tar_bytes"] = size
	r.Values["files_added"] = int64(added)
	r.Values["whiteouts"] = int64(whiteouts)
	r.endAt(now, false)
}

// ObserveChild charges a child process that was started at start and has been
// waited for to the current command. ps may be nil if the wait failed early.
func ObserveChild(start time.Time, ps *os.ProcessState) {
	d := RunChild.Since(start)
	if d == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	r := curCmd
	if r == nil {
		return
	}
	r.Values["child_count"]++
	r.Values["child_wall_ns"] += int64(d)
	if ps == nil {
		return
	}
	r.Values["child_user_ns"] += int64(ps.UserTime())
	r.Values["child_sys_ns"] += int64(ps.SystemTime())
	r.Values["child_exit_code"] = int64(ps.ExitCode())
	if ru, ok := ps.SysUsage().(*syscall.Rusage); ok {
		// kilobytes on Linux
		r.Values["child_maxrss_kb"] = max(r.Values["child_maxrss_kb"], int64(ru.Maxrss))
		r.Values["child_inblock"] += int64(ru.Inblock)
		r.Values["child_oublock"] += int64(ru.Oublock)
		r.Values["child_majflt"] += int64(ru.Majflt)
		r.Values["child_nvcsw"] += int64(ru.Nvcsw)
		r.Values["child_nivcsw"] += int64(ru.Nivcsw)
	}
}

// shorten collapses whitespace, heredocs included, and caps the length.
func shorten(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) <= maxTextLen {
		return text
	}
	return string(r[:maxTextLen-3]) + "..."
}

func kindOf(text string) string {
	f := strings.Fields(text)
	if len(f) == 0 {
		return ""
	}
	return strings.ToUpper(f[0])
}

func warnf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "kaniko metrics: "+format+"\n", args...)
}
