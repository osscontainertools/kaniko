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

# mz1374: a RUN bind mount of a symlink source mounts the link instead of its target.
# Mounts the link since v1.27.3, cross-stage since 236826dbe behind FF_KANIKO_RUN_CROSS_STAGE_BIND_MOUNT.
# context/value links to real/value in the context.
# The src stage links /value to /real/value, and the RUN stage has its own /real/value.
# A mounted absolute link would read the RUN stage's file.
# The context is passed as /workspace/../workspace, which SecureJoin rejects unless cleaned.
# The test steps stay because the context harness ignores file content.
ARG IMAGE_REPO
FROM ${IMAGE_REPO}busybox AS src
RUN mkdir /real && echo stage-content > /real/value && ln -s /real/value /value

FROM ${IMAGE_REPO}busybox
RUN mkdir /real && echo wrong > /real/value
RUN --mount=type=bind,source=context/value,target=/mnt/value test "$(cat /mnt/value)" = context-content
RUN --mount=type=bind,from=src,source=/value,target=/mnt/stage test "$(cat /mnt/stage)" = stage-content
