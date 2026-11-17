# kaniko - Build Images In Kubernetes

[![Unit tests](https://github.com/osscontainertools/kaniko/actions/workflows/unit-tests.yaml/badge.svg)](https://github.com/osscontainertools/kaniko/actions/workflows/unit-tests.yaml)
[![Integration tests](https://github.com/osscontainertools/kaniko/actions/workflows/integration-tests.yaml/badge.svg)](https://github.com/osscontainertools/kaniko/actions/workflows/integration-tests.yaml)
[![Build images](https://github.com/osscontainertools/kaniko/actions/workflows/images.yaml/badge.svg)](https://github.com/osscontainertools/kaniko/actions/workflows/images.yaml)
[![codecov](https://codecov.io/gh/osscontainertools/kaniko/graph/badge.svg)](https://codecov.io/gh/osscontainertools/kaniko)

![kaniko logo](logo/Kaniko-Logo.png)

kaniko is a tool to build container images from a Dockerfile, inside a container
or Kubernetes cluster.

> [!IMPORTANT]
> This is a supported replacement of the original `GoogleContainerTools/kaniko`
> repository, which was archived in June of 2025.
> The focus of this fork is to keep dependencies up-to-date, fix bugs and improve performance.
> The images are available on docker hub [martizih/kaniko](https://hub.docker.com/r/martizih/kaniko).
> News and blog posts are on [osscontainertools.org](https://osscontainertools.org).
> If you are new here you can refer to our [Changelog Overview](./CHANGELOG_OVERVIEW.md) for the main differences to Google's v1.24.0 release.

kaniko doesn't depend on a Docker daemon and executes each command within a
Dockerfile completely in userspace. This enables building container images in
environments that can't easily or securely run a Docker daemon, such as a
standard Kubernetes cluster.

kaniko is meant to be run as an image: `ghcr.io/osscontainertools/kaniko:latest`. We do **not** recommend
running the kaniko executor binary in another image, as it might not work as you
expect - see [Known Issues](#known-issues).

<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->

**Table of Contents** _generated with
[DocToc](https://github.com/thlorenz/doctoc)_

- [kaniko - Build Images In Kubernetes](#kaniko---build-images-in-kubernetes)
  - [Community](#community)
  - [Sponsorships](#sponsorships)
    - [Corporations](#corporations)
    - [Individuals](#individuals)
  - [Alternatives](#alternatives)
  - [Releases](#releases)
  - [How does kaniko work?](#how-does-kaniko-work)
  - [Known Issues](#known-issues)
  - [Demo](#demo)
  - [Tutorial](#tutorial)
  - [Using kaniko](#using-kaniko)
    - [kaniko Build Contexts](#kaniko-build-contexts)
    - [Using Azure Blob Storage](#using-azure-blob-storage)
    - [Using Private Git Repository](#using-private-git-repository)
    - [Using Standard Input](#using-standard-input)
    - [Running kaniko](#running-kaniko)
      - [Running kaniko in GitLab CI](#running-kaniko-in-gitlab-ci)
      - [Running kaniko in GitHub Actions](#running-kaniko-in-github-actions)
      - [Running kaniko in a Kubernetes cluster](#running-kaniko-in-a-kubernetes-cluster)
        - [Kubernetes secret](#kubernetes-secret)
      - [Running kaniko in gVisor](#running-kaniko-in-gvisor)
      - [Running kaniko in Google Cloud Build](#running-kaniko-in-google-cloud-build)
      - [Running kaniko in Docker](#running-kaniko-in-docker)
    - [Bootstrapping Kaniko](#bootstrapping-kaniko)
    - [Caching](#caching)
      - [Caching Layers](#caching-layers)
      - [Caching Base Images](#caching-base-images)
    - [Pushing to Different Registries](#pushing-to-different-registries)
    - [Subcommands](#subcommands)
      - [Subcommand `login`](#subcommand-login)
      - [Subcommand `push`](#subcommand-push)
      - [Subcommand `bake`](#subcommand-bake)
    - [Additional Flags](#additional-flags)
    - [Feature Flags](#feature-flags)
    - [Assertion Overrides](#assertion-overrides)
    - [Layer Hints](#layer-hints)
    - [Telemetry](#telemetry)
    - [Debug Image](#debug-image)
  - [Security](#security)
    - [Verifying Signed Kaniko Images](#verifying-signed-kaniko-images)
    - [FIPS 140-3 Image](#fips-140-3-image)
  - [Building Multi-arch Images](#building-multi-arch-images)
  - [Comparison with Other Tools](#comparison-with-other-tools)
  - [Limitations](#limitations)
    - [mtime and snapshotting](#mtime-and-snapshotting)
    - [Dockerfile commands `--chown` support](#dockerfile-commands---chown-support)
  - [References](#references)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Community

If you are interested in contributing to kaniko, learn more from our
[development](DEVELOPMENT.md) and [contributing](CONTRIBUTING.md) guides.

For any community discussion [participate in open
issues](https://github.com/osscontainertools/kaniko/issues) or [file a new
issue](https://github.com/osscontainertools/kaniko/issues/new/choose).

Join our official chat on Matrix at [#kaniko:matrix.org](https://app.element.io/#/room/#kaniko:matrix.org). It has dedicated rooms for [#support@kaniko:matrix.org](https://app.element.io/#/room/#support@kaniko:matrix.org) and [#announcements@kaniko:matrix.org](https://app.element.io/#/room/#announcements@kaniko:matrix.org). We will also continue to monitor discussions in the [#kaniko on Kubernetes Slack](https://kubernetes.slack.com/messages/CQDCHGX7Y/).

## Sponsorships
We are grateful to the organizations and individuals who support our project.

### Corporations
[<img src="docs/sponsors/l3montree-logo.svg" alt="L3montree" width="250">](https://l3montree.com)

**[L3montree](https://l3montree.com)**

We thank L3montree for their generous support and commitment to open-source sustainability.

[<img src="docs/sponsors/siemens-logo.svg" alt="Siemens" width="250">](https://opensource.siemens.com)

**[Siemens](https://opensource.siemens.com)**

We thank Siemens for their generous support, and especially the team behind [opensource.siemens.com](https://opensource.siemens.com).

### Individuals
<table>
<tr>
<td align="center"><a href="https://github.com/Sped0n"><img src="https://github.com/Sped0n.png?size=80" width="80" alt="Sped0n"><br><sub><b>Sped0n</b></sub></a></td>
<td align="center"><a href="https://github.com/bootc"><img src="https://github.com/bootc.png?size=80" width="80" alt="bootc"><br><sub><b>bootc</b></sub></a></td>
</tr>
</table>

We are grateful for your support.

## Alternatives

Chainguard forked kaniko and continues to maintain the project as https://github.com/chainguard-dev/kaniko.
Chainguard is a company founded by the [original authors](https://github.com/chainguard-dev/kaniko?tab=readme-ov-file#history-and-status) of kaniko and hence it is a project dear to their heart.
Their focus is to keep dependencies up to date and patch security issues, keeping kaniko more or less as-is feature wise. They do not release images publicly, only to chainguard customers. However, there is good guidance on how to build kaniko yourself from their source-only releases.


## Releases

kaniko releases are published as images on [ghcr.io/osscontainertools/kaniko](https://github.com/orgs/osscontainertools/packages/container/package/kaniko) and on Docker Hub as [martizih/kaniko](https://hub.docker.com/r/martizih/kaniko).

Release notes and source code archives are available on the [releases
section](https://github.com/osscontainertools/kaniko/releases). For the release cadence and feature flag graduation policy, see [docs/releases.md](docs/releases.md).

Images available from other vendors:

* [kaniko-build organization](https://github.com/kaniko-build)

## How does kaniko work?

The kaniko executor image is responsible for building an image from a Dockerfile
and pushing it to a registry. Within the executor image, we extract the
filesystem of the base image (the FROM image in the Dockerfile). We then execute
the commands in the Dockerfile, snapshotting the filesystem in userspace after
each one. After each command, we append a layer of changed files to the base
image (if there are any) and update image metadata.

## Known Issues

- kaniko does not support building Windows containers.
- Running kaniko in any Docker image other than the official kaniko image is not
  supported due to implementation details.
  - This includes copying the kaniko executables from the official image into
    another image (e.g. a Jenkins CI agent).
  - In particular, it cannot use chroot or bind-mount because its container must
    not require privilege, so it unpacks directly into its own container root
    and may overwrite anything already there.
- kaniko does not support the v1 Registry API
  ([Registry v1 API Deprecation](https://www.docker.com/blog/registry-v1-api-deprecation/))

## Demo

![Demo](/docs/demo.gif)

## Tutorial

For a detailed example of kaniko with local storage, please refer to a
[getting started tutorial](./docs/tutorial.md).

Please see [References](#References) for more docs & video tutorials

## Using kaniko

To use kaniko to build and push an image for you, you will need:

1. A [build context](#kaniko-build-contexts), aka something to build
2. A [running instance of kaniko](#running-kaniko)

### kaniko Build Contexts

kaniko's build context is very similar to the build context you would send your
Docker daemon for an image build; it represents a directory containing a
Dockerfile which kaniko will use to build your image. For example, a `COPY`
command in your Dockerfile should refer to a file in the build context.

You will need to store your build context in a place that kaniko can access.
Right now, kaniko supports these storage solutions:

- GCS Bucket
- S3 Bucket
- Azure Blob Storage
- Local Directory
- Local Tar
- Standard Input
- Git Repository

_Note about Local Directory: this option refers to a directory within the kaniko
container. If you wish to use this option, you will need to mount in your build
context into the container as a directory._

_Note about Local Tar: this option refers to a tar gz file within the kaniko
container. If you wish to use this option, you will need to mount in your build
context into the container as a file._

_Note about Standard Input: the only Standard Input allowed by kaniko is in
`.tar.gz` format._

If using a GCS or S3 bucket, you will first need to create a compressed tar of
your build context and upload it to your bucket. Once running, kaniko will then
download and unpack the compressed tar of the build context before starting the
image build.

To create a compressed tar, you can run:

```shell
tar -C <path to build context> -zcvf context.tar.gz .
```

Then, copy over the compressed tar into your bucket. For example, we can copy
over the compressed tar to a GCS bucket with gsutil:

```shell
gsutil cp context.tar.gz gs://<bucket name>
```

When running kaniko, use the `--context` flag with the appropriate prefix to
specify the location of your build context:

| Source             | Prefix                                                                | Example                                                                       |
| ------------------ | --------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Local Directory    | dir://[path to a directory in the kaniko container]                   | `dir:///workspace`                                                            |
| Local Tar Gz       | tar://[path to a .tar.gz in the kaniko container]                     | `tar:///path/to/context.tar.gz`                                               |
| Standard Input     | tar://[stdin]                                                         | `tar://stdin`                                                                 |
| GCS Bucket         | gs://[bucket name]/[path to .tar.gz]                                  | `gs://kaniko-bucket/path/to/context.tar.gz`                                   |
| S3 Bucket          | s3://[bucket name]/[path to .tar.gz]                                  | `s3://kaniko-bucket/path/to/context.tar.gz`                                   |
| Azure Blob Storage | https://[account].[azureblobhostsuffix]/[container]/[path to .tar.gz] | `https://myaccount.blob.core.windows.net/container/path/to/context.tar.gz`    |
| Git Repository     | git://[repository url][#reference][#commit-id]                        | `git://github.com/acme/myproject.git#refs/heads/mybranch#<desired-commit-id>` |

If you don't specify a prefix, kaniko will assume a local directory. For
example, to use a GCS bucket called `kaniko-bucket`, you would pass in
`--context=gs://kaniko-bucket/path/to/context.tar.gz`.

### Using Azure Blob Storage

If you are using Azure Blob Storage for context file, you will need to pass
[Azure Storage Account Access Key](https://docs.microsoft.com/en-us/azure/storage/common/storage-configure-connection-string?toc=%2fazure%2fstorage%2fblobs%2ftoc.json)
as an environment variable named `AZURE_STORAGE_ACCESS_KEY` through Kubernetes
Secrets

### Using Private Git Repository

You can use `Personal Access Tokens` for Build Contexts from Private
Repositories from
[GitHub](https://blog.github.com/2012-09-21-easier-builds-and-deployments-using-git-over-https-and-oauth/).

You can either pass this in as part of the git URL (e.g.,
`git://TOKEN@github.com/acme/myproject.git#refs/heads/mybranch`) or using the
environment variable `GIT_TOKEN`.

You can also pass `GIT_USERNAME` and `GIT_PASSWORD` (password being the token)
if you want to be explicit about the username.

### Using Standard Input

If running kaniko and using Standard Input build context, you will need to add
the docker or kubernetes `-i, --interactive` flag. Once running, kaniko will
then get the data from `STDIN` and create the build context as a compressed tar.
It will then unpack the compressed tar of the build context before starting the
image build. If no data is piped during the interactive run, you will need to
send the EOF signal by yourself by pressing `Ctrl+D`.

Complete example of how to interactively run kaniko with `.tar.gz` Standard
Input data, using docker:

```shell
echo -e 'FROM alpine \nRUN echo "created from standard input"' > Dockerfile | tar -cf - Dockerfile | gzip -9 | docker run \
  --interactive -v $(pwd):/workspace ghcr.io/osscontainertools/kaniko:latest \
  --context tar://stdin \
  --destination=<YOUR-REGISTRY>/$project/$image:$tag>
```

Complete example of how to interactively run kaniko with `.tar.gz` Standard
Input data, using Kubernetes command line with a temporary container and
completely dockerless:

```shell
echo -e 'FROM alpine \nRUN echo "created from standard input"' > Dockerfile | tar -cf - Dockerfile | gzip -9 | kubectl run kaniko \
--rm --stdin=true \
--image=ghcr.io/osscontainertools/kaniko:latest --restart=Never \
--overrides='{
  "apiVersion": "v1",
  "spec": {
    "containers": [
      {
        "name": "kaniko",
        "image": "ghcr.io/osscontainertools/kaniko:latest",
        "stdin": true,
        "stdinOnce": true,
        "args": [
          "--dockerfile=Dockerfile",
          "--context=tar://stdin",
          "--destination<YOUR-REGISTRY>/<YOUR-REPO>/my-image"
        ],
        "volumeMounts": [
          {
            "name": "cabundle",
            "mountPath": "/kaniko/ssl/certs/"
          },
          {
            "name": "docker-config",
            "mountPath": "/kaniko/.docker/"
          }
        ]
      }
    ],
    "volumes": [
      {
        "name": "cabundle",
        "configMap": {
          "name": "cabundle"
        }
      },
      {
        "name": "docker-config",
        "configMap": {
          "name": "docker-config"
        }
      }
    ]
  }
}'
```

### Running kaniko

There are several different ways to deploy and run kaniko:

- [In GitLab CI](#running-kaniko-in-gitlab-ci)
- [In GitHub Actions](#running-kaniko-in-github-actions)
- [In a Kubernetes cluster](#running-kaniko-in-a-kubernetes-cluster)
- [In gVisor](#running-kaniko-in-gvisor)
- [In Google Cloud Build](#running-kaniko-in-google-cloud-build)
- [In Docker](#running-kaniko-in-docker)

#### Running kaniko in GitLab CI

The [kaniko components](https://gitlab.com/explore/catalog/osscontainertools/components/kaniko) build `Dockerfile` in the project directory and push it to the project's container registry. Later jobs receive the image pinned by digest as `KANIKO_IMAGE`.

```yaml
include:
  - component: gitlab.com/osscontainertools/components/kaniko/build@1.0.0
```

For a multi-arch image, one native runner per architecture:

```yaml
include:
  - component: gitlab.com/osscontainertools/components/kaniko/build-multiarch@1.0.0
```

See the [components' README](https://gitlab.com/osscontainertools/components/kaniko) for caching, provenance, bootstrapping and the other inputs.

#### Running kaniko in GitHub Actions

The [kaniko action](https://github.com/marketplace/actions/kaniko-oss-container-tools) builds `Dockerfile` in the workspace and pushes it to `ghcr.io/<owner>/<repo>`. Later steps receive the image pinned by digest as the `image` output.

```yaml
on: push

permissions:
  contents: read
  packages: write

jobs:
  image:
    runs-on: ubuntu-26.04
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - id: build
        uses: osscontainertools/kaniko-action@62bd46e3c9e39a6625220e87090a7af9cc431218 # v1.0.3
      - run: echo "pushed ${{ steps.build.outputs.image }}"
```

See the [action's README](https://github.com/osscontainertools/kaniko-action) for multi-arch images, Kubernetes runners and the other inputs.

#### Running kaniko in a Kubernetes cluster

Requirements:

- Standard Kubernetes cluster (e.g. using
  [GKE](https://cloud.google.com/kubernetes-engine/))
- [Kubernetes Secret](#kubernetes-secret)
- A [build context](#kaniko-build-contexts)

##### Kubernetes secret

To run kaniko in a Kubernetes cluster, you will need a standard running
Kubernetes cluster and a Kubernetes secret, which contains the auth required to
push the final image.

To create a secret to authenticate to Google Cloud Registry, follow these steps:

1. Create a service account in the Google Cloud Console project you want to push
   the final image to with `Storage Admin` permissions.
2. Download a JSON key for this service account
3. Rename the key to `kaniko-secret.json`
4. To create the secret, run:

```shell
kubectl create secret generic kaniko-secret --from-file=<path to kaniko-secret.json>
```

_Note: If using a GCS bucket in the same GCP project as a build context, this
service account should now also have permissions to read from that bucket._

The Kubernetes Pod spec should look similar to this, with the args parameters
filled in:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kaniko
spec:
  containers:
    - name: kaniko
      image: ghcr.io/osscontainertools/kaniko:latest
      args:
        - "--dockerfile=<path to Dockerfile within the build context>"
        - "--context=gs://<GCS bucket>/<path to .tar.gz>"
        - "--destination=<<YOUR-REGISTRY>/$PROJECT/$IMAGE:$TAG>"
      volumeMounts:
        - name: kaniko-secret
          mountPath: /secret
      env:
        - name: GOOGLE_APPLICATION_CREDENTIALS
          value: /secret/kaniko-secret.json
  restartPolicy: Never
  volumes:
    - name: kaniko-secret
      secret:
        secretName: kaniko-secret
```

This example pulls the build context from a GCS bucket. To use a local directory
build context, you could consider using configMaps to mount in small build
contexts.

#### Running kaniko in gVisor

Running kaniko in [gVisor](https://github.com/google/gvisor) provides an
additional security boundary. You will need to add the `--force` flag to run
kaniko in gVisor, since currently there isn't a way to determine whether or not
a container is running in gVisor.

```shell
docker run --runtime=runsc -v $(pwd):/workspace -v ~/.config:/root/.config \
ghcr.io/osscontainertools/kaniko:latest \
--dockerfile=<path to Dockerfile> --context=/workspace \
--destination=<YOUR-REGISTRY>/<YOUR-REPO>/my-image --force
```

We pass in `--runtime=runsc` to use gVisor. This example mounts the current
directory to `/workspace` for the build context and the `~/.config` directory
for GCR credentials.

#### Running kaniko in Google Cloud Build

Requirements:

- A [build context](#kaniko-build-contexts)

To run kaniko in GCB, add it to your build config as a build step:

```yaml
steps:
  - name: ghcr.io/osscontainertools/kaniko:latest
    args:
      [
        "--dockerfile=<path to Dockerfile within the build context>",
        "--context=dir://<path to build context>",
        "--destination=<<YOUR-REGISTRY>/$PROJECT/$IMAGE:$TAG>",
      ]
```

kaniko will build and push the final image in this build step.

#### Running kaniko in Docker

Requirements:

- [Docker](https://docs.docker.com/install/)

We can run the kaniko executor image locally in a Docker daemon to build and
push an image from a Dockerfile.

```shell
docker run \
    -v "$HOME"/.docker/config.json:/kaniko/.docker/config.json:ro \
    -v /path/to/context:/workspace \
    ghcr.io/osscontainertools/kaniko:latest \
    --dockerfile /workspace/Dockerfile \
    --destination "registry.example.com/$IMAGE_NAME:$TAG" \
    --context dir:///workspace/
```

### Bootstrapping Kaniko

Images with files in `/kaniko` or `/busybox`, such as kaniko itself, need kaniko's bootstrap mode. The [GitLab components](#running-kaniko-in-gitlab-ci) and the [GitHub action](#running-kaniko-in-github-actions) switch it on with `bootstrap: true`:

```yaml
include:
  - component: gitlab.com/osscontainertools/components/kaniko/build@1.0.0
    inputs:
      dockerfile: deploy/Dockerfile
      target: kaniko-debug
      build_args: TARGETARCH=amd64
      bootstrap: true
```

```yaml
      - uses: osscontainertools/kaniko-action@62bd46e3c9e39a6625220e87090a7af9cc431218 # v1.0.3
        with:
          dockerfile: deploy/Dockerfile
          target: kaniko-debug
          build-args: TARGETARCH=amd64
          bootstrap: true
```

See [Bootstrapping kaniko](docs/bootstrapping.md) for how it works, and for running it without them.

### Caching

#### Caching Layers

kaniko can cache layers created by `RUN`(configured by flag
`--cache-run-layers`) and `COPY` (configured by flag `--cache-copy-layers`)
commands in a remote repository. Before executing a command, kaniko checks the
cache for the layer. If it exists, kaniko will pull and extract the cached layer
instead of executing the command. If not, kaniko will execute the command and
then push the newly created layer to the cache.

Note that kaniko cannot read layers from the cache after a cache miss: once a
layer has not been found in the cache, all subsequent layers are built locally
without consulting the cache.

Users can opt into caching by setting the `--cache=true` flag. A remote
repository for storing cached layers can be provided via the `--cache-repo`
flag. If this flag isn't provided, a cached repo will be inferred from the
`--destination` provided.

#### Caching Base Images

kaniko can cache images in a local directory that can be volume mounted into the
kaniko pod. To do so, the cache must first be populated, as it is read-only. We
provide a kaniko cache warming image at `gcr.io/kaniko-project/warmer`:

```shell
docker run -v $(pwd):/workspace ghcr.io/osscontainertools/kaniko:warmer --cache-dir=/workspace/cache --image=<image to cache> --image=<another image to cache>
docker run -v $(pwd):/workspace ghcr.io/osscontainertools/kaniko:warmer --cache-dir=/workspace/cache --dockerfile=<path to dockerfile>
docker run -v $(pwd):/workspace ghcr.io/osscontainertools/kaniko:warmer --cache-dir=/workspace/cache --dockerfile=<path to dockerfile> --build-arg version=1.19
```

`--image` can be specified for any number of desired images. `--dockerfile` can
be specified for the path of dockerfile for cache.These command will combined to
cache those images by digest in a local directory named `cache`. Once the cache
is populated, caching is opted into with the same `--cache=true` flag as above.
The location of the local cache is provided via the `--cache-dir` flag,
defaulting to `/cache` as with the cache warmer. See the `examples` directory
for how to use with kubernetes clusters and persistent cache volumes.

`--dryrun` prints which images are already cached and which would be fetched, without writing to the cache. `KANIKO_PRINT_PLAN=1` prints the same plan before warming.

`warmer bake <bakefile> [target...]` warms the base images of bakefile targets, see [docs/bakefile.md](docs/bakefile.md#warmer).

### Pushing to Different Registries

For registry-specific setup instructions (Docker Hub, GCR, ECR, ACR, JFrog,
registry mirrors and maps) see **[docs/registries.md](docs/registries.md)**.

### Subcommands

In addition to the default build-and-push flow, the `executor` binary exposes a small set of subcommands.

#### Subcommand `login`

`executor login <registry>` stores registry credentials in the Docker config file at `$DOCKER_CONFIG/config.json` (the executor image sets `DOCKER_CONFIG=/kaniko/.docker/`), so subsequent `executor` invocations can authenticate to the registry without a credential helper.

Flags:

- `-u, --username <user>` — username for the registry (required).
- `-p, --password <pass>` — password or token; mutually exclusive with `--password-stdin`.
- `--password-stdin` — read the password from standard input; useful for piping a secret without exposing it in the process list.

#### Subcommand `push`

`executor push <path> --destination <ref>` reads a pre-built image and pushes it to one or more registries, skipping the build entirely. The path may point at a docker-save format tarball produced by [`--tar-path`](docs/cli.md#flag---tar-path) or at an OCI image layout directory produced by [`--oci-layout-path`](docs/cli.md#flag---oci-layout-path). All registry, auth, retry, and digest-file flags are the same as on the build command. See the canonical workflow under [`--tar-path`](docs/cli.md#flag---tar-path).

The artifact must contain exactly one image. Multi-image tarballs and indexes are not supported.

#### Subcommand `bake`

`executor bake <bakefile> [target...]` builds several images from one multi-stage Dockerfile in a single invocation. A small HCL bakefile says which stage each image is built from, where it is pushed and which layer caches it uses, while the context, dockerfile, build args and registry settings stay on the usual flags and are shared by every target.

```hcl
target "app" {
  destination = ["registry.example.com/app:latest"]
}

target "tools" {
  destination = ["registry.example.com/tools:latest"]
}
```

It is the analogue of a `docker-bake.hcl`, expressed in kaniko's commands rather than buildx's, which also means a `docker-bake.hcl` will not parse. For the format, choosing targets, and overriding destinations with `--set`, see **[docs/bakefile.md](docs/bakefile.md)**.

### Additional Flags

Every executor flag is described in [docs/cli.md](docs/cli.md).

### Feature Flags

New behaviour ships behind `FF_KANIKO_*` environment variables before it becomes the default. The [Preview profile](docs/feature-flags.md#preview) turns on the flags that become default in the next minor release. Every flag and profile is described in [docs/feature-flags.md](docs/feature-flags.md).

### Assertion Overrides

Kaniko checks internal invariants at runtime. If one is violated the build stops with a message like:

```
Assertion violated [executor.build.metadata-only]: ...
```

This is always a bug in kaniko, please [open an issue](https://github.com/osscontainertools/kaniko/issues).

As a temporary workaround, pass the name in brackets to `KANIKO_IGNORE_ASSERTIONS` to skip that assertion and log a warning instead:

```sh
KANIKO_IGNORE_ASSERTIONS=executor.build.metadata-only
```

Multiple names can be passed as a comma-separated list.

### Layer Hints

Kaniko logs a hint when a command adds files to its layer that usually do not belong in an image:

```
HINT SnapshotCacheDir: 184.2MB in 1532 files under /root/.cache/pip, use RUN --mount=type=cache or remove it in the same RUN
```

See [docs/layer-hints.md](docs/layer-hints.md) for the list of hints.

To silence a hint, pass its name to `KANIKO_IGNORE_HINTS`:

```sh
KANIKO_IGNORE_HINTS=SnapshotCacheDir
```

Multiple names can be passed as a comma-separated list.

### Telemetry

Kaniko can export an OpenTelemetry trace of each build so you can track build times, cache hits and misses, and which Dockerfile instructions keep busting the cache. With the [GitLab components](#running-kaniko-in-gitlab-ci) and the [GitHub action](#running-kaniko-in-github-actions), set `telemetry: firn` or `telemetry: custom` with the endpoint, and they request the job's ID token themselves. On GitHub, the job needs the permission `id-token: write`.

Without them, telemetry is off by default and a no-op unless you point it at a collector:

```sh
KANIKO_TELEMETRY_ENDPOINT=http://otel-collector:4318
```

Each build becomes a trace, with a span per build phase and Dockerfile command. Telemetry is best effort and never fails a build.

**What leaves the machine**: traces carry build details unredacted, including the text of every instruction, listed in [What leaves the machine](docs/telemetry.md#what-leaves-the-machine). `KANIKO_TELEMETRY_OMIT_DOCKERFILE=true` withholds the full Dockerfile source, the build plan and the arguments of `RUN`, `ENV` and `ARG`. The value behind a `RUN --mount=type=secret` and the contents of a `--mount=type=cache` are never sent. If your Dockerfile or `RUN` commands contain credentials, treat the collector as part of your secret boundary.

Spans are sent over OTLP/**HTTP(S)**, OTLP/**gRPC** is not supported. The endpoint URL must include a scheme, and only `KANIKO_TELEMETRY_ENDPOINT` enables tracing, the standard `OTEL_EXPORTER_OTLP_ENDPOINT` alone does not.

See [Telemetry attributes](docs/telemetry.md) for the full list of exported span attributes.

Standard OpenTelemetry environment variables apply for the rest: `OTEL_EXPORTER_OTLP_HEADERS` for authenticating to the collector, and `OTEL_RESOURCE_ATTRIBUTES` for fleet labels of your own. `KANIKO_TELEMETRY_TOKEN_EXCHANGE_ENDPOINT` authenticates without a stored token, see [Authenticating to the collector](docs/telemetry.md#authenticating-to-the-collector).

### Debug Image

The kaniko executor image is based on scratch and doesn't contain a shell. We
provide `gcr.io/kaniko-project/executor:debug`, a debug image which consists of
the kaniko executor image along with a busybox shell to enter.

You can launch the debug image with a shell entrypoint:

```shell
docker run -it --entrypoint=/busybox/sh ghcr.io/osscontainertools/kaniko:debug
```

## Security

kaniko by itself **does not** make it safe to run untrusted builds inside your
cluster, or anywhere else.

kaniko relies on the security features of your container runtime to provide
build security.

The minimum permissions kaniko needs inside your container are governed by a few
things:

- The permissions required to unpack your base image into its container
- The permissions required to execute the RUN commands inside the container

If you have a minimal base image (SCRATCH or similar) that doesn't require
permissions to unpack, and your Dockerfile doesn't execute any commands as the
root user, you can run kaniko without root permissions. It should be noted that
Docker runs as root by default, so you still require (in a sense) privileges to
use kaniko.

You may be able to achieve the same default seccomp profile that Docker uses in
your Pod by setting
[seccomp](https://kubernetes.io/docs/concepts/policy/pod-security-policy/#seccomp)
profiles with annotations on a
[PodSecurityPolicy](https://cloud.google.com/kubernetes-engine/docs/how-to/pod-security-policies)
to create or update security policies on your cluster.

### Verifying Signed Kaniko Images

kaniko images are signed for versions >= 1.24.1 using
[cosign](https://github.com/sigstore/cosign)!

To verify a public image, install [cosign](https://github.com/sigstore/cosign)
and use keyless verification with github actions issuer.
Note that to verify images after v1.26.0 you require cosign v3.

```
$ cosign verify \
  --certificate-identity-regexp "https://github.com/osscontainertools/kaniko/.github/workflows/images.yaml@.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ghcr.io/osscontainertools/kaniko:latest
```

kaniko images for older versions [1.24.1 - 1.25.5] can be verified using
```
$ cosign verify \
  --certificate-identity-regexp "https://github.com/mzihlmann/kaniko/.github/workflows/images.yaml@.*" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  ghcr.io/osscontainertools/kaniko:latest
```

### FIPS 140-3 Image

The `slim-fips` image is the `slim` executor built with the Go Cryptographic Module v1.0.0.
FIPS 140-3 mode is on by default (`GODEBUG=fips140=on`).
The module's validation ([CMVP #5247](https://csrc.nist.gov/projects/cryptographic-module-validation-program/certificate/5247)) does not cover `linux/riscv64`, so the `riscv64` image runs in FIPS mode but is not certified.
Like `slim`, it has no gcr, ecr or acr credential helpers and no git, S3, GCS or Azure build contexts.

kaniko still uses non-approved hashes internally for file change detection and layer cache keys:

- highwayhash for snapshots in the default `--snapshot-mode=full`
- md5 for snapshots in `--snapshot-mode=redo` and `--snapshot-mode=time`
- blake3 for layer cache keys, or md5 with `FF_KANIKO_CACHE_HASH_BLAKE3=false`

We are working on removing the remaining non-approved hashes.
Until then `GODEBUG=fips140=only` panics in `redo` and `time` snapshot modes, and with `FF_KANIKO_CACHE_HASH_BLAKE3=false`.

## Building Multi-arch Images

kaniko builds one image per run. The [GitLab components](#running-kaniko-in-gitlab-ci) and the [GitHub action's](#running-kaniko-in-github-actions) reusable workflow `multiarch.yml` build one image per architecture, each on a native runner, and merge them into one multi-arch image. The action alone builds all architectures in one job with its `platforms` input, emulating all but the runner's own with QEMU.

```yaml
include:
  - component: gitlab.com/osscontainertools/components/kaniko/build-multiarch@1.0.0
```

```yaml
jobs:
  image:
    permissions:
      contents: read
      packages: write
    uses: osscontainertools/kaniko-action/.github/workflows/multiarch.yml@62bd46e3c9e39a6625220e87090a7af9cc431218 # v1.0.3
```

See [Multi-arch images](docs/multiarch.md) for how it works, and for running it without them.

## Comparison with Other Tools

Similar tools include:

- [BuildKit](https://github.com/moby/buildkit)
- [img](https://github.com/genuinetools/img)
- [orca-build](https://github.com/cyphar/orca-build)
- [umoci](https://github.com/openSUSE/umoci)
- [buildah](https://github.com/containers/buildah)
- [FTL](https://github.com/GoogleCloudPlatform/runtimes-common/tree/master/ftl)
- [Bazel rules_docker](https://github.com/bazelbuild/rules_docker)

All of these tools build container images with different approaches.

BuildKit (and `img`) can perform as a non-root user from within a container but
requires seccomp and AppArmor to be disabled to create nested containers.
`kaniko` does not actually create nested containers, so it does not require
seccomp and AppArmor to be disabled. BuildKit supports "cross-building"
multi-arch containers by leveraging QEMU.

`orca-build` depends on `runc` to build images from Dockerfiles, which can not
run inside a container (for similar reasons to `img` above). `kaniko` doesn't
use `runc` so it doesn't require the use of kernel namespacing techniques.
However, `orca-build` does not require Docker or any privileged daemon (so
builds can be done entirely without privilege).

`umoci` works without any privileges, and also has no restrictions on the root
filesystem being extracted (though it requires additional handling if your
filesystem is sufficiently complicated). However, it has no `Dockerfile`-like
build tooling (it's a slightly lower-level tool that can be used to build such
builders -- such as `orca-build`).

`Buildah` specializes in building OCI images. Buildah's commands replicate all
of the commands that are found in a Dockerfile. This allows building images with
and without Dockerfiles while not requiring any root privileges. Buildah’s
ultimate goal is to provide a lower-level coreutils interface to build images.
The flexibility of building images without Dockerfiles allows for the
integration of other scripting languages into the build process. Buildah follows
a simple fork-exec model and does not run as a daemon but it is based on a
comprehensive API in golang, which can be vendored into other tools.

`FTL` and `Bazel` aim to achieve the fastest possible creation of Docker images
for a subset of images. These can be thought of as a special-case "fast path"
that can be used in conjunction with the support for general Dockerfiles kaniko
provides.

## Limitations

### mtime and snapshotting

When taking a snapshot, kaniko's hashing algorithms include (or in the case of
[`--snapshot-mode=time`](#--snapshotmode), only use) a file's
[`mtime`](https://en.wikipedia.org/wiki/Inode#POSIX_inode_description) to
determine if the file has changed. Unfortunately, there is a delay between when
changes to a file are made and when the `mtime` is updated. This means:

- With the time-only snapshot mode (`--snapshot-mode=time`), kaniko may miss
  changes introduced by `RUN` commands entirely.
- With the default snapshot mode (`--snapshot-mode=full`), whether or not kaniko
  will add a layer in the case where a `RUN` command modifies a file **but the
  contents do not** change is theoretically non-deterministic. This _does not
  affect the contents_ which will still be correct, but it does affect the
  number of layers.

_Note that these issues are currently theoretical only. If you see this issue
occur, please
[open an issue](https://github.com/osscontainertools/kaniko/issues)._

### Dockerfile commands `--chown` support
Kaniko currently supports `COPY --chown` and `ADD --chown` Dockerfile command. It does not support `RUN --chown`.

## References

- [Kaniko - Building Container Images In Kubernetes Without Docker](https://youtu.be/EgwVQN6GNJg).
