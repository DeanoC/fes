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
import sys
from pathlib import Path

BUILD_ID = "0" * 32


def digest(path: Path) -> str:
    out = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            out.update(block)
    return out.hexdigest()


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
        producer._run_tool(commands[0], producer.ROOT, output / "yosys.log",
                           env=invocation.env, audit_source_root=producer.ROOT,
                           output_relative=producer.OUTPUT_RELATIVE)
    finally:
        invocation.close()
    return output / "synth.json", {name: tool.identity for name, tool in authenticated.items()}


CORES = {"fes.pong": synth_pong}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True,
                        help="the sources/misteross of the tree under test")
    parser.add_argument("--core", required=True, choices=sorted(CORES))
    parser.add_argument("--cache-root", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True,
                        help="directory to copy the netlist and its digest into")
    arguments = parser.parse_args(argv)
    tree = arguments.tree.resolve()
    out = arguments.out.resolve()
    out.mkdir(parents=True, exist_ok=True)
    netlist, tools = CORES[arguments.core](tree, arguments.cache_root.resolve())
    copied = out / "synth.json"
    copied.write_bytes(netlist.read_bytes())
    record = {"core": arguments.core, "tree": str(tree), "build_id": BUILD_ID,
              "netlist_sha256": digest(copied), "tools": tools}
    (out / "netlist.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    print(json.dumps(record, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
