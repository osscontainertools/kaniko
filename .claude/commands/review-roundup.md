---
model: sonnet
effort: medium
---

Categorize open non-draft PRs into a Slack-ready review-nudge message, prioritizing user-reported bugs. Usage: `/review-roundup`

1. List PRs:
   ```bash
   gh pr list --state open --draft=false --limit 100 \
     --json number,title,reviewRequests,additions,deletions,createdAt \
     --jq '.[] | "\(.number)\t+\(.additions)/-\(.deletions)\t[\(.reviewRequests|map(.login // .name)|join(","))]\t\(.title)"'
   ```
2. Linked issue per PR: `gh pr view <num> --json body --jq '.body[:600]'` → `Closes`/`Fixes #N`.
3. Issue author+labels: `gh issue view <num> --json author,title,labels --jq '.author.login + " [" + (.labels|map(.name)|join(",")) + "] " + .title'`
   - `mzihlmann` is the maintainer; his issues (typically `fuzz`/internal) are NOT user-reported and never go in 🔥/🙋, whatever the labels.
   - Multi-issue PR = user-reported only if ≥1 linked issue has an external author. All-`mzihlmann` → internal (🐛/🧹). Check every linked issue.
   - A PR continuing a user's contribution (PR body cites a prior user PR) credits that contributor (`continues @user's #N`) and rises out of plain 🧹.
4. Output the Slack message in a code block for the WYSIWYG composer: `*bold*` headers (single asterisk, not `**`), `- ` bullets, `[#N](url)` links. PR url: `https://github.com/osscontainertools/kaniko/pull/<num>`. No `---` separators, no prose around the block.
   - Header: `📋 *Open PRs needing review*`. No reviewer @mentions — pasted mentions don't notify and highlight inconsistently.
   - Groups in priority order, drop empty: 🔥 Top priority (user bug `bug`/`regression`) · 🙋 User-requested feature (user `enhancement`) · 🐛 Crash fixes (fuzz/internal) · ⚡ Caching · ♻️ Reproducible builds · 🧹 Maintenance (dead code, dep bumps, deprecation flags).
   - Line: `- [#<num>](url) — <what it fixes> — closes #<issue> by @<author> \`+A/-B\``. `by @<author>` only when user-reported. `⚠️ no reviewers` when `reviewRequests` empty. Tag `easy review` for low-risk PRs (dead code, coverage).
   - Never warn `large`. The `+A/-B` shown is the reviewable diff: exclude `vendor/`, `go.mod`/`go.sum`/`modules.txt`, and `golden/`/`testdata/` snapshots. When raw total is inflated, show the code count and note `(rest vendored)` / `(rest golden snapshots)`. Compute via: `gh pr view <num> --json files --jq '[.files[]|select(.path|test("^vendor/|go\\.(mod|sum)|modules\\.txt|golden/|testdata/")|not)]|"+\(map(.additions)|add)/-\(map(.deletions)|add)"'`.
5. If any PR lacks reviewers, flag and ask before assigning the shared set. Never assign without confirmation.
