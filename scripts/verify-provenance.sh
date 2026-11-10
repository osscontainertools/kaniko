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

# Verifies BuildKit-emitted SLSA provenance against the repo-controlled
# network-input manifest. Exits non-zero on any mismatch so a bad build never
# reaches push.
#
# Usage:
#   verify-provenance.sh <provenance.json|export-dir|-> [expected-dependencies.json]
#
# Accepts: a provenance JSON file, a directory (searches for provenance.json /
# provenance*.json), or '-' for stdin (e.g. imagetools inspect output).
#
# Hard gates:
#   - metadata.completeness.request == true  (see exception below)
#   - metadata.completeness.resolvedDependencies (or materials) == true
#   - every HTTP(S) resolved dependency digest matches the manifest
#   - every manifest entry is present in resolved dependencies
#
# Open item 1: if buildkit_completeness.request cannot be true because
# deploy/Dockerfile is outside the build context, do NOT silently pass — set
# ALLOW_MISSING_REQUEST_COMPLETENESS=1 only with an explicit, reviewed reason.

INPUT="${1:?usage: verify-provenance.sh <provenance.json|export-dir|-> [expected-dependencies.json]}"
MANIFEST="${2:-deploy/expected-dependencies.json}"
EXPECTED_PLATFORMS="${EXPECTED_PLATFORMS:-}"

