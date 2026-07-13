# mz334: stage elimination using the cache-lookahead precompute

## Where we are

`FF_KANIKO_CACHE_LOOKAHEAD` (introduced in part-2b) adds a precompute pass before the real build: for each `KanikoStage` we instantiate a `stageBuilder` and call `optimize(..., hasContext=false)`. That populates a `stageCacheInfo` (per-command `cacheKeys`/`cacheHits` and redirect keys), and the stage's `finalCacheKey` lands in `stageFinalCacheKeys[stage.Index]`.

Today we only use those precomputed keys to:

- Render `CACHE HIT` / `CACHE MISS` annotations in the dryrun plan (`RenderStages`).
- Assert in the real build loop that the precomputed `finalCacheKey` matches the one computed during `optimize(..., hasContext=true)` — the correctness fence (`pkg/executor/build.go:1156`).

Static stage elimination (skip-unused-stages, `FF_KANIKO_SQUASH_STAGES`) already runs upstream in `dockerfile.MakeKanikoStages`. It only looks at the dockerfile graph (FROM / COPY --from edges), not at the cache.

The next step is to feed precompute results back into the build plan and **dynamically drop stages whose results are already in the cache**. The historical attempt (commit `ad9a05ae0`, reverted by `2719b7fee`) did this with squashing — that part is now done statically in dockerfile.go, so this design is narrower: only cache-driven elimination, on top of the existing graph-driven elimination.

## What "eliminate" means here

Three terminal states for a stage after precompute:

1. **Build normally** — at least one command misses cache, or the cached result cannot replace the stage's role (e.g. it must be unpacked because a downstream stage `FROM`s an uncached stage that branches off it).
2. **Materialize from cache, do not run commands** — every command in the stage cache-hits. The final layer in the registry, combined with the cached intermediate layers, fully reconstructs the stage's FS / image. We still need the *result* (because something downstream consumes it), but we never execute commands or unpack the base into the rootfs.
3. **Drop entirely** — the stage exists only to feed downstream consumers, and every consumer's reference to it is itself cache-resolved. No need to fetch the base image, no need to materialize anything.

(1) is the status quo. (2) and (3) are what this design adds.

The cleanest distinction: (2) is about *not running commands*, (3) is about *not even constructing a stageBuilder*.

## Decision algorithm

After precompute populates `cacheInfo[idx]` for every stage, walk stages and classify them.

For each stage `s`, define:

- `s.fullyCached` ≡ every non-`MetadataOnly` command in `s` had `cacheHits[i] == true` during precompute (i.e. precompute's `optimize` actually swapped the command for a `Cached` impl).
- `s.consumedAsBase` ≡ some later stage has `BaseImageStoredLocally && BaseImageIndex == s.Index`.
- `s.consumedAsCopyFrom` ≡ some later stage has a `COPY --from=<s.Index>`.

Then walk consumers backward to compute `s.fsNeeded`: does anybody downstream actually need `s`'s filesystem materialised?

- A `FROM s` consumer needs `s`'s FS unless that consumer is itself in state (3).
- A `COPY --from=s` consumer needs `s`'s FS unless the consuming COPY command itself cache-hits (which the precompute pass already knows: it's the redirect hit recorded in `cacheInfo[consumer].redirectHits[copy_index]`, gated by `FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY`).
- The final stage always needs its FS materialised (we push it).

Classification rule:

| `fullyCached` | `fsNeeded` | classification |
|---------------|------------|----------------|
| false         | true       | (1) build normally |
| false         | false      | unreachable — see below |
| true          | true       | (2) materialize-from-cache |
| true          | false      | (3) drop |

The `fullyCached=false && fsNeeded=false` row is *currently* unreachable: if a stage isn't fully cached but nobody needs its FS, static skip-unused-stages would already have eliminated it. We assert this and panic if it ever fires — that catches a logic regression cheaply.

## Concrete cases (from `test_issue_mz334/Dockerfile`)

```
FROM busybox AS first
RUN touch /blubb

FROM first  AS second
RUN touch /bla

FROM second AS third
RUN touch /bli

FROM second AS final
COPY --from=first /blubb /blubb
COPY --from=third /bli   /bli
RUN ls -lah /blubb
```

Suppose everything is in the cache. The classification under this design:

