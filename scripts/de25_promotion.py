#!/usr/bin/env python3
"""Validate exact-main MENU/ZX81 smoke and restore evidence before D25 promotion."""

from __future__ import annotations

import argparse
import datetime
import json
import os
from pathlib import Path
import re
import tempfile


IDENTITY = {"kit_id": "de25-j8g", "serial": "TRWU8J8G",
            "target_id": "e2a51a4f-bae2-4041-a4e4-51f6774d4e45"}
PROMOTION_CORES = {"MENU": "fes.menu", "ZX81": "fes.zx81"}
PACKAGE_ID_RE = re.compile(r"^[0-9a-f]{64}$")


def load(path: Path) -> dict:
    value = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(value, dict):
        raise ValueError(f"{path}: expected object")
    return value


def promoted_record(baseline: dict, head: str, menu_run: Path, zx81_run: Path,
                    verified_at: str | None = None) -> dict:
    if not re.fullmatch(r"[0-9a-f]{40}", head):
        raise ValueError("promotion head must be a full commit SHA")
    if baseline.get("schema") != 1 or any(baseline.get(k) != v for k, v in IDENTITY.items()):
        raise ValueError("current D25 baseline identity is invalid")
    if menu_run.resolve() == zx81_run.resolve():
        raise ValueError("MENU and ZX81 promotion evidence must be distinct runs")
    package_ids = {}
    for name, run in (("MENU", menu_run), ("ZX81", zx81_run)):
        verdict = (run / "verdict").read_text(encoding="utf-8").strip()
        if verdict != "PASS":
            raise ValueError(f"{name} exact-main HIL verdict is not PASS")
        result_path = run / "out/result.json"
        if not result_path.exists() and name == "MENU":
            result_path = run / "out/menu-result.json"
        smoke = load(result_path)
        if smoke.get("head") != head or smoke.get("kit") != "D25":
            raise ValueError(f"{name} smoke is not for this exact main head and D25")
        if smoke.get("core_id") != PROMOTION_CORES[name]:
            raise ValueError(f"{name} smoke package is not the expected core")
        package_id = smoke.get("package_id")
        if not isinstance(package_id, str) or not PACKAGE_ID_RE.fullmatch(package_id):
            raise ValueError(f"{name} smoke has no valid package identity")
        package_ids[name] = package_id
        restore = load(run / "out/restore-verification.json")
        if restore.get("passed") is not True or restore.get("serial") != IDENTITY["serial"]:
            raise ValueError(f"{name} restore verification did not pass for J8G")
    if package_ids["MENU"] == package_ids["ZX81"]:
        raise ValueError("MENU and ZX81 promotion evidence must use distinct packages")
    result = dict(baseline)
    result["source"] = f"fetched origin/main {head}; independent Pro MENU and ZX81 exact-head HIL"
    result["verified_at_utc"] = verified_at or datetime.datetime.now(datetime.timezone.utc).isoformat()
    result["verified_restore_evidence"] = f"{menu_run};{zx81_run}"
    return result


def write_atomic(path: Path, value: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix=path.name + ".", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(value, stream, indent=2, sort_keys=True)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temp_name, path)
    finally:
        if os.path.exists(temp_name):
            os.unlink(temp_name)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True, type=Path)
    parser.add_argument("--head", required=True)
    parser.add_argument("--menu-run", required=True, type=Path)
    parser.add_argument("--zx81-run", required=True, type=Path)
    args = parser.parse_args(argv)
    try:
        value = promoted_record(load(args.baseline), args.head, args.menu_run, args.zx81_run)
        write_atomic(args.baseline, value)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        parser.exit(1, f"de25 promotion refused: {exc}\n")
    print(f"D25 restore baseline promoted for {args.head}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