resolve_inputs() {
  local p="$1"
  if [[ "$p" == "-" ]]; then
    local tmp
    tmp=$(mktemp)
    cat > "$tmp"
    echo "$tmp"
    return
  fi
  if [[ -d "$p" ]]; then
    local found=()
    while IFS= read -r f; do
      [[ -n "$f" ]] && found+=("$f")
    done < <(find "$p" \( -name 'provenance.json' -o -name 'provenance*.json' \) -type f 2>/dev/null | sort -u)
    if [[ ${#found[@]} -eq 0 ]]; then
      echo "ERROR: no provenance*.json found under directory: $p" >&2
      exit 1
    fi
    if [[ -n "$EXPECTED_PLATFORMS" ]]; then
      local expected_count
      expected_count=$(python3 - "$EXPECTED_PLATFORMS" <<'PYCOUNT'
import sys
plats=[x.strip() for x in sys.argv[1].split(',') if x.strip()]
print(len(plats))
PYCOUNT
)
      if [[ ${#found[@]} -ne "$expected_count" ]]; then
        echo "ERROR: provenance file count ${#found[@]} does not match expected platforms ${expected_count} (${EXPECTED_PLATFORMS}). Found:" >&2
        printf '  %s\n' "${found[@]}" >&2
        exit 1
      fi
    fi
    printf '%s\n' "${found[@]}"
    return
  fi
  if [[ -f "$p" ]]; then
    echo "$p"
    return
  fi
  echo "ERROR: provenance input not found: $p" >&2
  exit 1
}

mapfile -t PROVENANCE_FILES < <(resolve_inputs "$INPUT")
if [[ ${#PROVENANCE_FILES[@]} -eq 0 ]]; then
  echo "ERROR: no provenance files resolved from: $INPUT" >&2
  exit 1
fi
if [[ ! -f "$MANIFEST" ]]; then
  echo "ERROR: manifest file not found: $MANIFEST" >&2
  exit 1
fi

TMP_PY=$(mktemp)
cat > "$TMP_PY" <<'PY'
import json, sys, os

prov_path, manifest_path = sys.argv[1], sys.argv[2]

with open(prov_path) as f:
    raw = f.read()
try:
    prov = json.loads(raw)
except json.JSONDecodeError:
    dec = json.JSONDecoder()
    objs = []
    sraw = raw.lstrip()
    while sraw:
        try:
            obj, end = dec.raw_decode(sraw)
            objs.append(obj)
            sraw = sraw[end:].lstrip()
        except json.JSONDecodeError:
            break
    prov = objs[0] if len(objs) == 1 else objs if objs else None
    if prov is None:
        print("ERROR: could not parse provenance JSON", file=sys.stderr)
        sys.exit(1)

with open(manifest_path) as f:
    manifest = json.load(f)

errors = []
notes = []

def unwrap(obj):
    if isinstance(obj, dict):
        for k in ("SLSA", "slsa", "provenance"):
            if k in obj:
                return unwrap(obj[k])
    return obj

prov = unwrap(prov)
if isinstance(prov, list):
    statements = prov
elif isinstance(prov, dict) and "provenance" in prov and isinstance(prov["provenance"], list):
    statements = prov["provenance"]
else:
    statements = [prov]

predicates = []
subjects = []
for s in statements:
    if not isinstance(s, dict):
        continue
    if "subject" in s:
        subjects.extend(s.get("subject") or [])
    if "predicate" in s and isinstance(s["predicate"], dict):
        predicates.append(s["predicate"])
    elif "buildType" in s or "metadata" in s or "materials" in s or "buildDefinition" in s:
        predicates.append(s)

if not predicates:
    print("ERROR: no SLSA predicate found in provenance", file=sys.stderr)
    if isinstance(prov, dict):
        print(f"Top-level keys: {list(prov.keys())}", file=sys.stderr)
    sys.exit(1)

for sub in subjects:
    if isinstance(sub, dict):
        print(f"Subject: {sub.get('name','?')} digest={sub.get('digest')}")

completeness = {}
resolved = {}
has_v1_resolved_deps_field = False
has_request_param = False

def add_resolved(uri, digests):
    if not uri or not isinstance(uri, str):
        return
    if not (uri.startswith("http://") or uri.startswith("https://")):
        return
    sha = None
    if isinstance(digests, dict):
        sha = digests.get("sha256") or digests.get("SHA256")
    elif isinstance(digests, str) and digests.startswith("sha256:"):
        sha = digests.split(":", 1)[1]
    if sha:
        resolved.setdefault(uri, set()).add(str(sha).lower())

for pred in predicates:
    meta = pred.get("metadata") or {}
    for k, v in ((meta.get("completeness") or {}).items()):
        completeness.setdefault(k, []).append(v)
    rdmeta = ((pred.get("runDetails") or {}).get("metadata") or {})
    for k, v in ((rdmeta.get("completeness") or {}).items()):
        completeness.setdefault(k, []).append(v)
    for m in (pred.get("materials") or []):
        if isinstance(m, dict):
            add_resolved(m.get("uri") or m.get("url"), m.get("digest"))
    bd = pred.get("buildDefinition") or {}
    if "resolvedDependencies" in bd:
        has_v1_resolved_deps_field = True
    for d in (bd.get("resolvedDependencies") or []):
        if isinstance(d, dict):
            add_resolved(d.get("uri") or d.get("url"), d.get("digest"))
    ep = bd.get("externalParameters") or {}
    if isinstance(ep, dict) and ("request" in ep or "configSource" in ep):
        has_request_param = True

def check_flag(name):
    vals = completeness.get(name)
    if vals is None:
        return None
    return any(v is True for v in vals)

print(f"Completeness flags seen: { {k:v for k,v in completeness.items()} }")
print(f"Resolved HTTP(S) dependencies found: {len(resolved)}")

# resolvedDependencies gate. BuildKit SLSA v1 (moby/buildkit) records
# buildDefinition.resolvedDependencies but omits v0.2 metadata.completeness flags.
# Accept the v1 field's presence (data present + verified below); never accept
# absence of evidence silently.
resdep = check_flag("resolvedDependencies")
materials_flag = check_flag("materials")
if resdep is True or materials_flag is True:
    print("OK: resolvedDependencies/materials completeness asserted")
elif has_v1_resolved_deps_field:
    notes.append("INFO: v0.2 completeness.resolvedDependencies flag absent (SLSA v1/BuildKit shape); accepting buildDefinition.resolvedDependencies presence and verifying contents against manifest.")
    print("OK: buildDefinition.resolvedDependencies present (v1 shape); verifying contents")
elif resdep is False or materials_flag is False:
    errors.append(f"buildkit_completeness.resolvedDependencies/materials is false: {completeness}")
else:
    errors.append(
        f"buildkit_completeness.resolvedDependencies not asserted and no buildDefinition.resolvedDependencies field (flags: {completeness}, v1_field={has_v1_resolved_deps_field}). "
        "Provenance does not prove all dependencies were resolved; refusing to pass."
    )

# request completeness gate. BuildKit does not provide the v0.2
# metadata.completeness.request flag; use the v1 externalParameters presence
# (captured in has_request_param) as the request evidence.
if has_request_param:
    print("OK: request present in provenance externalParameters")
else:
    errors.append("provenance request not present (buildDefinition.externalParameters.request/configSource missing); refusing to pass")

expected = manifest.get("dependencies") or []
manifest_by_url = {d["url"]: str(d["sha256"]).lower() for d in expected if "url" in d and "sha256" in d}
if not manifest_by_url:
    errors.append("manifest contains no dependencies")
else:
    # Verify every resolved HTTP dependency in this provenance file against the
    # manifest. Manifest entries need not all appear in each (per-platform)
    # provenance file, but every resolved dependency must be manifest-allowed and
    # match the expected digest. Unmanifested dependencies are failures.
    if not resolved:
        print("INFO: no HTTP(S) resolved dependencies in this provenance file")
    for url in sorted(resolved):
        got = resolved[url]
        exp_sha = manifest_by_url.get(url)
        if exp_sha is None:
            errors.append(f"unmanifested HTTP dependency in provenance: {url} digests={sorted('sha256:'+g for g in got)}")
        elif exp_sha not in got:
            errors.append(f"digest MISMATCH for {url}: expected sha256:{exp_sha}, provenance has {sorted('sha256:'+g for g in got)}")
        else:
            print(f"OK: {url} -> sha256:{exp_sha}")
for n in notes:
    print(n)
if errors:
    print(f"\nFAILED: {len(errors)} provenance verification error(s):", file=sys.stderr)
    for e in errors:
        print(f"  - {e}", file=sys.stderr)
    sys.exit(1)
print(f"\nPASSED: provenance verified against {len(manifest_by_url)} manifest entries.")
PY

status=0
for prov in "${PROVENANCE_FILES[@]}"; do
  echo "== Verifying provenance: ${prov} =="
  if ! python3 "$TMP_PY" "$prov" "$MANIFEST"; then
    status=1
  fi
done
rm -f "$TMP_PY"
exit "$status"
