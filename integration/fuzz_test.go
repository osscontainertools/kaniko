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
	"archive/tar"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type severity int

const (
	sevClean severity = iota
	sevDockerDiff
	sevDeterminismDiff
	sevInvarianceDiff
	sevCacheDiff
	sevBuildOutcome
	sevCrash
)

func (s severity) String() string {
	switch s {
	case sevDockerDiff:
		return "DOCKER_DIFF"
	case sevDeterminismDiff:
		return "DETERMINISM_DIFF"
	case sevInvarianceDiff:
		return "INVARIANCE_DIFF"
	case sevCacheDiff:
		return "CACHE_DIFF"
	case sevBuildOutcome:
		return "BUILD_OUTCOME"
	case sevCrash:
		return "CRASH"
	default:
		return "CLEAN"
	}
}

type finding struct {
	seed       int64
	sev        severity
	summary    string
	detail     string
	dockerfile string
	known      map[string]int // known divergence classes seen on this case, counted not reported
}

// crashMarkers identify a kaniko process that panicked, tripped an assertion, or
// hit unreachable code. These outrank any image diff.
var crashMarkers = []string{
	"Assertion violated [",
	"Unreachable Code:",
	"panic:",
	"runtime error:",
	"goroutine ",
}

func detectCrash(out string) string {
	for _, m := range crashMarkers {
		if idx := strings.Index(out, m); idx >= 0 {
			line := out[idx:]
			if nl := strings.IndexByte(line, '\n'); nl >= 0 {
				line = line[:nl]
			}
			return line
		}
	}
	return ""
}

