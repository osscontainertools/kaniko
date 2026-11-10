#!/usr/bin/env python3

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

"""Generate deploy/expected-dependencies.json from deploy/Dockerfile.

Parses network `ADD --checksum=sha256:<hex> <https://...>` instructions and
emits the repo-controlled manifest the provenance verifier checks against.
deploy/Dockerfile is the single source of truth for these pins.

Usage:
  scripts/generate-expected-dependencies.py [Dockerfile] [output.json]
Defaults: deploy/Dockerfile -> deploy/expected-dependencies.json
"""
import json
import os
import re
import sys

dockerfile = sys.argv[1] if len(sys.argv) > 1 else "deploy/Dockerfile"
out_path = sys.argv[2] if len(sys.argv) > 2 else "deploy/expected-dependencies.json"

with open(dockerfile) as f:
    # join backslash-continued lines so ADD args span lines
    text = f.read().replace("\\\n", " ")

deps = []
for m in re.finditer(
    r"\bADD\b(?P<args>[^#\n]*?--checksum=sha256:(?P<sha>[0-9a-fA-F]{64})[^#\n]*?(?P<url>https?://\S+))",
    text,
):
    sha = m.group("sha").lower()
    url = m.group("url").rstrip()  # strip any trailing whitespace
    deps.append({"url": url, "sha256": sha})

# de-dup by (url, sha) preserving order
seen = set()
uniq = []
for d in deps:
    k = (d["url"], d["sha256"])
    if k not in seen:
        seen.add(k)
        uniq.append(d)

if not uniq:
    print(f"ERROR: no network ADD --checksum entries found in {dockerfile}", file=sys.stderr)
    sys.exit(1)

doc = {
    "version": 1,
    "description": (
        "Generated from deploy/Dockerfile (single source of truth) by "
        "scripts/generate-expected-dependencies.py. Do not edit by hand; "
        "run `make generate-expected-dependencies` and commit. "
        "The provenance verifier checks BuildKit resolvedDependencies against this manifest."
    ),
    "dependencies": uniq,
}

os.makedirs(os.path.dirname(out_path) or ".", exist_ok=True)
with open(out_path, "w") as f:
    json.dump(doc, f, indent=2)
    f.write("\n")

print(f"Wrote {len(uniq)} dependencies to {out_path}")
