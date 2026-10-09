"""Validate saved ISO staging output without changing the original evidence.

The first checksum belongs to the target whose size is recorded at the end.
Transport success, evidence freshness and host identity remain caller gates.
"""

import datetime
from pathlib import PurePosixPath
import re


def _path(value):
    if not isinstance(value, str) or any(ord(c) < 32 for c in value):
        raise ValueError("invalid ISO path")
    path = PurePosixPath(value)
    if (not path.is_absolute() or str(path) != value or ".." in path.parts
            or not value.endswith(".iso") or "\\" in value):
        raise ValueError("expected a canonical absolute ISO path")
    return value


def validate_iso_storage_evidence(text, *, expected_hashes, target_path, expected_target_bytes):
    """Require timestamp, exact checksum rows, and one target ISO_BYTES marker.

    All expectations must come from the independently bound release/retention
    manifests. An unlabelled size is bound only to the first checksum's path.
    Unknown lines are errors, never silently stripped to make a record pass.
    """
    if not isinstance(text, str) or not isinstance(expected_hashes, dict) or not expected_hashes:
        raise ValueError("missing ISO evidence or expected checksums")
    for path, digest in expected_hashes.items():
        _path(path)
        if not isinstance(digest, str) or re.fullmatch(r"[0-9a-f]{64}", digest) is None:
            raise ValueError("invalid expected SHA256")
    if _path(target_path) not in expected_hashes:
        raise ValueError("target is not in expected ISO paths")
    if type(expected_target_bytes) is not int or expected_target_bytes <= 0:
        raise ValueError("expected target size must be a positive integer")
    lines = text.splitlines()
    if len(lines) < 3 or re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ", lines[0]) is None:
        raise ValueError("missing first UTC observation timestamp")
    datetime.datetime.strptime(lines[0], "%Y-%m-%dT%H:%M:%SZ")
    marker = re.fullmatch(r"ISO_BYTES ([1-9][0-9]*)", lines[-1])
    if marker is None:
        raise ValueError("missing final positive ISO_BYTES marker")
    hashes = {}
    for line in lines[1:-1]:
        match = re.fullmatch(r"([0-9a-f]{64}) [ *](.+)", line)
        if match is None:
            raise ValueError("invalid ISO checksum row")
        digest, path = match.groups()
        _path(path)
        if path in hashes:
            raise ValueError("duplicate ISO checksum path")
        hashes[path] = digest
    if not hashes or next(iter(hashes)) != target_path:
        raise ValueError("size marker must describe the first target ISO")
    if hashes != expected_hashes:
        raise ValueError("ISO checksum paths or digests do not match")
    size = int(marker[1])
    if size != expected_target_bytes:
        raise ValueError("target ISO size does not match")
    return {"observedAt": lines[0], "hashes": hashes,
            "targetPath": target_path, "targetBytes": size}
