---
name: kaniko-integration-test
description: Write and run a kaniko integration test that reproduces a bug or validates a feature end to end. Use when adding a Dockerfile under integration/dockerfiles, wiring it into the TestRun or TestCache harness, and running it against a local registry.
---

# Writing a kaniko integration test

Integration tests build a Dockerfile with real kaniko (inside `docker run`) and compare the result against docker, or against a second cached build. They live in `integration/` and are driven by `scripts/integration-test.sh`.

## 1. Pick the harness

The test framework globs `integration/dockerfiles/Dockerfile_test_*` into `allDockerfiles`. Which suite runs a given file is decided in `integration/images.go`:

- **TestRun** (default): builds with both docker and kaniko, then diffs the two images with `diffoci` (not container-diff, which the fork no longer uses). Every discovered `Dockerfile_test_*` lands here unless excluded. Use it for build correctness (the kaniko output must match docker). To assert a runtime effect like uid/gid, write it into file content (for example `RUN echo "gid=$(id -g)" > /result.txt`) so the diff compares it.
- **TestCache**: builds the same Dockerfile twice against one cache repo, then asserts the two kaniko images are identical (`verifyBuildWith`). Register the file in `d.TestCacheDockerfiles` (this also excludes it from TestRun). Use it for anything cache related: cache hits, layer reuse, cache key correctness, and the `FF_KANIKO_CACHE_LOOKAHEAD` precompute assertions.
- **TestLayers / TestWarmer / TestReproducible**: niche suites, register in the matching map only when you specifically target layer counts, the warmer, or reproducibility.

Prefer wiring the test into one of these suites over writing a new `Test` function. Most setups fit through the per-file maps in `integration/images.go` (step 3). Write a dedicated test only when the scenario cannot be expressed in an existing suite.

Most bug repros that involve `--cache=true` go in TestCache. A pure build-correctness repro goes in TestRun (no registration needed beyond dropping the file in).

## 2. Write the Dockerfile

Path: `integration/dockerfiles/Dockerfile_test_issue_mz<num>` (the `mz<num>` matches the GitHub issue, per the repo naming convention).

Comment convention (see `Dockerfile_test_issue_mz655`, `Dockerfile_test_issue_3429`):

- Lead with `# mz<num>: <one line on what breaks>`.
- State the version it breaks on, for example `# Panics since v1.27.6 ...` or `# Creates cache misses on main@<sha> ...`. Use a release tag when the bug is in default behavior, a commit SHA when it only triggers behind a feature flag.
- A few short lines on the mechanism so the scenario explains the bug on its own.
- Plain short sentences, no em-dashes or semicolons.

A TestRun Dockerfile only sets up the scenario. Do not add assertion steps like `RUN [ -f /real/README.md ] && [ -L /linkdir ]` or `RUN test ...`: diffoci already compares the result against docker, and the extra layer only adds noise.

Keep the Dockerfile minimal: the fewest stages and layers that still reproduce. If a count is flaky, push it up until it is reliable rather than shipping a flaky repro.

## 3. Wire it into images.go

Register per-Dockerfile needs in the maps near the top of `integration/images.go`:

- `d.TestCacheDockerfiles` / other suite set: which test runs it.
- `envsMap[name]`: env vars passed as `docker run -e`, for example `{"FF_KANIKO_SQUASH_STAGES=0"}`. Flags meant to be on for the whole suite already live in the global `KanikoEnv` block (for example `FF_KANIKO_CACHE_LOOKAHEAD=1`), so do not repeat those per file.
- `argsMap[name]`: `--build-arg` values.
- `additionalKanikoFlagsMap[name]`: extra kaniko CLI flags, for example `--cache-copy-layers=true`.
- `additionalDockerFlagsMap[name]`: extra `docker build` flags. Use `{"--provenance=false"}` when kaniko emits a dockerv2 image and buildx would otherwise add a provenance attestation that forces ociv1, so the two mediatypes diverge and `diffoci` flags them. In short, `--provenance=false` forces the docker side to dockerv2 to match kaniko.

After editing, confirm it compiles: `go build ./integration/...`.

## 4. Run it locally

Never run `out/executor` on the host. Kaniko mutates the filesystem, so it only runs inside `docker run`, which the harness does for you.

Run the one test against a local registry:

```bash
LOCAL=1 DOCKERFILE_PATTERN=Dockerfile_test_issue_mz<num> ./scripts/integration-test.sh -run TestCache
```

- The harness builds `executor-image` from the working tree on every run, so it always tests your current code.
- `LOCAL=1` starts a `localhost:5000` registry and a TLS registry, and points the repo at them.
- `DOCKERFILE_PATTERN` narrows the glob to your file. `-run TestCache` (or `TestRun`) narrows the Go test.
- To prove the test reproduces the bug, run it on main with only the test commit cherry-picked and confirm it fails, then run it on the fix branch and confirm it passes.
