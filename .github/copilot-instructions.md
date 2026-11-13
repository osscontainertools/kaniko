# Kaniko code review instructions

This file guides reviews of pull requests against the kaniko repository
(osscontainertools/kaniko). It reflects the project's current conventions as
described in `CONTRIBUTING.md`, `DEVELOPMENT.md`, `docs/releases.md`,
`docs/testplan.md`, and `.github/pull_request_template.md`, plus patterns
observed in recently merged commits.

## When a feature flag is needed

New behaviour that could affect whether a build succeeds, what image it
produces, or how long it takes must ship behind a feature flag, even when the
change is correct or purely a performance improvement. The goal is to let users
upgrade kaniko safely without surprises.

- Gate new behaviour behind `FF_KANIKO_*`. `true` enables the new behaviour.
- Gate removal of existing behaviour behind `FF_KANIKO_DEPRECATE_*`.
- Register every flag in `pkg/config/featureflags.go` via
  `featureFlag("FF_KANIKO_NAME", def)` and add a matching `FeatureFlags` field.
  Init asserts every field is registered exactly once and every registration has
  a field, so do not add one without the other.
- Default `false` for opt-in behaviour: ships in the next patch release, becomes
  the default (`true`) in the next minor release, then is removed in a later
  minor release.
- Default `true` is allowed only in the greyzone (genuinely new behaviour that
  would harm most users to wait for, e.g. a security patch); users still get an
  escape hatch via `FF_KANIKO_*=0`.
- No feature flag is required only when the previous behaviour was so broken
  that no working workflow could have depended on it (kaniko errored, crashed,
  or produced corrupt images).
- Reference `docs/releases.md` ("Do I need a feature flag?") when in doubt.

Read feature flags in code as `config.FF.FlagName` (e.g. `config.FF.RunMountTmpfs`).
Do not read `os.Getenv` directly for feature flags; the config package owns the
registry, the default/opt-in/opt-out tracking, and the unknown-flag warning.

### Feature flag description wording

Every new flag gets a README entry under `## Feature flags` (mirroring the
existing entries at `README.md:1284+`). Follow this structure:

```
#### Flag `FF_KANIKO_NAME`

<One sentence describing the current default behaviour and what it affects.>
Set this flag to `true` to <what the new behaviour does>.
Defaults to `true`/`false`.
<Will be deprecated in `vX.Y.Z`. | Becomes default in `vX.Y.Z`. | Currently no plans to activate.>
```

Conventions observed in `README.md`:

- Lead with the current default behaviour and its impact (what breaks, what
  changes, what is slow), then say what `true` switches on.
- Use backticks for the flag name, Dockerfile directives, and command names.
- Mention who needs to set it (e.g. "on the warmer and the executor") when
  the flag spans multiple components.
- End with the lifecycle line matching the default:
  - default `false` → `Becomes default in vX.Y.Z.` (next minor release) or
    `Currently no plans to activate.`
  - default `true` → `Will be deprecated in vX.Y.Z.` (removal in a later minor)
- For `FF_KANIKO_DEPRECATE_*` flags, describe the old behaviour being removed
  and what `true` rejects instead.

Do not add a description field to `FeatureFlags` in `pkg/config/featureflags.go` —
the struct is bool-only and Init asserts a 1:1 field/registration match. The
description lives only in the README and the PR/commit body.

## Testing patterns

Follow the test pyramid described in `docs/testplan.md` and `DEVELOPMENT.md`.

### Integration tests (primary quality gate)

The integration tests answer: does kaniko produce the same image as docker?
Tests run the same dockerfile with docker and kaniko and compare with
`diffoci` (`integration/`).

- Every bug fix must be accompanied by a new integration test dockerfile that
  reproduces the regression.
- Every change that alters build output (filesystem or metadata) needs a new
  integration test dockerfile in `integration/dockerfiles/`.
- Name dockerfiles `Dockerfile_test_<issue-or-description>`, mirroring existing
  names (e.g. `Dockerfile_test_cache_from_to`, `Dockerfile_test_issue_mz1346`).
- Pin base images by digest where possible (e.g.
  `FROM debian:13.7@sha256:...`) so cached builds stay reproducible.
