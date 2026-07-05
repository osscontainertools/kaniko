# Differential fuzzing of docker vs kaniko builds

* Author: Martin Zihlmann
* Date: 2026-07-02
* Status: Proposed

## Background

The integration suite already builds the same Dockerfile with both docker and kaniko and compares the two images with `diffoci`. `TestRun` in `integration/integration_test.go` walks every `integration/dockerfiles/Dockerfile_*`, builds each with both tools into a local registry, then runs `diffoci diff --semantic --extra-ignore-file-content --extra-ignore-layer-length-mismatch`. This is differential testing. The weakness is that the scenarios are handpicked. Each Dockerfile was written by a human to probe one behaviour, usually after a bug was already reported.

The goal here is to generate Dockerfile scenarios at random, build them with both tools, diff the outputs, and surface every divergence between docker and kaniko that is not already known and accepted. This turns the existing hand-curated differential suite into a search.

The pieces we need already exist in the tree.

- `buildKanikoImage` and `BuildDockerImage` build a Dockerfile with each tool into the local registry.
- `containerDiff` and `diffoci` wrap the comparison and already expose every ignore flag we care about.
- `diffArgsMap` in `integration/images.go` is a hand-written catalog of known divergences. Each entry names the file or metadata that differs and a comment explains why. Examples already recorded there: `COPY --from` retouches timestamps (mz155), deleting a builtin file makes buildkit emit a whiteout and kaniko does not (mz511), buildkit switches to USTAR where kaniko stays PAX, `VOLUME` directory creation (mz793), `nobody` with uid -1. This map is exactly the allowlist a fuzzer needs, generalised.
- The buildkit dockerfile `parser` and `instructions` packages are vendored, so we can parse and re-serialise what we generate for free.
- The tar-fixtures-on-the-fly proposal (`docs/design_proposals/integration-tar-fixtures-on-the-fly.md`) already argues for generating build context from a declarative spec at test time. The fuzzer needs the same capability for random context files.

A first MVP of this design is implemented and has already found real divergences. The confirmed findings are listed in `docs/design_proposals/dockerfile-fuzzing-findings.md`.

## Goal

Find docker vs kaniko output divergences that no human wrote a test for, reproducibly, and reduce each finding to a minimal Dockerfile that a human can read and file.

## Non-goals

- Finding build failures for their own sake. A Dockerfile that fails under both tools is not interesting. A Dockerfile that builds under one and fails under the other is interesting and is reported as a divergence.
- High throughput. Each case is two real container builds plus a pull and a diff, so a case costs seconds to minutes. The design optimises for signal per case and for parallelism, not for cases per second.
- Relying on Go native fuzzing's own coverage feedback. Its engine instruments the test process, and kaniko runs in a separate container, so its feedback loop is blind to the code under test. We drive coverage feedback ourselves from the executor's own coverage data, described below. We still adopt native fuzzing's byte-driven input model and its corpus and shrinking machinery.

## Architecture

The pipeline is one function per stage, driven by a seed.

```
corpus input ─▶ generate(Dockerfile + context) ─▶ build docker ──┐
     ▲                                          └─ build kaniko ──┴─▶ diffoci ─▶ classify ─▶ {known, novel, build-divergence}
     │                                                   │                                        │
     │                                          read kaniko covdata delta                 novel ─▶ shrink ─▶ report
     │                                                   │
     └────────── admit to corpus if new coverage ◀───────┘
```

