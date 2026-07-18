## What's changed since Google's v1.24.0
### Security
* go stdlib v1.24.3: CVE-2025-0913 CVE-2025-4673 CVE-2025-4674 CVE-2025-22874 CVE-2025-47906 CVE-2025-47907 CVE-2025-47912 CVE-2025-58183 CVE-2025-58185 CVE-2025-58186 CVE-2025-58187 CVE-2025-58188 CVE-2025-58189 CVE-2025-61723 CVE-2025-61724 CVE-2025-61725 CVE-2025-61729 CVE-2025-61727 CVE-2025-61726 CVE-2025-61728 CVE-2025-61730 CVE-2025-68121 CVE-2026-27137 CVE-2026-25679 CVE-2026-27142 CVE-2026-27138 CVE-2026-27139 CVE-2026-32280 CVE-2026-33810 CVE-2026-32281 CVE-2026-32283 CVE-2026-32282 CVE-2026-32289 CVE-2026-32288 CVE-2026-33811 CVE-2026-33814 CVE-2026-39820 CVE-2026-39836 CVE-2026-42499 CVE-2026-39823 CVE-2026-39825 CVE-2026-39826 CVE-2026-42504 CVE-2026-27145 CVE-2026-42507 CVE-2026-39822 CVE-2026-42505
* containerd v1.7.27: GHSA-m6hq-p25p-ffr2 GHSA-pwhc-rpq9-4c8w
* containerd-v2 v2.1.1: GHSA-m6hq-p25p-ffr2 GHSA-pwhc-rpq9-4c8w
* selinux v1.12.0: GHSA-cgrx-mc8f-2prm
* remove binary artifacts: by @tlk in https://github.com/mzihlmann/kaniko/pull/54
* golang.org/x/crypto 0.44.0: CVE-2025-47914 CVE-2025-58181 CVE-2026-39827 CVE-2026-39828 CVE-2026-39829 CVE-2026-39830 CVE-2026-39831 CVE-2026-39832 CVE-2026-39833 CVE-2026-39834 CVE-2026-39835 CVE-2026-42508 CVE-2026-46595 CVE-2026-46597 CVE-2026-46598
* golang.org/x/net 0.40.0: CVE-2026-25680 CVE-2026-25681 CVE-2026-27136 CVE-2026-39821 CVE-2026-42502 CVE-2026-42506
* golang.org/x/text 0.25.0: CVE-2026-56852
* github.com/docker/cli v29.4.1: CVE-2025-15558
* github.com/go-git/go-billy/v5 v5.8.0: CVE-2026-44973 CVE-2026-44740
* github.com/go-git/go-git/v5 5.16.0: CVE-2026-25934 CVE-2026-34165 CVE-2026-33762 CVE-2026-41506 CVE-2026-45022 CVE-2026-45571 CVE-2026-45570 GHSA-w5pp-99ch-qj29
* go.opentelemetry.io/otel/sdk 1.39.0: CVE-2026-24051 CVE-2026-39883
* github.com/cloudflare/circl 1.6.1: CVE-2026-1229
* google.golang.org/grpc v1.79.1: CVE-2026-33186
* prevent hijacking via `ONBUILD COPY`: https://github.com/osscontainertools/kaniko/pull/587
* prevent hijacking via `COPY --from=<image>`: https://github.com/osscontainertools/kaniko/pull/586
* github.com/moby/buildkit 0.22.0: CVE-2026-33747 CVE-2026-33748
* github.com/go-jose/go-jose/v4 v4.1.3: CVE-2026-34986
* 🔗 `FF_KANIKO_SECUREJOIN_EXTRACTION=true` symlink-based path traversal during tar extraction prevented with SecureJoin: by @8none1 in https://github.com/osscontainertools/kaniko/pull/828
* generate integration tar fixtures on the fly instead of storing opaque binaries: https://github.com/osscontainertools/kaniko/pull/844

