# Principles

kaniko runs inside other people's build pipelines, often in places where nothing else is allowed to run. That puts limits on our code that a general purpose program does not have. This document lists the constraints we hold ourselves to and the promises we make to users.

## Constraints

### kaniko runs unprivileged

kaniko must work without any extra privileges. No chroot, no namespaces, no mounts, no capabilities beyond what an ordinary container gets. Nothing we add may require more.

Some of kaniko's design follows directly from this and is not open for discussion. The build runs on a single root filesystem, and snapshots are taken by walking that filesystem. Fixes that rely on isolation or overlay filesystems are out of scope.

### Avoid effects, don't revert them

When an operation would leave an unwanted change on the filesystem, don't perform it. Never perform it and undo it afterwards. Restoring an owner, a mode or a timestamp still leaves a trace, for example a changed ctime, and it costs extra syscalls.

### kaniko is invisible on the filesystem

Reading the filesystem must not change it. kaniko, the snapshotter included, never touches the timestamps of the files it inspects. Every read goes through `util.FSys`, which opens files with `O_NOATIME`, never through `os` directly.

### kaniko keeps its state in its own directory

Everything kaniko writes for itself, the build context, saved stages and staging files included, lives under `config.KanikoDir`, `/kaniko` by default. Nothing goes to `/tmp` or anywhere else on the root filesystem, because everything outside the kaniko directory ends up in the image.

### kaniko is a single binary

kaniko does not call out to other binaries for its own work. Compression, scanning and everything else kaniko does happens inside the kaniko binary. The only processes it starts are the ones a `RUN` instruction asks for. kaniko works inside the filesystem it builds, and a `RUN` step can replace any binary on it. Every call to an external binary trusts a file the build may have taken over.

### Every error is handled

Every error gets a visible branch at the call that produced it. A condition like `if err == nil && ...` that silently drops the other cases is not acceptable, even if a later call would fail on the same path anyway. Duplicate checks are fine. Error handling that looks accidentally left out is not.

### The spec is the reference

kaniko implements the [Dockerfile specification](https://docs.docker.com/reference/dockerfile/). When kaniko and buildkit behave the same, that is a consequence of following the spec, not a goal of its own. User-facing documentation describes what kaniko does and does not compare it with other builders. The README is user-facing documentation, not a specification.

## Promises

### Behavior changes ship behind a feature flag

Changes to kaniko's output or semantics ship behind an `FF_KANIKO_*` flag. Patch releases stay stable. See [releases.md](releases.md) for when a flag is needed, its exceptions, and how it becomes the default.

### Retired flag names are never reused

Once a feature flag is removed, its name is not used again for anything else. A pipeline that still sets an old flag must never get a different behavior from it.

### Behavior fixes do not invalidate caches

A fix does not change cache keys to throw away layers built with the old behavior. Users with an existing cache keep their cache hits.

### `--dryrun` renders the plan only

`--dryrun` shows what kaniko would do. It does not check whether the build would succeed, and it does not contact registries to verify a push.
