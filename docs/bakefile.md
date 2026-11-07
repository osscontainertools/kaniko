# Building Several Images From One Dockerfile

`executor bake <bakefile> [target]` builds several images from one multi-stage Dockerfile in a single invocation.

The bakefile says which stage each image is built from, where it is pushed and which layer caches it uses. Each target is built on its own, as if `executor` ran once per target. The context and dockerfile come from the usual flags unless the target sets its own. Build args and registry settings come from the usual flags and are shared by every target.

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

A target can also set its own `context` and `dockerfile`:

```hcl
target "app" {
  context    = "services/app"
  dockerfile = "Dockerfile"
  target     = "app"
  destination = ["registry.example.com/app:latest"]
}
```

- `context`: Path to the build context for this target. Relative paths are resolved relative to the bakefile.
- `context_sub_path`: Sub path within the target's context, like [`--context-sub-path`](../README.md#flag---context-sub-path).
- `dockerfile`: Path to the Dockerfile for this target. Relative paths are resolved relative to the target's context (or bakefile).

## Choosing Targets

With no target named on the command line, every target in the file is built:

```shell
executor bake /workspace/kaniko-bake.hcl --context /workspace
```

Name one to build only that one:

```shell
executor bake /workspace/kaniko-bake.hcl app --context /workspace
```

## Overriding Target Attributes

`--set <target>.<field>=<value>` replaces a target's field:

```shell
executor bake /workspace/kaniko-bake.hcl \
  --set app.destination=$CI_REGISTRY_IMAGE/app:$CI_COMMIT_SHA \
  --set tools.destination=$CI_REGISTRY_IMAGE/tools:$CI_COMMIT_SHA \
  --context /workspace
```

`context`, `context_sub_path` and `dockerfile` can also be overridden with `--set`:

```shell
executor bake /workspace/kaniko-bake.hcl \
  --set app.context=/custom/context \
  --set app.dockerfile=/custom/context/Dockerfile \
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

kaniko uses its own names. An image reference is a `destination`, not a `tag`. Variables, functions, `group`, `inherits`, `matrix` and attestations are not supported.

A buildx bakefile fails with the construct it does not understand named:

```text
4:3: "tags" in target "app" is a docker-bake.hcl key, use destination
```