1. **Generate.** A corpus input (a byte string) produces a Dockerfile and its build context. Same input, same output, always.
2. **Build both.** Reuse the existing harness to build with docker and kaniko into the local registry. Each kaniko build writes coverage into its own `GOCOVERDIR`. Record exit codes and logs from both.
3. **Coverage admission.** Compare the kaniko coverage from this build against the accumulated corpus coverage. If the input reached new counters, keep it in the corpus and prefer to mutate from it. This is the feedback loop that steers the search into unexercised executor code.
4. **Diff.** Run `diffoci` with as few ignores as possible, not with `--semantic`. Get the full structured diff and decide what matters ourselves.
5. **Classify and rank.** First check whether the kaniko build crashed, hit an assertion, or hit unreachable code. If so, that is the finding and it outranks any diff. Otherwise map the diff against the known-divergence catalog and emit: no diff, known divergence, novel divergence, or build-outcome divergence (one tool built, the other did not).
6. **Shrink.** For a crash or a novel divergence, minimise the Dockerfile while it still reproduces.
7. **Report.** Write the input, the minimal Dockerfile, the context spec, both build logs, and the raw diff or crash trace to an artifact directory, tagged with severity.

## The generator

The generator is the core of this work. It has to emit Dockerfiles that both tools accept, biased toward the constructs where docker and kaniko are known to diverge.

### Byte-driven and deterministic

The generator reads from a byte source rather than calling a random number generator directly. Every choice (which instruction, which flag, which path) consumes bytes from the source and maps them to a decision. Two consequences follow.

- A seeded PRNG can supply the byte stream for a plain randomised loop.
- Go native fuzzing supplies the byte stream from its corpus. The same generator runs under `go test -fuzz=FuzzDockerfile` unchanged, and we inherit its corpus persistence and its input minimisation.

A given input reproduces a byte-identical Dockerfile and context. This is the property that makes findings filable and shrinking possible.

```go
// Source hands out decisions from an opaque byte stream.
type Source struct{ b []byte; i int }
func (s *Source) intn(n int) int      // pick 0..n-1
func (s *Source) pick[T any([]T) T    // pick one of a set
func (s *Source) bool(pTrue float64) bool
```

The generator never uses wall-clock time or a global RNG. That keeps determinism intact and keeps two runs of the same seed comparable.

### Grammar

The generator emits a stage list. Each stage is a `FROM` plus a body of instructions. The grammar is weighted, not uniform. Weights bias toward the areas that historically diverge, drawn from `diffArgsMap` and the memory of past fixes.

Scope covers all four instruction families, selectable so a run can focus.

- **Filesystem: `COPY`, `ADD`, `RUN`.** `--chown` and `--chmod` in and out of range, `--from` an earlier stage or an external image, source globs, single file and directory and symlink and hardlink sources, destinations that exist and that do not, destinations that are a file and that are a symlink to a directory, deletes and overwrites that produce whiteouts, deeply nested paths, trailing-slash rules. This family is the highest yield.
- **Config: `ENV`, `ARG`, `WORKDIR`, `USER`, `EXPOSE`, `LABEL`, `VOLUME`, `ENTRYPOINT`, `CMD`, `STOPSIGNAL`, `HEALTHCHECK`, `SHELL`.** Both shell and exec forms. Numeric and named `USER`. `WORKDIR` that must be created and that pre-exists. These exercise the image config and the history, not the layers.
- **Multi-stage and `ONBUILD`.** Two to four `FROM` stages, cross-stage `COPY --from`, forward references that must be rejected, unused stages that `--skip-unused-stages` should prune, `ONBUILD` triggers fired by a later `FROM`.
- **Base image variety.** `FROM` chosen from a fixed pinned set: `scratch`, `alpine`, `debian`, a distroless image, plus one image published with an OCI media type and one with a docker media type. This exercises base-layer preservation and media-type handling, which the reproducible and preserve-base-layers work already touched.

Base images are pinned by digest, the same way `baseImageToCache` is pinned in `images.go`. The set is small and fixed so the fuzzer does not depend on network state and so a finding does not rot when a tag moves.

### RUN handling

`RUN` executes arbitrary shell, so it is both the richest divergence surface and the main source of false positives. The same `RUN date > /f` produces different bytes in the docker build and the kaniko build because they run at different wall-clock instants, and that is not a docker vs kaniko defect. The generator supports three modes, selectable per run, so we can trade coverage against noise.

