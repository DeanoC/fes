#!/usr/bin/env python3
"""D12 condition 1 for fes.pcxt-dos: its Yosys front end's netlist.

PC/XT elaborates its whole top with Yosys and places with Quartus, so its
netlist is elaborated.v from quartus_fes_pcxt's own program, run here alone:
the package and DOS profiles (what fes.pcxt-dos ships) with BUILD_ID pinned
to 32 zeros. Both sides run in one work path, so the roots substituted into
the program, and the src paths Yosys records, are the same on both sides.

  python3 pcxt_netlist.py --tree <tree>/sources/misteross --yosys <yosys> \
      --work /abs/work --out /abs/out/pcxt-base
"""
import argparse, hashlib, importlib, json, os, shutil, subprocess, sys
from pathlib import Path

BUILD_ID = "0" * 32


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tree", type=Path, required=True)
    parser.add_argument("--yosys", type=Path, required=True)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    tree = args.tree.resolve()
    sys.path.insert(0, str(tree))
    os.chdir(tree)
    producer = importlib.import_module("scripts.quartus_fes_pcxt")
    yosys = args.yosys.resolve(strict=True)
    expected = __import__("tomllib").loads((tree / producer.CORE / "toolchain.lock").read_text())["tool"]["yosys"]["commit"]
    version = subprocess.check_output([str(yosys), "-V"], text=True).strip()
    if "git sha1 " + expected[:9] not in version:
        raise SystemExit("not the PC/XT lock's Yosys")
    library = yosys.parent.parent / "share/yosys/intel_alm/common/megafunction_bb.v"
    shutil.rmtree(args.work, ignore_errors=True)
    args.work.mkdir(parents=True)
    source, identities = producer.capture_sources(tree, args.work)
    work = args.work / "elaboration"
    work.mkdir()
    producer.prepare_elaboration_inputs(source, work, dos_profile=True)
    program = producer.executed_elaboration(source, work, library, build_id=BUILD_ID, dos_profile=True)
    (work / "elaborate.ys").write_text(program)
    with (work / "elaborate.log").open("w") as log:
        subprocess.run([str(yosys), "-s", "elaborate.ys"], cwd=work, stdout=log, stderr=subprocess.STDOUT, check=True)
    netlist = work / "elaborated.v"
    if not netlist.is_file() or netlist.stat().st_size == 0:
        raise SystemExit("no netlist was written")
    args.out.mkdir(parents=True, exist_ok=True)
    shutil.copy2(netlist, args.out / "elaborated.v")
    summary = {
        "core": "fes.pcxt-dos", "tree": str(tree),
        "revision": subprocess.check_output(["git", "-C", str(tree), "rev-parse", "HEAD"], text=True).strip(),
        "tree_clean": not subprocess.check_output(["git", "-C", str(tree), "status", "--porcelain"], text=True).strip(),
        "yosys": version, "yosys_sha256": digest(yosys), "work": str(args.work),
        "inputs_sha256": hashlib.sha256(json.dumps(identities, sort_keys=True).encode()).hexdigest(),
        "input_count": len(identities), "netlist_sha256": digest(netlist),
    }
    (args.out / "netlist.json").write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
    print(json.dumps(summary, indent=2, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
