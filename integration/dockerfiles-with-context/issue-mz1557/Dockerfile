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

# mz1557: ADD resolves its sources and dest, then hands them to COPY, which resolves them again.
# Copies the wrong files on v1.29.2.
# The second pass reads a matched name as a pattern, so a[1].txt also matches the decoy a1.txt.
# It expands the $y in a matched x$y.txt, so the decoy x.txt is copied instead.
# A dest that expands to a literal $M is expanded to stage.
ARG IMAGE_REPO
FROM ${IMAGE_REPO}busybox
ARG M=stage
ENV LIT=\$M
ADD context/a?1?.txt context/x?y.txt /glob/
ADD context/plain.txt /lit/${LIT}