- **Deterministic RUN.** `RUN` is restricted to a curated vocabulary of filesystem operations with no time or network dependence: `mkdir -p`, `touch -d @0` at a fixed epoch, `chmod`, `chown`, `ln -s`, `ln`, `rm`, `install -m`, writing fixed byte content to a path. Output is stable, so we can compare with less ignored. This is the cleanest signal.
- **Free-form RUN.** A broader command vocabulary. Content will differ between the two builds for reasons that are not defects, so the classifier scores a content-only diff under a free-form RUN as a low-severity nondeterministic-content bucket rather than a blanket `diffoci` ignore. Metadata and structure are still compared in full. More coverage, more triage noise.
- **No RUN.** Only `COPY`, `ADD`, and config instructions over generated context. Fully deterministic, narrowest, cleanest. Useful as the first mode to bring up and as a fast regression gate.

The default run cycles through all three so a campaign gets breadth over time.

### Validity

Random instruction sequences mostly fail to build, which wastes the expensive build step. The generator constructs valid Dockerfiles by construction rather than generating garbage and filtering.

- It tracks a model of the filesystem and the defined stages as it emits, so a `COPY --from=2` only appears once stage 2 exists and a `COPY src` only names a context path it already created.
- Every emitted Dockerfile is parsed with the vendored buildkit parser before it is built. A parse failure is a generator bug, not a finding, and fails the run loudly.

A Dockerfile that still fails to build under both tools is dropped and the seed is logged as sterile. A Dockerfile that builds under exactly one tool is a build-outcome divergence and is reported.

## Coverage feedback

The executor is already built with `-cover` (the `COVER` switch in the `Makefile`), and every build in the integration suite mounts a host directory as `GOCOVERDIR` through `addCoverageFlags` in `integration/images.go`. CI already reads it back with `go tool covdata textfmt`. This means the test process can see exactly which executor code a given Dockerfile exercised. We use that as the fuzzer's feedback signal, which is the part native Go fuzzing cannot do here because it only instruments the test process, not the container.

The loop works per case.

- Each kaniko build gets its own empty `GOCOVERDIR` subdirectory rather than sharing one. Coverage profiles are named per process, so a private directory gives us the counters for exactly that build.
- After the build, `go tool covdata` merges the case profile into the accumulated corpus profile and reports whether new counters were hit.
- An input that reaches new executor coverage is admitted to the corpus and becomes a preferred parent for mutation. An input that adds nothing is dropped unless it produced a divergence.

This steers generation toward unexercised branches in `pkg/executor` rather than re-treading the same paths, which matters when each case is expensive. It also gives a concrete campaign metric: executor coverage reached, comparable against the coverage `TestRun` already produces, so we can see the fuzzer entering code the handpicked suite never touched.

The docker side has no equivalent and needs none. Coverage feedback is about steering into kaniko's own code. docker is only an oracle, not code we are testing.

## Context generation

`COPY` and `ADD` need files in the build context. The generator writes a context tree from a declarative spec, the same shape the tar-fixtures proposal recommends: path, type (file, dir, symlink, hardlink, tar), mode, uid, gid, mtime, and content. mtime is pinned to a constant and uid and gid are explicit, so the context is byte-stable and the only variation between the docker and kaniko builds is the tool. For `ADD` of a local tar or a URL, the generator produces the archive from the same spec rather than committing a blob, which is the point of that proposal.

## Comparison, feature flags, and the known-divergence allowlist

The comparison reuses `containerDiff` and `diffoci`, but not with `--semantic` and not with the broad ignore set `TestRun` passes today. Those ignores are too coarse. `--semantic` and `--extra-ignore-file-content` and `--extra-ignore-layer-length-mismatch` hide real divergences. The `diffArgsMap` comment on mz595 says so directly: files that go missing are not caught while layer-length-mismatch is ignored, and it should be turned off globally. So the fuzzer runs `diffoci` with as few ignores as reasonably possible and moves all suppression into our own classifier, where every suppressed class is named and explained rather than folded into one blanket flag.

The only ignores that stay at the `diffoci` level are the ones that are not a kaniko-versus-docker correctness question at all: the image name, and the wrapping image-config build timestamp that both tools stamp by design. Everything `--semantic` used to hide, file content, file timestamps, layer count, tar format, becomes a classified bucket that we score, not a diff we never see.

