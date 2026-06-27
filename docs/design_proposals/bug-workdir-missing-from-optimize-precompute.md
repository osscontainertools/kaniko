# Bug: WORKDIR not applied during optimize precompute pass

## Status
Known bug present in `main`. Fix should be done separately from the cache-lookahead work.

## Summary

`WorkdirCommand.MetadataOnly()` returns `false` for any path other than `/`, because
WORKDIR has a filesystem side effect (it creates the directory). As a result, `optimize`
never calls `ExecuteCommand` for WORKDIR, so `cfg.WorkingDir` is never updated during
the precompute pass.

## Impact

1. **Wrong cache keys for COPY after WORKDIR.** `populateCompositeKey` calls
   `command.FilesUsedFromContext(&cfg, s.args)`, which resolves relative COPY source
   paths against `cfg.WorkingDir`. If WorkingDir is stale (empty or from the base
   image), relative paths resolve incorrectly and the computed cache key differs from
   the one produced during the actual build.

2. **Wrong `stageFinalConfigs` for locally-stored stages.** The config propagated to a
   dependent stage via `stageFinalConfigs` carries a stale `WorkingDir`, so any
   command in that stage that resolves paths relative to the working directory (e.g.
   a subsequent COPY) will use the wrong base path when computing its cache key.

## Root cause

`WorkdirCommand.ExecuteCommand` does two things:
1. Resolves and sets `config.WorkingDir` — a pure config mutation, safe anywhere.
2. Creates the directory on the filesystem via `mkdirAllWithPermissions` — a side
   effect that must not run during a precompute pass (the rootfs is not yet unpacked).

Because both concerns live in the same method, and because `MetadataOnly()` is the
only signal available to `optimize`, WORKDIR is excluded from the precompute entirely.

## Proposed fix

Split the two concerns by introducing a new interface method (e.g. `ApplyConfig`) that
performs only the config mutation without any filesystem effects. `optimize` calls
`ApplyConfig` for every command, while the actual build calls `ExecuteCommand` as
before.

```go
// ApplyConfig updates v1.Config to reflect this command's effect on image metadata
// (e.g. WorkingDir, Env, Labels). It must not touch the filesystem.
// The default implementation in BaseCommand is a no-op.
ApplyConfig(config *v1.Config, buildArgs *dockerfile.BuildArgs) error
```

Commands that currently return `MetadataOnly() == true` (ARG, ENV, LABEL, USER, EXPOSE,
VOLUME, CMD, ENTRYPOINT, SHELL, STOPSIGNAL) can delegate `ApplyConfig` to their
existing `ExecuteCommand` since they have no filesystem effects.

`WorkdirCommand.ApplyConfig` would be:

```go
func (w *WorkdirCommand) ApplyConfig(config *v1.Config, buildArgs *dockerfile.BuildArgs) error {
    replacementEnvs := buildArgs.ReplacementEnvs(config.Env)
    resolved, err := util.ResolveEnvironmentReplacement(w.cmd.Path, replacementEnvs, true)
    if err != nil {
        return err
    }
    config.WorkingDir = ToAbsPath(resolved, config.WorkingDir)
    return nil
}
```

`optimize` would then replace the `if command.MetadataOnly()` check with
`command.ApplyConfig(&cfg, s.args)`.
