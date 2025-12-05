## What's changed since Google's v1.24.0
### Security
* go stdlib v1.24.3: CVE-2025-0913 CVE-2025-4673 CVE-2025-4674 CVE-2025-22874 CVE-2025-47906 CVE-2025-47907 CVE-2025-47912 CVE-2025-58183 CVE-2025-58185 CVE-2025-58186 CVE-2025-58187 CVE-2025-58188 CVE-2025-58189 CVE-2025-61723 CVE-2025-61724 CVE-2025-61725 CVE-2025-61729 CVE-2025-61727
* containerd v1.7.27: GHSA-m6hq-p25p-ffr2 GHSA-pwhc-rpq9-4c8w
* containerd-v2 v2.1.1: GHSA-m6hq-p25p-ffr2 GHSA-pwhc-rpq9-4c8w
* selinux v1.12.0: GHSA-cgrx-mc8f-2prm
* remove binary artifacts: by @tlk in https://github.com/mzihlmann/kaniko/pull/54
* golang.org/x/crypto 0.44.0: CVE-2025-47914 CVE-2025-58181
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
* `FF_KANIKO_RUN_MOUNT_CACHE=true` cache mounts: https://github.com/osscontainertools/kaniko/pull/245 https://github.com/osscontainertools/kaniko/pull/274 https://github.com/osscontainertools/kaniko/pull/284
* skip-unused-stages invalidates numeric references: https://github.com/osscontainertools/kaniko/pull/306
* cache mount option implements additional flags: https://github.com/osscontainertools/kaniko/pull/390
* `FF_KANIKO_RUN_MOUNT_SECRET=false` secret mounts: https://github.com/osscontainertools/kaniko/pull/391 https://github.com/osscontainertools/kaniko/pull/409
### Caching
* sourceImage's CreatedAt timestamp should not be included in cache key: https://github.com/mzihlmann/kaniko/pull/1
* ignore labels on base image for cache: https://github.com/mzihlmann/kaniko/pull/2
* intermediate images should not be labelled: https://github.com/mzihlmann/kaniko/pull/4
* Fix caching for empty RUN: https://github.com/mzihlmann/kaniko/pull/19
* WORKDIR learned to cache its potential output layer: https://github.com/mzihlmann/kaniko/pull/22 https://github.com/mzihlmann/kaniko/pull/23
* ADD learned to cache its output layer: https://github.com/mzihlmann/kaniko/pull/24
* whiteout annotations to prevent cache misses through `--annotation`: https://github.com/mzihlmann/kaniko/pull/209
### Performance
* `FF_KANIKO_SQUASH_STAGES=true` squash stages together, speeding up build: https://github.com/mzihlmann/kaniko/pull/141 https://github.com/osscontainertools/kaniko/pull/283
* `FF_KANIKO_OCI_STAGES=false` use ocilayout instead of tarballs during stage transitions: https://github.com/mzihlmann/kaniko/pull/303
* recompute whether a stage must be saved: https://github.com/osscontainertools/kaniko/pull/335
* port digest optimization to warmer: https://github.com/osscontainertools/kaniko/pull/325
* `FF_KANIKO_DISABLE_HTTP2=false` stop forcing http/2.0: https://github.com/osscontainertools/kaniko/pull/340
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
* `FF_KANIKO_NEW_CACHE_LAYOUT=true` organize kaniko dir: https://github.com/osscontainertools/kaniko/pull/285
* fix harbor authentication: https://github.com/osscontainertools/kaniko/pull/369
* new subcommand `executor login` to authenticate with a registry: by @brandon1024 in https://github.com/osscontainertools/kaniko/pull/407
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

for a more detailed view you can refer to our [Changelog](./CHANGELOG.md) or [release notes](https://github.com/osscontainertools/kaniko/releases)

🔗 indicates a change is in sync with chainguard's fork https://github.com/chainguard-dev/kaniko