The project's direction is that every place docker and kaniko diverge gets a feature flag that switches kaniko into buildkit-compatible behaviour. Several are already enabled in the integration suite. `KanikoEnv` in `integration/images.go` sets `FF_KANIKO_COPY_AS_ROOT`, `FF_KANIKO_OCI_SCRATCH_BASE`, `FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS`, and others. Not all of these are compatibility switches. Some select modern behaviour that is correct regardless of docker parity. The fuzzer's primary posture builds kaniko with the full `KanikoEnv` set, because that set is the compatible and modern configuration we ship and want to protect. A clean case there means kaniko matched docker in the configuration that matters most.

Running with older variants, meaning one or more of these flags flipped off, is a secondary posture. A divergence or crash that appears only with a flag off is a low-priority finding. It is still worth recording, because it maps the behaviour the flag governs, but it does not block on the same footing as a finding in the default set.

A divergence that survives with the compatibility flags on is one of two things, and the classifier's job is to say which.

- **A known divergence with no flag yet.** It matches a recorded class. `diffArgsMap` already documents these per test, for example the `COPY --from` timestamp retouch (mz155), the whiteout on deleting a builtin (mz511), USTAR versus PAX, and the `nobody` uid -1 case. We lift that catalog from per-filename ignores into shape matchers, because generated paths are not known in advance.
- **A novel divergence.** It matches nothing. This is the output we care about. It is either a bug in kaniko or the seed of the next feature flag.

```go
type KnownDivergence struct {
    Name string            // e.g. "builtin-delete-whiteout" (mz511)
    Why  string            // one line, the root cause
    Flag string            // the FF_KANIKO_* that fixes it, or "" if none exists yet
    Match func(Diff) bool  // does this diff entry fall under this class
}
```

The `Flag` field ties the catalog to the compatibility work. When a new flag ships that makes a class match buildkit, the fuzzer enables it in the compatibility set and that class stops appearing. When a novel class is judged expected-for-now, it graduates into a matcher with `Flag` empty, exactly as entries land in `diffArgsMap` today, and it becomes a candidate for a future flag.

The classifier reports counts per class per campaign. As flags graduate the known noise shrinks, and a new novel class stands out against a quiet baseline.

## The cache oracle

docker is not the only oracle. kaniko must also agree with itself across a cache. A build that populates a cache and a build that consumes it have to produce the same image. `verifyBuildWith` in `integration/integration_test.go` already does exactly this: it builds a Dockerfile once to fill a fresh cache, builds it again to read from the cache, then compares the two with `containerDiff` and no ignores at all. The comparison is strict because both sides are kaniko, so there is no docker nondeterminism to excuse away.

The fuzzer adds this as a second oracle on the same generated case.

- **docker oracle.** kaniko fresh versus docker. Cross-tool parity, compared with minimal ignores as above.
- **cache oracle.** kaniko cache-populate versus kaniko cache-consume. Self-consistency, compared with the strictest comparison we have. A divergence here is a cache bug, typically a stale layer or a wrong cache key, and it needs no docker at all.

The cache comparison is tighter than the docker one. The docker oracle still excuses the few things that differ between two different tools by design, chiefly file timestamps and tar format. The cache oracle excuses none of that. Both sides are kaniko building the identical context, so the two images should be byte-identical down to file mtimes, ownership, and tar layout. The only thing we allow to differ is the image name and the wrapping image-config `created` stamp, which is metadata and is what the existing `--ignore-image-timestamps` already covers. Everything else, including every file timestamp and the tar format, is compared exactly. A timestamp diff that the docker oracle would rightly ignore is a genuine cache finding here.

