# Building Several Images From One Dockerfile

`executor bake <bakefile> [target]` builds several images from one multi-stage Dockerfile in a single invocation.

The bakefile says which stage each image is built from, where it is pushed and which layer caches it uses. The context, dockerfile, build args and registry settings come from the usual flags and are shared by every target.

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

`cache_from` and `cache_to` are lists of layer caches the target reads from and pushes to. They take the same values as [`--cache-from`](../README.md#flag---cache-from) and [`--cache-to`](../README.md#flag---cache-to), which `bake` does not accept as flags. A target without either uses `--cache-repo`, or `<destination>/cache` with `--cache`.

```hcl
target "app" {
  destination = ["registry.example.com/app:latest"]
  cache_from  = ["registry.example.com/app/cache/feature", "registry.example.com/app/cache/main"]
  cache_to    = ["registry.example.com/app/cache/feature"]
}
```

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

`--set <target>.cache_from=<repo>` and `--set <target>.cache_to=<repo>` replace a target's caches the same way.

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

```text
4:3: "tags" in target "app" is a docker-bake.hcl key, use destination
```
