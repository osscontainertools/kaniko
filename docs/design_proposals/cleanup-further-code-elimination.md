# Cleanup: further code elimination opportunities

## Status
Survey. Companion to [cleanup-test-only-production-exports.md](cleanup-test-only-production-exports.md), which handled test-only exports and test-fixture files. This doc lists the remaining classes of removable code in rough order of ROI.

## Background

After the test-only-exports cleanup, `staticcheck` with its substantive (non-style) checks returns zero hits — no unused variables, no unreachable branches, no deprecated stdlib APIs. The codebase is healthy on that axis. The remaining elimination opportunities are at coarser granularities: deprecated CLI flags, vestigial hidden flags, integration scaffolding for code paths CI doesn't exercise, and dependency audit.

## Overview

| Class | Approx lines | Risk | Effort |
|---|---|---|---|
| [Deprecated CLI flags](#1-deprecated-cli-flags) | ~50 | Low (major-version breaking) | Mechanical |
| [Hidden but possibly vestigial flags](#2-hidden-flags) | ~10–20 | Need investigation first | Low-medium |
| [GCP integration scaffolding](#3-gcp-integration-scaffolding) | ~150 | Low (test-only) | Medium; deferred — see [cleanup-test-only-production-exports.md](cleanup-test-only-production-exports.md) |
| [`buildKanikoImage` unused return value](#4-unused-return-value-in-buildkanikoimage) | ~3 | None | Trivial |
| [Parallel cleanup for AWS/Azure](#5-aws-and-azure-integration-paths) | TBD | Need investigation | Medium |
| [Dependency audit](#6-dependency-audit) | indirect (vendor tree) | None | Low |
| [CLI subcommand/flag audit](#7-cli-subcommand-and-flag-audit) | TBD | Need investigation | Medium |

## 1. Deprecated CLI flags

Five `executor` flags (and one `warmer` flag) emit `logrus.Warn("Flag --X is deprecated. ...")` and translate to the new flag at runtime. Each one has:

- A field in `KanikoOptions` (`pkg/config/options.go`) — typically named `*Deprecated`
- A `flag.StringVarP` / `BoolVarP` registration in `cmd/executor/cmd/root.go` around lines 328–332
- A translation block in `checkNoDeprecatedFlags` (`cmd/executor/cmd/root.go:375+`)

Candidates:

| Flag | Replacement | Notes |
|---|---|---|
| `--snapshotMode` | `--snapshot-mode` | Naming-only alias |
| `--customPlatform` | `--custom-platform` | Naming-only alias |
| `--tarPath` | `--tar-path` | Naming-only alias |
| `--force-build-metadata` | n/a | Now the default behavior |
| `--skip-unused-stages` | n/a | Now the default behavior |
| `--whitelist-var-run` | `--ignore-var-run` | Renamed |

These are user-visible CLI changes, so they should land in a major version. Before removing, check `git blame` for how long each has been deprecated — anything deprecated for several releases (say, ≥3) is fair game; anything recent should wait.

**Effort:** mechanical, ~50 lines deleted across `pkg/config/options.go` and `cmd/executor/cmd/root.go`. One commit per flag for easy revert, or one bundled commit with a clear "Removed: ..." entry in the changelog.

## 2. Hidden flags

Two flags are registered but hidden from `--help`:

- `--azure-container-registry-config` (executor + warmer)
- `--bucket` (executor; comment says "Name of the GCS bucket from which to access build context as tarball")

Hidden flags are typically one of: (a) deprecated but kept for backwards-compat, (b) still functional but for internal use, (c) leftover registrations that no longer do anything.

**Investigation first:** trace each flag from its `StringVar` binding to actual consumption. If a flag's bound field has no production readers, it's vestigial and the registration can go. If it's still wired up, the question becomes whether to publicly re-expose it or formally deprecate.

`--bucket` is particularly suspicious given the GCP-integration-tests-not-exercised situation — needs to be paired with that audit.

## 3. GCP integration scaffolding

Covered in [cleanup-test-only-production-exports.md](cleanup-test-only-production-exports.md). Roughly ~150 lines removable in `integration/` plus `pkg/util/bucket.Delete`, conditional on the bigger question of whether to keep the production GCS code path at all. The production `pkg/buildcontext/gcs.go` is reachable from `gs://` source context and would stay regardless of CI exercise.

## 4. Unused return value in `buildKanikoImage`

`unparam` flags:

```
integration/images.go:893:4: buildKanikoImage - result 0 (string) is never used
```

The function returns `(string, error)` but every caller discards the string. Two-line fix: change signature to return `error` only, drop the value from every `return` and call site.

## 5. AWS and Azure integration paths

The same audit that applies to GCS should apply to S3 and Azure Blob:

- Are CI integration tests exercising `s3://` and Azure Blob source contexts?
- If not, the same "we don't validate this" status applies to S3-/Azure-specific code in `pkg/buildcontext/s3.go` and `pkg/buildcontext/azureblob.go`.
- Unlike GCP, these don't have a separate test-utility package equivalent to `pkg/util/bucket`, so the cleanup surface is smaller — mostly just the question of whether the production fetchers are exercised.

**Effort:** investigation first. If S3 and Azure are also not exercised, decide whether they're real user features kept on faith or candidates for deprecation.

## 6. Dependency audit

Vendor directory is currently **71M** (337 files in `vendor/github.com/envoyproxy/go-control-plane` alone). Of the 33 direct deps in `go.mod`, all are reachable from production or test code — there are no outright-unused direct dependencies. The interesting findings are about transitive cost.

### Heaviest vendored subtrees

| Subtree | Size | Pulled in by |
|---|---|---|
| `github.com/aws/` | 11M | `pkg/buildcontext/s3.go`, `pkg/util/bucket` |
| `github.com/envoyproxy/go-control-plane/` | **9.6M** | **TEST-only transitive chain** — see below |
| `cloud.google.com/go/` | 6.5M | `pkg/buildcontext/gcs.go`, `pkg/util/bucket`, `integration/` |
| `google.golang.org/grpc/` | 4.0M | gRPC, pulled via cloud.google.com |
| `go.opentelemetry.io/otel/` | 3.1M | Pulled via `integration → cloud.google.com/go/storage` |
| `github.com/Azure/` | 2.7M | `pkg/buildcontext/azureblob.go` |
| `google.golang.org/protobuf/` | 2.0M | Indirect; protobuf runtime |
| `github.com/go-git/` | 2.0M | `pkg/buildcontext/git.go` (production git context) |
| `github.com/google/` | 1.9M | go-containerregistry mostly |
| `google.golang.org/api/` | 1.3M | Cloud GCS SDK |

### The envoyproxy oddity

`github.com/envoyproxy/go-control-plane` is 9.6M of vendored code. `go mod why` initially makes this look like a test-only chain (it shows `.test` and `xds/e2e` in the trace), but **that's misleading** — the actual import path is through production grpc code:

```
pkg/util/bucket
  → cloud.google.com/go/storage
    → cloud.google.com/go/storage/grpc_dp.go             ← production file
      → google.golang.org/grpc/xds/googledirectpath      ← production file
        → google.golang.org/grpc/xds/*                   ← production
          → github.com/envoyproxy/go-control-plane/envoy/* (data types for Envoy)
```

`grpc_dp.go` is a 22-line file in the Google Cloud SDK whose entire content is two anonymous imports:

```go
//go:build !disable_grpc_modules

package storage

import (
    _ "google.golang.org/grpc/balancer/rls"
    _ "google.golang.org/grpc/xds/googledirectpath"
)
```

These register two gRPC mechanisms used for GCP-internal performance optimizations: **Direct Path** (low-latency GCS access from GCP VMs via the GCP-internal network) and **RLS** (route lookup service load balancing). Neither is needed for kaniko's use case — pulling a build context tarball once per build. The Google Cloud SDK gracefully falls back to plain HTTPS when these aren't available.

### The fix: `-tags=disable_grpc_modules`

The build tag is upstream-provided exactly to exclude these imports. Adding `-tags=disable_grpc_modules` to the executor and warmer build commands in `Makefile`:

```diff
- GOARCH=$(GOARCH) GOOS=$(GOOS) CGO_ENABLED=0 go build $(if $(COVER),-cover) -ldflags $(GO_LDFLAGS) -o $@ $(EXECUTOR_PACKAGE)
+ GOARCH=$(GOARCH) GOOS=$(GOOS) CGO_ENABLED=0 go build -tags=disable_grpc_modules $(if $(COVER),-cover) -ldflags $(GO_LDFLAGS) -o $@ $(EXECUTOR_PACKAGE)
```

Measured impact (`go build ./cmd/executor`, default `-ldflags '-w -s'`):

| Build | Binary size |
|---|---|
| Default | 72 MB |
| `-tags=disable_grpc_modules` | 54 MB |

**−18 MB, ~25% reduction** in the shipped executor binary. Same approximate reduction expected for the warmer. The linker drops `envoyproxy/go-control-plane/envoy/*`, `grpc/xds/*`, `grpc/balancer/rls`, and several transitively-reachable subpackages.

### What this does NOT change

- **Vendor directory stays at 71 MB.** `go mod vendor` always includes all build-tag variants — it can't know which tags a downstream build will use. The win is purely at link time.
- **Production GCS source context (`gs://`) still works.** Direct Path is an optimization; HTTPS is the always-supported fallback that the SDK uses transparently. Users see no functional difference.
- **`go.mod` is unchanged.** The dependencies are still declared; they just don't end up in the final binary.

### Trade-off

Kaniko users running inside a GCP VM that fetches build contexts from a same-project GCS bucket would lose the Direct Path optimization. For a typical build-context tarball (under ~100 MB) and the rare GCP-VM-to-GCS-in-same-project topology, the latency/throughput difference is negligible. For all other users (CI runners, Kubernetes clusters not on GCP, etc.) there is zero difference.

### Marginal direct-dep candidates

| Dep | Usage | Note |
|---|---|---|
| `github.com/google/slowjam` | One line in `cmd/executor/main.go`: `stacklog.MustStartFromEnv("STACKLOG_PATH")` — emits a stack trace on signal if the env var is set | If nobody on the team actively uses `STACKLOG_PATH` for diagnostics, drop it. Saves a small vendored module and one stray dep. |
| `github.com/golang/mock` | Only used by `pkg/util/mock_layer_test.go` (the `gomock`-generated `MockLayer` we just folded in) | Keep. gomock is the standard mock framework; replacing it would mean hand-writing a `MockLayer`. |

### Conditional cleanup tied to bigger questions

These deps come and go with bigger decisions:

- **Drop GCS entirely** (production + integration): removes `cloud.google.com/go/storage` (6.5M vendored) and most of the grpc/otel/envoyproxy baggage from the binary too. Risk: real users may rely on `gs://` source context.
- **Drop S3 entirely**: removes `aws/aws-sdk-go-v2/*` (11M vendored). Same risk question.
- **Drop Azure entirely**: removes `Azure/azure-sdk-for-go` (2.7M vendored). Same risk question.

These are product/user questions, not code-quality questions.

### Recommended action

1. **Add `-tags=disable_grpc_modules` to the Makefile build lines.** Cheapest, highest-impact change — 18 MB off the executor binary with no functional regression for kaniko's GCS use case. See the envoyproxy section above for the full analysis.
2. Check whether `STACKLOG_PATH` (slowjam) is anyone's actual diagnostic tool; remove if not.
3. Don't chase the vendor-size goal in isolation — vendor mode includes everything regardless of tags; the meaningful target is the linked binary, and most of its weight is justified by actual product features.

## 7. CLI subcommand and flag audit

Kaniko ships two binaries (`executor`, `warmer`) with many flags. Each flag is a contract — and contracts have maintenance cost.

- Walk every flag in `executor` and `warmer` and ask: is it documented in `README.md`? Is it used by any of the integration tests? When was its bound option field last read by production code?
- Surfaces likely candidates: flags that were added for one-off use cases, flags that overlap with environment-variable equivalents, flags whose entire purpose is GCP/AWS/Azure-specific behavior.

This is a more involved audit than the others — best done as a one-time pass with output captured in a markdown checklist.

## Out of scope

These came up during the analysis but are NOT recommended actions:

- **Style/naming fixes** (`ST1000`, `ST1003`, `ST1016`, `ST1020`, `ST1022`). Many findings, all cosmetic. Touching them would create churn without removing anything. Leave for future opportunistic cleanup as files are edited for other reasons.
- **Unused struct fields**. Go's tooling can't reliably detect these without custom analysis. The signal-to-noise ratio is poor.
- **Refactoring "looks weird" code**. Code that works but isn't pretty is not the same as dead code. Out of scope here.

## Risk

Low overall. The CLI changes (sections 1–2) are user-visible and warrant a major-version bump and a clear changelog entry. The rest is internal-only.
