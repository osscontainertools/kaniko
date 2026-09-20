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
| ADD url owner under USER | `USER u` then `ADD <url> /d/f`, COPY_AS_ROOT on | file owned by root | file owned by u | filed mz1112 |
| history for metadata instructions | `ENV`, `LABEL`, ... | one history row each | no row emitted | expected divergence |
| no-op RUN unchanged dir layer | a RUN with no net change on an existing dir | empty layer | layer holds the unchanged dir | expected divergence |
| copy symlink dereference | `COPY symlink /dest/` | dereferences to a file | preserves the symlink | expected, kaniko correct |
| removing a swapped symlink | `RUN rm -rf` a base symlink a mount pins | builds | build fails, Resource busy | filed mz1111 |
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

### 1038: base-image layer annotations propagate into derived images

```dockerfile
FROM ubuntu
RUN echo hi > /marker
```

Layers inherited from the base keep that base's descriptor annotations. `ubuntu`
ships `ci.umo.uncompressed_blob_size` on its layer descriptors, kaniko copies it
onto the derived image's layers, docker strips it. `FF_KANIKO_NO_PROPAGATE_ANNOTATIONS`
covers only manifest-level annotations: `WithoutAnnotations` nils
`manifest.annotations` and never touches `manifest.layers[].annotations`.

**The fuzzer cannot reach this.** Its bases are pinned: alpine is a docker v2
manifest, whose layer descriptors have no annotations field at all, and debian is
OCI but ships `annotations: null`. No generated Dockerfile can exercise
propagation when nothing in the input space carries an annotation to propagate.
The oracle was strict enough — layer descriptors are compared unless
`--single-snapshot` relaxes the layer count — so this is an input-space gap, not
a comparison gap.

The generator varies Dockerfile content and treats base images as fixed opaque
inputs, so every bug that depends on base-image *metadata* is out of reach the
same way: manifest annotations, config labels and env, foreign layers, empty
layers. Minting a base that carries the property, as the ONBUILD base already
does for image-config triggers, is the shape of the fix.

### mz1111: deleting a swapped symlink name hits the busy mount

```dockerfile
FROM localhost:5000/fuzz-symlink-base:latest
RUN rm -rf /opt/real/far
RUN echo done > /marker
```

Built with `-v lib.so:/opt/driver/far/lib.so:ro`, which is the symlink base plus one pinned path. The first campaign against that base ran 288 cases with up to four paths pinned at once and reported four findings, every one of them this shape, with no crash, cache or determinism diff beside it.

```
rm: can't remove '/opt/real/far/lib.so': Resource busy
rm: can't remove '/opt/real/far': Directory not empty
error building image: error building stage: failed to execute command: waiting for process to exit: exit status 1
```

The base ships `/opt/driver -> /opt/real` and `/opt/real/far -> /opt/elsewhere`. The runtime creates `/opt/driver/far` as a real directory before the base is extracted, so `/opt/driver` cannot become a symlink and the swap keeps the directory and makes `/opt/real` the name that resolves into it. `/opt/real/far` is then the pinned directory rather than a symlink, and the build's own `rm -rf` tries to unlink the mount. docker builds the same Dockerfile, because it has no such mount.

Three variants pin what causes it. With the flag off and the same mount the build succeeds, and the image matches docker apart from timestamps and history. With the flag on and no mount it succeeds. With the flag off and `rm -rf /opt/driver/far`, the name the runtime actually mounted, it fails the same way.

So this is not swap bookkeeping. Unlinking a bind mount needs privileges kaniko does not have, and the failure already existed for the mounted path itself. What the swap changes is which names reach it: the base image's symlink name now resolves into the pinned directory too, so a Dockerfile that deletes the symlink aborts where it used to build. The fuzzer counts it as a known build failure rather than reporting it, so the delete shape keeps being generated and a different failure under it still surfaces.

### mz1112: FF_KANIKO_COPY_AS_ROOT does not cover ADD from a URL

```dockerfile
FROM alpine
USER daemon
ADD --chmod=0600 http://127.0.0.1:8890/addfile /urlget3/addfile
```

docker leaves the downloaded file owned by uid 0, kaniko gives it uid 2, the active `USER`.

Owning brought-in files by the active `USER` is kaniko's default and a deliberate divergence, since the spec says uid and gid 0 and flipping the default would break most Dockerfiles. `FF_KANIKO_COPY_AS_ROOT` is the opt-in that aligns it, and the campaign runs with it on. The finding is that the flag does not reach this path, so one image carries two conventions at once.

| instruction, flag on, `USER daemon` | docker | kaniko |
|---|---|---|
| `COPY f /cp/f` | uid 0 | uid 0 |
| `ADD f /addl/f`, a local file | uid 0 | uid 0 |
| `ADD x.tar /extract/`, a local tar | tar's own uid | tar's own uid |
| `ADD <url> /urlget/f` | uid 0 | uid 2 |

`copy.go` reads `kConfig.FF.CopyAsRoot` and substitutes `0:0`. `add.go` calls `util.GetActiveUserGroup(config.User, a.cmd.Chown, ...)` and never consults the flag, so the uid and gid it hands to `DownloadFileToDest` are the active `USER`. A local file source lands on root only because `add.go` delegates it to the copy command, and a tar source keeps the uids in the archive.

The mode row on the implicit parent (`urlget3/` 0600 versus 0755) that shows up beside it is the separate mz922 class.

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
