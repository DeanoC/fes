#!/usr/bin/env python3
"""Compose the preserved DE25 GHRD with HPS-controlled ZX81; never program it."""
from __future__ import annotations
import argparse
import ast
from decimal import Decimal
from fractions import Fraction
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import xml.etree.ElementTree as ET
import zipfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
BOARD = "sources/misteross/boards/de25nano/"
JTAG_CONTROL_BRIDGE_BASE = 0x00100000
LOCAL = ("build.py", "board.v", "clocks.sdc", "integrate.tcl",
         "fes_de25_computer.v", "fes_de25_computer_hw.tcl", "tape_store.v", "video.v",
         "generated/fes_de25_computer.vh", "generated/fes_de25_computer.hpp",
         "generated/fes_computer.vh")
CORE_FILES = ("zx81_machine.sv", "zx81_machine_clock.v", "zx81_dpram.v",
              "zx81_model.vh", "zx81_bus_pack.vh", "zx81_keyboard.v",
              "zx81_tape_store.v", "zx8x.mif", "zx8x.hex")


def sha(data): return hashlib.sha256(data).hexdigest()


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


ZX81 = load(HERE.parent / "zx81/build.py", "de25_computer_zx81_helpers")
retarget_ram = ZX81.retarget_ram


def retarget_tape(source, replacement=None):
    if replacement is None:
        replacement = (HERE / "tape_store.v").read_bytes()
    marker = b"\n`else\n"
    if replacement.count(marker) != 1 or replacement.partition(marker)[2] != source + b"`endif\n":
        raise ValueError("ZX81 tape RAM adaptation contract changed")
    return replacement, {"source_sha256": sha(source), "derived_sha256": sha(replacement),
                         "adapter": "explicit even/odd M20K banks; unchanged native branch"}


def identity():
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=ROOT, text=True)
    return {"base_revision" if dirty else "fes_revision": revision,
            "source_classification": "working-copy-diagnostic" if dirty else "committed"}


def capture(revision=None):
    def read(path):
        return (ROOT / path).read_bytes() if revision is None else subprocess.check_output(
            ["git", "show", revision + ":" + path], cwd=ROOT)
    hps_path = BOARD + "hps/build.py"
    hps_source = read(hps_path)
    declarations = [n.value for n in ast.parse(hps_source).body if isinstance(n, ast.Assign)
                    and any(isinstance(t, ast.Name) and t.id == "SOURCES" for t in n.targets)]
    if len(declarations) != 1: raise ValueError("HPS source closure declaration changed")
    names = ast.literal_eval(declarations[0])
    if not isinstance(names, tuple) or any(not isinstance(n, str) or Path(n).is_absolute()
            or ".." in Path(n).parts for n in names): raise ValueError("unsafe HPS source closure")
    paths = {BOARD + "hps/" + n for n in names} | {BOARD + "build.py", BOARD + "zx81/build.py",
        BOARD + "display/build.py", BOARD + "display/vendor.lock.toml"}
    paths |= {BOARD + "zx81-hps/" + n for n in LOCAL}
    paths |= {"sources/misteross/cores/fes-zx81/rtl/" + n for n in CORE_FILES}
    paths |= {"sources/misteross/cores/fes-common/rtl/z80/" + n + ".sv" for n in
              ("fes_z80_alu", "fes_z80_engine", "fes_z80_bus", "fes_z80_nmos")}
    paths.add("sources/misteross/cores/fes-common/rtl/fes_computer_mailbox.v")
    return {p: hps_source if p == hps_path else read(p) for p in sorted(paths)}


def replace_once(source, before, after):
    if source.count(before) != 1: raise ValueError("ZX81 GHRD patch target changed: " + before[:100])
    return source.replace(before, after)


def transport_constant(source, name):
    values = re.findall(r"^`define FES_DE25_COMPUTER_" + name + r" 32'h([0-9a-f]{8})$", source, re.M)
    if len(values) != 1: raise ValueError("missing/duplicate transport constant: " + name)
    return int(values[0], 16)


def prepare(archive, output=None, revision=None):
    sources = capture(revision)
    selected = {"fes_revision": revision, "source_classification": "committed"} if revision else identity()
    if revision is None:
        selected = {"base_revision": selected.get("fes_revision", selected.get("base_revision")),
                    "source_classification": "working-copy-diagnostic"}
    if output is None:
        base = ROOT / "sources/misteross/build/diagnostic/de25nano-zx81-hps"
        base.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix="run-", dir=base))
    else:
        output = Path(output).resolve(); output.mkdir(parents=True, exist_ok=False)
    return prepare_captured(archive, output, sources, selected)


