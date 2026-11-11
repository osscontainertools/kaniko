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

from __future__ import annotations

import argparse
import json
import os
import subprocess
import tempfile
from typing import Any, Dict, List

from oci_attestation_utils import (
    collect_platforms,
    decode_attestation_payloads,
    load_blob,
    normalize_platform,
    parse_platforms,
)


def walk(
    layout: str,
    manifests: List[Dict[str, Any]],
    digest_to_platform: Dict[str, str],
    repo: str,
    issuer: str,
    identity_re: str,
    expected_platforms: List[str],
    verified_by_platform: Dict[str, int],
    depth: int = 0,
) -> None:
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
                effective_platform = pnorm
            elif len(expected_platforms) == 1 and pnorm is None:
                # single-arch attestation manifests may lack explicit platform
                effective_platform = expected_platforms[0]
            else:
                raise SystemExit(
                    f"verified attestation platform {pnorm!r} is outside expected platforms {expected_platforms} for {target}"
                )
            # count each platform only once so repeated attestations cannot satisfy coverage for multiple platforms
            verified_by_platform[effective_platform] = 1
            print(f"OK: signed+verified provenance for {target} platform={effective_platform}")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--oci-layout", required=True)
    ap.add_argument("--image-repo", required=True)
    ap.add_argument("--issuer", required=True)
    ap.add_argument("--identity-regexp", required=True)
    ap.add_argument("--expected-platforms", required=True)
    args = ap.parse_args()

    expected_platforms = parse_platforms(args.expected_platforms, required=True)
    verified_by_platform = {p: 0 for p in expected_platforms}

    with open(os.path.join(args.oci_layout, "index.json")) as f:
        idx = json.load(f)

    digest_to_platform: Dict[str, str] = {}
    collect_platforms(args.oci_layout, idx.get("manifests") or [], digest_to_platform)
    walk(args.oci_layout, idx.get("manifests") or [], digest_to_platform, args.image_repo, args.issuer, args.identity_regexp, expected_platforms, verified_by_platform)

    missing = [p for p, c in verified_by_platform.items() if c < 1]
    if missing:
        raise SystemExit(f"missing verified provenance for expected platform(s): {', '.join(missing)} (verified={verified_by_platform})")
    print(f"OK: verified provenance for all expected platforms: {verified_by_platform}")


if __name__ == "__main__":
    main()
