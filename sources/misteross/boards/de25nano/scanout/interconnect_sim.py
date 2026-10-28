#!/usr/bin/env python3
"""Simulate generated scanout CDC/AXI transport WITH MODELED M20K STORAGE."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import xml.etree.ElementTree as ET

TOP = "fes_de25_scanout_interconnect_test"
MASTER = "subsys_periph_fes_scanout_mem_"
SLAVE = "subsys_hps_f2sdram_adapter_axi4_sub_"
CASES = ["reset_disabled", "atomic_snapshot", "complete_frames_and_slot_reuse",
         "stop_drains_accepted_reads", "pixel_clock_stall_preserves_responses",
         "axi_error_crosses_cdc", "clock_fault_is_sticky"]

# This deliberately models only the hard RAM's reviewed noncollision storage
# contract. All generated FIFO pointers, synchronizers, credits and routing stay
# unchanged. It is not a vendor primitive model or physical RAM qualification.
RAM_MODEL = r'''
module altera_syncram #(
    parameter address_aclr_b="NONE", address_reg_b="CLOCK1",
    parameter clock_enable_input_a="BYPASS", clock_enable_input_b="BYPASS",
    parameter clock_enable_output_b="BYPASS", enable_ecc="FALSE",
    parameter lpm_type="altera_syncram", numwords_a=4, numwords_b=4,
    parameter operation_mode="DUAL_PORT", outdata_aclr_b="NONE",
    parameter outdata_sclr_b="NONE", outdata_reg_b="UNREGISTERED",
    parameter power_up_uninitialized="TRUE", ram_block_type="M20K",
    parameter read_during_write_mode_mixed_ports="DONT_CARE",
    parameter widthad_a=2, widthad_b=2, width_a=8, width_b=8, width_byteena_a=1
) (
    input wire [widthad_a-1:0] address_a,
    input wire [widthad_b-1:0] address_b,
    input wire clock0, clock1, wren_a,
    input wire [width_a-1:0] data_a,
    output wire [width_b-1:0] q_b,
    input wire aclr0, aclr1, address2_a, address2_b, addressstall_a, addressstall_b,
    input wire byteena_a, byteena_b, clocken0, clocken1, clocken2, clocken3,
    input wire [width_b-1:0] data_b,
    output wire [width_a-1:0] q_a,
    output wire [2:0] eccstatus,
    input wire eccencbypass,
    input wire [7:0] eccencparity,
    input wire rden_a, rden_b, sclr, wren_b
);
    reg [width_a-1:0] memory [0:numwords_a-1];
    reg [widthad_b-1:0] model_read_address;
    reg [numwords_a-1:0] model_written = 0;
    wire model_read_initialized = model_written[model_read_address];
    initial begin
        if (address_aclr_b!="NONE" || address_reg_b!="CLOCK1" ||
            clock_enable_input_a!="BYPASS" || clock_enable_input_b!="BYPASS" ||
            clock_enable_output_b!="BYPASS" || enable_ecc!="FALSE" ||
            lpm_type!="altera_syncram" || operation_mode!="DUAL_PORT" ||
            outdata_aclr_b!="NONE" || outdata_sclr_b!="NONE" ||
            outdata_reg_b!="UNREGISTERED" || power_up_uninitialized!="TRUE" ||
            ram_block_type!="M20K" || width_a!=width_b || numwords_a!=numwords_b ||
            read_during_write_mode_mixed_ports!="DONT_CARE" ||
            widthad_a!=widthad_b || (1<<widthad_a)!=numwords_a || width_byteena_a!=1)
            $fatal(1,"unsupported modeled RAM parameter contract");
        if (!((numwords_a==4 && (width_a==47 || width_a==35 || width_a==184 || width_a==8)) ||
              (numwords_a==32 && width_a==131)))
            $fatal(1,"unsupported modeled RAM shape");
    end
    // BYPASS means the clock-enable pins do not gate either used clock.
    always @(posedge clock0) begin
        if (aclr0 || aclr1 || sclr || addressstall_a || addressstall_b ||
            wren_b || !byteena_a || !byteena_b || !rden_a || !rden_b ||
            !address2_a || !address2_b || eccencbypass || eccencparity!=0)
            $fatal(1,"unsupported modeled RAM control inputs");
        if (wren_a) begin
            memory[address_a] <= data_a;
            model_written[address_a] <= 1'b1;
        end
    end
    always @(posedge clock1) model_read_address <= address_b;
    // Uninitialized contents are explicitly undefined. The private wrapper
    // asserts that real FIFO consumers never expose an unwritten RAM word.
    assign q_b = model_read_initialized ? memory[model_read_address] : {width_b{1'bx}};
    // These outputs are unconnected in every guarded generated instance.
    assign q_a = {width_a{1'bx}};
    assign eccstatus = 3'b000;
endmodule
'''


def guard_ram_primitives(sources):
    parameters = {
        "address_aclr_b": '"NONE"', "address_reg_b": '"CLOCK1"',
        "clock_enable_input_a": '"BYPASS"', "clock_enable_input_b": '"BYPASS"',
        "clock_enable_output_b": '"BYPASS"', "enable_ecc": '"FALSE"',
        "lpm_type": '"altera_syncram"', "numwords_a": "FIFO_DEPTH", "numwords_b": "FIFO_DEPTH",
        "operation_mode": '"DUAL_PORT"', "outdata_aclr_b": '"NONE"', "outdata_sclr_b": '"NONE"',
        "outdata_reg_b": '"UNREGISTERED"', "power_up_uninitialized": '"TRUE"', "ram_block_type": '"M20K"',
        "widthad_a": "ADDR_WIDTH", "widthad_b": "ADDR_WIDTH", "width_a": "PAYLOAD_WIDTH",
        "width_b": "PAYLOAD_WIDTH", "width_byteena_a": "1"}
    ties = {"address_a": "mem_wr_ptr", "address_b": "mem_rd_ptr", "clock0": "in_clk", "clock1": "out_clk",
            "data_a": "in_payload", "wren_a": "in_valid&&in_ready_copyA", "q_b": "fifo_out_payload",
            "aclr0": "1'b0", "aclr1": "1'b0", "address2_a": "1'b1", "address2_b": "1'b1",
            "addressstall_a": "1'b0", "addressstall_b": "1'b0", "byteena_a": "1'b1", "byteena_b": "1'b1",
            "clocken0": "1'b1", "clocken1": "1'b1", "clocken2": "1'b1", "clocken3": "1'b1",
            "data_b": "{PAYLOAD_WIDTH{1'b1}}", "q_a": "", "eccstatus": "", "eccencbypass": "1'b0",
            "eccencparity": "8'b0", "rden_a": "1'b1", "rden_b": "1'b1", "sclr": "1'b0", "wren_b": "1'b0"}
    checked, conditional = [], []
    for path, data in sources.items():
        source = uncomment(data.decode())
        if not re.search(r"\baltera_syncram\s*#", source):
            continue
        if "/st_dc_fifo_1953/synth/" not in path.as_posix():
            # Generic SC-FIFO sources also declare primitive branches. They
            # are not modeled: the elaborated-cell inventory below must prove
            # that none of those branches actually instantiates a RAM cell.
            conditional.append(path)
            continue
        matches = re.findall(r"\baltera_syncram\s*#\s*\((.*?)\)\s+altera_syncram_component\s*\((.*?)\);", source, re.S)
        if len(matches) != 1:
            raise ValueError(f"unexpected primitive instance count: {path}")
        def named(section):
            fields = re.findall(r"\.(\w+)\s*\(([^()]*)\)", section)
            if re.sub(r"\.(\w+)\s*\(([^()]*)\)", "", section).strip(" \t\r\n,"):
                raise ValueError("unsupported RAM named-argument syntax")
            if len(fields) != len(dict(fields)):
                raise ValueError("duplicate RAM named argument")
            return {k: re.sub(r"\s+", "", v) for k, v in fields}
        if named(matches[0][0]) != parameters or named(matches[0][1]) != ties:
            raise ValueError(f"generated primitive parameter/tie contract differs: {path}")
        checked.append(path)
    if len(checked) != 2:
        raise ValueError(f"expected two generated FIFO storage module definitions, found {len(checked)}")
    return checked, conditional


def guard_elaborated_ram(xml_path):
    """Prove the exact five active RAM cells, not source-text branch guesses."""
    root = ET.parse(xml_path).getroot()
    modules = {m.get("name"): m for m in root.findall(".//module")}
    expected = {}
    for bridge, shapes in (("fes_ctrl_cdc", ((47, 4), (35, 4))),
                           ("fes_mem_cdc", ((184, 4), (131, 32)))):
        for role, shape in zip(("cmd", "rsp"), shapes):
            expected[f"{TOP}.periph.{bridge}.{bridge}.{role}_fifo.my_altera_avalon_dc_fifo_{role}_fifo.altera_syncram_component"] = shape
    expected[TOP + ".ram_contract_test"] = (8, 4)
    fixed = {"address_aclr_b": "NONE", "address_reg_b": "CLOCK1",
             "clock_enable_input_a": "BYPASS", "clock_enable_input_b": "BYPASS",
             "clock_enable_output_b": "BYPASS", "enable_ecc": "FALSE", "lpm_type": "altera_syncram",
             "operation_mode": "DUAL_PORT", "outdata_aclr_b": "NONE", "outdata_sclr_b": "NONE",
             "outdata_reg_b": "UNREGISTERED", "power_up_uninitialized": "TRUE", "ram_block_type": "M20K",
             "read_during_write_mode_mixed_ports": "DONT_CARE", "width_byteena_a": 1}
    result = []
    for cell in root.findall(".//cell"):
        module = modules.get(cell.get("submodname"))
        if module is None or module.get("origName") != "altera_syncram":
            continue
        hierarchy = cell.get("hier")
        if hierarchy not in expected:
            raise ValueError(f"unexpected actually elaborated RAM primitive: {hierarchy}")
        values = {}
        for var in module.findall("var"):
            if var.get("param") != "true":
                continue
            constant = var.find("const")
            match = re.fullmatch(r"(\d+)'s?h([0-9a-f]+)", constant.get("name", "") if constant is not None else "")
            if not match:
                raise ValueError("unsupported elaborated RAM parameter constant")
            value = int(match[2], 16)
            name = var.get("name")
            values[name] = value.to_bytes((int(match[1]) + 7) // 8, "big").decode("ascii").lstrip("\0") if isinstance(fixed.get(name), str) else value
        width, depth = expected[hierarchy]
        wanted = fixed | {"width_a": width, "width_b": width, "widthad_a": depth.bit_length() - 1,
                          "widthad_b": depth.bit_length() - 1, "numwords_a": depth, "numwords_b": depth}
        if values != wanted:
            raise ValueError(f"actually elaborated RAM parameters differ: {hierarchy}")
        result.append({"hierarchy": hierarchy, "parameters": values})
    if len(result) != len(expected) or {c["hierarchy"] for c in result} != set(expected):
        raise ValueError("actual elaboration did not contain exactly the four reviewed CDC RAMs and self-test")
    return result


def sha(data):
    return hashlib.sha256(data).hexdigest()


def uncomment(source):
    return re.sub(r"/\*.*?\*/|//[^\n]*", " ", source, flags=re.S)


def ports(source, module):
    header = re.search(r"\bmodule\s+" + re.escape(module) + r"\s*\((.*?)\n\s*\);",
                       uncomment(source), re.S)
    if not header:
        raise ValueError(f"unsupported generated module header: {module}")
    pattern = r"\b(input|output)\s+wire\s*(\[\d+:\d+\])?\s*(\w+)\s*,?"
    result = re.findall(pattern, header[1])
    if not result or re.sub(pattern, "", header[1]).strip(" \t\r\n,"):
        raise ValueError(f"unsupported generated port declaration: {module}")
    return result


def source_closure(project):
    qip = project / "qsys_top/qsys_top.qip"
    # Pro emits independent QIPs for the subsystem and each external IP;
    # qsys_top.qip alone contains neither the peripheral top nor custom RTL.
    qips = {p: p.read_bytes() for p in [qip, *sorted((project / "peripheral_subsys").rglob("*.qip"))]}
    files, definitions = {}, {}
    for manifest, qip_data in qips.items():
        manifest.resolve(strict=True).relative_to(project)
        for rel in re.findall(r'-name (?:SYSTEMVERILOG|VERILOG)_FILE \[file join \$::quartus\(qip_path\) "([^"]+)"\]', qip_data.decode()):
            path = (manifest.parent / rel).resolve(strict=True)
            path.relative_to(project)
            if path in files:
                continue
            data = path.read_bytes()
            source = uncomment(data.decode())
            modules = re.findall(r"^[ \t]*module\s+(\w+)\b(.*?^[ \t]*endmodule\b)", source, re.M | re.S)
            files[path] = (data, source, [name for name, _ in modules])
            for name, body in modules:
                definitions.setdefault(name, []).append((path, re.sub(r"\s+", " ", body).strip()))
    def one(candidates, label):
        if len(candidates) != 1:
            raise ValueError(f"expected one generated {label}, found {len(candidates)}")
        return candidates[0]
    periph = one([p for p, (_, s, names) in files.items() if "peripheral_subsys" in names
                  and "fes_scanout_pixel_clk" in s and "fes_scanout_mem_address" in s], "scanout peripheral subsystem")
    memory = one([p for p, (_, s, _) in files.items()
                  if re.search(r"\bmodule\s+qsys_top_altera_mm_interconnect_\w+\b", s)
                  and MASTER + "address" in s and SLAVE + "araddr" in s], "scanout-to-AXI interconnect")
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
                    raise ValueError(f"ambiguous referenced generated module {name}")
                visit(bodies[0][0])
    visit(periph)
    visit(memory)
    cdc_instances = {}
    sopc = project / "qsys_top/qsys_top.sopcinfo"
    sopc_data = sopc.read_bytes()
    components = {m.get("path"): m for m in ET.fromstring(sopc_data).findall("module")}
    for instance in ("fes_ctrl_cdc", "fes_mem_cdc"):
        matches = re.findall(r"^[ \t]*(\w+)\s+" + instance + r"\s*\(", files[periph][1], re.M)
        module = one(matches, instance + " instance")
        if module != "peripheral_subsys_" + instance:
            raise ValueError(f"unsupported generated CDC module: {module}")
        metadata = components.get("subsys_periph." + instance)
        if metadata is None or metadata.get("kind") != "mm_ccb" or metadata.get("version") != "19.4.1":
            raise ValueError(f"generated instance is not the reviewed clock crossing: {instance}")
        bodies = definitions.get(module, [])
        if not bodies or len({body for _, body in bodies}) != 1 or bodies[0][0] not in selected:
            raise ValueError(f"CDC source is absent/ambiguous in selected closure: {module}")
        cdc_instances[instance] = {"kind": metadata.get("kind"), "version": metadata.get("version"),
                                   "module": module, "source": str(bodies[0][0].relative_to(project))}
    endpoint = definitions.get("peripheral_subsys_fes_scanout", [])
    if len(endpoint) != 1 or not re.search(r"\bfes_de25_scanout\s+fes_scanout\s*\(", files[endpoint[0][0]][1]):
        raise ValueError("unsupported generated endpoint wrapper")
    sources = {p: files[p][0] for p in selected}
    sources[sopc] = sopc_data
    # Generated custom components can include the board's frozen definitions.
    pending = list(sources)
    while pending:
        path = pending.pop()
        for rel in re.findall(r'`include\s+"([^"\n]+)"', uncomment(sources[path].decode())):
            include = (path.parent / rel).resolve(strict=True)
            include.relative_to(project)
            if include not in sources:
                sources[include] = include.read_bytes()
                pending.append(include)
    return qips, periph, memory, files[memory][2][0], sources, cdc_instances


def wrapper(periph_source, memory_source, memory_module):
    pp, mp = ports(periph_source, "peripheral_subsys"), ports(memory_source, memory_module)
    av = {"address": ("output", "[31:0]"), "burstcount": ("output", "[4:0]"),
          "read": ("output", ""), "write": ("output", ""),
          "writedata": ("output", "[127:0]"), "byteenable": ("output", "[15:0]"),
          "waitrequest": ("input", ""), "readdata": ("input", "[127:0]"),
          "readdatavalid": ("input", ""), "response": ("input", "[1:0]"),
          "writeresponsevalid": ("input", "[0:0]"), "debugaccess": ("output", "")}
    seen = {n: (d, w) for d, w, n in pp}
    required = {"address", "burstcount", "read", "byteenable", "waitrequest", "readdata", "readdatavalid", "response"}
    for role in required:
        if seen.get("fes_scanout_mem_" + role) != av[role]:
            raise ValueError(f"scanout memory port mismatch: {role}")
    control = {"address": ("input", "[16:0]", "address"), "read": ("input", "", "read"),
               "write": ("input", "", "write"), "writedata": ("input", "[31:0]", "writedata"),
               "byteenable": ("input", "[3:0]", "byteenable"),
               "waitrequest": ("output", "", "waitrequest"), "readdata": ("output", "[31:0]", "readdata"),
               "readdatavalid": ("output", "", "readdatavalid"), "burstcount": ("input", "[0:0]", "1'b1"),
               "debugaccess": ("input", "", "1'b0")}
    for role, (direction, width, _) in control.items():
        if seen.get("pb_cpu_0_s0_" + role) != (direction, width):
            raise ValueError(f"control port mismatch: {role}")
    axi = {"arid": ("output", "[4:0]"), "araddr": ("output", "[31:0]"),
           "arlen": ("output", "[7:0]"), "arsize": ("output", "[2:0]"),
           "arburst": ("output", "[1:0]"), "arvalid": ("output", ""), "arready": ("input", ""),
           "rid": ("input", "[4:0]"), "rdata": ("input", "[255:0]"), "rresp": ("input", "[1:0]"),
           "rlast": ("input", ""), "rvalid": ("input", ""), "rready": ("output", ""),
           "awvalid": ("output", ""), "wvalid": ("output", "")}
    mem_seen = {n: (d, w) for d, w, n in mp}
    for role, spec in axi.items():
        if mem_seen.get(SLAVE + role) != spec:
            raise ValueError(f"AXI256 port mismatch: {role}")
    external = ["input wire clk_mem, clk_pixel, reset_n, clock_fault", "input wire [16:0] address",
                "input wire read, write", "input wire [3:0] byteenable", "input wire [31:0] writedata",
                "output wire [31:0] readdata", "output wire readdatavalid, waitrequest",
                "output wire [23:0] video_rgb", "output wire video_de, video_hs, video_vs",
                "output wire endpoint_drained", "output wire [8:0] endpoint_outstanding",
                "output wire endpoint_readdatavalid", "output wire [1:0] endpoint_response",
                "output wire [31:0] endpoint_address", "output wire [4:0] endpoint_burstcount",
                "output wire endpoint_read, endpoint_waitrequest", "output wire [127:0] endpoint_readdata",
                "output wire [31:0] endpoint_accepted_sequence, endpoint_displayed_sequence",
                "output wire endpoint_pending_slot, endpoint_displayed_slot",
                "input wire [1:0] ram_test_write_address, ram_test_read_address",
                "input wire [7:0] ram_test_data", "input wire ram_test_write, ram_test_clock_enable",
                "output wire [7:0] ram_test_q"]
    external += [f"{d} wire {w} {role}" for role, (d, w) in axi.items()]
    external += [f"output wire {av[r][1]} mem_{r}" for r in required]
    lines = [f"module {TOP} (\n    " + ",\n    ".join(external) + "\n);"]
    for role, (_, width) in av.items():
        if role not in required:
            lines.append(f"wire {width} mem_{role};")
    pconnections, handled = [], set()
    for direction, width, name in pp:
        if name.startswith("fes_scanout_mem_"):
            role = name.removeprefix("fes_scanout_mem_")
            if role not in av or (direction, width) != av[role]:
                raise ValueError(f"unsupported generated memory role: {name}")
            value = "mem_" + role
        elif name.startswith("pb_cpu_0_s0_"):
            role = name.removeprefix("pb_cpu_0_s0_")
            if role not in control:
                raise ValueError(f"unsupported control role: {role}")
            value = control[role][2]
        elif name in ("clk_clk", "reset_reset_n"):
            if (direction, width) != ("input", ""):
                raise ValueError(f"system clock/reset port changed: {name}")
            value = "clk_mem" if name == "clk_clk" else "reset_n"
        elif name == "fes_scanout_pixel_clk_clk":
            if (direction, width) != ("input", ""):
                raise ValueError("pixel clock port changed")
            value = "clk_pixel"
            handled.add("pixel")
        elif name == "fes_scanout_reset_reset_n":
            if (direction, width) != ("input", ""):
                raise ValueError("common reset port changed")
            value = "reset_n"
            handled.add("reset")
        elif name == "fes_scanout_clock_fault_export":
            if (direction, width) != ("input", ""):
                raise ValueError("clock fault port changed")
            value = "clock_fault"
            handled.add("fault")
        elif name.startswith("fes_scanout_video_"):
            role = name.removeprefix("fes_scanout_video_")
            if role not in ("rgb", "de", "hs", "vs") or direction != "output" or width != ("[23:0]" if role == "rgb" else ""):
                raise ValueError(f"unsupported video role: {name}")
            value = "video_" + role
            handled.add(role)
        elif name.startswith("fes_scanout_"):
            raise ValueError(f"unrecognized scanout port: {name}")
        elif direction == "input":
            value = "'1" if name.endswith("_waitrequest") else "'0"
        else:
            value = ""
        pconnections.append(f" .{name}({value})")
    if handled != {"pixel", "reset", "fault", "rgb", "de", "hs", "vs"}:
        raise ValueError(f"missing scanout conduits: {handled}")
    if not re.search(r"\bperipheral_subsys_fes_scanout\s+fes_scanout\s*\(", uncomment(periph_source)):
        raise ValueError("unsupported generated endpoint instance identity")
    lines += ["peripheral_subsys periph (", ",\n".join(pconnections), ");"]
    monitors = {"drained": "drained", "outstanding": "outstanding_beats", "readdatavalid": "avm_readdatavalid",
                "response": "avm_response", "address": "avm_address", "burstcount": "avm_burstcount",
                "read": "avm_read", "waitrequest": "avm_waitrequest", "readdata": "avm_readdata",
                "accepted_sequence": "accepted_sequence", "displayed_sequence": "displayed_sequence",
                "pending_slot": "pending_slot", "displayed_slot": "displayed_slot"}
    lines += [f"assign endpoint_{role} = periph.fes_scanout.fes_scanout.{signal};" for role, signal in monitors.items()]
    for bridge in ("fes_ctrl_cdc", "fes_mem_cdc"):
        for role in ("cmd", "rsp"):
            fifo = f"periph.{bridge}.{bridge}.{role}_fifo.my_altera_avalon_dc_fifo_{role}_fifo"
            ram = fifo + ".altera_syncram_component"
            lines += [f"always @(posedge {fifo}.out_clk) if (reset_n && {fifo}.internal_out_ready && {fifo}.internal_out_valid)",
                      f" if (!{ram}.model_read_initialized) $fatal(1,\"FIFO exposed uninitialized modeled storage\");",
                      f"always @(posedge {fifo}.in_clk) if (reset_n && {fifo}.in_valid && {fifo}.in_ready_copyA &&",
                      f" {fifo}.internal_out_valid && {fifo}.mem_wr_ptr == {ram}.model_read_address)",
                      ' $fatal(1,"FIFO write collided with its live unread modeled head");']
    lines += ["altera_syncram ram_contract_test (",
              " .address_a(ram_test_write_address), .address_b(ram_test_read_address),",
              " .clock0(clk_mem), .clock1(clk_pixel), .data_a(ram_test_data), .wren_a(ram_test_write),",
              " .q_b(ram_test_q), .aclr0(1'b0), .aclr1(1'b0), .address2_a(1'b1), .address2_b(1'b1),",
              " .addressstall_a(1'b0), .addressstall_b(1'b0), .byteena_a(1'b1), .byteena_b(1'b1),",
              " .clocken0(ram_test_clock_enable), .clocken1(ram_test_clock_enable),",
              " .clocken2(ram_test_clock_enable), .clocken3(ram_test_clock_enable), .data_b(8'hff),",
              " .q_a(), .eccstatus(), .eccencbypass(1'b0), .eccencparity(8'b0),",
              " .rden_a(1'b1), .rden_b(1'b1), .sclr(1'b0), .wren_b(1'b0));"]
    connections = []
    for direction, width, name in mp:
        if name == MASTER + "translator_reset_reset_bridge_in_reset_reset":
            if (direction, width) != ("input", ""):
                raise ValueError("generated scanout translator reset port changed")
            value = "~reset_n"
        elif name.startswith(MASTER):
            role = name.removeprefix(MASTER)
            # The CCB exports a [0:0] writeresponsevalid vector; Pro's
            # top-level interconnect flattens that one-bit response to scalar.
            expected_width = "" if role == "writeresponsevalid" else av.get(role, (None, None))[1]
            if role not in av or (direction, width) != ("input" if av[role][0] == "output" else "output", expected_width):
                raise ValueError(f"generated memory master mismatch: {name}")
            value = "mem_" + role
        elif name.startswith(SLAVE) and name.removeprefix(SLAVE) in axi:
            value = name.removeprefix(SLAVE)
        elif direction == "input" and name.endswith("_clk"):
            value = "clk_mem"
        elif direction == "input" and "reset" in name:
            if name.endswith("reset_n"):
                raise ValueError("unexpected active-low generated interconnect reset")
            value = "~reset_n"
        elif direction == "input":
            value = "'0"
        else:
            value = ""
        connections.append(f" .{name}({value})")
    lines += [f"{memory_module} transport (", ",\n".join(connections), ");", "endmodule", ""]
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True, help="new private output; never the producer project")
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args(argv)
    project = args.project.resolve(strict=True)
    output = args.output.resolve()
    if output == project or project in output.parents:
        parser.error("simulation output must be outside the producer project")
    output.mkdir(parents=True, exist_ok=False)
    receipt = {"format": 1, "classification": "generated-scanout-cdc-transport-simulation-with-modeled-m20k-storage", "status": "failed",
               "hardware_access": "not-performed", "admission": "not-qualified", "project": str(project),
               "coverage": "actual generated peripheral control/memory CDC logic, endpoint/video/reader and Avalon128-to-AXI256 path WITH MODELED M20K STORAGE",
               "clock_model": {"system_hz": 100000000, "pixel_hz": 74250000,
                               "half_period_units": {"system": 297, "pixel": 400},
                               "pixel_initial_phase_units": 17, "physical_timing_proof": False},
               "not_modeled": ["HPS hard block", "MPFE/TBU", "SMMU translation", "firewall", "DDR controller",
                               "physical memory", "physical clock monitor", "reset/boot allocation epoch"]}
    try:
        generation_path = project.parent / "generation.json"
        generation_data = generation_path.read_bytes()
        generation = json.loads(generation_data)
        if generation.get("scanout", {}).get("ddr_scanout") is not True:
            raise ValueError("completed verified scanout IP-generation receipt is required")
        receipt["generation_sha256"] = sha(generation_data)
        receipt["generation_scanout"] = generation["scanout"]
        qips, periph, memory, module, sources, cdc_instances = source_closure(project)
        ram_sources, conditional_ram_sources = guard_ram_primitives(sources)
        ram_path = output / "modeled_m20k_storage.v"
        ram_path.write_text(RAM_MODEL)
        receipt["ram_storage_model"] = {"classification": "explicit noncollision storage model; not a vendor primitive model",
            "sha256": sha(ram_path.read_bytes()), "guarded_generated_sources": [str(p.relative_to(project)) for p in ram_sources],
            "conditional_primitive_sources_proven_unelaborated": [str(p.relative_to(project)) for p in conditional_ram_sources],
            "actual_shapes": [{"width": 47, "depth": 4}, {"width": 35, "depth": 4},
                              {"width": 184, "depth": 4}, {"width": 131, "depth": 32}],
            "self_test": "reset_disabled case checks independent clocks, address latency, unregistered output, write enable and BYPASS clock enables",
            "runtime_guards": ["FIFO RAM-to-output-stage valid transfer never consumes an unwritten RAM word",
                               "FIFO never overwrites its live unread RAM head; buffered output is separate"],
            "unproven": ["mixed-port same-address collision behavior (vendor default DONT_CARE)",
                         "undefined power-up data", "vendor primitive equivalence", "physical M20K behavior", "CDC metastability"]}
        header = project / "fes_mailbox/generated/fes_de25_scanout.h"
        sources[header] = header.read_bytes()
        receipt["source_sha256"] = {str(p.relative_to(project)): sha(b) for p, b in sorted(sources.items())}
        receipt["qip_sha256"] = {str(p.relative_to(project)): sha(data) for p, data in sorted(qips.items())}
        receipt["selected_modules"] = {"peripheral": str(periph.relative_to(project)), "memory": str(memory.relative_to(project)), "memory_module": module}
        receipt["cdc_instances"] = cdc_instances
        inputs = project.parent / "inputs.json"
        inputs_data = inputs.read_bytes()
        receipt["build_inputs_sha256"] = sha(inputs_data)
        receipt["build_inputs"] = json.loads(inputs_data)
        for path, data in sources.items():
            copy = output / "sources" / path.relative_to(project)
            copy.parent.mkdir(parents=True, exist_ok=True)
            copy.write_bytes(data)
        wrapper_path = output / (TOP + ".v")
        wrapper_path.write_text(wrapper(sources[periph].decode(), sources[memory].decode(), module))
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
        rtl_args = [*("-I" + p for p in includes), str(wrapper_path), str(ram_path), *rtl]
        xml_path = output / "elaboration.xml"
        elaborate = [verilator, "--xml-only", "--xml-output", str(xml_path), "-Wno-fatal", "--assert", "--top-module", TOP, *rtl_args]
        receipt["elaboration_command"] = elaborate
        with (output / "elaboration.log").open("w") as log:
            subprocess.run(elaborate, check=True, stdout=log, stderr=subprocess.STDOUT)
        receipt["ram_storage_model"]["actual_elaborated_cells"] = guard_elaborated_ram(xml_path)
        receipt["elaboration_sha256"] = sha(xml_path.read_bytes())
        command = [verilator, "--cc", "--exe", "--build", "-j", "2", "-O2", "-Wno-fatal", "--assert",
                   "--top-module", TOP, "--Mdir", str(output / "obj"),
                   "-CFLAGS", "-I" + str(output / "sources/fes_mailbox"),
                   *rtl_args, str(bench_copy)]
        receipt["build_command"] = command
        with (output / "build.log").open("w") as log:
            subprocess.run(command, check=True, stdout=log, stderr=subprocess.STDOUT)
        executable = output / "obj" / ("V" + TOP)
        receipt["executable_sha256"] = sha(executable.read_bytes())
        with (output / "simulation.log").open("w") as log:
            subprocess.run([str(executable)], check=True, stdout=log, stderr=subprocess.STDOUT)
        receipt["cases"] = [json.loads(line) for line in (output / "simulation.log").read_text().splitlines() if line.startswith("{")]
        if [c.get("case") for c in receipt["cases"]] != CASES or not all(c.get("status") == "pass" for c in receipt["cases"]):
            raise ValueError("simulation did not report every required case exactly once in order")
        if receipt["cases"][0].get("ram_storage_self_test_passed") is not True:
            raise ValueError("explicit RAM storage model contract self-test did not pass")
        if any(p.read_bytes() != data for p, data in qips.items()) or any(p.read_bytes() != data for p, data in sources.items()):
            raise ValueError("producer source changed during simulation")
        if sha(inputs.read_bytes()) != receipt["build_inputs_sha256"]:
            raise ValueError("producer inputs changed during simulation")
        if generation_path.read_bytes() != generation_data:
            raise ValueError("producer generation receipt changed during simulation")
        if bench.read_bytes() != bench_data or sha(Path(__file__).read_bytes()) != receipt["helper_sha256"]:
            raise ValueError("simulation helper/bench source changed during simulation")
        receipt["status"] = "pass"
    except (OSError, ValueError, ET.ParseError, subprocess.CalledProcessError) as error:
        receipt["error"] = str(error)
    finally:
        for name in ("elaboration.log", "build.log", "simulation.log"):
            path = output / name
            if path.is_file():
                receipt.setdefault("log_sha256", {})[name] = sha(path.read_bytes())
        (output / "receipt.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"status": receipt["status"], "receipt": str(output / "receipt.json")}, sort_keys=True))
    return 0 if receipt["status"] == "pass" else 1


if __name__ == "__main__":
    sys.exit(main())
