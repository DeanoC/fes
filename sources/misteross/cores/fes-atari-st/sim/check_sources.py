#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-or-later
"""Verify the preserved CPU bytes used by the actual machine simulation."""
import hashlib
import json
from pathlib import Path


def main():
    vendor = Path(__file__).resolve().parents[2] / "fes-common/rtl/fx68k"
    source = json.loads((vendor / "source.json").read_text())
    for item in source["files"]:
        path = vendor / item["path"]
        if hashlib.sha256(path.read_bytes()).hexdigest() != item["sha256"]:
            raise SystemExit(f"FX68K source differs from pinned upstream: {path}")
    print(f"FX68K {source['commit']}: {len(source['files'])} source digests verified")


if __name__ == "__main__":
    main()
