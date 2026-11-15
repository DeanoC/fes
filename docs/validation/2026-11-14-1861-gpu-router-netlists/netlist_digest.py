#!/usr/bin/env python3
"""Write one core's Yosys netlist from a given tree and print its digest.

D12 asks whether a producer-only edit leaves a core's netlist byte-identical to
main. The netlist is a Yosys product, so this runs each producer's own Yosys
command and nothing else: no placement, no routing, no GPU.

Every run passes BUILD_ID as 32 zeros rather than the record's real id, which
is what the producers that already have --synth-only do. Both sides therefore
synthesise the same constant, and a surviving difference is the source change
itself rather than the BUILD_ID the record moved.

  python3 netlist_digest.py --tree /path/to/sources/misteross --core fes.pong \
      --cache-root /abs/cache --out /abs/out/pong-branch

The tree under test supplies the producer: this program only drives it. Run it
once per side with one cache root, so both sides use one tool install.
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

BUILD_ID = "0" * 32

# Where each producer keeps the input list that decides its netlist.
PINNED = ("PINNED_INPUTS", "INPUTS")


def digest(path: Path) -> str:
    out = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            out.update(block)
    return out.hexdigest()


def _git(tree: Path, *arguments: str) -> str:
    return subprocess.run(("git", "-C", str(tree), *arguments),
                          capture_output=True, text=True, check=True).stdout.strip()


def source_identity(tree: Path, producer, *, allow_dirty: bool) -> dict:
    """Pin the bytes that were synthesised, not just the path they came from.

    A worktree path is mutable and a branch advances, so a digest attributed to
    a reviewed head has to carry the revision it was produced at. The pinned
    input digest is stronger still: it covers the producer's own declared
    inputs whatever git says.
    """
    revision = _git(tree, "rev-parse", "HEAD")
    status = _git(tree, "status", "--porcelain")
    inputs = None
    for name in PINNED:
        candidate = getattr(producer, name, None)
        if candidate:
            inputs = tuple(candidate)
            break
    if inputs is None and hasattr(producer, "inputs_for"):
        inputs = tuple(producer.inputs_for(100))
    if inputs is None:
        raise SystemExit(f"{producer.__name__}: no pinned input list to hash")
    listing = [[relative, digest(producer.ROOT / relative)] for relative in sorted(inputs)]
    dirty = [line for line in status.splitlines() if line.strip()]
    tracked = {relative for relative in inputs}
    dirty_inputs = sorted(line[3:] for line in dirty
                          if line[3:].removeprefix("sources/misteross/") in tracked)
    if dirty_inputs and not allow_dirty:
        raise SystemExit("refusing to record a netlist from modified pinned inputs: "
                         + ", ".join(dirty_inputs))
    return {
        "revision": revision,
        "tree_clean": not dirty,
        "dirty_pinned_inputs": dirty_inputs,
        "pinned_inputs_sha256": hashlib.sha256(
            json.dumps(listing, separators=(",", ":")).encode()).hexdigest(),
        "pinned_input_count": len(listing),
    }


def _load(tree: Path, module: str):
    """Import a producer from the tree under test, not from this checkout."""
    for stale in [name for name in sys.modules if name == "scripts" or name.startswith("scripts.")]:
        del sys.modules[stale]
    sys.path.insert(0, str(tree))
    os.chdir(tree)
    return importlib.import_module(f"scripts.{module}")


def synth_pong(tree: Path, cache_root: Path):
    producer = _load(tree, "build_fes_pong")
    from scripts.functional_execution import FunctionalInvocation

    authenticated = producer._authenticate_tools(producer.ROOT, cache_root=cache_root)
    invocation = FunctionalInvocation(authenticated, 0)
    try:
        output = producer._prepare_output(
            producer.ROOT, relative=producer.OUTPUT_RELATIVE,
            build_outputs=producer.BUILD_OUTPUTS)
        commands = producer.build_commands(
            producer.ROOT, output, BUILD_ID,
            {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")})
        _fresh(output / "synth.json")
        producer._run_tool(commands[0], producer.ROOT, output / "yosys.log",
                           env=invocation.env, audit_source_root=producer.ROOT,
                           output_relative=producer.OUTPUT_RELATIVE)
    finally:
        invocation.close()
    return output / "synth.json", {name: tool.identity for name, tool in authenticated.items()}, producer


def _fresh(netlist: Path) -> Path:
    """Delete the netlist first, so a crashed Yosys cannot leave a stale one.

    DeanoC/yosys#77: 3a5accbd can segfault in OPT_CLEAN. _run_tool does raise
    on a non-zero exit (its only tolerance is a finished nextpnr route), but a
    comparison must not rest on reading that correctly: if no netlist is
    written, there must be nothing there to compare.
    """
    netlist.unlink(missing_ok=True)
    return netlist


def _require(netlist: Path) -> Path:
    if not netlist.is_file() or netlist.stat().st_size == 0:
        raise SystemExit(f"no netlist was written: {netlist}")
    return netlist


def _run_yosys(producer, authenticated, output, command, output_relative):
    """Each producer's own Yosys invocation, with its own audited environment."""
    from scripts.functional_execution import FunctionalInvocation

    _fresh(output / "synth.json")
    # Producers reach the shared runner either directly or through `board`.
    run_tool = getattr(producer, "_run_tool", None) or producer.board._run_tool
    invocation = FunctionalInvocation(authenticated, 0)
    try:
        run_tool(command, producer.ROOT, output / "yosys.log",
                 env=invocation.env, audit_source_root=producer.ROOT,
                 output_relative=output_relative)
    finally:
        invocation.close()


