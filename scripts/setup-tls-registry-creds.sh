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

# ggcr refuses a token realm on a loopback IP literal, so the auth server is reached
# under the name localhost and the cert has to cover it.
if [[ ! -f "${TLS_REG_DIR}/tls.crt" ]] || ! openssl x509 -in "${TLS_REG_DIR}/tls.crt" -noout -text | grep -q "DNS:localhost"; then
  openssl req -x509 -newkey rsa:2048 \
    -keyout "${TLS_REG_DIR}/tls.key" \
    -out "${TLS_REG_DIR}/tls.crt" \
    -days 3650 -nodes \
    -subj "/CN=127.0.0.2" \
    -addext "subjectAltName=IP:127.0.0.2,DNS:localhost" \
    2>/dev/null
fi

function bcrypt {
  docker run --rm --entrypoint htpasswd httpd:2 -Bbn "$1" "$1" | cut -d: -f2-
}

if [[ ! -f "${TLS_REG_DIR}/auth_config.yml" ]]; then
  cat > "${TLS_REG_DIR}/auth_config.yml" <<EOF
server:
  addr: ":5002"
  certificate: /certs/tls.crt
  key: /certs/tls.key

token:
  issuer: kaniko-test-auth
  expiration: 900

users:
  kanikotest:
    password: "$(bcrypt kanikotest)"
  usera:
    password: "$(bcrypt usera)"
  userb:
    password: "$(bcrypt userb)"

acl:
  - match: {account: kanikotest}
    actions: ["*"]
  - match: {account: usera, name: "/^usera\\\\/.*/"}
    actions: ["*"]
  - match: {account: userb, name: "/^userb\\\\/.*/"}
    actions: ["*"]
EOF
fi
