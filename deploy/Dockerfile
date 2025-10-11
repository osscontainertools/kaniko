# Copyright 2018 Google, Inc. All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM golang:1.25.2 AS builder
WORKDIR /src

# This arg is passed by docker buildx & contains the target CPU architecture (e.g., amd64, arm64, etc.)
ARG TARGETARCH
ARG TARGETOS

ENV GOARCH=$TARGETARCH
ENV GOOS=$TARGETOS

ENV CGO_ENABLED=0

# Add .docker config dir
RUN mkdir -p /kaniko/.docker

#  dependencies https://github.com/golang/go/issues/48332

# The versions of these tools and their transitive dependencies are controlled via go.mod.
# To update the version of any tool installed here, run
#
#   go get <tool>@<commit-sha-or-tag>
#   go mod vendor
#
# Then submit a PR with the changes to `go.mod`, `go.sum`, and `vendor`.
COPY . .
RUN make out/executor out/warmer \
    && rm -rf /root/.cache

# Generate latest ca-certificates
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM debian:bookworm-slim AS certs
RUN apt update && apt install -y ca-certificates

# use musl busybox since it's staticly compiled on all platforms
FROM busybox:musl AS busybox
RUN mkdir -p /kaniko && chmod 777 /kaniko

FROM scratch AS kaniko-base-slim

# Create kaniko directory with world write permission to allow non root run
COPY --from=busybox /kaniko /kaniko

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /kaniko/ssl/certs/
COPY files/nsswitch.conf /etc/nsswitch.conf
ENV HOME=/root
ENV USER=root
ENV PATH=/usr/local/bin:/kaniko
ENV SSL_CERT_DIR=/kaniko/ssl/certs

FROM kaniko-base-slim AS kaniko-base

COPY --from=builder /kaniko/.docker /kaniko/.docker

ENV DOCKER_CONFIG=/kaniko/.docker/
ENV DOCKER_CREDENTIAL_GCR_CONFIG=/kaniko/.config/gcloud/docker_credential_gcr_config.json
WORKDIR /workspace

### FINAL STAGES ###

FROM kaniko-base AS kaniko-warmer

COPY --from=builder /src/out/warmer /kaniko/warmer

ENTRYPOINT ["/kaniko/warmer"]

FROM kaniko-base AS kaniko-executor

COPY --from=builder /src/out/executor /kaniko/executor

ENTRYPOINT ["/kaniko/executor"]

FROM kaniko-executor AS kaniko-debug

ENV PATH=/usr/local/bin:/kaniko:/busybox

COPY --from=builder /src/out/warmer /kaniko/warmer

COPY --from=busybox /bin /busybox
# Declare /busybox as a volume to get it automatically in the path to ignore
VOLUME /busybox

RUN ["/busybox/mkdir", "-p", "/bin"]
RUN ["/busybox/ln", "-s", "/busybox/sh", "/bin/sh"]

FROM kaniko-executor AS kaniko-debug-2

ENV PATH=/usr/local/bin:/kaniko:/busybox2

COPY --from=builder /src/out/warmer /kaniko/warmer

COPY --from=busybox /bin /busybox2
# Declare /busybox as a volume to get it automatically in the path to ignore
VOLUME /busybox2

RUN ["/busybox2/mkdir", "-p", "/bin"]
RUN ["/busybox2/ln", "-s", "/busybox2/sh", "/bin/sh"]
