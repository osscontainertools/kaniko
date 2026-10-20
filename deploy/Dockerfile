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
FROM --platform=$BUILDPLATFORM golang:1.27.2@sha256:820153096fda4de6ac1b7194d12e416b7dd8f4e9a33d17e0345b111b945162d8 AS golang

FROM golang AS builder
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
ARG COVER=""
RUN make COVER="$COVER" out/executor out/warmer \
    && rm -rf /root/.cache
RUN mkdir -p /licenses/go \
    && cp LICENSE /licenses/ \
    && cp /usr/local/go/LICENSE /licenses/go/ \
    && find vendor -type f \( -name 'LICEN[CS]E*' -o -name 'NOTICE*' -o -name 'COPYING*' \) -exec cp --parents {} /licenses/ \; \
    && hardlink -t /licenses

# The helper names must match the argv[0] dispatch in pkg/creds/helpers.go.
# A hardlink only shares the file when it is copied in the same layer as the binary.
RUN mkdir -p /dist/executor /dist/warmer \
    && cp out/executor /dist/executor/ \
    && cp out/warmer /dist/warmer/ \
    && for helper in docker-credential-gcr docker-credential-ecr-login docker-credential-acr-env; do \
        ln /dist/executor/executor /dist/executor/$helper \
        && ln /dist/warmer/warmer /dist/warmer/$helper; \
    done

# use musl busybox since it's staticly compiled on all platforms
FROM busybox:musl@sha256:ea2b9914a16a4ac1981994af97b318f7c7d4db76b580c56177f08bf76f4a0be8 AS busybox
RUN mkdir -p /kaniko && chmod 777 /kaniko

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

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /kaniko/ssl/certs/
COPY files/nsswitch.conf /etc/nsswitch.conf
COPY --from=builder /licenses /kaniko/licenses
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
FROM kaniko-base AS kaniko-base-amd64
ADD --checksum=sha256:c5b0666b4cb676901f90dfcb37106783c5fe2077b04590973b885950611b30ee \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-amd64 \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
FROM kaniko-base AS kaniko-base-arm
ADD --checksum=sha256:d33a3731dce1e7db7754ed92edf97236ca7c7b786455abe3b286cda82205a4ae \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-armhf \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
FROM kaniko-base AS kaniko-base-arm64
ADD --checksum=sha256:eae1d3aa50c48fb23b8cbdf4e369d0910dfc538566bfd09df89a774aa84a48b9 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-arm64 \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
FROM kaniko-base AS kaniko-base-s390x
ADD --checksum=sha256:95657aec2459ff06123e0674565da625c50704b1db86373cd91afc3c0dcce997 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-s390x \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
FROM kaniko-base AS kaniko-base-ppc64le
ADD --checksum=sha256:b913a6d5be168a14cfe5f27238a181a12aece281e30a57e5d31a148bc5dfb687 \
    --chown=0:0 --chmod=555 \
    https://github.com/krallin/tini/releases/download/v0.19.0/tini-static-ppc64le \
    /kaniko/tini
COPY --from=tini-licenses / /kaniko/licenses/
FROM kaniko-base AS kaniko-base-riscv64
FROM kaniko-base-${TARGETARCH} AS kaniko-base-2

### FINAL STAGES ###

FROM kaniko-base AS kaniko-warmer

COPY --from=builder /dist/warmer/ /kaniko/

ENTRYPOINT ["/kaniko/warmer"]

FROM kaniko-base-2 AS kaniko-executor

COPY --from=builder /dist/executor/ /kaniko/

ENTRYPOINT ["/kaniko/executor"]

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS kaniko-alpine

RUN apk add --no-cache ca-certificates git

COPY --from=builder /src/out/executor /kaniko/executor
COPY --from=builder /kaniko/.docker /kaniko/.docker
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /kaniko/ssl/certs/
COPY --from=kaniko-base-2 /kaniko/licenses /kaniko/licenses

# mz446: there is no tini binary release on riscv64
ARG TARGETARCH
RUN --mount=from=kaniko-base-2,source=/kaniko,target=/mnt/kaniko \
    [ "$TARGETARCH" = "riscv64" ] || cp /mnt/kaniko/tini /kaniko/tini

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
# Declare /busybox as a volume to get it automatically in the path to ignore
VOLUME /busybox

RUN ["/busybox/mkdir", "-p", "/bin"]
RUN ["/busybox/ln", "-s", "/busybox/sh", "/bin/sh"]