The cache oracle is cheap to add because the second build reuses the first build's context and the generator's flags. It also reaches code the docker oracle never touches. The cache-key computation, the layer pull, the lookahead and resolve paths gated by `FF_KANIKO_CACHE_LOOKAHEAD` and `FF_KANIKO_RESOLVE_CACHE_KEY`, only run on a cached build, so running the cache oracle grows executor coverage into `pkg/cache` and the caching branches of `pkg/executor` that a plain build leaves cold. That makes it a strong pairing with the coverage-guided loop.

The cache backend is itself a generated choice. The suite already exercises two: a registry cache and an OCI layout cache, split across `TestCacheDockerfiles` and `TestOCICacheDockerfiles`. The fuzzer picks the backend and the compressed-caching setting from the input, so a case can run through the cache oracle on either backend. Crash and assertion detection applies to the cache builds unchanged, and a stale-layer divergence enters the same classifier and severity ranking as any other diff.

A case that produces a clean docker comparison but a dirty cache comparison is a real finding and one the current handpicked cache suite can only catch on the Dockerfiles someone thought to add. Randomising the input is where this earns its keep.

## Additional oracles

The docker and cache oracles are cross-tool and cache-consistency checks. Two more oracles find bugs neither can, and two others only arbitrate rather than find.

### Build-twice determinism, two modes

Build the same case with kaniko twice, no cache, and compare. Any difference is a nondeterminism bug, found with docker out of the loop entirely. It runs in two modes because the two catch different things.

- Ignore-timestamps mode. Compare with file timestamps ignored. This catches structural nondeterminism: layer count, file set, ordering, mode, ownership, and media type that vary between two runs of the identical input.
- Reproducible mode. Build both with `--reproducible` and compare byte-strict, timestamps included. `--reproducible` pins timestamps to the epoch, so any remaining difference, including a stray non-epoch mtime, is a reproducibility defect. This is the tighter of the two and directly guards the mz731 and mz851 reproducibility work.

This is the cheapest new oracle and the earlier cache flake already hinted the nondeterminism class exists.

### tar-path versus push consistency

One build writes `--tar-path` and also pushes, and the two outputs must be the same image. This targets a known seam: the preserve-base-layers work had a tar-path media-type divergence. The wrinkle is that `--tar-path` always writes a docker v2 manifest, while the pushed image mirrors the base media type. So the comparison either forces the push to docker v2 as well, or restricts this oracle to docker v2 bases, so that a media-type difference the tar path dictates by design is not misread as a finding. Everything below the manifest media type, the layers and config, must match exactly.

### Optimization-flag invariance

A performance flag that only changes how kaniko caches or detects changes must not change what it builds. So building the same case twice, once with the flag and once without, must produce the same image, and, when caching, the same cache keys. A divergence is a bug in the optimization.

The first target is `FF_KANIKO_CACHE_LOOKAHEAD` (with the related `FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY` and `FF_KANIKO_RESOLVE_CACHE_KEY`). Build the case with `--cache` and the flag on, and again with it off, and compare the images byte-strict apart from image name and config timestamp. mz872 is exactly this class: lookahead over-folds later ARGs into an earlier command's aggregate cache key, which the internal `executor.build.cache-lookahead` assertion catches as a crash. This oracle also catches the silent variant, where lookahead does not crash but serves a wrong cached layer, which no assertion covers. It generalizes to any flag that is meant to be output-neutral, for example `--compressed-caching` and `--cache-run-layers`.

The wrinkle is that the flag lives in the environment, not the build args, so the harness needs an env override on one of the two builds rather than a CLI flag. A case where the flag-on build crashes on an assertion is already caught by crash detection before this oracle runs.

### Deferred or arbitration-only

- Previous-release regression. HEAD executor versus a pinned prior release on the same input. This is a CI regression guard, not a bug finder for now, so it belongs in the scheduled job rather than the local campaign.
- buildah as a third implementation. It will not find new bugs, but it arbitrates when docker and kaniko disagree: if buildah agrees with docker, kaniko is the outlier, and if it agrees with kaniko, the case is likely a docker quirk rather than a kaniko bug. Useful for triage, not for discovery, and the heaviest to set up.

## Severity and crash detection

