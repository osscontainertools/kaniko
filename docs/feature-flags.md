# Feature Flags

## Profiles

### Preview

Opting into the Preview profile gives you early access to upcoming performance improvements, bugfixes and features. While these flags are tested and ready to use, implementation details may still change.

```sh
FF_KANIKO_ADD_CHOWN_TAR=true
FF_KANIKO_ADD_FOLLOW_SYMLINK=true
FF_KANIKO_ALIGN_HISTORY=true
FF_KANIKO_CACHE_ADD_SOURCES=true
FF_KANIKO_CACHE_COPY_FROM=true
FF_KANIKO_CACHE_IMAGE_INDEX=true
FF_KANIKO_COPY_KEEP_DEST_OWNER=true
FF_KANIKO_COPY_LINK=true
FF_KANIKO_COPY_SUBDIR_MODE=true
FF_KANIKO_DEFER_CACHE_PUSH=true
FF_KANIKO_FORMAT_INDEPENDENT_CACHE_KEY=true
FF_KANIKO_FROM_PLATFORM=true
FF_KANIKO_FROM_PREDEFINED_ARGS=true
FF_KANIKO_GLOB_CHAR_PATHS=true
FF_KANIKO_ISOLATE_CACHE_PUSH=true
FF_KANIKO_LOCAL_UNPACK_REQUIRES_TAR=true
FF_KANIKO_NATIVE_INIT=true
FF_KANIKO_PARALLEL_GUNZIP=true
FF_KANIKO_PARALLEL_HASH=true
FF_KANIKO_PROBE_CREDENTIALS=true
FF_KANIKO_REPRODUCIBLE_SKIP_BASE_REWRITE=true
FF_KANIKO_RESUME_BLOB_DOWNLOADS=true
FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT=true
FF_KANIKO_RUN_HEREDOC_SCRIPT=true
FF_KANIKO_RUN_MOUNT_BIND_RELATIVE=true
FF_KANIKO_RUN_MOUNT_BIND_RESOLVE_SYMLINK=true
FF_KANIKO_RUN_MOUNT_TMPFS=true
FF_KANIKO_SKIP_UNSET_BUILD_ARGS=true
FF_KANIKO_SNAPSHOT_TRACK_CACHED_LAYERS=true
FF_KANIKO_WARMER_BUILD_ARGS=true
FF_KANIKO_WARMER_EXECUTOR_PARITY=true
FF_KANIKO_WARMER_FAIL_ON_ERROR=true
FF_KANIKO_WARMER_PREDEFINED_ARGS=true
```

### BuildKit compatibility

In a few places, Kaniko keeps its historical, non-compliant behaviour instead of following the Dockerfile specification. Switching to the compliant implementation by default would require users with large codebases to update their Dockerfiles, making migration to our fork tedious. These flags enable the spec-compliant behaviour, matching BuildKit one-to-one. Our integration tests run this profile on top of Preview.

```sh
FF_KANIKO_CHOWN_ON_IMPLICIT_DIRS=true
FF_KANIKO_COPY_AS_ROOT=true
FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS=true
FF_KANIKO_COPY_LINK=true
FF_KANIKO_UNPACK_XZ=true
```

## Flag `FF_KANIKO_ADD_CHECKSUM`

`ADD --checksum=sha256:<hex> <url> <dest>` states that the download has to hash to the given digest. With this flag off kaniko parses the flag and never checks it, so a substituted download is added to the image and the build succeeds.
Set this flag to `true` to verify the download and fail the build on a mismatch.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_ADD_CHOWN_TAR`

`ADD --chown` is not applied to an extracted archive, the files keep the owners recorded in the tar.
Set this flag to `true` to apply `--chown` to the extracted files.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_ADD_FOLLOW_SYMLINK`

`ADD <url> /dir/` fails with `destination cannot be a symlink` when `/dir` is a symlink to a directory.
Set this flag to `true` to write the download through the symlink. Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_ADD_UNPACK`

`ADD --unpack=<bool>` overrides whether a tar archive is extracted. A local archive is extracted and a URL download is not, so `--unpack=false` adds a local archive as a file and `--unpack=true` extracts a download. A source that turns out not to be an archive is added as a file either way. With this flag off kaniko parses the modifier and ignores it, so both sources keep their default treatment.
Set this flag to `true` to honor `--unpack`.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_ALIGN_HISTORY`

