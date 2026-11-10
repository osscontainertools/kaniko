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

"""Attest and verify per-platform SBOMs from export/OCI layout.

Matches each exported SBOM to its platform manifest, attests the SPDX JSON
predicate to that manifest, and verifies signature + decoded payload (SPDX
schema, bound subject, exact predicate). Requires coverage for every expected
platform.
"""

import argparse
import base64
import glob
import json
import os
import re
import subprocess
import sys
import tempfile


def parse_platforms(raw):
    return [p.strip() for p in (raw or "").split(",") if p.strip()]


def normalize_platform(p):
    if not p:
        return None
    os_name = p.get("os") or "linux"
    arch = p.get("architecture") or ""
    variant = p.get("variant") or ""
    if arch in ("", "unknown") or os_name in ("", "unknown"):
        return None
    name = f"{os_name}/{arch}"
    if variant:
        name += f"/{variant}"
    return name


def load_blob(layout, digest):
    algo, h = digest.split(":", 1)
    with open(os.path.join(layout, "blobs", algo, h), "rb") as f:
        return f.read()


def collect_platforms(layout, manifests, digest_to_platform, platform_to_digest, depth=0):
    if depth > 8:
        return
    for m in manifests or []:
        mt = m.get("mediaType", "")
        if "index" in mt:
            doc = json.loads(load_blob(layout, m["digest"]))
            collect_platforms(layout, doc.get("manifests") or [], digest_to_platform, platform_to_digest, depth + 1)
            continue
        ref = (m.get("annotations") or {}).get("vnd.docker.reference.type")
        if ref == "attestation-manifest":
            continue
        pnorm = normalize_platform(m.get("platform") or {})
        if pnorm and m.get("digest"):
            digest_to_platform[m["digest"]] = pnorm
            platform_to_digest[pnorm] = m["digest"]


def decode_attestation_payloads(verify_output):
    stmts = []
    for line in (verify_output or "").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except Exception:
            continue
        payload = obj.get("payload")
        if not payload:
            continue
        try:
            decoded = base64.b64decode(payload)
            stmts.append(json.loads(decoded))
        except Exception:
            continue
    return stmts


def platform_tokens(plat):
    parts = [x for x in plat.split("/") if x]
    if len(parts) < 2:
        return []
    os_name, arch = parts[0], parts[1]
    variant = parts[2] if len(parts) > 2 else None
    toks = []
    if variant:
        toks.append(f"{os_name}/{arch}/{variant}")
        toks.append(f"{os_name}-{arch}-{variant}")
        toks.append(f"{os_name}_{arch}_{variant}")
        toks.append(f"{arch}-{variant}")
        toks.append(f"{arch}_{variant}")
        toks.append(f"{arch}{variant}")
    toks.append(f"{os_name}/{arch}")
    toks.append(f"{os_name}-{arch}")
    toks.append(f"{os_name}_{arch}")
    toks.append(arch)
    seen = set()
    out = []
    for t in toks:
        if t and t not in seen:
            seen.add(t)
            out.append(t)
    return out


def has_token(text, token):
    return re.search(r"(?<![A-Za-z0-9])" + re.escape(token) + r"(?![A-Za-z0-9])", text) is not None


