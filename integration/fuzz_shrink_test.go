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
	"strings"
	"testing"
)

// reportable is true for any finding worth reporting and shrinking.
func reportable(s severity) bool { return s != sevClean }

// instructionCount counts the non-empty lines of a Dockerfile.
func instructionCount(dockerfile string) int {
	n := 0
	for line := range strings.SplitSeq(dockerfile, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// rowTypes returns the set of diffoci row types present in text, independent of
// any table header, so it works on a full diff and on a joined set of rows.
func rowTypes(text string) map[string]bool {
	out := map[string]bool{}
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, ok := diffTypes[fields[0]]; ok {
			out[fields[0]] = true
		}
	}
	return out
}

// sameClass reports whether cand reproduces the same finding class as target.
// For diffs it requires the same severity and that cand's diff-row types are a
// subset of target's, so a reduction that morphs into a different divergence is
// rejected. For crashes and build-outcome findings the severity and summary match.
func sameClass(target, cand *finding) bool {
	if cand == nil || cand.sev != target.sev {
		return false
	}
	switch target.sev {
	case sevDockerDiff, sevCacheDiff:
		ts := rowTypes(target.detail)
		cs := rowTypes(cand.detail)
		if len(cs) == 0 {
			return false
		}
		for k := range cs {
			if !ts[k] {
				return false
			}
		}
		return true
	default:
		return cand.summary == target.summary
	}
}

// shrinkFinding greedily removes instructions from a reproducing Dockerfile while
// the same finding class still reproduces, returning the finding of the minimal
// reproducer so its dockerfile and diff detail stay consistent. FROM is always
// kept. The cache oracle is skipped unless the target is a cache diff, so shrinking
// a docker or crash finding does not pay for cache builds per candidate.
func shrinkFinding(t *testing.T, target *finding, gen genResult) *finding {
	lines := splitDockerfile(gen.dockerfile)
	if len(lines) <= 2 {
		return target
	}
	skipCache := target.sev != sevCacheDiff

	candidate := 0
	reproduces := func(cand genResult) *finding {
		candidate++
		// Label carries the seed so concurrent shrinks of different findings do not
		// collide on image tags or cache repos.
		label := fmt.Sprintf("fuzz-shrink-%d-%d", target.seed, candidate)
		f := buildAndClassify(t, target.seed, label, cand, "", skipCache)
		if sameClass(target, f) {
			return f
		}
		return nil
	}

	best := target
	changed := true
	for changed {
		changed = false
		for i := 1; i < len(lines); i++ { // keep FROM at index 0
			reduced := removeAt(lines, i)
			if f := reproduces(genFromLines(reduced, gen.context)); f != nil {
				lines = reduced
				best = f
				changed = true
				break
			}
		}
	}
	return best
}

func splitDockerfile(dockerfile string) []string {
	var lines []string
	for line := range strings.SplitSeq(strings.TrimRight(dockerfile, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func removeAt(lines []string, i int) []string {
	out := make([]string, 0, len(lines)-1)
	out = append(out, lines[:i]...)
	return append(out, lines[i+1:]...)
}

// genFromLines rebuilds a genResult from Dockerfile lines, keeping the full
// context. Unreferenced context files do not affect the build, and keeping them
// avoids mishandling directory or hardlink entries whose Dockerfile reference
// (e.g. COPY d0) does not match the per-file context names (d0/f0, d0/f1).
func genFromLines(lines []string, ctx []fileSpec) genResult {
	return genResult{dockerfile: strings.Join(lines, "\n") + "\n", context: ctx}
}
