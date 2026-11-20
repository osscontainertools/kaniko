# CLI Flags

## Flag `--build-arg`

This flag allows you to pass in ARG values at build time, similarly to Docker.
You can set it multiple times for multiple arguments.

Note that passing values that contain spaces is not natively supported - you
need to ensure that the IFS is set to null before your executor command. You can
set this by adding `export IFS=''` before your executor call. See the following
example

```bash
export IFS=''
/kaniko/executor --build-arg "MY_VAR='value with spaces'" ...
```

## Flag `--cache`

Set this flag as `--cache=true` to opt into caching with kaniko.

## Flag `--cache-dir`

Set this flag to specify a local directory cache for base images. Defaults to
`/cache`.

_This flag must be used in conjunction with the `--cache=true` flag._

## Flag `--add-cache`

Cache remote `ADD` sources and use them for builds that share the local cache directory.
The warmer accepts `--add-cache` with `--dockerfile` and writes remote sources under
`<cache-dir>/add-cache/`. The executor accepts `--add-cache` only while `--cache=true`
and `--cache-dir` is set. It resolves an entry before downloading the URL, then falls back
to a normal download on a cache miss or cache error.

An `ADD --checksum=<digest>` source is keyed and verified by its digest. Sources without a
checksum are keyed by a normalized URL. Warmer `--require-add-checksum` rejects a remote
`ADD` source that omits `--checksum`; it is available on both the warmer and executor.

## Flag `--cache-repo`

Set this flag to specify a remote repository that will be used to store cached
layers.

If this flag is not provided, a cache repo will be inferred from the
`--destination` flag. If `--destination=gcr.io/kaniko-project/test`, then cached
layers will be stored in `gcr.io/kaniko-project/test/cache`.

Cannot be combined with `--cache-from` or `--cache-to`.

_This flag must be used in conjunction with the `--cache=true` flag._

## Flag `--cache-from`

`--cache-from=<repo>` reads cached layers from `<repo>`. Prefix a path with `oci:` to read from an OCI layout on disk instead.
Set it repeatedly to read from several caches. The first cache that holds a layer is used.

Enables caching. Without `--cache-to` no cached layers are pushed.
Cannot be combined with `--cache-repo`.

## Flag `--cache-to`

`--cache-to=<repo>` pushes cached layers to `<repo>`. Prefix a path with `oci:` to write an OCI layout on disk instead.
Set it repeatedly to push to several caches.

Enables caching. Cannot be combined with `--cache-repo` or `--no-push-cache`.

For example, a feature branch reads its own cache and falls back to the one of `main`, but only writes its own:

```bash
--cache-from=registry.example.com/app/cache/feature \
--cache-from=registry.example.com/app/cache/main \
--cache-to=registry.example.com/app/cache/feature
```

## Flag `--cache-copy-layers`

Set this flag to cache copy layers.

## Flag `--cache-run-layers`

Set this flag to cache run layers (default=true).

## Flag `--cache-ttl`

Cache timeout in hours. Defaults to two weeks.

## Flag `--pre-cleanup`

Set this flag to clean the filesystem before the build.
ie. in order to support custom built kaniko images.

Defaults to `false`. Can also be set via the `KANIKO_PRE_CLEANUP` environment variable.

## Flag `--cleanup`

Set this flag to clean the filesystem and kaniko's working directory at the end of the build.

Defaults to `false`. Can also be set via the `KANIKO_CLEANUP` environment variable.

## Flag `--compression`

Use this flag to select the compression algorithm `[gzip, zstd]`. Defaults to `gzip`.

## Flag `--compression-level`

Use this flag to select the compression level. When it is not set, layers compress at the fastest level of the selected codec.

