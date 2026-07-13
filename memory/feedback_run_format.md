---
name: RUN command formatting in Dockerfiles
description: Preferred multi-command RUN format in Dockerfiles
type: feedback
---

Use backslash-continuation with one command per line, indented 4 spaces:

```dockerfile
RUN apt-get update \
    && apt-get install -y \
        libcap2-bin \
    && rm -rf /var/lib/apt/lists/*
```

**Why:** Consistent with the style used across the project's integration test Dockerfiles.
**How to apply:** Any time writing RUN commands with multiple chained operations in a Dockerfile.
