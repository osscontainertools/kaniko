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
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
	sevWarmerDiff
	sevCacheDiff
	sevBuildOutcome
	sevCrash
	sevSecretLeak
)

func (s severity) String() string {
	switch s {
	case sevDockerDiff:
		return "DOCKER_DIFF"
	case sevDeterminismDiff:
		return "DETERMINISM_DIFF"
	case sevInvarianceDiff:
		return "INVARIANCE_DIFF"
	case sevWarmerDiff:
		return "WARMER_DIFF"
	case sevCacheDiff:
		return "CACHE_DIFF"
	case sevBuildOutcome:
		return "BUILD_OUTCOME"
	case sevCrash:
		return "CRASH"
	case sevSecretLeak:
		return "SECRET_LEAK"
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
	// Full invocation, so a written finding can be replayed exactly. Without these a
	// finding's flags/env/context were lost and triage had to guess them.
	flags            []string
	envFlags         []string
	cacheCompression string
	cacheLocal       bool
	context          []fileSpec
}

// mergeKnown folds src into f.known without dropping counts already there (a counted
// known-crash carries its own map, so overwriting would lose it).
func mergeKnown(f *finding, src map[string]int) {
	if len(src) == 0 {
		return
	}
	if f.known == nil {
		f.known = map[string]int{}
	}
	for k, v := range src {
		f.known[k] += v
	}
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
// FUZZ_INVARIANCE=1 (also run the cache-lookahead on-vs-off invariance oracle),
// FUZZ_WARMER=1 (also run the warmed-vs-cold base-image-cache invariance oracle); each
// adds kaniko builds per case. Under parallelism a finding is reproduced from its written
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
	// A mirrored tag is reused only while it still holds the pinned digest, so repinning a
	// base takes effect instead of silently reusing the previous copy. crane copy preserves
	// the source digest, so the two are comparable; the skopeo-minted OCI base re-formats
	// by design and falls back to an existence check.
	mirror := func(name, src, dst string, copyArgs ...string) {
		have, err := RunCommandWithoutTest(exec.Command("crane", "digest", dst))
		if err == nil {
			_, want, pinned := strings.Cut(src, "@")
			if copyArgs[0] != "crane" || !pinned || strings.TrimSpace(string(have)) == want {
				return
			}
		}
		if out, err := RunCommandWithoutTest(exec.Command(copyArgs[0], copyArgs[1:]...)); err != nil {
			t.Fatalf("failed to mirror %s base %s -> %s: %v\n%s", name, src, dst, err, out)
		}
	}
	mirror("alpine", alpineFuzzBase, strings.ToLower(config.imageRepo+baseAlpineTag),
		"crane", "copy", alpineFuzzBase, strings.ToLower(config.imageRepo+baseAlpineTag))
	mirror("debian", debianFuzzBase, strings.ToLower(config.imageRepo+baseDebianTag),
		"crane", "copy", debianFuzzBase, strings.ToLower(config.imageRepo+baseDebianTag))
	// Second copies under the library/ paths a bare docker.io ref normalizes to, for the
	// registry-mirror oracle.
	mirror("library alpine", alpineFuzzBase, strings.ToLower(config.imageRepo+libraryAlpineTag),
		"crane", "copy", alpineFuzzBase, strings.ToLower(config.imageRepo+libraryAlpineTag))
	mirror("library debian", debianFuzzBase, strings.ToLower(config.imageRepo+libraryDebianTag),
		"crane", "copy", debianFuzzBase, strings.ToLower(config.imageRepo+libraryDebianTag))
	// The OCI base is minted from alpine with skopeo; --src-no-creds forces an anonymous
	// pull, avoiding a stale credential in the environment.
	mirror("oci", alpineFuzzBase, ociBase,
		"skopeo", "copy", "--src-no-creds", "--format", "oci", "--dest-tls-verify=false",
		"docker://"+alpineFuzzBase, "docker://"+ociBase)

	// Bases minted from an inline Dockerfile, each adding one property the upstream mirrors
	// do not have. All build on the mirrored alpine so none of them pulls from Docker Hub.
	alpineRef := strings.ToLower(config.imageRepo + baseAlpineTag)

	// ONBUILD triggers in the config, so a generated FROM exercises the image-config trigger
	// path (loaded from the base, not the Dockerfile). Context-free RUN so any child builds.
	mintBase(t, strings.ToLower(config.imageRepo+onbuildBaseTag), "FROM "+alpineRef+`
ONBUILD RUN mkdir -p /onbuild-base && echo fired > /onbuild-base/marker
`, false)

	// Several layers, pinned to docker v2. See multiLayerBaseTag.
	mintBase(t, strings.ToLower(config.imageRepo+multiLayerBaseTag), "FROM "+alpineRef+`
RUN mkdir -p /ml1 && echo one > /ml1/f
RUN mkdir -p /ml2 && echo two > /ml2/f
RUN mkdir -p /ml3 && echo three > /ml3/f
`, false)

	// Inheritable config fields. WORKDIR makes the child's relative WORKDIR draws resolve
	// against an inherited directory; ENTRYPOINT plus CMD exercises docker's rule that a
	// child ENTRYPOINT resets an inherited CMD. USER is set explicitly to root so the field
	// is inherited without dropping privileges.
	mintBase(t, strings.ToLower(config.imageRepo+richConfigBaseTag), "FROM "+alpineRef+`
ENV FUZZ_BASE_ENV=base-value
WORKDIR /basewd
USER root
LABEL fuzz.base.label=base-value
EXPOSE 9999
VOLUME /basevol
STOPSIGNAL SIGQUIT
HEALTHCHECK CMD /bin/true
ENTRYPOINT ["/bin/echo", "base-entry"]
CMD ["base-cmd"]
`, true)

	// Mint a base carrying OCI manifest annotations, so a generated FROM against it
	// exercises how base-image annotations are handled and propagated (build.go, mz507
	// FF_KANIKO_NO_PROPAGATE_ANNOTATIONS). crane mutate adds the annotations onto the OCI
	// base's manifest without rebuilding layers.
	annotBase := strings.ToLower(config.imageRepo + annotBaseTag)
	if _, err := RunCommandWithoutTest(exec.Command("crane", "manifest", annotBase)); err != nil {
		mutate := exec.Command("crane", "mutate", ociBase,
			"--annotation", "org.opencontainers.image.authors=fuzz",
			"--annotation", "fuzz.marker=annotated-base", "-t", annotBase)
		if out, err := RunCommandWithoutTest(mutate); err != nil {
			t.Fatalf("mint annotated base: %v\n%s", err, out)
		}
	}

	// Warm the base media-type cache single-threaded so the concurrent workers only
	// read it (baseIsOCI would otherwise write the map from many goroutines).
	for _, ref := range fuzzBaseRefs() {
		baseIsOCI(ref)
	}

	// Bring up the local TLS server for the https-context oracle once for the whole
	// Port layout first: every server address below is derived from it.
	resolveFuzzPorts()

	// campaign. If it fails to start the oracle stays disabled (httpsServedDir == "").
	if os.Getenv("FUZZ_HTTPSCONTEXT") == "1" {
		if err := startHTTPSContextServer(); err != nil {
			t.Logf("https-context oracle disabled: %v", err)
		} else {
			defer os.RemoveAll(httpsServedDir)
			t.Logf("https-context oracle enabled, serving %s", httpsBaseURL)
		}
	}

	// Bring up the ADD-url file server. Best-effort: if it fails, generated ADD-url lines
	// fail to fetch in both tools (sterile), not a false finding.
	if err := startAddURLServer(); err != nil {
		t.Logf("ADD-url server disabled: %v", err)
	} else {
		t.Logf("ADD-url server serving %s", addURL)
	}

	// OTLP sink for the tracing oracle. If the port cannot be bound the oracle asserts on a
	// collector that never answers, so leave it disabled rather than reporting false findings.
	if err := startOTLPSink(); err != nil {
		t.Logf("tracing oracle disabled: %v", err)
	} else {
		otlpUp = true
		t.Logf("OTLP trace sink listening on %s", otlpAddr)
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
		return &finding{
			seed: seed, sev: sev, summary: summary, detail: detail, dockerfile: gen.dockerfile,
			flags: gen.kanikoFlags, envFlags: gen.envFlags, cacheCompression: gen.cacheCompression, cacheLocal: gen.cacheLocal, context: gen.context,
		}
	}

	// crashOr inspects a build's output for a crash. A crash already filed (knownCrashes)
	// is counted, not reported, so it does not flood the campaign; a novel crash is
	// returned as a sevCrash finding. Returns nil when the output shows no crash.
	crashOr := func(out string) *finding {
		crash := detectCrash(out)
		if crash == "" {
			return nil
		}
		if name := knownCrashName(crash); name != "" {
			return &finding{seed: seed, sev: sevClean, known: map[string]int{name: 1}}
		}
		return fail(sevCrash, crash, out)
	}

	// kaniko mirrors the final base's media type, so tell docker to emit the same.
	dockerOut, dockerErr := runFuzzDocker(dir, dockerImage, dockerWantsOCI(gen), gen.buildArgs, gen.labels, gen.annotations, gen.target, gen.usesSecret)
	kanikoOut, kanikoErr := runFuzzKanikoEnv(dir, kanikoImage, gen.kanikoFlags, covDir, gen.envFlags)

	if f := crashOr(kanikoOut); f != nil {
		return f
	}
	switch {
	case dockerErr != nil && kanikoErr != nil:
		// Both builds failed. Not a divergence, and the case teaches us nothing.
		return &finding{seed: seed, sev: sevClean, summary: "sterile"}
	case dockerErr != nil && kanikoErr == nil:
		return fail(sevBuildOutcome, "docker failed, kaniko built", fmt.Sprintf("docker error: %v\n%s", dockerErr, dockerOut))
	case dockerErr == nil && kanikoErr != nil:
		for _, kf := range knownBuildFailures {
			if kf.match(kanikoOut) {
				return &finding{seed: seed, sev: sevClean, known: map[string]int{kf.name: 1}}
			}
		}
		return fail(sevBuildOutcome, "kaniko failed, docker built", kanikoOut)
	}

	// secret oracle: a build secret must never reach the pushed image. Scan the kaniko image
	// (the fuzzer's subject) and the docker image for the secret token; any occurrence in a
	// layer or the config is a leak. Runs only when the case declares a secret.
	if gen.usesSecret {
		if f := secretLeakOracle(seed, kanikoImage, fail); f != nil {
			return f
		}
		if f := secretLeakOracle(seed, dockerImage, fail); f != nil {
			f.summary = "secret leaked into the docker image (buildkit)"
			return f
		}
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
		v0 := GetVersionedKanikoImage(config.imageRepo, label, 0)
		v1 := GetVersionedKanikoImage(config.imageRepo, label, 1)
		// The cache backend is part of the search space: a registry repo, or an on-disk
		// OCI layout (pkg/cache LayoutCache) when gen.cacheLocal. The backend must not
		// change the built image, so the same populate-consume comparison applies. The
		// layout persists in a host dir bind-mounted at /cache across the two builds.
		cacheRepo := strings.ToLower(config.imageRepo + "fuzzcache-" + label)
		cacheHost := ""
		if gen.cacheLocal {
			d, err := os.MkdirTemp("", "kaniko-fuzz-layoutcache-")
			if err == nil {
				cacheHost = d
				defer os.RemoveAll(cacheHost)
				cacheRepo = "oci:/cache/layout"
			}
		}
		// --cache-copy-layers is required for COPY layers to be served from cache;
		// without it the consume build re-runs COPY and restamps wall-clock mtimes.
		cacheArgs := append([]string{"--cache=true", "--cache-copy-layers=true", "--cache-repo=" + cacheRepo}, gen.kanikoFlags...)
		// Compression is applied only to the cache builds (both v0 and v1), never the
		// docker-compared fresh build, so a zstd layer media type is not mistaken for a
		// docker-parity diff. Both sides match, so the cache comparison stays strict.
		if gen.cacheCompression != "" {
			cacheArgs = append(cacheArgs, "--compression="+gen.cacheCompression)
		}
		// runCache builds one side of the cache comparison against the chosen backend, at
		// the same per-case GOCOVERDIR as the fresh build (sequential, so it accumulates).
		runCache := func(image, cov string) (string, error) {
			if cacheHost != "" {
				return runFuzzKanikoCache(dir, image, cacheArgs, cov, cacheHost, gen.envFlags)
			}
			return runFuzzKanikoEnv(dir, image, cacheArgs, cov, gen.envFlags)
		}
		c0, ce0 := runCache(v0, covDir)
		if f := crashOr(c0); f != nil {
			return f
		}
		c1, ce1 := runCache(v1, covDir)
		if f := crashOr(c1); f != nil {
			return f
		}

		if ce0 != nil || ce1 != nil {
			// A cache-build failure matching a known, filed failure is counted, not
			// reported, so it does not flood the campaign.
			for _, kf := range knownBuildFailures {
				if kf.match(c0) || kf.match(c1) {
					f := &finding{seed: seed, sev: sevClean, known: map[string]int{kf.name: 1}}
					mergeKnown(f, dockerCls.known)
					return f
				}
			}
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
		// But diffoci compares through the docker daemon (the insecure registry blocks
		// diffoci's direct read), which can misread a layer under concurrent load. So a
		// diff is diagnosed against registry ground truth before it is trusted: a diff
		// that does not reproduce is a measurement artifact, counted not reported.
		if !cacheSame {
			real, evidence := diagnoseCacheDiff(v0, v1, cacheIgnores, cacheDiff)
			if real {
				// The mz876 snapshot map-leak: a cache diff on a cross-stage case, or any
				// case with --cache-run-layers=false (which forces the consume RUN to
				// re-snapshot after a cached COPY whose files never entered the map). Filed;
				// count, do not report, so novel cache bugs stay visible.
				if isCrossStage(gen.dockerfile) || hasFlag(gen.kanikoFlags, "--cache-run-layers=false") {
					f := &finding{seed: seed, sev: sevClean, known: map[string]int{mz876CrossStage: 1}}
					mergeKnown(f, dockerCls.known)
					return f
				}
				f := fail(sevCacheDiff, "cache populate and consume differ", cacheDiff+"\n\n[diagnosis]\n"+evidence)
				f.known = dockerCls.known
				return f
			}
			t.Logf("[%s] cache diff did not reproduce, treating as measurement artifact:\n%s", label, evidence)
		}

		// invariance oracle: cache-lookahead is a performance optimization and must not
		// change output. Build the same case with --cache and the lookahead family of
		// flags off, then compare the cache-consume image to the lookahead-on one (v1).
		// A divergence is a lookahead bug (mz872 is the crashing variant; this catches
		// the silent variant where a wrong cached layer is served). Gated: adds builds.
		if os.Getenv("FUZZ_INVARIANCE") == "1" {
			// Keep the case's fuzzed env flags on both sides so only the lookahead family
			// differs; docker keeps the last -e, so the =0 overrides win over KanikoEnv.
			offEnv := append(append([]string{}, gen.envFlags...), "FF_KANIKO_CACHE_LOOKAHEAD=0", "FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY=0", "FF_KANIKO_RESOLVE_CACHE_KEY=0")
			// Mirror the on-side's cache backend so the comparison isolates lookahead: a
			// local-layout on-side must compare against a local-layout off-side, not a
			// registry one.
			offRepo := strings.ToLower(config.imageRepo + "fuzzcache-off-" + label)
			offHost := ""
			if gen.cacheLocal {
				d, err := os.MkdirTemp("", "kaniko-fuzz-layoutcache-off-")
				if err == nil {
					offHost = d
					defer os.RemoveAll(offHost)
					offRepo = "oci:/cache/layout"
				}
			}
			offArgs := append([]string{"--cache=true", "--cache-copy-layers=true", "--cache-repo=" + offRepo}, gen.kanikoFlags...)
			// Match the on-side's compression so the invariance comparison (v1 vs w1) does
			// not flag a media-type difference instead of a real lookahead divergence.
			if gen.cacheCompression != "" {
				offArgs = append(offArgs, "--compression="+gen.cacheCompression)
			}
			runOff := func(image string) (string, error) {
				if offHost != "" {
					return runFuzzKanikoCache(dir, image, offArgs, "", offHost, offEnv)
				}
				return runFuzzKanikoEnv(dir, image, offArgs, "", offEnv)
			}
			w0 := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-off0")
			w1 := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-off1")
			defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", w0, w1))
			o0, oe0 := runOff(w0)
			if f := crashOr(o0); f != nil {
				return f
			}
			o1, oe1 := runOff(w1)
			if f := crashOr(o1); f != nil {
				return f
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
		if f := determinismOracle(label, dir, kanikoImage, gen.kanikoFlags, gen.envFlags, fail, crashOr); f != nil {
			// A cross-stage reproducible-determinism diff is the filed mz876 class (load
			// dependent); count, do not report, so novel nondeterminism stays visible.
			if f.sev == sevDeterminismDiff && isCrossStage(gen.dockerfile) {
				kf := &finding{seed: seed, sev: sevClean, known: map[string]int{mz876CrossStage: 1}}
				mergeKnown(kf, dockerCls.known)
				return kf
			}
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// warmer oracle: the cache warmer pre-populates the base-image cache, a pure
	// performance step that must not change the built image. Build the case with a
	// warmed --cache-dir and again with a cold one, identical flags otherwise, and
	// compare. A divergence is a warmer bug (wrong, corrupted, or restamped base
	// layer served from cache). Gated behind FUZZ_WARMER because it adds a warmer run
	// plus two cache-dir builds per case.
	if os.Getenv("FUZZ_WARMER") == "1" {
		if f := warmerOracle(label, dir, gen.dockerfile, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// tar-context oracle: the build context can be given as a directory or a tar
	// (pkg/buildcontext), and the built image must be identical either way. Build the
	// case from a tar context and compare to the fresh dir-context image. A divergence
	// is a build-context bug. Gated behind FUZZ_TARCONTEXT; adds a build and exercises
	// pkg/buildcontext, which the default local-dir path never reaches.
	if os.Getenv("FUZZ_TARCONTEXT") == "1" {
		if f := tarContextOracle(seed, label, dir, kanikoImage, gen.dockerfile, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// https-context oracle: the same invariance over pkg/buildcontext's HTTPSTar handler,
	// fetching the context as an https:// tarball from the campaign's local TLS server.
	// Gated behind FUZZ_HTTPSCONTEXT and only if the server came up.
	if os.Getenv("FUZZ_HTTPSCONTEXT") == "1" && httpsServedDir != "" {
		if f := httpsContextOracle(seed, label, dir, kanikoImage, gen.dockerfile, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// two-step oracle: build to a tarball (--no-push --tar-path) then push it separately must
	// equal a direct build+push. Gated behind FUZZ_TWOSTEP; adds a build plus a crane push.
	if os.Getenv("FUZZ_TWOSTEP") == "1" {
		if f := twoStepPushOracle(seed, label, dir, kanikoImage, gen.dockerfile, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// dockerfile-http oracle: fetching the Dockerfile over http (-f <url>) must produce the
	// same image as a local Dockerfile. Gated behind FUZZ_DFHTTP; reuses the ADD-url server.
	if os.Getenv("FUZZ_DFHTTP") == "1" {
		if f := dockerfileHTTPOracle(seed, label, dir, kanikoImage, gen.dockerfile, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// digest-file oracle: --digest-file must record the digest the registry actually stored.
	// Gated behind FUZZ_DIGESTFILE; adds one build per case.
	if os.Getenv("FUZZ_DIGESTFILE") == "1" {
		if f := digestFileOracle(seed, label, dir, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// context-sub-path oracle: the same context nested one level down and reached with
	// --context-sub-path must give the same image. Gated behind FUZZ_SUBPATHCONTEXT.
	if os.Getenv("FUZZ_SUBPATHCONTEXT") == "1" {
		if f := subPathContextOracle(seed, label, dir, kanikoImage, gen.dockerfile, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// stdin-context oracle: the context piped in as a gzipped tar must give the same image as
	// the dir context. Gated behind FUZZ_STDINCONTEXT; adds a build and a tar per case.
	if os.Getenv("FUZZ_STDINCONTEXT") == "1" {
		if f := stdinContextOracle(seed, label, dir, kanikoImage, gen.dockerfile, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// tracing oracle: OTLP tracing is instrumentation, so the traced image must equal the
	// untraced one, and the collector must actually receive spans. Gated behind FUZZ_TRACING
	// and only if the sink bound its port.
	if os.Getenv("FUZZ_TRACING") == "1" && otlpUp {
		if f := tracingOracle(seed, label, dir, kanikoImage, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// registry-mirror oracle: the same case built from docker.io refs with the mirror
	// redirecting to the campaign registry must give the same image. Gated behind
	// FUZZ_REGISTRYMIRROR; only fires on cases whose bases are all alpine or debian.
	if os.Getenv("FUZZ_REGISTRYMIRROR") == "1" {
		if f := registryMirrorOracle(seed, label, dir, kanikoImage, gen.dockerfile, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// dryrun oracle: --dryrun renders the plan and must not push (mz992). Gated behind
	// FUZZ_DRYRUN; adds one build per case, and no registry write to clean up.
	if os.Getenv("FUZZ_DRYRUN") == "1" {
		if f := dryrunOracle(label, dir, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// shared-cache oracle: two different-arg builds sharing one --cache-repo must not leak
	// layers between each other, even when their composite keys collide (mz873), and the
	// cache-lookahead precompute must not mis-resolve a colliding cross-stage pointer
	// (mz872). Gated behind FUZZ_SHAREDCACHE; only runs on cases that declare >=2 args.
	if os.Getenv("FUZZ_SHAREDCACHE") == "1" {
		if f := sharedCacheOracle(seed, label, dir, gen.dockerfile, gen.argNames, gen.kanikoFlags, gen.envFlags, covDir, fail, crashOr); f != nil {
			mergeKnown(f, dockerCls.known)
			return f
		}
	}

	// chaos-flags build: one extra build with a random subset of internal FF_KANIKO_*
	// toggles flipped on or off, purely to exercise flag-gated branches for coverage. No
	// image comparison, so non-output-neutral flips cannot pollute the parity oracles; only
	// a novel crash is a finding. Gated behind FUZZ_CHAOSFLAGS.
	if os.Getenv("FUZZ_CHAOSFLAGS") == "1" && len(gen.chaosEnv) > 0 {
		chaosImg := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-chaos")
		defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", chaosImg))
		cout, _ := runFuzzKanikoEnv(dir, chaosImg, gen.kanikoFlags, covDir, gen.chaosEnv)
		if f := crashOr(cout); f != nil {
			// Record the chaos toggles so the crash is reproducible.
			f.envFlags = append(append([]string{}, gen.envFlags...), gen.chaosEnv...)
			f.detail = "[chaos flags]\n" + strings.Join(gen.chaosEnv, " ") + "\n\n" + f.detail
			mergeKnown(f, dockerCls.known)
			return f
		}
		// coverage-only otherwise: build success or plain failure is not a finding.
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
func determinismOracle(label, dir, fresh string, flags, envFlags []string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	repo := config.imageRepo

	// Mode 1, structural: a second fresh build must match the first apart from
	// timestamps. Any difference in layers, files, mode, ownership, or media type
	// between two runs of the identical input is nondeterminism.
	b := strings.ToLower(repo + kanikoPrefix + label + "-det-b")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", b))
	out, err := runFuzzKanikoEnv(dir, b, flags, "", envFlags)
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
	o0, e0 := runFuzzKanikoEnv(dir, r0, append([]string{"--reproducible"}, flags...), "", envFlags)
	if f := crashOr(o0); f != nil {
		return f
	}
	o1, e1 := runFuzzKanikoEnv(dir, r1, append([]string{"--reproducible"}, flags...), "", envFlags)
	if f := crashOr(o1); f != nil {
		return f
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

// warmerOracle builds the case with a warmed base-image cache and again with a cold
// one, identical flags otherwise, and checks the two images match. The warmer only
// pre-fetches base layers, so the output must be invariant to it. Returns a finding on
// any divergence, else nil. It removes the extra images and cache dirs it creates.
func warmerOracle(label, dir, dockerfile string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	bases := basesInDockerfile(dockerfile)
	if len(bases) == 0 {
		// Every FROM resolves to an earlier stage, so there is no external base to warm.
		return nil
	}
	warmCache, err := os.MkdirTemp("", "kaniko-fuzz-warm-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(warmCache)
	coldCache, err := os.MkdirTemp("", "kaniko-fuzz-cold-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(coldCache)

	if out, werr := warmFuzzCache(warmCache, bases, covDir, envFlags); werr != nil {
		// docker and the fresh build already succeeded, so a warmer that cannot cache
		// the base it was given is itself the finding.
		return fail(sevWarmerDiff, "warmer failed to populate cache", out)
	}

	// --cache-run-layers=false and --no-push-cache keep the layer cache out of the
	// picture; only the base-image cache-dir differs between the two builds. Both
	// invocations are identical apart from whether that dir was pre-warmed.
	cacheFlags := append([]string{"--cache=true", "--cache-run-layers=false", "--no-push-cache"}, flags...)
	warmImg := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-warm")
	coldImg := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-cold")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", warmImg, coldImg))

	wOut, wErr := runFuzzKanikoCache(dir, warmImg, cacheFlags, covDir, warmCache, envFlags)
	if f := crashOr(wOut); f != nil {
		return f
	}
	cOut, cErr := runFuzzKanikoCache(dir, coldImg, cacheFlags, "", coldCache, envFlags)
	if f := crashOr(cOut); f != nil {
		return f
	}
	if wErr != nil && cErr == nil {
		return fail(sevWarmerDiff, "warmed build failed while cold build succeeded", wOut)
	}
	if wErr != nil || cErr != nil {
		// Both cache-dir builds failed the same way. Not a warmer divergence.
		return nil
	}

	// Mode 1, structural: the two builds run at different wall-clock times, so
	// RUN-created file mtimes differ regardless of the warmer. Compare content, mode,
	// ownership, and structure exactly; a warmer bug (wrong or corrupted base layer)
	// shows there.
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(warmImg, coldImg, ignores)
	if !same {
		return fail(sevWarmerDiff, "warmed and cold builds differ", diff)
	}

	// Mode 2, reproducible: with --reproducible the timestamps are pinned, so warm and
	// cold can be compared byte-strict (only the image name excused). Combined with
	// FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS (set in KanikoEnv), the base layers
	// are carried through as-is, so this catches a warmer that stores a base layer with
	// even a byte's difference from a fresh pull, which the timestamp-tolerant mode
	// above would hide. Both builds apply the same flags, so any rewrite is identical.
	reproCold, err := os.MkdirTemp("", "kaniko-fuzz-cold-repro-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(reproCold)
	reproFlags := append([]string{"--reproducible"}, cacheFlags...)
	warmRepro := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-warm-repro")
	coldRepro := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-cold-repro")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", warmRepro, coldRepro))

	// warmCache is already populated, so the warm reproducible build reads the base
	// from it; the cold one gets an empty dir and pulls the base fresh.
	rwOut, rwErr := runFuzzKanikoCache(dir, warmRepro, reproFlags, covDir, warmCache, envFlags)
	if f := crashOr(rwOut); f != nil {
		return f
	}
	rcOut, rcErr := runFuzzKanikoCache(dir, coldRepro, reproFlags, "", reproCold, envFlags)
	if f := crashOr(rcOut); f != nil {
		return f
	}
	if rwErr != nil && rcErr == nil {
		return fail(sevWarmerDiff, "warmed reproducible build failed while cold built", rwOut)
	}
	if rwErr != nil || rcErr != nil {
		return nil
	}
	diff, same, _ = runFuzzDiffoci(warmRepro, coldRepro, []string{"--ignore-image-name"})
	if !same {
		return fail(sevWarmerDiff, "warmed and cold reproducible builds differ (byte-strict)", diff)
	}
	return nil
}

// basesInDockerfile returns the distinct external base images the Dockerfile pulls,
// skipping FROM lines that reference an earlier stage. Only the mirrored fuzz bases
// are cacheable, so the set is intersected with them.
func basesInDockerfile(dockerfile string) []string {
	known := map[string]bool{}
	for _, ref := range fuzzBaseRefs() {
		known[ref] = true
	}
	seen := map[string]bool{}
	var out []string
	for line := range strings.SplitSeq(dockerfile, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "FROM" {
			continue
		}
		ref := f[1]
		if known[ref] && !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// warmFuzzCache runs the kaniko warmer to populate cacheDir with the given base
// images. covDir, if set, collects the warmer binary's coverage. --force overwrites
// any stale entry so a reused cache dir cannot mask a warmer bug.
//
// The case's envFlags go to the warmer as well as to the executor. FF_KANIKO_OCI_WARMER
// picks the on-disk cache format in the warmer and the read path in the executor, so the
// two have to be given the same value or the warm build just misses a cache it cannot read.
func warmFuzzCache(cacheDir string, bases []string, covDir string, envFlags []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host", "-v", cacheDir + ":/cache"}
	for _, e := range envFlags {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, WarmerImage, "--cache-dir=/cache", "--force")
	for _, b := range bases {
		flags = append(flags, "-i", b)
	}
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
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
			if err := writeTarFixture(p, false); err != nil {
				return err
			}
		case kindTarGz:
			if err := writeTarFixture(p, true); err != nil {
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

// writeTarFixture writes a small deterministic tar so ADD can auto-extract it. When gz is
// set the tar is gzip-wrapped (.tar.gz), exercising kaniko's compressed-archive extraction.
func writeTarFixture(path string, gz bool) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var w io.Writer = f
	if gz {
		zw := gzip.NewWriter(f)
		defer zw.Close()
		w = zw
	}
	tw := tar.NewWriter(w)
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
	ociFuzzBaseTag = "fuzz-oci-base:latest"     // OCI media type, minted from alpine
	baseAlpineTag  = "fuzz-base-alpine:latest"  // docker v2, single layer
	baseDebianTag  = "fuzz-base-debian:latest"  // OCI index, glibc, passwd db
	onbuildBaseTag = "fuzz-onbuild-base:latest" // alpine + baked-in ONBUILD triggers
	annotBaseTag   = "fuzz-annot-base:latest"   // OCI base carrying manifest annotations

	// Every other base resolves to a single layer, which leaves the layer-plural paths
	// exercised only in their degenerate form: relabelLayers runs its loop body once, and
	// FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS has no plural base layers to preserve.
	// This one stacks several RUN layers on alpine, and is pinned to docker v2, which also
	// rebalances a base pool that was otherwise four-to-one OCI.
	multiLayerBaseTag = "fuzz-multilayer-base:latest"

	// A base whose config carries the inheritable fields, so a child Dockerfile exercises
	// config inheritance and the override-versus-merge rules rather than only the fields it
	// sets itself. Deliberately no non-root USER: every generated stage writes to absolute
	// paths at the root, so a base that dropped privileges would make every case using it
	// fail in both tools and count as sterile.
	richConfigBaseTag = "fuzz-config-base:latest"

	// Docker-Hub-normalized copies of the two upstream bases, mirrored a second time under
	// the repository path a bare docker.io ref resolves to ("alpine" -> library/alpine).
	// They let the registry-mirror oracle build a case from a real docker.io reference and
	// have --registry-mirror redirect it here, which is how a mirror is actually used. crane
	// copy preserves the digest, so the redirected pull yields the identical image and the
	// oracle stays an invariance check rather than a comparison of two different bases.
	libraryAlpineTag = "library/alpine:fuzz"
	libraryDebianTag = "library/debian:fuzz"
)

// mintBase builds a base from an inline Dockerfile and pushes it, unless the tag is already
// in the registry. The manifest format is pinned rather than inherited: `docker build -t`
// follows the daemon default, which is OCI wherever the containerd image store is enabled,
// so a base meant to be docker v2 would silently come out OCI.
func mintBase(t *testing.T, ref, dockerfile string, oci bool) {
	if _, err := RunCommandWithoutTest(exec.Command("crane", "manifest", ref)); err == nil {
		return
	}
	dir, err := os.MkdirTemp("", "kaniko-fuzz-mintbase-")
	if err != nil {
		t.Fatalf("mint %s: %v", ref, err)
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("mint %s: %v", ref, err)
	}
	ociFlag := "false"
	if oci {
		ociFlag = "true"
	}
	cmd := exec.Command("docker", "buildx", "build", "--no-cache", "--provenance=false",
		"--output=type=image,name="+ref+",oci-mediatypes="+ociFlag+",push=true", dir)
	if out, err := RunCommandWithoutTest(cmd); err != nil {
		t.Fatalf("mint %s: %v\n%s", ref, err, out)
	}
}

func fuzzBaseRefs() []string {
	repo := config.imageRepo
	return []string{
		strings.ToLower(repo + baseAlpineTag),
		strings.ToLower(repo + baseDebianTag),
		strings.ToLower(repo + ociFuzzBaseTag),
		strings.ToLower(repo + onbuildBaseTag),
		strings.ToLower(repo + annotBaseTag),
		strings.ToLower(repo + multiLayerBaseTag),
		strings.ToLower(repo + richConfigBaseTag),
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

// dockerWantsOCI decides whether docker should emit OCI media types so its image matches
// kaniko's. For a normal base kaniko mirrors the base's media type. FROM scratch has no
// base, so kaniko's output type is set by FF_KANIKO_OCI_SCRATCH_BASE, which the case
// carries in its env flags; match that instead of probing a nonexistent base ref.
func dockerWantsOCI(gen genResult) bool {
	// An explicit --image-format overrides whatever the base carries: kaniko relabels the
	// manifest, config and layers to that vendor, so docker has to emit the same vendor or
	// the oracle reports a format difference the flag asked for.
	if hasFlag(gen.kanikoFlags, "--image-format=oci") {
		return true
	}
	if hasFlag(gen.kanikoFlags, "--image-format=docker") {
		return false
	}
	ref := finalBaseRef(gen.dockerfile, gen.target, gen.buildArgs)
	if ref == "scratch" {
		return scratchEnvOCI(gen.envFlags)
	}
	return baseIsOCI(ref)
}

// scratchEnvOCI reports the effective FF_KANIKO_OCI_SCRATCH_BASE for the case, defaulting
// to true because KanikoEnv sets it on; a case's env override (=0) wins as docker keeps
// the last value.
func scratchEnvOCI(envFlags []string) bool {
	oci := true
	for _, e := range envFlags {
		if e == "FF_KANIKO_OCI_SCRATCH_BASE=1" {
			oci = true
		}
		if e == "FF_KANIKO_OCI_SCRATCH_BASE=0" {
			oci = false
		}
	}
	return oci
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

// finalBaseRef returns the external image ref that determines the built image's media
// type, which docker must be told to match. With --target the built image is that
// stage, not the last FROM, so target selects the starting ref. A FROM may name an
// earlier stage (FROM stageJ), so the alias chain is followed back to the external base
// whose media type actually flows through; otherwise a stage-name ref would misdetect.
func finalBaseRef(dockerfile, target string, buildArgs []string) string {
	args := map[string]string{}  // ARG NAME=default, then --build-arg overrides on top
	alias := map[string]string{} // stage name -> the raw ref it was built FROM
	ref := ""
	for line := range strings.SplitSeq(dockerfile, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "ARG" {
			if k, v, ok := strings.Cut(f[1], "="); ok {
				args[k] = v
			}
			continue
		}
		if len(f) < 2 || f[0] != "FROM" {
			continue
		}
		ref = f[1] // raw, may be $NAME or a stage name
		if len(f) >= 4 && strings.EqualFold(f[2], "AS") {
			alias[f[3]] = f[1]
		}
	}
	// A --build-arg overrides the ARG default, so a FROM $SBASE base resolves to the
	// overridden image; apply overrides after the defaults so they win.
	for _, kv := range buildArgs {
		if k, v, ok := strings.Cut(kv, "="); ok {
			args[k] = v
		}
	}
	// With --target, start from that stage's own FROM ref instead of the last FROM.
	if target != "" {
		if base, ok := alias[target]; ok {
			ref = base
		}
	}
	// Resolve $ARG substitution and stage aliases until an external ref is reached.
	for i := 0; i < 16; i++ {
		if strings.HasPrefix(ref, "$") {
			if v, ok := args[strings.Trim(ref[1:], "{}")]; ok {
				ref = v
				continue
			}
			break
		}
		if next, ok := alias[ref]; ok && next != ref {
			ref = next
			continue
		}
		break
	}
	return ref
}

// runFuzzDocker builds and pushes with docker. --provenance=false drops attestation
// entries and yields a docker v2 manifest; oci adds oci-mediatypes=true to emit OCI
// instead, matching whatever media type the final base carries so the comparison
// does not flag a format difference the base itself dictated.
func runFuzzDocker(contextDir, image string, oci bool, buildArgs, labels, annotations []string, target string, usesSecret bool) (string, error) {
	argFlags := []string{}
	for _, kv := range buildArgs {
		argFlags = append(argFlags, "--build-arg", kv)
	}
	for _, kv := range labels {
		argFlags = append(argFlags, "--label", kv)
	}
	for _, kv := range annotations {
		argFlags = append(argFlags, "--annotation", kv)
	}
	if target != "" {
		argFlags = append(argFlags, "--target", target)
	}
	// Same --secret spec as kaniko; the value comes from the command environment set below.
	if usesSecret {
		argFlags = append(argFlags, secretFlag)
	}
	// Force the manifest media type explicitly rather than trusting the daemon default:
	// with the containerd image store a plain `docker build -t` emits OCI even for a
	// docker-v2 base, which diverges from kaniko (which mirrors the base). Set
	// oci-mediatypes to match the base kaniko mirrors, so the parity oracle is not
	// swamped by a spurious media-type diff.
	ociFlag := "false"
	if oci {
		ociFlag = "true"
	}
	// buildkit's cache is left on. Its keys are content-based, so a hit means the inputs were
	// identical, and the parity comparison ignores file timestamps anyway (a cached layer
	// carries the earlier build's mtimes, and docker and kaniko never agree on those regardless
	// since they build seconds apart). Coverage-guided generation is what makes this pay:
	// mutateInput copies a corpus parent and flips a few bytes, and the generator reads the
	// byte stream in order, so a child shares its parent's Dockerfile up to the earliest flip.
	// The cheapest and most common mutation, a single late flip, preserves the longest prefix.
	cmd := append([]string{"build", "--provenance=false"}, argFlags...)
	cmd = append(cmd, "--output", "type=image,name="+image+",oci-mediatypes="+ociFlag+",push=true", contextDir)
	c := exec.Command("docker", cmd...)
	if usesSecret {
		c.Env = append(os.Environ(), secretEnvVar+"="+secretToken)
	}
	out, err := RunCommandWithoutTest(c)
	return string(out), err
}

// runFuzzKaniko mirrors buildKanikoImage's invocation (full KanikoEnv, executor
// image, read-only context mount) but returns the raw output so the harness can
// detect crashes and classify failures itself. When covDir is non-empty it is
// mounted as GOCOVERDIR so the build's coverage can be measured in isolation.
func runFuzzKaniko(contextDir, image string, extra []string, covDir string) (string, error) {
	return runFuzzKanikoEnv(contextDir, image, extra, covDir, nil)
}

// runFuzzKanikoCache is runFuzzKaniko with a host directory bind-mounted at /cache for
// the base-image cache. The caller passes --cache-dir=/cache in extra. Used by the
// warmer oracle so the executor reads the base layers the warmer wrote there.
func runFuzzKanikoCache(contextDir, image string, extra []string, covDir, cacheDir string, envOverride []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host", "-v", contextDir + ":/workspace:ro", "-v", cacheDir + ":/cache"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, ExecutorImage,
		"-f", path.Join(buildContextPath, "Dockerfile"),
		"-d", image,
		"-c", buildContextPath,
		"--cache-dir=/cache",
	)
	flags = append(flags, extra...)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// runFuzzKanikoEnv is runFuzzKaniko with extra -e env vars appended after KanikoEnv.
// docker keeps the last value for a repeated -e, so an override here wins over the
// KanikoEnv default (used to flip FF_KANIKO_* flags off for the invariance oracle).
func runFuzzKanikoEnv(contextDir, image string, extra []string, covDir string, envOverride []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host", "-v", contextDir + ":/workspace:ro"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, ExecutorImage,
		"-f", path.Join(buildContextPath, "Dockerfile"),
		"-d", image,
		"-c", buildContextPath,
	)
	flags = append(flags, extra...)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// copyFuzzContext duplicates a generated build context into dst and replaces the Dockerfile
// with the given content. cp -a is used rather than a file walk because the generator puts
// setuid bits, symlinks and hardlinks in the context on purpose, and a naive copy flattens
// exactly the attributes the oracles are comparing.
func copyFuzzContext(src, dst, dockerfile string) error {
	if out, err := RunCommandWithoutTest(exec.Command("cp", "-a", src+"/.", dst)); err != nil {
		return fmt.Errorf("copy context: %w: %s", err, out)
	}
	return os.WriteFile(filepath.Join(dst, "Dockerfile"), []byte(dockerfile), 0o644)
}

// prepareTarContext writes a gzipped tar of the context dir's contents (Dockerfile and
// context files) into the dir as .buildctx.tar.gz, for use as a tar:// build context.
// The tar is built at a temp path first so it does not contain itself.
func prepareTarContext(dir string) error {
	tmp, err := os.CreateTemp("", "kaniko-fuzz-tarctx-*.tar.gz")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	if out, err := RunCommandWithoutTest(exec.Command("tar", "czf", tmp.Name(), "-C", dir, ".")); err != nil {
		return fmt.Errorf("tar context: %w: %s", err, out)
	}
	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".buildctx.tar.gz"), data, 0o644)
}

// runFuzzKanikoStdin builds with the context piped to the executor's stdin as a gzipped tar
// (--context=tar://stdin). docker run needs -i or the container gets a TTY on stdin and
// kaniko rejects it as "no data found". .buildctx.tar.gz must already be in contextDir; it
// is fed as the process stdin rather than mounted, so this is the one context path that
// never touches a volume. No -v of the context dir: mounting it would let the build read
// the real files and mask a bug in the stdin unpack.
func runFuzzKanikoStdin(contextDir, image string, extra []string, covDir string, envOverride []string) (string, error) {
	tarball, err := os.Open(filepath.Join(contextDir, ".buildctx.tar.gz"))
	if err != nil {
		return "", err
	}
	defer tarball.Close()
	flags := []string{"run", "--rm", "-i", "--net=host"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, ExecutorImage,
		"-f", "Dockerfile",
		"-d", image,
		"-c", "tar://stdin",
	)
	flags = append(flags, extra...)
	cmd := exec.Command("docker", flags...)
	cmd.Stdin = tarball
	out, err := RunCommandWithoutTest(cmd)
	return string(out), err
}

// stdinContextOracle feeds the case's context to the executor over stdin and requires the
// image to match the dir-context build. This is the only build context that arrives as a
// stream with no filesystem behind it, and it is the uncovered half of
// Tar.UnpackTarFromBuildContext, which the tar:// oracle never reaches because that path
// takes the file branch.
func stdinContextOracle(seed int64, label, dir, dirImage, dockerfile string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	if err := prepareTarContext(dir); err != nil {
		return nil
	}
	img := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-stdin")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", img))
	out, err := runFuzzKanikoStdin(dir, img, flags, covDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		return fail(sevInvarianceDiff, "stdin-context build failed while the dir-context build succeeded", out)
	}
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(dirImage, img, ignores)
	if !same {
		return classifyContextDiff(seed, dockerfile, dirImage, img, ignores, diff, "dir-context and stdin-context builds differ", fail)
	}
	return nil
}

// runFuzzKanikoSubPath builds with the context nested one directory down and reached via
// --context-sub-path, which kaniko resolves by joining it onto the context path
// (cmd/executor/cmd/root.go). The Dockerfile lives inside the sub directory too, so every
// relative source in it has to resolve against the joined root rather than the mount point.
func runFuzzKanikoSubPath(contextDir, image, sub string, extra []string, covDir string, envOverride []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host", "-v", contextDir + ":/workspace:ro"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, ExecutorImage,
		"-f", path.Join(buildContextPath, sub, "Dockerfile"),
		"-d", image,
		"-c", buildContextPath,
		"--context-sub-path="+sub,
	)
	flags = append(flags, extra...)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// subPathContextOracle nests the whole context one directory deeper and builds it through
// --context-sub-path, requiring the same image as the flat build. Every relative COPY source
// in the case then has to resolve against the joined root, so a subpath that is applied to
// the context but not to a source lookup (or applied twice) shows up as a missing or
// misplaced file rather than staying silent.
func subPathContextOracle(seed int64, label, dir, dirImage, dockerfile string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	nested, err := os.MkdirTemp("", "kaniko-fuzz-subpath-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(nested)
	const sub = "ctxroot"
	if err := os.MkdirAll(filepath.Join(nested, sub), 0o755); err != nil {
		return nil
	}
	if err := copyFuzzContext(dir, filepath.Join(nested, sub), dockerfile); err != nil {
		return nil
	}
	img := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-subpath")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", img))
	out, err := runFuzzKanikoSubPath(nested, img, sub, flags, covDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		// mz1026: --context-sub-path is ignored when the context is a local directory.
		// resolveSourceContext returns early for any context without a "://" scheme, before
		// the ctxSubPath join, so FileContext.Root stays at the mount root and every relative
		// COPY source resolves one level too high. Counted rather than reported until that is
		// fixed, otherwise this one bug is the only thing the oracle can ever say. Remove this
		// skip to re-arm the oracle against a regression.
		// Returns nil rather than a clean finding so the oracles after this one still run;
		// a finding of any severity short-circuits the rest of the case.
		if strings.Contains(out, "failed to get files used from context") && strings.Contains(out, buildContextPath+"/") {
			return nil
		}
		return fail(sevInvarianceDiff, "context-sub-path build failed while the flat-context build succeeded", out)
	}
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(dirImage, img, ignores)
	if !same {
		return classifyContextDiff(seed, dockerfile, dirImage, img, ignores, diff, "flat-context and context-sub-path builds differ", fail)
	}
	return nil
}

// runFuzzKanikoTar builds with the context supplied as a tar:// archive rather than a
// local dir, exercising pkg/buildcontext's Tar handler. .buildctx.tar.gz must already
// be in contextDir; the Dockerfile is resolved relative to the unpacked tar.
func runFuzzKanikoTar(contextDir, image string, extra []string, covDir string, envOverride []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host", "-v", contextDir + ":/workspace:ro"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, ExecutorImage,
		"-f", "Dockerfile",
		"-d", image,
		"-c", "tar://"+path.Join(buildContextPath, ".buildctx.tar.gz"),
	)
	flags = append(flags, extra...)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// tarContextOracle builds the case from a tar:// context and checks it matches the
// dir-context image. The two contexts hold identical content, so the images must be
// identical apart from image name and timestamps; a diff is a build-context bug.
func tarContextOracle(seed int64, label, dir, dirImage, dockerfile string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	if err := prepareTarContext(dir); err != nil {
		return nil
	}
	tarImage := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-tarctx")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", tarImage))
	out, err := runFuzzKanikoTar(dir, tarImage, flags, covDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		// The dir build already succeeded, so a tar build that fails is a finding.
		return fail(sevInvarianceDiff, "tar-context build failed while dir-context built", out)
	}
	// Two independent builds at different times, so file mtimes differ; compare content,
	// mode, ownership, and structure exactly.
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(dirImage, tarImage, ignores)
	if !same {
		return classifyContextDiff(seed, dockerfile, dirImage, tarImage, ignores, diff, "dir-context and tar-context builds differ", fail)
	}
	return nil
}

// runFuzzKanikoTarPath builds with --no-push --oci-layout-path, writing the image as an OCI
// layout to a host dir mounted at /tarout instead of pushing. An OCI layout preserves the
// image's media type (a docker-save --tar-path would force docker v2), so a later push can
// reproduce the direct build exactly. The caller pushes the layout separately.
func runFuzzKanikoTarPath(contextDir, image, hostDir string, extra []string, covDir string, envOverride []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host", "-v", contextDir + ":/workspace:ro", "-v", hostDir + ":/tarout"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, ExecutorImage,
		"-f", path.Join(buildContextPath, "Dockerfile"), "-c", buildContextPath, "-d", image,
		"--no-push", "--oci-layout-path=/tarout/layout",
	)
	flags = append(flags, extra...)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// runFuzzKanikoPush runs `executor push` to push a pre-built OCI layout with kaniko's own
// push path, not crane, so the push under test is kaniko's.
func runFuzzKanikoPush(hostDir, image string, envOverride []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host", "-v", hostDir + ":/tarout"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	flags = append(flags, ExecutorImage, "push", "/tarout/layout", "--destination", image)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// twoStepPushOracle builds with --no-push --tar-path, then pushes the tarball separately with
// crane, and checks the result matches a direct build+push. Splitting build from push must
// not change the image. Exercises the --no-push/--tar-path write path (build.go layout.Write,
// push.go setDummyDestinations).
func twoStepPushOracle(seed int64, label, dir, dirImage, dockerfile string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	// --tar-path writes a docker schema2 tarball, which the executor refuses to combine with
	// --image-format=oci. Skip rather than report the executor rejecting its own conflict.
	if hasFlag(flags, "--image-format=oci") {
		return nil
	}
	hostDir, err := os.MkdirTemp("", "kaniko-fuzz-tarpath-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(hostDir)
	img := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-2step")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", img))
	out, err := runFuzzKanikoTarPath(dir, img, hostDir, flags, covDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		return fail(sevInvarianceDiff, "--no-push --tar-path build failed while direct build succeeded", out)
	}
	if pout, perr := runFuzzKanikoPush(hostDir, img, envFlags); perr != nil {
		return fail(sevInvarianceDiff, "kaniko `executor push` of the --tar-path tarball failed", pout)
	}
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(dirImage, img, ignores)
	if !same {
		return classifyContextDiff(seed, dockerfile, dirImage, img, ignores, diff, "direct build+push and --tar-path+separate-push differ", fail)
	}
	return nil
}

// runFuzzKanikoDfURL builds with the Dockerfile fetched from an http URL (-f <url>) while
// the context stays a local dir, exercising resolveDockerfilePath's URL branch.
func runFuzzKanikoDfURL(contextDir, image, dfURL string, extra []string, covDir string, envOverride []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host", "-v", contextDir + ":/workspace:ro"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, ExecutorImage, "-f", dfURL, "-c", buildContextPath, "-d", image)
	flags = append(flags, extra...)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// dockerfileHTTPOracle builds the case with its Dockerfile served over http (-f <url>) and
// checks the image matches the local-Dockerfile build. Where the Dockerfile comes from must
// not change the result. Reuses the ADD-url server to host the Dockerfile.
func dockerfileHTTPOracle(seed int64, label, dir, dirImage, dockerfile string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	if addURLDir == "" {
		return nil
	}
	name := "df-" + label
	served := filepath.Join(addURLDir, name)
	if err := os.WriteFile(served, []byte(dockerfile), 0o644); err != nil {
		return nil
	}
	defer os.Remove(served)
	img := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-dfhttp")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", img))
	out, err := runFuzzKanikoDfURL(dir, img, "http://"+addURLAddr+"/"+name, flags, covDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		return fail(sevInvarianceDiff, "http-Dockerfile build failed while local-Dockerfile built", out)
	}
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(dirImage, img, ignores)
	if !same {
		return classifyContextDiff(seed, dockerfile, dirImage, img, ignores, diff, "local-Dockerfile and http-Dockerfile builds differ", fail)
	}
	return nil
}

// digestFileOracle builds the case with --digest-file and asserts the written digest equals
// the digest the registry actually stored (crane digest). A mismatch means kaniko reported a
// digest for an image different from what it pushed. Reuses the /cache mount to receive the
// file. Exercises push.go getDigest/writeDigestFile, which no other path reaches.
func digestFileOracle(seed int64, label, dir string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	hostDir, err := os.MkdirTemp("", "kaniko-fuzz-digest-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(hostDir)
	img := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-df")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", img))
	args := append([]string{"--digest-file=/cache/d"}, flags...)
	out, err := runFuzzKanikoCache(dir, img, args, covDir, hostDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		return nil // build failed for unrelated reasons; not this oracle's concern
	}
	got, rerr := os.ReadFile(filepath.Join(hostDir, "d"))
	if rerr != nil {
		return fail(sevInvarianceDiff, "--digest-file was not written by a successful build", out)
	}
	want, werr := RunCommandWithoutTest(exec.Command("crane", "digest", img))
	if werr != nil {
		return nil
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		return fail(sevInvarianceDiff, "--digest-file content differs from the pushed image digest",
			fmt.Sprintf("digest-file:  %s\ncrane digest: %s", strings.TrimSpace(string(got)), strings.TrimSpace(string(want))))
	}
	return nil
}

// dryrunOracle builds the case with --dryrun and asserts nothing reached the registry
// (mz992). A dryrun renders the plan; it must not push. The destination tag is unique to
// this oracle, so any digest the registry can serve for it came from the dryrun build.
// Also catches the reverse: a dryrun that fails on a case the real build accepts.
func dryrunOracle(label, dir string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	img := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-dry")
	out, err := runFuzzKanikoEnv(dir, img, append([]string{"--dryrun"}, flags...), covDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		return fail(sevInvarianceDiff, "--dryrun failed on a case the real build accepted", out)
	}
	_, derr := RunCommandWithoutTest(exec.Command("crane", "digest", img))
	if derr == nil {
		return fail(sevInvarianceDiff, "--dryrun pushed the destination tag to the registry", out)
	}
	return nil
}

// registryMirrorOracle rebuilds the case from real docker.io references with
// --registry-mirror pointing at the campaign registry, and requires the image to match the
// one built from local refs. This runs the mirror the way a user does: kaniko resolves
// "alpine:fuzz" to index.docker.io, the mirror redirects it to the local copy mirrored under
// library/, and because that copy carries the upstream digest the redirected pull returns the
// identical base. So the whole remap path (parseRegistryMapping, remapRepository,
// setNewRepository) runs against a genuine cross-registry redirect while the output stays
// fixed. Nothing reaches Docker Hub: the mirror satisfies every pull, and
// --skip-default-registry-fallback makes that a hard guarantee rather than a hope, since a
// failed redirect then errors instead of silently falling back to the real docker.io.
//
// Only applies to cases whose bases are all alpine or debian; the locally minted oci,
// onbuild and annotation bases have no docker.io equivalent to redirect from.
func registryMirrorOracle(seed int64, label, dir, dirImage, dockerfile string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	repo := strings.ToLower(config.imageRepo)
	host := strings.TrimSuffix(repo, "/")
	local := map[string]string{
		repo + strings.ToLower(baseAlpineTag): "alpine:fuzz",
		repo + strings.ToLower(baseDebianTag): "debian:fuzz",
	}
	// Any base outside the rewritable set leaves the case untranslatable.
	for _, b := range basesInDockerfile(dockerfile) {
		if _, ok := local[strings.ToLower(b)]; !ok {
			return nil
		}
	}
	rewritten := dockerfile
	for from, to := range local {
		rewritten = strings.ReplaceAll(rewritten, from, to)
	}
	if rewritten == dockerfile {
		return nil // no base was rewritten, nothing for the mirror to redirect
	}
	mirrorDir, err := os.MkdirTemp("", "kaniko-fuzz-mirror-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(mirrorDir)
	if err := copyFuzzContext(dir, mirrorDir, rewritten); err != nil {
		return nil
	}
	img := strings.ToLower(repo + kanikoPrefix + label + "-mirror")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", img))
	args := append([]string{"--registry-mirror=" + host, "--skip-default-registry-fallback"}, flags...)
	out, err := runFuzzKanikoEnv(mirrorDir, img, args, covDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		return fail(sevInvarianceDiff, "build through --registry-mirror failed while the local-ref build succeeded", out)
	}
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(dirImage, img, ignores)
	if !same {
		return classifyContextDiff(seed, dockerfile, dirImage, img, ignores, diff, "local-ref and registry-mirror builds differ", fail)
	}
	return nil
}

// sharedCacheOracle builds one Dockerfile twice with two DIFFERENT build-arg sets that
// collide in the joined composite cache key (the mz873 shape), both pushing to one shared
// --cache-repo, then checks the second set's image is unaffected by the first having
// pre-populated the cache. Two different builds sharing a cache-repo is the real-world CI
// setup; the cache backend must not let one build's layer leak into another's, and the
// cache-lookahead precompute must not mis-resolve a colliding cross-stage pointer (mz872).
//
// A divergence between the shared-cache image and a fresh-cache image of the same set is
// mz873 (a colliding layer served across builds). A cache-lookahead assertion crash during
// the shared-cache build is mz872; crashOr catches it. Needs a case that declares >=2 args.
// RESOLVE_CACHE_KEY is forced off so the arg-referencing command string stays literal and
// the composite keys actually collide.
func sharedCacheOracle(seed int64, label, dir, dockerfile string, argNames, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	if len(argNames) < 2 {
		return nil
	}
	n1, n2 := argNames[0], argNames[1]
	// setA and setB differ but join to the same replacementEnvs string:
	// "n1=x-n2=z"+"n2=y" and "n1=x"+"n2=z-n2=y" both serialize to n1=x-n2=z-n2=y.
	setA := []string{"--build-arg=" + n1 + "=x-" + n2 + "=z", "--build-arg=" + n2 + "=y"}
	setB := []string{"--build-arg=" + n1 + "=x", "--build-arg=" + n2 + "=z-" + n2 + "=y"}
	// Drop the case's own build-args so only the colliding sets are in play (docker keeps
	// the last value, but being explicit keeps the composite key clean).
	var base []string
	for _, f := range flags {
		if !strings.HasPrefix(f, "--build-arg=") {
			base = append(base, f)
		}
	}
	base = append([]string{"--cache=true", "--cache-copy-layers=true"}, base...)
	env := append(append([]string{}, envFlags...), "FF_KANIKO_RESOLVE_CACHE_KEY=0")

	sharedRepo := strings.ToLower(config.imageRepo + "fuzzshared-" + label)
	cleanRepo := strings.ToLower(config.imageRepo + "fuzzshared-clean-" + label)
	imgA := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-shA")
	imgBShared := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-shB")
	imgBClean := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-clB")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", imgA, imgBShared, imgBClean))

	build := func(image, repo string, set []string) (string, error) {
		args := append(append([]string{}, base...), "--cache-repo="+repo)
		args = append(args, set...)
		return runFuzzKanikoEnv(dir, image, args, covDir, env)
	}
	// 1. Populate the shared cache with set A.
	aOut, aErr := build(imgA, sharedRepo, setA)
	if f := crashOr(aOut); f != nil {
		return f
	}
	if aErr != nil {
		return nil // set A failed to build; nothing to compare
	}
	// 2. Build set B into the shared (A-contaminated) cache. A cache-lookahead assertion
	// crash here is mz872.
	bsOut, bsErr := build(imgBShared, sharedRepo, setB)
	if f := crashOr(bsOut); f != nil {
		return f
	}
	if bsErr != nil {
		return fail(sevBuildOutcome, "shared-cache set-B build failed while set-A built", bsOut)
	}
	// 3. Build set B into a fresh cache: the ground truth for set B's output.
	bcOut, bcErr := build(imgBClean, cleanRepo, setB)
	if f := crashOr(bcOut); f != nil {
		return f
	}
	if bcErr != nil {
		return nil
	}
	// 4. Set B's image must not depend on whether a colliding set A pre-populated the cache.
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(imgBClean, imgBShared, ignores)
	if !same {
		// A cross-stage case is the mz876 map-leak class (still filed, load-dependent), so
		// count it. Otherwise the colliding-arg sets now hash apart under the recursive
		// composite key (mz873 fixed on main), so set A's pre-population must not change set
		// B's output; a diff here is a genuine regression worth reporting.
		if isCrossStage(dockerfile) {
			return &finding{seed: seed, sev: sevClean, known: map[string]int{mz876CrossStage: 1}}
		}
		return fail(sevCacheDiff, "shared-cache: set B output depends on colliding set A (mz873 regression)", diff)
	}
	return nil
}

// HTTPS build-context server. When FUZZ_HTTPSCONTEXT=1 one TLS file server runs for the
// whole campaign, serving each case's tar from httpsServedDir. The self-signed cert is
// trusted by the executor via SSL_CERT_FILE; HTTPSTar uses the default http client with
// no skip-TLS, so the cert must be genuinely trusted. This exercises pkg/buildcontext's
// HTTPSTar handler and util.CreateTargetTarfile, neither reachable from a local context.
var (
	httpsServedDir string
	httpsCertPath  string
	httpsBaseURL   string
)

// Local server addresses, derived from a base port so a second campaign can run alongside
// the first with FUZZ_PORT_BASE set. The default is fixed rather than an ephemeral port
// because a finding's Dockerfile embeds the ADD url verbatim, and a repro has to stay
// replayable after the campaign that produced it has exited. Without an override a second
// run fails to bind and quietly continues with these oracles disabled, which reads as a
// batch of sterile cases rather than as a misconfiguration.
var (
	httpsCtxAddr = "127.0.0.1:8899"
	addURLAddr   = "127.0.0.1:8890"
	addURL       = "http://" + addURLAddr + "/addfile"
	otlpAddr     = "127.0.0.1:8891"
)

// resolveFuzzPorts applies FUZZ_PORT_BASE, keeping the same relative layout as the defaults.
func resolveFuzzPorts() {
	base := os.Getenv("FUZZ_PORT_BASE")
	if base == "" {
		return
	}
	b, err := strconv.Atoi(base)
	if err != nil || b <= 0 || b > 65000 {
		return
	}
	addURLAddr = fmt.Sprintf("127.0.0.1:%d", b)
	addURL = "http://" + addURLAddr + "/addfile"
	otlpAddr = fmt.Sprintf("127.0.0.1:%d", b+1)
	httpsCtxAddr = fmt.Sprintf("127.0.0.1:%d", b+9)
}

// ADD-url server: a plain HTTP file server serving one fixed file, so a generated
// `ADD http://.../addfile` exercises kaniko's remote-fetch path (util.DownloadFileToDest),
// which no local COPY/ADD reaches. Started once for the campaign; both docker and kaniko
// fetch the same bytes, so the parity oracle stays valid. Plain http avoids a TLS cert.
const addURLContent = "add-url-fuzz-content\n"

// addURLSha256 is the hex digest of addURLContent, set when the server starts and used by
// generated ADD --checksum lines.
var addURLSha256 string

// OTLP trace sink: accepts the executor's span exports and counts them, so the tracing
// oracle can assert spans were actually emitted rather than only that the build still worked.
// An empty 200 with the protobuf content type is a valid ExportTraceServiceResponse (an empty
// message), so the SDK treats the export as successful without this needing a real collector.
var (
	otlpExports atomic.Int64
	// otlpUp gates the tracing oracle: false when the sink could not bind its port, so the
	// oracle stays off rather than asserting against a collector that cannot answer.
	otlpUp bool
)

func startOTLPSink() error {
	ln, err := net.Listen("tcp", otlpAddr)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		otlpExports.Add(1)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	return nil
}

// tracingOracle rebuilds the case with OTLP tracing pointed at the campaign's sink. Tracing
// is pure instrumentation, so the image must be byte-for-byte what the untraced build
// produced; a divergence means telemetry is perturbing the build. It also asserts the sink
// actually received an export, which is what separates this from a build that silently
// disabled tracing and passed. Covers Init, tracesEndpoint, buildAttrs, buildID,
// registryAttrs and Shutdown, none of which any other path reaches.
//
// The endpoint is given without a path so tracesEndpoint has to append /v1/traces itself,
// and the span-limit env is drawn so both branches of spanLimits are exercised.
func tracingOracle(seed int64, label, dir, dirImage string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	img := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-trace")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", img))
	env := append(append([]string{}, envFlags...), "KANIKO_TELEMETRY_ENDPOINT=http://"+otlpAddr)
	// Half the cases pin the attribute-value limit explicitly, which is the branch
	// spanLimits takes when the operator has set it; the rest take kaniko's own default.
	if seed%2 == 0 {
		env = append(env, "OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT=4096")
	}
	before := otlpExports.Load()
	out, err := runFuzzKanikoEnv(dir, img, flags, covDir, env)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		return fail(sevInvarianceDiff, "build with tracing enabled failed while the untraced build succeeded", out)
	}
	if otlpExports.Load() == before {
		return fail(sevInvarianceDiff, "tracing was configured but no spans reached the collector", out)
	}
	// Same ignores as the other invariance oracles. Two builds run at different wall-clock
	// times legitimately differ in directory mtimes, so only a --reproducible pair can be
	// compared with timestamps included; that comparison is the determinism oracle's job.
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(dirImage, img, ignores)
	if !same {
		return fail(sevInvarianceDiff, "traced and untraced builds differ", diff)
	}
	return nil
}

// secretLeakOracle asserts the secret value never appears in the pushed image: not in any
// layer's flattened content and not in the config (env, history, labels). The build reads
// the secret through an ephemeral mount and writes only a marker, so any occurrence of the
// token is a real leak, regardless of what the Dockerfile does. A leak is the highest-signal
// finding the fuzzer can produce, so it is always reported.
func secretLeakOracle(seed int64, image string, fail func(severity, string, string) *finding) *finding {
	if cfg, err := RunCommandWithoutTest(exec.Command("crane", "config", image)); err == nil && strings.Contains(string(cfg), secretToken) {
		return fail(sevSecretLeak, "secret value leaked into image config", "crane config "+image+" contains the secret token")
	}
	// crane export flattens every layer into the final rootfs tar; grepping the stream finds
	// the token in any file's content or name. -a treats the tar as text.
	scan := exec.Command("bash", "-c", fmt.Sprintf("crane export %s - 2>/dev/null | grep -a -c %s", image, secretToken))
	out, _ := RunCommandWithoutTest(scan)
	count := strings.TrimSpace(string(out))
	if count != "" && count != "0" {
		return fail(sevSecretLeak, "secret value leaked into an image layer", "crane export of "+image+" contains the secret token ("+count+" matches)")
	}
	return nil
}

// startAddURLServer serves a fixed file at addURL. A fixed Last-Modified (from the file's
// mtime) makes both tools stamp the same time, so only atime differs, which the oracle
// already excuses. Returns an error if the port cannot be bound.
// addURLDir is the directory the ADD-url server serves; the dockerfile-http oracle drops
// per-case Dockerfiles here to fetch with -f <url>.
var addURLDir string

func startAddURLServer() error {
	d, err := os.MkdirTemp("", "kaniko-fuzz-addurl-")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d, "addfile"), []byte(addURLContent), 0o644); err != nil {
		return err
	}
	// Digest of the exact bytes served, so an ADD --checksum generated for this URL cannot
	// drift out of sync with the file if the content ever changes.
	sum := sha256.Sum256([]byte(addURLContent))
	addURLSha256 = hex.EncodeToString(sum[:])
	ln, err := net.Listen("tcp", addURLAddr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: http.FileServer(http.Dir(d))}
	go srv.Serve(ln)
	addURLDir = d
	return nil
}

// startHTTPSContextServer generates a self-signed cert for 127.0.0.1 and starts a TLS
// file server over httpsServedDir. It returns an error if the cert or listener fails so
// the caller can leave the oracle disabled rather than run it half-configured.
func startHTTPSContextServer() error {
	d, err := os.MkdirTemp("", "kaniko-fuzz-httpsctx-")
	if err != nil {
		return err
	}
	cert := filepath.Join(d, "cert.pem")
	key := filepath.Join(d, "key.pem")
	genCert := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048",
		"-keyout", key, "-out", cert, "-days", "2", "-nodes",
		"-subj", "/CN=127.0.0.1", "-addext", "subjectAltName=IP:127.0.0.1")
	if out, err := RunCommandWithoutTest(genCert); err != nil {
		return fmt.Errorf("gen cert: %w: %s", err, out)
	}
	srv := &http.Server{Addr: httpsCtxAddr, Handler: http.FileServer(http.Dir(d))}
	go srv.ListenAndServeTLS(cert, key)
	httpsServedDir = d
	httpsCertPath = cert
	httpsBaseURL = "https://" + httpsCtxAddr + "/"
	return nil
}

// runFuzzKanikoHTTPS builds with the context fetched from the campaign's local TLS
// server as an https:// tarball. tarName is the file already placed in httpsServedDir.
func runFuzzKanikoHTTPS(image, tarName string, extra []string, covDir string, envOverride []string) (string, error) {
	flags := []string{"run", "--rm", "--net=host",
		"-v", httpsCertPath + ":/certs/ca.crt:ro", "-e", "SSL_CERT_FILE=/certs/ca.crt"}
	for _, e := range KanikoEnv {
		flags = append(flags, "-e", e)
	}
	for _, e := range envOverride {
		flags = append(flags, "-e", e)
	}
	if covDir != "" {
		flags = append(flags, "-v", covDir+":/covdata", "-e", "GOCOVERDIR=/covdata")
	}
	flags = append(flags, ExecutorImage,
		"-f", "Dockerfile",
		"-d", image,
		"-c", httpsBaseURL+tarName,
	)
	flags = append(flags, extra...)
	out, err := RunCommandWithoutTest(exec.Command("docker", flags...))
	return string(out), err
}

// httpsContextOracle builds the case from an https:// tar context and checks it matches
// the dir-context image, the same invariance as tarContextOracle over a different handler.
func httpsContextOracle(seed int64, label, dir, dirImage, dockerfile string, flags, envFlags []string, covDir string, fail func(severity, string, string) *finding, crashOr func(string) *finding) *finding {
	tarName := "ctx-" + label + ".tar.gz"
	served := filepath.Join(httpsServedDir, tarName)
	if err := tarDirTo(dir, served); err != nil {
		return nil
	}
	defer os.Remove(served)
	httpsImage := strings.ToLower(config.imageRepo + kanikoPrefix + label + "-httpsctx")
	defer RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", httpsImage))
	out, err := runFuzzKanikoHTTPS(httpsImage, tarName, flags, covDir, envFlags)
	if f := crashOr(out); f != nil {
		return f
	}
	if err != nil {
		return fail(sevInvarianceDiff, "https-context build failed while dir-context built", out)
	}
	ignores := []string{"--ignore-image-name", "--ignore-image-timestamps", "--ignore-file-timestamps"}
	diff, same, _ := runFuzzDiffoci(dirImage, httpsImage, ignores)
	if !same {
		return classifyContextDiff(seed, dockerfile, dirImage, httpsImage, ignores, diff, "dir-context and https-context builds differ", fail)
	}
	return nil
}

// tarDirTo writes a gzip tar of srcDir's contents to outPath, which must lie outside
// srcDir so the archive does not include itself.
func tarDirTo(srcDir, outPath string) error {
	if out, err := RunCommandWithoutTest(exec.Command("tar", "czf", outPath, "-C", srcDir, ".")); err != nil {
		return fmt.Errorf("tar %s: %w: %s", srcDir, err, out)
	}
	return nil
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

// craneLayerDigests returns image's layer digests straight from the registry via crane,
// which does not go through the docker daemon. Used as daemon-free ground truth when a
// cache diff is suspected of being a daemon-side misread.
func craneLayerDigests(image string) ([]string, error) {
	out, err := RunCommandWithoutTest(exec.Command("crane", "manifest", image))
	if err != nil {
		return nil, fmt.Errorf("crane manifest: %w", err)
	}
	var m struct {
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	ds := make([]string, len(m.Layers))
	for i, l := range m.Layers {
		ds[i] = l.Digest
	}
	return ds, nil
}

// diagnoseCacheDiff decides whether a diffoci cache diff is a real divergence or a
// docker-daemon measurement artifact. diffoci must compare through the daemon because
// the insecure registry blocks its direct read, and under concurrent load (many workers
// pulling, pushing, and rmi-ing at once) the daemon can hand diffoci a wrong layer. Two
// daemon-free checks confirm the diff: crane reads the registry layer digests directly,
// and a re-compare after dropping the daemon copies re-fetches from the registry. A diff
// that neither confirms did not reproduce and is treated as an artifact.
func diagnoseCacheDiff(v0, v1 string, ignores []string, firstDetail string) (bool, string) {
	var b strings.Builder
	d0, e0 := craneLayerDigests(v0)
	d1, e1 := craneLayerDigests(v1)
	if e0 == nil && e1 == nil && len(d0) > 0 {
		if strings.Join(d0, ",") == strings.Join(d1, ",") {
			fmt.Fprintf(&b, "crane: registry layer digests are IDENTICAL, so the two images match and diffoci's diff came from the shared docker daemon, not the cache\n  layers = %v", d0)
			return false, b.String()
		}
		fmt.Fprintf(&b, "crane: registry layer digests differ\n  v0 = %v\n  v1 = %v\n", d0, d1)
	} else {
		fmt.Fprintf(&b, "crane manifest inconclusive (v0 err=%v, v1 err=%v, v0 layers=%d)\n", e0, e1, len(d0))
	}
	// Drop the daemon copies so the re-compare pulls fresh from the registry.
	RunCommandWithoutTest(exec.Command("docker", "rmi", "-f", v0, v1))
	again, sameAgain, _ := runFuzzDiffoci(v0, v1, ignores)
	if sameAgain {
		fmt.Fprintf(&b, "diffoci re-run after fresh pull: IDENTICAL, the first diff did not reproduce (measurement artifact)")
		return false, b.String()
	}
	fmt.Fprintf(&b, "diffoci re-run after fresh pull: still DIFFERS (stable):\n%s", again)
	return true, b.String()
}

// classifyContextDiff decides what a dir-vs-context image diff means. A cross-stage case is
// the load-dependent mz876 snapshot-map-leak class (a wrong layer mis-attributed under
// concurrent builds); count it, do not report. Otherwise the diff is diagnosed against
// registry ground truth and a fresh-pull re-compare (diagnoseCacheDiff): a diff that does
// not reproduce is a docker-daemon measurement artifact, also not reported. Only a stable,
// non-cross-stage diff is a real build-context divergence.
func classifyContextDiff(seed int64, dockerfile, dirImage, otherImage string, ignores []string, diff, summary string, fail func(severity, string, string) *finding) *finding {
	if isCrossStage(dockerfile) {
		return &finding{seed: seed, sev: sevClean, known: map[string]int{mz876CrossStage: 1}}
	}
	real, evidence := diagnoseCacheDiff(dirImage, otherImage, ignores, diff)
	if !real {
		return nil
	}
	return fail(sevInvarianceDiff, summary, diff+"\n\n[diagnosis]\n"+evidence)
}

func writeFinding(outDir string, f finding) error {
	dir := filepath.Join(outDir, fmt.Sprintf("seed_%d_%s", f.seed, f.sev))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(f.dockerfile), 0o644); err != nil {
		return err
	}
	// Recreate the exact context (hardlinks, tars, symlinks, modes) so the finding is
	// self-contained and replayable, not just a Dockerfile.
	ctxDir := filepath.Join(dir, "context")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		return err
	}
	if err := writeContext(ctxDir, genResult{dockerfile: f.dockerfile, context: f.context}); err != nil {
		return err
	}
	// repro.sh replays the finding with the exact env and flags the case used, so triage
	// does not have to guess them (which previously made load-vs-deterministic ambiguous).
	if err := os.WriteFile(filepath.Join(dir, "repro.sh"), []byte(reproScript(f)), 0o755); err != nil {
		return err
	}
	report := fmt.Sprintf("seed: %d\nseverity: %s\nsummary: %s\nflags: %s\nenvFlags: %s\ncacheCompression: %q\ncacheLocal: %v\n\n%s\n",
		f.seed, f.sev, f.summary, strings.Join(f.flags, " "), strings.Join(f.envFlags, " "), f.cacheCompression, f.cacheLocal, f.detail)
	return os.WriteFile(filepath.Join(dir, "report.txt"), []byte(report), 0o644)
}

// reproScript emits a runnable reproducer carrying the exact KanikoEnv + fuzzed env
// flags, kaniko flags, and cache compression the case used. It covers the fresh build
// and, for cache/determinism findings, the paired builds plus the diffoci comparison.
func reproScript(f finding) string {
	var env strings.Builder
	for _, e := range KanikoEnv {
		fmt.Fprintf(&env, " -e %s", e)
	}
	for _, e := range f.envFlags {
		fmt.Fprintf(&env, " -e %s", e)
	}
	flagStr := strings.Join(f.flags, " ")
	comp := ""
	if f.cacheCompression != "" {
		comp = "--compression=" + f.cacheCompression
	}
	// Mirror the harness: --single-snapshot and --cache-run-layers=false legitimately
	// restamp cached mtimes, so the cache comparison excuses file timestamps for them.
	cacheIgnore := ""
	if hasFlag(f.flags, "--single-snapshot") || hasFlag(f.flags, "--cache-run-layers=false") {
		cacheIgnore = " --ignore-file-timestamps"
	}
	repo := config.imageRepo
	tag := fmt.Sprintf("repro-%d", f.seed)
	var b strings.Builder
	fmt.Fprintf(&b, "#!/usr/bin/env bash\n# Reproducer for seed %d (%s): %s\n", f.seed, f.sev, f.summary)
	// No set -e: diffoci exits non-zero when images differ, which is exactly the finding
	// we want to see, not a reason to abort before later sections run.
	b.WriteString("# Run from this directory. Assumes the fuzz local registry and executor-image are up.\nset -ux\n")
	fmt.Fprintf(&b, "CTX=\"$(cd \"$(dirname \"$0\")/context\" && pwd)\"\nENV=\"%s\"\nFLAGS=\"%s\"\nREPO=%q\n\n", strings.TrimSpace(env.String()), flagStr, repo)
	b.WriteString("# fresh build (docker-parity / determinism baseline)\n")
	fmt.Fprintf(&b, "docker run --rm --net=host -v \"$CTX\":/workspace:ro $ENV %s -f /workspace/Dockerfile -c /workspace -d ${REPO}%s-fresh $FLAGS\n\n", ExecutorImage, tag)
	b.WriteString("# cache populate then consume, compared byte-strict (cache oracle)\n")
	// The case's cache backend: an on-disk OCI layout shared across the two builds, or a
	// registry repo. The built image must not depend on which.
	cacheRepoArg := fmt.Sprintf("--cache-repo=${REPO}%s-cache", tag)
	cacheMount := ""
	if f.cacheLocal {
		cacheRepoArg = "--cache-repo=oci:/cache/layout --cache-dir=/cache"
		cacheMount = " -v \"$LAYOUT\":/cache"
		b.WriteString("LAYOUT=$(mktemp -d)\n")
	}
	fmt.Fprintf(&b, "for v in v0 v1; do docker run --rm --net=host -v \"$CTX\":/workspace:ro%s $ENV %s -f /workspace/Dockerfile -c /workspace -d ${REPO}%s-$v --cache=true --cache-copy-layers=true %s %s $FLAGS; done\n", cacheMount, ExecutorImage, tag, cacheRepoArg, comp)
	// diffoci reads from the docker daemon, so pull the pushed images into it first.
	fmt.Fprintf(&b, "docker pull ${REPO}%s-v0; docker pull ${REPO}%s-v1\n", tag, tag)
	fmt.Fprintf(&b, "diffoci diff --ignore-image-name --ignore-image-timestamps%s docker://${REPO}%s-v0 docker://${REPO}%s-v1\n\n", cacheIgnore, tag, tag)
	b.WriteString("# determinism (reproducible, byte-strict) - relevant for DETERMINISM_DIFF\n")
	fmt.Fprintf(&b, "for r in r0 r1; do docker run --rm --net=host -v \"$CTX\":/workspace:ro $ENV %s -f /workspace/Dockerfile -c /workspace -d ${REPO}%s-$r --reproducible $FLAGS; done\n", ExecutorImage, tag)
	fmt.Fprintf(&b, "docker pull ${REPO}%s-r0; docker pull ${REPO}%s-r1\n", tag, tag)
	fmt.Fprintf(&b, "diffoci diff --ignore-image-name docker://${REPO}%s-r0 docker://${REPO}%s-r1\n", tag, tag)
	return b.String()
}
