#!/usr/bin/env bash

# Copyright 2026 OSS Container Tools
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
set -euo pipefail

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

RED='\033[0;31m'
GREEN='\033[0;32m'
RESET='\033[0m'

FLAGS=(
  "-cover"
  "-coverprofile=out/coverage.out"
  "-timeout=120s"
  "-v"
)
EXTRA_FLAGS=()

if [[ -n ${DOCKERFILE_PATTERN:-} ]]; then
  EXTRA_FLAGS+=("--dockerfiles-pattern=${DOCKERFILE_PATTERN}")
fi

echo "Running go tests..."
go test ${FLAGS[@]} ./golden/... ${EXTRA_FLAGS[@]} \
  | sed ''/PASS/s//$(printf "${GREEN}PASS${RESET}")/'' \
  | sed ''/FAIL/s//$(printf "${RED}FAIL${RESET}")/''
