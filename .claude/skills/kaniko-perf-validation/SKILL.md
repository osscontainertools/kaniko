---
name: kaniko-perf-validation
description: "Run the testbench that proves a kaniko performance PR and post the result as a `## Performance validation` PR comment. Use when a PR carries the `performance` label, changes build speed or resource usage, or the user asks to verify, benchmark, or validate a performance change."
---

# kaniko performance validation

Usage: `/kaniko-perf-validation <pr-number>`

## Rules

- Never run `out/executor` on the host. Every build runs inside `docker run --rm --net=host <image>`.
- Pick configs from the diff:
  - Every change sits behind a new flag: two configs from one binary, flag off and flag on.
  - The change modifies code behind an existing flag: add `main-ff` (merge base, flag on). `main-ff` vs `branch-ff` proves no regression in the flagged path.
  - Any change outside the flag's activation: three configs, `main` (the merge base, name its SHA), `branch` (PR binary, flag off), `branch-ff` (PR binary, flag on). `main` vs `branch` proves no regression, `branch-ff` shows the win.
  - No flag: `main` and `branch`.
- Two scenarios: `extreme`, a synthetic Dockerfile that amplifies the mechanism, and `ex01`, Akihiro Suda's buildbench example as the regular case. Clone `github.com/AkihiroSuda/buildbench` and use `examples/ex01`. Swap `ex01` for another regular build only when it never reaches the changed code path, and say why in the comment.
- Five runs per config per scenario, interleaved and randomly ordered: each round runs every config once, in an order drawn with `shuf`, and records each run's position. Interleaving without shuffling is not enough, timings can depend on run order.
- One warm-up build per scenario before round 1, discarded.
- Pin every base image by digest.
- Pair wall time with a deterministic mechanism count that stays constant across rounds, ie. sockets/requests from the registry log, `Deleting filesystem` or extraction lines from the build log, mounts/blob uploads, bytes. The count carries the claim when the clock is noisy.
- Peak RSS (`VmHWM` polled from `/proc/<pid>/status` of the executor process) varies between runs. Report it like wall time, median and mean ± sd, not as a count.
- Use a local `registry:2` on `localhost:5000` as cache repo and push target unless the mechanism needs distance (bandwidth, round trips). Then say so and use the remote setup.
- Use a fresh cache repo and a fresh push target repo per run when the scenario measures cold behaviour. The registry skips blobs it already has, so a reused push target hides uploads.
- A build that exits non-zero aborts the benchmark. Never record its timing.

## Steps

1. Read the PR diff and name the mechanism and the flag. Pick the count that exposes it.
2. Build one executor image per binary from `deploy/Dockerfile --target kaniko-executor`: `kaniko-perf:branch` from the PR head, plus `kaniko-perf:main` from the merge base when the configs include `main`. Use a separate worktree for main.
3. Write the two scenario Dockerfiles and any context into the scratchpad.
4. Write a runner script that loops rounds, shuffles configs, runs each build with `/usr/bin/time` or `date +%s.%N`, scrapes the count, and appends `scenario,round,position,config,seconds,count` to a CSV.
5. Run it. Check that every count is identical across rounds of the same config. If not, find out why before reporting.
6. Compute median and mean ± sd per scenario and config from the CSV with `awk` or `jq`.
7. Show the comment to the user. Post it with `gh pr comment <pr-number> --body-file <file>` only after they approve.

## Comment format

````markdown
## Performance validation

`main` is `<sha>`, `branch` is this branch with the flag off, `branch-ff` the same binary with `FF_KANIKO_<NAME>=1` (or: one binary, the flag is the only difference). <how kaniko ran, registry setup>, warm-up build per scenario, configs interleaved and shuffled within each round.

### extreme

<one line on the scenario>

```dockerfile
<Dockerfile, digests may be shortened to sha256:ea2b...be8>
```

| config | <count> | median | mean ± sd |
|---|---|---|---|
| main | ... | ... | ... |
| branch | ... | ... | ... |
| branch-ff | **...** | **...** | ... |

### ex01

<same shape, link the buildbench example instead of inlining its Dockerfile>

<Two or three plain sentences: counts identical in every round, main and branch agree, why wall time does or does not move, any anomaly and its cause.>
````

Bold the winning cells. n=5 unless stated. Explain outliers instead of dropping them.
