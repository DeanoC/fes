#!/usr/bin/env python3
"""Static Cyclone V CRAM overlay linker.

Refuse compressed-frame splicing. Always decompress, overlay a CRAM rectangle
from a link map, rewrite CRCs, and recompress. The composed bitstream is one
full-chip RBF.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any, Sequence

try:
    from .cyclonev_rbf import (
        SLOT_BEL,
        SX120F,
        TARGET_DEVICE,
        CramRect,
        classify_cram_diff,
        default_slot_rect,
        overlay_cram,
        rbf_load,
        rbf_save,
        tile_column_cram_x,
    )
except ImportError:  # pragma: no cover - script entry
    from cyclonev_rbf import (
        SLOT_BEL,
        SX120F,
        TARGET_DEVICE,
        CramRect,
        classify_cram_diff,
        default_slot_rect,
        overlay_cram,
        rbf_load,
        rbf_save,
        tile_column_cram_x,
    )


OVERLAY_CRAM_RECT = "cram_rect"
OVERLAY_M10K_RAM = "m10k_ram"


class LinkError(ValueError):
    """Raised when a static overlay cannot be produced."""


def bel_to_bt_name(bel: str) -> str:
    """Convert ``MISTRAL_M10K.26.1.0`` to mistral-cv ``M10K.026.001``."""

    parts = bel.split(".")
    if len(parts) < 3:
        raise LinkError(f"unsupported slot BEL {bel!r}")
    kind = parts[0].removeprefix("MISTRAL_")
    try:
        x = int(parts[1], 10)
        y = int(parts[2], 10)
    except ValueError as exc:
        raise LinkError(f"unsupported slot BEL {bel!r}") from exc
    return f"{kind}.{x:03d}.{y:03d}"


def overlay_m10k_ram_bt(base_text: str, cart_text: str, bel: str = SLOT_BEL) -> str:
    """Replace slot M10K RAM mux lines in a mistral-cv decompile."""

    prefix = f"s {bel_to_bt_name(bel)}:RAM."
    cart_ram: dict[str, str] = {}
    for line in cart_text.splitlines():
        if line.startswith(prefix):
            cart_ram[line.split()[1]] = line
    if not cart_ram:
        raise LinkError(f"cart decompile has no RAM muxes for {bel}")
    replaced = 0
    out: list[str] = []
    for line in base_text.splitlines():
        if line.startswith(prefix):
            key = line.split()[1]
            if key not in cart_ram:
                raise LinkError(f"cart decompile is missing {key}")
            out.append(cart_ram[key])
            replaced += 1
        else:
            out.append(line)
    if replaced != len(cart_ram):
        raise LinkError(f"replaced {replaced} RAM muxes, cart has {len(cart_ram)}")
    return "\n".join(out) + "\n"


def find_mistral_cv() -> Path:
    env = os.environ.get("MISTRAL_CV")
    if env:
        path = Path(env)
        if path.is_file() and os.access(path, os.X_OK):
            return path
        raise LinkError(f"MISTRAL_CV is not an executable: {env}")
    local = Path(__file__).resolve().parents[1] / "build/toolchain/install/bin/mistral-cv"
    if local.is_file() and os.access(local, os.X_OK):
        return local
    found = shutil.which("mistral-cv")
    if found:
        return Path(found)
    raise LinkError("mistral-cv is required for overlay_mode=m10k_ram")


def parse_link_map(text: str) -> dict[str, Any]:
    """Parse a minimal TOML-like link map without extra dependencies."""

    data: dict[str, Any] = {}
    rect: dict[str, int] = {}
    in_rect = False
    for raw in text.splitlines():
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        if line == "[cram_rect]":
            in_rect = True
            continue
        if line.startswith("[") and line.endswith("]"):
            in_rect = False
            continue
        if "=" not in line:
            raise LinkError(f"invalid link-map line: {raw!r}")
        key, value = (part.strip() for part in line.split("=", 1))
        parsed: Any
        if value.startswith("[") and value.endswith("]"):
            inner = value[1:-1].strip()
            parsed = [] if not inner else [item.strip().strip('"').strip("'") for item in inner.split(",")]
        elif value.startswith('"') and value.endswith('"'):
            parsed = value[1:-1]
        elif value.startswith("'") and value.endswith("'"):
            parsed = value[1:-1]
        elif value in ("true", "True"):
            parsed = True
        elif value in ("false", "False"):
            parsed = False
        else:
            try:
                parsed = int(value, 0)
            except ValueError as exc:
                raise LinkError(f"invalid link-map value for {key}") from exc
        if in_rect:
            if not isinstance(parsed, int):
                raise LinkError("cram_rect values must be integers")
            rect[key] = parsed
        else:
            data[key] = parsed
    if rect:
        data["cram_rect"] = rect
    return data


def load_link_map(path: Path) -> dict[str, Any]:
    return parse_link_map(path.read_text(encoding="utf-8"))


def rect_from_map(mapping: dict[str, Any]) -> CramRect:
    device = mapping.get("device", TARGET_DEVICE)
    if device != TARGET_DEVICE:
        raise LinkError(f"unsupported device {device!r}")
    die = mapping.get("die", SX120F.name)
    if die != SX120F.name:
        raise LinkError(f"unsupported die {die!r}")
    rect = mapping.get("cram_rect")
    if isinstance(rect, dict) and {"x0", "y0", "x1", "y1"} <= set(rect):
        return CramRect(int(rect["x0"]), int(rect["y0"]), int(rect["x1"]), int(rect["y1"]))
    column = int(mapping.get("slot_column", 26))
    return default_slot_rect(SX120F, column)


def overlay_mode_from_map(mapping: dict[str, Any]) -> str:
    mode = mapping.get("overlay_mode", OVERLAY_CRAM_RECT)
    if mode not in (OVERLAY_CRAM_RECT, OVERLAY_M10K_RAM):
        raise LinkError(f"unsupported overlay_mode {mode!r}")
    return str(mode)


def _run_mistral_cv(binary: Path, args: Sequence[str]) -> None:
    try:
        subprocess.run([str(binary), *args], check=True, capture_output=True, text=True)
    except subprocess.CalledProcessError as exc:
        detail = (exc.stderr or exc.stdout or "").strip()
        raise LinkError(detail or f"mistral-cv {' '.join(args)} failed") from exc


def overlay_m10k_ram_files(
    base_path: Path, cart_path: Path, mapping: dict[str, Any], output_path: Path
) -> bytes:
    bels = mapping.get("slot_bels", [SLOT_BEL])
    if not isinstance(bels, list) or not bels:
        bels = [SLOT_BEL]
    mistral_cv = find_mistral_cv()
    with tempfile.TemporaryDirectory(prefix="fes-m10k-ram-") as directory:
        root = Path(directory)
        base_bt = root / "base.bt"
        cart_bt = root / "cart.bt"
        composed_bt = root / "composed.bt"
        composed_rbf = root / "composed.rbf"
        _run_mistral_cv(mistral_cv, ["decomp", TARGET_DEVICE, str(base_path), str(base_bt)])
        _run_mistral_cv(mistral_cv, ["decomp", TARGET_DEVICE, str(cart_path), str(cart_bt)])
        text = base_bt.read_text(encoding="utf-8")
        cart_text = cart_bt.read_text(encoding="utf-8")
        for bel in bels:
            text = overlay_m10k_ram_bt(text, cart_text, str(bel))
        composed_bt.write_text(text, encoding="utf-8")
        _run_mistral_cv(mistral_cv, ["comp", str(composed_bt), str(composed_rbf)])
        output = composed_rbf.read_bytes()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_bytes(output)
    return output


def overlay_files(base_path: Path, cart_path: Path, map_path: Path, output_path: Path) -> dict[str, Any]:
    mapping = load_link_map(map_path)
    if mapping.get("device", TARGET_DEVICE) != TARGET_DEVICE:
        raise LinkError(f"unsupported device {mapping.get('device')!r}")
    if mapping.get("die", SX120F.name) != SX120F.name:
        raise LinkError(f"unsupported die {mapping.get('die')!r}")
    mode = overlay_mode_from_map(mapping)
    base_bytes = base_path.read_bytes()
    cart_bytes = cart_path.read_bytes()
    if mode == OVERLAY_M10K_RAM:
        output = overlay_m10k_ram_files(base_path, cart_path, mapping, output_path)
        rect = None
    else:
        rect = rect_from_map(mapping)
        base = rbf_load(base_bytes)
        cart = rbf_load(cart_bytes)
        classified = classify_cram_diff(base, cart, rect)
        if mapping.get("require_slot_only", True) and int(classified["bits_outside_slot"]) != 0:
            raise LinkError(
                "cart CRAM changes "
                f"{classified['bits_outside_slot']} bits outside the reserved rect"
            )
        composed = overlay_cram(base, cart, rect)
        output = rbf_save(composed, compressed=True)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_bytes(output)
    receipt = {
        "device": TARGET_DEVICE,
        "die": SX120F.name,
        "overlay_mode": mode,
        "slot_bels": mapping.get("slot_bels", [SLOT_BEL]),
        "base_sha256": hashlib.sha256(base_bytes).hexdigest(),
        "cart_sha256": hashlib.sha256(cart_bytes).hexdigest(),
        "map_sha256": hashlib.sha256(map_path.read_bytes()).hexdigest(),
        "output_sha256": hashlib.sha256(output).hexdigest(),
        "output_size": len(output),
    }
    if rect is not None:
        receipt["cram_rect"] = {"x0": rect.x0, "y0": rect.y0, "x1": rect.x1, "y1": rect.y1}
    receipt_path = output_path.with_suffix(output_path.suffix + ".receipt.json")
    receipt_path.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    return receipt


def diff_files(left_path: Path, right_path: Path) -> dict[str, Any]:
    left = rbf_load(left_path.read_bytes())
    right = rbf_load(right_path.read_bytes())
    slot = default_slot_rect()
    classified = classify_cram_diff(left, right, slot)
    return {
        "device": TARGET_DEVICE,
        "slot_column_x": list(tile_column_cram_x(SX120F, 26)),
        "slot_rect": {"x0": slot.x0, "y0": slot.y0, "x1": slot.x1, "y1": slot.y1},
        **classified,
    }


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    overlay = sub.add_parser("overlay", help="compose base+cart CRAM into one RBF")
    overlay.add_argument("--base", required=True, type=Path)
    overlay.add_argument("--cart", required=True, type=Path)
    overlay.add_argument("--map", required=True, type=Path)
    overlay.add_argument("--output", required=True, type=Path)
    diff = sub.add_parser("diff", help="report the CRAM bounding box between two RBFs")
    diff.add_argument("--a", required=True, type=Path)
    diff.add_argument("--b", required=True, type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == "overlay":
            receipt = overlay_files(args.base, args.cart, args.map, args.output)
            print(json.dumps(receipt, indent=2))
            return 0
        print(json.dumps(diff_files(args.a, args.b), indent=2))
        return 0
    except (LinkError, ValueError, OSError) as exc:
        print(f"link_static_rbf: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
