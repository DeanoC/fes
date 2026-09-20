#!/usr/bin/env python3
"""Pass-2 freeze-scaffold: merge a cart JSON into a routed empty-socket shell."""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from pathlib import Path

from experiment_policy import PolicyError, policy_for
from link_static_rbf import overlay_files


ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"


class SlotBuildError(RuntimeError):
    """Raised when a freeze-scaffold compose cannot be produced."""


def _require_file(path: Path) -> Path:
    if not path.is_file():
        raise SlotBuildError(f"missing required file: {path}")
    return path


def _run(command: list[str], cwd: Path) -> None:
    try:
        subprocess.run(command, cwd=str(cwd), check=True)
    except subprocess.CalledProcessError as exc:
        raise SlotBuildError(f"{' '.join(command)} failed") from exc


def _toolchain_bin(name: str) -> Path:
    env = os.environ.get(name.upper().replace("-", "_"))
    if env:
        return _require_file(Path(env))
    install = Path(os.environ.get("TOOLCHAIN_INSTALL", ROOT / "build/toolchain/install"))
    return _require_file(install / "bin" / name)


def synth_cart(experiment: str, output: Path) -> Path:
    policy = policy_for(experiment)
    if policy.top != "cart":
        raise SlotBuildError(f"{experiment} is not a cart top")
    yosys = _toolchain_bin("yosys")
    reads = " ".join(f"read_verilog {path};" for path in policy.sources)
    flags = " ".join(policy.synth_intel_alm_flags)
    extra = policy.yosys_post_synth
    output.parent.mkdir(parents=True, exist_ok=True)
    program = (
        f"{reads} synth_intel_alm{(' ' + flags) if flags else ''} -top {policy.top}; "
        f"{extra} stat; write_json {output}"
    )
    _run([str(yosys), "-p", program], ROOT)
    _run(
        [
            sys.executable,
            str(ROOT / "scripts/experiment_policy.py"),
            "--experiment",
            experiment,
            "--fix-synth-json",
            str(output),
        ],
        ROOT,
    )
    _run(
        [
            sys.executable,
            str(ROOT / "scripts/experiment_policy.py"),
            "--experiment",
            experiment,
            "--check-synth-json",
            str(output),
        ],
        ROOT,
    )
    return output


def place_cart(
    *,
    shell_json: Path,
    cart_json: Path,
    qsf: Path,
    sdc: Path,
    output_json: Path,
    output_rbf: Path,
    router: str,
) -> None:
    nextpnr = _toolchain_bin("nextpnr-mistral")
    output_json.parent.mkdir(parents=True, exist_ok=True)
    output_rbf.parent.mkdir(parents=True, exist_ok=True)
    command = [
        str(nextpnr),
        "--json",
        str(shell_json),
        "--device",
        TARGET,
        "--qsf",
        str(qsf),
        "--sdc",
        str(sdc),
        "--freq",
        "50",
        "--fes-scaffold",
        "--fes-cart",
        str(cart_json),
        "--no-pack",
        "--router",
        router,
        "--rbf",
        str(output_rbf),
        "--compress-rbf",
        "--write",
        str(output_json),
    ]
    _run(command, ROOT)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--shell-json", type=Path, required=True)
    parser.add_argument("--shell-rbf", type=Path, required=True)
    parser.add_argument("--cart", required=True, help="cart experiment name (900_expansion_bus or 903_wide_cart)")
    parser.add_argument("--map", type=Path, default=ROOT / "experiments/901_plugged_base/link.toml")
    parser.add_argument("--qsf", type=Path, default=ROOT / "experiments/901_plugged_base/pins.qsf")
    parser.add_argument("--sdc", type=Path, default=ROOT / "boards/de10nano/clocks.sdc")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--router", default="gpu")
    parser.add_argument("--work", type=Path, default=None)
    args = parser.parse_args(argv)
    try:
        work = args.work or args.output.parent
        work.mkdir(parents=True, exist_ok=True)
        cart_json = synth_cart(args.cart, work / f"{args.cart}.json")
        composed_json = work / f"{args.cart}.placed.json"
        composed_rbf = work / f"{args.cart}.placed.rbf"
        place_cart(
            shell_json=_require_file(args.shell_json),
            cart_json=cart_json,
            qsf=_require_file(args.qsf),
            sdc=_require_file(args.sdc),
            output_json=composed_json,
            output_rbf=composed_rbf,
            router=args.router,
        )
        overlay_files(
            _require_file(args.shell_rbf),
            composed_rbf,
            _require_file(args.map),
            args.output,
        )
        return 0
    except (SlotBuildError, PolicyError, OSError) as exc:
        print(f"build_fes_slot: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
