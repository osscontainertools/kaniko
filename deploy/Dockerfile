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
FROM --platform=$BUILDPLATFORM golang:1.27.3@sha256:f20d83a83e18dbd2a4714b8ffd4b30b287b6243962dd75608e86abcc63ba4efc AS builder-base
WORKDIR /src

# This arg is passed by docker buildx & contains the target CPU architecture (e.g., amd64, arm64, etc.)
ARG TARGETARCH
ARG TARGETOS

ENV GOARCH=$TARGETARCH
ENV GOOS=$TARGETOS

ENV CGO_ENABLED=0

# Add .docker config dir
RUN mkdir -p /kaniko/.docker

COPY . .
RUN mkdir -p /licenses/go \
    && cp LICENSE /licenses/ \
    && cp /usr/local/go/LICENSE /licenses/go/ \
    && find vendor -type f \( -name 'LICEN[CS]E*' -o -name 'NOTICE*' -o -name 'COPYING*' \) -exec cp --parents {} /licenses/ \; \
    && hardlink -t /licenses

FROM builder-base AS builder
ARG COVER=""
RUN make COVER="$COVER" out/executor out/warmer \
    && rm -rf /root/.cache

# The helper names must match the argv[0] dispatch in pkg/creds.
# A hardlink only shares the file when it is copied in the same layer as the binary.
RUN mkdir -p /dist/executor /dist/warmer \
    && cp out/executor /dist/executor/ \
    && cp out/warmer /dist/warmer/ \
    && for helper in docker-credential-gcr docker-credential-ecr-login docker-credential-acr-env; do \
        ln /dist/executor/executor /dist/executor/$helper \
        && ln /dist/warmer/warmer /dist/warmer/$helper; \
    done

FROM builder-base AS builder-slim
ARG COVER=""
RUN make COVER="$COVER" out/executor-slim \
    && rm -rf /root/.cache

# use musl busybox since it's staticly compiled on all platforms
# Keep in sync with deploy/sources/busybox.spdx.json.
FROM busybox:musl@sha256:ea2b9914a16a4ac1981994af97b318f7c7d4db76b580c56177f08bf76f4a0be8 AS busybox
RUN mkdir -m 777 -p /kaniko

FROM scratch AS busybox-licenses
ADD --checksum=sha256:bbfc9843646d483c334664f651c208b9839626891d8f17604db2146962f43548 \
    https://raw.githubusercontent.com/mirror/busybox/371fe9f71d445d18be28c82a2a6d82115c8af19d/LICENSE /busybox/
ADD --checksum=sha256:f9bc4423732350eb0b3f7ed7e91d530298476f8fec0c6c427a1c04ade22655af \
    https://raw.githubusercontent.com/kraj/musl/v1.2.5/COPYRIGHT /musl/

FROM scratch AS tini-licenses
ADD --checksum=sha256:e5f46bca81266bdd511cf08018d66866870531794569c04f9b45f50dd23c28b0 \
    https://raw.githubusercontent.com/krallin/tini/v0.19.0/LICENSE /tini/
ADD --checksum=sha256:dc626520dcd53a22f727af3ee42c770e56c97a64fe3adb063799d8ab032fe551 \
    https://raw.githubusercontent.com/bminor/glibc/glibc-2.23/COPYING.LIB /glibc/
ADD --checksum=sha256:61abdd6930c9c599062d89e916b3e7968783879b6be0ee1c6229dd6169def431 \
    https://raw.githubusercontent.com/bminor/glibc/glibc-2.23/LICENSES /glibc/

FROM scratch AS kaniko-base-slim

# Create kaniko directory with world write permission to allow non root run
COPY --from=busybox /kaniko /kaniko

COPY --from=builder-base /etc/ssl/certs/ca-certificates.crt /kaniko/ssl/certs/
COPY files/nsswitch.conf /etc/nsswitch.conf
COPY --from=builder-base /licenses /kaniko/licenses
ENV HOME=/root
ENV USER=root
ENV PATH=/usr/local/bin:/kaniko
ENV SSL_CERT_DIR=/kaniko/ssl/certs

FROM kaniko-base-slim AS kaniko-base

COPY --from=builder /kaniko/.docker /kaniko/.docker

ENV DOCKER_CONFIG=/kaniko/.docker/
ENV DOCKER_CREDENTIAL_GCR_CONFIG=/kaniko/.config/gcloud/docker_credential_gcr_config.json
WORKDIR /workspace