On a base image whose history does not match its layers, the image history shows kaniko's build steps against the wrong layers.
Set this flag to `true` to show each build step against its own layer.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_CACHE_ADD_SOURCES`

`ADD --checksum=sha256:<hex> <url>` always downloads the URL during the build, even with a warmed `--cache-dir`.
Set this flag to `true` on the warmer and the executor to cache these sources in `--cache-dir`.
The executor reads them only with `--cache=true`.
The warmer also needs `FF_KANIKO_WARMER_EXECUTOR_PARITY`.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_CACHE_COPY_FROM`

`COPY --from=<image>` always pulls the image from the registry, even when it is in `--cache-dir`.
Set this flag to `true` on the warmer and the executor to cache it like a `FROM` base image.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_CACHE_HASH_BLAKE3`

`COPY` and `ADD` inputs are folded into the layer cache key with md5, which is not collision resistant.
Set this flag to `true` to fold them with [BLAKE3](https://github.com/BLAKE3-team/BLAKE3) instead.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_CACHE_IMAGE_INDEX`

The warmer does not cache image indexes. A base image pinned to an index digest still needs the registry, so air-gapped builds fail even with a warm cache.
Set this flag to `true` on the warmer and the executor to cache the index too.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_CACHE_LOOKAHEAD`

Set this flag to `true` to run a precompute pass before the build loop that derives each stage's final cache key ahead of time. The build loop still recomputes each key during its own `optimize()` call and asserts that it matches the precomputed value.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_CACHE_PROBE_AFTER_MISS`

By default, a single layer cache miss within a stage disables every subsequent cache lookup in that same stage. A 30-step Dockerfile with one transient miss at step 3 will rebuild the remaining 27 layers locally even when later layers are still cached in the registry.

Set this flag to `true` to keep probing the cache after a miss. Cached tar diffs apply cleanly on top of locally-rebuilt prior layers under the same determinism assumption the cache scheme already requires (cache keys encode commands and inputs, not prior-layer outputs).

The other `stopCache` site — `COPY --from` in the precompute pass — is intentionally not affected by this flag. That one signals "key cannot be computed without the file context", not a transient miss, and is required for correctness.
Defaults to `false`.
Currently no plans to activate.

## Flag `FF_KANIKO_CHOWN_ON_IMPLICIT_DIRS`

When `WORKDIR` creates a directory whose parents do not exist yet, kaniko creates the parents but leaves them owned by root, assigning the active `USER` only to the final directory. buildkit assigns the active `USER` to every directory it creates, so `WORKDIR /work/dir` under `USER 1000` leaves `/work` owned by root in kaniko but by `1000` in buildkit.
Set this flag to `true` to chown every implicitly created directory to the active user, matching buildkit. Defaults to `false`.
Currently no plans to activate.

## Flag `FF_KANIKO_CONFINE_COPY_SOURCE`

A `COPY` or `ADD` source that points into the kaniko directory, for example `COPY ../kaniko/.docker/config.json /` or `COPY --from=<stage> /kaniko/executor /`, copies kaniko's own files into the image.
Set this flag to `true` to fail the build instead.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_CONFINE_HARDLINK_TARGET`

A hardlink in a base image layer or an `ADD`ed tar whose target passes through a symlink into the kaniko directory, for example `escape -> /kaniko` followed by a hardlink to `escape/executor`, links kaniko's own files into the image.
Set this flag to `true` to skip such a hardlink with a warning instead.
Defaults to `true`.
Will be deprecated in `v1.31.0`.

## Flag `FF_KANIKO_COPY_AS_ROOT`

When files are copied from context, kaniko will copy them as the current user. But according to [dockerfile specification](https://docs.docker.com/reference/dockerfile/#copy---chown---chmod) they should always be copied as `root:root` unless specified otherwise.
Set this flag to `true` to implement COPY/ADD as specified. Defaults to `false`.
Currently no plans to activate.

## Flag `FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS`

When files are copied into a non-existing directory, both kaniko and buildkit will create the directory and all required parent directories implicitly. If chmod option is given, buildkit will apply the chmod not only on the copied files & folders, but on all implicit parent dirs too. Kaniko will use regular folder permissions (0755) on parent directories instead and only apply the chmod on the explicitly created files & folders.
Set this flag to `true` to implement COPY chmod like buildkit. Defaults to `false`.
Currently no plans to activate.

## Flag `FF_KANIKO_COPY_KEEP_DEST_OWNER`

`COPY --chown` of a directory also changes the owner of an existing destination directory.
Set this flag to `true` to keep its owner. Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_COPY_LINK`