- `first`: fully cached. Consumed as base by `second`. `fsNeeded` propagates up from `second`. If `second` is dropped, and `first`'s COPY --from in `final` is a redirect-hit, `first` drops entirely (no base fetch, no FS).
- `second`: fully cached. Consumed as base by `third` and `final`. Both downstream consumers need `second` as their base FS unless they themselves drop.
- `third`: fully cached. Only consumed via `COPY --from=third /bli` in `final`. If that COPY is a redirect-hit, `third` drops; otherwise we materialize from cache to extract `/bli`.
- `final`: must build (it's the push target), but every command precomputed as a hit → final is in state (2): materialize from cache.

The all-cached happy path collapses the build to "pull final image from cache, push it". That's the headline win.

## Where this slots into build.go

The precompute loop currently lives at `pkg/executor/build.go:1004-1051`. The real build loop at `pkg/executor/build.go:1113-1255` does the work today. Stage elimination changes both:

### Precompute pass

Augment the precompute output. Today it produces `cacheInfo[]` and writes `stageFinalCacheKeys`. Add:

- A per-stage `disposition` (drop / materialize / build), computed after the precompute walk by the classification above.
- For materialize: capture the cached final image reference (the layer returned by `layerCache.RetrieveLayer(finalCacheKey)`) so the real build doesn't repeat the lookup.

The precompute pass already calls `layerCache.RetrieveLayer` per command via `optimize`. We don't need new round trips — we just need to thread the per-command hit signal up to a per-stage signal, and reuse the layer references.

One blocker: precompute currently aborts cache key tracking when it hits `COPY --from` because `populateCompositeKey` cannot hash a not-yet-built stage's files (`pkg/executor/build.go:323-330`). But the `crossStageCacheKey` shortcut (using `stageFinalCacheKeys` instead of hashing files) already exists for the build pass — it just isn't invoked from precompute. To make `fullyCached` meaningful for stages that contain `COPY --from`, precompute must take the shortcut path too. That is a prerequisite — without it, any stage with `COPY --from` always classifies as (1).

This prerequisite is small but it changes the meaning of the assertion at `build.go:1156`: precompute will now produce non-empty `finalCacheKey` for stages with `COPY --from`, and the real-build key must match. The existing equality check already handles that — but we should add a golden test that includes `COPY --from` precisely to exercise it.

### Real build pass

Branch by disposition:

- **drop**: skip the stage entirely. Do not call `RetrieveSourceImage`, do not call `newStageBuilder`, do not record into `stageArgs`. Cross-stage deps (`SAVE FILES`) must already be unneeded — that's part of `fsNeeded=false`.
- **materialize**: construct a `stageBuilder` but replace `sb.build(...)` with a fast path that sets `sb.image` to the cached image and computes the cross-stage outputs without running commands. We still need `reviewConfig` and `mutate.Config` to produce the right config for downstream `FROM` consumers and for `final.push`.
- **build**: today's path, unchanged.

The materialize path's tricky part: if a downstream stage uses this stage's FS via `FROM` and that downstream stage is **not** itself materialized (some downstream command is a miss), we need the FS unpacked into `/kaniko/stages/N`. So materialize still needs an "unpack into stage dir" step when downstream consumers need the FS. That's the same condition that drives `SaveStage` today.

For final-stage materialize with no downstream cross-stage consumers, the fast path is: pull cached final image, run push logic, exit. No snapshotter, no unpack, no command loop.

## Stage args propagation

`stageArgs` (`build.go:1002`) tracks each stage's `BuildArgs` because downstream `FROM stage_N` inherits the parent's args. Today, both the precompute and the real build pass populate it. With drops, we still need `stageArgs[droppedIndex]` populated for any consumer that does `FROM droppedStage` — *unless* the consumer is itself dropped. The simplest rule: always populate `stageArgs` in the precompute pass; drops only skip the real build, not the precompute bookkeeping. (Precompute already does this today.)

## Cross-stage dependency files

`crossStageDependencies` is calculated up front from the dockerfile and tells us which files each stage must `SAVE FILES` for downstream use. A materialized stage still has to produce those files — it must unpack just enough to copy them out, OR a smarter path: read the files directly from the cached image's layers without unpacking. The simple approach (unpack-then-copy) is enough for v1; the layer-direct path is an optimization for later.

A dropped stage by construction has no downstream FS consumers (that was the condition for dropping), so it has no `SAVE FILES` obligation.

## Plan rendering

The dryrun `RenderStages` output is what the golden tests assert on. With elimination, the plan changes meaningfully:

- A dropped stage produces a single line: `DROP STAGE <name>` (or omitted entirely — TBD by what reads better in golden diffs).
- A materialized stage replaces `UNPACK ... <commands> ... CLEAN` with something like `MATERIALIZE FROM CACHE: <finalCacheKey>` followed by `SAVE FILES` / `SAVE STAGE` as needed.
- A normally-built stage looks exactly like today.

Concrete: the `test_issue_mz334/plans/cached` golden currently shows every command with a `CACHE HIT:` line. Under elimination it should collapse the all-hits case to a single `MATERIALIZE` line per stage, dramatically shortening the plan.

We will need a third plan file for the test (or replace `cached` with the elimination output). I lean toward an additional `eliminated` plan, gated by a new feature flag (see below), so we can ship behind a flag and keep the existing assertion path intact for one release.

## Feature flag

Introduce `FF_KANIKO_CACHE_STAGE_ELIMINATION`, off by default. It requires `FF_KANIKO_CACHE_LOOKAHEAD=1` to take effect — assert this combination at startup. The flag wraps:

- the disposition classification,
- the drop/materialize branches in the real build loop,
- the new `RenderStages` output.

Keeping it separate from `FF_KANIKO_CACHE_LOOKAHEAD` lets us turn on elimination without losing the lookahead assertion safety net during rollout.

## Prerequisites and ordering

These should land as separate commits, in this order:

1. **Precompute past `COPY --from`**. Replace the `stopCache=true, keyValid=false` bailout in `optimize` with the `crossStageCacheKey` shortcut when `!hasContext` and `stageFinalCacheKeys[fromIdx]` is set. Validates immediately via the existing equality assertion. Golden test: extend `test_issue_mz334` so the precompute plan's `CACHE HIT/MISS` annotations cover the `COPY --from` commands in `final`.
2. **Classification scaffold**. Compute disposition per stage after the precompute walk, log it, but still take the existing build path. Golden test renders disposition labels but does not change behaviour.
3. **Materialize fast path**. Implement (2) — fully-cached stages that still need their FS get materialized from the cached final image instead of running commands. Behind `FF_KANIKO_CACHE_STAGE_ELIMINATION`.
4. **Drop fast path**. Implement (3) — fully-cached stages with no FS consumers are dropped completely.
5. **Final-stage materialize push-only fast path**. Special-case the common "everything cached, final image pulled from cache and pushed" path so we skip snapshotter init and `fetchExtraStages` when nothing remains to build.

Each step is independently testable via golden tests with the `KeySequence` mechanism already used in `test_issue_mz334`.

## Risks

- **Image equivalence**. The layer cache stores per-command diffs; "materialize from cache" relies on the assumption that fetching+applying all cached layers yields a bit-identical image to building from scratch. The current per-command cache substitution already relies on this. For state (2) we additionally need to make sure config-file mutations (`reviewConfig`, OS/arch override, labels) still happen — `sb.image` is set, but the subsequent `mutate.Config` / `mutate.ConfigFile` block at `build.go:1168-1190` must still run.
- **`fetchExtraStages`** (`build.go:1064`) pulls images referenced via `--from=<external-image>` or similar non-stage references. Dropping a stage that contained such a reference would skip its fetch. The cleanest rule: precompute walks every stage's commands, so `fetchExtraStages` runs based on the union of all stages including dropped ones. (Today `fetchExtraStages(kanikoStages, opts)` already gets the full list — so this is automatically correct as long as we don't filter `kanikoStages` before that call.)
- **`PreserveContext` interactions**. The build loop saves and restores the build context across stages. A drop must still leave the snapshotter in the right state for the next stage. Materialize might be able to skip snapshot ops entirely if nothing in the materialized stage touched the rootfs.
- **Cache poisoning blast radius**. With elimination, a single poisoned cache entry can short-circuit an entire chain of stages. The lookahead assertion (`precomputedKey == finalCacheKey`) doesn't help here because elimination *replaces* the build pass — there's nothing to assert against. Mitigation: keep the assertion alive for stages classified as (1), and accept that (2)/(3) inherit the existing per-command-cache trust assumption.
- **`fullyCached` vs. the `stopCache` semantics**. Today `optimize` may set `stopCache=true` on a miss (or under `FF_KANIKO_CACHE_PROBE_AFTER_MISS`, may continue probing). `fullyCached` must agree with "every non-MetadataOnly command was actually replaced with a `Cached` impl", not just "every key resolved" — `MetadataOnly` commands are not in the layer cache and must be excluded from the count.

## Open questions

- Should `FF_KANIKO_CACHE_STAGE_ELIMINATION` subsume `FF_KANIKO_CACHE_LOOKAHEAD` once stable, or stay as a separate knob long-term?
- Do we want a "soft" mode where we classify and log dispositions but never actually drop/materialize? Could be useful for telemetry-only rollout before flipping behaviour.
- For materialize: pull the entire image vs. apply layer-by-layer? The former needs fewer registry round trips but breaks if any single intermediate layer is missing while the final is present. The latter matches today's per-command path more closely. Start with the latter for safety.