def synth_menu(tree: Path, cache_root: Path):
    """fes.menu is the package producer, which is the one that carries BUILD_ID."""
    producer = _load(tree, "build_fes_menu_package")
    authenticated = producer._authenticate_tools(producer.ROOT, cache_root=cache_root)
    output = producer.board._prepare_output(
        producer.ROOT, relative=producer.OUTPUT, build_outputs=producer.OUTPUTS)
    yosys, _route = producer.build_commands(
        BUILD_ID, {name: authenticated[name].path
                   for name in ("yosys", "nextpnr-mistral")})
    _run_yosys(producer, authenticated, output, yosys, producer.OUTPUT)
    return output / "synth.json", {n: t.identity for n, t in authenticated.items()}, producer


def synth_ramtest(tree: Path, cache_root: Path):
    producer = _load(tree, "build_fes_ramtest")
    authenticated = producer.authenticate_for(producer.ROOT, 100, cache_root)
    relative = producer.output_for(100)
    output = producer.board._prepare_output(
        producer.ROOT, relative=relative, build_outputs=producer.BUILD_OUTPUTS)
    commands = producer.build_commands(
        producer.ROOT, BUILD_ID, {name: authenticated[name].path
                                  for name in ("yosys", "nextpnr-mistral")},
        memory_mhz=100)
    _run_yosys(producer, authenticated, output, commands[0], relative)
    return output / "synth.json", {n: t.identity for n, t in authenticated.items()}, producer


def synth_riscv(tree: Path, cache_root: Path):
    producer = _load(tree, "build_fes_riscv")
    authenticated = producer._authenticate_tools(producer.ROOT, cache_root=cache_root)
    output = producer.board._prepare_output(
        producer.ROOT, relative=producer.OUTPUT, build_outputs=producer.BUILD_OUTPUTS)
    commands = producer.build_commands(
        BUILD_ID, {name: authenticated[name].path
                   for name in ("yosys", "nextpnr-mistral")})
    _run_yosys(producer, authenticated, output, commands[0], producer.OUTPUT)
    return output / "synth.json", {n: t.identity for n, t in authenticated.items()}, producer


def _zx81_model(model: str):
    def synth(tree: Path, cache_root: Path):
        producer = _load(tree, "build_fes_zx81_oss")
        authenticated = producer._authenticate_tools(
            producer.ROOT, cache_root=cache_root, gpu_router="HIP")
        relative = producer.model_for(model).output
        output = producer.ROOT / relative
        output.mkdir(parents=True, exist_ok=True)
        yosys, _route = producer.build_commands(
            producer.ROOT, output, BUILD_ID,
            {name: authenticated[name].path
             for name in ("yosys", "nextpnr-mistral")}, model=model)
        _run_yosys(producer, authenticated, output, yosys, relative)
        return output / "synth.json", {n: t.identity for n, t in authenticated.items()}, producer
    return synth


