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

function start_local_registry {
  docker start registry || docker run --name registry -d -p 5000:5000 registry:2
}

# TODO: to get this working, we need a way to override the gcs endpoint of kaniko at runtime
# If this is done, integration test main includes flags --gcs-endpoint and --disable-gcs-auth
# to mock the gcs endpoints and upload files to the fake-gcs-server
function start_fake_gcs_server {
  docker start fake-gcs-server || docker run -d -p 4443:4443 --name fake-gcs-server fsouza/fake-gcs-server -scheme http
}

IMAGE_REPO="${IMAGE_REPO:-gcr.io/kaniko-test}"

docker version

echo "Running integration tests..."
make out/executor
make out/warmer

FLAGS=(
  "--timeout=50m"
)

if [[ -n ${DOCKERFILE_PATTERN} ]]; then
  FLAGS+=("--dockerfiles-pattern=${DOCKERFILE_PATTERN}")
fi

if [[ -n ${LOCAL} ]]; then
  echo "running in local mode, mocking registry and gcs bucket..."
  start_local_registry

  IMAGE_REPO="localhost:5000"
  GCS_BUCKET=""
fi

FLAGS+=(
  "--bucket=${GCS_BUCKET}"
  "--repo=${IMAGE_REPO}"
)

if [[ -n ${COVERAGE_DIR} ]]; then
  FLAGS+=("--coverage-dir=${COVERAGE_DIR}")
fi

go test ./integration/... "${FLAGS[@]}" "$@"

if [[ -n ${COVERAGE_DIR} ]]; then
  # Coverage files are written as root by the container; fix ownership so the
  # calling user can read and process them.
  sudo chown -R "$(id -u):$(id -g)" "${COVERAGE_DIR}" 2>/dev/null || true
  merged="${COVERAGE_DIR}-merged"
  mkdir -p "${merged}"
  go tool covdata merge -i "${COVERAGE_DIR}" -o "${merged}"
  go tool covdata textfmt -i "${merged}" -o "${COVERAGE_DIR}/coverage.txt"
  echo "Integration coverage written to ${COVERAGE_DIR}/coverage.txt"
fi
