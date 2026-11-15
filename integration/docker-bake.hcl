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
  targets = ["kaniko", "kaniko-variant", "executor-image-bake-context", "base", "single-manifest-base"]
}

target "kaniko" {
  name = item.tag
  matrix = {
    item = [
      { tag = "executor-image", target = "kaniko-executor" },
      { tag = "warmer-image", target = "kaniko-warmer" },
      { tag = "alpine-image", target = "kaniko-alpine" },
    ]
  }
  context    = ".."
  dockerfile = "deploy/Dockerfile"
  target     = item.target
  args       = { COVER = COVER, RACE = "1" }
  tags       = [item.tag]
  output     = ["type=docker"]
}

target "kaniko-variant" {
  name = item.tag
  matrix = {
    item = [
      { tag = "executor-image-moved", file = "Dockerfile_test_issue_mz444" },
      { tag = "executor-image-tainted", file = "Dockerfile_test_issue_mz455" },
    ]
  }
  context    = "."
  contexts   = { executor-image = "target:executor-image" }
  dockerfile = "dockerfiles/${item.file}"
  target     = "kaniko"
  args       = { COVER = COVER }
  tags       = [item.tag]
  output     = ["type=docker"]
}

target "executor-image-bake-context" {
  context    = "bakefiles/test_bake_unmounted_context"
  contexts   = { executor-image = "target:executor-image" }
  dockerfile = "Dockerfile.executor"
  tags       = ["executor-image-bake-context"]
  output     = ["type=docker"]
}

target "base" {
  name = item.name
  matrix = {
    item = [
      { name = "busybox", file = "Dockerfile_busybox_base", target = "" },
      { name = "alpine", file = "Dockerfile_alpine_base", target = "" },
      { name = "onbuild-base", file = "Dockerfile_onbuild_base", target = "" },
      { name = "onbuild-copy", file = "Dockerfile_test_issue_mz332", target = "onbuild" },
      { name = "hardlink-base", file = "Dockerfile_hardlink_base", target = "" },
      { name = "hijack", file = "Dockerfile_test_issue_mz560", target = "base" },
      { name = "malformed-oci", file = "Dockerfile_test_issue_mz725", target = "base" },
      { name = "nvidia-operator-base", file = "Dockerfile_test_issue_mz753", target = "base" },
      { name = "test-issue-mz1073-base", file = "Dockerfile_test_issue_mz1073", target = "base" },
      { name = "mounted-symlink-kaniko-base", file = "Dockerfile_test_mounted_symlink_kaniko", target = "base" },
      { name = "test-issue-mz1113-base", file = "Dockerfile_test_issue_mz1113", target = "base" },
      { name = "test-issue-mz1172-base", file = "Dockerfile_test_issue_mz1172", target = "base" },
    ]
  }
  context    = "."
  dockerfile = "dockerfiles/${item.file}"
  target     = item.target
  tags       = ["${IMAGE_REPO}${item.name}:latest"]
  output     = ["type=registry"]
}

target "single-manifest-base" {
  context    = "."
  contexts   = { "${IMAGE_REPO}busybox" = "target:busybox" }
  dockerfile = "dockerfiles/Dockerfile_test_issue_2567"
  target     = "base"
  args       = { IMAGE_REPO = IMAGE_REPO }
  attest     = ["type=provenance,disabled=true"]
  tags       = ["${IMAGE_REPO}single-manifest-base:latest"]
  output     = ["type=registry,oci-mediatypes=false"]
}
