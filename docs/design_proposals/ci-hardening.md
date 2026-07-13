# CI supply chain hardening

* Author: Martin Zihlmann
* Date: 2026-04-26
* Status: Under implementation

## Background

GitHub Actions workflows are a common supply chain attack surface. A compromised action, a floating script download, or overly broad permissions can allow an attacker to exfiltrate secrets, push malicious images, or inject code into the build. This document tracks the remaining hardening work for kaniko's four workflows: `images.yaml`, `integration-tests.yaml`, `unit-tests.yaml`, and `nightly-vulnerability-scan.yml`.

## Open items

**1. Switch `integration-tests.yaml` harden-runner from `audit` to `block`**
Needs a main-branch run with harden-runner present to collect the egress domain list. Once that data is available, derive the allowlist and flip the policy.

**2. Add harden-runner to `coverage-merge` job** (`integration-tests.yaml`)
The `coverage-merge` job does not yet have a `harden-runner` step.

## Implementation order

1. Item 1 — merge the ci-hardening branch so integration tests run on main with harden-runner in audit mode; collect the domain list from those logs, then switch to `block`.
2. Item 2 can be done independently.
