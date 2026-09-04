# Telemetry attributes

kaniko can export an OpenTelemetry trace of each build. It is off by default and enabled by pointing it at an OTLP collector:

```sh
KANIKO_TELEMETRY_ENDPOINT=http://otel-collector:4318
```

Spans are sent over OTLP/HTTP (`http://` or `https://`, collector port 4318 by default). OTLP/gRPC (port 4317) is not supported. The endpoint URL must include a scheme. `OTEL_EXPORTER_OTLP_HEADERS` authenticates to the collector and `OTEL_RESOURCE_ATTRIBUTES` adds fleet labels such as `tenant`, `repo`, and `git.sha`.

Each build is one trace: a root `build` span, a `Stage` span per build stage, and under each stage a span per build phase and Dockerfile command. Stage and command spans are named `Stage` and `Command` (low cardinality, so backends can aggregate on the name). The full instruction text is in the `kaniko.command` attribute. The build phases keep their descriptive names.

Set `KANIKO_TELEMETRY_OMIT_DOCKERFILE=true` to keep the Dockerfile source out of the trace.

Attribute values are capped at 64 KiB. `OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT` and `OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT` override the cap, including an explicit `-1` for unlimited.

## Authenticating to the collector

A collector that requires a credential can be given one two ways.

`OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer <token>` sends a token the job already holds. It always wins over the exchange below.

Or kaniko trades the job's CI identity token for one, so no token has to be stored in the repository:

```sh
KANIKO_TELEMETRY_EXCHANGE_URL=https://<backend>/ingest/token
```

Nothing is exchanged unless that URL is set. A job may hold an identity token for reasons that have nothing to do with telemetry, and kaniko does not offer a credential to a host that did not ask for one. The identity token is looked for in order:

| Source | Where it comes from |
| --- | --- |
| `KANIKO_TELEMETRY_ID_TOKEN` | any CI system that can put a token in the environment, such as GitLab `id_tokens:` |
| `KANIKO_TELEMETRY_ID_TOKEN_FILE` | the same token in a file, such as a Kubernetes projected service-account token |
| `ACTIONS_ID_TOKEN_REQUEST_URL` and `ACTIONS_ID_TOKEN_REQUEST_TOKEN` | GitHub Actions, which hands out a request URL rather than a token; needs `permissions: id-token: write` |

A token already in the environment wins over one kaniko has to go ask for. A source that is configured but fails ends the search rather than falling through to the next one.

kaniko asks for the audience `kaniko-telemetry`, overridable with `KANIKO_TELEMETRY_AUDIENCE`. Where the pipeline sets the audience itself (GitLab's `aud:`), a mismatch is reported rather than left as a bare 401.

A refusal logs `ingest token exchange refused` and the build continues without telemetry. The exchange is bounded at 10s. The identity token is sent only over `https` (loopback excepted, for local development), is never carried across a redirect, and neither token is logged.

## Build span

| Attribute | Value |
| --- | --- |
| `kaniko.version` | kaniko version |
| `kaniko.telemetry.auth` | how the exporter authenticated: `exchange`, `env` or `none` |
| `kaniko.dockerfile` | Dockerfile path |
| `kaniko.dockerfile.content` | full Dockerfile source (absent for URL Dockerfiles) |
| `kaniko.plan` | build plan, the text `--dryrun` would print |
| `kaniko.target` | build target(s), comma-joined |
| `kaniko.build_id` | sha256 of Dockerfile content + target, for grouping runs of the same build (falls back to the path when the Dockerfile is unreadable) |
| `kaniko.ff.*` | explicitly-set `FF_KANIKO_*` feature flags (flags left at their defaults are not reported) |
| `service.name` | `kaniko`, unless `OTEL_SERVICE_NAME` is set |
| `kaniko.registry.sockets.opened` | TCP connections the build made to registries |
| `kaniko.registry.sockets.closed` | how many of those were closed before the build ended |
| `kaniko.registry.sockets.open_at_exit` | connections still open when the build ended |
| `kaniko.registry.sockets.peak` | highest number open at the same time |
| `kaniko.registry.requests` | HTTP requests to registries |
| `kaniko.registry.requests.reused` | how many of those reused a connection |
| `kaniko.registry.tls.handshakes` | TLS handshakes |
| `kaniko.registry.tls.ms` | time those handshakes took |
| `kaniko.registry.dial.ms` | time spent opening connections |
| `kaniko.registry.idle.ms` | total time connections sat idle before being reused |

## Stage spans

| Attribute | Value |
| --- | --- |
| `kaniko.stage` | stage index (integer) |
| `kaniko.stage.name` | stage name from `FROM ... AS <name>`, empty for unnamed stages |

## Command spans

| Attribute | Value |
| --- | --- |
| `kaniko.command` | full instruction text |
| `kaniko.command.hash` | hash of the stage index and command text |
| `kaniko.instruction.index` | command index within the stage |
| `kaniko.instruction.line` | source line in the Dockerfile |
| `kaniko.stage` | stage index (integer) |
| `kaniko.cache.hit` | `true` when the command was replayed from cache (only with `--cache`, absent when caching is off) |
| `kaniko.cache.key` | cache key for the command (only with `--cache`) |

## Phases

`kaniko.phase` is `network`, `build` or `kaniko`, and follows the span name.
