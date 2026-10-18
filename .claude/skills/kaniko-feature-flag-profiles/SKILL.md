---
name: kaniko-feature-flag-profiles
description: Sync kaniko feature flag profiles (preview, buildkit). Use when a FF_KANIKO_* flag is added, removed or changes default, when a README "Becomes default in" line changes, when config.Profiles is edited, or at a minor release flag graduation.
model: haiku
effort: low
---

# Feature flag profiles

Source: `config.Profiles` in `pkg/config/featureflags.go`.

## Rules

- `preview`: flags with README "Becomes default in `<next minor>`".
- `buildkit`: default-`false` flags for BuildKit-compliant behavior.
- Drop a flag from both once its default is `true`.
- Sort alphabetically.

## Steps

1. Edit `config.Profiles`.
2. Mirror it in the `README.md` `sh` blocks under `##### Preview` and `##### BuildKit compatibility`, one `FLAG=true` per line.
3. Keep the example flag in the README `#### Flag --profile` section a member of a profile.
4. New or renamed profile: update the `--profile` help text in `cmd/executor/cmd/root.go`.
5. Run:

```bash
go test ./pkg/config/
make golden
```