# mz446: there is no tini binary release on riscv64
# Keep in sync with deploy/sources/tini-*.spdx.json.
FROM kaniko-base AS kaniko-base-amd64
ADD --checksum=sha256:c5b0666b4cb676901f90dfcb37106783c5fe2077b04590973b885950611b30ee \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-amd64 \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
COPY deploy/sources/tini-native.spdx.json /kaniko/sbom/tini.spdx.json
FROM kaniko-base AS kaniko-base-arm
ADD --checksum=sha256:d33a3731dce1e7db7754ed92edf97236ca7c7b786455abe3b286cda82205a4ae \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-armhf \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
COPY deploy/sources/tini-cross.spdx.json /kaniko/sbom/tini.spdx.json
FROM kaniko-base AS kaniko-base-arm64
ADD --checksum=sha256:eae1d3aa50c48fb23b8cbdf4e369d0910dfc538566bfd09df89a774aa84a48b9 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-arm64 \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
COPY deploy/sources/tini-cross.spdx.json /kaniko/sbom/tini.spdx.json
FROM kaniko-base AS kaniko-base-s390x
ADD --checksum=sha256:95657aec2459ff06123e0674565da625c50704b1db86373cd91afc3c0dcce997 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-s390x \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
COPY deploy/sources/tini-cross.spdx.json /kaniko/sbom/tini.spdx.json
FROM kaniko-base AS kaniko-base-ppc64le
ADD --checksum=sha256:b913a6d5be168a14cfe5f27238a181a12aece281e30a57e5d31a148bc5dfb687 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-ppc64le \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
COPY deploy/sources/tini-cross.spdx.json /kaniko/sbom/tini.spdx.json
FROM kaniko-base AS kaniko-base-riscv64
FROM kaniko-base-${TARGETARCH} AS kaniko-base-2

### FINAL STAGES ###

FROM kaniko-base AS kaniko-warmer

COPY --from=builder /dist/warmer/ /kaniko/

ENTRYPOINT ["/kaniko/warmer"]

FROM kaniko-base-2 AS kaniko-executor

COPY --from=builder /dist/executor/ /kaniko/

ENTRYPOINT ["/kaniko/executor"]

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS kaniko-alpine

RUN apk add --no-cache ca-certificates git

COPY --from=builder /src/out/executor /kaniko/executor
COPY --from=builder /kaniko/.docker /kaniko/.docker
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /kaniko/ssl/certs/
COPY --from=kaniko-base-2 /kaniko/licenses /kaniko/licenses

# mz446: there is no tini binary release on riscv64
ARG TARGETARCH
RUN --mount=from=kaniko-base-2,source=/kaniko,target=/mnt/kaniko \
    [ "$TARGETARCH" = "riscv64" ] || cp -r /mnt/kaniko/tini /mnt/kaniko/sbom /kaniko/

# mz595: pre-existing alpine files would result in invalid build outputs.
# with these flags we wipe the filesystem before build, circumventing this issue.
ENV KANIKO_PRESERVE_CONTEXT=1 \
    KANIKO_PRE_CLEANUP=1 \
    KANIKO_CLEANUP=1 \
    DOCKER_CONFIG=/kaniko/.docker/ \
    DOCKER_CREDENTIAL_GCR_CONFIG=/kaniko/.config/gcloud/docker_credential_gcr_config.json \
    SSL_CERT_DIR=/kaniko/ssl/certs

ENTRYPOINT ["/kaniko/executor"]

FROM kaniko-executor AS kaniko-debug

ENV PATH=/usr/local/bin:/kaniko:/busybox

COPY --from=builder /src/out/warmer /kaniko/warmer

COPY --from=busybox /bin /busybox
COPY --from=busybox-licenses / /kaniko/licenses/
COPY deploy/sources/busybox.spdx.json /kaniko/sbom/busybox.spdx.json
# Declare /busybox as a volume to get it automatically in the path to ignore
VOLUME /busybox

RUN ["/busybox/mkdir", "-p", "/bin"]
RUN ["/busybox/ln", "-s", "/busybox/sh", "/bin/sh"]

FROM kaniko-executor AS kaniko-debug-2

ENV PATH=/usr/local/bin:/kaniko:/busybox2

COPY --from=builder /src/out/warmer /kaniko/warmer

COPY --from=busybox /bin /busybox2
COPY --from=busybox-licenses / /kaniko/licenses/
COPY deploy/sources/busybox.spdx.json /kaniko/sbom/busybox.spdx.json
# Declare /busybox as a volume to get it automatically in the path to ignore
VOLUME /busybox2

RUN ["/busybox2/mkdir", "-p", "/bin"]
RUN ["/busybox2/ln", "-s", "/busybox2/sh", "/bin/sh"]

FROM kaniko-base-slim AS kaniko-slim

COPY --from=builder-slim /src/out/executor-slim /kaniko/executor

ENTRYPOINT ["/kaniko/executor"]