For `--compression=gzip` the level runs from `-2` to `9`, see [compress/gzip](https://pkg.go.dev/compress/gzip#pkg-constants).

For `--compression=zstd` the level runs from `1` to `22` and maps onto four encoder presets, see [EncoderLevelFromZstd](https://pkg.go.dev/github.com/klauspost/compress/zstd#EncoderLevelFromZstd).

## Flag `--image-format`

Use this flag to select the output image media type `[docker, oci]`. `docker` writes a Docker schema2 manifest, `oci` writes an OCI image manifest. When unset, kaniko inherits the format of the base image. A base image that mixes OCI and docker media types is inherited as it is, set this flag to unify the output on one of them.

## Flag `--compressed-caching`

Set this to false in order to prevent tar compression for cached layers. This
will increase the runtime of the build, but decrease the memory usage especially
for large builds. Try to use `--compressed-caching=false` if your build fails
with an out of memory error. Defaults to true.

## Flag `--context-sub-path`

Set a sub path within the given `--context`.

Its particularly useful when your context is, for example, a git repository, and
you want to build one of its subfolders instead of the root folder.

## Flag `--credential-helpers`

Use these credential helpers automatically, select from (env, google, ecr, acr, gitlab). Set it repeatedly for multiple helpers, defaults to all, set it to empty string to deactivate.

## Flag `--custom-platform`

Allows to build with another default platform than the host, similarly to docker
build --platform xxx the value has to be on the form
`--custom-platform=linux/arm`, with acceptable values listed here:
[GOOS/GOARCH](https://gist.github.com/asukakenji/f15ba7e588ac42795f421b48b8aede63).

It's also possible specifying CPU variants adding it as a third parameter (like
`--custom-platform=linux/arm/v5`). Currently CPU variants are only known to be
used for the ARM architecture as listed here:
[GOARM](https://go.dev/wiki/GoArm#supported-architectures)

_The resulting images cannot provide any metadata about CPU variant due to a
limitation of the OCI-image specification._

_This is not virtualization and cannot help to build an architecture not
natively supported by the build host. This is used to build i386 on an amd64
Host for example, or arm32 on an arm64 host._

## Flag `--digest-file`

Set this flag to specify a file in the container. This file will receive the
digest of a built image. This can be used to automatically track the exact image
built by kaniko.

For example, setting the flag to `--digest-file=/dev/termination-log` will write
the digest to that file, which is picked up by Kubernetes automatically as the
`{{.state.terminated.message}}` of the container.

## Flag `--dockerfile`

Path to the dockerfile to be built. (default "Dockerfile")

## Flag `--dryrun`

Instead of building the docker image, just print a plan of what kaniko would do.

## Flag `--force`

Force building outside of a container

## Flag `--git`

Branch to clone if build context is a git repository (default
branch=,single-branch=false,depth=0,recurse-submodules=false,insecure-skip-tls=false)

## Flag `--image-name-with-digest-file`

Specify a file to save the image name w/ digest of the built image to.

## Flag `--image-name-tag-with-digest-file`

Specify a file to save the image name w/ image tag and digest of the built image
to.

## Flag `--insecure`

Set this flag if you want to push images to a plain HTTP registry. It is
supposed to be used for testing purposes only and should not be used in
production!

## Flag `--insecure-pull`

Set this flag if you want to pull images from a plain HTTP registry. It is
supposed to be used for testing purposes only and should not be used in
production!

## Flag `--insecure-registry`

You can set `--insecure-registry <registry-name>` to use plain HTTP requests
when accessing the specified registry. It is supposed to be used for testing
purposes only and should not be used in production! You can set it multiple
times for multiple registries.

## Flag `--kaniko-dir`

Set this flag as `--kaniko-dir /not-kaniko` to move the kaniko binaries to `/not-kaniko` before the build starts. This is helpful in [Bootstrapping Kaniko](../README.md#bootstrapping-kaniko).
Will be deprecated in `v1.28.0`. Use the env variable `KANIKO_DIR` instead.

## Flag `--label`

Set this flag as `--label key=value` to set some metadata to the final image.
This is equivalent as using the `LABEL` within the Dockerfile.

## Flag `--annotation`

Set this flag as `--annotation key=value` to set some metadata to the final image.
[Annotation levels](https://docs.docker.com/build/metadata/annotations/#specify-annotation-level)
are currently not supported and it's always the manifest that's
annotated.

## Flag `--log-format`

Set this flag as `--log-format=<text|color|json>` to set the log format.
Defaults to `color`.

## Flag `--log-timestamp`

Set this flag as `--log-timestamp=<true|false>` to add timestamps to
`<text|color>` log format. Defaults to `false`.

## Flag `--materialize`

Set this boolean flag to `true` if you want kaniko to ensure that the filesystem is in a well-defined state after the build finishes. If you have a 100% cache-hitrate kaniko can skip unpacking files to the filesystem as it is superfluous if the goal is to simply build an image and push it to registry, but this also means that state of the filesystem after the build depends on whether that optimization was possible or not. This option disables this optimization entirely making sure that the filesystem is always well-defined.

This is useful if you use kaniko not to build a docker image, but to initialize your environment.

Defaults to `false`

## Flag `--no-push`

Set this flag if you only want to build the image, without pushing to a
registry. This can also be defined through `KANIKO_NO_PUSH` environment
variable.

NOTE: this will still push cache layers to the repo, to disable pushing cache layers use `--no-push-cache`

## Flag `--no-push-cache`

Set this flag if you do not want to push cache layers to a
registry.  Can be used in addition to `--no-push` to push no layers to a registry.

Cannot be combined with `--cache-to`.

## Flag `--oci-layout-path`

Set this flag to specify a directory in the container where the OCI image layout
of a built image will be placed. This can be used to automatically track the
exact image built by kaniko.

For example, to surface the image digest built in a
[Tekton task](https://github.com/tektoncd/pipeline/blob/v0.6.0/docs/resources.md#surfacing-the-image-digest-built-in-a-task),
this flag should be set to match the image resource `outputImageDir`.

_Note: Depending on the built image, the media type of the image manifest might
be either `application/vnd.oci.image.manifest.v1+json` or
`application/vnd.docker.distribution.manifest.v2+json`._

## Flag `--preserve-context`

Set this boolean flag to `true` if you want kaniko to restore the build-context for multi-stage builds.
If set, kaniko will take a snapshot of the full filesystem before it starts building to later restore to that state. If combined with the `--cleanup` flag it will also restore the state after cleanup. If combined with `--pre-cleanup` it will **not** restore the state in between stages.

This is useful if you want to pass in secrets via files or if you want to execute commands after the build completes.

It will only take the snapshot if we are building a multistage image or if we plan to cleanup the filesystem either before or after the build.

Defaults to `false`. Can also be set via `KANIKO_PRESERVE_CONTEXT` environment variable.

## Flag `--push-ignore-immutable-tag-errors`

Set this boolean flag to `true` if you want the Kaniko process to exit with
success when a push error related to tag immutability occurs.

This is useful for example if you have parallel builds pushing the same tag
and do not care which one actually succeeds.

Defaults to `false`.

## Flag `--push-retry`

Set this flag to the number of retries that should happen for the push of an
image to a remote destination. Defaults to `0`.

## Flag `--registry-certificate`

Set this flag to provide a certificate for TLS communication with a given
registry.

Expected format is `my.registry.url=/path/to/the/certificate.cert`

## Flag `--registry-client-cert`

Set this flag to provide a certificate/key pair for mutual TLS (mTLS)
communication with a given
[registry that requires mTLS](https://docs.docker.com/engine/security/certificates/)
for authentication.

Expected format is
`my.registry.url=/path/to/client/cert.crt,/path/to/client/key.key`

## Flag `--registry-map`

Set this flag if you want to remap registries references. Useful for air gap
environment for example. You can use this flag more than once, if you want to
set multiple mirrors for a given registry. You can mention several remap in a
single flag too, separated by semi-colon. If an image is not found on the first
mirror, Kaniko will try the next mirror(s), and at the end fallback on the
original registry.

Registry maps can also be defined through `KANIKO_REGISTRY_MAP` environment
variable.

Expected format is
`original-registry=remapped-registry[;another-reg=another-remap[;...]]` for
example.

Note that you **can** specify a URL with scheme for this flag. Some valid options
are:

- `index.docker.io=mirror.gcr.io`
- `gcr.io=127.0.0.1`
- `quay.io=192.168.0.1:5000`
- `index.docker.io=docker-io.mirrors.corp.net;index.docker.io=mirror.gcr.io;gcr.io=127.0.0.1`
  will try `docker-io.mirrors.corp.net` then `mirror.gcr.io` for
  `index.docker.io` and `127.0.0.1` for `gcr.io`
- `docker.io=harbor.private.io/theproject`

## Flag `--registry-mirror`

Set this flag if you want to use a registry mirror instead of the default
`index.docker.io`. You can use this flag more than once, if you want to set
multiple mirrors. If an image is not found on the first mirror, Kaniko will try
the next mirror(s), and at the end fallback on the default registry.

Mirror can also be defined through `KANIKO_REGISTRY_MIRROR` environment
variable.

Expected format is `mirror.gcr.io` or `mirror.gcr.io/path` for example.

Note that you **can** specify a URL with scheme for this flag. Some valid options
are:

- `mirror.gcr.io`
- `127.0.0.1`
- `192.168.0.1:5000`
- `mycompany-docker-virtual.jfrog.io`
- `harbor.private.io/theproject`

## Flag `--require-add-checksum`

Fail if an `ADD` instruction pulls a remote URL (HTTP/HTTPS) without a `--checksum` digest.
Available on the warmer and executor. On the warmer, it requires `--add-cache`.

## Flag `--require-digest`

Fail if a base image is not pinned by digest, e.g. `debian:13.7@sha256:...`. Build args are resolved first. Also available on the warmer.

## Flag `--skip-default-registry-fallback`

Set this flag if you want the build process to fail if none of the mirrors
listed in flag [registry-mirror](#flag---registry-mirror) can pull some image.
This should be used with mirrors that implements a whitelist or some image
restrictions.

If [registry-mirror](#flag---registry-mirror) is not set or is empty, this flag
is ignored.

## Flag `--reproducible`

Set this flag to strip timestamps out of the built image and make it
reproducible.

## Flag `--secret`

Set this flag as `--secret id=MY_SECRET[,src=/file][,env=VAR][,type=file|env]` to configure build-secrets to be used during the build.

> [!IMPORTANT]
> The secret is **not stored securely** during the build and may be recoverable by other `RUN` steps even without explicitly mounting it. It should therefore not be considered confidential within the context of the build. The secret is never added to the image and never pushed.

## Flag `--single-snapshot`

This flag takes a single snapshot of the filesystem at the end of the build, so
only one layer will be appended to the base image.

## Flag `--skip-push-permission-check`

Set this flag to skip push permission check. This can be useful to delay Kanikos
first request for delayed network-policies.

## Flag `--skip-tls-verify`

Set this flag to skip TLS certificate validation when pushing to a registry. It
is supposed to be used for testing purposes only and should not be used in
production!

## Flag `--skip-tls-verify-pull`

Set this flag to skip TLS certificate validation when pulling from a registry.
It is supposed to be used for testing purposes only and should not be used in
production!

## Flag `--skip-tls-verify-registry`

You can set `--skip-tls-verify-registry <registry-name>` to skip TLS certificate
validation when accessing the specified registry. It is supposed to be used for
testing purposes only and should not be used in production! You can set it
multiple times for multiple registries.

## Flag `--snapshot-hash-workers`

Set `--snapshot-hash-workers=<n>` to hash the files of a snapshot on `n` goroutines. Defaults to `GOMAXPROCS`, at most 8.

## Flag `--snapshot-mode`

You can set the `--snapshot-mode=<full (default), redo, time>` flag to set how
kaniko will snapshot the filesystem.

- If `--snapshot-mode=full` is set, the full file contents and metadata are
  considered when snapshotting. This is the least performant option, but also
  the most robust.

- If `--snapshot-mode=redo` is set, the file mtime, size, mode, owner uid and
  gid will be considered when snapshotting. This may be up to 50% faster than
  "full", particularly if your project has a large number files.

- If `--snapshot-mode=time` is set, only file mtime will be considered when
  snapshotting (see [limitations related to mtime](../README.md#mtime-and-snapshotting)).

## Flag `--tar-path`

Set this flag as `--tar-path=<path>` to save the image as a tarball at path. You
need to set `--destination` as well (for example `--destination=image`). If you
want to save the image as tarball only you also need to set `--no-push`.

A common use case is to scan the tarball for vulnerabilities before pushing.
The companion [`executor push`](../README.md#subcommand-push) subcommand pushes a tarball
produced by `--tar-path --no-push` without re-running the build:

```bash
# 1. Build to a tarball
/kaniko/executor --tar-path=/tmp/image.tar --no-push ...

# 2. Scan
trivy image --input /tmp/image.tar --exit-code 1

# 3. Push only if the scan passes — same binary, no extra tooling
/kaniko/executor push /tmp/image.tar --destination registry.example.com/myapp:latest
```

## Flag `--target`

Set this flag to indicate which stages to build. If multiple targets are configured the first in the list is pushed.
If not set we implicitly target the last stage of the Dockerfile.
On the warmer it caches only the images those stages need, and without it every stage is warmed. The warmer requires `FF_KANIKO_WARMER_EXECUTOR_PARITY=true` for it.

## Flag `--use-new-run`

Using this flag enables an experimental implementation of the Run command which
does not rely on snapshotting at all. In this approach, in order to compute
which files were changed, a marker file is created before executing the Run
command. Then the entire filesystem is walked (takes ~1-3 seconds for 700Kfiles)
to find all files whose ModTime is greater than the marker file. With this new
run command implementation, the total build time is reduced seeing performance
improvements in the range of ~75%. This new run mode trades off
accuracy/correctness in some cases (potential for missed files in a "snapshot")
for improved performance by avoiding the full filesystem snapshots.

## Flag `--verbosity`

Set this flag as `--verbosity=<panic|fatal|error|warn|info|debug|trace>` to set
the logging level. Defaults to `info`.

## Flag `--ignore-var-run`

Ignore /var/run when taking image snapshot. Set it to false to preserve
/var/run/\* in destination image. (Default true).

## Flag `--ignore-path`

Set this flag as `--ignore-path=<path>` to ignore path when taking an image
snapshot. Set it multiple times for multiple ignore paths.

## Flag `--image-fs-extract-retry`

Set this flag to the number of retries that should happen for the extracting an
image filesystem. Defaults to `0`.
When set, base images are downloaded and verified in the local store before they are extracted, and the download is what gets retried.
Has no effect with `FF_KANIKO_SHARED_BASE_CACHE=false`.

## Flag `--image-download-retry`

Set this flag to the number of retries that should happen when downloading the
remote image. Consecutive retries occur with exponential backoff and an initial
delay of 1 second. Defaults to `0`.

## Flag `--predefined-annotations`

Set `--predefined-annotations` to annotate the image manifest with the OCI pre-defined annotation keys kaniko can determine. Defaults to `false`.

| key | value |
| --- | --- |
| `org.opencontainers.image.source`, `org.opencontainers.image.url` | the repository URL |
| `org.opencontainers.image.revision` | the commit |
| `org.opencontainers.image.version` | the git tag, when CI builds a tag |
| `org.opencontainers.image.created` | the build time |
| `org.opencontainers.image.base.name` | the fully qualified reference of the base image of the pushed stage |
| `org.opencontainers.image.base.digest` | the digest of that base image |

The repository, commit and tag come from GitLab CI or GitHub Actions, and from the git checkout of the context outside CI.

With `--reproducible` only the `base.*` keys are set. `--annotation` takes precedence over every key.

With `--provenance` the repository, commit and tag are also set on the index.

## Flag `--provenance`

Set `--provenance` to attach a SLSA provenance attestation to the image. Defaults to `false`. Inspect it with:

```bash
docker buildx imagetools inspect registry.example.com/app:v1 --format '{{json .Provenance}}'
```

The tag then points at an index holding the image and its attestation. `--digest-file` keeps the image digest, use `--index-file` to get the index.

The attestation lists the base images, the commit and the CI job. Build arg values and secrets are not recorded. See [Provenance](provenance.md) for every recorded field.

## Flag `--index-file`

Set `--index-file` to save the OCI index to a file. It requires `--provenance`.

```bash
jq -r '.manifests[] | select(.annotations["vnd.docker.reference.type"] != "attestation-manifest") | .digest' index.json
```
