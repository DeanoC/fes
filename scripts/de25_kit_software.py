#!/usr/bin/env python3
"""Validate the exact managed software bundle used by J8G HIL."""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import tarfile


SHA256 = re.compile(r"^[0-9a-f]{64}$")


def runtime_digest(bundle: Path, head: str) -> str:
    with tarfile.open(bundle, "r:*") as archive:
        member = archive.extractfile("manifest.json")
        if member is None:
            raise ValueError("kit-software manifest is missing")
        manifest = json.load(member)
    if manifest.get("board") != "de25-nano" or manifest.get("architecture") != "aarch64":
        raise ValueError("kit-software bundle is not for DE25")
    provenance = manifest.get("provenance") or {}
    if provenance.get("fogcast") != head or provenance.get("runtime") != head:
        raise ValueError("kit-software bundle is not built from the exact requested head")
    files = {item.get("path"): item.get("sha256") for item in manifest.get("files", [])}
    digest = files.get("bin/mister-runtime")
    if not isinstance(digest, str) or not SHA256.fullmatch(digest):
        raise ValueError("kit-software manifest has no valid mister-runtime digest")
    return digest


def verify_runtime(expected: str, observed: str) -> None:
    if not SHA256.fullmatch(expected):
        raise ValueError("expected runtime digest is invalid")
    actual = observed.strip()
    if not SHA256.fullmatch(actual) or actual != expected:
        raise ValueError(f"running mister-runtime digest mismatch: expected {expected}, got {actual}")


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    bundle = sub.add_parser("runtime-digest")
    bundle.add_argument("--bundle", required=True, type=Path)
    bundle.add_argument("--head", required=True)
    runtime = sub.add_parser("verify-runtime")
    runtime.add_argument("--expected", required=True)
    runtime.add_argument("--observed-file", required=True, type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == "runtime-digest":
            print(runtime_digest(args.bundle, args.head))
        else:
            verify_runtime(args.expected, args.observed_file.read_text(encoding="utf-8"))
            print(f"running mister-runtime matches manifest: {args.expected}")
    except (OSError, ValueError, tarfile.TarError, json.JSONDecodeError) as error:
        parser.exit(1, f"DE25 kit software verification failed: {error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
