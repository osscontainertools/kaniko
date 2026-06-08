# mz762: scope `.dockerignore` to the build context when hashing `COPY --from` sources

## Problem

An allowlist-style `.dockerignore` (`*` then `!keep`) combined with a multi-stage `COPY --from` produces a stale image when the allowlisted file changes (osscontainertools/kaniko#762). With `--cache`, editing the only allowlisted file does not invalidate the layer that copies it; a subsequent build serves the old content.

PR #763 fixes this in `FileContext.ExcludesFile` by returning "not excluded" for any absolute path that lies outside the build context. We have gated that behind `FF_KANIKO_SCOPED_DOCKERIGNORE` (default off) and added unit + integration coverage. This document proposes the proper fix and argues the `ExcludesFile` guard is a symptom-level workaround that should be replaced rather than promoted to default.

## Root cause

`.dockerignore` patterns are matched against paths **relative to the build context**. `ExcludesFile` is fed two unrelated kinds of path:

1. **Build-context files** — e.g. `/workspace/keep`. Under `c.Root`; relativized to `keep` and matched. Correct, and exclusion here is necessary (an ignored file must not enter the cache key, since it is never copied).
2. **Inter-stage `COPY --from` sources** — e.g. `/kaniko/deps/builder/app/keep`. These live in a prior stage's extracted filesystem, not the build context.

The `COPY` command already resolves its sources with the right context. `copyCmdFilesUsedFromContext` switches to a deps-rooted context with **no** ignore patterns when `cmd.From != ""`:

```go
if cmd.From != "" {
    fileContext = util.FileContext{Root: filepath.Join(kConfig.KanikoInterStageDepsDir, cmd.From)}
}
```

But the **cache-key hasher does not get that context.** `stageBuilder.optimize` threads its single `fileContext` parameter — the *build* context (`Root = <context>`, `ExcludedFiles = .dockerignore`) — into `populateCompositeKey(command, files, …, fileContext, …)`, which calls `compositeKey.AddPath(p, fileContext)`, which calls `context.ExcludesFile(p)` unconditionally. So the resolved `/kaniko/deps/...` paths are matched against the *build context's* patterns. With an allowlist, the catch-all `*` matches the absolute path, `!keep` (anchored at the context root) does not rescue it, and the file is reported excluded → dropped from the cache key → stale image.

So the defect is not in `ExcludesFile` per se; it is that the per-command source context is not threaded into the cache-key computation for `COPY --from`. Applying the build context's `.dockerignore` to prior-stage paths is a category error — it only ever appeared harmless because, without an allowlist, no pattern happened to match those absolute paths.

## Proposed fix

Hash `COPY --from` source files with the same deps-rooted context the command already uses for resolution (`Root = KanikoInterStageDepsDir/<from>`, empty `ExcludedFiles`). Then `ExcludesFile("/kaniko/deps/<from>/app/keep")` relativizes to `app/keep`, matches against *empty* patterns, and never excludes — no `IsAbs` special case and no feature flag needed.

### Cache-key stability

`hashFile`/`hashDir` hash the file **content** (`util.CacheHasher`) at the absolute path; `context.Root` is used only for the `ExcludesFile` relativization, and `ExcludedFiles` only for the exclusion decision. Therefore, switching the context from build-rooted to deps-rooted changes the cache key **only** for paths whose exclusion decision flips — i.e. exactly the wrongly-excluded allowlist case. For every build that was not hitting the bug (no pattern matched the `/kaniko/...` path), the computed keys are byte-for-byte identical, so existing caches stay valid. This is the key argument for shipping the proper fix as default rather than behind a flag.

## Implementation options

### Option A — derive the context in `populateCompositeKey` (pragmatic)

In `populateCompositeKey` (it already receives `command`), detect a cross-stage copy and build the deps-rooted context before `AddPath`:

```go
fc := fileContext
if copyCmd, ok := commands.CastAbstractCopyCommand(command); ok && copyCmd.From() != "" {
    fc = util.FileContext{Root: filepath.Join(config.KanikoInterStageDepsDir, copyCmd.From())}
}
for _, f := range files {
    if err := compositeKey.AddPath(f, fc); err != nil { return compositeKey, err }
}
```

Smallest change; mirrors `copyCmdFilesUsedFromContext`. Downsides: duplicates the deps-context construction and pushes command-type knowledge (`CastAbstractCopyCommand`/`From()`) into the cache layer. The duplication can be removed by extracting a shared `commands.InterStageContext(from)` helper used by both sites.

### Option B — the command owns its source context (cleaner)

Let the command expose the context its `FilesUsedFromContext` paths belong to, so the hasher never has to know about `From`. Either return it alongside the files or add a method:

```go
// on DockerCommand
FilesUsedFromContext(*v1.Config, *dockerfile.BuildArgs) (util.FileContext, []string, error)
```

`CopyCommand`/`CachingCopyCommand` return the deps-rooted context for `--from`, the build context otherwise; every other command returns the build context. `optimize`/`populateCompositeKey` simply hash each file with the returned context. This removes the category error at the source and keeps the cache layer command-agnostic, at the cost of an interface change touching all command implementations.

**Recommendation:** Option B. It localizes "which context do these files belong to?" to the command that already answers it for resolution, and leaves `ExcludesFile`/`AddPath` free of `--from` special-casing. Option A is an acceptable interim if the interface churn is undesirable.

## Relationship to `FF_KANIKO_SCOPED_DOCKERIGNORE`

The proper fix makes the `ExcludesFile` `IsAbs` guard redundant: with the correct context, out-of-context absolute paths no longer reach the matcher with foreign patterns. Plan:

- Keep `FF_KANIKO_SCOPED_DOCKERIGNORE` **off by default** as the opt-in workaround for the current release.
- Land the proper fix (Option B) as **default** behavior — safe per the cache-key-stability argument above.
- Remove the `ExcludesFile` guard and the flag once the proper fix ships.

## Testing

- The existing `Test_ExcludesFile_AbsoluteOutsideBuildContext` unit test should be repointed: it currently asserts `ExcludesFile` behavior under the flag; with the proper fix the relevant assertion moves to the cache-key layer (a `populateCompositeKey`/`AddPath` test that a `COPY --from` source is hashed regardless of the build context's `.dockerignore`).
- `integration.TestCacheInvalidatesOnAllowlistedFileChange` should pass **without** `FF_KANIKO_SCOPED_DOCKERIGNORE` once the proper fix is default; drop the flag from `KanikoEnv` at that point.
- Add a cache-key-stability regression: for a build whose `.dockerignore` does **not** match the `/kaniko/...` source, assert the composite key is unchanged versus the pre-fix computation, to prove existing caches are not invalidated.

## Blast radius

Changes the cache-key computation path for `COPY --from`. The stability argument bounds the behavioral change to the allowlist case, but the change touches code shared by every cached build, so it warrants the integration cache suite plus a key-stability assertion before defaulting.