```dockerfile
COPY --link ./dist /app
```

Kaniko parses `--link` and drops it, building the layer as if it were not there. buildkit caches the layer on its own, so a base image bump or an edit above it does not rebuild it, and writes to the destination as written rather than through a symlink the layers below put there.
The layer is owned by root regardless of `USER`, and `--chown` takes numeric ids only.
Set this flag to `true` to honour `--link` on `COPY` and `ADD`.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_COPY_SKIP_SPECIAL_FILES`

`COPY` reads each source file to copy it, which is wrong for anything that is not a regular file. A socket fails the build with `ENXIO`, and a block or character device is read as if it were a file, baking its contents into the image in place of the device node.
Set this flag to `true` to skip both with a warning instead. Fifos are always recreated with `mkfifo` regardless of this flag, since opening one hung the build outright. Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_COPY_SUBDIR_MODE`

When a directory COPY lands on a subdirectory that already exists, kaniko keeps its old mode. Set this flag to `true` to give it the mode of the source subdirectory. Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_CROSS_REPO_MOUNT`

A registry can copy a blob between its own repositories for free, but only if it is told which repository already holds it. Kaniko loses that as soon as it copies a layer locally. Worse, with `--cache` every built layer goes up twice, once to the cache repo and once inside the image. Set this flag to `true` to remember which layers can be mounted remotely and which ones genuinely need to be pushed.
If a cross-repository mount cannot be authorized or completed,
kaniko falls back to the normal blob upload path using destination credential.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_DEFER_CACHE_PUSH`

Every stage waits for its cache layers to finish uploading before the next stage starts. Set this flag to `true` to keep uploading cache layers in the background while the build continues. They are awaited before the image is pushed.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_DEPRECATE_LAYERLESS_CACHE_ENTRIES`

Before `v1.25.0` a command that changed no files, `WORKDIR /` for example, was cached as an entry without a layer. kaniko no longer writes such entries, but on a hit it still accepts one and logs `No files were changed, appending empty layer to config`. Set this flag to `true` to reject them.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_DISABLE_HTTP2`

We noticed that there is a significant performance gap when using http/2.0 together with gitlab registry. Set this flag to `true` to enforce http/1.1 protocol, the same behaviour as if setting `GODEBUG="http2client=0"`.
Defaults to `false`.
Currently no plans to activate.

## Flag `FF_KANIKO_EXPAND_HEREDOC`

Docker applies Dockerfile word-expansion to a `COPY` or `ADD` heredoc body when the delimiter is unquoted, so `${VAR}` expands and `\${VAR}` keeps the literal text. A quoted delimiter (`<<'EOF'`) leaves the body verbatim. kaniko writes the body verbatim in every case, so the expanded files diverge from Docker.
Set this flag to `true` to expand build args and env in unquoted `COPY` and `ADD` heredoc bodies.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_FORMAT_INDEPENDENT_CACHE_KEY`

Cache keys depend on the manifest format of the base image, so switching `--image-format` rebuilds every layer even though the cached layers are the same.
Set this flag to `true` to compute cache keys independently of the manifest format.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_FROM_PLATFORM`

Supports `--platform` in `FROM` instructions. The stage builds on the base image for that platform instead of the one for `--custom-platform`, for example `FROM --platform=$BUILDPLATFORM golang AS build`.
Set this flag to `true` on the warmer and the executor to enable `FROM --platform`.
The warmer also needs `FF_KANIKO_WARMER_EXECUTOR_PARITY`.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_FROM_PREDEFINED_ARGS`

The predefined platform args such as `TARGETARCH` resolve to empty in `FROM` and in global `ARG` defaults.
Set this flag to `true` to resolve them from `--customPlatform` and the host.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_GLOB_CHAR_PATHS`

A directory whose name contains glob characters such as `[dir]` copies empty when it is the source of `COPY`, or the source of a `RUN --mount=type=bind`.
Set this flag to `true` to copy its contents.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_HASH_DIR_FRAMING`

Directory cache keys concatenate each relative path and file hash without recording their boundaries. A filename can therefore absorb an adjacent hash and make distinct directory trees produce the same cache key, silently reusing the wrong cached layer.
Set this flag to `true` to length-prefix every path and file hash before hashing the directory. Toggling it changes cache keys for `COPY` and `ADD` directory inputs, forcing those layers to rebuild once.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_IMAGE_STAGES`