// TestFuzz runs a differential fuzzing campaign: it generates random Dockerfiles
// and, for each, builds with docker and kaniko and compares across two oracles,
// docker parity and cache self-consistency. Gated behind FUZZ_CASES or FUZZ_DURATION
// so it does not run as part of the normal suite. Env knobs: FUZZ_CASES=N (case
// count), FUZZ_DURATION=30m (run to a deadline, wins over FUZZ_CASES), FUZZ_WORKERS=4
// (concurrent cases), FUZZ_SEED (first seed), FUZZ_OUT (artifact dir, also holds a
// live summary.txt), FUZZ_DETERMINISM=1 (also run the build-twice determinism oracle),
// FUZZ_INVARIANCE=1 (also run the cache-lookahead on-vs-off invariance oracle); both add
// kaniko builds per case. Under parallelism a finding is reproduced from its written
// Dockerfile, not from the seed alone, since corpus state affects inputs.
func TestFuzz(t *testing.T) {
	casesStr := os.Getenv("FUZZ_CASES")
	durStr := os.Getenv("FUZZ_DURATION")
	if casesStr == "" && durStr == "" {
		t.Skip("set FUZZ_CASES=N or FUZZ_DURATION=30m to run the differential fuzzer")
	}
	maxCases := 0
	if casesStr != "" {
		v, err := strconv.Atoi(casesStr)
		if err != nil || v <= 0 {
			t.Fatalf("invalid FUZZ_CASES=%q", casesStr)
		}
		maxCases = v
	}
	// FUZZ_DURATION runs until a deadline instead of a fixed count, for long
	// unattended campaigns. It takes precedence over FUZZ_CASES.
	var deadline time.Time
	if durStr != "" {
		d, err := time.ParseDuration(durStr)
		if err != nil || d <= 0 {
			t.Fatalf("invalid FUZZ_DURATION=%q", durStr)
		}
		deadline = time.Now().Add(d)
	}
	var seedBase int64
	if s := os.Getenv("FUZZ_SEED"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("invalid FUZZ_SEED=%q", s)
		}
		seedBase = v
	}
	workers := 4
	if w := os.Getenv("FUZZ_WORKERS"); w != "" {
		if v, err := strconv.Atoi(w); err == nil && v > 0 {
			workers = v
		}
	}
	outDir := os.Getenv("FUZZ_OUT")
	if outDir == "" {
		d, err := os.MkdirTemp("", "kaniko-fuzz-findings-")
		if err != nil {
			t.Fatal(err)
		}
		outDir = d
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The test process runs in the package directory, so resolve FUZZ_OUT to an
	// absolute path and log it rather than surprising the caller with a relative one.
	if abs, err := filepath.Abs(outDir); err == nil {
		outDir = abs
	}
	bound := fmt.Sprintf("%d cases", maxCases)
	if !deadline.IsZero() {
		bound = "until " + durStr + " elapsed"
	}
	t.Logf("fuzz campaign: %s from seed %d, %d workers, artifacts in %s", bound, seedBase, workers, outDir)

	// Mirror the upstream bases into the local registry once, so no build pulls from
	// Docker Hub during the campaign (concurrent Docker Hub pulls get rate-limited and
	// hang the executor). Each step is skipped if the target already exists. crane copy
	// preserves the source media type, so alpine stays docker v2 and debian stays OCI.
	ociBase := strings.ToLower(config.imageRepo + ociFuzzBaseTag)
	mirror := func(name, src, dst string, copyArgs ...string) {
		if _, err := RunCommandWithoutTest(exec.Command("crane", "manifest", dst)); err == nil {
			return
		}
		if out, err := RunCommandWithoutTest(exec.Command(copyArgs[0], copyArgs[1:]...)); err != nil {
			t.Fatalf("failed to mirror %s base %s -> %s: %v\n%s", name, src, dst, err, out)
		}
	}
	mirror("alpine", alpineFuzzBase, strings.ToLower(config.imageRepo+baseAlpineTag),
		"crane", "copy", alpineFuzzBase, strings.ToLower(config.imageRepo+baseAlpineTag))
	mirror("debian", debianFuzzBase, strings.ToLower(config.imageRepo+baseDebianTag),
		"crane", "copy", debianFuzzBase, strings.ToLower(config.imageRepo+baseDebianTag))
	// The OCI base is minted from alpine with skopeo; --src-no-creds forces an anonymous
	// pull, avoiding a stale credential in the environment.
	mirror("oci", alpineFuzzBase, ociBase,
		"skopeo", "copy", "--src-no-creds", "--format", "oci", "--dest-tls-verify=false",
		"docker://"+alpineFuzzBase, "docker://"+ociBase)

	// Warm the base media-type cache single-threaded so the concurrent workers only
	// read it (baseIsOCI would otherwise write the map from many goroutines).
	for _, ref := range fuzzBaseRefs() {
		baseIsOCI(ref)
	}

	tracker := newCoverageTracker()
	if tracker.enabled {
		t.Logf("coverage admission enabled (reading executor GOCOVERDIR)")
	} else {
		t.Logf("coverage admission disabled: run with -coverage-dir=... to enable")
	}

	var (
		mu          sync.Mutex // guards findings, corpus, knownTotals, sterile
		findings    []finding
		corpus      [][]byte // inputs that reached new coverage, preferred parents for mutation
		knownTotals = map[string]int{}
		sterile     int
		caseIdx     int64 = -1
		doneCount   int64
	)

	writeSummary := func(final bool) {
		// Called under mu. Writes a live snapshot so a long run can be watched
		// without parsing the log.
		var sb strings.Builder
		state := "running"
		if final {
			state = "done"
		}
		fmt.Fprintf(&sb, "state: %s\nbound: %s\nworkers: %d\ncases done: %d\nfindings: %d\nsterile: %d\ncoverage blocks: %d\ncorpus: %d\n\n",
			state, bound, workers, atomic.LoadInt64(&doneCount), len(findings), sterile, tracker.total(), len(corpus))
		fmt.Fprintf(&sb, "known-divergence baseline (counted, not reported):\n")
		for _, kd := range allKnownClasses() {
			flag := kd.flag
			if flag == "" {
				flag = "no flag yet"
			}
			fmt.Fprintf(&sb, "  %-30s %d  [%s]\n", kd.name, knownTotals[kd.name], flag)
		}
		if len(findings) > 0 {
			fmt.Fprintf(&sb, "\nreported findings:\n")
			for _, f := range findings {
				fmt.Fprintf(&sb, "  seed %d  %s  %s\n", f.seed, f.sev, f.summary)
			}
		}
		if err := os.WriteFile(filepath.Join(outDir, "summary.txt"), []byte(sb.String()), 0o644); err != nil {
			t.Logf("write summary: %v", err)
		}
	}

	worker := func() {
		for {
			if !deadline.IsZero() && time.Now().After(deadline) {
				return
			}
			idx := atomic.AddInt64(&caseIdx, 1)
			if deadline.IsZero() && idx >= int64(maxCases) {
				return
			}
			seed := seedBase + idx

			// Prefer mutating an admitted parent so the search builds on inputs that
			// reached new executor code; fall back to a bare seed until the corpus fills.
			mu.Lock()
			input := seedBytes(seed)
			if len(corpus) > 0 {
				input = mutateInput(corpus[int(seed)%len(corpus)], seed)
			}
			mu.Unlock()

			f, newCov := runFuzzCase(t, seed, input, tracker)

			mu.Lock()
			if newCov > 0 {
				corpus = append(corpus, input)
				t.Logf("[seed %d] +%d new coverage blocks (total %d), corpus %d", seed, newCov, tracker.total(), len(corpus))
			}
			if f != nil {
				for name, c := range f.known {
					knownTotals[name] += c
				}
				switch {
				case f.sev == sevClean && f.summary == "sterile":
					sterile++
				case f.sev == sevClean:
					// no divergence
				default:
					findings = append(findings, *f)
					t.Logf("[seed %d] %s: %s", seed, f.sev, f.summary)
					if err := writeFinding(outDir, *f); err != nil {
						t.Logf("failed to write finding for seed %d: %v", seed, err)
					}
				}
			}
			d := atomic.AddInt64(&doneCount, 1)
			if d%25 == 0 {
				t.Logf("progress: %d done, %d findings, %d sterile, %d coverage blocks, corpus %d",
					d, len(findings), sterile, tracker.total(), len(corpus))
				writeSummary(false)
			}
			mu.Unlock()
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker()
		}()
	}
	wg.Wait()

	mu.Lock()
	t.Logf("campaign done: %d cases, %d findings, %d sterile, %d coverage blocks, corpus %d",
		doneCount, len(findings), sterile, tracker.total(), len(corpus))
	t.Logf("known-divergence baseline (counted, not reported):")
	for _, kd := range allKnownClasses() {
		flag := kd.flag
		if flag == "" {
			flag = "no flag yet"
		}
		t.Logf("  %-30s %d  [%s]", kd.name, knownTotals[kd.name], flag)
	}
	writeSummary(true)
	for _, f := range findings {
		if f.sev == sevCrash {
			t.Errorf("crash on seed %d: %s", f.seed, f.summary)
		}
	}
	mu.Unlock()
}

