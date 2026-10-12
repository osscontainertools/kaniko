ARG BASE=debian:bookworm
ARG VERSION=3.20
ARG ALPINE=alpine:${VERSION}
FROM ${BASE} AS base

FROM base AS derived

FROM Base AS upper

FROM ${ALPINE}

FROM debian:bookworm

FROM scratch
