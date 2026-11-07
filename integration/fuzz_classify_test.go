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
	"regexp"
	"strconv"
	"strings"
)

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

// implicitDirChmodDivergence recognizes the mz922 implicit-parent-dir chmod class on a
// diffoci row. diffoci prints the docker mode first and the kaniko mode second. It matches
// a directory row (path ends with "/") that is a pure mode diff where the kaniko mode is
// either the docker mode with the launcher umask cleared (the COPY masking, 0777 to 0755)
// or the plain default 0755 (the ADD-from-URL path never applying the mode).
func implicitDirChmodDivergence(row string) bool {
	fields := strings.Fields(row)
	if len(fields) < 2 || fields[0] != "File" || !strings.HasSuffix(fields[1], "/") {
		return false
	}
	var modes []int64
	for _, f := range fields {
		if strings.HasPrefix(f, "0x") {
			v, err := strconv.ParseInt(f[2:], 16, 32)
			if err == nil {
				modes = append(modes, v&0o7777)
			}
		}
	}
	if len(modes) != 2 {
		return false
	}
	docker, kaniko := modes[0], modes[1]
	return docker != kaniko && (kaniko == docker&^0o22 || kaniko == 0o755)
}

// Generated instructions whose divergence is already filed write to a distinctive path
// prefix, so a diff row can be attributed to the instruction that produced it rather than
// matched by shape. That keeps the suppression narrow: an unrelated bug touching some other
// path still reports, even in a case that also used one of these instructions.
var (
	copyParentsPath = regexp.MustCompile(`\bparents[0-9]+/`)
	copyExcludePath = regexp.MustCompile(`\bexcl[0-9]+/`)
	copyLinkIntoDir = regexp.MustCompile(`\blkdir[0-9]+/`)
	// a directory the --link shape writes under, its owner rows only
	copyLinkParentOwner = regexp.MustCompile(`\blk(s|[0-9]+)/\s+(Uid|Gid) `)
)

// dockerKnownDivergences are the classes observed on the docker oracle. The cache
// oracle passes no allowlist, so any cache diff is treated as novel.
var dockerKnownDivergences = []knownDivergence{
	{
		name: "copy-parents-not-implemented",
		why:  "COPY --parents is accepted by the vendored parser and never read by kaniko (Parents has no references in the tree), so docker preserves the source's parent directories under the destination and kaniko flattens them. Tracked upstream as GoogleContainerTools/kaniko#3165",
		flag: "",
		match: func(row string) bool {
			return copyParentsPath.MatchString(row)
		},
	},
	{
		name: "copy-link-into-existing-dir",
		why:  "mz1178: COPY --link to a destination without a trailing slash copies into a directory an earlier layer made, where buildkit replaces the directory with the file",
		flag: "FF_KANIKO_COPY_LINK",
		match: func(row string) bool {
			return copyLinkIntoDir.MatchString(row)
		},
	},
	{
		name: "copy-link-chown-existing-parent",
		why:  "mz1214: COPY --link --chown keeps the owner of a parent directory an earlier layer made, where buildkit's link layer carries the parent with the --chown owner",
		flag: "",
		match: func(row string) bool {
			return copyLinkParentOwner.MatchString(row)
		},
	},
	{
		name: "copy-exclude-not-implemented",
		why:  "COPY --exclude is accepted by the vendored parser and never read by kaniko (ExcludePatterns has no references in the tree), so docker drops the excluded entry and kaniko copies it anyway",
		flag: "",
		match: func(row string) bool {
			return copyExcludePath.MatchString(row)
		},
	},
	{
		name: "chmod-implicit-parent-dir",
		why:  "mz922: COPY/ADD --chmod is not applied verbatim to implicitly created parent dirs. The COPY path masks the explicit mode with the launcher umask (kaniko == docker &^ 022, so 0777 becomes 0755), and the ADD-from-URL path never applies it (kaniko stays 0755). Pre-existing, only partially closed by the mz863 fix",
		flag: "FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS",
		match: func(row string) bool {
			return implicitDirChmodDivergence(row)
		},
	},
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
	{
		name: "chown-named-owner-no-passwd-db",
		why:  "mz897: COPY/ADD --chown with a named user or group on a base with no /etc/passwd or /etc/group (scratch) fails; kaniko parses the name as a numeric id and errors while docker resolves it",
		flag: "",
		match: func(out string) bool {
			return strings.Contains(out, "getting user group from chown")
		},
	},
	{
		name: "workdir-named-user-no-passwd-db",
		why:  "mz1173: WORKDIR after USER with a name fails on a scratch base, the same lookup as mz897 reached from WORKDIR",
		flag: "",
		match: func(out string) bool {
			return strings.Contains(out, "is not a uid and does not exist on the system")
		},
	},
	{
		name: "targetarch-empty-in-from",
		why:  "mz1126: the executor resolves $TARGETARCH in FROM to empty, so the base reference ends in a bare colon. The warmer half (#1210) is only reachable once this is fixed",
		flag: "",
		match: func(out string) bool {
			return strings.Contains(out, "could not parse reference: ") && strings.Contains(out, archBaseTag+"\n")
		},
	},
	{
		name: "run-self-referential-symlink",
		why:  "mz1174: a RUN that leaves a symlink pointing at itself fails the snapshot, ResolvePaths follows every changed link and aborts on the loop",
		flag: "",
		match: func(out string) bool {
			return strings.Contains(out, "failed to take snapshot: EvalSymlinks: too many links")
		},
	},
}

