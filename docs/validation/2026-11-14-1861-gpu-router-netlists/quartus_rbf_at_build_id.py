#!/usr/bin/env python3
"""D12 condition 2 for the ZX81 family: a Quartus fit at a chosen BUILD_ID.

#1891 moved fes.zx81/zx80/ts1000/ts1500 onto build_fes_zx81_quartus, so their
condition 2 ("a build with the PR's tooling and main's BUILD_ID reproduces
main's RBF byte for byte") is a Quartus fit rather than a nextpnr route.

The producer derives BUILD_ID from its own record and offers no override, so
as in rbf_at_build_id.py the comparison is made the other way round: both
sides are compiled at the **same** chosen BUILD_ID, which is main's own,
derived on the base tree. The BUILD_ID reaches the design as a QSF Verilog
macro, so holding it equal makes every Quartus input byte-identical and
isolates exactly what the condition asks.

This calls the producer's own write_project, compile_project and
seed_evidence. It is not a replica of them: a replica that drifted from the
producer would give both sides the same wrong project and compare them
successfully. write_project is handed the producer's own canonical QSF digest,
so a project that does not match what the record names is refused here too.

Only the sealed seed is compiled. The producer's own gated build already
compiles three sign-off seeds plus a repeat of the first and refuses a build
whose repeat changed the RBF, so Quartus determinism is enforced there, by it,
rather than assumed here.

  QUARTUS_ROOTDIR=/abs/intelFPGA_lite/17.0/quartus \
    python3 quartus_rbf_at_build_id.py --tree <tree>/sources/misteross \
        --model zx81 --cache-root /abs/cache --build-id <32 hex> --out /abs/out
"""
from __future__ import annotations

import argparse
import hashlib
import importlib
import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

HEX32 = set("0123456789abcdef")


def digest(path: Path) -> str:
    out = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            out.update(block)
    return out.hexdigest()


def _git(tree: Path, *arguments: str) -> str:
    return subprocess.run(("git", "-C", str(tree), *arguments),
                          capture_output=True, text=True, check=True).stdout.strip()


def _load(tree: Path):
    for stale in [n for n in sys.modules if n == "scripts" or n.startswith("scripts.")]:
        del sys.modules[stale]
    sys.path.insert(0, str(tree))
    os.chdir(tree)
    return importlib.import_module("scripts.build_fes_zx81_quartus")


def source_identity(tree: Path, producer, *, allow_dirty: bool) -> dict:
    """Pin the bytes that were compiled, and refuse modified declared inputs.

    A forced compile bypasses the producer's own clean-source check, so it has
    to make the same guarantee itself, or the report cannot say which bytes
    produced the RBF. The producer declares its inputs as SOURCE_ROOTS.
    """
    roots = tuple(getattr(producer, "SOURCE_ROOTS"))
    listing, missing = [], []
    for relative in sorted(roots):
        path = producer.ROOT / relative
        if path.is_file():
            listing.append([relative, digest(path)])
        elif path.is_dir():
            for child in sorted(p for p in path.rglob("*") if p.is_file()):
                listing.append([child.relative_to(producer.ROOT).as_posix(), digest(child)])
        else:
            missing.append(relative)
    if missing:
        raise SystemExit("declared source root is absent: " + ", ".join(missing))
    status = _git(tree.parent.parent, "status", "--porcelain")
    tracked = {entry[0] for entry in listing}
    dirty = sorted(line[3:] for line in status.splitlines() if line.strip()
                   and line[3:].removeprefix("sources/misteross/") in tracked)
    if dirty and not allow_dirty:
        raise SystemExit("refusing to compile from modified declared inputs: " + ", ".join(dirty))
    return {
        "revision": _git(tree.parent.parent, "rev-parse", "HEAD"),
        "tree_clean": not status.strip(),
        "dirty_declared_inputs": dirty,
        "declared_inputs_sha256": hashlib.sha256(
            json.dumps(listing, separators=(",", ":")).encode()).hexdigest(),
        "declared_input_count": len(listing),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree", type=Path, required=True)
    parser.add_argument("--model", required=True, choices=("zx81", "zx80", "ts1000", "ts1500"))
    parser.add_argument("--cache-root", type=Path, required=True)
    parser.add_argument("--build-id", required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--allow-dirty", action="store_true")
    arguments = parser.parse_args()
    if len(arguments.build_id) != 32 or any(c not in HEX32 for c in arguments.build_id):
        raise SystemExit("--build-id must be 32 lowercase hexadecimal characters")
    tree = arguments.tree.resolve()
    out = arguments.out.resolve()
    out.mkdir(parents=True, exist_ok=True)

    producer = _load(tree)
    from scripts.functional_execution import execution_environment

    machine = producer.model_for(arguments.model)
    recipe = producer.selected_recipe(producer.ROOT, machine.core_id)
    seed = producer.recipe_seeds(recipe)[0]
    tools = producer.authenticate_tools(producer.ROOT, cache_root=arguments.cache_root)
    paths = {name: tool.path for name, tool in tools.items()}
    identities = {name: tool.identity for name, tool in tools.items()}
    expected_qsf = producer.canonical_qsf_sha256(producer.ROOT, machine, recipe)

    with tempfile.TemporaryDirectory(prefix="fes-zx81-forced-home-") as home:
        env = execution_environment(Path(home), paths)
        workspace = producer.ROOT / "build" / f"fes-{machine.key}-quartus-forced" / arguments.build_id
        project_dir = workspace / f"seed-{seed}"
        if project_dir.exists():
            raise SystemExit(f"refusing to reuse an existing project directory: {project_dir}")
        project = producer.write_project(producer.ROOT, project_dir, arguments.build_id,
                                         seed, machine, recipe, expected_qsf)
        producer.compile_project(project, paths["quartus"], env)
        evidence = producer.seed_evidence(project, machine)

    # Taken after the fit, so an edit made during it is caught too.
    identity = source_identity(tree, producer, allow_dirty=arguments.allow_dirty)
    rbf = project / "output_files/top.rbf"
    (out / "top.rbf").write_bytes(rbf.read_bytes())
    sealed = producer.canonical_rbf(rbf.read_bytes())
    (out / "core.rbf").write_bytes(sealed)
    record = {
        "core": machine.core_id, "model": machine.key, "front_end": producer.FRONT_END,
        "tree": str(tree), "build_id": arguments.build_id, "seed": seed,
        "rbf_sha256": evidence["rbf_sha256"], "rbf_bytes": rbf.stat().st_size,
        "canonical_rbf_sha256": hashlib.sha256(sealed).hexdigest(),
        "canonical_qsf_sha256": expected_qsf,
        "timing": evidence["timing"], "clocks": evidence["clocks"], "fit": evidence["fit"],
        "tools": identities, **identity,
    }
    (out / "rbf.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    print(json.dumps({k: v for k, v in record.items()
                      if k not in ("timing", "clocks", "fit")}, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
