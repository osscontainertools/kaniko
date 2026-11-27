FROM alpine:3.18 AS base
ONBUILD RUN --mount=type=bind,from=named-assets,target=/named true
ONBUILD RUN --mount=type=bind,from=3,target=/numeric true

FROM base AS runtime

FROM golang:1.20 AS named-assets
FROM busybox:latest AS numeric-assets
