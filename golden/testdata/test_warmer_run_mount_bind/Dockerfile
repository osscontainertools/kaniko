FROM alpine:3.20 AS builder
RUN --mount=type=bind,from=alpine:latest,target=/external true
RUN --mount=type=bind,from=busybox:1.36,target=/external true
RUN --mount=type=bind,from=busybox:1.36,target=/again true

FROM scratch AS final
RUN --mount=type=bind,from=builder,target=/builder true
RUN --mount=type=bind,from=0,target=/stage true
