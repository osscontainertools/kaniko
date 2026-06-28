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
  docker start registry 2>/dev/null || docker run --name registry -d -p 5000:5000 registry:2
}

function start_local_tls_registry {
  local dir="/tmp/kaniko-tls-registry"
  "$(dirname "$0")/setup-tls-registry-creds.sh"
  if ! docker start kaniko-tls-registry 2>/dev/null; then
    docker rm -f kaniko-tls-registry 2>/dev/null || true
    docker run -d --name kaniko-tls-registry \
      -p 127.0.0.2:5001:5000 \
      -v "${dir}/tls.crt:/certs/tls.crt:ro" \
      -v "${dir}/tls.key:/certs/tls.key:ro" \
      -v "${dir}/htpasswd:/auth/htpasswd:ro" \
      -e REGISTRY_HTTP_TLS_CERTIFICATE=/certs/tls.crt \
      -e REGISTRY_HTTP_TLS_KEY=/certs/tls.key \
      -e REGISTRY_AUTH=htpasswd \
      -e REGISTRY_AUTH_HTPASSWD_REALM=Registry \
      -e REGISTRY_AUTH_HTPASSWD_PATH=/auth/htpasswd \
      registry:2
  fi
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
  echo "running in local mode, mocking registry..."
  start_local_registry
  start_local_tls_registry

  IMAGE_REPO="localhost:5000"
fi

FLAGS+=(
  "--repo=${IMAGE_REPO}"
)

if [[ -n ${COVERAGE_DIR} ]]; then
  FLAGS+=("--coverage-dir=${COVERAGE_DIR}")
fi

export TLS_REGISTRY_CERT="/tmp/kaniko-tls-registry/tls.crt"
go test -v ./integration/... "${FLAGS[@]}" "$@"

