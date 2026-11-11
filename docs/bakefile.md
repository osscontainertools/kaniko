# Building Several Images From One Dockerfile

`executor bake <bakefile> [target]` builds several images from one multi-stage Dockerfile in a single invocation.

The bakefile says which stage each image is built from, where it is pushed and which layer caches it uses. Each target is built on its own, as if `executor` ran once per target. The context and dockerfile come from the usual flags unless the target sets its own. Build args and registry settings come from the usual flags and are shared by every target. With several targets, every distinct context is copied into the kaniko directory before the first build.

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

`context`, `context_sub_path` and `dockerfile` can also be overridden with `--set`. Relative paths resolve the same way as in the bakefile:

```shell
executor bake /workspace/kaniko-bake.hcl \
  --set app.context=/custom/context \
  --set app.dockerfile=/custom/context/Dockerfile \
  --context /workspace
```

Set it repeatedly to override several targets, or to give one target several references.

`--set <target>.cache_from=<repo>` and `--set <target>.cache_to=<repo>` replace a target's caches the same way.

## Printing the Resolved Targets

`--print` prints the selected targets as JSON after `--set` is applied, then exits without building. Fields a target does not set are left out and come from the flags:

```shell
executor bake /workspace/kaniko-bake.hcl app --print \
  --set app.context=/custom/context \
  --set app.destination=registry.example.com/app:dev
```

```json
{
  "target": {
    "app": {
      "context": "/custom/context",
      "target": "app",
      "destination": [
        "registry.example.com/app:dev"
      ]
    }
  }
}
```

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

## Warmer Bake Mode

`warmer bake <bakefile> [target]` warms base image caches for targets defined in the same bakefile consumed by `executor bake`. It parses each selected target's Dockerfile, resolves base images, and writes them into OCI-layout cache directories that the executor can read via `--cache-from oci:<dir>`.

```shell
warmer bake /workspace/kaniko-bake.hcl --cache-dir=/workspace/cache
warmer bake /workspace/kaniko-bake.hcl app --cache-dir=/workspace/cache
```

### Cache write location

- If a target has an `oci:`-prefixed `cache_from` entry (e.g. `cache_from = ["oci:/workspace/cache/app", "registry.example.com/app/cache"]`), the warmer writes that target's base images into the directory named by the first `oci:` entry (prefix stripped). Non-`oci:` `cache_from` entries (registry refs) are ignored by the warmer.
- If a target has no `oci:` `cache_from` entry, the warmer writes into the global `--cache-dir` (default `/cache`), shared by all such targets.

### Consistency rule

Across all targets warmed in a single invocation, either **every** target must have an `oci:` `cache_from` entry or **none** may. Mixing per-target `oci:` caches with the shared global `--cache-dir` is not supported and returns an error:

```text
mixing per-target oci caches with shared --cache-dir is not supported
```

The consistency check runs after `--set` overrides are applied and considers only the targets being warmed (so naming a subset that is consistently all-`oci:` or all-shared is allowed).

### `--print` and `--dryrun`

- `--print` resolves targets (after `--set` overrides) and prints JSON including the computed `resolved_cache_dir` for each target, then exits without writing anything.
- `--dryrun` prints a per-target plan (target id, resolved cache dir, and each base image with hit/miss status) and exits without creating directories or writing cache entries.

### Overriding cache location

Use `--set <target>.cache_from=oci:/path` to override a target's write location:

```shell
warmer bake /workspace/kaniko-bake.hcl app \
  --set app.cache_from=oci:/workspace/cache/app-override
```

### Consuming with executor

Warm per-target caches and point executor at the matching directory:

```shell
warmer bake /workspace/kaniko-bake.hcl app --cache-dir=/workspace/cache
executor bake /workspace/kaniko-bake.hcl app -c /workspace \
  --cache-from oci:/workspace/cache/app
```
