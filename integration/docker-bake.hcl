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

# Images the integration tests need before they start.
# Tags must match the image names in integration/images.go and newIntegrationTestConfig.

variable "IMAGE_REPO" {
  default = "localhost:5000/"
}

variable "COVER" {
  default = ""
}

group "default" {
  targets = [
    "executor",
    "warmer",
    "alpine-executor",
    "executor-moved",
    "executor-tainted",
    "executor-bake-context",
    "busybox-base",
    "alpine-base",
    "onbuild-base",
    "onbuild-copy",
    "hardlink-base",
    "hijack-base",
    "malformed-oci-base",
    "nvidia-operator-base",
    "mz1073-base",
    "mounted-symlink-base",
    "mz1113-base",
    "mz1172-base",
    "single-manifest-base",
  ]
}

target "_deploy" {
  context    = ".."
  dockerfile = "deploy/Dockerfile"
  args = {
    COVER = COVER
    RACE  = "1"
  }
  output = ["type=docker"]
}

target "executor" {
  inherits = ["_deploy"]
  target   = "kaniko-executor"
  tags     = ["executor-image"]
}

target "warmer" {
  inherits = ["_deploy"]
  target   = "kaniko-warmer"
  tags     = ["warmer-image"]
}

target "alpine-executor" {
  inherits = ["_deploy"]
  target   = "kaniko-alpine"
  tags     = ["alpine-image"]
}

target "_from_executor" {
  context  = "."
  contexts = { executor-image = "target:executor" }
  output   = ["type=docker"]
}

target "executor-moved" {
  inherits   = ["_from_executor"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz444"
  target     = "kaniko"
  args       = { COVER = COVER }
  tags       = ["executor-image-moved"]
}

target "executor-tainted" {
  inherits   = ["_from_executor"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz455"
  target     = "kaniko"
  args       = { COVER = COVER }
  tags       = ["executor-image-tainted"]
}

target "executor-bake-context" {
  inherits   = ["_from_executor"]
  context    = "bakefiles/test_bake_unmounted_context"
  dockerfile = "Dockerfile.executor"
  tags       = ["executor-image-bake-context"]
}

target "_base" {
  context = "."
  output  = ["type=registry"]
}

target "busybox-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_busybox_base"
  tags       = ["${IMAGE_REPO}busybox:latest"]
}

target "alpine-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_alpine_base"
  tags       = ["${IMAGE_REPO}alpine:latest"]
}

target "onbuild-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_onbuild_base"
  tags       = ["${IMAGE_REPO}onbuild-base:latest"]
}

target "onbuild-copy" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz332"
  target     = "onbuild"
  tags       = ["${IMAGE_REPO}onbuild-copy:latest"]
}

target "hardlink-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_hardlink_base"
  tags       = ["${IMAGE_REPO}hardlink-base:latest"]
}

target "hijack-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz560"
  target     = "base"
  tags       = ["${IMAGE_REPO}hijack:latest"]
}

target "malformed-oci-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz725"
  target     = "base"
  tags       = ["${IMAGE_REPO}malformed-oci:latest"]
}

target "nvidia-operator-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz753"
  target     = "base"
  tags       = ["${IMAGE_REPO}nvidia-operator-base:latest"]
}

target "mz1073-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz1073"
  target     = "base"
  tags       = ["${IMAGE_REPO}test-issue-mz1073-base:latest"]
}

target "mounted-symlink-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_mounted_symlink_kaniko"
  target     = "base"
  tags       = ["${IMAGE_REPO}mounted-symlink-kaniko-base:latest"]
}

target "mz1113-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz1113"
  target     = "base"
  tags       = ["${IMAGE_REPO}test-issue-mz1113-base:latest"]
}

target "mz1172-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_mz1172"
  target     = "base"
  tags       = ["${IMAGE_REPO}test-issue-mz1172-base:latest"]
}

target "single-manifest-base" {
  inherits   = ["_base"]
  dockerfile = "dockerfiles/Dockerfile_test_issue_2567"
  target     = "base"
  args       = { IMAGE_REPO = IMAGE_REPO }
  contexts   = { "${IMAGE_REPO}busybox" = "target:busybox-base" }
  attest     = ["type=provenance,disabled=true"]
  output     = ["type=registry,oci-mediatypes=false"]
  tags       = ["${IMAGE_REPO}single-manifest-base:latest"]
}