// buildAndClassify builds gen with docker and kaniko under the given label, runs
// the oracles, and returns the highest-severity finding. covDir, when set, is
// mounted as the fresh build's GOCOVERDIR. skipCache omits the cache oracle, which
// the shrinker sets when the target finding is not a cache divergence. It holds no
// coverage or corpus logic so the shrinker can reuse it as a reproduce predicate.
func buildAndClassify(t *testing.T, seed int64, label string, gen genResult, covDir string, skipCache bool) *finding {
	dir, err := os.MkdirTemp("", "kaniko-fuzz-ctx-")
	if err != nil {
		t.Logf("[%s] tempdir: %v", label, err)
		return nil
	}
	defer os.RemoveAll(dir)
	if err := writeContext(dir, gen); err != nil {
		t.Logf("[%s] write context: %v", label, err)
		return nil
	}

	dockerImage := GetDockerImage(config.imageRepo, label)
	kanikoImage := GetKanikoImage(config.imageRepo, label)
	// Remove this case's images from the local daemon once done. Without this the
	// daemon store grows by several images per case and fills the disk over a long run.
	defer cleanupFuzzImages(label)

	fail := func(sev severity, summary, detail string) *finding {
		return &finding{seed: seed, sev: sev, summary: summary, detail: detail, dockerfile: gen.dockerfile}
	}

	// kaniko mirrors the final base's media type, so tell docker to emit the same.
	_, dockerErr := runFuzzDocker(dir, dockerImage, baseIsOCI(finalBaseRef(gen.dockerfile)))
	kanikoOut, kanikoErr := runFuzzKaniko(dir, kanikoImage, gen.kanikoFlags, covDir)

	if crash := detectCrash(kanikoOut); crash != "" {
		return fail(sevCrash, crash, kanikoOut)
	}
	switch {
	case dockerErr != nil && kanikoErr != nil:
		// Both builds failed. Not a divergence, and the case teaches us nothing.
		return &finding{seed: seed, sev: sevClean, summary: "sterile"}
	case dockerErr != nil && kanikoErr == nil:
		return fail(sevBuildOutcome, "docker failed, kaniko built", fmt.Sprintf("docker error: %v", dockerErr))
	case dockerErr == nil && kanikoErr != nil:
		for _, kf := range knownBuildFailures {
			if kf.match(kanikoOut) {
				return &finding{seed: seed, sev: sevClean, known: map[string]int{kf.name: 1}}
			}
		}
		return fail(sevBuildOutcome, "kaniko failed, docker built", kanikoOut)
	}

	// FUZZ_TREAT_KNOWN_AS_NOVEL makes every docker diff reportable, so the known
	// classes can be exercised as if novel (useful for validating the shrinker).
	allow := dockerKnownDivergences
	if os.Getenv("FUZZ_TREAT_KNOWN_AS_NOVEL") == "1" {
		allow = nil
	}

	// docker oracle: cross-tool parity. File timestamps and tar format differ
	// between the two tools by design, so those are the only diffs excused here.
	dockerIgnores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	// --single-snapshot squashes kaniko's layers, so docker's per-instruction layer
	// count legitimately differs; excuse layer-count here while still comparing file
	// content and metadata.
	if hasFlag(gen.kanikoFlags, "--single-snapshot") {
		dockerIgnores = append(dockerIgnores, "--extra-ignore-layer-length-mismatch")
	}
	dockerDiff, dockerSame, derr := runFuzzDiffoci(dockerImage, kanikoImage, dockerIgnores)
	if derr != nil && dockerSame {
		t.Logf("[%s] docker-oracle diffoci error: %v", label, derr)
	}
	var dockerCls classification
	if !dockerSame {
		dockerCls = classify(dockerDiff, allow)
	}

	// cache oracle: kaniko populate versus kaniko consume. Both sides are kaniko
	// building the identical context, so the comparison is byte-strict. Only the
	// image name and config timestamp are excused, nothing at the file level.
	if !skipCache {
		cacheRepo := strings.ToLower(config.imageRepo + "fuzzcache-" + label)
		v0 := GetVersionedKanikoImage(config.imageRepo, label, 0)
		v1 := GetVersionedKanikoImage(config.imageRepo, label, 1)
		// --cache-copy-layers is required for COPY layers to be served from cache;
		// without it the consume build re-runs COPY and restamps wall-clock mtimes.
		cacheArgs := append([]string{"--cache=true", "--cache-copy-layers=true", "--cache-repo=" + cacheRepo}, gen.kanikoFlags...)
		// Point the cache builds at the same per-case GOCOVERDIR as the fresh build.
		// They run sequentially, so GOCOVERDIR just accumulates per-process files, and
		// observe() then captures the union, including the pkg/cache paths the fresh
		// build never touches.
		c0, ce0 := runFuzzKaniko(dir, v0, cacheArgs, covDir)
		if crash := detectCrash(c0); crash != "" {
			return fail(sevCrash, crash, c0)
		}
		c1, ce1 := runFuzzKaniko(dir, v1, cacheArgs, covDir)
		if crash := detectCrash(c1); crash != "" {
			return fail(sevCrash, crash, c1)
		}

		if ce0 != nil || ce1 != nil {
			f := fail(sevBuildOutcome, "cache build failed while fresh build succeeded", c0+"\n"+c1)
			f.known = dockerCls.known
			return f
		}
		cacheIgnores := []string{"--ignore-image-name", "--ignore-image-timestamps"}
		// Some flags legitimately make a cached consume build restamp wall-clock mtimes:
		// --single-snapshot re-snapshots the whole filesystem each build, and
		// --cache-run-layers=false leaves RUN layers uncached so the consume build
		// re-runs them. Excuse timestamps for those while still comparing content, mode,
		// ownership, and structure exactly.
		if hasFlag(gen.kanikoFlags, "--single-snapshot") || hasFlag(gen.kanikoFlags, "--cache-run-layers=false") {
			cacheIgnores = append(cacheIgnores, "--ignore-file-timestamps")
		}
		cacheDiff, cacheSame, _ := runFuzzDiffoci(v0, v1, cacheIgnores)

		// The cache oracle uses no allowlist: both sides are kaniko building the
		// identical context, so any diff is novel and outranks a cross-tool diff.
		if !cacheSame {
			f := fail(sevCacheDiff, "cache populate and consume differ", cacheDiff)
			f.known = dockerCls.known
			return f
		}

		// invariance oracle: cache-lookahead is a performance optimization and must not
		// change output. Build the same case with --cache and the lookahead family of
		// flags off, then compare the cache-consume image to the lookahead-on one (v1).
		// A divergence is a lookahead bug (mz872 is the crashing variant; this catches
		// the silent variant where a wrong cached layer is served). Gated: adds builds.
		if os.Getenv("FUZZ_INVARIANCE") == "1" {
			offEnv := []string{"FF_KANIKO_CACHE_LOOKAHEAD=0", "FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY=0", "FF_KANIKO_RESOLVE_CACHE_KEY=0"}
			offRepo := strings.ToLower(config.imageRepo + "fuzzcache-off-" + label)
			offArgs := append([]string{"--cache=true", "--cache-copy-layers=true", "--cache-repo=" + offRepo}, gen.kanikoFlags...)
			w0 := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-off0")
			w1 := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-off1")
			defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", w0, w1))
			o0, oe0 := runFuzzKanikoEnv(dir, w0, offArgs, "", offEnv)
			if crash := detectCrash(o0); crash != "" {
				return fail(sevCrash, crash, o0)
			}
			o1, oe1 := runFuzzKanikoEnv(dir, w1, offArgs, "", offEnv)
			if crash := detectCrash(o1); crash != "" {
				return fail(sevCrash, crash, o1)
			}
			if oe0 == nil && oe1 == nil {
				// The on and off images come from two independently populated caches
				// built at different wall-clock times, so cached-layer file timestamps
				// differ regardless of lookahead correctness. Compare content, mode,
				// ownership, and structure exactly; a real lookahead output bug shows there.
				invIgnores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
				invDiff, invSame, _ := runFuzzDiffoci(v1, w1, invIgnores)
				if !invSame {
					f := fail(sevInvarianceDiff, "cache-lookahead changes output (on vs off)", invDiff)
					f.known = dockerCls.known
					return f
				}
			}
		}
	}

	// determinism oracle: kaniko must agree with itself across two independent
	// builds of the same context, docker out of the loop. Gated behind
	// FUZZ_DETERMINISM because it adds kaniko builds per case. Two modes catch
	// different classes, so a divergence is a nondeterminism bug either way.
	if os.Getenv("FUZZ_DETERMINISM") == "1" {
		if f := determinismOracle(label, dir, kanikoImage, gen.kanikoFlags, fail); f != nil {
			f.known = dockerCls.known
			return f
		}
	}

	// A docker diff made only of known classes is the expected baseline: counted,
	// not reported. Only a row that matches no known class is a finding.
	if len(dockerCls.novel) > 0 {
		f := fail(sevDockerDiff, "docker and kaniko differ (novel)", strings.Join(dockerCls.novel, "\n"))
		f.known = dockerCls.known
		return f
	}
	return &finding{seed: seed, sev: sevClean, known: dockerCls.known}
}

