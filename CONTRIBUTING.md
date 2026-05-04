# Contributing to Kaniko

We'd love to accept your patches and contributions to this project!!

To get started developing, see our [DEVELOPMENT.md](./DEVELOPMENT.md).

In this file you'll find info on:

- [Contributing to Kaniko](#contributing-to-kaniko)
  - [Code reviews](#code-reviews)
  - [Standards](#standards)
    - [Commit Messages](#commit-messages)
    - [Coding standards](#coding-standards)
  - [Finding something to work on](#finding-something-to-work-on)

## Code reviews

All submissions, including submissions by project members, require review. We
use GitHub pull requests for this purpose. Consult
[GitHub Help](https://help.github.com/articles/about-pull-requests/) for more
information on using pull requests.

## Standards

This section describes the standards we will try to maintain in this repo.

### Commit Messages

Reference the related issue in the subject using a short prefix to indicate which repository the issue lives in:

| Prefix | Repository |
|--------|-----------|
| `mzNNN` | This repository (`osscontainertools/kaniko`, formerly `mzihlmann/kaniko`) |
| `cgNNN` | Chainguard fork (`chainguard-dev/kaniko`) |
| `NNN` (no prefix) | Original Google repository (`GoogleContainerTools/kaniko`) |

Include the PR number in parentheses at the end of the subject line:

```
mz661: resolve secrets before moving kaniko dir (#662)
```

For bug fixes, add a body paragraph explaining what the bug was and how the fix works:

```
mz661: resolve secrets before moving kaniko dir (#662)

When KANIKO_DIR is set to a path other than the executor directory,
moveKanikoDir relocates all files under the original kaniko dir before
resolveSecrets runs. Any --secret with src= pointing into that dir is
therefore missing by the time its path is read, causing the build to
fail with "no such file or directory".

The fix moves resolveSecrets before moveKanikoDir.
```

For simple changes with no associated issue, a subject line alone is fine.

### Coding standards

The code in this repo should follow best practices, specifically:

- [Go code review comments](https://go.dev/wiki/CodeReviewComments)

## Finding something to work on

Thanks so much for considering contributing to our project!! We hope very much
you can find something interesting to work on:

- To find issues that we particularly would like contributors to tackle, look
  for
  [issues with the "help wanted" label](https://github.com/osscontainertools/kaniko/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22).
- Issues that are good for new folks will additionally be marked with
  ["good first issue"](https://github.com/osscontainertools/kaniko/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).
