# Cleanup: production symbols only reachable from tests

## Status
Survey. Each row is a separate candidate; the recommendation column is the suggested action, not a commitment.

## Background

`deadcode ./...` (`golang.org/x/tools/cmd/deadcode`) on the kaniko main binary lists functions unreachable from `main`. Filtering out `_test.go`, fakes/mocks, `testutil/`, and the `integration/` helper package leaves nine candidates. All of them compile into production binaries today but are only exercised by test code, an integration helper, or godoc.

The prior cleanup of `util.ParentDirectoriesWithoutLeadingSlash` (commit `c8dffc9b`) is the same pattern.

## Already done

- `warmer.ExampleWarmer_Warm` — deleted (commit `e06b100e`).
- `executor.ResolveCrossStageInstructions` — deleted along with four no-op call sites and the orphan test (commit `5bc9e946`).
- `snapshot.filesWithLinks` + `util.GetSymLink` — deleted along with `TestFileWithLinks` and the orphaned `setupSymlink`/`sortAndCompareFilepaths` test helpers (commit `e11bc167`). Function had been dead since `a675ad998` (2020-02-20).
- `util.GetInputFrom` — deleted along with `TestGetInputFrom` (commit `f5481bde`). One-line wrapper around `io.ReadAll`, dead since `2ea368dde` (2021-12-26) when stdin reading switched to streaming via `gzip.NewReader`.
- `util.CreateTarballOfDirectory` — moved into `integration/tar.go` as private `tarballOfDirectory` (commit `4c161701`). Introduced in `679c71c90` (2022-06-14) as an integration-test helper and never had a production caller. Its unit test (`Test_CreateTarballOfDirectory`) and `pkg/commands/add_test.go` (the latter's coverage is duplicated by `Dockerfile_test_add` and friends in the integration suite) were dropped at the same time.

## Decided to leave alone

| Symbol | Location | Reason |
|---|---|---|
| `util.OSFS` (type + `Open`) | `pkg/util/fs_util.go:1419` | Mechanical rename across 6 sites for a 6-line type. Not worth the churn. |
| `bucket.Delete` | `pkg/util/bucket/bucket_util.go:46` | Marginal cleanup. Also: GCP integration tests aren't actually exercised in CI, so the whole GCS path (including `bucket.Delete`'s single integration caller) is in a "we don't validate this" state — a bigger question than where to put one function. |
| `timing.Summary` / `timing.TimedRun.Summary` | `pkg/timing/timing.go:77,86` | Small, coherent public API of `pkg/timing`; revisit if `pkg/timing` ever shrinks further. |

## Future consideration

GCP integration tests are not run in CI. That means anything reachable only through GCS code paths (bucket cleanup, GCS context fetch in `cmd/executor` if untouched by other tests, etc.) is effectively unvalidated. Worth a separate pass to identify which GCS-related code is truly used by anyone vs. carry-over from upstream that we should drop.

## Detail by candidate

### `bucket.Delete` — move to `integration/`

`pkg/util/bucket/bucket_util.go:46`. Single caller: `integration/integration_test.go:102` (GCS cleanup between integration runs). The function does not belong in `pkg/util/bucket` if the only consumer is the integration test.

Plan: move the function (and any test for it) into the `integration/` package. `pkg/util/bucket` keeps the upload-side functions that production code actually uses.

### `util.OSFS` — move to `testutil`

`pkg/util/fs_util.go:1419` defines `OSFS` as a `fs.FS` implementation that calls `os.Open`. Production code uses `NoAtimeFS` exclusively (`FSys fs.FS = NoAtimeFS{}` at line 48). `OSFS` exists so tests can swap `FSys` to a regular reader; it is referenced from:

- `pkg/commands/copy_test.go` (2 sites)
- `pkg/util/command_util_test.go`
- `pkg/util/fs_util_test.go` (3 sites)

Plan: move the type to `testutil` (e.g. `testutil.OSFS`) since it's a cross-package fixture. Mechanical rename in the call sites.

## Risk

Low across the board for the deletions that were done. The `executor` and `util` packages are not formally part of the kaniko library API even though they are under `pkg/`. A changelog note under "Removed API" covers the exported deletions for any out-of-tree consumer.