### Bugfixes
* cache extract fails on invalid symlinks: https://github.com/mzihlmann/kaniko/pull/3
* cache collision under rename: by @SJrX in https://github.com/mzihlmann/kaniko/pull/62
* skip-unused-stages fails on numeric references: https://github.com/mzihlmann/kaniko/pull/103
* skip-unused-stages fails on capitalized references: https://github.com/mzihlmann/kaniko/pull/104
* pass correct storage account URL to azure blob client: by @okhaliavka in https://github.com/mzihlmann/kaniko/pull/201
* AWS ECR immutable tag update error message: by @Sapr0 in https://github.com/mzihlmann/kaniko/pull/204
* prevent layer overwrites in image resulting in `BLOB_UNKNOWN` error: by @mafredri in https://github.com/mzihlmann/kaniko/pull/230
* Adjust the determination priority of runtime under the Kubernetes cluster with cgroupv2: by @lcgash in https://github.com/mzihlmann/kaniko/pull/235
* parse metaArgs in warmer: https://github.com/osscontainertools/kaniko/pull/256
* warmer tries to load stage references: https://github.com/osscontainertools/kaniko/pull/266
* `FF_KANIKO_IGNORE_CACHED_MANIFEST=false` ignore potentially invalid cached manifest files: by @luxurine in https://github.com/osscontainertools/kaniko/pull/267
* don't reuse interstage dependencies: https://github.com/osscontainertools/kaniko/pull/286
* image-index digests causes warmer cache misses: https://github.com/osscontainertools/kaniko/pull/321
* refs/pull is not a valid branchname: https://github.com/osscontainertools/kaniko/pull/509
* ARG values leak across sibling stages in multi-stage builds: https://github.com/osscontainertools/kaniko/pull/623
* malformed Dockerfile input now errors instead of crashing: https://github.com/osscontainertools/kaniko/pull/733
* malformed base-image config now errors instead of crashing: https://github.com/osscontainertools/kaniko/pull/742

