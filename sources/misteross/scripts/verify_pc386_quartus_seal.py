#!/usr/bin/env python3
"""Check the pc386 Quartus seal outputs against the exact source and firmware."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
FIRMWARE_LOCK = ROOT / "cores/fes-pc386/firmware/firmware.lock"
HEAD_RE = re.compile(r"[0-9a-f]{40}")


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


def verify(
    summary_path: Path,
    inputs_path: Path,
    expected_head: str,
    firmware_lock_path: Path = FIRMWARE_LOCK,
) -> dict[str, Any]:
    """Require a passing timing/seal summary for *expected_head* and locked BIOS."""
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
    return summary


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--summary", type=Path, required=True, help="build-summary.json from the seal")
    parser.add_argument("--inputs", type=Path, required=True, help="build-inputs.json from the seal")
    parser.add_argument("--head", required=True, help="full exact source commit SHA")
    parser.add_argument("--firmware-lock", type=Path, default=FIRMWARE_LOCK)
    args = parser.parse_args(argv)

    try:
        summary = verify(args.summary, args.inputs, args.head, args.firmware_lock)
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
    print("SEAL_OK")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
