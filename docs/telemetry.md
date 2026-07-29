# Telemetry attributes

kaniko can export an OpenTelemetry trace of each build. It is off by default and enabled by pointing it at an OTLP collector:

```sh
KANIKO_TELEMETRY_ENDPOINT=http://otel-collector:4318
```

Spans are sent over OTLP/HTTP (`http://` or `https://`, collector port 4318 by default). OTLP/gRPC (port 4317) is not supported. The endpoint URL must include a scheme. `OTEL_EXPORTER_OTLP_HEADERS` authenticates to the collector and `OTEL_RESOURCE_ATTRIBUTES` adds fleet labels such as `tenant`, `repo`, and `git.sha`.

Each build is one trace: a root `build` span plus a span per build phase and Dockerfile command. Command spans are named `Command` (low cardinality, so backends can aggregate on the name). The full instruction text is in the `kaniko.command` attribute. The build phases keep their descriptive names.

## Build span

| Attribute | Value |
| --- | --- |
| `kaniko.version` | kaniko version |
| `kaniko.dockerfile` | Dockerfile path |
| `kaniko.dockerfile.content` | full Dockerfile source (absent for URL Dockerfiles) |
| `kaniko.target` | build target(s), comma-joined |
| `kaniko.build_id` | sha256 of Dockerfile content + target, for grouping runs of the same build (falls back to the path when the Dockerfile is unreadable) |
| `kaniko.ff.*` | explicitly-set `FF_KANIKO_*` feature flags (flags left at their defaults are not reported) |
| `service.name` | `kaniko` |

## Command spans

| Attribute | Value |
| --- | --- |
| `kaniko.command` | full instruction text |
| `kaniko.command.hash` | hash of the stage index and command text |
| `kaniko.phase` | `kaniko`, `build`, or `network` |
| `kaniko.instruction.index` | command index within the stage |
| `kaniko.instruction.line` | source line in the Dockerfile |
| `kaniko.stage` | stage index (integer) |
| `kaniko.cache.hit` | `true` when the command was replayed from cache (only with `--cache`, absent when caching is off) |
| `kaniko.cache.key` | cache key for the command (only with `--cache`) |