- Use `ARG IMAGE_REPO` + `FROM ${IMAGE_REPO}busybox` when the test needs a
  controllable base image (fixtures are bumped via dependabot in `deploy/`).
- Run locally: `make integration-test` (or `LOCAL=1 make integration-test` for
  a local registry); filter with
  `DOCKERFILE_PATTERN="Dockerfile_test_*" make integration-test-run`.

### Golden tests (planning logic correctness)

Golden tests verify the build plan kaniko computes (stages, steps, cache keys)
without building anything (`golden/`). Run with `make golden`; update snapshots
with `UPDATE=1 make golden`.

- Add golden tests when a change affects how kaniko plans a build but not the
  final image output (flow optimizations, caching logic, stage squashing,
  skip-unused-stages).
- Each test lives under `golden/testdata/test_issue_mzNNN/` with a
  `plans/` directory holding the expected plan snapshots.
- Follow the existing `types.GoldenTests` structure: `Env`, `Args`, `Plan`.
- Set preview flags via the `previewEnv` map in `golden/golden_test.go` when
  exercising behaviour behind preview feature flags, and call
  `config.InitFeatureFlags()` in each subtest so env takes effect.
- Do not add comments that restate the code (recent merge dropped them).

### Unit tests

Unit tests live next to the code they test (`make test`). They verify the code
behaves as written but are not the primary quality signal. Per
`DEVELOPMENT.md`: "we do not add new ones." Prefer integration or golden tests
for user-facing behaviour changes.

## Pull request checklist (from .github/pull_request_template.md)

- [ ] Adds integration tests if the output changes, or golden tests if the build
  plan changes.
- [ ] The code flow looks good.
- [ ] Integration or golden tests added where appropriate.

## Commit message conventions

Follow `CONTRIBUTING.md`:

- Reference the related issue with a short prefix:
  - `mzNNN` — this repository (osscontainertools/kaniko)
  - `cgNNN` — chainguard fork (chainguard-dev/kaniko)
  - `NNN` (no prefix) — original Google repository (GoogleContainerTools/kaniko)
- For bug fixes, add a body paragraph describing the incorrect behaviour and why
  the fix resolves it.
- Simple changes with no associated issue: a subject line alone is fine.
- Examples of recently merged subjects:
  - `bake: per-target cache_from and cache_to`
  - `RUN tmpfs mount support (#1315)`
  - `block docker hub in cached warmer builds`
  - `build(deps): bump github.com/Azure/azure-sdk-for-go/sdk/azcore`

## Coding standards

- Follow [Go code review comments](https://go.dev/wiki/CodeReviewComments).
- CI runs `staticcheck` (version pinned in `DEVELOPMENT.md`, currently
  `2026.2.2`). Run locally with:
  `go run honnef.co/go/tools/cmd/staticcheck@2026.2.2 ./...`
- Format with `gofmt -s -w` (exclude `vendor/`):
  `find . -name "*.go" | grep -v vendor/ | xargs gofmt -l -s -w`
- Copyright header on new Go files:
  ```
  /*
  Copyright 2026 OSS Container Tools
  ...
  */
  ```
- Repository structure (from `DEVELOPMENT.md`):
  - `pkg/commands/` — Dockerfile command implementations (COPY, RUN, ADD, …)
  - `pkg/executor/` — core build logic (`build.go` is the heart of the build loop)
  - `pkg/snapshot/` — filesystem snapshotting
  - `pkg/dockerfile/` — Dockerfile parsing
  - `pkg/cache/` — remote layer cache lookup
  - `golden/` — golden tests and snapshots
  - `integration/` — integration tests and test dockerfiles

## Review focus

When reviewing a PR, check in this order:

1. Behaviour change present? → feature flag registered in
   `pkg/config/featureflags.go` with `FF_KANIKO_*` name and correct default.
2. Output changed? → new integration test dockerfile in
   `integration/dockerfiles/`.
3. Build plan changed (not output)? → new/updated golden test under
   `golden/testdata/`.
4. Bug fix? → body paragraph in commit message explaining the incorrect
   behaviour and the fix; integration test reproducing the regression.
5. Lint/formatting: `staticcheck` clean, `gofmt -s` applied.
6. Commit message follows the `mzNNN`/`cgNNN`/`NNN` prefix convention.
