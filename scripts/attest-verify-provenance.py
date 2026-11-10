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

"""Attest and verify per-platform BuildKit provenance from an OCI layout.

Extracts the exported BuildKit provenance predicate/type per platform, re-attests
it to the matching pushed platform manifest with cosign (preserving predicate and
predicate type), and verifies signature + decoded payload (predicateType, bound
subject, exact predicate). Requires verified provenance for every expected platform.
"""

import argparse
import base64
import json
import os
import subprocess
import sys
import tempfile


def parse_platforms(raw):
    plats = [p.strip() for p in (raw or "").split(",") if p.strip()]
    if not plats:
        raise SystemExit("no expected platforms provided")
    return plats


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


def decode_attestation_payloads(verify_output):
    stmts = []
    for line in (verify_output or "").splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
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


def collect_platforms(layout, manifests, digest_to_platform, depth=0):
    if depth > 8:
        return
    for m in manifests or []:
        mt = m.get("mediaType", "")
        if "index" in mt:
            doc = json.loads(load_blob(layout, m["digest"]))
            collect_platforms(layout, doc.get("manifests") or [], digest_to_platform, depth + 1)
            continue
        ref = (m.get("annotations") or {}).get("vnd.docker.reference.type")
        if ref == "attestation-manifest":
            continue
        pnorm = normalize_platform(m.get("platform") or {})
        if pnorm and m.get("digest"):
            digest_to_platform[m["digest"]] = pnorm


def walk(layout, manifests, digest_to_platform, repo, issuer, identity_re, expected_platforms, verified_by_platform, depth=0):
    if depth > 8:
        return
    for m in manifests or []:
        mt = m.get("mediaType", "")
        if "index" in mt:
            doc = json.loads(load_blob(layout, m["digest"]))
            walk(layout, doc.get("manifests") or [], digest_to_platform, repo, issuer, identity_re, expected_platforms, verified_by_platform, depth + 1)
            continue
        ref = (m.get("annotations") or {}).get("vnd.docker.reference.type")
        if ref != "attestation-manifest":
            continue
        am = json.loads(load_blob(layout, m["digest"]))
        subj = am.get("subject") or {}
        subject_digest = subj.get("digest")
        attestation_platform = m.get("platform") or {}
        if not subject_digest:
            raise SystemExit(f"attestation manifest missing subject digest: {m.get('digest')}")
        target = f"{repo}@{subject_digest}"
        want = subject_digest.split(":", 1)[1]
        for layer in (am.get("layers") or []):
            ann = layer.get("annotations") or {}
            ptype_ann = ann.get("in-toto.io/predicate-type", "")
            if "slsa.dev/provenance" not in ptype_ann:
                continue
            blob = load_blob(layout, layer["digest"])
            stmt = json.loads(blob)
            subj_digests = [s.get("digest", {}).get("sha256") for s in (stmt.get("subject") or []) if isinstance(s, dict)]
            if want not in subj_digests:
                raise SystemExit(f"statement subject {subj_digests} does not bind to attested manifest {subject_digest}")

            predicate = stmt.get("predicate", stmt)
            if isinstance(stmt, dict) and "_type" not in stmt and "buildDefinition" in stmt and "predicate" not in stmt:
                predicate = stmt
            pred_type = stmt.get("predicateType") or ptype_ann or "https://slsa.dev/provenance/v1"

            pred_path = None
            try:
                with tempfile.NamedTemporaryFile("w", suffix=".predicate.json", delete=False) as tf:
                    json.dump(predicate, tf)
                    pred_path = tf.name
                subprocess.run([
                    "cosign", "attest", "--yes",
                    "--type", pred_type,
                    "--predicate", pred_path,
                    target,
                ], check=True)
                out = subprocess.check_output([
                    "cosign", "verify-attestation",
                    "--type", pred_type,
                    "--certificate-oidc-issuer", issuer,
                    "--certificate-identity-regexp", identity_re,
                    target,
                ], text=True)
            finally:
                if pred_path and os.path.exists(pred_path):
                    os.unlink(pred_path)

            ok = False
            for vstmt in decode_attestation_payloads(out):
                vptype = vstmt.get("predicateType", "")
                vsubs = [s.get("digest", {}).get("sha256") for s in (vstmt.get("subject") or []) if isinstance(s, dict)]
                vp = vstmt.get("predicate")
                if vptype == pred_type and want in vsubs and vp == predicate:
                    ok = True
                    break
            if not ok:
                raise SystemExit(f"verified attestation payload predicate/predicateType/subject mismatch for {target} (expected predicateType={pred_type})")

            pnorm = digest_to_platform.get(subject_digest) or normalize_platform(attestation_platform)
            if pnorm in verified_by_platform:
                verified_by_platform[pnorm] += 1
            elif len(expected_platforms) == 1 and pnorm is None:
                verified_by_platform[expected_platforms[0]] += 1
            print(f"OK: signed+verified provenance for {target} platform={pnorm or 'unknown'}")


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--oci-layout", required=True)
    ap.add_argument("--image-repo", required=True)
    ap.add_argument("--issuer", required=True)
    ap.add_argument("--identity-regexp", required=True)
    ap.add_argument("--expected-platforms", required=True)
    args = ap.parse_args()

    expected_platforms = parse_platforms(args.expected_platforms)
    verified_by_platform = {p: 0 for p in expected_platforms}

    with open(os.path.join(args.oci_layout, "index.json")) as f:
        idx = json.load(f)

    digest_to_platform = {}
    collect_platforms(args.oci_layout, idx.get("manifests") or [], digest_to_platform)
    walk(args.oci_layout, idx.get("manifests") or [], digest_to_platform, args.image_repo, args.issuer, args.identity_regexp, expected_platforms, verified_by_platform)

    missing = [p for p, c in verified_by_platform.items() if c < 1]
    if missing:
        raise SystemExit(f"missing verified provenance for expected platform(s): {', '.join(missing)} (verified={verified_by_platform})")
    print(f"OK: verified provenance for all expected platforms: {verified_by_platform}")


if __name__ == "__main__":
    main()
