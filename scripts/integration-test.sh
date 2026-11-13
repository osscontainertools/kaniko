#!/usr/bin/env bash
# Copyright 2018 Google LLC
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

set -e

IMAGE_REPO="${IMAGE_REPO:-localhost:5000}"

docker version

echo "Running integration tests..."
make out/executor
make out/warmer

FLAGS=(
  "--timeout=50m"
  "-parallel=${TEST_PARALLELISM:-8}"
)

if [[ -n ${DOCKERFILE_PATTERN} ]]; then
  FLAGS+=("--dockerfiles-pattern=${DOCKERFILE_PATTERN}")
fi

"$(dirname "$0")/local-registries.sh"

FLAGS+=(
  "--repo=${IMAGE_REPO}"
)

if [[ -n ${COVERAGE_DIR} ]]; then
  FLAGS+=("--coverage-dir=${COVERAGE_DIR}")
fi

export TLS_REGISTRY_CERT="/tmp/kaniko-tls-registry/tls.crt"
go test -v ./integration/... "${FLAGS[@]}" "$@"