A stage whose commands are all cache hits is already an image. Kaniko still unpacks every one of its cached layers onto the root filesystem and deletes them again when the stage ends, although nothing reads them.
Set this flag to `true` to download the image and use it as is, skipping the unpack to the root filesystem.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY`

When a multi-stage build uses `COPY --from=<stage>`, kaniko normally hashes the copied files from the source stage's filesystem to compute the downstream cache key. The source stage's `finalCacheKey` is a deterministic function of its build inputs and can be used as a stable proxy for those file contents, so the downstream cache key can be inferred without accessing the filesystem at all. This is a preparatory optimisation for a future change that will avoid unpacking the source stage's filesystem entirely when all downstream stages are also fully cached.
Set this flag to `true` to add additional cache entries for the shortcuts, currently they do not yet allow optimization.
Requires `--cache-copy-layers`. Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_ISOLATE_CACHE_PUSH`

With `--cache`, kaniko writes the build's output files for every pushed cache layer, not only for the final image.
Set this flag to `true` to write them for the final image only.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_LAYER_HINTS`

Set this flag to `true` to log [layer hints](../README.md#layer-hints).
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_LOCAL_UNPACK_REQUIRES_TAR`

`ADD` takes any local compressed file for a tar archive, so a gzip file whose content is not a tar fails with `unpacking local tar: unexpected EOF`.
Set this flag to `true` to add such a file as it is.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_NATIVE_COPY`

Three copy paths use the `github.com/otiai10/copy` library instead of kaniko's own copy: persisting a stage's files so `COPY --from=<stage>` can read them, the cross-device fallback when a bind mount target is moved aside, and the `RUN --mount=type=bind` source copy. The library copies every file on its own and knows nothing about hardlinks or file capabilities, so all three expand hardlinks into independent copies. Capabilities fare a little better: the cross-stage save reapplies them, but only to the path named in the `COPY`, so a file carrying capabilities inside a copied directory still loses them, and the two mount paths do not reapply them at all. For the cross-stage save all of this happens before the dependent stage ever sees the files, which is why hardlink-preserving extraction alone cannot keep the hardlinks.
Set this flag to `true` to use kaniko's own copy for all three, walking each tree once and linking repeated inodes instead of duplicating them. Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_NATIVE_INIT`

Kaniko usually runs as PID 1 but does not reap zombie processes or forward signals to `RUN` commands.
Set this flag to `true` to have kaniko handle both itself, without `/kaniko/tini`.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_OCI_SCRATCH_BASE`

When a Dockerfile uses `FROM scratch`, kaniko uses an empty Docker-format image as the build base, which means the output image is produced in Docker manifest schema v2 format.
Set this flag to `true` to use an empty OCI-format image instead, causing `FROM scratch` builds to produce output in OCI manifest schema v1 format. Defaults to `false`.
Currently no plans to activate.

## Flag `FF_KANIKO_PARALLEL_GUNZIP`

kaniko decompresses gzip base layers on the same thread that writes their files to disk.
Set this flag to `true` to decompress them ahead of extraction on a separate thread.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_PARALLEL_HASH`

