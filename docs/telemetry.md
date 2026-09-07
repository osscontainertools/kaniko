# Telemetry attributes

kaniko can export an OpenTelemetry trace of each build. It is off by default and enabled by pointing it at an OTLP collector:

```sh
KANIKO_TELEMETRY_ENDPOINT=http://otel-collector:4318
```

Spans are sent over OTLP/HTTP (`http://` or `https://`, collector port 4318 by default). OTLP/gRPC (port 4317) is not supported. The endpoint URL must include a scheme. `OTEL_EXPORTER_OTLP_HEADERS` authenticates to the collector and `OTEL_RESOURCE_ATTRIBUTES` adds fleet labels such as `tenant`, `repo`, and `git.sha`.

Each build is one trace: a root `build` span, a `Stage` span per build stage, and under each stage a span per build phase and Dockerfile command. Stage and command spans are named `Stage` and `Command` (low cardinality, so backends can aggregate on the name). The full instruction text is in the `kaniko.command` attribute. The build phases keep their descriptive names.

Set `KANIKO_TELEMETRY_OMIT_DOCKERFILE=true` to keep the Dockerfile source out of the trace.

Attribute values are capped at 64 KiB. `OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT` and `OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT` override the cap, including an explicit `-1` for unlimited.

## CI attributes

On GitLab CI and GitHub Actions kaniko reads the predefined variables and emits them itself, so a pipeline does not have to repeat them in `OTEL_RESOURCE_ATTRIBUTES`. Detection is `GITLAB_CI` / `GITHUB_ACTIONS`; elsewhere none of this is emitted.

| Attribute | GitLab | GitHub Actions |
| --- | --- | --- |
| `repo` | `CI_PROJECT_PATH` | `GITHUB_REPOSITORY` |
| `ci.pipeline` | `CI_PIPELINE_ID` | `GITHUB_RUN_ID` |
| `git.sha` | `CI_COMMIT_SHA` | `GITHUB_SHA` |
| `git.ref` | `CI_COMMIT_REF_NAME` | `GITHUB_HEAD_REF`, else `GITHUB_REF_NAME` |
| `vcs.repository.url.full` | `CI_PROJECT_URL` | `GITHUB_SERVER_URL` + `GITHUB_REPOSITORY` |
| `vcs.repository.name` | `CI_PROJECT_PATH` | `GITHUB_REPOSITORY` |
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

`OTEL_RESOURCE_ATTRIBUTES` has the last word: anything it sets overrides what kaniko read off the CI system, and it remains the way to label a build outside these two forges.

Never put a tenant, customer or account identifier here. A multi-tenant collector derives that from the CI credential it verified and discards what the build sent.

## Build span

| Attribute | Value |
| --- | --- |
| `kaniko.version` | kaniko version |
| `kaniko.dockerfile` | Dockerfile path |
| `kaniko.dockerfile.content` | full Dockerfile source (absent for URL Dockerfiles) |
| `kaniko.plan` | build plan, the text `--dryrun` would print |
| `kaniko.target` | build target(s), comma-joined |
| `kaniko.build_id` | groups runs of the same build. In CI: sha256 of the job's identity + target — project and job name on GitLab, repository, workflow and job on GitHub — so it survives commits and Dockerfile edits. Outside CI, or when those variables are incomplete: sha256 of Dockerfile content + target, falling back to the path when the Dockerfile is unreadable. `KANIKO_TELEMETRY_BUILD_ID` overrides all of it |
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
