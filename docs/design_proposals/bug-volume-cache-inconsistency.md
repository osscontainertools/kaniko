# Bug: VOLUME + cache produces inconsistent builds in multistage Dockerfiles

This is the same root cause as the `WORKDIR` cache bug at https://github.com/GoogleContainerTools/kaniko/issues/3340.

**Actual behavior**

When `VOLUME /vol` is declared, kaniko creates the directory implicitly via `os.MkdirAll`. But the directory is not snapshotted (`FilesToSnapshot` returns `[]`), so the timestamp change is never included in a layer. the cache key for the `VOLUME` instruction itself is unaffected, so this goes unnoticed in single stage builds.

In a **multistage** build the fresh `mtime` on `/vol` is visible to later stages that copy from this stage, causing a guaranteed cache miss on every subsequent run even when nothing in the Dockerfile has changed.

**Expected behavior**

The directory timestamp of a `VOLUME`-declared path should be stable across builds so that dependent cache keys are not invalidated. Two consecutive kaniko builds of the same multistage Dockerfile with `--cache` should both get a full cache hit after the first build.

**To Reproduce**

```dockerfile
FROM busybox AS base
VOLUME /vol
RUN echo "blubb" > /vol/b

FROM busybox
COPY --from=base /vol /vol
```

1. Build the image with kaniko and `--cache` (first run, cache miss) — succeeds.
2. Build the image again with the same `--cache` repo — the `COPY --from=base` stage gets a cache miss because `/vol`'s `mtime` changed when `VOLUME` recreated it in step 1.

**WORKAROUND**

Explicitly create the directory prior to calling `VOLUME`

```Dockerfile
RUN mkdir /vol
VOLUME /vol
```

**Fix**

`FF_KANIKO_SKIP_VOLUME_MKDIR=true` avoids the problem entirely by not creating the directory at all, matching Docker/BuildKit behaviour where `VOLUME` only declares a mountpoint in the image config without touching the filesystem, basically enforcing the workaround.
