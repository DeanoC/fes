#!/usr/bin/env python3
"""Exercise the generated FPGA-B transport and RAM endpoint; no hardware access."""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import resource
import shutil
import subprocess
import xml.etree.ElementTree as ET

HERE = Path(__file__).resolve().parent
TOP = "fes_de25_ram_transport_test"
MASTER = "subsys_periph_fes_ramtest_fpga_mem_"
SLAVE = "ramtest_b_emif_lpddr4b_s0_axi4_"
ENDPOINT = "peripheral_subsys_fes_ramtest_fpga"
CASES = ["raw_width_lanes_and_stalls", "endpoint_complete_scan", "read_data_corruption", "read_response_error",
         "write_response_error", "stop_waits_for_write_response", "stop_waits_for_read_response",
         "reset_does_not_invent_completion", "unsolicited_axi_response_asserts"]


def digest(data):
    return hashlib.sha256(data).hexdigest()


def uncomment(source):
    return re.sub(r"/\*.*?\*/|//[^\n]*", " ", source, flags=re.S)


def contained_file(project, path):
    if path.is_symlink():
        raise ValueError("generated input must not be a symlink")
    path = path.resolve(strict=True)
    path.relative_to(project)
    return path.read_bytes()


def ports(source, module):
    header = re.search(r"\bmodule\s+" + re.escape(module) + r"\s*\((.*?)\n\s*\);", uncomment(source), re.S)
    if not header:
        raise ValueError("unsupported generated module header: " + module)
    pattern = r"\b(input|output)\s+wire\s*(\[\d+:\d+\])?\s*(\w+)\s*,?"
    result = re.findall(pattern, header[1])
    if (not result or re.sub(pattern, "", header[1]).strip(" \t\r\n,")
            or len({name for _, _, name in result}) != len(result)):
        raise ValueError("unsupported generated port declaration: " + module)
    return result


def check_elaboration(path):
    modules = ET.parse(path).findall(".//module")
    if any(m.get("origName") in ("altera_syncram", "altsyncram") for m in modules):
        raise ValueError("transport unexpectedly instantiates unmodeled hard RAM")


def source_closure(project):
    qips = {p: contained_file(project, p) for p in [project / "qsys_top/qsys_top.qip",
            *sorted((project / "peripheral_subsys").rglob("*.qip"))]}
    files, definitions = {}, {}
    for manifest, data in qips.items():
        manifest.resolve(strict=True).relative_to(project)
        for relative in re.findall(r'-name (?:SYSTEMVERILOG|VERILOG)_FILE \[file join \$::quartus\(qip_path\) "([^"]+)"\]', data.decode()):
            path = manifest.parent / relative
            raw = contained_file(project, path)
            path = path.resolve(strict=True)
            path.relative_to(project)
            if path in files:
                continue
            source = uncomment(raw.decode())
            modules = re.findall(r"^[ \t]*module\s+(\w+)\b(.*?^[ \t]*endmodule\b)", source, re.M | re.S)
            files[path] = (raw, source, [name for name, _ in modules])
            for name, body in modules:
                definitions.setdefault(name, []).append((path, re.sub(r"\s+", " ", body).strip()))
    roots = [p for p, (_, source, _) in files.items()
             if MASTER + "address" in source and SLAVE + "araddr" in source
             and re.search(r"\bmodule\s+qsys_top_altera_mm_interconnect_\w+\b", source)]
    endpoints = definitions.get(ENDPOINT, [])
    if len(roots) != 1 or len(endpoints) != 1:
        raise ValueError("expected unique generated FPGA-B transport and endpoint")
    memory, endpoint = roots[0], endpoints[0][0]
    selected = set()

    def visit(path):
        if path in selected:
            return
        selected.add(path)
        for name, bodies in definitions.items():
            if name in files[path][2]:
                continue
            if re.search(r"\b" + re.escape(name) + r"\s*(?:#\s*\(|\w+\s*\()", files[path][1]):
                if len({body for _, body in bodies}) != 1:
                    raise ValueError("ambiguous selected generated module: " + name)
                visit(bodies[0][0])
    visit(memory)
    visit(endpoint)
    sources = {p: files[p][0] for p in selected}
    pending = list(sources)
    while pending:
        path = pending.pop()
        for relative in re.findall(r'`include\s+"([^"\n]+)"', uncomment(sources[path].decode())):
            include = path.parent / relative
            data = contained_file(project, include)
            include = include.resolve(strict=True)
            include.relative_to(project)
            if include not in sources:
                sources[include] = data
                pending.append(include)
    return qips, memory, files[memory][2][0], endpoint, sources


