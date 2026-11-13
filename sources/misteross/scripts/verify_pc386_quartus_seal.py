#!/usr/bin/env python3
"""Check the pc386 Quartus seal outputs against the exact source and firmware.

The summary must pass, carry the locked BIOS, and be bound to the requested
revision: its source-input record (path -> SHA-256, relative to
sources/misteross) must be present, every file in it must hash the same at
that revision in the repository, the receipt (build-inputs.json) must name
the revision, and the receipt's tools, recipe and ABI definition must be the
summary's."""

from __future__ import annotations

import argparse
import json
import hashlib
import re
import subprocess
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
REPOSITORY = ROOT.parents[1]
SOURCE_PREFIX = ROOT.relative_to(REPOSITORY).as_posix()  # sources/misteross
FIRMWARE_LOCK = ROOT / "cores/fes-pc386/firmware/firmware.lock"
HEAD_RE = re.compile(r"[0-9a-f]{40}")
SHA256_RE = re.compile(r"[0-9a-f]{64}")


class VerificationError(ValueError):
    """A seal summary does not match the requested source and firmware."""


def _read_object(path: Path, label: str) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise VerificationError(f"cannot read {label} {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise VerificationError(f"{label} must be a JSON object: {path}")
    return value


def _blob_sha256(repository: Path, revision: str, paths: list[str]) -> dict[str, str | None]:
    """SHA-256 of each path's blob at revision (None where it is absent)."""
    request = "".join(f"{revision}:{path}\n" for path in paths).encode()
    try:
        out = subprocess.run(["git", "-C", str(repository), "cat-file", "--batch"], input=request,
                             capture_output=True, check=True).stdout
    except (OSError, subprocess.CalledProcessError) as exc:
        raise VerificationError(f"cannot read {revision} in {repository}: {exc}") from exc
    found: dict[str, str | None] = {}
    at = 0
    for path in paths:
        end = out.index(b"\n", at)
        header = out[at:end].split()
        at = end + 1
        if len(header) != 3:
            found[path] = None
            continue
        size = int(header[2])
        found[path] = hashlib.sha256(out[at:at + size]).hexdigest()
        at += size + 1
    return found


def _bind_sources(summary: dict[str, Any], inputs: dict[str, Any], expected_head: str,
                  repository: Path, prefix: str) -> int:
    """Tie the summary to the receipt and to expected_head; return the files checked."""
    record = summary.get("inputs")
    if not isinstance(record, dict) or not record:
        raise VerificationError("build summary has no source-input record: it is not bound to a revision")
    for path, digest in record.items():
        if not isinstance(path, str) or not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            raise VerificationError(f"source-input record entry is malformed: {path!r}")
    if summary.get("tools") != inputs.get("tools"):
        raise VerificationError("build summary tools differ from the inputs receipt's")
    for key in ("recipe", "abi_definition"):
        path, digest = inputs.get(key), inputs.get(f"{key}_sha256")
        relative = path[len(prefix) + 1:] if isinstance(path, str) and path.startswith(prefix + "/") else None
        if relative is None or record.get(relative) != digest:
            raise VerificationError(f"inputs.{key} {path} is not the summary's recorded source")
    paths = sorted(record)
    actual = _blob_sha256(repository, expected_head, [f"{prefix}/{path}" for path in paths])
    wrong = [path for path in paths if actual[f"{prefix}/{path}"] != record[path]]
    if wrong:
        raise VerificationError(f"{len(wrong)} recorded source inputs differ at {expected_head}: {wrong[:5]}")
    return len(paths)


def verify(
    summary_path: Path,
    inputs_path: Path,
    expected_head: str,
    firmware_lock_path: Path = FIRMWARE_LOCK,
    repository: Path = REPOSITORY,
    prefix: str = SOURCE_PREFIX,
) -> dict[str, Any]:
    """Require a passing timing/seal summary for *expected_head* and locked BIOS,
    bound to that revision through its source-input record and the receipt."""
    if HEAD_RE.fullmatch(expected_head) is None:
        raise VerificationError(f"expected head must be a full lowercase Git SHA: {expected_head!r}")

    summary = _read_object(summary_path, "build summary")
    inputs = _read_object(inputs_path, "build inputs")
    firmware_lock = _read_object(firmware_lock_path, "firmware lock")

    timing = summary.get("timing")
    timing_status = timing.get("status") if isinstance(timing, dict) else None
    status = summary.get("status")
    if status != "pass" or timing_status != "pass":
        raise VerificationError(f"seal did not pass: status={status} timing={timing_status}")

    try:
        expected_bios = firmware_lock["images"]["bios"]["sha256"]
    except (KeyError, TypeError) as exc:
        raise VerificationError(f"firmware lock has no BIOS digest: {firmware_lock_path}") from exc
    firmware = summary.get("firmware")
    actual_bios = firmware.get("bios") if isinstance(firmware, dict) else None
    if actual_bios != expected_bios:
        raise VerificationError(f"firmware.bios {actual_bios} != locked {expected_bios}")

    revision = inputs.get("revision")
    if revision != expected_head:
        raise VerificationError(f"inputs.revision {revision} != exact head {expected_head}")
    summary["_bound_inputs"] = _bind_sources(summary, inputs, expected_head, repository, prefix)
    return summary


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--summary", type=Path, required=True, help="build-summary.json from the seal")
    parser.add_argument("--inputs", type=Path, required=True, help="build-inputs.json from the seal")
    parser.add_argument("--head", required=True, help="full exact source commit SHA")
    parser.add_argument("--firmware-lock", type=Path, default=FIRMWARE_LOCK)
    parser.add_argument("--repository", type=Path, default=REPOSITORY,
                        help="the Git repository that holds --head (default: this checkout's)")
    args = parser.parse_args(argv)

    try:
        summary = verify(args.summary, args.inputs, args.head, args.firmware_lock, args.repository)
    except VerificationError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    timing = summary["timing"]
    firmware = summary["firmware"]
    print("status", summary["status"])
    print("build_id", summary.get("build_id"))
    print("seed", summary.get("seed"))
    print("firmware.bios", firmware["bios"])
    print("timing.status", timing["status"])
    print("rbf", summary.get("rbf"))
    print("inputs.revision", args.head)
    print("inputs.bound", summary["_bound_inputs"], "files")
    print("SEAL_OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
