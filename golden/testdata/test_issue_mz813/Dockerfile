# mz813: a WORKDIR whose path expands a build arg is not reflected in the layer
# cache key. The WORKDIR is keyed on the raw instruction, so a build that only
# changes the arg reuses the layer cached for a different resolved path and
# serves a stale directory. Same defect as mz791 for COPY/ADD.
FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d
ARG A
WORKDIR /$A/foo