### Standardization
* sticky bit gets lost on COPY: https://github.com/mzihlmann/kaniko/pull/45
* COPY with restrictive chmod makes directory inacessible: https://github.com/mzihlmann/kaniko/pull/80
* file permissions: https://github.com/mzihlmann/kaniko/pull/101
* Persist capabilities on COPY: https://github.com/mzihlmann/kaniko/pull/107
* `FF_KANIKO_COPY_AS_ROOT=false` COPY from context should always default to root:root: https://github.com/mzihlmann/kaniko/pull/145 https://github.com/mzihlmann/kaniko/pull/166
* COPY --from preserves mtime: https://github.com/mzihlmann/kaniko/pull/161
* snapshotting preserves atime: https://github.com/mzihlmann/kaniko/pull/178
* skip snapshotting rootdir: https://github.com/mzihlmann/kaniko/pull/183
* predefined build args: by @kit101 in https://github.com/mzihlmann/kaniko/pull/185 https://github.com/osscontainertools/kaniko/pull/277
* add heredoc `<<EOF` syntax support: https://github.com/mzihlmann/kaniko/pull/206 https://github.com/mzihlmann/kaniko/pull/213 https://github.com/mzihlmann/kaniko/pull/214 https://github.com/mzihlmann/kaniko/pull/215
* cache mounts: https://github.com/osscontainertools/kaniko/pull/245 https://github.com/osscontainertools/kaniko/pull/274 https://github.com/osscontainertools/kaniko/pull/284
* skip-unused-stages invalidates numeric references: https://github.com/osscontainertools/kaniko/pull/306
* cache mount option implements additional flags: https://github.com/osscontainertools/kaniko/pull/390
* secret mounts: https://github.com/osscontainertools/kaniko/pull/391 https://github.com/osscontainertools/kaniko/pull/409
* `FF_KANIKO_RUN_VIA_TINI=false` reap zombie processes: https://github.com/osscontainertools/kaniko/pull/211 https://github.com/osscontainertools/kaniko/pull/450
* Skip chown/chmod for paths in ignore list: by @mesaglio in https://github.com/osscontainertools/kaniko/pull/435
* resolve remote `ONBUILD` instructions: https://github.com/osscontainertools/kaniko/pull/354
* `FF_KANIKO_COPY_CHMOD_ON_IMPLICIT_DIRS=false` add buildkit compatibility mode: https://github.com/osscontainertools/kaniko/pull/510 https://github.com/osscontainertools/kaniko/pull/866
* activate dockerfile linter: https://github.com/osscontainertools/kaniko/pull/590
* `FF_KANIKO_NO_PROPAGATE_ANNOTATIONS=true` stop propagating base image annotations: https://github.com/osscontainertools/kaniko/pull/566 https://github.com/osscontainertools/kaniko/pull/605
* `FF_KANIKO_VOLUME_SKIP_MKDIR=true` skip implicit mkdir in `VOLUME`: https://github.com/osscontainertools/kaniko/pull/638
* `FF_KANIKO_PRESERVE_HARDLINKS=true` preserve hardlinks during `COPY --from`: https://github.com/osscontainertools/kaniko/pull/630
* `FF_KANIKO_BUILDKIT_ARG_ENV_PRECEDENCE=true` upstream ENV shadows local ARG: https://github.com/osscontainertools/kaniko/pull/624
* `FF_KANIKO_RUN_MOUNT_BIND=true` support for `RUN --mount=type=bind`: https://github.com/osscontainertools/kaniko/pull/615
* `FF_KANIKO_REPRODUCIBLE_PRESERVE_BASE_LAYERS=false` `--reproducible` leaves base-image layers untouched so they still match the registry: https://github.com/osscontainertools/kaniko/pull/732
* `FF_KANIKO_SCOPED_DOCKERIGNORE=false` scope `.dockerignore` patterns to the build context: by @vidbregar in https://github.com/osscontainertools/kaniko/pull/763
* `FF_KANIKO_SKIP_WRITE_WHITEOUTS=false` cross-stage `COPY --from` on a cache hit copies whiteout markers as real files and silently deletes targets in later builds: https://github.com/osscontainertools/kaniko/pull/796
* `FF_KANIKO_UNTAR_SKIP_ROOT=false` `ADD` with a tar archive overwrites the destination directory mode and ownership from the archive root entry, unlike Docker: https://github.com/osscontainertools/kaniko/pull/842
* `FF_KANIKO_RUN_HONOR_GROUP=false` honor an explicit group from `USER user:group` in `RUN`, matching Docker: https://github.com/osscontainertools/kaniko/pull/840
* `FF_KANIKO_EXPAND_HEREDOC=false` expand build args and env in unquoted `COPY` and `ADD` heredoc bodies, matching Docker: https://github.com/osscontainertools/kaniko/pull/821