def match_target(path, stmt_or_doc, digest_to_platform, platform_to_digest):
    if isinstance(stmt_or_doc, dict) and stmt_or_doc.get("_type", "").startswith("https://in-toto.io/Statement"):
        subs = stmt_or_doc.get("subject") or []
        for sub in subs:
            d = (sub.get("digest") or {}).get("sha256")
            if d:
                cand = "sha256:" + d
                if cand in digest_to_platform:
                    return cand, digest_to_platform[cand]
    low = path.lower().replace("\\", "/")
    candidates = []
    for plat, dig in platform_to_digest.items():
        toks = platform_tokens(plat.lower())
        matched = [t for t in toks if has_token(low, t)]
        if matched:
            candidates.append((len(max(matched, key=len)), len(matched), plat, dig))
    if candidates:
        candidates.sort(reverse=True)
        best = candidates[0]
        if len(candidates) > 1 and candidates[1][0:2] == best[0:2] and candidates[1][2] != best[2]:
            return None, None
        return best[3], best[2]
    if len(platform_to_digest) == 1:
        plat, dig = next(iter(platform_to_digest.items()))
        return dig, plat
    return None, None


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--export-dir", required=True)
    ap.add_argument("--oci-layout", required=True)
    ap.add_argument("--image-repo", required=True)
    ap.add_argument("--issuer", required=True)
    ap.add_argument("--identity-regexp", required=True)
    ap.add_argument("--expected-platforms", required=True)
    args = ap.parse_args()

    expected_platforms = parse_platforms(args.expected_platforms)

    with open(os.path.join(args.oci_layout, "index.json")) as f:
        idx = json.load(f)
    digest_to_platform = {}
    platform_to_digest = {}
    collect_platforms(args.oci_layout, idx.get("manifests") or [], digest_to_platform, platform_to_digest)

    sbom_files = sorted(set(glob.glob(os.path.join(args.export_dir, "**", "sbom*.spdx.json"), recursive=True) +
                             glob.glob(os.path.join(args.export_dir, "**", "sbom*.json"), recursive=True) +
                             glob.glob(os.path.join(args.export_dir, "sbom*.spdx.json")) +
                             glob.glob(os.path.join(args.export_dir, "sbom*.json"))))
    sbom_files = [p for p in sbom_files if os.path.isfile(p)]
    if not sbom_files:
        raise SystemExit("no sbom*.json found in export (SBOM attestation required)")

    attested = set()
    for path in sbom_files:
        with open(path) as f:
            obj = json.load(f)
        if isinstance(obj, dict) and obj.get("_type", "").startswith("https://in-toto.io/Statement") and "predicate" in obj:
            pred = obj["predicate"]
            stmt_like = obj
        else:
            pred = obj
            stmt_like = obj
        target_digest, pnorm = match_target(path, stmt_like, digest_to_platform, platform_to_digest)
        if not target_digest:
            raise SystemExit(f"could not match SBOM to a platform manifest: {path} (known platforms={sorted(platform_to_digest)})")
        target = f"{args.image_repo}@{target_digest}"
        with tempfile.NamedTemporaryFile("w", suffix=".spdx.json", delete=False) as tf:
            json.dump(pred, tf)
            pred_path = tf.name
        try:
            subprocess.run(["cosign", "attest", "--yes", "--type", "spdxjson", "--predicate", pred_path, target], check=True)
            verify_out = subprocess.check_output([
                "cosign", "verify-attestation", "--type", "spdxjson",
                "--certificate-oidc-issuer", args.issuer,
                "--certificate-identity-regexp", args.identity_regexp,
                target,
            ], text=True)
        finally:
            os.unlink(pred_path)

        want = target_digest.split(":", 1)[1]
        ok = False
        for vstmt in decode_attestation_payloads(verify_out):
            vpred = vstmt.get("predicate") or {}
            vsubs = [s.get("digest", {}).get("sha256") for s in (vstmt.get("subject") or []) if isinstance(s, dict)]
            if isinstance(vpred, dict) and vpred == pred and vpred.get("spdxVersion") and vpred.get("SPDXID") and vpred.get("dataLicense") and want in vsubs:
                ok = True
                break
        if not ok:
            raise SystemExit(f"verified SBOM attestation missing valid SPDX predicate schema/subject/predicate match for {target} (from {path})")
        attested.add(pnorm or target_digest)
        print(f'OK: SBOM attested+verified for {target} platform={pnorm or "unknown"} ({os.path.basename(path)})')

    if expected_platforms:
        missing = [p for p in expected_platforms if p not in attested]
        if missing:
            raise SystemExit(f"missing SBOM attestation for expected platform(s): {', '.join(missing)}")
    print(f"OK: SBOM attestations verified for {len(attested)} platform manifest(s)")


if __name__ == "__main__":
    main()