A crash beats a diff. A Dockerfile that makes the kaniko executor panic, trip an assertion, or hit unreachable code is a higher-value finding than one that merely produces a different image, and it is ranked above every diff class.

kaniko already makes these easy to detect. `util.Assert` panics through `logrus.Panicf` with the prefix `Assertion violated [name]:`, and `util.Unreachable` panics with `Unreachable Code:`. A Go panic prints a goroutine stack. So the harness inspects the kaniko build outcome first, before it looks at any image.

- **Crash.** The executor exited on a Go panic, a runtime error, or a segfault. Highest severity.
- **Assertion or unreachable.** stderr carries `Assertion violated [` or `Unreachable Code:`. These are invariant violations the code itself flags. Highest severity, and the assertion name goes straight into the report so the finding is already half-triaged.
- **Build-outcome divergence.** One tool built and the other failed cleanly, with no crash. High severity.
- **Novel diff.** The images differ in a class no matcher recognises. Medium severity.
- **Known diff, no flag.** A recorded class with no compatibility flag yet. Low severity, counted not failed.
- **Older-variant only.** Any of the above that reproduces only with a `KanikoEnv` flag flipped off. Low severity, recorded to map what the flag governs.

The report sorts by this order, so a campaign surfaces crashes and assertions at the top regardless of how many diffs it also found. Because the default posture already sets `KANIKO_IGNORE_ASSERTIONS` unset, assertions fire rather than degrade to warnings, which is what we want during fuzzing.

## Harness integration

The fuzzer lives in the `integration` package and reuses the build path, the registry, the platform handling, and the diff wrapper. Nothing about the container build or the registry is reimplemented.

The per-Dockerfile maps in `images.go` (`argsMap`, `envsMap`, the flag maps, `diffArgsMap`) are keyed by a committed filename. Generated Dockerfiles have no committed name. Rather than mutate those global maps at runtime, the fuzzer passes build args, env, and flags directly to `buildKanikoImage` and `BuildDockerImage`, which already take them as parameters. The generator emits the arg and flag set alongside the Dockerfile, so a case is self-contained.

Builds are the bottleneck, so cases run in parallel with `t.Parallel`, bounded by a worker count, the same way `TestRun` already parallelises across Dockerfiles.

Entry points:

- `TestFuzz` runs a bounded campaign from a seed and a case count set by env or flag, for CI and for a local `go test` run.
- `FuzzDockerfile` is the native `go test -fuzz` target for a local corpus-driven session.

Both call the same generate, build, diff, classify path.

## Reproducibility, corpus, and shrinking

- **Reproducibility.** A finding is a seed or a corpus input. Replaying it regenerates the exact Dockerfile and context and rebuilds. The report includes the seed so anyone can reproduce with one command.
- **Corpus.** The corpus lives in `testdata/fuzz/FuzzDockerfile` in the native-fuzz layout and persists across runs. We seed it deliberately rather than starting from noise, in two ways. First, a hand-picked set of suspicious base images, chosen because their layers stress the areas kaniko handles differently: an image with a whiteout in a lower layer, one with hardlinks, one with unusual permission or ownership bits, one published with an OCI media type and one with a docker media type, one with an opaque directory. Second, encodings of a few existing handpicked Dockerfiles so the search also starts from known-good structure. Coverage admission then grows the corpus from these seeds toward new executor code.
- **Shrinking.** On a novel finding the harness minimises. Native fuzzing minimises the input bytes. On top of that we shrink at the Dockerfile level: drop instructions and drop stages one at a time and keep the smallest Dockerfile that still reproduces the divergence. Dockerfile-level shrinking produces a reproducer a human can read, which byte-level shrinking alone does not guarantee.

## Failure triage

Each novel finding writes an artifact directory: the seed, the minimal Dockerfile, the context spec, both build logs, the pulled image references, and the raw `diffoci` output. The report groups findings by classifier bucket so a run of a thousand cases collapses into a handful of distinct divergence shapes rather than a thousand lines.

## Running it

