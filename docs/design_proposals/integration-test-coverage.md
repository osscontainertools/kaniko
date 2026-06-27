# Integration test coverage for untested CLI flags

* Author: Martin Zihlmann
* Date: 2026-05-01
* Status: Under implementation (PR #669)

## Background

The kaniko executor exposes many CLI flags that affect build behaviour, output format, caching strategy, snapshot mode, and filesystem handling. Coverage on `pkg/executor/build.go` shows a large number of option-gated branches that are never exercised by the integration test suite. This document identifies those gaps and proposes concrete integration tests for each one.

The analysis is based on comparing every field in `pkg/config/options.go` against the flags and Dockerfiles wired up in `integration/images.go` and `integration/integration_test.go`.

## Gap analysis

The following flags have zero integration test coverage.

| Flag | Config field | build.go reference |
|---|---|---|
| `--tar-path` | `TarPath` | image push path |
| `--oci-layout-path` | `OCILayoutPath` | image push path |
| `--materialize` | `Materialize` | line 406 |
| `--ignore-var-run=false` | `IgnoreVarRun` | snapshot exclusion |

Already covered: `--compression=zstd`, `--compression-level`, `--compressed-caching=false` (all three wired onto `Dockerfile_test_cache` via `additionalKanikoFlagsMap`; covers `pushLayerToCache` zstd path in `push.go`, `CompressionLevel > 0` and `CompressedCaching` false branch in `getLayerOptionFromOpts`; note: the zstd path in `saveSnapshotToLayer` at `build.go:639` requires an OCI-format base image and remains uncovered), `--digest-file`, `--image-name-with-digest-file`, `--image-name-tag-with-digest-file` (all three passed as `--digest-file=/dev/stdout` etc. in every `buildKanikoImage` call, output goes to captured stdout), `--reproducible`, `--single-snapshot`, `--snapshot-mode=redo`, `--use-new-run`, `--target`, `--cache`, `--no-push-cache`, `--cache-copy-layers`, `--secret`, `--cleanup`, `--skip-unused-stages`, `--annotation`, `--label`, `--registry-mirror`, `--registry-map`, `--skip-default-registry-fallback`, `--kaniko-dir`, `--build-arg`, `--git`, `--pre-cleanup` (baked as `KANIKO_PRE_CLEANUP=1` into `kaniko-alpine`, exercised by `mz595`), `--preserve-context` (baked as `KANIKO_PRESERVE_CONTEXT=1` into `kaniko-alpine`, exercised by `mz595`; additionally has a dedicated behavioural test via `Dockerfile_test_preserve_context`), `--ignore-path` (`Dockerfile_test_ignore_path` with `--ignore-path=/kaniko-extra-file --ignore-path=/kaniko-extra-dir` in `additionalKanikoFlagsMap`, layer-length-mismatch disabled so absence of the path is verifiable), `--cache-run-layers=false` (hardcoded in `buildKanikoImage` at `images.go:750`, exercised by every build including all cache builds), `--snapshot-mode=time` (`TestSnapshotModes` builds `Dockerfile_test_run` with `full`, `redo`, and `time` and compares all three), `--custom-platform` (`Dockerfile_test_cross_compile`: two-stage arm64 build with cross-stage `COPY`, no `RUN`; wired via `additionalKanikoFlagsMap` with `--custom-platform=linux/arm64` and `additionalDockerFlagsMap` with `--platform=linux/arm64`; `platformMap` ensures `docker pull` and diffoci both use `linux/arm64`), `FF_KANIKO_CACHE_LOOKAHEAD=1` (in global `envVars` at `images.go:122`, exercised by every cache build), `KANIKO_PRINT_PLAN=1` (in global `KanikoEnv`, `RenderStages` exercised on every build).

## Proposed tests

### Group 1: Tar and OCI layout output — `TestTarPath`, `TestOCILayoutPath`

**File:** `integration/integration_test.go` (two new functions)

Both tests skip pushing to a registry. Mount a host temp dir into the kaniko container and point the flag at it.

`TestTarPath`: pass `--tar-path=/out/image.tar --no-push`. Assert the output file is a valid tar archive containing `manifest.json` (Docker image layout).

`TestOCILayoutPath`: pass `--oci-layout-path=/out/image --no-push`. Assert the output directory contains `oci-layout` and `index.json` (OCI image layout spec).

Reuse `Dockerfile_test_env` for both. No new Dockerfiles needed.

## Prioritised implementation order

| Priority | Group | Effort | Primary coverage gain |
|---|---|---|---|
| 1 | Group 1 — tar / OCI output | Low | `TarPath`, `OCILayoutPath` push paths |
| 2 | `--materialize` | High | requires pre-warmed cache setup |
