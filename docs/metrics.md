# Build metrics

`KANIKO_METRICS_FILE` makes kaniko write one JSON report about the build it just ran: how long each phase took, how many bytes and files went through it, what each command and snapshot cost, and what the process used. It answers one question: of the time a build takes, how much is the workload (`npm ci`, `go build`, ...) and how much is kaniko itself (walking and hashing the filesystem, writing the layer tar, compressing, extracting, talking to the cache and the registry).

It complements [telemetry](telemetry.md), which sends one span per build phase and Dockerfile command to a collector. Metrics need no collector, and they count what a span is too expensive for: per-file work such as hashing is summed into a counter instead of being a span of its own. The two are independent and can be on together.

## Enabling

| Variable | Effect |
|---|---|
| `KANIKO_METRICS_FILE=<path>` | Write the JSON report to `<path>` when kaniko exits, also when the build fails or panics. |
| `KANIKO_PPROF_DIR=<dir>` | Write `cpu.pprof` (the whole run) and `heap.pprof` (at exit) to `<dir>`. |

```shell
docker run --rm \
  -v "$PWD/out:/metrics" \
  -e KANIKO_METRICS_FILE=/metrics/run.json \
  -v "$PWD/ctx:/workspace" \
  ghcr.io/osscontainertools/kaniko:latest --context=/workspace --no-push
```

- Put the files on a **bind mount outside the build rootfs**. The mount point is added to the ignore list like any other, so the report is never part of a layer. The container's own filesystem is gone when kaniko exits.
- Do not put them under the kaniko directory (`/kaniko`, `--kaniko-dir`): it is cleaned and moved during a build.
- Relative paths are made absolute at startup, before kaniko changes into `/`.
- Run at `-v info`. At `-v debug` kaniko logs every file of every snapshot, which distorts the timings.
- With `KANIKO_TELEMETRY_OMIT_DOCKERFILE=true` the command text in the report is redacted as it is in the spans. Build arguments, secrets and registry credentials are never recorded; the `options` block holds the settings that change what a build costs, and `network.hosts` the registries talked to.

## Without the variables

Nothing changes. Starting is a no-op when both variables are empty: no goroutine, no file, no exit handler. Every hook is one atomic load. The layer is not asked for its size and the command is not turned into text unless a report is written, and a panic is not recovered.

## The report

`"schema": "kaniko-metrics/v1"`. Durations are in nanoseconds (`_ns`).

| Key | Content |
|---|---|
| `meta` | kaniko version, Go version, `GOOS`/`GOARCH`, CPUs and `GOMAXPROCS`, start and end time, wall time, active feature flags, the `options` above. |
| `exit` | `status` (`success` or `error`), exit `code`, and the error message, shortened. |
| `phases` | One entry per phase in the table below: `count`, `total_ns`, `max_ns`, and, where the table says so, `bytes` and `items`. Totals of the whole run. |
| `stages`, `commands` | One record per stage and per Dockerfile command: wall time and the `phases` that ran inside it (the difference of the counters between its start and end). A `RUN` also has the usage of its child process: `child_wall_ns`, `child_user_ns`, `child_sys_ns`, `child_maxrss_kb`, `child_inblock`, `child_oublock`, `child_exit_code`, and the time kaniko itself spent around it (`overhead_ns`). |
| `snapshots` | One record per snapshot, with the stage and command it belongs to, `files_added`, `whiteouts` and `tar_bytes`, and its phases. Two builds that snapshot the same files give the same list of these three numbers, which makes it usable to check that a change does not alter what ends up in the layers. |
| `resources` | CPU and memory of kaniko and its children (`getrusage`, `/proc/self/status`, Go memory statistics), and, where cgroup v2 is mounted, the cgroup counters (`cpu.stat`, `memory.peak`, `memory.events`, `io.stat`, ...) at start and at exit. |
| `network` | Per host the connections, requests, bytes read and written, and for the registry the sockets opened, reused and closed, TLS handshakes, and the time spent dialling and idle. |

A build that fails leaves the stage and command that failed in the report, closed at the time of the failure.

## Phases

The phases are fixed names. A phase has a timing when it has a `count`; the ones the table calls "no timing" only count `bytes` and `items`.

| Phase | What it measures |
|---|---|
| `context.fetch` | resolving --context, downloads a remote context |
| `push.permission_check` | CheckPushPermissions before the build |
| `build.total` | DoBuild, once per target |
| `cache.wait` | the build blocked on cache pushes |
| `push.total` | DoPush of the final image |
| `build.extra_stage_digests` | manifests of COPY --from=<image> |
| `cache.lookahead` | cache probe of every stage up front, FF_KANIKO_CACHE_LOOKAHEAD |
| `build.extra_stage_download` | saving and extracting COPY --from=<image> |
| `extra_stage.extract` | extracting one COPY --from=<image> |
| `base.download_shared` | writing a base image to the shared store, FF_KANIKO_SHARED_BASE_CACHE |
| `stage.base_image` | resolving the base image, a manifest for remote ones |
| `stage.optimize` | cache keys and lookups of the stage |
| `stage.build` | running the commands of the stage |
| `stage.finalize` | config, platform and output format of the stage image |
| `stage.save` | writing a stage or COPY --from image as OCI layout |
| `stage.save_files` | copying files later stages depend on |
| `base.unpack` | extracting the base image into the rootfs |
| `fs.delete` | deleting the rootfs between stages and targets |
| `command.key` | files used from the context and the cache key |
| `command.exec` | executing the command, a RUN's child process included |
| `command.cached_layer` | fetching and extracting a cache hit's layer |
| `command.layer` | turning the snapshot or cache hit into the appended layer; bytes: compressed size, items: layers |
| `run.child` | a RUN's child process, start to wait |
| `cache.key` | populateCompositeKey, hashes COPY sources |
| `cache.key_hash` | bytes: context file content hashed, items: files |
| `cache.lookup_hit` | a cache lookup that found a layer |
| `cache.lookup_miss` | a cache lookup that found none |
| `cache.extract` | extracting a cached layer into the rootfs |
| `cache.push` | pushLayerToCache, on its own goroutine |
| `cache.push_layer` | compressing the cache layer; bytes: compressed size |
| `cache.push_pointer` | pushing a cache pointer entry |
| `snapshot.init` | Snapshotter.Init, the full scan a stage starts from |
| `snapshot.command` | the snapshot after a command; bytes: tar size, items: paths added and whited out |
| `snapshot.context` | the --preserve-context snapshot; bytes: tar size |
| `snapshot.scan_full` | scanFullFilesystem; items: changed paths before resolving |
| `snapshot.syncfs` | syncfs before a full scan |
| `snapshot.walk` | one WalkFS; items: paths visited. Includes hashing when the walk checks for changes |
| `snapshot.walk_stat` | one GetFSInfoMap (--use-new-run); items: paths visited |
| `snapshot.hash` | no timing; bytes: file content hashed, items: files hashed |
| `snapshot.resolve` | resolving paths, ignore list and layered map updates |
| `snapshot.add` | adding the listed files of a command, hashing them; items: files |
| `snapshot.tar` | writing the layer tar; items: entries without whiteouts |
| `layer.extract` | one layer into a directory; bytes: tar content, items: entries |
| `layer.compress` | tarball.LayerFromFile: gzip and digest, then diffID; bytes: compressed size |
| `layer.convert` | re-encoding a cached layer for the image's media type; bytes: compressed size |
