# Docker vs kaniko divergences found by differential fuzzing

* Author: Martin Zihlmann
* Date: 2026-07-03
* Status: Findings log

This report lists the docker versus kaniko divergences surfaced by the differential fuzzer (`integration/fuzz_test.go`, `fuzz_gen_test.go`, `fuzz_classify_test.go`, `fuzz_coverage_test.go`, `fuzz_shrink_test.go`). Each entry was reduced to a minimal reproducer by the shrinker and then confirmed by hand, either by inspecting the image config and layer tars with `crane` or by reading the kaniko source. The fuzzer runs with the full `KanikoEnv` compatibility flag set, docker builds with `--provenance=false`, and the harness detects each base image's media type and tells docker to emit the same, so a media-type or format difference is never reported.

## Summary

| Class | Trigger | docker | kaniko | Status |
|---|---|---|---|---|
| chmod on implicit parent dir | `COPY --chmod=M f /new/f` | `/new` gets mode M | `/new` stays 0755 | filed mz863 |
| ownership of implicit parent dir | `USER u` then `WORKDIR /new/sub` | `/new` owned by u | `/new` owned by root | filed mz864 |
| dangling-symlink dest resolution | copy a symlink, then COPY through it | builds | build fails | real, not filed |
| history for metadata instructions | `ENV`, `LABEL`, ... | one history row each | no row emitted | expected divergence |
| no-op RUN unchanged dir layer | a RUN with no net change on an existing dir | empty layer | layer holds the unchanged dir | expected divergence |
| copy symlink dereference | `COPY symlink /dest/` | dereferences to a file | preserves the symlink | expected, kaniko correct |
| oci vs docker media type | any build | OCI by default | mirrors the base | dismissed, harness artifact |

## Filed bugs

### mz863 and mz864 are mirror images

Both are the same defect: a directory that kaniko creates implicitly, as a side effect of another instruction, does not get the attributes buildkit gives it. They surface on different attributes through two different helpers, and each helper gets one attribute right and the other wrong.

| | mz863, COPY `--chmod` | mz864, WORKDIR |
|---|---|---|
| Code path | `createParentDirectory` (`pkg/util/fs_util.go`) | `MkdirAllWithPermissions` (`pkg/util/fs_util.go`) |
| Chowns intermediate dirs | yes | no, only the leaf |
| Applies the intended mode to intermediate dirs | no, hardcodes 0755 | not applicable, no chmod |
| Diverges on | mode | ownership |

Because they live in two functions, fixing one does not fix the other. They are good candidates for a single helper that creates intermediate directories with buildkit-matching mode and ownership, replacing both half-implementations.

### mz863: COPY --chmod not applied to implicit parent directories

```dockerfile
FROM debian:12.10
COPY --chmod=0600 ctx1 /dest/ctx1
```

`/dest` is created implicitly. docker gives it mode `0600` from `--chmod`, kaniko gives it `0755`. `createParentDirectory` creates each missing parent with a fixed `os.Mkdir(dir, 0o755)` and never applies the chmod. `FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS` handles this only for the directory-copy path, not the single-file path.

### mz864: WORKDIR leaves implicit parents owned by root

```dockerfile
FROM alpine
USER 1000
WORKDIR /work/dir4
```

`/work` and `/work/dir4` are created. docker owns both by uid `1000`, kaniko owns `/work/dir4` by `1000` but `/work` by root. `MkdirAllWithPermissions` calls `os.MkdirAll` then chowns only the final path, so intermediate parents stay root owned.

## Confirmed real, not yet filed

### dangling-symlink dest resolution failure

```dockerfile
FROM alpine
COPY linkS /dest/linkS
COPY linkS /dest/linkS
```

`linkS` is a context symlink to `ctx0`. kaniko copies it as a dangling symlink `/dest/linkS` to `/dest/ctx0`, which does not exist. A later COPY whose destination resolves through that symlink then fails:

```
error building stage: failed to execute command:
resolving dest symlink: failed to eval symlinks: lstat /dest/ctx0: no such file or directory
```

docker builds, because it dereferenced the symlink and there is no dangling link. This is a build-outcome divergence: kaniko refuses to build a Dockerfile docker builds. It is a more clear-cut bug than the expected divergences below and is a strong candidate to file. It is a direct consequence of the copy-symlink-dereference behavior.

## Expected divergences, not bugs

These are real and stable, but reflect either intended kaniko behavior or long-standing differences that are not obviously wrong. They are counted in the classifier baseline, not reported as findings.

### copy symlink dereference

`COPY` of a symlinked source: kaniko preserves the symlink, docker dereferences it and copies the target as a regular file, so the copied entry differs by `Linkname`. `diffArgsMap` in `images.go` already records this with the comment that docker is wrong and kaniko copies the symlink correctly.

### history for metadata instructions

kaniko does not record a history entry for instructions that create no layer, such as `ENV` and `LABEL`, while docker records one empty-layer entry each. kaniko also formats the `created_by` of a RUN differently from buildkit. So the history arrays differ in length and content.

### no-op RUN unchanged dir layer

A RUN that makes no net filesystem change on an existing directory produces an empty layer under docker, but kaniko includes the unchanged directory in the layer, so layer contents and count diverge. This is the mz595 layer-length-mismatch class.

## Dismissed: OCI vs docker media type

The fuzzer first reported a media-type divergence on every case. It was a harness artifact. kaniko emits whatever media type the base image carries, docker emits OCI by default and docker v2 when provenance is disabled. The harness now detects the final base image's media type with `crane` and tells docker to emit the same, so this class no longer appears. It was removed from the classifier rather than kept, because it describes no real kaniko behavior.

## Method note

Findings came from `TestFuzz` with minimal `diffoci` ignores, so the two oracles compared as strictly as reasonably possible. The docker oracle excuses only image name, image-config timestamps, and cross-tool file timestamps. The cache oracle excuses only image name and image-config timestamps and is otherwise byte-strict. Every candidate was reproduced in isolation and confirmed by reading the config, the layer tars, or the kaniko source before being classified as real, expected, or artifact. That is how the media-type artifact was caught before it could be recorded as a bug, and it is why the classifier baseline is trusted: a case that is not one of the known classes is a genuinely new divergence.

Two campaigns produced these results. One reached about 700 cases before it exhausted local disk, and every one of its 140 findings was one of the two symlink classes above. A later one-hour campaign of 441 cases, run after adding per-case image cleanup and classifiers for the known classes, reported only the WORKDIR class (mz864) against a quiet baseline, which is the intended signal-to-noise.
