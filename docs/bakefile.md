# Building Several Images From One Dockerfile

`executor bake <bakefile> [target]` builds several images from one multi-stage Dockerfile in a single invocation.

The bakefile says which stage each image is built from and where it is pushed. The context, dockerfile, build args, cache and registry settings come from the usual flags and are shared by every target.

It is the analogue of a `docker-bake.hcl`, expressed in kaniko's commands rather than buildx's. A `docker-bake.hcl` will not parse, see [Relationship to docker-bake.hcl](#relationship-to-docker-bakehcl).

```shell
executor bake /workspace/kaniko-bake.hcl --context /workspace --dockerfile Dockerfile
```

## The Bakefile

```hcl
target "app" {
  target      = "app"
  destination = ["registry.example.com/app:latest", "registry.example.com/app:v1"]
}

target "tools" {
  target      = "tools"
  destination = ["registry.example.com/tools:latest"]
}
```

`target "<name>"` declares one image. The name is what you pass on the command line and what `--set` refers to.

`target` inside the block is the Dockerfile stage to build. It defaults to the target's own name, so both blocks above could omit it.

`destination` is a list of references to push the image to. These are the same values [`--destination`](../README.md#flag---destination) takes.

## Choosing Targets

With no target named on the command line, every target in the file is built:

```shell
executor bake /workspace/kaniko-bake.hcl --context /workspace
```

Name one to build only that one:

```shell
executor bake /workspace/kaniko-bake.hcl app --context /workspace
```

## Overriding Destinations

`--set <target>.destination=<ref>` replaces a target's destinations:

```shell
executor bake /workspace/kaniko-bake.hcl \
  --set app.destination=$CI_REGISTRY_IMAGE/app:$CI_COMMIT_SHA \
  --set tools.destination=$CI_REGISTRY_IMAGE/tools:$CI_COMMIT_SHA \
  --context /workspace
```

Set it repeatedly to override several targets, or to give one target several references.

## Flags That Write One File

These flags write to a single path and cannot be used when building more than one target:

- `--digest-file`
- `--image-name-with-digest-file`
- `--image-name-tag-with-digest-file`
- `--tar-path`
- `--oci-layout-path`

Name a single target to use them.

## Relationship to docker-bake.hcl

kaniko uses its own names. An image reference is a `destination`, not a `tag`. There is no per-target `context` or `dockerfile`, those are flags. Variables, functions, `group`, `inherits`, `matrix` and attestations are not supported.

A buildx bakefile fails with the construct it does not understand named:

```
4:3: "tags" in target "app" is a docker-bake.hcl key, use destination
```
