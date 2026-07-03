# Assertions that multiply the differential fuzzer

* Author: Martin Zihlmann
* Date: 2026-07-03
* Status: Proposed

## Why assertions are the next lever

The differential fuzzer (`docs/design_proposals/dockerfile-fuzzing.md`) ranks findings crash > build-outcome > cache diff > docker diff, and its crash detector matches the `Assertion violated [name]:` prefix that `util.Assert` panics with. That gives every asserted invariant three properties the diff oracles cannot offer.

- Oracle independence. A diff only fires when kaniko disagrees with docker or with itself. An assertion fires when kaniko violates its own invariant, even on a case where both tools happen to emit the same wrong thing, and even on a case whose diff is already classified as a known divergence and therefore suppressed.
- Self-triage. A diff finding needs manual root-causing. An assertion carries its name and message, so the report arrives half-triaged, as the fuzzer's crash path already surfaces the assertion name directly.
- Precision in time. A diff observes the final image, far from the defect. An assertion fires at the moment the invariant breaks, in the function that broke it.

The fuzzer runs with assertions live (`KANIKO_IGNORE_ASSERTIONS` unset), so every assertion added below immediately widens what a campaign can catch. The reverse also holds: the three bugs the fuzzer found so far (mz863, mz864, mz868) were all in emitted-output shape or destination resolution, areas where today's assertions are thinnest.

## What exists

The tree already carries about fifty assertions. Almost all are bookkeeping invariants: counts monotone (`expose.port-count-monotone`), slices in sync (`layeredmap.slices-sync`), non-nil state (`executor.stagebuilder.config-nonnull`), cache-key agreement (`executor.build.cache-lookahead`). They protect internal control flow. Only one guards the emitted layer content itself (`tar.root-path-excluded` in `AddFileToTar`).

What is missing is the output-shape layer: invariants on the tars, whiteouts, config, and manifest that kaniko actually emits. That is where the fuzzer's findings live, and where docker parity bugs hide silently.

## Proposed assertions

### Tier 1: layer tar shape, in `util.Tar` (`pkg/util/tar_util.go`)

`Tar` is the choke point: every entry of every emitted layer passes through `AddFileToTar` or `Whiteout`, and the struct already keeps per-tar state (`hardlinks`). Add a `seen map[string]struct{}` of written header names and assert on it.

| Name | Invariant | Catches |
|---|---|---|
| `tar.entry-unique` | a header name is written at most once per tar | duplicate entries make extraction order-dependent; a dup is always a snapshot bookkeeping bug |
| `tar.name-clean` | header name is relative, non-empty, and contains no `..` segment | path traversal and root-dir trimming regressions, complements the existing `tar.root-path-excluded` |
| `tar.whiteout-conflict` | a tar never contains both `foo` and `.wh.foo` | an add and a delete of the same path in one layer is a contradiction; extraction behavior is undefined |
| `tar.hardlink-target-in-tar` | a `TypeLink` header's `Linkname` names an entry already written to this tar | `checkHardlink` designates the first path of an inode as the target; if the link is emitted without it, extraction fails or links to stale content (mz2595 territory) |
| `tar.kaniko-excluded` | no header name is `kaniko` or under `kaniko/` | the executor leaking into the image is a known hazard (the tainted-executor fixtures exist because of it); this is the last line of defense after the ignore list |

Cost: one map insert per entry, freed with the tar. Names follow the existing `tar.*` prefix.

### Tier 2: snapshot set consistency, in `writeToTar` (`pkg/snapshot/snapshot.go:257`)

| Name | Invariant | Catches |
|---|---|---|
| `snapshot.add-whiteout-disjoint` | `files` and `whiteouts` share no path | the snapshotter deciding a path was both changed and deleted in one snapshot; upstream of `tar.whiteout-conflict`, firing closer to the defect |

### Tier 3: final image assembly, before push (`pkg/executor/build.go` around the `pushImage` assembly at line 1314, or a `checkImage` helper in `pkg/image`)

These are OCI conformance invariants. Violating them produces a structurally malformed image that some registries and runtimes accept and others reject, which is worse than a clean failure.

| Name | Invariant | Catches |
|---|---|---|
| `image.layers-history-sync` | `len(layers)` equals the count of history entries with `EmptyLayer == false` | history bookkeeping drift; `image/transform.go` already asserts alignment for the base prefix, this extends it to the whole image at the moment it is final |
| `image.diffids-sync` | `len(config.RootFS.DiffIDs)` equals `len(layers)` | config/manifest divergence during assembly |
| `image.mediatype-family` | manifest, config, and every layer media type belong to the same family, all docker v2 or all OCI | mixed-family assembly; the mz851 media-type work makes this a live seam, and the fuzzer once observed a cache-populate and cache-consume pair diverge to different families in a flake that an in-process assertion would have pinned down. Already implemented on the PR #858 branch, so it needs no new work here, only that branch landing |

### Tier 4: mode-scoped

| Name | Invariant | Catches |
|---|---|---|
| `reproducible.epoch-timestamps` | with `--reproducible`, every tar header ModTime in an emitted layer is the epoch | timestamp leaks that break reproducibility, mz731 and mz851 territory; assert in the tar writer gated on the option rather than diffing after the fact |

## What assertions will not catch

Honest limits, so the fuzzer's oracles stay in place alongside them.

- Behavior divergence that is internally consistent. mz863 and mz864 violated no kaniko invariant; kaniko believed 0755 and root ownership were intended. Only the docker oracle sees those.
- Errors kaniko already reports. mz868 fails the build with a clear error; an assertion adds nothing there. The build-outcome oracle catches that class.
- Anything in code kaniko does not execute, which is what coverage admission is for.

Assertions target the third class of bug: silent output corruption that neither errors nor necessarily diverges from docker on the cases the fuzzer happens to compare.

## Rollout

1. Tier 1 and Tier 2 first: one struct field and six assertions, all in code every build exercises, so a single fuzz hour regression-tests them against about 400 builds.
2. Tier 3 next: needs a small helper reading manifest and config at assembly time.
3. Tier 4 with the next reproducible-mode work, gated on the option.

Assertion style per repo convention: the message states the invariant only, the reason lives in a comment above the call, and every name is unique so `KANIKO_IGNORE_ASSERTIONS` can disable one precisely. Each new assertion should land with a fuzz campaign run to confirm it does not fire on the known-good corpus before it ships.