def wrapper(memory_source, memory_module, endpoint_source):
    av = {"address": "[31:0]", "burstcount": "[4:0]", "read": "", "write": "",
          "writedata": "[127:0]", "byteenable": "[15:0]", "waitrequest": "",
          "readdata": "[127:0]", "readdatavalid": "", "response": "[1:0]", "writeresponsevalid": ""}
    returned = {"waitrequest", "readdata", "readdatavalid", "response", "writeresponsevalid"}
    axi = {"awid": ("output", "[6:0]"), "awaddr": ("output", "[29:0]"), "awlen": ("output", "[7:0]"),
           "awsize": ("output", "[2:0]"), "awburst": ("output", "[1:0]"), "awlock": ("output", "[0:0]"),
           "awprot": ("output", "[2:0]"), "awuser": ("output", "[13:0]"), "awqos": ("output", "[3:0]"),
           "awvalid": ("output", ""), "awready": ("input", ""), "wdata": ("output", "[255:0]"),
           "wstrb": ("output", "[31:0]"), "wlast": ("output", ""), "wvalid": ("output", ""),
           "wready": ("input", ""), "bid": ("input", "[6:0]"), "bresp": ("input", "[1:0]"),
           "bvalid": ("input", ""), "bready": ("output", ""), "arid": ("output", "[6:0]"),
           "araddr": ("output", "[29:0]"), "arlen": ("output", "[7:0]"), "arsize": ("output", "[2:0]"),
           "arburst": ("output", "[1:0]"), "arlock": ("output", "[0:0]"), "arprot": ("output", "[2:0]"),
           "aruser": ("output", "[13:0]"), "arqos": ("output", "[3:0]"), "arvalid": ("output", ""),
           "arready": ("input", ""), "rid": ("input", "[6:0]"), "rdata": ("input", "[255:0]"),
           "rresp": ("input", "[1:0]"), "rlast": ("input", ""), "rvalid": ("input", ""), "rready": ("output", "")}
    memory_ports = ports(memory_source, memory_module)
    expected = {MASTER + name: ("output" if name in returned else "input", width) for name, width in av.items()}
    expected.update({SLAVE + name: spec for name, spec in axi.items()})
    expected.update({MASTER + "translator_reset_reset_bridge_in_reset_reset": ("input", ""),
                     SLAVE + "translator_clk_reset_reset_bridge_in_reset_reset": ("input", ""),
                     "clk_100_out_clk_clk": ("input", "")})
    if {name: (direction, width) for direction, width, name in memory_ports} != expected:
        raise ValueError("generated FPGA-B Avalon128/AXI256 port contract changed")
    ep = ports(endpoint_source, ENDPOINT)
    controls = {"clk": ("input", ""), "reset_n": ("input", ""), "address": ("input", "[7:0]"),
                "read": ("input", ""), "write": ("input", ""), "byteenable": ("input", "[3:0]"),
                "writedata": ("input", "[31:0]"), "readdata": ("output", "[31:0]"),
                "readdatavalid": ("output", ""), "waitrequest": ("output", ""),
                "writeresponsevalid": ("output", ""), "response": ("output", "[1:0]"),
                "calibrated": ("input", ""), "memory_ready": ("input", "")}
    ep_expected = dict(controls)
    ep_expected.update({"avm_" + name: ("input" if name in returned else "output", width) for name, width in av.items()})
    if {name: (direction, width) for direction, width, name in ep} != ep_expected:
        raise ValueError("generated RAM endpoint contract changed")
    if not re.search(r"\.DATA_W\s*\(128\).*?\.CHANNEL\s*\(1\).*?\.LOCAL_MEMORY\s*\(1\)", endpoint_source, re.S):
        raise ValueError("generated endpoint is not the dedicated FPGA-B scanner")
    top_ports = ["input wire clk, reset_n, raw_mode", "input wire [7:0] address", "input wire read, write",
                 "input wire [3:0] byteenable", "input wire [31:0] writedata", "output wire [31:0] readdata",
                 "output wire readdatavalid, waitrequest, writeresponsevalid", "output wire [1:0] response",
                 "input wire calibrated, memory_ready"]
    top_ports += [f"{'output' if name in returned else 'input'} wire {width} raw_{name}" for name, width in av.items()]
    top_ports += [f"{direction} wire {width} axi_{name}" for name, (direction, width) in axi.items()]
    lines = [f"module {TOP}(\n" + ",\n".join(top_ports) + "\n);"]
    lines += [f"wire {width} ep_{name};" for name, width in av.items() if name not in returned]
    lines += [f"wire {width} mem_{name};" for name, width in av.items()]
    for name in av:
        if name in returned:
            lines.append(f"assign raw_{name}=mem_{name};")
        else:
            lines.append(f"assign mem_{name}=raw_mode?raw_{name}:ep_{name};")
    ep_connections = []
    for _, _, name in ep:
        value = name
        if name == "reset_n":
            value = "reset_n && !raw_mode"
        elif name.startswith("avm_"):
            role = name.removeprefix("avm_")
            value = ("mem_" if role in returned else "ep_") + role
        ep_connections.append(f".{name}({value})")
    lines += [ENDPOINT + " endpoint(\n" + ",\n".join(ep_connections) + "\n);"]
    connections = []
    for _, _, name in memory_ports:
        if name in (MASTER + "translator_reset_reset_bridge_in_reset_reset", SLAVE + "translator_clk_reset_reset_bridge_in_reset_reset"):
            value = "~reset_n"
        elif name == "clk_100_out_clk_clk":
            value = "clk"
        elif name.startswith(MASTER):
            value = "mem_" + name.removeprefix(MASTER)
        else:
            value = "axi_" + name.removeprefix(SLAVE)
        connections.append(f".{name}({value})")
    lines += [memory_module + " transport(\n" + ",\n".join(connections) + "\n);", "endmodule\n"]
    return "\n".join(lines)


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def disable_core_dump():
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args(argv)
    project, output = args.project.resolve(strict=True), args.output.resolve()
    if output == project or project in output.parents:
        parser.error("simulation output must be outside the producer project")
    output.mkdir(parents=True, exist_ok=False)
    receipt = {"format": 1, "classification": "generated-ramtest-fpga-b-transport-simulation",
               "status": "failed", "hardware_access": "not-performed", "project": str(project),
               "coverage": "generated FPGA-B endpoint and Avalon128-to-AXI256 memory interconnect",
               "not_modeled": ["physical memory", "EMIF controller/calibration", "PHY", "physical CDC/timing",
                               "full peripheral CSR routing", "HPS memory ownership/admission"]}
    try:
        verifier_sources = {p: p.read_bytes() for p in (Path(__file__), HERE / "build.py", HERE.parent / "hps/build.py", HERE.parent / "build.py")}
        inputs_path, generation_path = project.parent / "inputs.json", project.parent / "generation.json"
        inputs_data, generation_data = inputs_path.read_bytes(), generation_path.read_bytes()
        inputs, generation = json.loads(inputs_data), json.loads(generation_data)
        if (inputs.get("ramtest", {}).get("hps_destructive_enabled") is not False or "ramtest" not in generation
                or inputs.get("source_classification") != "committed"
                or not re.fullmatch(r"[0-9a-f]{40}", inputs.get("fes_revision", ""))
                or not re.fullmatch(r"[0-9a-f]{32}", inputs["ramtest"].get("build_id", ""))):
            raise ValueError("completed RAM generation receipt with HPS writes disabled is required")
        version_data = (project.parent / "quartus-version.txt").read_bytes()
        if "Version 26.3.0 Build 114" not in version_data.decode() or "SC Pro Edition" not in version_data.decode():
            raise ValueError("expected original Quartus 26.3 Build114 generation identity")
        build = load(HERE / "build.py", "ram_transport_build")
        hps = load(HERE.parent / "hps/build.py", "ram_transport_hps")
        verified = build.verify_generated(project, hps)
        if verified != generation["ramtest"]:
            raise ValueError("current generated route differs from generation receipt")
        qips, memory, module, endpoint, sources = source_closure(project)
        header = project / "fes_mailbox/generated/fes_de25_ramtest.hpp"
        sources[header] = contained_file(project, header)
        receipt.update({"inputs_sha256": digest(inputs_data), "generation_sha256": digest(generation_data),
                        "source_revision": inputs.get("fes_revision"), "source_sha256": {str(p.relative_to(project)): digest(d) for p, d in sorted(sources.items())},
                        "qip_sha256": {str(p.relative_to(project)): digest(d) for p, d in sorted(qips.items())},
                        "verified_route": verified, "build_id": inputs["ramtest"]["build_id"]})
        receipt["verifier_source_sha256"] = {str(p.relative_to(HERE.parents[4])): digest(data) for p, data in verifier_sources.items()}
        receipt["generation_tool_version"] = version_data.decode().strip()
        for path, data in sources.items():
            target = output / "sources" / path.relative_to(project)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        wrapper_path = output / (TOP + ".v")
        wrapper_path.write_text(wrapper(sources[memory].decode(), module, sources[endpoint].decode()))
        bench = HERE / "interconnect_tb.cpp"
        bench_data = bench.read_bytes()
        bench_copy = output / bench.name
        bench_copy.write_bytes(bench_data)
        receipt.update({"helper_sha256": digest(Path(__file__).read_bytes()), "bench_sha256": digest(bench_data),
                        "wrapper_sha256": digest(wrapper_path.read_bytes())})
        revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=HERE, text=True).strip()
        checked_sources = {**verifier_sources, bench: bench_data}
        committed = True
        for path, data in checked_sources.items():
            selected = subprocess.run(["git", "show", revision + ":" + path.relative_to(HERE.parents[4]).as_posix()],
                                      cwd=HERE, capture_output=True)
            committed &= selected.returncode == 0 and selected.stdout == data
        receipt["verifier_git_revision"] = revision
        receipt["verifier_sources_committed"] = bool(committed)
        verilator = shutil.which("verilator")
        if not verilator:
            raise ValueError("Verilator is required")
        receipt["verilator"] = subprocess.check_output([verilator, "--version"], text=True).strip()
        includes = sorted({str((output / "sources" / p.relative_to(project)).parent) for p in sources})
        rtl = sorted(str(output / "sources" / p.relative_to(project)) for p in sources if p.suffix in (".v", ".sv"))
        options = ["--top-module", TOP, "-Wno-fatal", "--assert", "-DFES_DE25_RAMTEST_BUILD_ID=128'h" + inputs["ramtest"]["build_id"],
                   *("-I" + p for p in includes), str(wrapper_path), *rtl]
        xml_path = output / "elaboration.xml"
        with (output / "elaboration.log").open("w") as log:
            subprocess.run([verilator, "--xml-only", "--xml-output", str(xml_path), *options], check=True, stdout=log, stderr=subprocess.STDOUT)
        check_elaboration(xml_path)
        receipt["elaboration_sha256"] = digest(xml_path.read_bytes())
        build_id = inputs["ramtest"]["build_id"]
        expected_id = " ".join(f"-DBUILD_ID_{i}=0x{build_id[24-i*8:32-i*8]}u" for i in range(4))
        command = [verilator, "--cc", "--exe", "--build", "-j", "2", "--Mdir", str(output / "obj"),
                   "-CFLAGS", "-O2 " + expected_id + " -I" + str(output / "sources/fes_mailbox"), *options, str(bench_copy)]
        receipt["build_command"] = command
        with (output / "build.log").open("w") as log:
            subprocess.run(command, check=True, stdout=log, stderr=subprocess.STDOUT)
        executable = output / "obj" / ("V" + TOP)
        receipt["executable_sha256"] = digest(executable.read_bytes())
        with (output / "simulation.log").open("w") as log:
            subprocess.run([str(executable)], check=True, stdout=log, stderr=subprocess.STDOUT, timeout=180)
        cases = [json.loads(line) for line in (output / "simulation.log").read_text().splitlines() if line.startswith("{")]
        receipt["cases"] = cases
        with (output / "unsolicited-response.log").open("w") as log:
            negative = subprocess.run([str(executable), "--unsolicited"], stdout=log, stderr=subprocess.STDOUT,
                                      timeout=30, preexec_fn=disable_core_dump)
        expected_assertion = "ERROR_readdata_receive_but_read_ID_fifo_is_empty"
        if negative.returncode >= 0 or expected_assertion not in (output / "unsolicited-response.log").read_text():
            raise ValueError("generated transport did not assert on the unsolicited AXI response")
        cases.append({"case": "unsolicited_axi_response_asserts", "status": "passed",
                      "returncode": negative.returncode, "assertion": expected_assertion,
                      "scope": "generated simulation assertion; not a synthesized hardware fault flag"})
        if [c.get("case") for c in cases] != CASES or not all(c.get("status") == "passed" for c in cases):
            raise ValueError("not every required case passed exactly once in order")
        if any(p.read_bytes() != d for p, d in {**sources, **qips}.items()) or inputs_path.read_bytes() != inputs_data or generation_path.read_bytes() != generation_data:
            raise ValueError("producer files changed during simulation")
        if bench.read_bytes() != bench_data:
            raise ValueError("bench source changed during simulation")
        if any(p.read_bytes() != data for p, data in verifier_sources.items()) or (project.parent / "quartus-version.txt").read_bytes() != version_data:
            raise ValueError("verifier or tool identity changed during simulation")
        receipt.update({"status": "passed", "cases": cases})
    finally:
        (output / "simulation.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    print(output / "simulation.json")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