- Locally: `go test ./integration -run TestFuzz` with a seed and case count, against a local registry, the same setup `TestRun` needs.
- Corpus session: `go test ./integration -fuzz=FuzzDockerfile`.
- CI: a scheduled job, not a per-PR gate at first. Container builds are too slow to fuzz on every PR. The scheduled job runs a fixed seed range so a regression is reproducible, uploads artifacts on any novel finding, and does not fail the build on known classes.

Throughput is roughly one case every few seconds to a minute depending on RUN mode and base image, times the worker count. A campaign is hundreds to low thousands of cases, not millions.

## Phased implementation

Status as of 2026-07-02. The implemented pieces live in `integration/fuzz_test.go`, `fuzz_gen_test.go`, `fuzz_classify_test.go`, `fuzz_coverage_test.go`, and `fuzz_shrink_test.go`. Confirmed findings are in `docs/design_proposals/dockerfile-fuzzing-findings.md`.

| Phase | Scope | Output | Status |
|---|---|---|---|
| 1 | No-RUN generator over COPY and ADD and config, seeded loop, reuse build and diff, full `KanikoEnv` set on, crash and assertion detection, no shrinking | `TestFuzz` catches crashes and confirms clean on the deterministic subset | done |
| 2 | `diffoci` run with minimal ignores, classifier and known-divergence catalog lifted from `diffArgsMap`, keyed to `FF_KANIKO_*` flags, severity ranking | Novel vs known separation, per-class counts, crashes ranked first | done |
| 3 | Cache oracle: populate-versus-consume on registry and OCI backends, byte-strict comparison, reuses classifier and crash detection | Cache bugs on generated input, coverage into `pkg/cache` | partial: registry backend done, OCI layout backend pending |
| 4 | Coverage admission from executor `GOCOVERDIR`, corpus seeded with the suspicious base images | Coverage-guided search, executor coverage metric per campaign | partial: per-case `GOCOVERDIR` admission and corpus mutation done, suspicious-base-image seed corpus pending |
| 5 | Deterministic RUN, multi-stage, ONBUILD, base-image variety | Full instruction surface | partial: deterministic RUN done, multi-stage, ONBUILD, and base-image variety pending |
| 6 | Dockerfile-level shrinking and the artifact report | Minimal reproducers | done |
| 7 | `FuzzDockerfile` native target, scheduled CI job | Corpus-driven local sessions and continuous runs | pending |
| 8 | Free-form RUN mode | Broadest coverage, gated by triage capacity | pending |

The generator currently produces single-stage Dockerfiles and omits USER to keep builds succeeding, so multi-stage, ONBUILD, USER, and base-image variety are the main breadth gaps. A `FUZZ_TREAT_KNOWN_AS_NOVEL` toggle exists to exercise the shrinker against the known classes, since a tuned run reports zero findings.

## Risks and open questions

- **Noise.** If the classifier is weak, every campaign drowns in known divergences. Phase 2 is therefore load-bearing and should ship before RUN broadens the surface in phase 3.
- **Flakiness.** Any residual nondeterminism in a build (a stray timestamp, a network fetch) produces a divergence that does not reproduce. The deterministic-first ordering of the phases is meant to keep the early signal trustworthy. A finding that fails to reproduce on replay is discarded, not filed.
- **Generator bias.** The weights decide what we find. Coverage admission mitigates this by pulling the search toward unexercised executor code, but it cannot generate a construct the grammar never emits. We record both instruction-family coverage and executor coverage per campaign so we can see what the generator never explored, and adjust weights rather than assume breadth.
- **Base image drift.** Pinned digests avoid tag movement but go stale. The set needs occasional refresh, the same maintenance `baseImageToCache` already carries.
- **Where findings go.** Each novel class becomes one of three things: a bug with a reduced reproducer under `integration/dockerfiles`, a new `FF_KANIKO_*` flag that switches kaniko to buildkit-compatible behaviour for that class, or a recorded matcher with a comment when the divergence is expected and no flag is planned yet. All three mirror how the compatibility flags and `diffArgsMap` grow today.
