# Development

This doc explains the development workflow so you can get started
[contributing](CONTRIBUTING.md) to Kaniko!

## Getting started

You will need [Go 1.26+](https://go.dev/dl/) and [Docker](https://docs.docker.com/install/) installed.

First you will need to setup your GitHub account and create a fork:

1. Create [a GitHub account](https://github.com/join)
1. Setup [GitHub access via
   SSH](https://help.github.com/articles/connecting-to-github-with-ssh/)
1. [Create and checkout a repo fork](#checkout-your-fork)

Once you have those, you can iterate on kaniko:

1. [Build kaniko](#building-kaniko)
1. [Test your changes](#testing-your-changes)
1. [Verifying kaniko builds](#verifying-kaniko-builds)
1. [Run kaniko tests](#testing-kaniko)

When you're ready, you can [create a PR](#creating-a-pr)!

## Repository structure

```
cmd/
  executor/       # kaniko executor entry point
  warmer/         # cache warmer entry point
pkg/
  commands/       # Dockerfile command implementations (COPY, RUN, ADD, …)
  executor/       # core build logic (build.go is the heart of the build loop)
  snapshot/       # filesystem snapshotting
  dockerfile/     # Dockerfile parsing
  buildcontext/   # build context sources (local, GCS, S3, git, …)
  image/          # image pulling and pushing
  cache/          # cache lookup (remote layer cache and warmer-primed base images)
  warmer/         # warmer implementation, populates the local base image cache
  util/           # shared utilities
deploy/           # Dockerfiles for the published kaniko images
integration/      # integration tests and test Dockerfiles
golden/           # golden tests and their snapshots
examples/         # example Kubernetes manifests
docs/             # documentation
```

If you are fixing a Dockerfile command bug, start in `pkg/commands/`. If you are working on snapshotting or layer output, start in `pkg/snapshot/`. For the overall build flow, start in `pkg/executor/build.go`.

## Checkout your fork

To check out this repository:

1. Create your own [fork of this
  repo](https://help.github.com/articles/fork-a-repo/)
2. Clone it to your machine:

  ```shell
  git clone git@github.com:${YOUR_GITHUB_USERNAME}/kaniko.git
  cd kaniko
  git remote add upstream git@github.com:osscontainertools/kaniko.git
  git remote set-url --push upstream no_push
  ```

_Adding the `upstream` remote sets you up nicely for regularly [syncing your
fork](https://help.github.com/articles/syncing-a-fork/)._

## Building kaniko

Build the kaniko executor binary with:

```shell
make
```

Note that the resulting binary cannot and should not be run directly on your host machine. As part of a normal build, kaniko performs destructive filesystem operations that are safe only inside a container. See [Running kaniko](README.md#running-kaniko) for how to run it.

## Testing your changes

After `make`, the executor binary is at `out/executor`. Mount it into the debug image to run your local build:

```shell
# Build only, no push, useful for quick iteration
docker run --rm \
  -v $(pwd)/out/executor:/kaniko/executor \
  -v $(pwd)/integration:/workspace \
  ghcr.io/osscontainertools/kaniko:debug \
  --context /workspace \
  --dockerfile /workspace/dockerfiles/Dockerfile_test_issue_new \
  --no-push

# Build and push to a local registry
docker run --network=host --rm \
  -v $(pwd)/out/executor:/kaniko/executor \
  -v $(pwd)/integration:/workspace \
  ghcr.io/osscontainertools/kaniko:debug \
  --context /workspace \
  --dockerfile /workspace/dockerfiles/Dockerfile_test_issue_new \
  --destination localhost:5000/test
```

Swap in any Dockerfile from `integration/dockerfiles/` or write your own.

Note: `run_in_docker.sh` in the repo root is a different tool. It runs the published kaniko image against a user-provided Dockerfile and is not intended for testing local code changes.

## Verifying kaniko builds

Images built with kaniko should be no different from images built elsewhere.
While you iterate on kaniko, you can verify images built with kaniko by:

1. Build the image using another system, such as `docker build`
2. Use [`diffoci`](https://github.com/mzihlmann/diffoci) to diff the images

## Testing kaniko

kaniko has [unit tests](#unit-tests), [golden tests](#golden-tests), and [integration tests](#integration-tests).

Please note that the tests require a Linux machine - use Vagrant to quickly set
up the test environment needed if you work with macOS or Windows.

### Unit Tests

The unit tests live with the code they test and can be run with:

```shell
make test
```

### Golden Tests

Golden tests verify the build plan kaniko computes for a given Dockerfile (the sequence of stages, steps, and cache keys it would execute) without actually building anything. See [testplan.md](docs/testplan.md) for more detail on what they cover.

Run them with:

```shell
make golden
```

If your change intentionally affects how kaniko plans a build, update the golden files:

```shell
UPDATE=1 make golden
```

To run a specific subset:

```shell
TESTS=<pattern> make golden
```

### Lint Checks

The helper script to install and run lint is placed here at the root of project.

```shell
./hack/linter.sh
```

To fix any `gofmt` issues, you can simply run `gofmt` with `-w` flag like this

```shell
find . -name "*.go" | grep -v vendor/ | xargs gofmt -l -s -w
```

### Integration tests

Currently the integration tests that live in [`integration`](./integration) can be run against your own gcloud space or a local registry.

These tests will be kicked off by [reviewers](#reviews) for submitted PRs using GitHub Actions.

In either case, you will need the following tools:

* [`diffoci`](https://github.com/mzihlmann/diffoci#installation)

#### GCloud

To run integration tests with your GCloud Storage, you will also need the following tools:

* [`gcloud`](https://cloud.google.com/sdk/install)
* [`gsutil`](https://cloud.google.com/storage/docs/gsutil_install)
* A bucket in [GCS](https://cloud.google.com/storage/) which you have write access to via
  the user currently logged into `gcloud`
* An image repo which you have write access to via the user currently logged into `gcloud`
* A docker account and a `~/.docker/config.json` with login credentials if you run
  into rate limiting problems during tests.

Once this step done, you must override the project using environment variables:

* `GCS_BUCKET` - The name of your GCS bucket
* `IMAGE_REPO` - The path to your Docker image repo on your registry host

This can be done as follows:

```shell
export GCS_BUCKET="gs://<your bucket>"
export IMAGE_REPO="YOUR-REGISTRY/YOUR-REPO"
```

Login for both user and application credentials
```shell
gcloud auth login
gcloud auth application-default login
```

Then you can launch integration tests as follows:

```shell
make integration-test
```

You can also run individual test suites:

```shell
make integration-test-layers
make integration-test-run
make integration-test-k8s
make integration-test-misc
```

#### Local integration tests

To run integration tests locally against a local registry and gcs bucket, set the LOCAL environment variable

```shell
LOCAL=1 make integration-test
```

#### Running integration tests for a specific dockerfile

In order to test only specific dockerfiles during local integration testing, you can specify a pattern to match against inside the integration/dockerfiles directory.

```shell
DOCKERFILE_PATTERN="Dockerfile_test_add*" make integration-test-run
```

This will only run dockerfiles that match the pattern `Dockerfile_test_add*`



### Benchmarking

The goal is for Kaniko to be at least as fast at building Dockerfiles as Docker is, and to that end, we've built
in benchmarking to check the speed of not only each full run, but also how long each step of each run takes. To turn
on benchmarking, just set the `BENCHMARK_FILE` environment variable, and kaniko will output all the benchmark info
of each run to that file location.

```shell
docker run -v $(pwd):/workspace -v ~/.config:/root/.config \
-e BENCHMARK_FILE=/workspace/benchmark_file \
ghcr.io/osscontainertools/kaniko:latest \
--dockerfile=<path to Dockerfile> --context=/workspace \
--destination=<YOUR-REGISTRY>/<YOUR-REPO>/new-image
```
Additionally, the integration tests can output benchmarking information to a `benchmarks` directory under the
`integration` directory if the `BENCHMARK` environment variable is set to `true.`

```shell
BENCHMARK=true go test -v --bucket $GCS_BUCKET --repo $IMAGE_REPO
```

#### Profiling

If your builds are taking long, you can analyze kaniko
function calls using [Slow Jam](https://github.com/google/slowjam). To start
profiling,

1. Add an environment variable `STACKLOG_PATH` to your
   [pod definition](https://github.com/osscontainertools/kaniko/blob/main/examples/pod-build-profile.yaml#L15).
2. If you are using the kaniko `debug` image, you can copy the file in the
   `pre-stop` container lifecycle hook.


## Creating a PR

When you have changes you would like to propose to kaniko, you will need to:

1. Ensure the commit message(s) follow the style described in [CONTRIBUTING.md](CONTRIBUTING.md#commit-messages)
1. If your change introduces new behavior, wrap it in a feature flag. See [docs/releases.md](docs/releases.md) for when this is required
1. [Create a pull request](https://help.github.com/articles/creating-a-pull-request-from-a-fork/)

### Reviews

Each PR must be reviewed by a maintainer. Maintainers will trigger [the integration tests](#integration-tests) in CI, which must pass for the PR to be submitted.
