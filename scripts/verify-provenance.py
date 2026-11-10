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

"""Verify BuildKit-emitted SLSA provenance against expected-dependencies manifest.

Accepts a provenance file, a directory containing provenance*.json, or '-' for stdin.
Verifies every resolved file and, when EXPECTED_PLATFORMS is set, requires one file
per expected platform.
"""

import argparse
import json
import os
import sys


def parse_platforms(raw):
    return [p.strip() for p in (raw or "").split(",") if p.strip()]


def find_provenance_files(directory):
    found = []
    try:
        names = sorted(os.listdir(directory))
    except OSError:
        return found
    for name in names:
        if name == "provenance.json" or (name.startswith("provenance") and name.endswith(".json")):
            full = os.path.join(directory, name)
            if os.path.isfile(full):
                found.append(full)
    return found


def platform_dir_candidates(root, platform):
    # Established platform naming conventions for export dirs (deterministic order).
    norm = platform.replace("\\", "/").strip("/")
    variants = [
        norm,
        norm.replace("/", "_"),
        norm.replace("/", "-"),
    ]
    if norm.startswith("linux/"):
        rest = norm[len("linux/"):]
        variants.extend([
            rest,
            rest.replace("/", "_"),
            rest.replace("/", "-"),
        ])
    seen = set()
    out = []
    for v in variants:
        if not v or v in seen:
            continue
        seen.add(v)
        cand = os.path.join(root, v)
        if os.path.isdir(cand):
            out.append(cand)
    return out


def resolve_inputs(input_path, expected_platforms):
    if input_path == "-":
        return ["-"]
    if os.path.isdir(input_path):
        found = find_provenance_files(input_path)
        if not found:
            plats = parse_platforms(expected_platforms)
            if plats:
                for plat in plats:
                    pf = None
                    for pdir in platform_dir_candidates(input_path, plat):
                        pfiles = find_provenance_files(pdir)
                        if pfiles:
                            pf = pfiles[0]
                            break
                    if pf:
                        found.append(pf)
        if not found:
            print(f"ERROR: no provenance*.json found under directory: {input_path}", file=sys.stderr)
            sys.exit(1)
        plats = parse_platforms(expected_platforms)
        if plats and len(found) != len(plats):
            print(f"ERROR: provenance file count {len(found)} does not match expected platforms {len(plats)} ({expected_platforms}). Found:", file=sys.stderr)
            for f in found:
                print(f"  {f}", file=sys.stderr)
            sys.exit(1)
        return found
    if os.path.isfile(input_path):
        return [input_path]
    print(f"ERROR: provenance input not found: {input_path}", file=sys.stderr)
    sys.exit(1)


def load_provenance(path):
    if path == "-":
        raw = sys.stdin.read()
    else:
        with open(path) as f:
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
            return None
    return prov


def unwrap(obj):
    if isinstance(obj, dict):
        for k in ("SLSA", "slsa", "provenance"):
            if k in obj:
                return unwrap(obj[k])
    return obj


def verify_one(prov_path, manifest):
    prov = load_provenance(prov_path)
    if prov is None:
        return False

    errors = []
    notes = []

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
        return False

    for sub in subjects:
        if isinstance(sub, dict):
            print(f"Subject: {sub.get('name','?')} digest={sub.get('digest')}")

    completeness = {}
    resolved = {}
    has_v1_resolved_deps_field = False
    has_request_param = False
    request_v1_vals = []

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
        else:
            errors.append(f"resolved HTTP(S) dependency missing sha256 digest: {uri} (digests={digests!r})")

    for pred in predicates:
        meta = pred.get("metadata") or {}
        for k, v in ((meta.get("completeness") or {}).items()):
            completeness.setdefault(k, []).append(v)
        rdmeta = ((pred.get("runDetails") or {}).get("metadata") or {})
        for k, v in ((rdmeta.get("completeness") or {}).items()):
            completeness.setdefault(k, []).append(v)
        bk_comp = rdmeta.get("buildkit_completeness") or {}
        if "request" in bk_comp:
            request_v1_vals.append(bk_comp.get("request"))
            completeness.setdefault("request", []).append(bk_comp.get("request"))
        if "resolvedDependencies" in bk_comp:
            completeness.setdefault("resolvedDependencies", []).append(bk_comp.get("resolvedDependencies"))
        if "materials" in bk_comp:
            completeness.setdefault("materials", []).append(bk_comp.get("materials"))
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

    def flag_vals(name):
        return completeness.get(name)

    def has_true(name):
        vals = flag_vals(name)
        return vals is not None and any(v is True for v in vals)

    def has_explicit_false(name):
        vals = flag_vals(name)
        return vals is not None and any(v is False for v in vals)

    print(f"Completeness flags seen: { {k:v for k,v in completeness.items()} }")
    print(f"Resolved HTTP(S) dependencies found: {len(resolved)}")

    rd_true = has_true("resolvedDependencies") or has_true("materials")
    rd_false = has_explicit_false("resolvedDependencies") or has_explicit_false("materials")
    if rd_false:
        errors.append(f"buildkit_completeness.resolvedDependencies/materials is explicitly false: {completeness}")
    elif rd_true:
        print("OK: resolvedDependencies/materials completeness asserted")
    elif has_v1_resolved_deps_field:
        notes.append("INFO: v0.2 completeness.resolvedDependencies flag absent (SLSA v1/BuildKit shape); accepting buildDefinition.resolvedDependencies presence and verifying contents against manifest.")
        print("OK: buildDefinition.resolvedDependencies present (v1 shape); verifying contents")
    else:
        errors.append(
            f"buildkit_completeness.resolvedDependencies not asserted and no buildDefinition.resolvedDependencies field (flags: {completeness}, v1_field={has_v1_resolved_deps_field}). "
            "Provenance does not prove all dependencies were resolved; refusing to pass."
        )

    if any(v is False for v in request_v1_vals):
        errors.append("provenance rejected: runDetails.metadata.buildkit_completeness.request is explicitly false")
    elif has_request_param:
        print("OK: request present in provenance externalParameters")
    else:
        errors.append("provenance request not present (buildDefinition.externalParameters.request/configSource missing); refusing to pass")

    expected = manifest.get("dependencies") or []
    manifest_by_url = {d["url"]: str(d["sha256"]).lower() for d in expected if "url" in d and "sha256" in d}
    if not manifest_by_url:
        errors.append("manifest contains no dependencies")
    else:
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
        return False
    print(f"\nPASSED: provenance verified against {len(manifest_by_url)} manifest entries.")
    return True


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("input", help="provenance.json, export directory, or '-' for stdin")
    ap.add_argument("manifest", nargs="?", default="deploy/expected-dependencies.json")
    ap.add_argument("--expected-platforms", default=os.environ.get("EXPECTED_PLATFORMS", ""))
    args = ap.parse_args()

    if not os.path.isfile(args.manifest):
        print(f"ERROR: manifest file not found: {args.manifest}", file=sys.stderr)
        sys.exit(1)
    with open(args.manifest) as f:
        manifest = json.load(f)

    files = resolve_inputs(args.input, args.expected_platforms)
    status = 0
    for prov in files:
        label = prov if prov != "-" else "stdin"
        print(f"== Verifying provenance: {label} ==")
        if not verify_one(prov, manifest):
            status = 1
    sys.exit(status)


if __name__ == "__main__":
    main()