// determinismOracle builds the case again with kaniko and checks that kaniko agrees
// with itself, docker out of the loop. fresh is the already-built non-reproducible
// image for the structural comparison. Returns a finding on any nondeterminism, else
// nil. It removes the extra images it creates.
func determinismOracle(label, dir, fresh string, flags []string, fail func(severity, string, string) *finding) *finding {
	repo := config.imageRepo

	// Mode 1, structural: a second fresh build must match the first apart from
	// timestamps. Any difference in layers, files, mode, ownership, or media type
	// between two runs of the identical input is nondeterminism.
	b := strings.ToLower(repo + kanikoPrefix + label + "-det-b")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", b))
	out, err := runFuzzKaniko(dir, b, flags, "")
	if crash := detectCrash(out); crash != "" {
		return fail(sevCrash, crash, out)
	}
	if err != nil {
		return fail(sevDeterminismDiff, "second build failed while the first succeeded", out)
	}
	structuralIgnores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	d, same, _ := runFuzzDiffoci(fresh, b, structuralIgnores)
	if !same {
		return fail(sevDeterminismDiff, "two builds differ (structural)", d)
	}

	// Mode 2, reproducible: two --reproducible builds must be byte-identical, so the
	// comparison excuses only the image name. --reproducible pins timestamps to the
	// epoch, so any remaining difference is a reproducibility defect.
	r0 := strings.ToLower(repo + kanikoPrefix + label + "-det-r0")
	r1 := strings.ToLower(repo + kanikoPrefix + label + "-det-r1")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", r0, r1))
	o0, e0 := runFuzzKaniko(dir, r0, append([]string{"--reproducible"}, flags...), "")
	if crash := detectCrash(o0); crash != "" {
		return fail(sevCrash, crash, o0)
	}
	o1, e1 := runFuzzKaniko(dir, r1, append([]string{"--reproducible"}, flags...), "")
	if crash := detectCrash(o1); crash != "" {
		return fail(sevCrash, crash, o1)
	}
	if e0 != nil || e1 != nil {
		return fail(sevDeterminismDiff, "reproducible build failed", o0+"\n"+o1)
	}
	d, same, _ = runFuzzDiffoci(r0, r1, []string{"--ignore-image-name"})
	if !same {
		return fail(sevDeterminismDiff, "two reproducible builds differ (byte-strict)", d)
	}
	return nil
}