### Caching
* sourceImage's CreatedAt timestamp should not be included in cache key: https://github.com/mzihlmann/kaniko/pull/1
* ignore labels on base image for cache: https://github.com/mzihlmann/kaniko/pull/2
* intermediate images should not be labelled: https://github.com/mzihlmann/kaniko/pull/4
* Fix caching for empty RUN: https://github.com/mzihlmann/kaniko/pull/19
* WORKDIR learned to cache its potential output layer: https://github.com/mzihlmann/kaniko/pull/22 https://github.com/mzihlmann/kaniko/pull/23
* ADD learned to cache its output layer: https://github.com/mzihlmann/kaniko/pull/24
* whiteout annotations to prevent cache misses through `--annotation`: https://github.com/mzihlmann/kaniko/pull/209
* `FF_KANIKO_CACHE_PROBE_AFTER_MISS=false` keep probing the cache after a layer miss: by @iahsanGill in https://github.com/osscontainertools/kaniko/pull/703
* `FF_KANIKO_WARMER_CACHE_LOCK=true` coordinate concurrent warmers on a shared cache volume: by @iahsanGill in https://github.com/osscontainertools/kaniko/pull/705 https://github.com/osscontainertools/kaniko/pull/706
* `FF_KANIKO_SKIP_RELABEL_RECOMPRESS=false` skip re-gzip when relabeling a cached layer to a different media type: https://github.com/osscontainertools/kaniko/pull/778
* `FF_KANIKO_RESOLVE_CACHE_KEY=false` `COPY`, `ADD`, and `WORKDIR` layer cache keys now reflect build args and env referenced in the instruction, including `COPY`, `ADD`, and `RUN` heredoc bodies: https://github.com/osscontainertools/kaniko/pull/792 https://github.com/osscontainertools/kaniko/pull/801 https://github.com/osscontainertools/kaniko/pull/837 https://github.com/osscontainertools/kaniko/pull/823 https://github.com/osscontainertools/kaniko/pull/825
* `FF_KANIKO_INFER_CROSS_STAGE_CACHE_KEY=false` infer the cross-stage `COPY --from` cache key instead of hashing the copied files, so it hits without unpacking the source stage: https://github.com/osscontainertools/kaniko/pull/618 https://github.com/osscontainertools/kaniko/pull/741 https://github.com/osscontainertools/kaniko/pull/767
* `FF_KANIKO_ROLLING_CACHE_KEY=false` fold composite cache-key parts into a rolling hash so distinct key sequences can no longer collide and serve the wrong layer: https://github.com/osscontainertools/kaniko/pull/875
* `FF_KANIKO_HASH_DIR_FRAMING=false` length-prefix directory paths and file hashes in the cache key so distinct directory trees can no longer collide: by @JSap0914 in https://github.com/osscontainertools/kaniko/pull/912

### Performance
* squash stages together, speeding up build: https://github.com/mzihlmann/kaniko/pull/141 https://github.com/osscontainertools/kaniko/pull/283
* use ocilayout instead of tarballs during stage transitions: https://github.com/mzihlmann/kaniko/pull/303
* recompute whether a stage must be saved: https://github.com/osscontainertools/kaniko/pull/335
* port digest optimization to warmer: https://github.com/osscontainertools/kaniko/pull/325
* `FF_KANIKO_DISABLE_HTTP2=false` stop forcing http/2.0: https://github.com/osscontainertools/kaniko/pull/340
* `FF_KANIKO_OCI_WARMER=true` ocilayout warmer: https://github.com/osscontainertools/kaniko/pull/307
* `FF_KANIKO_PRECOMPILE_DOCKERIGNORE=false` compile `.dockerignore` patterns once per build instead of once per file: https://github.com/osscontainertools/kaniko/pull/887