def prepare_captured(archive, output, sources, selected):
    """Prepare a caller-owned private output from explicitly captured bytes."""
    snapshot = output / "functional-source"
    for name, data in sources.items():
        p = snapshot / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_bytes(data)
    hps = load(snapshot / BOARD / "hps/build.py", "computer_retained_hps")
    display = load(snapshot / BOARD / "display/build.py", "computer_retained_display")
    helpers = load(snapshot / BOARD / "zx81/build.py", "computer_retained_zx81")
    hps.source_identity = lambda: selected.copy()
    # Freeze the exact archive buffer used by all extractions.
    archive = Path(archive)
    if not archive.is_file() or archive.is_symlink(): raise ValueError("vendor ZIP must be a regular file")
    archive_bytes = archive.read_bytes()
    lock = helpers.tomllib.loads(sources[BOARD + "display/vendor.lock.toml"].decode())
    if sha(archive_bytes) != lock["archive"]["sha256"]: raise ValueError("vendor ZIP differs from display lock")
    retained = output / "vendor.zip"; retained.write_bytes(archive_bytes)
    directory = hps.prepare_project(retained, output / "hps")
    project = directory / "project"
    with zipfile.ZipFile(io.BytesIO(archive_bytes)) as z:
        prefix = lock["archive"]["prefix"]
        names = ("golden_top.qsf", "sys_pll.ip", "HDMI_I2C/I2C_HDMI_Config.v", "HDMI_I2C/I2C_Controller.v")
        if any(z.namelist().count(prefix + n) != 1 for n in names): raise ValueError("missing/duplicate display source")
        vendor = {n: z.read(prefix + n) for n in names}
    if display.pin_assignments((project / "golden_top.qsf").read_text()) != display.pin_assignments(vendor["golden_top.qsf"].decode()):
        raise ValueError("GHRD and HDMI pin assignments differ")
    config = vendor["HDMI_I2C/I2C_HDMI_Config.v"].decode()
    for pattern in (r"parameter\s+CLK_Freq\s*=\s*50000000\s*;", r"parameter\s+I2C_Freq\s*=\s*20000\s*;",
                    r"if\(\s*mI2C_CLK_DIV\s*<\s*\(CLK_Freq/I2C_Freq\)\s*\)",
                    r"mI2C_CLK_DIV\s*<=\s*0\s*;\s*mI2C_CTRL_CLK\s*<=\s*~mI2C_CTRL_CLK"):
        if not re.search(pattern, config): raise ValueError("vendor I2C divider contract changed")
    pll, pll_change = display.retarget_pll(vendor["sys_pll.ip"])
    local = project / "fes_mailbox"
    files = {n: sources[BOARD + "zx81-hps/" + n] for n in LOCAL if n not in ("build.py", "integrate.tcl")}
    files["computer-build.py"] = sources[BOARD + "zx81-hps/build.py"]
    for name in CORE_FILES:
        files[name] = sources["sources/misteross/cores/fes-zx81/rtl/" + name]
    for name in ("fes_z80_alu", "fes_z80_engine", "fes_z80_bus", "fes_z80_nmos"):
        files[name + ".sv"] = sources["sources/misteross/cores/fes-common/rtl/z80/" + name + ".sv"]
    files["fes_computer_mailbox.v"] = sources["sources/misteross/cores/fes-common/rtl/fes_computer_mailbox.v"]
    repairs = {}
    for name, transform in (("zx81_dpram", helpers.retarget_ram),
                            ("zx81_tape_store", lambda source: retarget_tape(source, files["tape_store.v"]))):
        files[name + "_de25.v"], repairs[name] = transform(files[name + ".v"])
    for name, data in files.items():
        p = local / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_bytes(data)
    (project / "sys_pll.ip").write_bytes(pll)
    for name in names[2:]: (local / Path(name).name).write_bytes(vendor[name])
    for name in ("zx8x.mif", "zx8x.hex"): (project / name).write_bytes(files[name])
    fingerprint = sha(json.dumps({"source_sha256": {n: sha(d) for n, d in sources.items()},
        "vendor_sha256": sha(archive_bytes), "system_hz": 100000000, "pixel_hz": 74250000}, sort_keys=True).encode())[:32]
    top = (project / "golden_top.v").read_text()
    top = replace_once(top, ".ninit_done_ninit_done                     (ninit_done),", ".ninit_done_ninit_done                     (ninit_done),\n"
        ".h2f_warm_reset_handshake_reset_req (),\n.h2f_warm_reset_handshake_reset_ack (1'b1),")
    top = replace_once(top, "qsys_top soc_inst (", "wire computer_pixel_clk;\nwire [23:0] computer_rgb;\nwire computer_de, computer_hs, computer_vs;\nqsys_top soc_inst (\n"
        ".fes_computer_pixel_clk_clk(computer_pixel_clk),\n.fes_computer_video_rgb(computer_rgb),\n"
        ".fes_computer_video_de(computer_de), .fes_computer_video_hs(computer_hs), .fes_computer_video_vs(computer_vs),")
    top = replace_once(top, "endmodule", """
fes_de25_zx81_board fes_video_inst (
 .CLOCK0_50(CLOCK0_50), .ninit_done(ninit_done), .pixel_clk(computer_pixel_clk),
 .video_rgb(computer_rgb), .video_de(computer_de), .video_hs(computer_hs), .video_vs(computer_vs),
 .HDMI_TX_INT(HDMI_TX_INT), .HDMI_TX_CLK(HDMI_TX_CLK), .HDMI_TX_DE(HDMI_TX_DE),
 .HDMI_TX_HS(HDMI_TX_HS), .HDMI_TX_VS(HDMI_TX_VS), .HDMI_TX_D(HDMI_TX_D),
 .HDMI_I2C_SCL(HDMI_I2C_SCL), .HDMI_I2C_SDA(HDMI_I2C_SDA),
 .HDMI_MCLK(HDMI_MCLK), .HDMI_SCLK(HDMI_SCLK), .HDMI_LRCLK(HDMI_LRCLK), .HDMI_I2S(HDMI_I2S));
endmodule
""")
    (project / "golden_top.v").write_text(top)
    script = (project / "integrate-mailbox.tcl").read_text()
    extra = sources[BOARD + "zx81-hps/integrate.tcl"].decode()
    definition = files["generated/fes_de25_computer.vh"].decode()
    offset = transport_constant(definition, "PERIPHERAL_OFFSET")
    extra = extra.replace("@COMPUTER_BASE@", hex(offset))
    script = replace_once(script, "save_system $peripheral_system\n\n# Refresh", extra + "\nsave_system $peripheral_system\n\n# Refresh")
    anchor = "save_system " + hps.tcl_word(project / "qsys_top.qsys") + "\n"
    exports = ("# Existing maintenance master gains peripheral control, not a new DDR master.\n"
               "add_connection subsys_debug.fpga_m_master subsys_periph.pb_cpu_0_s0\n"
               "set_connection_parameter_value subsys_debug.fpga_m_master/subsys_periph.pb_cpu_0_s0 "
               f"baseAddress {hex(JTAG_CONTROL_BRIDGE_BASE)}\n")
    for name, kind in (("fes_computer_pixel_clk", "clock sink"), ("fes_computer_video", "conduit end")):
        exports += f"add_interface {name} {kind}\nset_interface_property {name} EXPORT_OF subsys_periph.{name}\n"
    script = replace_once(script, anchor, exports + anchor)
    (project / "integrate-mailbox.tcl").write_text(script)
    with (project / "golden_top.qsf").open("a") as qsf:
        qsf.write("\n# FES HPS-controlled ZX81 diagnostic; no runtime admission.\n"
            "set_global_assignment -name VERILOG_MACRO QUARTUS=1\n"
            "set_global_assignment -name SEED 1\n"
            f"set_global_assignment -name VERILOG_MACRO \"FES_DE25_COMPUTER_BUILD_ID=128'h{fingerprint}\"\n"
            "set_global_assignment -name IP_FILE sys_pll.ip\n"
            "set_global_assignment -name VERILOG_FILE fes_mailbox/board.v\n"
            "set_global_assignment -name VERILOG_FILE fes_mailbox/I2C_HDMI_Config.v\n"
            "set_global_assignment -name VERILOG_FILE fes_mailbox/I2C_Controller.v\n"
            "set_global_assignment -name SEARCH_PATH fes_mailbox\n"
            "set_global_assignment -name SEARCH_PATH fes_mailbox/generated\n"
            "set_global_assignment -name SDC_FILE fes_mailbox/clocks.sdc\n")
    inputs = json.loads((directory / "inputs.json").read_text()); inputs.pop("base_revision", None); inputs.update(selected)
    inputs["functional_snapshot_sha256"] = {n: sha(d) for n, d in sources.items()}
    inputs["computer"] = {"peripheral_offset": offset, "span": transport_constant(definition, "SPAN"),
        "jtag_control_bridge_base": JTAG_CONTROL_BRIDGE_BASE,
        "system_hz": 100000000, "pixel_hz": 74250000, "ula_ratio": [13, 200],
        "build_id": fingerprint, "reviewed_ram_changes": repairs, "reviewed_pll_change": pll_change,
        "pixel_pll_parameters": display.pll_parameters(pll),
        "source_sha256": {n: sha(d) for n, d in files.items()}, "vendor_display_sha256": {n: sha(d) for n, d in vendor.items()},
        "top_sha256": sha(top.encode()), "integration_sha256": sha(script.encode()),
        "configuration_release": "existing GHRD ninit_done; no additional Reset Release IP",
        "new_dma_master": False, "inherited_probe_and_debug_masters": True,
        "runtime_admission": "not-qualified", "physical_mmio": "not-established", "warm_reset_ack_n": 1}
    (directory / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return output, hps, helpers


def pll_review(folder, expected):
    xml = folder / (folder.name + ".xml")
    tree = ET.parse(xml)
    rates = [n.get("value") for n in tree.findall("./perimeter/interface[@name='outclk0']/property[@name='clockRate']")]
    if rates != [str(expected)]: raise ValueError("generated PLL rate differs: " + str(folder))
    for name, value in (("gui_actual_phase_shift0", "0"), ("gui_actual_duty_cycle0", "50")):
        nodes = tree.findall("./entity/parameter[@name='" + name + "']")
        if len(nodes) != 1 or Decimal(nodes[0].get("value", "NaN")) != Decimal(value):
            raise ValueError("generated PLL phase/duty differs: " + name)
    files = list(folder.glob("altera_iopll_*/synth/*.v"))
    if len(files) != 1: raise ValueError("generated PLL atom changed")
    text = files[0].read_text()
    def counter(name):
        values = re.findall(r"\." + name + r"\((\d+)\)", text)
        if len(values) != 1 or int(values[0]) <= 0: raise ValueError("PLL counter contract changed: " + name)
        return int(values[0])
    values = {n: counter(n) for n in ("fb_clk_m_div", "ref_clk_n_div", "out_clk_0_c_div",
        "fb_clk_fractional_div_num", "fb_clk_fractional_div_den", "fb_clk_fractional_div_value")}
    if any(values[n] != 1 for n in values if "fractional" in n): raise ValueError("unexpected fractional PLL feedback")
    if Fraction(50000000 * values["fb_clk_m_div"], values["ref_clk_n_div"] * values["out_clk_0_c_div"]) != expected:
        raise ValueError("generated PLL silently rounded the requested rate")
    if re.findall(r"\.out_clk_0_freq\(36'd(\d+)\)", text) != [str(expected)]:
        raise ValueError("generated PLL atom frequency metadata differs")
    return {"clock_hz": expected, "counters": values, "xml_sha256": sha(xml.read_bytes()), "hdl_sha256": sha(files[0].read_bytes())}


def verify_computer(project, hps):
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = {m.get("path"): m for m in tree.findall("module")}
    component = modules.get("subsys_periph.fes_computer")
    if component is None or component.get("kind") != "fes_de25_computer" or component.get("version") != "1.0":
        raise ValueError("missing generated computer endpoint/version")
    ports = hps.component_ports(component, "avs")
    if ports != {"address": ("Input", 5), "read": ("Input", 1), "write": ("Input", 1),
            "byteenable": ("Input", 4), "writedata": ("Input", 32), "readdata": ("Output", 32),
            "readdatavalid": ("Output", 1), "waitrequest": ("Output", 1),
            "writeresponsevalid": ("Output", 1), "response": ("Output", 2)}:
        raise ValueError("generated computer Avalon width/response interface differs")
    definition = (project / "fes_mailbox/generated/fes_de25_computer.vh").read_text()
    offset, span = transport_constant(definition, "PERIPHERAL_OFFSET"), transport_constant(definition, "SPAN")
    connections = {(c.get("kind"), c.get("start"), c.get("end")): c for c in tree.findall("connection")}
    required = (("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_computer.avs"),
        ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_computer.clk"),
        ("reset", "subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_computer.reset"),
        ("clock", "subsys_periph_fes_computer_pixel_clk.out_clk", "subsys_periph_fes_computer.pixel_clk"))
    if any(k not in connections for k in required): raise ValueError("generated computer bridge/clock/reset route differs")
    if int(connections[required[0]].findtext("parameter[@name='baseAddress']/value"), 0) != offset:
        raise ValueError("generated computer base differs")
    if connections[required[1]].findtext("parameter[@name='clockRateSysInfo']/value") != "100000000":
        raise ValueError("generated computer system clock differs")
    if connections[required[3]].findtext("parameter[@name='clockRateSysInfo']/value") != "74250000":
        raise ValueError("generated computer pixel clock differs")
    lw = connections[("avalon", "subsys_hps_agilex_hps.lwhps2fpga", "subsys_periph_pb_cpu_0.s0")]
    regions = hps.peripheral_slave_map(lw.findtext("parameter[@name='addressMapSysInfo']/value"), hps.probe_base())
    region = [r for r in regions if r["name"] == "subsys_periph.fes_computer.avs"]
    if len(region) != 1 or region[0]["base"] != offset or region[0]["span"] != span: raise ValueError("computer slave span differs")
    jtag = connections.get(("avalon", "subsys_debug_fpga_m.master", "subsys_periph_pb_cpu_0.s0"))
    if jtag is None or int(jtag.findtext("parameter[@name='baseAddress']/value", "-1"), 0) != JTAG_CONTROL_BRIDGE_BASE:
        raise ValueError("generated JTAG control bridge missing or base differs")
    jtag_regions = hps.peripheral_slave_map(jtag.findtext("parameter[@name='addressMapSysInfo']/value"),
                                          JTAG_CONTROL_BRIDGE_BASE + hps.probe_base())
    jtag_region = [r for r in jtag_regions if r["name"] == "subsys_periph.fes_computer.avs"]
    if len(jtag_region) != 1 or jtag_region[0]["base"] != JTAG_CONTROL_BRIDGE_BASE + offset or jtag_region[0]["span"] != span:
        raise ValueError("generated JTAG computer slave span/base differs")
    return {"instance": component.get("path"), "peripheral_slave_map": regions,
            "jtag_slave_map": jtag_regions, "jtag_computer_base": JTAG_CONTROL_BRIDGE_BASE + offset,
            "clock_hz": 100000000, "pixel_hz": 74250000, "physical_mmio": "not-established"}


def generate(output, hps, tool, version):
    directory = output / "hps"; project = directory / "project"
    hps.generate_project(directory, tool, version)
    generation = json.loads((directory / "generation.json").read_text())
    generation["computer"] = verify_computer(project, hps)
    generation["computer"]["pll_reviews"] = {"system": pll_review(project / "ip/pll", 100000000),
                                            "pixel": pll_review(project / "sys_pll", 74250000)}
    (directory / "generation.json").write_text(json.dumps(generation, indent=2, sort_keys=True) + "\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "generate", "build"))
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        selected = identity(); revision = selected.get("fes_revision") if args.action != "project" else None
        if args.action != "project" and not revision: raise ValueError("computer build requires clean committed source")
        output, hps, helpers = prepare(args.vendor_archive, args.output, revision)
        print("HPS ZX81 diagnostic: " + str(output), flush=True)
        if args.action != "project":
            if identity() != selected: raise ValueError("source changed before generation")
            tool, version = hps.BOARD_BUILD.locate_quartus(args.quartus_root)
            generate(output, hps, tool, version)
            if args.action == "build":
                artifact = hps.compile_project(output / "hps", tool)
                generation = json.loads((output / "hps/generation.json").read_text())
                project = output / "hps/project"
                actual = {"system": pll_review(project / "ip/pll", 100000000), "pixel": pll_review(project / "sys_pll", 74250000)}
                if actual != generation["computer"]["pll_reviews"]: raise ValueError("compile changed PLL identity")
                print("Diagnostic SOF: " + str(artifact), flush=True)
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-zx81-hps: " + str(error) + "\n")


if __name__ == "__main__": main()
