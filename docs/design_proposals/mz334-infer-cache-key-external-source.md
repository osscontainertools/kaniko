# mz334: infer cross-stage cache key — part 2 (external image sources)

## Where we are

PR [#618](https://github.com/osscontainertools/kaniko/pull/618) introduced `FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY`. When set together with `--cache-copy-layers`, a `COPY --from=<stage>` emits **two** cache entries side by side:

- the existing content-addressed entry, keyed by hashes of the copied files;
- a new cachekey-addressed entry, keyed by the source stage's precomputed `finalCacheKey` (a pointer that resolves to the content key).

The pointer entry is what lets cache-lookahead skip the filesystem scan: when we already trust the source stage's identity via its final cache key, the COPY layer's cache key can be derived without reading any files. The content-addressed entry is retained because it remains more stable in the opposite direction — file contents can be unchanged while the source stage's `finalCacheKey` shifts (e.g. metadata changes upstream). Both writes happen on the build pass; the optimize pass can hit on either.

The shortcut today only fires for **local stage references**: `crossStageCacheKey` (`pkg/executor/build.go:202`) parses `copyCmd.From()` as a numeric stage index via `strconv.Atoi` and returns false on failure. `resolveCrossStageCommands` (`pkg/dockerfile/dockerfile.go:265`) rewrites `COPY --from=<name>` to the numeric form only when the name matches a known stage; external image references like `COPY --from=alpine:3.18` stay as image refs.

So external-image `COPY --from` still walks the file-hashing path: kaniko pulls and unpacks the external image (via `fetchExtraStages` machinery) and `populateCompositeKey` hashes every file the COPY reads. That's expensive — large external images make the cache key computation itself a significant cost — and it's avoidable, because the external image already has a globally stable content-addressable identifier: its digest.

## What this part adds

Extend the inferred-key shortcut to external-image `COPY --from` sources by using the source image's digest as the stand-in identifier. Functionally:

- For `COPY --from=<stage>`: behaviour unchanged (use `stageFinalCacheKeys[fromIdx]`).
- For `COPY --from=<image-ref>`: resolve the image's digest once, use it as the inferred-key contribution.

The composite cache key then no longer depends on the file content scan of an external image — only on its registry-resolved digest, the COPY command string, and any args. The redirect-pointer machinery (`pushPointer` / `redirectCacheKey`) extends naturally to this case.

This is **strictly an optimization**: the file-hashing path remains correct. The flag continues to gate the optimization so we can validate equality before flipping the default.

## Why it belongs separate from cache-lookahead

PR #618 framed the two features as something to "activate together" — that's still the rollout plan for lookahead, because lookahead has nothing to use the inferred keys for if they aren't emitted. But the *extension to external sources* is a strict superset of what part 1 does and stands on its own: it doesn't depend on lookahead, doesn't depend on stage elimination, and improves cost in the standalone infer-cache-key path. Bundling it into the cache-lookahead workstream would conflate two rollouts and force users to opt into both behaviours together; treating it as part 2 of infer-cache-key keeps the flag matrix clean.

## Code change sketch

`crossStageCacheKey` becomes a two-branch lookup:

```go
func crossStageCacheKey(command commands.DockerCommand, stageFinalCacheKeys map[int]string, externalImageDigests map[string]string) (string, bool) {
    copyCmd, ok := commands.CastAbstractCopyCommand(command)
    if !ok || copyCmd.From() == "" {
        return "", false
    }
    if fromIdx, err := strconv.Atoi(copyCmd.From()); err == nil {
        cacheKey, ok := stageFinalCacheKeys[fromIdx]
        return cacheKey, ok
    }
    digest, ok := externalImageDigests[copyCmd.From()]
    return digest, ok
}
```

`externalImageDigests` is populated upstream from the same place that fetches external `COPY --from` images today (`fetchExtraStages` and its callers). We resolve each external image once, cache its `Image.Digest()`, and thread the map down through `optimize` / `populateCompositeKey` alongside `stageFinalCacheKeys`.

The optimize-side block (`pkg/executor/build.go:333`) does not need new branches — it just calls into `populateCompositeKey` with the augmented signature.

The redirect-pointer write (`pkg/executor/build.go:576`) works unchanged: a pointer keyed by the inferred key resolving to the content key already covers the external-image case the moment the inferred key starts being computed.

## Stability vs. PR #618's local-source path

PR #618 highlighted that the inferred key for a local stage can be *less* stable than file-hashing in some scenarios — upstream metadata changes can shift `finalCacheKey` without changing the files the COPY touches. That argument doesn't translate to external image sources: the manifest digest is the registry-canonical content identifier, so the inferred key for `COPY --from=alpine@sha256:...` is at least as stable as file-hashing, and often more so (no transient FS attributes leak into the hash).

For unpinned tag references (`COPY --from=alpine:3.18`), the digest can shift between builds when the tag is republished — but that's the same caveat external base images already carry (`FROM alpine:3.18` has identical behaviour). The inferred key inherits this; it does not make caching less safe than the file-hashing path, since both are computed *after* the image has been resolved.

Like part 1, both cache entries (content-addressed + cachekey-addressed) are emitted, so a downstream consumer that hashes files identically gets a cache hit either way.

## Scope

Strictly: when `FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY=1`, the existing redirect-pointer logic now also fires for `COPY --from=<external-image>`. No new flag, no change to the flag default, no change to the local-stage path. A consumer that has the flag off sees no behavioural difference; a consumer with the flag on simply gets the optimization on more `COPY --from` instructions.

## Test plan

A golden test under `golden/testdata/test_issue_mz334_external/` covering `COPY --from=<external-image>` with `--cache --cache-copy-layers` and `FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY=1`. Pin the source by digest (`alpine@sha256:...`) so the golden output stays stable, and assert the plan emits `CACHE REDIRECT HIT/MISS` on the COPY just as local-source tests do today.

## Open questions

- Where does the `externalImageDigests` map get built? Cleanest is at `fetchExtraStages` time, since that's where we already pull the external image. The map needs to be in scope wherever `populateCompositeKey` is called for a COPY command.
- Image digest vs. config digest vs. manifest digest: `Image.Digest()` returns the manifest digest. That's the right choice — it's what registry deduplication uses and what `FROM image@<digest>` references.
