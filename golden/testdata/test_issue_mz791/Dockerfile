# mz791: a COPY whose path expands a build arg is not reflected in the layer
# cache key. The COPY is keyed on the raw instruction, so a build that only
# changes the arg reuses the layer cached for a different resolved path and
# serves a stale layer. The base stage is its own ocilayout stage so the COPY
# --from goes through the cross-stage cache-key inference.
FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d AS first
RUN touch /foo

FROM busybox@sha256:fd8d9aa63ba2f0982b5304e1ee8d3b90a210bc1ffb5314d980eb6962f1a9715d AS second
ARG A
COPY --from=first /foo /$A/foo