// runFuzzCase generates the case for one seed, evaluates it, records the coverage
// it reached, and shrinks any reportable finding to a minimal reproducer.
func runFuzzCase(t *testing.T, seed int64, input []byte, tracker *coverageTracker) (*finding, int) {
	gen := generate(&source{b: input}, fuzzBaseRefs())
	label := fmt.Sprintf("fuzz-seed-%d", seed)

	// The fresh build gets its own GOCOVERDIR so its coverage can be measured in
	// isolation for the admission decision. When a coverage-dir is set, persist each
	// case's profile in a per-seed subdirectory of it (rather than a deleted temp) so
	// the accumulated coverage is inspectable after the run with `go tool covdata`,
	// which is how gaps, functions the fuzzer never reaches, are found.
	var covDir string
	if coverageDir != "" {
		covDir = filepath.Join(coverageDir, fmt.Sprintf("cov-%d", seed))
	} else {
		covDir, _ = os.MkdirTemp("", "kaniko-fuzz-cov-")
		defer os.RemoveAll(covDir)
	}
	if covDir != "" {
		os.MkdirAll(covDir, 0o777)
		os.Chmod(covDir, 0o777)
	}

	f := buildAndClassify(t, seed, label, gen, covDir, false)

	newCov := 0
	if n, cerr := tracker.observe(covDir); cerr != nil {
		t.Logf("[seed %d] coverage: %v", seed, cerr)
	} else {
		newCov = n
	}

	if f != nil && reportable(f.sev) && os.Getenv("FUZZ_NO_SHRINK") != "1" {
		before := instructionCount(f.dockerfile)
		min := shrinkFinding(t, f, gen)
		// Adopt the minimal reproducer's dockerfile and diff detail so the report
		// is self-consistent; keep the original seed and known-class counts.
		f.dockerfile, f.detail, f.summary = min.dockerfile, min.detail, min.summary
		t.Logf("[seed %d] shrank reproducer %d -> %d instructions", seed, before, instructionCount(f.dockerfile))
	}
	return f, newCov
}

