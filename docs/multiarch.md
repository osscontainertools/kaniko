# Multi-arch images

The [GitLab components and the GitHub action](../README.md#building-multi-arch-images) do all of this. This page explains the workflow underneath, for running kaniko without them.

kaniko builds one image per run. A multi-arch image is several builds, one per architecture, stitched together into a single index with a tool such as [manifest-tool](https://github.com/estesp/manifest-tool) or crane.

## General Workflow

1. Build separate container images using kaniko on build hosts matching your target architecture and tag them with the appropriate architecture tag.
2. Push the separate images to your container registry.
3. Manifest-tool identifies the separate manifests in your container registry, according to a given template.
4. Manifest-tool pushes a combined manifest referencing the separate manifests.

![Workflow Multi-arch](images/multi-arch.drawio.svg)

In GitLab CI, with a runner tagged per architecture:

```yaml
build:
  stage: build
  parallel:
    matrix:
      - ARCH: [amd64, arm64]
  tags: [runner-$ARCH]
  image:
    name: ghcr.io/osscontainertools/kaniko:debug
    entrypoint: [""]
  script:
    - /kaniko/executor
      --context "$CI_PROJECT_DIR"
      --dockerfile "$CI_PROJECT_DIR/Dockerfile"
      --destination "$CI_REGISTRY_IMAGE:$CI_COMMIT_SHORT_SHA-$ARCH"

merge:
  stage: merge
  image:
    name: mplatform/manifest-tool:alpine
    entrypoint: [""]
  script:
    - manifest-tool
      --username "$CI_REGISTRY_USER" --password "$CI_REGISTRY_PASSWORD"
      push from-args
      --platforms linux/amd64,linux/arm64
      --template "$CI_REGISTRY_IMAGE:$CI_COMMIT_SHORT_SHA-ARCH"
      --target "$CI_REGISTRY_IMAGE:$CI_COMMIT_SHORT_SHA"
```

manifest-tool replaces `ARCH` in the template with each platform's architecture. The [components' multi-arch template](https://gitlab.com/osscontainertools/components/kaniko/-/blob/main/templates/build-multiarch.yml) is a complete example, which also keeps the provenance attestations when merging and hands the result on pinned by digest.

## Limitations and Pitfalls

1. You need build machines running the desired architectures. gitlab.com and github.com both offer hosted arm64 runners besides x86_64. Running kaniko under an emulator such as QEMU also works, but `RUN` steps get much slower.
2. kaniko needs to be able to run on the desired architectures. At the time of writing, the official kaniko container supports [linux/amd64, linux/arm64, linux/s390x and linux/ppc64le (not on \*-debug images)](https://github.com/osscontainertools/kaniko/blob/main/.github/workflows/images.yaml).
3. The container registry of your choice must be OCIv1 or Docker v2.2 compatible.
