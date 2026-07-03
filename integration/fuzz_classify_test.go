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

import "strings"

// knownDivergence recognizes one class of docker-versus-kaniko diff that is
// already understood. A diff row that matches a known class is counted, not
// reported. A row that matches nothing is novel and is the output we care about.
// This is the shape-matcher generalization of the per-file diffArgsMap in images.go.
type knownDivergence struct {
	name  string
	why   string
	flag  string // the FF_KANIKO_* that switches kaniko to buildkit behaviour, or "" if none yet
	match func(row string) bool
}

// dockerKnownDivergences are the classes observed on the docker oracle. The cache
// oracle passes no allowlist, so any cache diff is treated as novel.
var dockerKnownDivergences = []knownDivergence{
	{
		name: "history-metadata-instructions",
		why:  "kaniko omits history rows for metadata-only instructions (ENV, LABEL, ...) that docker records with empty_layer=true, and formats created_by differently (\"RUN mkdir\" vs buildkit's \"RUN /bin/sh -c mkdir # buildkit\"), so the history arrays differ in length and content",
		flag: "",
		match: func(row string) bool {
			// Both the array-level "History: length mismatch" row and the
			// per-index "History[N]" rows are the same class.
			return strings.HasPrefix(row, "Cfg") && strings.Contains(row, "History")
		},
	},
	{
		name: "copy-symlink-dereference",
		why:  "COPY of a symlink source: kaniko preserves the symlink, docker dereferences it, so the copied entry differs by Linkname (diffArgsMap copy_symlink, kaniko considered correct)",
		flag: "",
		match: func(row string) bool {
			return strings.HasPrefix(row, "File") && strings.Contains(row, "Linkname")
		},
	},
	{
		name: "chmod-implicit-parent-dir",
		why:  "COPY --chmod is not applied to implicitly created parent dirs; kaniko leaves them at 0755 (0x1ed) while docker uses the chmod value (mz863)",
		flag: "FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS",
		match: func(row string) bool {
			// A directory-mode row (File <path>/ Mode ...) where the kaniko side is
			// the default 0755. The 0x1ed tell keeps this from swallowing other mode diffs.
			return strings.HasPrefix(row, "File") && strings.Contains(row, "Mode ") && strings.Contains(row, "0x1ed")
		},
	},
	{
		name: "workdir-implicit-dir-ownership",
		why:  "WORKDIR leaves implicitly created parent dirs owned by root while docker owns them by the active USER; kaniko chowns only the leaf (mz864)",
		flag: "",
		match: func(row string) bool {
			// A directory-ownership row where the kaniko side is root (Uid 0).
			return strings.HasPrefix(row, "File") && strings.Contains(row, "Uid ") && strings.HasSuffix(strings.TrimSpace(row), "Uid 0")
		},
	},
	{
		name: "noop-run-unchanged-dir-layer",
		why:  "when a RUN makes no net filesystem change, docker emits an empty layer but kaniko includes the unchanged parent directory, so layer contents and count diverge (mz595)",
		flag: "",
		match: func(row string) bool {
			return strings.HasPrefix(row, "Layer") && (strings.Contains(row, "length mismatch") || strings.Contains(row, "only appears in input"))
		},
	},
}

// knownBuildFailures recognize a kaniko build failure (kaniko fails, docker builds)
// that is already understood, matched against kaniko's output rather than a diff row.
// A failure matching none of these is reported as a build-outcome divergence.
var knownBuildFailures = []knownDivergence{
	{
		name: "dangling-symlink-dest-resolution",
		why:  "COPY of a symlink whose target is absent leaves a dangling symlink; a later COPY resolving its destination through that symlink fails in kaniko (lstat ... no such file) while docker builds",
		flag: "",
		match: func(out string) bool {
			return strings.Contains(out, "resolving dest symlink: failed to eval symlinks")
		},
	},
}

// allKnownClasses is every known class, for reporting the campaign baseline.
func allKnownClasses() []knownDivergence {
	return append(append([]knownDivergence{}, dockerKnownDivergences...), knownBuildFailures...)
}

// diffTypes are the leading tokens diffoci uses for a diff row. A line starting
// with one of these, after the table header, is a real diff and not a log line.
var diffTypes = map[string]struct{}{
	"Desc": {}, "Cfg": {}, "Layer": {}, "File": {}, "Dir": {}, "Symlink": {}, "Mode": {}, "Whiteout": {},
}

// diffRows extracts the diff rows from diffoci output, dropping the leading pull
// and load log lines and the table header.
func diffRows(out string) []string {
	var rows []string
	seenHeader := false
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "TYPE" {
			seenHeader = true
			continue
		}
		if !seenHeader {
			continue
		}
		if _, ok := diffTypes[fields[0]]; ok {
			rows = append(rows, line)
		}
	}
	return rows
}

// classification splits a diff into counts of known classes and the rows that
// match no known class.
type classification struct {
	known map[string]int
	novel []string
}

func classify(out string, allowlist []knownDivergence) classification {
	c := classification{known: map[string]int{}}
	for _, row := range diffRows(out) {
		matched := false
		for _, kd := range allowlist {
			if kd.match(row) {
				c.known[kd.name]++
				matched = true
				break
			}
		}
		if !matched {
			c.novel = append(c.novel, row)
		}
	}
	return c
}
