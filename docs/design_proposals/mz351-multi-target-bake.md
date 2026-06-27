# mz351: multi-target builds, push each target stage to its own destination

Tracking: discussion mz351.

## Goal of the first step

Build several target stages of one Dockerfile in a single executor run and push each one to its own destination. Same context, same Dockerfile, one global set of build args. The only new capability over what kaniko does today is the per-target destination.

The input is a small JSON bakefile. It is deliberately minimal for this step, but the schema is chosen so we can grow it over time (per-target build args, matrix expansion, per-target context) without a breaking change.

Explicitly out of scope for this step:

- Per-target build args. Build args stay global, exactly as today. The combinatoric case from the discussion (same stage built with `PYTHON_VERSION=3.10` and `3.11`) needs per-target args and is a later step.
- Matrix expansion, interpolation, HCL.

This is one build and one push cycle, not N independent builds.

## What kaniko already does

`--target` already takes a list of stages. `dockerfile.targetStages` resolves them and the build loop in `DoBuild` builds the whole required subgraph in topological order, into the shared rootfs, with the existing per-stage `util.DeleteFilesystem()` resetting the filesystem between stages (`build.go:1270`). Cross-stage dependency files are saved to `KanikoInterStageDepsDir` and restored by later stages. Independent target stages are isolated by that same reset. Nothing new is needed here, and there is no cross-target filesystem contamination to design around.

## The gap

Only one of those stages is ever pushed.

- `pushStage = targetStages[0]` and stages are marked `Push: i == pushStage` (`dockerfile.go:305`, `385`). Exactly one stage is a push stage.
- The build loop sets a single `pushImage` when it hits the push stage (`build.go:1238`).
- The loop returns that single image the moment it reaches the `Final` stage (`build.go:1244`), and `DoBuild` returns one `v1.Image`.
- `DoPush` pushes that one image to every entry in the global `opts.Destinations`.

So every other built target stage is computed and then thrown away unless something downstream consumes it. The first step lifts exactly this: let every selected target stage carry its own destination and get pushed.

## Input format and invocation

A new `bake` subcommand, alongside the existing `push` and `login` subcommands, takes a JSON bakefile and the name of the target to build, both positional. The target name may be omitted when the bakefile defines exactly one target.

```
/kaniko/executor bake bake.json app --context . --dockerfile Dockerfile --build-arg FOO=bar
```

The bakefile may define several targets; selection is positional, in the style of `docker buildx bake <target>`. Building one selected target at a time is the current step; building several in a single pass is the follow-up. The bakefile supplies each target's stage and destination. Everything else stays on the existing flags and applies globally: `--context`, `--dockerfile`, `--build-arg`, cache, registry, platform, secrets. The `bake` command registers the shared build flags (`addSharedBuildFlags`) but not `--target` or `--destination`, which the bakefile owns (see "CLI flags and the bakefile" below).

The root executor command is left untouched. Single-target builds keep using `executor --destination ...` exactly as today.

Step-one schema:

```json
{
  "version": "1",
  "targets": {
    "app": {
      "target": "app",
      "destination": ["registry.example.com/app:latest"]
    },
    "tools": {
      "target": "tools",
      "destination": ["registry.example.com/tools:latest"]
    }
  }
}
```

- `version` is a string. Present from day one so the parser can reject or adapt to later schema revisions.
- `targets` is a map keyed by an arbitrary target id.
- `target` is the Dockerfile stage to build. Optional, defaults to the map key when omitted.
- `destination` is a list of image references the built stage is pushed to. A target with no destination is an error unless `--no-push` is set.

### How the format is meant to grow

The schema is shaped so future steps are additive, never breaking:

- Per-target build args: add an optional `buildArgs` object to a target. It layers over the global `--build-arg` set, target keys winning. Same merge model for future `labels` and `annotations`.
- Per-target context or dockerfile: add optional `context` and `dockerfile` to a target, falling back to the global flag.
- Matrix expansion: add an optional `matrix` block to a target that expands into several concrete targets at parse time, each resolving to the flat per-target form above. The flat list stays the compilation target, so matrix is pure front-end sugar.
- Top-level shared defaults: a future `version` bump can promote `context` and `dockerfile` into the file itself as top-level keys that targets inherit, for users who want the build fully described by the file rather than split across flags.