// knownCrashes recognize a kaniko crash or assertion that is already filed and would
// otherwise flood a campaign, matched against the crash line. A crash matching one of
// these is counted, not reported, so novel crashes still surface. Remove an entry once
// its issue is fixed so the fuzzer guards against regressions again.
var knownCrashes = []knownDivergence{
	{
		name: "cache-lookahead-arg-scope",
		why:  "mz872: with FF_KANIKO_CACHE_LOOKAHEAD the aggregate finalCacheKey over-includes a later-declared ARG in an earlier command, so the precompute and the build disagree and the executor.build.cache-lookahead assertion fires",
		flag: "FF_KANIKO_CACHE_LOOKAHEAD",
		match: func(crash string) bool {
			return strings.Contains(crash, "executor.build.cache-lookahead")
		},
	},
}

// knownWarmerGaps recognize an image the executor pulls and the warmer does not warm that
// is already understood, matched against a warmerPlanOracle row. Remove an entry once its
// fix is on main so the fuzzer guards the regression.
var knownWarmerGaps = []knownDivergence{}

// knownCrashName returns the class name if the crash line matches a known filed crash,
// else "". Used to count-not-report crashes that are already understood.
func knownCrashName(crash string) string {
	for _, kc := range knownCrashes {
		if kc.match(crash) {
			return kc.name
		}
	}
	return ""
}

// mz876CachedLayer names the cached-layer half of mz876: a layer served from the cache is
// extracted onto the filesystem without entering the layered map, so a command snapshotted
// after it in the same build attributes those files to the wrong layer. It needs a build that
// mixes cache hits with re-snapshotted commands.
const mz876CachedLayer = "mz876-cached-layer-map"

// mz1365ReproducibleStage names a --reproducible diff of only file timestamps on a case
// where a stage is both a FROM base and a COPY --from source. That stage's layers are
// preserved like base image layers and keep their build-time mtimes.
const mz1365ReproducibleStage = "mz1365-reproducible-stage-mtimes"

var fromStage = regexp.MustCompile(`(?m)^FROM (stage[0-9]+)\b`)

// reproducibleStageMtimes reports whether a diff between two --reproducible builds is the
// mz1365 class.
func reproducibleStageMtimes(dockerfile, diff string) bool {
	shape := false
	for _, m := range fromStage.FindAllStringSubmatch(dockerfile, -1) {
		if strings.Contains(dockerfile, "--from="+m[1]+" ") || strings.Contains(dockerfile, "--from="+strings.TrimPrefix(m[1], "stage")+" ") {
			shape = true
		}
	}
	rows := diffRows(diff)
	if !shape || len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if !strings.HasPrefix(row, "File") || strings.Count(row, " GMT") != 2 {
			return false
		}
	}
	return true
}

// layerAttributionOnly reports whether a kaniko-vs-kaniko diff only moves entries between
// layers, which is all mz876 does. A row about content, mode, ownership or config is
// something else.
func layerAttributionOnly(diff string) bool {
	rows := diffRows(diff)
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if !strings.HasPrefix(row, "Layer") || !(strings.Contains(row, "only appears in") || strings.Contains(row, "length mismatch")) {
			return false
		}
	}
	return true
}

// allKnownClasses is every known class, for reporting the campaign baseline.
func allKnownClasses() []knownDivergence {
	classes := append([]knownDivergence{}, dockerKnownDivergences...)
	classes = append(classes, knownBuildFailures...)
	classes = append(classes, knownCrashes...)
	classes = append(classes, knownWarmerGaps...)
	// Structural classes, counted by the oracle rather than matched on diff text.
	classes = append(classes, knownDivergence{name: mz1365ReproducibleStage, why: "--reproducible keeps build-time mtimes of a stage that is both a FROM base and a COPY --from source (mz1365)", flag: "FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS"})
	return append(classes, knownDivergence{name: mz876CachedLayer, why: "a cached layer never enters the layered map, so a later snapshot misattributes its files (mz876)", flag: ""})
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
