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

FROM busybox

# 'context/secret' is excluded by .dockerignore and must not be accessible via bind mount
RUN --mount=type=bind,source=context,target=/tmp/bound \
    test -f /tmp/bound/foo \
    && test ! -f /tmp/bound/secret
