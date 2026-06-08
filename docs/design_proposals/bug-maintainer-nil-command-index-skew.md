# Bug: MAINTAINER instruction causes index skew between stage.Commands and stageBuilder.cmds

## Status
Confirmed latent bug present in `main`. Triggered by any Dockerfile containing a `MAINTAINER` instruction.

## Summary

`GetCommand` in `pkg/commands/commands.go:107` returns `nil, nil` for `MAINTAINER`
(the instruction is deprecated and intentionally skipped). `newStageBuilder` filters these
nil results out, so `stageBuilder.cmds` is shorter than `stage.Commands` whenever a
`MAINTAINER` instruction is present. Any code that indexes into a slice built from
`s.cmds` by position, then reads that slice using a position from `stage.Commands`, will
read the wrong entry or panic with an out-of-bounds access.

## Concrete impact: dryrun cache annotation output

`stageCacheInfo` (`build.go:83`) stores per-command cache keys and hit flags. It is
populated in `optimize` using index `i` over `s.cmds`, then consumed in `RenderStages`
using index `jdx` over `s.Commands`.

Example with `MAINTAINER` at position 0:

```
stage.Commands = [MAINTAINER, RUN touch /a, RUN touch /b]   len=3
stageBuilder.cmds = [RUN touch /a, RUN touch /b]             len=2

ci.cacheKeys[0] = key_for_RUN_touch_a   (i=0 in optimize)
ci.cacheKeys[1] = key_for_RUN_touch_b   (i=1 in optimize)

RenderStages:
  jdx=0  → MAINTAINER  → ci.cacheKeys[0] = key_for_RUN_touch_a  ← wrong command
  jdx=1  → RUN touch /a → ci.cacheKeys[1] = key_for_RUN_touch_b  ← wrong key
  jdx=2  → RUN touch /b → ci.cacheKeys[2]                        ← out-of-bounds panic
```

The panic only occurs when `opts.Cache && FF_KANIKO_CACHE_LOOKAHEAD` are both set.
Without the cache-lookahead feature flag the `cacheInfo` slice is all nils and
`RenderStages` never reads it.

## Root cause

`newStageBuilder` (`build.go:147`) maps `stage.Commands → s.cmds` with a gap wherever
`GetCommand` returns nil:

```go
for _, cmd := range stage.Commands {
    command, err := commands.GetCommand(cmd, ...)
    if command == nil {
        continue          // ← gap introduced here
    }
    s.cmds = append(s.cmds, command)
}
```

There is no record of which position in `stage.Commands` each `s.cmds[i]` came from, so
callers cannot reconstruct the mapping.

## Other potentially affected sites

These sites iterate `stage.Commands` and could diverge from `s.cmds`-indexed data in
future work:

| File | Location | Risk |
|------|-----------|------|
| `build.go:893` | `RenderStages` — `for jdx, c := range s.Commands` | Active: reads `ci.cacheKeys[jdx]` (see above) |
| `build.go:1321` | stage dependency scan — `for _, c := range stage.Commands` | Low: reads only, no parallel `s.cmds` index |
| `build.go:1413` | cross-stage dep resolution — `for _, c := range stage.Commands` | Low: reads only |

## Proposed fix

Track the raw index of each command in `stageBuilder`, then use it when writing into
`stageCacheInfo`.

### 1. Add mapping fields to `stageBuilder`

```go
type stageBuilder struct {
    ...
    cmds        []commands.DockerCommand
    cmdRawIdxs  []int // cmdRawIdxs[i] = index in stage.Commands for s.cmds[i]
    numRawCmds  int   // len(stage.Commands)
    ...
}
```

### 2. Populate them in `newStageBuilder`

```go
s.numRawCmds = len(stage.Commands)
for rawIdx, cmd := range stage.Commands {
    command, err := commands.GetCommand(cmd, ...)
    if command == nil {
        continue
    }
    s.cmds = append(s.cmds, command)
    s.cmdRawIdxs = append(s.cmdRawIdxs, rawIdx)
}
```

### 3. Size and index `stageCacheInfo` by raw position in `optimize`

```go
ci := &stageCacheInfo{
    redirectKeys: make([]string, s.numRawCmds),
    redirectHits: make([]bool, s.numRawCmds),
    cacheKeys:    make([]string, s.numRawCmds),
    cacheHits:    make([]bool, s.numRawCmds),
}
...
for i, command := range s.cmds {
    raw := s.cmdRawIdxs[i]
    ci.cacheKeys[raw] = ck
    ci.redirectKeys[raw] = inferredCK
    ci.redirectHits[raw] = true / false
    ci.cacheHits[raw] = true / false
}
```

`RenderStages` then reads `ci.cacheKeys[jdx]` with `jdx` from `s.Commands` iteration —
which now correctly aligns.

## Test coverage needed

1. **Golden test with MAINTAINER**: add a Dockerfile variant for an existing golden test
   that prepends `MAINTAINER deprecated@example.com` before the first `RUN`. With
   `FF_KANIKO_CACHE_LOOKAHEAD=1` the plan output must be identical to the variant without
   `MAINTAINER` (the instruction is a no-op).

2. **Unit test for `newStageBuilder`**: assert that `cmdRawIdxs` is populated correctly
   and `numRawCmds` equals `len(stage.Commands)` even when one entry is nil.

3. **`optimize` unit test**: verify that `stageCacheInfo` entries land at the correct raw
   index when `s.cmds` is shorter than `stage.Commands`.
