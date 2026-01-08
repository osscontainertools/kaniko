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
FROM --platform=$BUILDPLATFORM golang:1.25.5 AS builder
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
RUN make out/executor out/warmer \
    && rm -rf /root/.cache

# Build static tini
ARG BUILDPLATFORM
FROM --platform=$BUILDPLATFORM debian:trixie-slim AS tini-builder
ARG BUILDARCH
ARG TARGETARCH

ARG DEBIAN_FRONTEND=noninteractive
RUN <<EOF
set -eux
case "${TARGETARCH}" in
    "${BUILDARCH}") GCC=gcc LIBC=libc6-dev ;;
    amd64) GCC=gcc-x86-64-linux-gnu LIBC=libc6-dev-amd64-cross ;;
    arm64) GCC=gcc-aarch64-linux-gnu LIBC=libc6-dev-arm64-cross ;;
    s390x) GCC=gcc-s390x-linux-gnu LIBC=libc6-dev-s390x-cross ;;
    riscv64) GCC=gcc-riscv64-linux-gnu LIBC=libc6-dev-riscv64-cross ;;
    ppc64le) GCC=gcc-powerpc64le-linux-gnu LIBC=libc6-dev-ppc64el-cross ;;
    *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;;
esac
apt update
apt install --no-install-recommends -y \
    ca-certificates \
    git \
    make \
    cmake \
    $GCC \
    $LIBC
rm -rf /var/lib/apt/lists/*
EOF

ARG TINI_VERSION=v0.19.0
RUN <<EOF
set -eux
case "${TARGETARCH}" in
    "${BUILDARCH}") CC=gcc ;;
    amd64) CC=x86_64-linux-gnu-gcc ;;
    arm64) CC=aarch64-linux-gnu-gcc ;;
    s390x) CC=s390x-linux-gnu-gcc ;;
    riscv64) CC=riscv64-linux-gnu-gcc ;;
    ppc64le) CC=powerpc64le-linux-gnu-gcc ;;
    *) echo "unsupported arch: ${TARGETARCH}" >&2; exit 1 ;;
esac
git clone --branch ${TINI_VERSION} --depth 1 https://github.com/krallin/tini.git
mkdir -p tini/build
cd tini/build
cmake .. \
    -DCMAKE_BUILD_TYPE=Release \
    -DMINIMAL=ON \
    -DTINI_STATIC=ON \
    -DCMAKE_C_COMPILER=${CC}
make -j4 tini-static
mv tini-static /
cd ..
rm -rf build
EOF

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

COPY --from=builder --chown=0:0 /src/out/docker-credential-gcr /kaniko/docker-credential-gcr
COPY --from=builder --chown=0:0 /src/out/docker-credential-ecr-login /kaniko/docker-credential-ecr-login
COPY --from=builder --chown=0:0 /src/out/docker-credential-acr-env /kaniko/docker-credential-acr-env

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
COPY --from=tini-builder /tini-static /kaniko/tini

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
