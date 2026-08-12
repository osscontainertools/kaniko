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

# Generates htpasswd files and an nginx config for a reverse proxy
# that requires a different Basic-Auth credential per repository namespace
# (/v2/kaniko/patha/... vs /v2/kaniko/pathb/...)
# in front of the plain, unauthenticated local registry.
# A single registry with one global user cannot prove that
# a client selects credentials per repository path;
# this proxy is what TestPathScopedRegistryAuth builds that proof against.

set -euo pipefail

PROXY_DIR="/tmp/kaniko-path-scoped-auth-proxy"
mkdir -p "$PROXY_DIR"

# org-a-user:org-a-pass
[ -f "$PROXY_DIR/htpasswd-org-a" ] ||
  docker run --rm --entrypoint htpasswd httpd:2 \
    -Bbn org-a-user org-a-pass > "$PROXY_DIR/htpasswd-org-a"

# org-b-user:org-b-pass
[ -f "${PROXY_DIR}/htpasswd-org-b" ] ||
  docker run --rm --entrypoint htpasswd httpd:2 \
    -Bbn org-b-user org-b-pass > "$PROXY_DIR/htpasswd-org-b"

# project-user:project-pass
[ -f "${PROXY_DIR}/htpasswd-project" ] ||
  docker run --rm --entrypoint htpasswd httpd:2 \
    -Bbn project-user project-pass > "$PROXY_DIR/htpasswd-project"

cat > "$PROXY_DIR/nginx.conf" <<'EOF'
server {
  listen 5002;
  client_max_body_size 0;

  location = /v2/ {
    proxy_pass http://host.docker.internal:5000/v2/;
  }

  location /v2/kaniko/patha/ {
    auth_basic "org-a";
    auth_basic_user_file /etc/nginx/htpasswd-org-a;
    proxy_pass http://host.docker.internal:5000;
    proxy_set_header Host $http_host;
  }

  location /v2/kaniko/pathb/ {
    auth_basic "org-b";
    auth_basic_user_file /etc/nginx/htpasswd-org-b;
    proxy_pass http://host.docker.internal:5000;
    proxy_set_header Host $http_host;
  }

  location /v2/kaniko/patha/project/ {
    auth_basic "project";
    auth_basic_user_file /etc/nginx/htpasswd-project;
    proxy_pass http://host.docker.internal:5000;
    proxy_set_header Host $http_host;
  }

  location /v2/ {
    proxy_pass http://host.docker.internal:5000;
    proxy_set_header Host $http_host;
  }
}
EOF
