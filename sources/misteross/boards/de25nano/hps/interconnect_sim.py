#!/usr/bin/env python3
"""Simulate the exact generated DE25 probe-to-AXI256 transport, without hardware."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys


MASTER = "subsys_periph_fes_probe_mem_"
SLAVE = "subsys_hps_f2sdram_adapter_axi4_sub_"
TOP = "fes_de25_interconnect_test"


def sha(data):
    return hashlib.sha256(data).hexdigest()


def uncomment(source):
    return re.sub(r"/\*.*?\*/|//[^\n]*", " ", source, flags=re.S)


def source_closure(project):
    """Select complete, unchanged modules from the generated top-level QIP."""
    qip = project / "qsys_top/qsys_top.qip"
    qip_data = qip.read_bytes()
    files = {}
    modules = {}
    for rel in re.findall(r'-name (?:SYSTEMVERILOG|VERILOG)_FILE \[file join \$::quartus\(qip_path\) "([^"]+)"\]',
                          qip_data.decode()):
        path = (qip.parent / rel).resolve()
        path.relative_to(project)
        data = path.read_bytes()
        if path in files:
            continue
        source = uncomment(data.decode())
        definitions = re.findall(r"^[ \t]*module\s+(\w+)\b(.*?^[ \t]*endmodule\b)", source, re.M | re.S)
        names = [name for name, _ in definitions]
        files[path] = (data, source, names)
        for name, body in definitions:
            modules.setdefault(name, []).append((path, re.sub(r"\s+", " ", body).strip()))
    candidates = [p for p, (_, s, _) in files.items()
                  if re.search(r"\bmodule\s+qsys_top_altera_mm_interconnect_\w+\b", s)
                  and MASTER + "address" in s and SLAVE + "araddr" in s]
    if len(candidates) != 1:
        raise ValueError(f"expected one generated probe-to-AXI interconnect, found {len(candidates)}")
    top_path = candidates[0]
    top_module = files[top_path][2][0]
    selected = set()

    def visit(path):
        if path in selected:
            return
        selected.add(path)
        source = files[path][1]
        for name, definitions in modules.items():
            if name in files[path][2]:
                continue
            if re.search(r"\b" + re.escape(name) + r"\s*(?:#\s*\(|\w+\s*\()", source):
                if len({body for _, body in definitions}) != 1:
                    raise ValueError(f"ambiguous referenced generated module {name}")
                visit(definitions[0][0])

    visit(top_path)
    return qip, qip_data, top_path, top_module, {p: files[p][0] for p in selected}


def wrapper(source, module):
    header = re.search(r"\bmodule\s+" + re.escape(module) + r"\s*\((.*?)\n\s*\);", uncomment(source), re.S)
    if not header:
        raise ValueError("unsupported generated interconnect header")
    pattern = r"\b(input|output)\s+wire\s*(\[\d+:\d+\])?\s*(\w+)\s*,?"
    ports = re.findall(pattern, header[1])
    if not ports:
        raise ValueError("generated interconnect has no parsed ports")
    if re.sub(pattern, "", header[1]).strip(" \t\r\n,"):
        raise ValueError("unsupported generated interconnect port declaration")
    expected_master = {"address": ("input", "[31:0]"), "burstcount": ("input", "[4:0]"),
                       "read": ("input", ""), "byteenable": ("input", "[15:0]"),
                       "waitrequest": ("output", ""), "readdata": ("output", "[127:0]"),
                       "readdatavalid": ("output", ""), "response": ("output", "[1:0]")}
    seen = {name: (direction, width) for direction, width, name in ports}
    for name, specification in expected_master.items():
        if seen.get(MASTER + name) != specification:
            raise ValueError(f"generated master port mismatch: {name}")
    if seen.get(SLAVE + "rdata") != ("input", "[255:0]") or seen.get(SLAVE + "araddr") != ("output", "[31:0]"):
        raise ValueError("generated subordinate is not the selected AXI256/address32 port")
    axi = {"arid": "output wire [4:0]", "araddr": "output wire [31:0]",
           "arlen": "output wire [7:0]", "arsize": "output wire [2:0]",
           "arburst": "output wire [1:0]", "arvalid": "output wire", "arready": "input wire",
           "rid": "input wire [4:0]", "rdata": "input wire [255:0]", "rresp": "input wire [1:0]",
           "rlast": "input wire", "rvalid": "input wire", "rready": "output wire",
           "awvalid": "output wire", "wvalid": "output wire"}
    external = ["input wire clk, reset_n", "input wire [6:0] address", "input wire read, write",
                "input wire [3:0] byteenable", "input wire [31:0] writedata",
                "output wire [31:0] readdata", "output wire readdatavalid, waitrequest"]
    external.extend(f"{kind} {name}" for name, kind in axi.items())
    external.extend(f"output wire {width} probe_{name}" for name, (direction, width) in expected_master.items())
    lines = [f"module {TOP} (\n    " + ",\n    ".join(external) + "\n);",
             "fes_de25_ddr_probe probe ( .clk(clk), .reset_n(reset_n), .address(address),",
             " .read(read), .write(write), .byteenable(byteenable), .writedata(writedata),",
             " .readdata(readdata), .readdatavalid(readdatavalid), .waitrequest(waitrequest),",
             ",\n".join(f" .avm_{name}(probe_{name})" for name in expected_master) + ");"]
    connections = []
    for direction, width, name in ports:
        if name.startswith(MASTER):
            role = name.removeprefix(MASTER)
            if role not in expected_master:
                raise ValueError(f"unexpected probe interface port {role}")
            value = "probe_" + role
        elif name.startswith(SLAVE) and name.removeprefix(SLAVE) in axi:
            value = name.removeprefix(SLAVE)
        elif direction == "input" and name.endswith("_clk"):
            value = "clk"
        elif direction == "input" and "reset" in name:
            if name.endswith("reset_n"):
                raise ValueError("unexpected active-low generated reset")
            value = "~reset_n"
        elif direction == "input":
            value = "'0"
        else:
            value = ""
        connections.append(f" .{name}({value})")
    lines.extend([f"{module} transport (", ",\n".join(connections), ");", "endmodule", ""])
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", type=Path, required=True, help="completed IP generation's project directory")
    parser.add_argument("--output", type=Path, required=True, help="new private simulation output directory")
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args(argv)
    project = args.project.resolve(strict=True)
    output = args.output.absolute()
    output.mkdir(parents=True, exist_ok=False)
    receipt = {"format": 1, "classification": "generated-interconnect-transport-simulation",
               "hardware_access": "not-performed", "admission": "not-qualified",
               "project": str(project), "coverage": "real probe and unchanged generated Avalon128-to-AXI256 interconnect",
               "not_modeled": ["HPS hard block", "MPFE/TBU", "SMMU translation", "firewall", "DDR controller", "physical memory"],
               "status": "failed"}
    try:
        qip, qip_data, top_path, module, sources = source_closure(project)
        probe = project / "fes_mailbox/fes_de25_ddr_probe.v"
        sources[probe] = probe.read_bytes()
        for name in ("fes_de25_ddr_probe.vh", "fes_de25_ddr_probe.hpp"):
            path = project / "fes_mailbox/generated" / name
            sources[path] = path.read_bytes()
        receipt["source_sha256"] = {str(p.relative_to(project)): sha(data) for p, data in sorted(sources.items())}
        receipt["qip_sha256"] = sha(qip_data)
        receipt["interconnect"] = str(top_path.relative_to(project))
        receipt["interconnect_module"] = module
        inputs = project.parent / "inputs.json"
        if inputs.is_file():
            receipt["build_inputs_sha256"] = sha(inputs.read_bytes())
            receipt["build_inputs"] = json.loads(inputs.read_text())
        for path, data in sources.items():
            copy = output / "sources" / path.relative_to(project)
            copy.parent.mkdir(parents=True, exist_ok=True)
            copy.write_bytes(data)
        wrapper_path = output / (TOP + ".v")
        wrapper_path.write_text(wrapper(sources[top_path].decode(), module))
        receipt["wrapper_sha256"] = sha(wrapper_path.read_bytes())
        bench = Path(__file__).with_name("interconnect_tb.cpp").resolve(strict=True)
        bench_data = bench.read_bytes()
        bench_copy = output / bench.name
        bench_copy.write_bytes(bench_data)
        receipt["bench_sha256"] = sha(bench_data)
        receipt["helper_sha256"] = sha(Path(__file__).read_bytes())
        verilator = shutil.which(args.verilator)
        if not verilator:
            raise ValueError("Verilator not found")
        receipt["verilator"] = {"path": verilator, "version": subprocess.check_output([verilator, "--version"], text=True).strip()}
        includes = sorted({str((output / "sources" / p.relative_to(project)).parent) for p in sources})
        rtl = sorted(str(output / "sources" / p.relative_to(project)) for p in sources if p.suffix in (".v", ".sv"))
        command = [verilator, "--cc", "--exe", "--build", "-j", "2", "-O2", "-Wno-fatal", "--assert",
                   "--top-module", TOP, "--Mdir", str(output / "obj"),
                   "-CFLAGS", "-I" + str(output / "sources/fes_mailbox"),
                   *("-I" + path for path in includes), str(wrapper_path), *rtl, str(bench_copy)]
        receipt["build_command"] = command
        with (output / "build.log").open("w") as log:
            subprocess.run(command, check=True, stdout=log, stderr=subprocess.STDOUT)
        receipt["executable_sha256"] = sha((output / "obj" / ("V" + TOP)).read_bytes())
        with (output / "simulation.log").open("w") as log:
            subprocess.run([str(output / "obj" / ("V" + TOP))], check=True, stdout=log, stderr=subprocess.STDOUT)
        receipt["cases"] = [json.loads(line) for line in (output / "simulation.log").read_text().splitlines() if line.startswith("{")]
        if len(receipt["cases"]) != 10 or not all(case.get("status") == "pass" for case in receipt["cases"]):
            raise ValueError("simulation did not report the ten required passing cases")
        if qip.read_bytes() != qip_data or any(p.read_bytes() != data for p, data in sources.items()):
            raise ValueError("generated source changed during simulation")
        receipt["status"] = "pass"
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        receipt["error"] = str(error)
    finally:
        for name in ("build.log", "simulation.log"):
            path = output / name
            if path.is_file():
                receipt.setdefault("log_sha256", {})[name] = sha(path.read_bytes())
        (output / "receipt.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"status": receipt["status"], "receipt": str(output / "receipt.json")}, sort_keys=True))
    return 0 if receipt["status"] == "pass" else 1


if __name__ == "__main__":
    sys.exit(main())
