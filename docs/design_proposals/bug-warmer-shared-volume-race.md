# Plan: warmer concurrency lock, gated behind `FF_KANIKO_WARMER_CACHE_LOCK`

## Context

Issue #364 reports a TOCTOU race in the cache warmer: two warmer processes sharing the same cache volume can both decide an image is missing, both download it, and then race on the final rename-into-place. With the legacy tarball cache the loser silently overwrites the winner (wasted bandwidth). With the new ocilayout cache (`FF_KANIKO_OCI_WARMER`) the loser's `os.Rename` of a directory onto a non-empty destination fails with `ENOTEMPTY` and the warmer exits non-zero — the user-visible failure mode that motivated the bug.

PR #705 (external contribution) proposes a per-digest `flock(2)` on `cacheDir/.warmer-locks/<digest>.lock` taken around the recheck + rename step. The idea is correct. Two issues to address before merge:

1. **No integration test exercises the race.** The existing `TestWarmerTwice` is sequential and (since commit 9f4021ae) gives each subtest its own tmp cache dir, deliberately working around the race. The PR's unit tests cover the lock primitive itself but not the end-to-end behavior with two warmer containers contending on a shared volume. We need a test that fails on `main` without the fix and passes with it.
2. **No feature flag.** The PR argues the fix falls under the "behavior so broken no working workflow could depend on it" exception in `docs/releases.md`. That's defensible for the `ENOTEMPTY` failure under `FF_KANIKO_OCI_WARMER`, but the same code path also changes legacy tarball behavior (silent overwrite → "drop our copy, keep theirs" with a new log line). Per release policy when in doubt, gate it. Picking a flag also makes the integration test easy: run the same test with the FF on (expect success) and the FF being default-off means existing users see no behavior change.

Outcome: warmer is safe under concurrent shared-volume usage when `FF_KANIKO_WARMER_CACHE_LOCK=true`, with an integration test that proves both directions (race reproduces with FF off in the OCI path; FF on fixes it).

## Approach

Three pieces of work, in this order:

### 1. Integration test: revert 9f4021ae and see if `TestWarmerTwice` reproduces the race naturally

Commit 9f4021ae gives each `TestWarmerTwice` subtest its own `tmpDir` precisely to dodge this bug. Reverting it puts all 3 subtests back on a shared volume:

| Subtest | Reference | Final cache key |
|---|---|---|
| 1 | `debian:trixie-slim` | digest A |
| 2 | `debian:12.10@sha256:264982…` (image-index) | digest M (resolved to per-arch manifest via `warm.go:202-244`) |
| 3 | `debian:12.10@sha256:6bc30d…` (image-manifest) | digest M (direct) |

Subtests 2 and 3 both end up writing to `cacheDir/<M>`, so the revert gives a free 2-way same-digest race; subtest 1 adds cross-digest noise (validates the lock granularity is per-digest, not global).

**Plan:**

1. Revert 9f4021ae on `main` (no fix, no FF wiring).
2. Run `TestWarmerTwice` in OCI mode several times (5–10), count `ENOTEMPTY` triggers.
3. Branch on result:
   - **≥80% reproduce** — revert alone is sufficient. Skip to step 2 (lock impl).
   - **50–80% reproduce** — add a 4th subtest that targets digest M via yet another reference (e.g. by tag) to widen the race surface.
   - **<50% reproduce** — write a dedicated `TestWarmerConcurrent` that launches K=4 warmer containers in parallel against the same image via `sync.WaitGroup`. Independent of `t.Parallel` scheduling.

Once the test reliably reproduces the bug, wire `FF_KANIKO_WARMER_CACHE_LOCK=1` into `WarmerEnv` in `integration/images.go:128` (after the fix lands) so all warmer integration tests exercise the new path.

### 2. Lock primitive + wiring

New file `pkg/warmer/lock.go` with `acquireCacheLock(cacheDir, key string) (release func(), err error)`:

- `os.MkdirAll(cacheDir/.warmer-locks, 0o755)`
- `os.OpenFile(cacheDir/.warmer-locks/<key>.lock, O_RDWR|O_CREATE, 0o644)`
- `unix.Flock(fd, LOCK_EX)`
- Return a `release` closure that flock-unlocks and closes (idempotent).

This is essentially the PR's primitive. `golang.org/x/sys/unix` is already a transitive dep — no new modules.

Wire into both warm paths in `pkg/warmer/warm.go`:

- `warmToFile` (legacy tarball, line ~88): after `cw.Warm(...)` succeeds and `finalCachePath` is computed (line 121), gate the lock + recheck + rename block on `config.EnvBool("FF_KANIKO_WARMER_CACHE_LOCK")`. When off: keep existing `os.Rename` for both file and manifest (legacy behavior preserved bit-for-bit). When on: acquire lock, re-stat `finalCachePath`; if present log "Image %v became available in cache while warming; keeping existing copy" and return nil, else rename file then manifest.
- `ociWarmToFile` (line ~139): same shape. Gate the lock + recheck on the FF. The recheck is what prevents the `ENOTEMPTY` even with the lock held — the second warmer must skip the rename, not retry it.

Per `feedback_comments.md`: no new explanatory comments in the wiring sites unless they document a non-obvious invariant. The PR's verbose comments can be trimmed; the FF name + the conditional structure read clearly enough.

### 3. Documentation

In `README.md`:

- Add a ToC entry at the bottom of the feature-flag list (line ~142, after `FF_KANIKO_CACHE_PROBE_AFTER_MISS`).
- Add a section body in the feature-flag descriptions block (after the `FF_KANIKO_CACHE_PROBE_AFTER_MISS` section, line ~1219). Follow the existing pattern: 1 paragraph problem statement, 1 sentence "Set this flag to `true` to ...", `Defaults to false.`, `Becomes default in vX.Y.Z.` once a target minor is picked.
- Update the existing `FF_KANIKO_OCI_WARMER` section (line 1134): replace "Note that currently there is no mutex lock mechanism yet, so it does not support multiple parallel writes." with a reference to the new flag.

No `docs/releases.md` change needed — the policy doc already covers feature-flag lifecycle.

## Critical files

- `pkg/warmer/warm.go` — add FF-gated lock + recheck blocks in `warmToFile` and `ociWarmToFile`
- `pkg/warmer/lock.go` — new, contains `acquireCacheLock`
- `pkg/warmer/lock_test.go` — new, unit tests for the primitive (mutual exclusion, different keys don't block, release is idempotent). Adapted from PR #705's tests.
- `integration/integration_test.go` — new `TestWarmerConcurrent`; optionally revert the per-subtest tmpDir in `TestWarmerTwice`
- `integration/images.go` — add `FF_KANIKO_WARMER_CACHE_LOCK=1` to `WarmerEnv` (line 128)
- `README.md` — ToC entry + new section body + edit of the `FF_KANIKO_OCI_WARMER` note

## Reused functions

- `config.EnvBool(key string) bool` from `pkg/config/options.go:196` — gates the new path, same pattern as the existing `FF_KANIKO_OCI_WARMER` check at `pkg/warmer/warm.go:62`.
- `golang.org/x/sys/unix.Flock` — already in vendor (used by go-billy transitively, per PR description).
- Existing `WarmerEnv` array in `integration/images.go:128` — the integration test framework already propagates this into `docker run -e` flags for warmer containers (see `integration_test.go:883`).

## Verification

**Reproduce the race on main first** (proves the test catches the bug we're fixing):

1. Check out `main` at e28a9a0db.
2. Add only the new `TestWarmerConcurrent` test (no fix yet), with `FF_KANIKO_OCI_WARMER=1` from `WarmerEnv`.
3. Build warmer image and run the test inside the sandbox. Expect: at least one container fails with `ENOTEMPTY`.

**Verify the fix:**

1. Apply lock + wiring + FF.
2. Re-run `TestWarmerConcurrent` with `FF_KANIKO_WARMER_CACHE_LOCK=1`. Expect: all containers exit zero, exactly one `<tmpDir>/<digest>` directory present, "Image ... became available in cache while warming" appears in at least N-1 of the logs.
3. Re-run `TestWarmerConcurrent` with `FF_KANIKO_WARMER_CACHE_LOCK=0`. Expect: same failure as the main-baseline run, confirming the FF gates the new behavior.
4. Run existing `TestWarmer` and `TestWarmerTwice` with the FF on. Expect: pass (no regression in the sequential paths).

**Unit-level:**

- `go test ./pkg/warmer/... -race` — passes, including the new `lock_test.go` (mutual-exclusion goroutine test, idempotent release).

**Build / lint:**

- Build executor + warmer images inside the sandbox per `feedback_kaniko_sandbox.md` — never run `out/executor` or `out/warmer` directly on the host.
