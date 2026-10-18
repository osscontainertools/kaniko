---
name: kaniko-feature-flag-profiles
description: Keep kaniko's feature flag profiles (preview, buildkit) in sync across code, README and integration tests. Use whenever a FF_KANIKO_* flag is added, removed or changes its default, when a README flag section gets or changes a "Becomes default in" line, when config.Profiles in pkg/config/featureflags.go is edited, or when flags graduate for a minor release.
model: sonnet
effort: medium
---

# Feature flag profiles

`config.Profiles` in `pkg/config/featureflags.go` is the source. Every other list below mirrors it.

## Membership

- `preview`: every flag whose README section says "Becomes default in `<next minor>`". A flag leaves when its default becomes `true`.
- `buildkit`: every default-`false` flag that switches kaniko to BuildKit-compliant Dockerfile behavior. A flag leaves when its default becomes `true`.
- A flag with "Currently no plans to activate" is in no profile unless it is BuildKit compliance.
- Keep each list sorted alphabetically.

## Steps

1. Update `config.Profiles` per the membership rules.
2. In `README.md`, rewrite the `sh` blocks under `##### Preview` and `##### BuildKit compatibility` as one `FLAG=true` line per profile entry, same order.
3. In the README `#### Flag --profile` section, make sure the example flag in "so `FF_KANIKO_...=false` turns off a single flag" is still in a profile. Swap it for one that is if not.
4. If a profile was added or renamed, update the `--profile` help text in `cmd/executor/cmd/root.go` and the README `--profile` examples.
5. In `integration/images.go`, make sure `KanikoEnv` has `<FLAG>=1` for every flag in both profiles. Keep its `FF_KANIKO_*` entries alphabetical.
6. Golden tests enable `preview` through `config.ApplyProfiles` in `golden/golden_test.go`. Nothing to edit there. A golden case that must run with a preview flag off sets `<FLAG>: "0"` in its own `Env`.
7. Verify:

```bash
go test ./pkg/config/
make golden
```

`InitFeatureFlags` asserts that every profile entry is a registered flag, so a removed flag left in a profile fails the first command.