FROM kaniko-executor AS kaniko-debug-2

ENV PATH=/usr/local/bin:/kaniko:/busybox2

COPY --from=builder /src/out/warmer /kaniko/warmer

COPY --from=busybox /bin /busybox2
COPY --from=busybox-licenses / /kaniko/licenses/
# Declare /busybox as a volume to get it automatically in the path to ignore
VOLUME /busybox2

RUN ["/busybox2/mkdir", "-p", "/bin"]
RUN ["/busybox2/ln", "-s", "/busybox2/sh", "/bin/sh"]

FROM kaniko-base-slim AS kaniko-slim

COPY --from=builder /src/out/executor /kaniko/executor

ENTRYPOINT ["/kaniko/executor"]

### SOURCE STAGES ###

FROM golang AS ca-certificates-source
WORKDIR /sources/ca-certificates
RUN snapshot=$(sed -n 's|^# \(http://snapshot.debian.org/archive/debian/[0-9TZ]*\)$|\1|p' /etc/apt/sources.list.d/debian.sources | head -n1) \
    && printf 'Types: deb-src\nURIs: %s\nSuites: trixie trixie-updates\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.pgp\nCheck-Valid-Until: no\n' "$snapshot" \
        > /etc/apt/sources.list.d/debian-src.sources \
    && apt-get update \
    && apt-get source --download-only "ca-certificates=$(dpkg-query -W -f='${Version}' ca-certificates)" \
    && rm -rf /var/lib/apt/lists/*

# Keep in sync with the busybox image, the revisions are its per-platform
# org.opencontainers.image.revision annotations.
FROM scratch AS busybox-source-amd64
ADD --checksum=sha256:c53d3539454a86ce9db4ca3625439d600a5de95474807dd022845618c5d0d79d \
    https://github.com/docker-library/busybox/archive/066dbf90090ab2dbb905310fac2fa10ea39a7ba6.tar.gz \
    /busybox/docker-library-busybox.tar.gz
FROM scratch AS busybox-source-arm
ADD --checksum=sha256:9d1a71cdfc193b746da9d733c9eba419e6b1e0c38c0890dba88e5e8bdde690a1 \
    https://github.com/docker-library/busybox/archive/00b331050fbeb638579951c8360998e131c68f28.tar.gz \
    /busybox/docker-library-busybox.tar.gz
FROM scratch AS busybox-source-arm64
ADD --checksum=sha256:2ed2e9d73a59b16eeee82cd76da0456add7c3327f5344b0f8a9fc5ed933e277b \
    https://github.com/docker-library/busybox/archive/b6edf7d5c5200d629d7c3f7758293a3675471dde.tar.gz \
    /busybox/docker-library-busybox.tar.gz
FROM scratch AS busybox-source-s390x
ADD --checksum=sha256:3212d9b0d87845b97bd51f793864c9c168dc280ee90f03fa1a841e334d5f4be6 \
    https://github.com/docker-library/busybox/archive/8b4d2482716221b788fa1f00e0fdc5c496669843.tar.gz \
    /busybox/docker-library-busybox.tar.gz
FROM scratch AS busybox-source-ppc64le
ADD --checksum=sha256:5f405ad1e9ff7e4b58c2165abd9d0376ed867402dd2bb1902ac69338f59c561e \
    https://github.com/docker-library/busybox/archive/6e5781318fdd5e1400daa6661f3e8bda8b85f390.tar.gz \
    /busybox/docker-library-busybox.tar.gz
FROM scratch AS busybox-source-riscv64
ADD --checksum=sha256:469cb0bef5033f51388f0966ed404345b664d3130b547c96e17718ff6b045d77 \
    https://github.com/docker-library/busybox/archive/cb64779ccb8db12059f711141b6b1e67f6e2c580.tar.gz \
    /busybox/docker-library-busybox.tar.gz
FROM busybox-source-${TARGETARCH} AS busybox-source
ADD --checksum=sha256:34f9ea6ff8636f2c9241153b9114eefa9e65674a45318ae1ef95bb5f31c53bb2 \
    https://busybox.net/downloads/busybox-1.38.0.tar.bz2 /busybox/
ADD --checksum=sha256:d87d0cbb3690ae2c5d8cc218349fd8278b93855dd625deaf7ae50e320aad247c \
    https://github.com/alpinelinux/aports/raw/48b16204aeeda5bc1f87e49c6b8e23d9abb07c73/main/musl/getconf.c /busybox/

# The glibc the tini release binaries link statically. Ubuntu xenial, native on
# amd64 and from cross-toolchain-base on the other platforms.
FROM scratch AS glibc-source-amd64
ADD --checksum=sha256:422c3e17758e3378474bed63e2ed140364045c671f913fa99928a6eeff64adb6 \
    https://launchpad.net/ubuntu/+archive/primary/+sourcefiles/glibc/2.23-0ubuntu10/glibc_2.23-0ubuntu10.dsc /glibc/
ADD --checksum=sha256:bf6c528eeebefcacc295270068b79330c1fb2b22458ff66285b4175d23442c96 \
    https://launchpad.net/ubuntu/+archive/primary/+sourcefiles/glibc/2.23-0ubuntu10/glibc_2.23.orig.tar.xz /glibc/
ADD --checksum=sha256:4918baa16af87741a50aa349ebe12294f856d73bd052d8558f5385c0607fc05e \
    https://launchpad.net/ubuntu/+archive/primary/+sourcefiles/glibc/2.23-0ubuntu10/glibc_2.23-0ubuntu10.debian.tar.xz /glibc/
FROM scratch AS glibc-source-cross
ADD --checksum=sha256:647f33e229b3d838ff72b3dcb80242d2c6c61ff93aa9798009962b9a21992260 \
    https://launchpad.net/ubuntu/+archive/primary/+sourcefiles/glibc/2.23-0ubuntu3/glibc_2.23-0ubuntu3.dsc /glibc/
ADD --checksum=sha256:bf6c528eeebefcacc295270068b79330c1fb2b22458ff66285b4175d23442c96 \
    https://launchpad.net/ubuntu/+archive/primary/+sourcefiles/glibc/2.23-0ubuntu3/glibc_2.23.orig.tar.xz /glibc/
ADD --checksum=sha256:5e01dd448c8d78e850661ca88f5009f19011b00c6a7031f7578fe09c330a41d3 \
    https://launchpad.net/ubuntu/+archive/primary/+sourcefiles/glibc/2.23-0ubuntu3/glibc_2.23-0ubuntu3.debian.tar.xz /glibc/
ADD --checksum=sha256:11265ed751cb009555bf546809fc4168f731ff1f95da5e4534737adb84e33364 \
    https://launchpad.net/ubuntu/+archive/primary/+sourcefiles/cross-toolchain-base/10ubuntu1/cross-toolchain-base_10ubuntu1.dsc /glibc/
ADD --checksum=sha256:4ad675289604ff38a49f625e026acba11e87bcba1c4128db9b71f8688f89e056 \
    https://launchpad.net/ubuntu/+archive/primary/+sourcefiles/cross-toolchain-base/10ubuntu1/cross-toolchain-base_10ubuntu1.tar.xz /glibc/
FROM glibc-source-cross AS glibc-source-arm
FROM glibc-source-cross AS glibc-source-arm64
FROM glibc-source-cross AS glibc-source-s390x
FROM glibc-source-cross AS glibc-source-ppc64le
FROM scratch AS glibc-source-riscv64
FROM glibc-source-${TARGETARCH} AS glibc-source

# Keep in sync with the busybox image, the musl its docker-library builder linked
# from alpine:3.23 at build time.
FROM scratch AS musl-source
ADD --checksum=sha256:a9a118bbe84d8764da0ea0d28b3ab3fae8477fc7e4085d90102b8596fc7c75e4 \
    https://distfiles.alpinelinux.org/distfiles/v3.23/musl-1.2.5.tar.gz /musl/
ADD --checksum=sha256:05d8ce8d147c04bc2a23fe122516c7ee6ce68b947475c5b2621917452ad63d2a \
    "https://gitlab.alpinelinux.org/alpine/aports/-/archive/258256b3380c945961196d5dd80c9a0c3ae1b0d8/aports-258256b3380c945961196d5dd80c9a0c3ae1b0d8.tar.gz?path=main/musl" \
    /musl/aports-musl-1.2.5-r23.tar.gz

FROM scratch AS kaniko-sources
COPY . /kaniko/
COPY --from=ca-certificates-source /sources/ /
COPY --from=busybox-source / /
COPY --from=musl-source / /
COPY --from=glibc-source / /
ADD --checksum=sha256:0fd35a7030052acd9f58948d1d900fe1e432ee37103c5561554408bdac6bbf0d \
    https://github.com/krallin/tini/archive/refs/tags/v0.19.0.tar.gz /tini/tini-0.19.0.tar.gz
