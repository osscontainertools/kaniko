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
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	schemaVersion = "kaniko-metrics/v1"
	maxMessageLen = 500
	cgroupRoot    = "/sys/fs/cgroup"
)

// cgroup v2 files sampled at start and at exit, best effort.
var cgroupFiles = []string{"cpu.stat", "cpu.max", "io.stat", "memory.current", "memory.peak", "memory.max", "memory.stat", "memory.events", "pids.peak"}

// Finish writes the CPU profile, the JSON report and the heap profile. Only
// the first call writes, so every exit path may call it; code is the exit code
// the process is about to return.
func Finish(err error, code int) {
	if !active.Load() {
		return
	}
	finishOnce.Do(func() { finish(err, code) })
}

func finish(err error, code int) {
	end := time.Now()
	if cpuProfile != nil {
		pprof.StopCPUProfile()
		writeFile(filepath.Join(pprofDir, "cpu.pprof"), cpuProfile.Bytes())
	}
	if enabled.Load() {
		data, merr := json.MarshalIndent(report(err, code, end), "", "  ")
		if merr != nil {
			warnf("encoding report: %v", merr)
		} else {
			writeFile(outPath, append(data, '\n'))
		}
	}
	if pprofDir != "" {
		// a heap profile is as of the last GC
		runtime.GC()
		var heap bytes.Buffer
		if herr := pprof.Lookup("heap").WriteTo(&heap, 0); herr != nil {
			warnf("heap profile: %v", herr)
		} else {
			writeFile(filepath.Join(pprofDir, "heap.pprof"), heap.Bytes())
		}
	}
}

func report(err error, code int, end time.Time) map[string]any {
	mu.Lock()
	fns := make(map[string]func() any, len(providers))
	for name, fn := range providers {
		fns[name] = fn
	}
	mu.Unlock()
	// providers read other packages' state, call them outside the lock
	attached := map[string]any{}
	for name, fn := range fns {
		attached[name] = fn()
	}
	endState := sampleState()

	mu.Lock()
	defer mu.Unlock()
	for _, list := range [][]*Record{snapshots, commands, stages} {
		for _, r := range list {
			// a failed build leaves its stage and command open
			r.endAt(end, true)
		}
	}
	slices.SortStableFunc(commands, func(a, b *Record) int {
		return cmp.Or(cmp.Compare(a.Build, b.Build), cmp.Compare(a.Stage, b.Stage), cmp.Compare(a.Index, b.Index))
	})

	phases := map[string]Totals{}
	for _, s := range stats {
		phases[s.name] = s.load()
	}

	exit := map[string]any{"status": "success", "code": code}
	if err != nil {
		exit["status"] = "error"
		exit["message"] = truncate(err.Error(), maxMessageLen)
	}

	m := map[string]any{
		"go_version": runtime.Version(),
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
		"num_cpu":    runtime.NumCPU(),
		"gomaxprocs": runtime.GOMAXPROCS(0),
		"pid":        os.Getpid(),
		"start":      startTime.Format(time.RFC3339Nano),
		"end":        end.Format(time.RFC3339Nano),
		"wall_ns":    int64(end.Sub(startTime)),
		"file":       outPath,
		"pprof_dir":  pprofDir,
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision", "vcs.time", "vcs.modified":
				m[strings.ReplaceAll(s.Key, ".", "_")] = s.Value
			}
		}
	}
	for k, v := range meta {
		m[k] = v
	}

	out := map[string]any{
		"schema":    schemaVersion,
		"meta":      m,
		"exit":      exit,
		"phases":    phases,
		"stages":    nonNil(stages),
		"commands":  nonNil(commands),
		"snapshots": nonNil(snapshots),
		"resources": resources(startState, endState),
	}
	for name, v := range attached {
		out[name] = v
	}
	return out
}

func nonNil(r []*Record) []*Record {
	if r == nil {
		return []*Record{}
	}
	return r
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}

// writeFile writes through a temporary file in the same directory, so a reader
// never sees a partial file.
func writeFile(path string, data []byte) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		warnf("creating %s: %v", dir, err)
		return
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		warnf("writing %s: %v", path, err)
		return
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, 0o644)
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		os.Remove(tmp)
		warnf("writing %s: %v", path, werr)
	}
}

