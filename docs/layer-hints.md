# Layer hints

kaniko logs a hint when a command adds files to its layer that usually do not belong in an image:

```
HINT SnapshotCacheDir: 184.2MB in 1532 files under /root/.cache/pip, use RUN --mount=type=cache or remove it in the same RUN
```

| Hint | Layer contains |
| --- | --- |
| `SnapshotCacheDir` | package manager or build caches, such as `~/.cache/pip`, `~/.npm`, `~/.m2/repository`, `/var/lib/apt/lists` |
| `SnapshotVCSDir` | `.git`, `.hg` or `.svn` directories |

Pass hint names to `KANIKO_IGNORE_HINTS` to silence them:

```sh
KANIKO_IGNORE_HINTS=SnapshotCacheDir,SnapshotVCSDir
```

With telemetry on, each hint is a `kaniko.hint` event on the command span, see [Telemetry attributes](telemetry.md).
