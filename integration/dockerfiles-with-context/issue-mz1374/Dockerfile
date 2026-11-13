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

FROM busybox AS src
RUN mkdir /real && echo stage-content > /real/value && ln -s /real/value /value

FROM busybox
RUN mkdir /real && echo wrong > /real/value

# Symlinked bind-mount source must resolve to the target contents.
RUN --mount=type=bind,source=context/value,target=/mnt/value test "$(cat /mnt/value)" = context-content
# An absolute link resolves inside the source stage, not the stage running the RUN.
RUN --mount=type=bind,from=src,source=/value,target=/mnt/stage test "$(cat /mnt/stage)" = stage-content