def _spectrum_model(model: str):
    """Spectrum already has synth(): it pins the same BUILD_ID this program does."""
    def synth(tree: Path, cache_root: Path):
        producer = _load(tree, "build_fes_spectrum_oss")
        _fresh(producer.ROOT / producer.model_for(model).output / "synth.json")
        evidence = producer.synth(producer.ROOT, cache_root=cache_root, model=model)
        output = producer.ROOT / producer.model_for(model).output
        return output / "synth.json", evidence["tools"], producer
    return synth


def synth_c64(tree: Path, cache_root: Path):
    producer = _load(tree, "build_fes_c64_oss")
    _fresh(producer.ROOT / producer.OUTPUT_RELATIVE / "synth.json")
    evidence = producer.synth(producer.ROOT, cache_root=cache_root)
    output = producer.ROOT / producer.OUTPUT_RELATIVE
    return output / "synth.json", evidence["tools"], producer


def synth_coleco(tree: Path, cache_root: Path):
    producer = _load(tree, "build_fes_coleco_socket_v2")
    authenticated = producer.authenticate_tools(producer.ROOT, cache_root)
    relative = producer.OUTPUT_RELATIVE
    output = producer._prepare_output(
        producer.ROOT, relative=relative, build_outputs=producer.BUILD_OUTPUTS)
    yosys, _route = producer.build_commands(
        producer.ROOT, output, BUILD_ID,
        {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")})
    _run_yosys(producer, authenticated, output, yosys, relative)
    return output / "synth.json", {n: t.identity for n, t in authenticated.items()}, producer


def synth_splash(tree: Path, cache_root: Path):
    """The splash seals the image-pinned idle RBF and is not a recipe core.

    It authenticates the repository-wide lock with gpu_router=OFF and refuses a
    shared cache, so it needs a local install at <tree>/build/toolchain. Its
    Yosys program sets no BUILD_ID at all, so this comparison does not even
    need the zeroed constant the others use.
    """
    producer = _load(tree, "build_fes_splash")
    os.environ.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
    authenticated = producer.authenticate_oss_tools(producer.ROOT)
    output = producer._prepare_output(producer.ROOT)
    commands = producer.build_commands(
        producer.ROOT, {"yosys": authenticated["yosys"].path}, synth_only=True)
    _fresh(output / "synth.json")
    producer.board._run_tool(commands[0], producer.ROOT, output / "yosys.log",
                             output_relative=producer.OUTPUT_RELATIVE)
    return output / "synth.json", {n: t.identity for n, t in authenticated.items()}, producer


CORES = {
    "fes.pong": synth_pong,
    "fes.splash": synth_splash,
    "fes.coleco": synth_coleco,
    "fes.spectrum": _spectrum_model("48"),
    "fes.spectrum128": _spectrum_model("128"),
    "fes.spectrum-plus3": _spectrum_model("plus3"),
    "fes.pentagon": _spectrum_model("pentagon"),
    "fes.c64": synth_c64,
    "fes.menu": synth_menu,
    "fes.ramtest": synth_ramtest,
    "fes.riscv": synth_riscv,
    "fes.zx81": _zx81_model("zx81"),
    "fes.zx80": _zx81_model("zx80"),
    "fes.ts1000": _zx81_model("ts1000"),
    "fes.ts1500": _zx81_model("ts1500"),
}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True,
                        help="the sources/misteross of the tree under test")
    parser.add_argument("--core", required=True, choices=sorted(CORES))
    parser.add_argument("--cache-root", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True,
                        help="directory to copy the netlist and its digest into")
    parser.add_argument("--allow-dirty", action="store_true",
                        help="record a run whose pinned inputs are modified (not evidence)")
    arguments = parser.parse_args(argv)
    tree = arguments.tree.resolve()
    out = arguments.out.resolve()
    out.mkdir(parents=True, exist_ok=True)
    netlist, tools, producer = CORES[arguments.core](tree, arguments.cache_root.resolve())
    _require(netlist)
    copied = out / "synth.json"
    copied.write_bytes(netlist.read_bytes())
    record = {"core": arguments.core, "tree": str(tree), "build_id": BUILD_ID,
              "netlist_sha256": digest(copied), "tools": tools,
              **source_identity(tree, producer, allow_dirty=arguments.allow_dirty)}
    (out / "netlist.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    print(json.dumps(record, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
