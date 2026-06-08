# CI: git-context tests always fetch from main on PRs

## Status

Known issue. No fix in place.

## Problem

`getBranchCommitAndURL()` in `integration/integration_test.go` hardcodes `branch = "main"` whenever `GITHUB_HEAD_REF` is set (i.e. whenever the run is triggered by a pull request):

```go
if _, isPR := os.LookupEnv("GITHUB_HEAD_REF"); isPR {
    branch = "main"
}
```

Any test that passes this branch to `DockerGitRepo` or `KanikoGitRepo` as the build context will clone `main` — not the PR branch — during CI. The PR branch's file changes are invisible to those tests until after merge.

Affected tests (12 at time of writing):

- `TestBuildWithAnnotations`
- `TestBuildWithLabels`
- `TestBuildWithHTTPError`
- `TestBuildSkipFallback`
- `TestBuildViaRegistryMirrors`
- `TestBuildViaRegistryMap`
- `TestGitBuildcontext`
- `TestGitBuildcontextNoRef`
- `TestGitBuildcontextSubPath`
- `TestGitBuildcontextExplicitCommit`
- `TestKanikoDir`
- `TestExpectError`

The `TestGitBuildcontext*` family is intentionally testing kaniko's git-context feature and must use a real git URL, so the impact there is different (they test the mechanic, not the Dockerfile content). The others (`TestBuildWithAnnotations`, `TestBuildWithLabels`, etc.) use a git URL purely as a convenience to supply a build context — the Dockerfile content is what matters, and it is silently sourced from main.

This caused a real incident: pinning `Dockerfile.trivial` to `ubuntu:24.04` in PR #680 had no effect on `TestBuildWithAnnotations` during CI because the test kept fetching the un-pinned file from main, requiring `--extra-ignore-annotations` as a workaround.

## Fix

For tests that use a git URL only as a build context (not to test the git-context feature itself), replace the git URL with the local checkout. `runtime.Caller(0)` is already used elsewhere in the integration suite to get the local source directory.

```go
// Before
DockerGitRepo(url, "", branch)           // fetches github.com/.../kaniko.git#main
KanikoGitRepo(url, "", branch)           // fetches git://github.com/.../kaniko.git#refs/heads/main

// After
cwd                                      // local path, already used in BuildImage / BuildImageWithContext
```

For kaniko the local path would be passed as `-c <path>` (the directory context flag), which is the same pattern used by the main `TestRun` loop via `buildContextPath`.

The `TestGitBuildcontext*` tests must keep using a git URL because they are specifically exercising that feature. Those should be updated to use `GITHUB_SHA` (the merge commit) rather than branch, so the exact commit under test is always fetched:

```go
// GITHUB_SHA for a PR is the ephemeral merge commit GitHub creates,
// which contains the PR branch changes merged into main.
_, commit, url := getBranchCommitAndURL()
KanikoGitRepo(url, commit, "")   // fetches the exact merge commit
```

## Open questions

**Should `getBranchCommitAndURL` be removed entirely?**

Once the non-git-context tests switch to local paths, the function is only needed by the `TestGitBuildcontext*` family. It could be simplified or inlined there.
