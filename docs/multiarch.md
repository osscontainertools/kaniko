# Multi-arch images

The [GitLab components and the GitHub action](../README.md#building-multi-arch-images) do all of this. This page explains the workflow underneath, for running kaniko without them.

kaniko builds one image per run. A multi-arch image is several builds, one per architecture, stitched together into a single index with a tool such as [manifest-tool](https://github.com/estesp/manifest-tool) or crane.

## General Workflow

1. Build separate container images using kaniko on build hosts matching your target architecture and tag them with the appropriate architecture tag.
2. Push the separate images to your container registry.
3. Manifest-tool identifies the separate manifests in your container registry, according to a given template.
4. Manifest-tool pushes a combined manifest referencing the separate manifests.

![Workflow Multi-arch](images/multi-arch.drawio.svg)

## Limitations and Pitfalls

1. You need build machines running the desired architectures. gitlab.com and github.com both offer hosted arm64 runners besides x86_64. Running kaniko under an emulator such as QEMU also works, but `RUN` steps get much slower.
2. kaniko needs to be able to run on the desired architectures. At the time of writing, the official kaniko container supports [linux/amd64, linux/arm64, linux/s390x and linux/ppc64le (not on \*-debug images)](https://github.com/osscontainertools/kaniko/blob/main/.github/workflows/images.yaml).
3. The container registry of your choice must be OCIv1 or Docker v2.2 compatible.
