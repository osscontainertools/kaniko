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

"""Shared OCI layout/attestation helpers for verification scripts."""

from __future__ import annotations

import base64
import json
import os
from typing import Any, Dict, List, Optional


def parse_platforms(raw: Optional[str], required: bool = False) -> List[str]:
    plats = [p.strip() for p in (raw or "").split(",") if p.strip()]
    if required and not plats:
        raise SystemExit("no expected platforms provided")
    return plats


def normalize_platform(p: Optional[Dict[str, Any]]) -> Optional[str]:
    if not p:
        return None
    os_name = str(p.get("os") or "linux")
    arch = str(p.get("architecture") or "")
    variant = str(p.get("variant") or "")
    if arch in ("", "unknown") or os_name in ("", "unknown"):
        return None
    name = f"{os_name}/{arch}"
    if variant:
        name += f"/{variant}"
    return name


def load_blob(layout: str, digest: str) -> bytes:
    algo, h = digest.split(":", 1)
    with open(os.path.join(layout, "blobs", algo, h), "rb") as f:
        return f.read()


def decode_attestation_payloads(verify_output: Optional[str]) -> List[Dict[str, Any]]:
    stmts: List[Dict[str, Any]] = []
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
            stmt = json.loads(decoded)
            if isinstance(stmt, dict):
                stmts.append(stmt)
        except Exception:
            continue
    return stmts


def collect_platforms(
    layout: str,
    manifests: List[Dict[str, Any]],
    digest_to_platform: Dict[str, str],
    platform_to_digest: Optional[Dict[str, str]] = None,
    depth: int = 0,
) -> None:
    """Collect non-attestation image manifest platforms.

    Populates digest_to_platform (digest -> normalized platform). If
    platform_to_digest is provided, also populates platform -> digest.
    """
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
        digest = m.get("digest")
        if pnorm and isinstance(digest, str):
            digest_to_platform[digest] = pnorm
            if platform_to_digest is not None:
                platform_to_digest[pnorm] = digest
