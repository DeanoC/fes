#!/usr/bin/env python3
"""Route one core at a chosen BUILD_ID and report the RBF digest.

D12 condition 2 asks whether a build with this PR's tooling and *main's*
BUILD_ID reproduces main's RBF byte for byte. The producers derive BUILD_ID
from their own build record and offer no override, so the comparison is made
the other way round: both sides are routed at the **same** chosen BUILD_ID.
The route is a function of the netlist, device, QSF, SDC, frequency, seed list,
weights, recipe and nextpnr binary; this PR changes none of those except the
netlist, whose only difference is that constant. Holding the constant equal and
comparing the RBFs therefore isolates exactly what the condition is about.

This calls each producer's own `build_commands` and its own route function
rather than reimplementing the build, so the route being measured is the one
the producer performs. It is deliberately not a replica: a faithful-looking
replica that drifted from the producer would produce evidence that reads as
valid and is not.

  python3 rbf_at_build_id.py --tree <tree>/sources/misteross --core fes.menu \
      --cache-root /abs/cache --build-id <32 hex> --out /abs/out

Routing needs a GPU, which the producer's own tooling leases through gpu-run.
"""
from __future__ import annotations

import argparse
import hashlib
import importlib
import json
import os
import subprocess
import sys
from pathlib import Path


def digest(path: Path) -> str:
    out = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            out.update(block)
    return out.hexdigest()


def _load(tree: Path, module: str):
    for stale in [n for n in sys.modules if n == "scripts" or n.startswith("scripts.")]:
        del sys.modules[stale]
    sys.path.insert(0, str(tree))
    os.chdir(tree)
    return importlib.import_module(f"scripts.{module}")


def route_menu(tree: Path, cache_root: Path, build_id: str):
    """fes.menu routes against a checked-in QSF, so no generated file is needed."""
    producer = _load(tree, "build_fes_menu_package")
    from scripts.functional_execution import FunctionalInvocation

    authenticated = producer._authenticate_tools(producer.ROOT, cache_root=cache_root)
    invocation = FunctionalInvocation(authenticated, 0)
    try:
        output = producer.board._prepare_output(
            producer.ROOT, relative=producer.OUTPUT, build_outputs=producer.OUTPUTS)
        yosys, _route = producer.build_commands(
            build_id, {n: authenticated[n].path for n in ("yosys", "nextpnr-mistral")},
            gpu_device=invocation.gpu_device, env=invocation.env)
        producer.board._run_tool(yosys, producer.ROOT, output / "yosys.log",
                                 output_relative=producer.OUTPUT,
                                 env=invocation.env, audit_source_root=producer.ROOT)
        winner = producer._route_placement(
            producer.ROOT, output, authenticated["nextpnr-mistral"].path,
            invocation, producer.menu.seed_for("ddr", 5))
        return output, {n: t.identity for n, t in authenticated.items()}, winner
    finally:
        invocation.close()


def _zx81_model(model: str):
    """ZX81-family route: a generated socket QSF and a netlist transform first.

    Mirrors build_fes_zx81_oss.build() between _prepare_output and
    route_after_synth: the shell QSF written into the output, Yosys, then
    prepare_shell_netlist, then the search with the socket accept hook. Each
    step matters, which is why the control run on main has to reproduce the
    producer's own RBF before any of this is trusted.
    """
    def route(tree: Path, cache_root: Path, build_id: str):
        producer = _load(tree, "build_fes_zx81_oss")
        from scripts.functional_execution import FunctionalInvocation
        from scripts.search_placer_qor import route_after_synth
        from scripts import zx81_expansion

        machine = producer.model_for(model)
        qor_mode = "first-pass-paired"
        qor_weights, qor_budget = producer.placement_policy(qor_mode)
        authenticated = producer._authenticate_tools(
            producer.ROOT, cache_root=cache_root, gpu_router="HIP")
        invocation = FunctionalInvocation(authenticated, 0)
        try:
            output = producer._prepare_output(producer.ROOT, machine.output)
            (output / "socket.qsf").write_text(
                zx81_expansion.shell_qsf((producer.ROOT / producer.QSF).read_text()))
            yosys, _route = producer.build_commands(
                producer.ROOT, output, build_id,
                {n: authenticated[n].path for n in ("yosys", "nextpnr-mistral")},
                model=model)
            producer._run_tool(yosys, producer.ROOT, output / "yosys.log",
                               output_relative=machine.output,
                               env=invocation.env, audit_source_root=producer.ROOT)
            if not (output / "synth.json").is_file():
                raise SystemExit("Yosys produced no synthesis evidence")
            zx81_expansion.prepare_shell_netlist(output / "synth.json")
            winner = route_after_synth(
                nextpnr=authenticated["nextpnr-mistral"].path,
                fixture=output / "synth.json", dest=output, device=producer.TARGET,
                qsf=output / "socket.qsf", sdc=producer.ROOT / producer.SDC,
                freq=producer.PNR["freq"], seeds=producer.PLACER_SEEDS,
                weights=qor_weights, critexp=producer.PLACER_CRITICALITY_EXPONENT,
                budget=qor_budget, mode=qor_mode, extra=(),
                required=producer.PLACER_QOR_CLOCKS, timeout=producer.PNR["timeout_s"],
                gpu_devices=(invocation.gpu_device,),
                accept=zx81_expansion.accept_socket_route, core_id=machine.core_id,
                env=invocation.env, audit_source_root=producer.ROOT)
            return output, {n: t.identity for n, t in authenticated.items()}, winner
        finally:
            invocation.close()
    return route


CORES = {
    "fes.menu": route_menu,
    "fes.zx81": _zx81_model("zx81"),
}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True)
    parser.add_argument("--core", required=True, choices=sorted(CORES))
    parser.add_argument("--cache-root", type=Path, required=True)
    parser.add_argument("--build-id", required=True,
                        help="32 lowercase hex characters, the same on both sides")
    parser.add_argument("--out", type=Path, required=True)
    arguments = parser.parse_args(argv)
    if len(arguments.build_id) != 32 or any(
            c not in "0123456789abcdef" for c in arguments.build_id):
        raise SystemExit("--build-id must be 32 lowercase hexadecimal characters")
    tree = arguments.tree.resolve()
    out = arguments.out.resolve()
    out.mkdir(parents=True, exist_ok=True)
    revision = subprocess.run(("git", "-C", str(tree), "rev-parse", "HEAD"),
                              capture_output=True, text=True, check=True).stdout.strip()
    output, tools, winner = CORES[arguments.core](
        tree, arguments.cache_root.resolve(), arguments.build_id)
    rbf = output / "core.rbf"
    if not rbf.is_file() or rbf.stat().st_size == 0:
        raise SystemExit(f"no RBF was written: {rbf}")
    (out / "core.rbf").write_bytes(rbf.read_bytes())
    record = {
        "core": arguments.core, "tree": str(tree), "revision": revision,
        "build_id": arguments.build_id,
        "rbf_sha256": digest(rbf), "rbf_bytes": rbf.stat().st_size,
        "netlist_sha256": digest(output / "synth.json"),
        "placer_seed": getattr(winner, "seed", None),
        "placer_heap_timingweight": getattr(winner, "weight", None),
        "tools": tools,
    }
    (out / "rbf.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    print(json.dumps(record, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