// sampleState reads the cumulative counters that are worth a before/after
// pair: the cgroup and the network interfaces of the container.
func sampleState() map[string]any {
	dir, tried := cgroupDir()
	cg := map[string]any{"dir": dir, "tried": tried}
	if dir != "" {
		values := map[string]any{}
		errs := map[string]string{}
		for _, name := range cgroupFiles {
			v, err := readCgroupFile(filepath.Join(dir, name))
			if err != nil {
				errs[name] = err.Error()
				continue
			}
			values[name] = v
		}
		cg["values"] = values
		if len(errs) > 0 {
			cg["errors"] = errs
		}
	}
	state := map[string]any{"cgroup": cg}
	if nd, err := netDev(); err == nil {
		state["netdev"] = nd
	} else {
		state["netdev_error"] = err.Error()
	}
	return state
}

func resources(start, end map[string]any) map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return map[string]any{
		"rusage_self":     rusage(syscall.RUSAGE_SELF),
		"rusage_children": rusage(syscall.RUSAGE_CHILDREN),
		"proc_status":     procStatus(),
		"go_memstats": map[string]any{
			"heap_alloc":     ms.HeapAlloc,
			"heap_sys":       ms.HeapSys,
			"heap_inuse":     ms.HeapInuse,
			"total_alloc":    ms.TotalAlloc,
			"sys":            ms.Sys,
			"mallocs":        ms.Mallocs,
			"num_gc":         ms.NumGC,
			"pause_total_ns": ms.PauseTotalNs,
		},
		"start": start,
		"end":   end,
	}
}

func rusage(who int) map[string]any {
	var ru syscall.Rusage
	if err := syscall.Getrusage(who, &ru); err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{
		"user_ns":   time.Duration(ru.Utime.Nano()),
		"sys_ns":    time.Duration(ru.Stime.Nano()),
		"maxrss_kb": ru.Maxrss,
		"minflt":    ru.Minflt,
		"majflt":    ru.Majflt,
		"inblock":   ru.Inblock,
		"oublock":   ru.Oublock,
		"nvcsw":     ru.Nvcsw,
		"nivcsw":    ru.Nivcsw,
	}
}

// procStatus reads the peak and current resident set of this process.
func procStatus() map[string]any {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	out := map[string]any{}
	for _, line := range strings.Split(string(data), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch key {
		case "VmHWM", "VmRSS", "VmPeak", "VmSize", "Threads":
			f := strings.Fields(rest)
			if len(f) == 0 {
				continue
			}
			if n, err := strconv.ParseInt(f[0], 10, 64); err == nil {
				if len(f) > 1 && f[1] == "kB" {
					key += "_kb"
				}
				out[key] = n
			}
		}
	}
	return out
}

// cgroupDir finds this process's cgroup v2 directory: the path from
// /proc/self/cgroup when the whole hierarchy is mounted, else the mount root,
// which is the container's own cgroup under a cgroup namespace.
func cgroupDir() (string, []string) {
	var candidates []string
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if p, ok := strings.CutPrefix(line, "0::"); ok && p != "/" && p != "" {
				candidates = append(candidates, filepath.Join(cgroupRoot, p))
			}
		}
	}
	candidates = append(candidates, cgroupRoot)
	var tried []string
	for _, c := range candidates {
		tried = append(tried, c)
		// the root cgroup has no memory.current
		if _, err := os.Stat(filepath.Join(c, "memory.current")); err == nil {
			return c, tried
		}
	}
	return "", tried
}

// readCgroupFile parses a single value, a flat keyed file ("key value" per
// line) or a nested keyed file ("dev key=value ..." per line).
func readCgroupFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(data))
	if !strings.Contains(text, "\n") && !strings.Contains(text, "=") {
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return n, nil
		}
		return text, nil
	}
	flat := map[string]int64{}
	nested := map[string]map[string]int64{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if !strings.Contains(f[1], "=") {
			if n, err := strconv.ParseInt(f[1], 10, 64); err == nil {
				flat[f[0]] = n
			}
			continue
		}
		kv := map[string]int64{}
		for _, field := range f[1:] {
			k, v, _ := strings.Cut(field, "=")
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				kv[k] = n
			}
		}
		nested[f[0]] = kv
	}
	if len(nested) > 0 {
		return nested, nil
	}
	return flat, nil
}

// netDev reads the byte and packet counters of the network namespace's
// interfaces, which in a container counts all traffic of the build.
func netDev() (map[string]map[string]int64, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]int64{}
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 10 {
			continue
		}
		v := make([]int64, 10)
		for i := range v {
			v[i], _ = strconv.ParseInt(f[i], 10, 64)
		}
		out[strings.TrimSpace(name)] = map[string]int64{
			"rx_bytes":   v[0],
			"rx_packets": v[1],
			"tx_bytes":   v[8],
			"tx_packets": v[9],
		}
	}
	return out, nil
}