The invariant behind all of this: a target is resolved by layering its own fields over a shared base (flags today, top-level file keys later), and the build consumes a flat list of fully resolved targets. Step one ships the flat list with only `target` and `destination` populated. Every later feature adds optional fields or a pre-expansion pass, and old bakefiles keep parsing.

## Design

### 1. Parse the bakefile into resolved targets

A new small package parses the JSON into a slice of resolved targets, each carrying a stage name and a destination list, through a standalone function (roughly `bake.Parse(path) ([]Target, error)`). It must not live inside the cobra `RunE`, because both the `bake` subcommand and the golden test harness call it. For step one resolution is trivial: stage name and destinations straight from the file, everything else from the global flags. This is the seam where per-target overrides and matrix expansion slot in later.

### 1b. The bake subcommand and shared build setup

The `bake` subcommand follows the existing `push` subcommand pattern: its own `*config.KanikoOptions`, `AddKanikoOptionsFlags(bakeCmd, opts)` for the global flags, a positional bakefile path, and a `RunE` that parses the file, runs one `DoBuild`, and pushes each result.

The catch is that the root executor's pre-build setup (`ValidateFlags`, `resolveSecrets`, `moveKanikoDir`, `resolveEnvironmentBuildArgs`, `resolveSourceContext`, `resolveDockerfilePath`, ignore-list setup) currently lives in `RootCmd.PersistentPreRunE` behind a `cmd.Use == "executor"` guard, so a subcommand does not get it. This setup must be extracted into a shared helper that both the executor `Run` and the `bake` `RunE` call. That refactor is a prerequisite and worth landing on its own.

### 2. Mark every target as a push stage

In `dockerfile.go`, drop the "first target is the push stage" rule and mark every selected target as a push stage. `Final` stays the highest-index stage so the loop still has a definite last stage to stop on. The set of push stages becomes "all targets" rather than "targets[0]". Each push stage is associated with its destination list, either stored on `KanikoStage` or kept in a `map[stageIndex][]destination` threaded through `DoBuild`. A map keeps `KanikoStage` free of push concerns and is easy to hand to the push phase.

### 3. Collect multiple images in the build loop

Replace the single `pushImage` with an accumulator. At each push stage, run the existing finalization (`mutate.CreatedAt`, reproducible canonicalization, labels, annotations) and append `{image, destinations}` to the result set. Each push image is captured during its own stage iteration, before that stage's `DeleteFilesystem`, so capturing is safe.

The early `return` at `Final` becomes a return of the accumulated set. Because `Final` is the highest-index target and is itself now a push stage, every push image has already been captured by the time the loop reaches it.

### 4. DoBuild and DoPush signatures

`DoBuild` returns a slice instead of one image, roughly:

```go
type BuiltImage struct {
    Image        v1.Image
    Destinations []string
}

func DoBuild(opts *config.KanikoOptions) ([]BuiltImage, error)
```

`RootCmd.Run` iterates the result and calls push per image. `DoPush` is refactored so its core pushes one image to one destination set, with the per-target destinations replacing the global `opts.Destinations` lookup. The no-push, tar-path, and oci-layout paths extend to "once per built image". The single-target path is just a slice of length one, so the common case stays unchanged in behaviour.

## What stays global

Build args, cache settings, registry options, platform, secrets. These are process-wide flags and do not move into the bakefile in this step. Labels and annotations are applied during push-stage finalization today and would apply to every pushed image for now. Per-target labels, annotations, and build args are the documented next step.

## CLI flags and the bakefile

`bake` is its own interface, not a backward-compatible wrapper over `executor`/`build`. That decision removes a whole class of complexity up front: a setting lives in exactly one place, never both, so there is no global-flag-versus-bakefile merge behaviour to define and no precedence to reason about.

The migration is one-directional. A build setting is a plain `bake` CLI flag only until it is added to the bakefile schema; once it is in the schema, it lives in the bakefile and its flag is removed from `bake`:

- Not yet in the schema: a normal `bake` flag, applied globally to the build (transitional).
- In the schema: bakefile-only, the flag is gone.

`target` and `destination` are on the bakefile side from day one. As `buildArgs`, `labels`, `platform`, and the rest land in the schema, their flags (`--build-arg`, `--label`, ...) leave `bake`. Because a setting is never in both places, `destination` and `build-arg` need no different treatment; they simply migrate at different times.

Overriding a bakefile value from the command line is still supported, through a targeted override in the style of docker bake's `--set`:

```
/kaniko/executor bake bake.json --set app.destination=registry/app:dev
```

`--set <target>.<field>=<value>` names the target, so it stays unambiguous with many targets. It is the single override channel; plain build flags are never an override path. This need not land in the first step (the only bakefile-owned settings are `target`/`destination`, so there is little to override yet), but it is the intended shape, and we should not build a competing global-flag override.

Because settings are never duplicated across CLI and bakefile, there is also no reason to gate which CLI flags are accepted on the bakefile `version`. `version` governs how the bakefile is interpreted, never the CLI surface.

One plain validation remains, independent of all the above: destinations must be unique across the resolved targets, so two targets pushing to the same ref fails loudly rather than silently clobbering.

## Output files

`--digest-file`, `--image-name-with-digest-file`, and `--image-name-tag-with-digest-file` are single-valued and cannot name N images. First cut: reject them when a bakefile produces more than one target, with a clear error. A per-target or directory-based output is a follow-up.

## Testing

Two layers.

End-to-end golden plan tests reuse the existing `golden` harness. That harness already drives builds through real CLI flags plus an on-disk `Dockerfile`, runs `DoBuild --dryrun`, and diffs the rendered plan against `plans/<name>`. Bake gets its own test function rather than being folded into `TestRun`, because the input model differs: `TestRun` is "Dockerfile plus flags", bake is "Dockerfile plus bakefile plus optional `--target` subset". A new `TestBake` keeps each convention clean and carries the bake-specific cases (target-subset selection, multiple destinations per target, missing destination). The shared part, run `DoBuild --dryrun`, capture the output, and diff-or-update against the plan path, is extracted into a helper that both `TestRun` and `TestBake` call, so the compare and `-update` logic is not duplicated.

`TestBake` differs only in setup. The bakefile is one more on-disk input next to the `Dockerfile` in the testdata directory. The harness reads it through the same `bake.Parse` the subcommand uses, populates the per-target destinations on `opts`, then calls the shared helper. The bakefile lives as a file, not a struct, so the real parser is exercised and new schema features are covered by adding a fixture and re-rendering with `-update`, no Go changes.

For this to be meaningful, the dryrun plan (`RenderStages`) must emit a `PUSH <destination>` line at each push stage. Today it renders the stage graph with no push information (see existing plans, which end at the final stage with no `PUSH` line). Adding per-stage push lines makes the golden file capture the stage-to-destination mapping, which is the behaviour under test.

The bakefile parser and resolver get their own narrow unit test in the bake package, table-driven, with the JSON as an inline string going in and resolved targets coming out. This is where a struct or inline literal is the right tool, pinning the parse boundary directly: `target` defaulting to the map key, missing-destination errors, unknown `version`, malformed JSON. Keep it separate from the golden plan tests.

## Why not N build-and-push cycles

An earlier sketch of this proposal ran one `DoBuild` per target with a filesystem reset between them. That was the wrong shape. The build loop already builds the full multi-target subgraph in one pass with correct inter-stage resets, and shared ancestor stages are built once in-process rather than recomputed or replayed from cache per target. Building once and pushing many reuses all of that. The only real work is letting more than one stage be a push target and letting `DoBuild` hand back more than one image.

## Open questions

- Whether push-permission checks (`CheckPushPermissions`, run before the build in `Run`) should validate all target destinations up front so a typo in one target fails before any build work. Leaning yes.
- Parallel pushing of the finished images is a cheap later win and does not affect this design.
