# Bootstrapping kaniko

The [GitLab components and the GitHub action](../README.md#bootstrapping-kaniko) do all of this with `bootstrap: true`. This page explains what happens underneath, for running kaniko without them.

kaniko's approach to building images is unique. It doesn't build up and snapshot an image in a separate container, instead it builds up and snapshots the image in the container where kaniko is installed. The benefit is that kaniko can run without any privileges, because it's not actually using any containerization technologies. The downside is that some builds yield surprising results, and bootstrapping the kaniko image with kaniko is one such case.

The first problem is that the kaniko binaries are installed in the `/kaniko` directory. This directory must be ignored during snapshots, so the build tool does not leak into the images it produces. But this also means that kaniko by default can't build a kaniko image with the binaries installed in `/kaniko`. The environment variable `KANIKO_DIR=/kaniko2` moves all the binaries to a different location before the build.

The second problem only affects builds using the `debug` image, such as with gitlab-runner. The shell in the debug image is in `/busybox`, which can't be snapshotted either, and there is no override to move it. The way around it is a bootstrap image that has the shell in a different location, `/busybox2`, which then builds the actual new debug image.

That bootstrap image is published as `ghcr.io/osscontainertools/kaniko:bootstrap`, built from the `kaniko-debug-2` target of [deploy/Dockerfile](../deploy/Dockerfile). Run the build in it with `KANIKO_DIR=/kaniko2`. In GitLab CI, building kaniko's debug image looks like this:

```yaml
build-kaniko:
  image:
    name: ghcr.io/osscontainertools/kaniko:bootstrap
    entrypoint: [""]
  variables:
    KANIKO_DIR: /kaniko2
  script:
    - /kaniko/executor
      --context "$CI_PROJECT_DIR"
      --dockerfile deploy/Dockerfile
      --target kaniko-debug
      --destination "$CI_REGISTRY_IMAGE/kaniko:$CI_COMMIT_SHORT_SHA"
```

The same with Docker. Mount the registry credentials outside `/kaniko`, since kaniko moves that directory away and a mount inside it cannot move:

```shell
docker run \
    -e KANIKO_DIR=/kaniko2 \
    -e DOCKER_CONFIG=/docker-config \
    -v "$HOME"/.docker/config.json:/docker-config/config.json:ro \
    -v "$PWD":/workspace \
    ghcr.io/osscontainertools/kaniko:bootstrap \
    --context dir:///workspace/ \
    --dockerfile /workspace/deploy/Dockerfile \
    --target kaniko-debug \
    --destination "registry.example.com/kaniko:$TAG"
```

The [components' build template](https://gitlab.com/osscontainertools/components/kaniko/-/blob/main/templates/build.yml) is a complete example, bootstrap mode included.
