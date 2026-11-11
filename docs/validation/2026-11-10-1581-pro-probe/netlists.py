#!/usr/bin/env python3
"""S1b: Yosys netlists of two blocks with no CPU, in the pc386 Quartus producer's shape
(build_fes_pc386_quartus.machine_program), for a Quartus Pro analysis compile.

  python3 netlists.py --yosys /abs/slot/install/bin/yosys --output ~/fes-scratch/s1b/netlists

Writes, per netlist, <name>.raw.v (Yosys's escaped names) and <name>.v (the producer's
plain_identifiers rewrite, which Quartus 17.0.2 needs), plus yosys-<name>.log.
"""
import argparse
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[3] / "sources/misteross"
sys.path.insert(0, str(ROOT / "scripts"))
from build_fes_pc386_quartus import OPL2, plain_identifiers  # noqa: E402

OPL2_TREE = (
    f"{OPL2}/opl2_pkg.sv",
    *(f"{OPL2}/{name}.sv" for name in (
        "pipeline_sr", "mem_simple_dual_port", "mem_simple_dual_port_async_read", "mem_single_bank",
        "mem_single_bank_reset", "opl2_log_sine_lut", "opl2_exp_lut", "ksl_add_rom", "tremolo", "vibrato",
        "calc_phase_inc", "calc_envelope_shift", "calc_rhythm_phase", "envelope_generator", "phase_generator",
        "operator", "control_operators", "dac_prep", "channels")),
)
OPL2_MACHINE = (*OPL2_TREE, "cores/fes-pc386/rtl/pc386_rate.sv", "cores/fes-pc386/rtl/pc386_opl2.sv")
BLOCKS = {
    # netlist name: (top, sources, hierarchy options, extra passes before write_verilog)
    "pc386_opl2": ("pc386_opl2", OPL2_MACHINE, "", ""),
    # A `unique case` leaves parallel_case on the $pmux cell and write_verilog adds its
    # own, which Quartus Pro rejects as a duplicate attribute (error 22074).
    "pc386_opl2.fixed": ("pc386_opl2", OPL2_MACHINE, "", "attrmap -remove parallel_case; "),
    # WINDOW_BASE is FES_APPLICATION_HPS_DDR_WINDOW_BASE; at the zero default the reader
    # never starts and its FIFO is removed. The Cyclone V hint is illegal on Agilex 5;
    # rewrite it in the netlist.
    "fes_menu_reader": ("fes_menu_reader", ("cores/fes-menu/rtl/fes_menu_reader.v",), " -chparam WINDOW_BASE 32'h30000000",
                        'attrmap -map ramstyle="M10K" ramstyle="M20K"; '),
}


def program(top, sources, options, extra, netlist):
    return (
        f"read_verilog -sv {' '.join(sources)}; "
        f"hierarchy -top {top}{options}; proc -norom; flatten; opt -purge; memory_collect; "
        "memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d; opt -purge; "
        "attrmap -remove src; attrmap -remove hdlname; "
        f"{extra}write_verilog {netlist.as_posix()}"
    )


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--yosys", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    print(subprocess.check_output([str(args.yosys), "-V"], text=True).strip())
    for name, (top, sources, options, extra) in BLOCKS.items():
        raw = output / f"{name}.raw.v"
        script = program(top, sources, options, extra, raw)
        (output / f"yosys-{name}.ys").write_text(script + "\n")
        subprocess.run([str(args.yosys), "-q", "-l", str(output / f"yosys-{name}.log"), "-p", script],
                       cwd=ROOT, check=True)
        text = raw.read_text()
        (output / f"{name}.v").write_text(plain_identifiers(text))
        print(name, "escaped identifiers:", text.count("\\"), "ramstyle:",
              sorted({line.strip() for line in text.splitlines() if "ramstyle" in line}))


if __name__ == "__main__":
    main()
