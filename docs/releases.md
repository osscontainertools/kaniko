# Release Policy

## Cadence

kaniko ships on a fixed schedule, independent of what has been merged:

- **Patch release** (`1.X.Y`) every two weeks: bug fixes only, no behaviour changes.
- **Minor release** (`1.X.0`) roughly every three months: new behaviour is graduated from feature flags to defaults, and old flags are removed.

The version number is decided in advance. A patch release will never contain behaviour changes, regardless of what is available in the codebase. This means users can safely bump a patch version knowing nothing will behave differently.

## Feature flags

Because minor releases are infrequent, new behaviour ships immediately behind a [feature flag](../README.md#feature-flags) rather than waiting for the next minor cycle. This gives users who want to try the feature early a way to opt-in, while everyone else gets the usual patch-release stability guarantee.

The graduation lifecycle of a feature flag is:

1. **Introduced**: flag defaults to `false`, behaviour is opt-in. Ships in the next patch release.
2. **Becomes default**: flag defaults to `true` in the next minor release. Existing opt-in users are unaffected.
3. **Deprecated**: flag is removed in a subsequent minor release.

This means a new behaviour is opt-in for at least two weeks and up to three months, then opt-out for a further three months before the flag disappears entirely. Users have between three and six months to react to any change. We expect most users to upgrade roughly once per quarter rather than every two weeks, so in practice the full window is available to them.

- `FF_KANIKO_*` gates a new feature, `true` enables it.
- `FF_KANIKO_DEPRECATE_*` gates the removal of existing behaviour, `true` removes it.

## Do I need a feature flag?

The goal of a feature flag is to let users upgrade kaniko safely without surprises. Any change that could affect whether a build succeeds, what image it produces, or how long it takes should be gated behind a flag, even if the change is correct, even if it is a performance improvement, and even if the previous behaviour was technically wrong. Some users will have built workflows around the old behaviour and need time to migrate. Others will be running kaniko in environments where a new code path fails in ways the old one did not. A feature flag gives them the escape hatch.

The only exception is a fix for behaviour that was so broken that no working workflow could have depended on it: kaniko was erroring out, crashing, or producing corrupt images. There is no stable behaviour to protect in that case, and making users wait three months for a minor release would cause more harm than the fix itself.

There is a greyzone where the behaviour is genuinely new and a workflow might exist, but it would harm the majority of the users to wait, a security patch for example. A compromise is to skip the opt-in phase and release the feature flag as default `true` from the start. This way, if the behaviour does break someone's workflow they at least have an escape hatch and can disable the feature.

When in doubt, open an issue and ask.

## Security fixes

We do not cut special releases to address CVEs. Security fixes are committed to `main` immediately and ship as part of the normal release cycle. If you cannot wait for the next scheduled release, images built from `main` are available as `ghcr.io/osscontainertools/kaniko-dev`, tagged with the commit hash, and go through the same automated testing as tagged releases.
