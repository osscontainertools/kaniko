# Kaniko Project Roadmap

## Introduction

Kaniko is an open-source tool for building OCI-compliant container images securely and efficiently in environments without a Docker daemon. It enables fully unprivileged, reproducible builds that integrate seamlessly with CI/CD pipelines and Kubernetes-based infrastructure. This roadmap outlines our strategic goals and key areas of development.

## Strategic Goals
### Security
Improve the security standing between executor and payload, whilst keeping a best-in-class security boundary between the host and container.
### Standardization
Support the most recent Dockerfile standard and match the buildkit implementation bit-by-bit.
### Performance
Best-in-class build performance for large images and complex multi-stage dependencies.
### Community
Foster an active community, encouraging contributions, feedback, and collaboration. Set an end to the ghosting.

## Key Initiatives
### Security
  - Integrate [landlock](https://github.com/landlock-lsm) and other novel approaches to implement security boundaries entirely in unprivileged user spare.
### Bugfixes
  - Triage the entire backlog of [707 open issues](https://github.com/GoogleContainerTools/kaniko/issues) in an [open google sheet](https://docs.google.com/spreadsheets/d/e/2PACX-1vSKowmg8m13dTHhINEBxxNwme6ftCOykhIvB_XxVcIrgfVcDcCyI4uZvyh_eXTrUplXAfLYx8qXFNuc/pubhtml?gid=0&single=true).
### Standardization
  - Implement all command options as in the dockerfile standard.
### Performance
  - Implement cache-lookahead for multi-stage builds.
  - Support multi-image builds.
  - Switch from tarball to ocilayout for intermediates
### Usability
  - Simplify CLI surface by exposing only key build parameters and hiding implementation-specific flags
  - Support bakefile syntax.
  - Multi-arch support for images without non-native `RUN` statement.
### Community
  - Assemble a team of active Maintainers.