After each `RUN`, kaniko hashes the files of the whole filesystem one at a time, which is slow for large images.
Set this flag to `true` to hash them in parallel. [`--snapshot-hash-workers`](cli.md#flag---snapshot-hash-workers) sets how many at once.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_PATH_SCOPED_REGISTRY_AUTH`

An `auths` entry is historically matched by either the registry host or by the exact repository path. kaniko and most other tools have no concept of namespaces for docker auth. This means in registries that allow for namespaced auth, ie. quay robot accounts, users will end up configuring credentials for each repository path regardless.

```text
registry:   registry.example.com
namespace:  registry.example.com/org-a
repository: registry.example.com/org-a/project-1
repository: registry.example.com/org-a/project-2
repository: registry.example.com/org-a/project-3
```

We introduced a new PathScopedKeychain resolver that implements namespace aware authentication, so users would only need to configure a single credential for the entire `registry.example.com/org-a`. This affects explicit `auths` entries only, `DOCKER_AUTH_CONFIG`. `credsStore` and `credHelpers` keep the scopes they have without the flag. See **[docs/registries.md](registries.md)** for further details and the full lookup order. Set this flag to `true` to switch to the new namespace aware PathScopedKeychain resolver and match namespace entries.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

> [!IMPORTANT]
> The new resolver also stops offering an entry to repositories it does not cover. The legacy resolver sent an entry for `registry.example.com/org-a` to every repository on that host, ie. `registry.example.com/org-b` too. This was intended to support URL-style entries that docker login used to write, but it was scoped too broadly and included entries without a scheme. We continue to support URL-style entries, what breaks is implicit auth to sibling namespaces. A build that relies on that behaviour goes anonymous and fails with a `401`. Set `FF_KANIKO_PATH_SCOPED_REGISTRY_AUTH=false` to restore the legacy resolver until `v1.30.0`.

## Flag `FF_KANIKO_PEEK_ARCHIVE_HEADER`

To detect whether an `ADD` source is a compressed archive, kaniko reads the entire file into memory, at least twice per `ADD`. A multi-GB archive can push the build past its memory limit.
Set this flag to `true` to read only the first 8 bytes.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_PLATFORM_CACHE_KEY`

Multi-arch builds would write to the same cache causing false positive cache hits.
Set this flag to `true` to add the target platform to the cache key.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_POOL_REGISTRY_CONNECTIONS`

With this flag off a build opens a new connection for every registry operation and repeats the token exchange each time.
Set this flag to `true` to share one connection pool per registry.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_PRECOMPILE_DOCKERIGNORE`

`.dockerignore` rebuilds the pattern matcher and recompiles every glob to a regexp on every file. Set this flag to `true` to pre-compile the matcher instead.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_PRESERVE_MOUNTED_SYMLINKS`

Ubuntu ships `/lib` as a symlink to `/usr/lib`. A bind-mount into `/lib`, as the NVIDIA GPU operator does on GPU nodes, forces it to stay a real directory, so kaniko drops the symlink and nothing reaches the base image files through `/lib` any more. Every `RUN` then fails with `fork/exec /bin/sh: no such file or directory`.
Set this flag to `true` to keep both paths resolving to the same files. Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_PROBE_CREDENTIALS`

kaniko uses the first credential source that has an entry for a repository, even when the registry rejects it. A `DOCKER_AUTH_CONFIG` that can only pull then fails the push, and a Google metadata-server token without registry access fails the pull of a public `gcr.io` image.
Set this flag to `true` to use the first credential source the registry accepts for each repository, or no credential for a pull.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS`

`--reproducible` re-tars every layer to zero its timestamps, including layers inherited from the `FROM` image. Base-layer blobs get fresh digests on every build and stop matching the upstream registry, defeating layer reuse even though kaniko changed nothing in them.
Set this flag to `true` to re-time only kaniko-appended layers and pass base layers through unchanged.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_REPRODUCIBLE_PRESERVE_FORMAT`

`--reproducible` internally uses go-containerregistry's `mutate.Canonical`. It re-writes every image to dockerv2+gzip, no matter the base image or the flags you passed.
Set this flag to `true` to keep the compression and the image format the build was asked for.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_REPRODUCIBLE_SKIP_BASE_REWRITE`

`--reproducible` reads the whole base image a second time and holds each base layer in memory, which is slow and can run out of memory on large bases.
Set this flag to `true` to only process the layers kaniko added. The built image is the same.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_RESOLVE_CACHE_KEY`

A `COPY`, `ADD` or `WORKDIR` layer cache key is built from the raw instruction text, so build args and env expanded in the instruction (for example `COPY foo /$A/foo` or `WORKDIR /$A/foo`) do not enter the key. A build that only changes such a variable can hit stale cache entries.
Set this flag to `true` to interpolate build args and env.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_RESUME_BLOB_DOWNLOADS`

A connection that drops while a layer downloads fails the build with `unexpected EOF`, and `--image-fs-extract-retry` downloads every layer again from the first byte.
Set this flag to `true` to request the rest of the blob with a `Range` header and continue where the download stopped.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_ROLLING_CACHE_KEY`

By default the composite cache joins all inputs with `-` and hashes the result. Since `-` is a legal input too, different sequences can join to the same text and collide, silently picking up the wrong cache layer.
This flag switches to a rolling hash, think git, where `state = SHA256(state || input)`. This makes boundaries unambiguous and prevents collisions. It also prevents the hash input from growing, so key computation gets marginally cheaper. Toggling it changes every cache key, forcing a rebuild from scratch.
Set this flag to `true` to enable the rolling cache key.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT`

Kaniko skips `RUN --mount=type=bind` flags with `from=<stage>` or `from=<image>`.
Set this flag to `true` to mount files from another stage or image. Writes to the mount are discarded.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_RUN_HEREDOC_SCRIPT`

```dockerfile
RUN <<EOF
echo $(echo "a (b)")
EOF
```

Kaniko was running the body of a lone `RUN <<EOF` through the shell twice. This caused variables to get expanded before the script runs and the example above to fail.
Set this flag to `true` to run the body as the script itself.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_RUN_HONOR_GROUP`

When a stage sets `USER user:group`, `RUN` applies only the user and the gid falls back to the user's primary group, so an explicit group is silently dropped.
Set this flag to `true` to pass the full `user:group` to the command so `RUN` runs with the requested group, matching docker.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_RUN_MOUNT_BIND_RELATIVE`

A relative `RUN --mount=type=bind` target resolves against `/`, whatever the `WORKDIR` is.
Set this flag to `true` to resolve it against `WORKDIR`. Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_RUN_MOUNT_BIND_RESOLVE_SYMLINK`

A `RUN --mount=type=bind` source that is a symlink is mounted as the link itself.
Set this flag to `true` to mount the file it points to. Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_RUN_MOUNT_TMPFS`

Supports `--mount=type=tmpfs` flags in `RUN` instructions. The target is an empty directory for the duration of the command, and its contents are discarded afterwards.
Set this flag to `true` to enable tmpfs mounts in `RUN` statements.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_RUN_VIA_TINI`

Kaniko usually runs as PID1 in the container, but kaniko currently does not implement reaping of zombie processes, nor does it offload that task to the kernel. As a result, any short-lived child processes spawned by your `RUN` command may linger around as zombies and potentially cause your build to hang.
Set this flag to `true` to run any `RUN` commands via `tini` init system as subreaper, to properly handle zombie processes.
Note that for this feature to work the tini binary must be available as `/kaniko/tini`.
Defaults to `false`.
Currently no plans to activate.
Superseded by [`FF_KANIKO_NATIVE_INIT`](#flag-ff_kaniko_native_init).
Will be deprecated together with `/kaniko/tini` in `v1.31.0`.

## Flag `FF_KANIKO_SCOPED_DOCKERIGNORE`

Scopes `.dockerignore` patterns to the build context. Without it, an absolute path outside the context (such as an intermediate `/kaniko/...` source of a multi-stage `COPY --from`) is matched against the patterns; with an allowlist `.dockerignore` (e.g. `*` then `!keep`) the catch-all wrongly excludes it, dropping it from the layer cache key and serving a stale image when only that file changed. Set this flag to `true` to never match paths outside the context.
Defaults to `false`.
Currently no plans to activate.

## Flag `FF_KANIKO_SCOPED_REGISTRY_CERTIFICATES`

A CA passed with `--registry-certificate` is trusted for every registry that has one, for example `--registry-certificate a.example.com=/ca-a.pem --registry-certificate b.example.com=/ca-b.pem` accepts a certificate for `b.example.com` signed by `/ca-a.pem`.
Set this flag to `true` to trust each CA only for its own registry. A certificate file without a valid certificate then fails the build.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_SHARED_BASE_CACHE`

When several stages build on the same remote base image, kaniko downloads that base once per stage. Set this flag to `true` to download a shared base once, store it under `/kaniko/bases`, and have the other stages read it from there instead of downloading it again. A base read by a single stage is only stored when kaniko reads it again, ie. for `--reproducible`, `--tar-path`, `--oci-layout-path` or a push to a different registry.
Stored bases stay in `/kaniko/bases` after the build, `--cleanup` does not remove them. A long-lived executor that runs many builds therefore accumulates every base it ever downloaded.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_SKIP_CACHED_STAGES`

When a multi-stage build uses `COPY --from=<stage>`, the downstream cache key depends on the copied files. So the entire source stage has to be built and unpacked, only to then realize that we had a cache hit and can throw away the upstream stage. We recently introduced `FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY`, `FF_KANIKO_CACHE_LOOKAHEAD` and `FF_KANIKO_ROLLING_CACHE_KEY`, with that we can know a-priori whether we will have a cache hit or not. `FF_KANIKO_SKIP_CACHED_STAGES` is the logical conclusion then, it simply runs another elision pass over the now updated list of stages and drops all stages that are no longer required to be built. Where a key cannot be inferred the stage is built as before. A fully cached build collapses into a single stage with nothing to unpack.
Set this flag to `true` to run the second elision pass.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_SKIP_RELABEL_RECOMPRESS`

When a cached layer is reused in an image of a different media-type vendor, kaniko not only relabels the layer but re-gzips it too. if the compression is unchanged, the re-encoded blob is byte-identical to the original.
Set this flag to `true` to skip the unecessary re-compression and serve the relabeled already-compressed blob.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_SKIP_UNSET_BUILD_ARGS`

Passing `--build-arg NAME` without a value looks up `NAME` in the environment, and turns it into `NAME=` when `NAME` is unset, overriding any `ARG NAME=default` in the Dockerfile with an empty string.
Set this flag to `true` to skip unset build args so the `ARG` default applies, matching Docker and Podman.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_SKIP_WRITE_WHITEOUTS`

When kaniko extracts a cached layer it applies the layer's whiteouts by deleting the target files, but it also writes the `.wh.<name>` marker files onto the working filesystem. With `--cache-copy-layers` a later cross-stage `COPY --from=<stage>` copies such a marker verbatim and commits it as a real whiteout, so a cache-hit build deletes a file that the cache-miss build kept.
Set this flag to `true` to skip writing the marker files, the deletion alone already applies the whiteout. Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_SNAPSHOT_TRACK_CACHED_LAYERS`

With `--cache`, a `COPY` served from the cache after an uncached `RUN` has its files stored a second time in the next `RUN` layer.
Set this flag to `true` to store them once. Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_UNPACK_XZ`

`ADD` extracts a local tar archive, and docker recognises xz among the compression formats it accepts. kaniko detects xz but does not decompress it, so `ADD archive.tar.xz` fails with `unsupported compression algorithm`.
Set this flag to `true` to extract xz archives.
Defaults to `false`.
Currently no plans to activate.

## Flag `FF_KANIKO_UNPACK_ZSTD`

`ADD` extracts a local tar archive, and docker recognises zstd among the compression formats it accepts. kaniko detects zstd but does not decompress it, so `ADD archive.tar.zst` fails with `unsupported compression algorithm`.
Set this flag to `true` to extract zstd archives.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_UNTAR_SKIP_ROOT`

When `ADD` extracts a local tar archive into a directory, kaniko applies the archive's root `.` entry to the destination directory and overwrites its mode and ownership, while docker leaves the destination untouched.
Set this flag to `true` to skip the root `.` entry when untarring.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_VERIFY_LAYER_DIGEST`

Layer extraction stops at the tar end-of-archive marker and never reads the blob to its end, so a registry or mirror serving different content under a layer digest goes unnoticed.
Set this flag to `true` to read every layer to its end and fail the build on a digest mismatch.
Defaults to `true`.
Will be deprecated in `v1.30.0`.

## Flag `FF_KANIKO_WARMER_BUILD_ARGS`

In warmer an `ARG` default wins over an explicit `--build-arg`, and `--build-arg BASE` without a value is not read from the environment.
Set this flag to `true` to resolve `--build-arg` like the executor does.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_WARMER_EXECUTOR_PARITY`

The warmer reads the Dockerfile with its own rules. It misses `COPY --from` images that a base image's `ONBUILD` adds and the images of `RUN --mount=from=<image>`, and it resolves `FROM` args differently from the executor.
Set this flag to `true` to find the images of every stage the way the executor does.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_WARMER_FAIL_ON_ERROR`

The warmer only fails when every image fails to warm. A partial failure is logged as a warning, so the build that relies on the cache fails later, ie. in an air-gapped environment.
Set this flag to `true` to fail the warmer when any image fails to warm.
Defaults to `false`.
Becomes default in `v1.30.0`.

## Flag `FF_KANIKO_WARMER_PREDEFINED_ARGS`

In warmer the predefined platform args such as `TARGETARCH` resolve to empty in `FROM` and in global `ARG` defaults.
Set this flag to `true` to resolve them from `--customPlatform` and the host.
Defaults to `false`.
Becomes default in `v1.30.0`.
