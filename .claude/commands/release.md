---
model: sonnet
effort: high
---

Prepare a kaniko release.

Usage: `/release <version>`

0. **Create release branch** from `main`:
   ```
   git checkout main && git pull
   git checkout -b release-v<version>
   ```

1. **Update Makefile** — set `VERSION_MAJOR`, `VERSION_MINOR`, `VERSION_BUILD` to match `<version>`.

2. **Get PR list** — find `mergedAt` of the previous release PR:
   ```bash
   gh pr list --repo osscontainertools/kaniko --state merged \
     --search "release v<prev-version>" --json mergedAt --jq '.[0].mergedAt'
   ```
   Then list PRs merged after that timestamp:
   ```
   gh pr list --repo osscontainertools/kaniko --state merged --limit 200 \
     --json number,title,mergedAt,author \
     --jq '.[] | select(.mergedAt > "PREV_RELEASE_MERGEDAT") | "\(.mergedAt) \(.number) \(.author.login) \(.title)"' | sort
   ```
   Filter by `mergedAt`, never by PR number — PRs merge out of order.

3. **CHANGELOG.md scaffolding** — insert at top, two blank lines before previous release:
   ```
   # v<version> Release YYYY-MM-DD

   ## What's Changed
   ### Security        CVE fixes, hardening
   ### Bugfixes        incorrect behavior now fixed; "no feature flag" means the fix itself is not gated by a new flag — bugs in code paths enabled by an existing flag are still Bugfixes
   ### Standardization FF flags correcting spec-violating behavior (diverged from Docker/OCI, flag gates fix for backwards compat)
   ### Caching         cache correctness or cache-hit-rate improvements
   ### Performance     speed/resource
   ### Usability       FF flags or options adding new user-facing capabilities
   ### Maintenance     build(deps): bumps
   ### Fork Related    test infra, CI, internal tooling; preparatory work with no user benefit until follow-up lands; if there is an associated FF flag, prefix the entry with it even if this PR does not introduce the flag
   ### Refactorings    zero runtime behavior change — if the change can panic, log differently, or alter any output in any code path, it is not a Refactoring; move it to Fork Related instead


   # v<prev> Release ...
   ```

4. **Strip attribution** — across all PR titles (not just Maintenance), replace ` by @dependabot[bot] in ` and ` by @mzihlmann in ` with `: `. Keep attribution for all other contributors.

5. **Maintenance** — move all `build(deps):` lines into `### Maintenance`.

6. **Deflate grouped bumps** — expand "bump the gomod group with N updates" into one line per package via `gh pr view <n> --repo osscontainertools/kaniko`.

7. **Merge repeated bumps** — collapse multiple bumps of the same lib into one line with combined version range; space-separate all PR URLs.

7b. **Sanity-check completeness** — every version change in `go.mod`, `.github/workflows/`, and `deploy/` must appear in Maintenance:
    ```bash
    PREV_SHA=$(git log --oneline --grep="^release$" | head -1 | awk '{print $1}')
    git diff $PREV_SHA -- go.mod .github/workflows/ deploy/
    ```
   If anything is missing, **stop and report the gap**.

8. **Drop "in ..." qualifiers** — strip `in the gomod group`, `in the actions group`, `in /deploy`.

9. **Sort PRs into categories** — order within each category by `mergedAt` ascending (dep entries: by earliest constituent PR). Drop empty categories. Group a bugfix-only-to-unblock-feature PR under the feature. Group an immediate-consequence fix with its causing PR. Deprecation notices → **Usability**.

10. **Rewrite titles** — user-facing descriptions based on PR bodies: what the user observes or gains, not internals. No em dashes or semicolons. Feature flags: only mention if the PR introduces the flag (check by whether it's added to the README). New flag format: `` `FF_KANIKO_FLAG_NAME=false` what it does ``. If a bug is only observable under an existing flag, omit the flag. Fork Related prep work: prefix with `` `FF_KANIKO_FLAG_NAME=false` `` even if the PR doesn't introduce the flag. Always include `=value` default; look up in README or code. Never use "part N" labels.

11. **Security CVEs** — scan previous release image:
    ```bash
    trivy image --ignore-unfixed martizih/kaniko:v<prev-version>
    ```
    Cross-reference "Fixed Version" with bumps in this release. Confirmed CVE fixes: add a new entry to `### Security` AND keep the Maintenance entry.

    Format: `* github.com/foo/bar v1.2.3: CVE-YYYY-NNNNN CVE-YYYY-NNNNN` — version is the vulnerable "from" version; space-separate CVEs.

    For GHSA IDs, look up canonical CVE:
    ```bash
    curl -s https://api.github.com/advisories/GHSA-XXXX-XXXX-XXXX | jq -r '.cve_id'
    ```
    Fall back to GHSA ID only if no CVE assigned.

12. **CHANGELOG_OVERVIEW.md** — cumulative diff from Google's v1.24.0:
    - **Security**: append CVEs to existing library lines (don't update the version — it's the Google baseline). New line only if library wasn't in Google's version.
    - **Bugfixes**: only bugs that existed in Google's v1.24.0 and are now fixed. Omit bugs introduced and fixed entirely in our fork.
    - **All other sections**: new features and user-visible changes a Google-version user gains by switching.

13. **Attribution check** — external contributors (not mzihlmann/dependabot). First-timers: PR authors where the following returns only the current PR:
    ```bash
    gh pr list --repo osscontainertools/kaniko --state merged --author <login> --json number
    ```
    Reporters: authors of issues closed in this release. If either non-empty, insert between release header and `## What's Changed`:
    ```
    ## Community Update
    @user made their first contribution in URL

    Also many thanks to @reporter1 and @reporter2 for reporting issues fixed in this release.
    ```
    Omit either line if its group is empty. "an issue"/"issues" by count. Also add `by @username in URL` to externally-authored PR entries.

14. **Consistency check** — compare entry style, capitalisation, and backtick usage against the two previous release entries. Fix deviations.

15. **Commit**:
    ```
    git add Makefile CHANGELOG.md CHANGELOG_OVERVIEW.md
    git commit -m "release"
    ```
    ```
    git push -u origin release-v<version>
    gh pr create --repo osscontainertools/kaniko \
      --base main \
      --title "release v<version>" \
      --body ""
    ```
