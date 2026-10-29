#!/usr/bin/env python3
"""Run real ZX81 CPU/ROM and RAM-pack RTL against the PR control state machine."""
import argparse
import importlib.util
import json
from pathlib import Path
import subprocess
HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("de25_zx81_pr_builder", HERE.parent / "build.py")
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)
build.PROFILE = build.load_profile(HERE / "profile.py")

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--output", required=True, type=Path)
    args = p.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    frozen = build.capture()
    local = output / "fes_link"
    local.mkdir()
    for name in build.SOURCE_NAMES:
        target = local / name.removeprefix("fpga/")
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(frozen["files"]["sources/misteross/boards/de25nano/link-proof/" + name])
    build.PROFILE.prepare(local, output, frozen)
    for name in ("sim_top.v", "tb.cpp"):
        (local / name).write_bytes((HERE / name).read_bytes())
    rtl = [local / name for name in ("shell.v", "personas.v", "static.v", "sim_top.v", "zx81_machine.sv",
        "zx81_machine_clock.v", "zx81_dpram.v", "zx81_ram_pack.v", "fes_z80_alu.sv", "fes_z80_engine.sv", "fes_z80_bus.sv", "fes_z80_nmos.sv")]
    build.run(["verilator", "--cc", "--exe", "--build", "-j", "2", "--x-initial", "unique", "-Wno-fatal",
        "--top-module", "fes_zx81_pr_sim", "-I" + str(local), "--Mdir", str(output / "obj_dir")] +
        [str(x) for x in rtl] + [str(local / "tb.cpp")], output, "build.log")
    build.run([str(output / "obj_dir/Vfes_zx81_pr_sim"), "+verilator+rand+reset+2", "+verilator+seed+17"], output, "simulation.log")
    sources = {str(x.relative_to(output)): build.sha(x.read_bytes()) for x in local.rglob("*") if x.is_file()}
    sources.update({"sim.py": build.sha(Path(__file__).read_bytes())})
    (output / "simulation.json").write_bytes(build.canonical({"status": "passed", "kind": "actual-ZX81-CPU-ROM-RAM-pack-RTL",
        "source_revision": frozen["revision"], "source_committed": frozen["committed"], "source_sha256": sources,
        "log_sha256": build.sha((output / "simulation.log").read_bytes()), "physical_PR": "not-modeled",
        "primitive_memory": "behavioral", "hardware_acceptance": "not-run"}) + b'\n')
    print(output)
if __name__ == "__main__": main()
