# Copyright 2026 OSS Container Tools All rights reserved.
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

FROM golang:1.27.4@sha256:5e8afd493a7124a8586e134d9b532731161d4b9becf9dee5a5956b107426f93d

RUN apt-get update \
    && apt-get install -y --no-install-recommends bzip2 openssl xz-utils zstd \
    && rm -rf /var/lib/apt/lists/*

# gopls builds only against its own golang.org/x/tools, which golangci-lint in the tools module overrides.
RUN --mount=type=cache,target=/root/.cache/go-build \
    go install golang.org/x/tools/gopls@v0.24.0

COPY go.mod go.sum /tools/
RUN --mount=type=cache,target=/root/.cache/go-build \
    go -C /tools install \
        github.com/reproducible-containers/diffoci/cmd/diffoci \
        github.com/go-delve/delve/cmd/dlv
