FROM golang:1.20 AS assets
RUN echo assets
FROM debian:bookworm AS unrelated
FROM alpine:3.18 AS runtime
COPY --from=assets /assets /assets
