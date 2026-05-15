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

TLS_REG_DIR="/tmp/kaniko-tls-registry"
mkdir -p "${TLS_REG_DIR}"

if [[ ! -f "${TLS_REG_DIR}/tls.crt" ]]; then
  openssl req -x509 -newkey rsa:2048 \
    -keyout "${TLS_REG_DIR}/tls.key" \
    -out "${TLS_REG_DIR}/tls.crt" \
    -days 3650 -nodes \
    -subj "/CN=127.0.0.2" \
    -addext "subjectAltName=IP:127.0.0.2" \
    2>/dev/null
fi

if [[ ! -f "${TLS_REG_DIR}/htpasswd" ]]; then
  # kanikotest:kanikotest
  docker run --rm --entrypoint htpasswd httpd:2 -Bbn kanikotest kanikotest \
    > "${TLS_REG_DIR}/htpasswd"
fi
