# Telemetry attributes

kaniko can export an OpenTelemetry trace of each build. It is off by default and enabled by pointing it at an OTLP collector:

```sh
KANIKO_TELEMETRY_ENDPOINT=http://otel-collector:4318
```

Spans are sent over OTLP/HTTP (`http://` or `https://`, collector port 4318 by default). OTLP/gRPC (port 4317) is not supported. The endpoint URL must include a scheme. `OTEL_EXPORTER_OTLP_HEADERS` authenticates to the collector and `OTEL_RESOURCE_ATTRIBUTES` adds fleet labels of your own.

Each build is one trace: a root `build` span, a `Stage` span per build stage, and under each stage a span per build phase and Dockerfile command. Stage and command spans are named `Stage` and `Command` (low cardinality, so backends can aggregate on the name). The full instruction text is in the `kaniko.command` attribute. The build phases keep their descriptive names.

Attribute values are capped at 64 KiB. `OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT` and `OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT` override the cap, including an explicit `-1` for unlimited.

## What leaves the machine

Traces carry build details unredacted. The [build](#build-span), [stage](#stage-spans) and [command](#command-spans) span tables list every attribute.

Sent by default:

- the kaniko version, Dockerfile path, build targets and stage names
- the full Dockerfile source and the build plan
- the text of every instruction
- the `.dockerignore` the build applied
- layer hints, including the paths they name
- cache keys
- the values of explicitly set `FF_KANIKO_*` flags
- timings per phase and per command, and registry connection statistics
- on CI: repository, branch, commit and pipeline, see [CI attributes](#ci-attributes)

Hidden with `KANIKO_TELEMETRY_OMIT_DOCKERFILE=true`:

- the full Dockerfile source
- the build plan

Never sent: the value behind a `RUN --mount=type=secret` and the contents of a `--mount=type=cache`.

If your Dockerfile or `RUN` commands contain credentials, treat the collector as part of your secret boundary.

## Authenticating to the collector

Send a token the job already holds:

```sh
OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer <token>
```

Or have kaniko trade the job's CI identity token for one, so no token is stored in the repository:

```sh
KANIKO_TELEMETRY_TOKEN_EXCHANGE_ENDPOINT=https://<backend>/ingest/token
```

`OTEL_EXPORTER_OTLP_HEADERS` wins if both are set. Nothing is exchanged unless the exchange endpoint is set. The identity token is looked for in order:

| Source | Where it comes from |
| --- | --- |
| `KANIKO_TELEMETRY_ID_TOKEN` | any CI system that exports a token, such as GitLab `id_tokens:` |
| `KANIKO_TELEMETRY_ID_TOKEN_FILE` | the same token in a file, such as a Kubernetes projected service-account token |

A source that is configured but fails ends the search rather than falling through to the next one.

On GitLab, declare the token on the job and it is exported for you:

```yaml
build:
  id_tokens:
    KANIKO_TELEMETRY_ID_TOKEN:
      aud: kaniko-telemetry
```

GitHub Actions hands the job a request URL rather than a token, so mint it with `permissions: id-token: write` in the step before the build. The token expires five minutes after it is minted:

```yaml
- run: |
    token=$(curl -sf -H "Authorization: Bearer $ACTIONS_ID_TOKEN_REQUEST_TOKEN" \
      "$ACTIONS_ID_TOKEN_REQUEST_URL&audience=kaniko-telemetry" | jq -er .value)
    echo "::add-mask::$token"
    echo "KANIKO_TELEMETRY_ID_TOKEN=$token" >> "$GITHUB_ENV"
```

A refusal logs `ingest token exchange refused` and the build continues without telemetry. Both endpoints have to be `https`, loopback excepted for local development.

## CI attributes

kaniko reads the predefined variables of the CI system it runs on and emits them itself, so a pipeline does not have to repeat them in `OTEL_RESOURCE_ATTRIBUTES`.

| Attribute | GitLab | GitHub Actions |
| --- | --- | --- |
| `repo` | `CI_PROJECT_PATH` | `GITHUB_REPOSITORY` |
| `ci.pipeline` | `CI_PIPELINE_ID` | `GITHUB_RUN_ID` |
| `git.sha` | `CI_COMMIT_SHA` | `GITHUB_SHA` |
| `git.ref` | `CI_COMMIT_REF_NAME` | `GITHUB_HEAD_REF`, else `GITHUB_REF_NAME` |
| `vcs.repository.url.full` | `CI_PROJECT_URL` | `GITHUB_SERVER_URL` + `GITHUB_REPOSITORY` |
| `vcs.repository.name` | `CI_PROJECT_NAME` | `GITHUB_REPOSITORY`, without the organization |
| `vcs.ref.head.name` | `CI_COMMIT_REF_NAME` | `GITHUB_HEAD_REF`, else `GITHUB_REF_NAME` |
| `vcs.ref.head.revision` | `CI_COMMIT_SHA` | `GITHUB_SHA` |
| `vcs.change.id` | `CI_MERGE_REQUEST_IID` | pull request number, from `GITHUB_REF_NAME` |
| `cicd.pipeline.name` | `CI_PIPELINE_NAME` | `GITHUB_WORKFLOW` |
| `cicd.pipeline.run.id` | `CI_PIPELINE_ID` | `GITHUB_RUN_ID` |
| `cicd.pipeline.run.url.full` | `CI_PIPELINE_URL` | constructed, including `GITHUB_RUN_ATTEMPT` past the first |
| `cicd.pipeline.task.name` | `CI_JOB_NAME` | `GITHUB_JOB` |
| `cicd.pipeline.task.run.id` | `CI_JOB_ID` | — |
| `cicd.pipeline.task.run.url.full` | `CI_JOB_URL` | — |
| `kaniko.ci` | `gitlab` | `github` |
| `kaniko.ci.run_attempt` | — | `GITHUB_RUN_ATTEMPT` |

An absent variable is an absent attribute, never an empty one. `repo`, `ci.pipeline`, `git.sha` and `git.ref` are kept alongside their `vcs.*` and `cicd.*` equivalents because consumers order on them.

`OTEL_RESOURCE_ATTRIBUTES` has the last word: anything it sets overrides what kaniko read off the CI system.

Never put a tenant, customer or account identifier here. A multi-tenant collector derives that from the CI credential it verified and discards what the build sent.

## Build span

| Attribute | Value |
| --- | --- |
| `kaniko.version` | kaniko version |
| `kaniko.telemetry.auth` | how the exporter authenticated: `exchange`, `env` or `none` |
| `kaniko.dockerfile` | Dockerfile path |
| `kaniko.dockerfile.content` | full Dockerfile source (absent for URL Dockerfiles) |
| `kaniko.dockerignore.content` | the applied `.dockerignore`, resolved as `<dockerfile>.dockerignore` then `<context>/.dockerignore` |
| `kaniko.plan` | build plan, the text `--dryrun` would print |
| `kaniko.target` | build target(s), comma-joined |
| `kaniko.build_id` | groups runs of the same build. In CI: sha256 of the job's identity + target — project and job name on GitLab, repository, workflow file and job on GitHub — so it survives commits and Dockerfile edits. Outside CI, or when those variables are incomplete: sha256 of Dockerfile content + target, falling back to the path when the Dockerfile is unreadable. `KANIKO_TELEMETRY_BUILD_ID` overrides all of it |
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

Each [layer hint](../README.md#layer-hints) is a `kaniko.hint` event on the command span, with `kaniko.hint.rule` and `kaniko.hint.message`.

## Phases

`kaniko.phase` is `network`, `build` or `kaniko`, and follows the span name.