func writeContext(dir string, gen genResult) error {
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(gen.dockerfile), 0o644); err != nil {
		return err
	}
	fixed := time.Unix(fixedFuzzEpoch, 0)
	for _, f := range gen.context {
		p := filepath.Join(dir, f.name)
		switch f.kind {
		case kindSymlink:
			if err := os.Symlink(f.target, p); err != nil {
				return err
			}
		case kindHardlink:
			if err := os.Link(filepath.Join(dir, f.target), p); err != nil {
				return err
			}
		case kindTar:
			if err := writeTarFixture(p); err != nil {
				return err
			}
		default:
			// f.name may contain a subdirectory (e.g. d0/f0), so ensure the parent exists.
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
				return err
			}
			// Chmod explicitly so the exact mode lands, including setuid, setgid, and
			// sticky bits that WriteFile's umask-masked perm would drop.
			if err := os.Chmod(p, f.mode); err != nil {
				return err
			}
			if err := os.Chtimes(p, fixed, fixed); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeTarFixture writes a small deterministic tar so ADD can auto-extract it.
func writeTarFixture(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	body := []byte("fuzz-tar-content\n")
	hdr := &tar.Header{
		Name:     "inside/file0",
		Mode:     0o644,
		Size:     int64(len(body)),
		ModTime:  time.Unix(fixedFuzzEpoch, 0),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(body); err != nil {
		return err
	}
	return tw.Close()
}

// Local-registry base tags. The harness mirrors the pinned upstream bases into the
// local registry once at setup, and the generator uses only these local refs, so a
// kaniko or docker build never pulls from Docker Hub during the campaign. That avoids
// the rate-limited pulls that otherwise hang the executor under concurrent workers.
const (
	ociFuzzBaseTag   = "fuzz-oci-base:latest"   // OCI media type, minted from alpine
	baseAlpineTag    = "fuzz-base-alpine:latest" // docker v2, single layer
	baseDebianTag    = "fuzz-base-debian:latest" // OCI index, multi layer
)

func fuzzBaseRefs() []string {
	repo := config.imageRepo
	return []string{
		strings.ToLower(repo + baseAlpineTag),
		strings.ToLower(repo + baseDebianTag),
		strings.ToLower(repo + ociFuzzBaseTag),
	}
}

var baseOCICache = map[string]bool{}

// baseIsOCI reports whether the base image's resolved linux/amd64 manifest uses
// OCI media types. kaniko mirrors this, so docker must be told to match it. The
// value is detected from the registry rather than assumed, since a ref like debian
// resolves to an OCI manifest while alpine resolves to docker v2. Results are cached
// because the shrinker asks about the same base many times.
// cleanupFuzzImages removes a case's images from the local docker daemon. The
// registry copies remain, but the daemon store is what grows fastest across a run.
func cleanupFuzzImages(label string) {
	refs := []string{
		GetDockerImage(config.imageRepo, label),
		GetKanikoImage(config.imageRepo, label),
		GetVersionedKanikoImage(config.imageRepo, label, 0),
		GetVersionedKanikoImage(config.imageRepo, label, 1),
	}
	RunCommandWithoutTest(exec.Command("docker", append([]string{"rmi", "-f"}, refs...)...))
}

func hasFlag(flags []string, f string) bool {
	for _, v := range flags {
		if v == f {
			return true
		}
	}
	return false
}

func baseIsOCI(ref string) bool {
	if v, ok := baseOCICache[ref]; ok {
		return v
	}
	out, err := RunCommandWithoutTest(exec.Command("crane", "manifest", "--platform", "linux/amd64", ref))
	oci := err == nil && strings.Contains(string(out), "vnd.oci.image")
	baseOCICache[ref] = oci
	return oci
}

// finalBaseRef returns the image ref of the last FROM, whose base determines the
// media type kaniko emits and therefore the format docker must be told to match.
func finalBaseRef(dockerfile string) string {
	ref := ""
	for line := range strings.SplitSeq(dockerfile, "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "FROM" {
			ref = f[1]
		}
	}
	return ref
}

// runFuzzDocker builds and pushes with docker. --provenance=false drops attestation
// entries and yields a docker v2 manifest; oci adds oci-mediatypes=true to emit OCI
// instead, matching whatever media type the final base carries so the comparison
// does not flag a format difference the base itself dictated.
func runFuzzDocker(contextDir, image string, oci bool) (string, error) {
	if oci {
		out, err := RunCommandWithoutTest(exec.Command("docker", "build", "--no-cache", "--provenance=false",
			"--output", "type=image,name="+image+",oci-mediatypes=true,push=true", contextDir))
		return string(out), err
	}
	out, err := RunCommandWithoutTest(exec.Command("docker", "build", "--no-cache", "--provenance=false", "-t", image, contextDir))
	if err != nil {
		return string(out), err
	}
	pushOut, err := RunCommandWithoutTest(exec.Command("docker", "push", image))
	return string(out) + string(pushOut), err
}

// runFuzzKaniko mirrors buildKanikoImage's invocation (full KanikoEnv, executor
// image, read-only context mount) but returns the raw output so the harness can
// detect crashes and classify failures itself. When covDir is non-empty it is
// mounted as GOCOVERDIR so the build's coverage can be measured in isolation.
func runFuzzKaniko(contextDir, image string, extra []string, covDir string) (string, error) {
	return runFuzzKanikoEnv(contextDir, image, extra, covDir, nil)
}

// runFuzzKanikoEnv is runFuzzKaniko with extra -e env vars appended after KanikoEnv.
// docker keeps the last value for a repeated -e, so an override here wins over the
// KanikoEnv default (used to flip FF_KANIKO_* flags off for the invariance oracle).
func runFuzzKanikoEnv(contextDir, image string, extra []string, covDir string, envOverride []string) (string, error) {
	flags := []string{"run", "--net=host", "-v", contextDir + ":/workspace:ro"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = addServiceAccountFlags(flags, config.serviceAccount)
	flags = append(flags, ExecutorImage,
		"-f", path.Join(buildContextPath, "Dockerfile"),
		"-d", image,
		"-c", buildContextPath,
	)
	flags = append(flags, extra...)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// runFuzzDiffoci pulls both images and diffs them with the given ignores. It
// returns the diff output, whether the images are identical, and any tool error.
func runFuzzDiffoci(image1, image2 string, ignores []string) (string, bool, error) {
	if out, err := RunCommandWithoutTest(exec.Command("docker", "pull", image1)); err != nil {
		return string(out), false, fmt.Errorf("pull %s: %w", image1, err)
	}
	if out, err := RunCommandWithoutTest(exec.Command("docker", "pull", image2)); err != nil {
		return string(out), false, fmt.Errorf("pull %s: %w", image2, err)
	}
	args := append([]string{"diff"}, ignores...)
	args = append(args, daemonPrefix+image1, daemonPrefix+image2)
	out, err := RunCommandWithoutTest(exec.Command("diffoci", args...))
	return string(out), err == nil, err
}

func writeFinding(outDir string, f finding) error {
	dir := filepath.Join(outDir, fmt.Sprintf("seed_%d_%s", f.seed, f.sev))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(f.dockerfile), 0o644); err != nil {
		return err
	}
	report := fmt.Sprintf("seed: %d\nseverity: %s\nsummary: %s\n\n%s\n", f.seed, f.sev, f.summary, f.detail)
	return os.WriteFile(filepath.Join(dir, "report.txt"), []byte(report), 0o644)
}
