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
FROM --platform=$BUILDPLATFORM golang:1.26.3@sha256:313faae491b410a35402c05d35e7518ae99103d957308e940e1ae2cfa0aac29b AS builder
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

# Get credential helpers
COPY go.mod go.sum ./
RUN go build -o out/ github.com/GoogleCloudPlatform/docker-credential-gcr/v2 \
    && go build -o out/ github.com/awslabs/amazon-ecr-credential-helper/ecr-login/cli/docker-credential-ecr-login \
    && go build -o out/ github.com/chrismellard/docker-credential-acr-env \
    && rm -rf /root/.cache /go/pkg

COPY . .
ARG COVER=""
RUN make COVER="$COVER" out/executor out/warmer \
    && rm -rf /root/.cache

# Generate latest ca-certificates
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM debian:trixie-slim@sha256:109e2c65005bf160609e4ba6acf7783752f8502ad218e298253428690b9eaa4b AS certs
RUN apt update && apt install -y ca-certificates

# use musl busybox since it's staticly compiled on all platforms
FROM busybox:musl@sha256:19b646668802469d968a05342a601e78da4322a414a7c09b1c9ee25165042138 AS busybox
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

COPY --from=builder --chown=0:0 /src/out/docker-credential-gcr /kaniko/docker-credential-gcr
COPY --from=builder --chown=0:0 /src/out/docker-credential-ecr-login /kaniko/docker-credential-ecr-login
COPY --from=builder --chown=0:0 /src/out/docker-credential-acr-env /kaniko/docker-credential-acr-env

COPY --from=builder /kaniko/.docker /kaniko/.docker

ENV DOCKER_CONFIG=/kaniko/.docker/
ENV DOCKER_CREDENTIAL_GCR_CONFIG=/kaniko/.config/gcloud/docker_credential_gcr_config.json
WORKDIR /workspace

# mz446: there is no tini binary release on riscv64
FROM kaniko-base AS kaniko-base-amd64
ADD --checksum=sha256:c5b0666b4cb676901f90dfcb37106783c5fe2077b04590973b885950611b30ee \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-amd64 \
    /kaniko/tini
FROM kaniko-base AS kaniko-base-arm64
ADD --checksum=sha256:eae1d3aa50c48fb23b8cbdf4e369d0910dfc538566bfd09df89a774aa84a48b9 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-arm64 \
    /kaniko/tini
FROM kaniko-base AS kaniko-base-s390x
ADD --checksum=sha256:95657aec2459ff06123e0674565da625c50704b1db86373cd91afc3c0dcce997 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-s390x \
    /kaniko/tini
FROM kaniko-base AS kaniko-base-ppc64le
ADD --checksum=sha256:b913a6d5be168a14cfe5f27238a181a12aece281e30a57e5d31a148bc5dfb687 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-ppc64le \
    /kaniko/tini
FROM kaniko-base AS kaniko-base-riscv64
FROM kaniko-base-${TARGETARCH} AS kaniko-base-2

### FINAL STAGES ###

FROM kaniko-base AS kaniko-warmer

COPY --from=builder /src/out/warmer /kaniko/warmer

ENTRYPOINT ["/kaniko/warmer"]

FROM kaniko-base-2 AS kaniko-executor

COPY --from=builder /src/out/executor /kaniko/executor

ENTRYPOINT ["/kaniko/executor"]

FROM alpine:3.23.4@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11 AS kaniko-alpine

RUN apk add --no-cache ca-certificates git

COPY --from=builder /src/out/executor /kaniko/executor

# mz446: there is no tini binary release on riscv64
ARG TARGETARCH
RUN --mount=from=kaniko-base-2,source=/kaniko,target=/mnt/kaniko \
    [ "$TARGETARCH" = "riscv64" ] || cp /mnt/kaniko/tini /kaniko/tini

# mz595: pre-existing alpine files would result in invalid build outputs.
# with these flags we wipe the filesystem before build, circumventing this issue.
ENV KANIKO_PRESERVE_CONTEXT=1 \
    KANIKO_PRE_CLEANUP=1

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

FROM kaniko-base-slim AS kaniko-slim

COPY --from=builder /src/out/executor /kaniko/executor

ENTRYPOINT ["/kaniko/executor"]