### Usability
* if target stage is unspecified we now implicitly target the last stage: https://github.com/mzihlmann/kaniko/pull/27
* kaniko learned `--preserve-context` to preserve the build-context across multi-stage builds: https://github.com/mzihlmann/kaniko/pull/28
* kaniko learned `--materialize` forcing the filesystem into a well-defined state after the build: https://github.com/mzihlmann/kaniko/pull/29
* bootstrap image: https://github.com/mzihlmann/kaniko/pull/59
* deprecate force-build-metadata: https://github.com/mzihlmann/kaniko/pull/99
* make skip-unused-stages the default: https://github.com/mzihlmann/kaniko/pull/100
* kaniko learned `--credential-helpers` to select credential helpers: https://github.com/mzihlmann/kaniko/pull/135
* 🔗 Annotation flag: by @markusthoemmes in https://github.com/mzihlmann/kaniko/pull/98
* relative OCILayoutPath: by @EladAviczer in https://github.com/mzihlmann/kaniko/pull/187
* new cli option `--pre-cleanup` to clean the filesystem prior to build, allowing customized kaniko images to work properly: https://github.com/mzihlmann/kaniko/pull/196
* add git depth option: https://github.com/mzihlmann/kaniko/pull/203
* add docs for azure chinacloud: https://github.com/mzihlmann/kaniko/pull/216
* riscv image: https://github.com/mzihlmann/kaniko/pull/220
* add env credential helper: https://github.com/mzihlmann/kaniko/pull/236 https://github.com/mzihlmann/kaniko/pull/249
* allow skip push cache: https://github.com/osscontainertools/kaniko/pull/268
* organize kaniko dir: https://github.com/osscontainertools/kaniko/pull/285
* fix harbor authentication: https://github.com/osscontainertools/kaniko/pull/369
* new subcommand `executor login` to authenticate with a registry: by @brandon1024 in https://github.com/osscontainertools/kaniko/pull/407
* `FF_KANIKO_CLEAN_KANIKO_DIR=true` cleanup kaniko workspace on failure too: https://github.com/osscontainertools/kaniko/pull/453 https://github.com/osscontainertools/kaniko/pull/532
* multitarget builds - part 1: https://github.com/osscontainertools/kaniko/pull/485
* `FF_KANIKO_OCI_SCRATCH_BASE=false` oci scratch base image: https://github.com/osscontainertools/kaniko/pull/612
* `kaniko-alpine` image (`martizih/kaniko:alpine`): https://github.com/osscontainertools/kaniko/pull/647 https://github.com/osscontainertools/kaniko/pull/659
* `executor push` subcommand pushes a pre-built tarball or OCI layout without a separate `crane` binary: https://github.com/osscontainertools/kaniko/pull/737
* `FF_KANIKO_PRESERVE_MOUNTED_PATHS=true` keep read-only bind mounts (e.g. NVIDIA GPU driver artifacts) in place during extraction: https://github.com/osscontainertools/kaniko/pull/754
* `FF_KANIKO_DEPRECATE_INTER_STAGE_RESTORE=true` deprecate the `--preserve-context` inter-stage restore: https://github.com/osscontainertools/kaniko/pull/710
* `COPY` and `ADD` `--chmod` now accepts symbolic notation (e.g. `go=u`, `u=rwX,go=rX`) in addition to octal: https://github.com/osscontainertools/kaniko/pull/800
* `--image-format=docker|oci` pins the output manifest media type instead of inheriting it from the base image: https://github.com/osscontainertools/kaniko/pull/850

### Shoutout & Thanks
* 🔗 cleanup jobs: by @cpanato in https://github.com/mzihlmann/kaniko/pull/55
* 🔗 update ENV syntax in Dockerfile: by @babs in https://github.com/mzihlmann/kaniko/pull/60
* 🔗 update docs: by @mosabua @cpanato in https://github.com/mzihlmann/kaniko/pull/136
* 🔗 group dependabot updates for go and github actions: by @cpanato in https://github.com/mzihlmann/kaniko/pull/162
* remove deprecated github.com/containerd/containerd/platforms: by @BobDu in https://github.com/osscontainertools/kaniko/pull/252
* move github.com/docker/docker/api -> github.com/moby/moby/api: by @BobDu in https://github.com/osscontainertools/kaniko/pull/258
* 🔗 fix code scanning alert 1: by @cpanato in https://github.com/osscontainertools/kaniko/pull/272
* update docs: by @6543 in https://github.com/osscontainertools/kaniko/pull/300
* cleanup unused release script: by @BobDu in https://github.com/osscontainertools/kaniko/pull/347
* publish images to ghcr: by @babs in https://github.com/osscontainertools/kaniko/pull/329 https://github.com/osscontainertools/kaniko/pull/353
* ci: rework, use GHCR as primary, separate dev builds from release: by @babs in https://github.com/osscontainertools/kaniko/pull/368 https://github.com/osscontainertools/kaniko/pull/371
* replace github.com/pkg/errors with stdlib errors: by @BobDu in https://github.com/osscontainertools/kaniko/pull/370
* chore(ci): run staticcheck: by @nejch in https://github.com/osscontainertools/kaniko/pull/445
* dockerfile: don't use +x for chmod: by @Bixilon in https://github.com/osscontainertools/kaniko/pull/458

for a more detailed view you can refer to our [Changelog](./CHANGELOG.md) or [release notes](https://github.com/osscontainertools/kaniko/releases)

🔗 indicates a change is in sync with chainguard's fork https://github.com/chainguard-dev/kaniko