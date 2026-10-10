# Layer hints

| Hint | Layer contains |
| --- | --- |
| `SnapshotCacheDir` | package manager or build caches, such as `~/.cache/pip`, `~/.npm`, `~/.m2/repository`, `/var/lib/apt/lists` |
| `SnapshotVCSDir` | `.git`, `.hg` or `.svn` directories |

With telemetry on, each hint is a `kaniko.hint` event on the command span, see [Telemetry attributes](telemetry.md).
