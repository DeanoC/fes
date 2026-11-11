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
import types
import xml.etree.ElementTree as ET
import zipfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
BOARD = "sources/misteross/boards/de25nano/"
JTAG_CONTROL_BRIDGE_BASE = 0x00100000
TIMED_RESET_RELEASE = "timed-bram-system-reset-release-v1"
TIMED_RESET_INSTANCES = ("soc_inst|rst_controller", "soc_inst|subsys_periph|rst_controller")
LOCAL = ("build.py", "board.v", "clocks.sdc", "integrate.tcl",
         "managed_bram.py",
         "fes_de25_computer.v", "fes_de25_computer_hw.tcl", "tape_store.v", "video.v",
         "generated/fes_de25_computer.vh", "generated/fes_de25_computer.hpp",
         "generated/fes_computer.vh")
CORE_FILES = ("zx81_machine.sv", "zx81_machine_clock.v", "zx81_dpram.v",
              "zx81_model.vh", "zx81_bus_pack.vh", "zx81_keyboard.v",
              "zx81_tape_store.v")
SHARED_CORE_FILES = frozenset(("zx81_machine.sv", "zx81_machine_clock.v"))


def core_source_path(name):
    if name in SHARED_CORE_FILES:
        return BOARD + "shared-rtl/" + name
    return "sources/misteross/cores/fes-zx81/rtl/" + name


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
    prefix, boundary, native = replacement.partition(marker)
    if (not boundary or prefix.count(b'`ifdef') != 1 or prefix.count(b'`endif') != 0 or
            native != source + b"`endif\n"):
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
    paths |= {core_source_path(n) for n in CORE_FILES}
    firmware = load(HERE.parent / 'zx81/firmware.py', 'de25_computer_firmware_inputs')
    paths |= {p.relative_to(ROOT).as_posix() for p in firmware.source_paths(revision)}
    paths |= {"sources/misteross/cores/fes-common/rtl/z80/" + n + ".sv" for n in
              ("fes_z80_alu", "fes_z80_engine", "fes_z80_bus", "fes_z80_nmos")}
    paths.add("sources/misteross/cores/fes-common/rtl/fes_computer_mailbox.v")
    paths |= {BOARD + "scanout/" + name for name in
              ("build.py", "linux_dma.py", "hdmi_inputs.py", "clocks.sdc", "fes_de25_scanout_board.v")}
    return {p: hps_source if p == hps_path else read(p) for p in sorted(paths)}


def replace_once(source, before, after):
    if source.count(before) != 1: raise ValueError("ZX81 GHRD patch target changed: " + before[:100])
    return source.replace(before, after)


def capture_standard(revision=None):
    """Source closure for the standard component, before a board recipe selects it."""
    sources = capture(revision)
    paths = {BOARD + "zx81-hps/" + name for name in
             ("fes_de25_zx81_standard.v", "fes_de25_zx81_standard_hw.tcl", "standard-clocks.sdc")}
    paths |= {"sources/misteross/cores/fes-zx81/rtl/" + name for name in
              ("fes_zx81_core.v", "zx81_video_720p.v")}
    paths |= {"sources/misteross/cores/fes-common/rtl/" + name for name in
              ("fes_display_plane.v", "fes_display_cdc.v")}
    paths |= {"sources/misteross/cores/fes-common/generated/" + name for name in
              ("fes_computer.vh", "fes_application.vh", "fes_display_plane.vh")}
    paths |= {"sources/misteross/cores/fes-menu/rtl/" + name for name in
              ("fes_menu_control.v", "fes_menu_video.v", "fes_menu_reader.v")}
    paths |= {BOARD + "scanout/" + name for name in
              ("fes_de25_session_display.v", "fes_de25_scanout.v", "generated/fes_de25_scanout.vh")}
    for path in sorted(paths):
        sources[path] = (ROOT / path).read_bytes() if revision is None else subprocess.check_output(
            ["git", "show", revision + ":" + path], cwd=ROOT)
    return sources


def prepare_standard_component(output, revision=None):
    """Stage a captured Qsys component; no generation, synthesis or admission."""
    sources = capture_standard(revision)
    observed = identity() if revision is None else {}
    selected = {"fes_revision": revision, "source_classification": "committed"} if revision else {
        "base_revision": observed.get("fes_revision", observed.get("base_revision")),
        "source_classification": "working-copy-diagnostic"}
    output = Path(output).resolve()
    output.mkdir(parents=True, exist_ok=False)
    snapshot = output / "functional-source"
    for name, data in sources.items():
        path = snapshot / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(data)
    retained = load(snapshot / BOARD / "zx81-hps/build.py", "standard_retained_computer")
    display = load(snapshot / BOARD / "display/build.py", "standard_retained_display")
    core = "sources/misteross/cores/fes-zx81/rtl/"
    common = "sources/misteross/cores/fes-common/"
    files = {name: sources[BOARD + "zx81-hps/" + name] for name in
             ("fes_de25_zx81_standard.v", "fes_de25_zx81_standard_hw.tcl", "fes_de25_computer.v")}
    files.update({name: sources[core + name] for name in
        ("fes_zx81_core.v", "zx81_machine.sv", "zx81_machine_clock.v", "zx81_keyboard.v",
         "zx81_model.vh", "zx81_bus_pack.vh")})
    repairs = {}
    if sources[core + "zx81_machine.sv"].count(b".address_a({1'b0, rom_a[12], rom_a[11:0]}),") != 1:
        raise ValueError("standard ZX81 reachable firmware window changed")
    if core + "zx8x.mif" in sources:
        # An explicitly selected historical revision executes its own captured
        # conversion recipe. Current builds never read the deleted ROM files.
        files["zx8x.mif"], repairs["zx8x.mif"] = retained.complete_standard_firmware(
            sources[core + "zx8x.mif"], sources[core + "zx81_machine.sv"])
    else:
        firmware = load(snapshot / BOARD / "zx81/firmware.py", "standard_selected_firmware")
        generated, repairs["zx8x.mif"] = firmware.generate(sources, output / "firmware")
        files.update(generated)
    for source, target, derive in (
        ("zx81_dpram.v", "zx81_dpram_de25.v", retained.retarget_ram),
        ("zx81_tape_store.v", "zx81_tape_store_de25.v", retained.retarget_tape),
        ("zx81_video_720p.v", "zx81_video_720p_de25.v", retained.ZX81.retarget_video)):
        files[target], repairs[target] = derive(sources[core + source])
    files["fes_menu_reader_de25.v"], repairs["fes_menu_reader_de25.v"] = display.retarget_menu_ram(
        sources["sources/misteross/cores/fes-menu/rtl/fes_menu_reader.v"])
    for name in ("fes_computer_mailbox.v", "fes_display_plane.v", "fes_display_cdc.v"):
        files[name] = sources[common + "rtl/" + name]
    for name in ("fes_z80_alu", "fes_z80_engine", "fes_z80_bus", "fes_z80_nmos"):
        files[name + ".sv"] = sources[common + "rtl/z80/" + name + ".sv"]
    for name in ("fes_menu_control.v", "fes_menu_video.v"):
        files[name] = sources["sources/misteross/cores/fes-menu/rtl/" + name]
    for name in ("fes_computer.vh", "fes_application.vh", "fes_display_plane.vh"):
        files["generated/" + name] = sources[common + "generated/" + name]
    files["generated/fes_de25_computer.vh"] = sources[BOARD + "zx81-hps/generated/fes_de25_computer.vh"]
    for name in ("fes_de25_session_display.v", "fes_de25_scanout.v", "generated/fes_de25_scanout.vh"):
        files[name] = sources[BOARD + "scanout/" + name]
    component = output / "component"
    for name, data in files.items():
        path = component / name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(data)
    record = {**selected, "scope": "standard-Qsys-component-only", "hardware_acceptance": False,
        "sources": {name: sha(data) for name, data in sorted(sources.items())},
        "files": {name: sha(data) for name, data in sorted(files.items())}, "derivations": repairs}
    (output / "component-inputs.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    return component


def standard_audio_paths():
    return [BOARD + "zx81-hps/fes_de25_audio.v"] + [
        "sources/misteross/cores/fes-common/rtl/" + name for name in ("fes_audio_output.v", "fes_audio_i2s.v")]


def capture_standard_board(revision=None):
    """Capture the complete managed standard machine, reader and audio closure."""
    source = (ROOT / (BOARD + "scanout/build.py")).read_bytes() if revision is None else subprocess.check_output(
        ["git", "show", revision + ":" + BOARD + "scanout/build.py"], cwd=ROOT)
    scanout = types.ModuleType("standard_selected_scanout")
    scanout.__file__ = str(HERE.parent / "scanout/build.py")
    exec(compile(source, scanout.__file__, "exec"), scanout.__dict__)
    sources = scanout.capture(revision) | capture_standard(revision)
    paths = standard_audio_paths()
    for name in paths:
        sources[name] = (ROOT / name).read_bytes() if revision is None else subprocess.check_output(
            ["git", "show", revision + ":" + name], cwd=ROOT)
    return sources


def standard_staged_sources(project):
    local = project / "fes_mailbox"
    paths = list(local.glob("*")) + list((local / "generated").rglob("*"))
    return {path.relative_to(project).as_posix(): sha(path.read_bytes()) for path in sorted(paths)
            if path.is_file() and path.suffix in (".v", ".sv", ".vh", ".py", ".sdc", ".tcl", ".mif")}


def prepare_standard_board(archive, output, revision=None, *, package_build_id=None, expansion_socket=False):
    """Select the standard machine in the managed GHRD; never deploy it.

    The canonical package selects internal 16K. The explicit standard PR
    profile selects the socket and separately supplies its registered boundary.
    """
    if type(expansion_socket) is not bool:
        raise ValueError("standard expansion selection must be boolean")
    if expansion_socket and (not revision or not package_build_id):
        raise ValueError("standard PR board requires committed canonical identity")
    sources = capture_standard_board(revision)
    source = sources[BOARD + "scanout/build.py"]
    scanout = types.ModuleType("standard_selected_scanout")
    scanout.__file__ = str(HERE.parent / "scanout/build.py")
    exec(compile(source, scanout.__file__, "exec"), scanout.__dict__)
    paths = standard_audio_paths()
    # Include the complete machine/reader/audio closure before selecting any
    # hardware identity. The result is an integration ID, not a package seal.
    if package_build_id is not None and (not revision or not isinstance(package_build_id,str) or
            not re.fullmatch(r"[0-9a-f]{32}",package_build_id) or package_build_id=="0"*32):
        raise ValueError("standard package identity requires committed source and a nonzero 128-bit ID")
    build_id = package_build_id or sha(json.dumps({"sources": {name: sha(data) for name, data in sorted(sources.items())},
                                                 "expansion_socket": expansion_socket},
                             sort_keys=True).encode())[:32]
    scanout.capture = lambda revision=None: sources.copy()
    # Managed fabric removes the unused JTAG/F2H masters and uses the exact
    # Linux SID, reset and FIFO recipe already used by the allocated MENU.
    # Working-copy inspection is allowed; generation still requires a commit.
    observed = identity() if revision is None else {}
    selected_revision = revision or observed.get("base_revision", observed.get("fes_revision"))
    output, combined, hps, display = scanout.prepare(archive, output, selected_revision,
        hps_first=True, linux_dma=True, package_build_id=build_id)
    directory = output / "combined/hps"
    project = directory / "project"
    local = project / "fes_mailbox"
    component = prepare_standard_component(output / "standard-component", revision)
    for path in component.rglob("*"):
        if path.is_file():
            target = local / path.relative_to(component)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(path.read_bytes())
    (local / "standard-clocks.sdc").write_bytes(sources[BOARD + "zx81-hps/standard-clocks.sdc"])
    # Remove the unused standalone reader component definition. Its reader
    # implementation remains selected by the standard component's fileset.
    (local / "fes_de25_scanout_hw.tcl").unlink()
    script_path = project / "integrate-mailbox.tcl"
    script = script_path.read_text()
    for before, after in (
        ("add_instance fes_scanout fes_de25_scanout 1.0", "add_instance fes_scanout fes_de25_zx81_standard 1.0\n"
            f"set_instance_parameter_value fes_scanout EXPANSION_SOCKET {int(expansion_socket)}"),
        ("add_connection fes_pixel_clk.out_clk fes_scanout.clk", "add_connection fes_pixel_clk.out_clk fes_scanout.clk\n"
            "add_connection periph_clk.out_clk fes_scanout.clk_sys\n"
            "add_connection periph_rst_in.out_reset fes_scanout.system_reset\n"
            "add_connection pb_cpu_0.m0 fes_scanout.computer\n"
            "set_connection_parameter_value pb_cpu_0.m0/fes_scanout.computer baseAddress 0x10300"),
    ):
        script = replace_once(script, before, after)
    # Export the two standard-core conduits through both hierarchy levels.
    anchor = "add_interface fes_scanout_mem avalon start\n"
    exports = ""
    for name in ("audio", "expansion"):
        exports += (f"add_interface fes_scanout_{name} conduit end\n"
                    f"set_interface_property fes_scanout_{name} EXPORT_OF fes_scanout.{name}\n")
    script = replace_once(script, anchor, exports + anchor)
    anchor = "add_interface fes_scanout_video conduit end\nset_interface_property fes_scanout_video EXPORT_OF subsys_periph.fes_scanout_video\n"
    exports = ""
    for name in ("audio", "expansion"):
        exports += (f"add_interface fes_scanout_{name} conduit end\n"
                    f"set_interface_property fes_scanout_{name} EXPORT_OF subsys_periph.fes_scanout_{name}\n")
    script_path.write_text(replace_once(script, anchor, anchor + exports))
    top_path = project / "golden_top.v"
    top = top_path.read_text()
    for pin in ("HDMI_MCLK", "HDMI_SCLK", "HDMI_LRCLK", "HDMI_I2S"):
        top = replace_once(top, f".{pin}({pin})", f".{pin}()")
    top = replace_once(top, "qsys_top soc_inst (", """wire [15:0] standard_audio_sample;
wire standard_exec_reset;
fes_de25_audio fes_audio_inst (
  .CLOCK1_50(CLOCK1_50), .source_clk(system_clk_100_internal),
  .reset_n(top_reset_n), .hold(standard_exec_reset || scanout_clock_fault),
  .left_sample(standard_audio_sample), .right_sample(standard_audio_sample), .locked(),
  .HDMI_MCLK(HDMI_MCLK), .HDMI_SCLK(HDMI_SCLK),
  .HDMI_LRCLK(HDMI_LRCLK), .HDMI_I2S(HDMI_I2S)
);
qsys_top soc_inst (
.fes_scanout_audio_sample(standard_audio_sample), .fes_scanout_audio_reset(standard_exec_reset),
.fes_scanout_expansion_expansion_running(1'b1),
.fes_scanout_expansion_bus_addr(), .fes_scanout_expansion_bus_wdata(),
.fes_scanout_expansion_bus_mreq_n(), .fes_scanout_expansion_bus_iorq_n(),
.fes_scanout_expansion_bus_rd_n(), .fes_scanout_expansion_bus_wr_n(),
.fes_scanout_expansion_bus_m1_n(), .fes_scanout_expansion_bus_rfsh_n(),
.fes_scanout_expansion_bus_cpu_clock(), .fes_scanout_expansion_bus_reset(),
.fes_scanout_expansion_bus_rdata(8'b0),
.fes_scanout_expansion_bus_peek_data(8'b0), .fes_scanout_expansion_bus_dsel(1'b0),
.fes_scanout_expansion_bus_romcs(1'b0), .fes_scanout_expansion_bus_wait(1'b0),
.fes_scanout_expansion_bus_ram_present(1'b0),""")
    top_path.write_text(top)
    # Reuse the locked Terasic audio PLL, changing only the two device metadata
    # values. All 259 PLL parameters other than the target device stay equal.
    with zipfile.ZipFile(archive) as package:
        name = "Demonstration/FPGA/HDMI_ASx4/av_pll.ip"
        if package.namelist().count(name) != 1:
            raise ValueError("missing/duplicate stock audio PLL")
        original = package.read(name)
    parameters = display.pll_parameters(original)
    if any(parameters.get(key) != value for key, value in {
        "gui_reference_clock_frequency": "50.0", "gui_output_clock_frequency0": "12.288",
        "gui_number_of_clocks": "1", "gui_use_locked": "true", "gui_phase_shift0": "0.0",
        "gui_duty_cycle0": "50.0", "system_info_device_component": "A5EB013BB23BE4SR1"}.items()):
        raise ValueError("stock audio PLL contract changed")
    before = b"<ipxact:value>A5EB013BB23BE4SR1</ipxact:value>"
    if original.count(before) != 2:
        raise ValueError("stock audio PLL device metadata changed")
    audio_pll = original.replace(before, b"<ipxact:value>A5EB013BB23BE4SCS</ipxact:value>")
    (local / "av_pll.ip").write_bytes(audio_pll)
    for name in paths:
        (local / Path(name).name).write_bytes(sources[name])
    qsf_path = project / "golden_top.qsf"
    qsf = replace_once(qsf_path.read_text(),
        "set_global_assignment -name SDC_FILE fes_mailbox/clocks.sdc\n",
        "set_global_assignment -name SDC_FILE fes_mailbox/standard-clocks.sdc\n")
    qsf += "\nset_global_assignment -name IP_FILE fes_mailbox/av_pll.ip\n"
    if re.search(r"(?m)^\s*set_global_assignment\s+-name\s+VERILOG_MACRO\s+.*QUARTUS", qsf):
        raise ValueError("upstream already selects the Quartus RAM branch; review before overriding")
    # The portable machine selects altsyncram/MIF in Quartus and a readmemh
    # model otherwise. Use the existing board's guarded Agilex/M20K repairs.
    qsf += "set_global_assignment -name VERILOG_MACRO QUARTUS=1\n"
    (project / "zx8x.mif").write_bytes((component / "zx8x.mif").read_bytes())
    for name in paths:
        qsf += "set_global_assignment -name VERILOG_FILE fes_mailbox/" + Path(name).name + "\n"
    # The preserved GHRD already owns CLOCK1_50; require its actual stock pin.
    if (not re.search(r"set_location_assignment\s+PIN_V16\s+-to\s+CLOCK1_50\s*$", qsf, re.M) or
            not re.search(r'set_instance_assignment -name IO_STANDARD "3.3-V LVCMOS" -to CLOCK1_50\s*$', qsf, re.M)):
        raise ValueError("GHRD CLOCK1_50 does not match the stock audio reference")
    qsf_path.write_text(qsf)
    inputs_path = directory / "inputs.json"
    inputs = json.loads(inputs_path.read_text())
    if revision is None:
        inputs.pop("fes_revision", None)
        inputs.update(base_revision=selected_revision, source_classification="working-copy-diagnostic")
    inputs["standard_zx81"] = {"build_id": build_id, "expansion_socket": expansion_socket,
        "computer_offset": 0x10300, "scope": "standard-core-board-integration", "runtime_admission": "not-qualified",
        "standard_sdc_sha256": sha((local / "standard-clocks.sdc").read_bytes()),
        "audio_pll": {"vendor_sha256": sha(original), "derived_sha256": sha(audio_pll),
                      "parameters": display.pll_parameters(audio_pll)}}
    inputs["scanout"]["build_identity_scheme"] = ("standard-pr-computer-v1" if expansion_socket else
        "canonical-standard-computer-v2" if package_build_id else "standard-board-integration-source-profile")
    inputs["standard_zx81"]["staged_source_sha256"] = standard_staged_sources(project)
    inputs["standard_zx81"]["firmware_sha256"] = sha((project / "zx8x.mif").read_bytes())
    inputs["scanout"]["compiled_top_sha256"] = sha(top_path.read_bytes())
    inputs["scanout"]["rendered_integration_sha256"] = sha(script_path.read_bytes())
    inputs_path.write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    snapshot_path = output / "snapshot.json"
    snapshot_record = json.loads(snapshot_path.read_text())
    if revision is None:
        snapshot_record.pop("fes_revision", None)
        snapshot_record.update(base_revision=selected_revision, source_classification="working-copy-diagnostic")
    snapshot_record["standard_zx81"] = inputs["standard_zx81"]
    snapshot_path.write_text(json.dumps(snapshot_record, indent=2, sort_keys=True) + "\n")
    return output, scanout, combined, hps, display


def generate_standard_board(output, scanout, combined, hps, display, tool, version):
    """Generate the real managed GHRD and verify both standard-core slaves."""
    directory = output / "combined/hps"
    inputs = json.loads((directory / "inputs.json").read_text())
    if inputs.get("source_classification") != "committed" or not inputs.get("fes_revision"):
        raise ValueError("standard board generation requires committed source")
    if (directory / "project/golden_top.qsf").read_text().count(
            "set_global_assignment -name VERILOG_MACRO QUARTUS=1\n") != 1:
        raise ValueError("standard board requires the selected Quartus/M20K memory branch")
    if ((directory / "project/golden_top.qsf").read_text().count(
            "set_global_assignment -name SDC_FILE fes_mailbox/standard-clocks.sdc\n") != 1 or
            sha((directory / "project/fes_mailbox/standard-clocks.sdc").read_bytes()) !=
            inputs["standard_zx81"]["standard_sdc_sha256"]):
        raise ValueError("standard board timing source selection differs")
    combined.generate(output / "combined", hps, display, tool, version)
    scanout.repair_generated_reset_timing(directory)
    scanout.shared_reset_helper(directory).apply(directory)
    generation_path = directory / "generation.json"
    generation = json.loads(generation_path.read_text())
    generation["standard_zx81"] = verify_standard_board(output, scanout, hps, display)
    generation_path.write_text(json.dumps(generation, indent=2, sort_keys=True) + "\n")


def verify_standard_board(output, scanout, hps, display, *, expected_expansion_socket=False):
    directory = output / "combined/hps"
    inputs = json.loads((directory / "inputs.json").read_text())
    project = directory / "project"
    selected = inputs["standard_zx81"]
    if (type(expected_expansion_socket) is not bool or
            type(selected.get("expansion_socket")) is not bool or
            selected["expansion_socket"] != expected_expansion_socket):
        raise ValueError("standard core expansion selection differs from the selected recipe")
    if (inputs["scanout"]["build_id"] != selected["build_id"] or
            sha((project / "golden_top.v").read_bytes()) != inputs["scanout"]["compiled_top_sha256"] or
            sha((project / "integrate-mailbox.tcl").read_bytes()) != inputs["scanout"]["rendered_integration_sha256"] or
            sha((project / "zx8x.mif").read_bytes()) != selected["firmware_sha256"]):
        raise ValueError("standard compiled top, integration, identity or firmware changed")
    actual = standard_staged_sources(project)
    if actual != selected["staged_source_sha256"]:
        raise ValueError("standard staged source inventory/content changed")
    if display.pll_parameters((project/"fes_combined/sys_pll.ip").read_bytes()) != inputs["combined_display"]["pixel_pll_parameters"]:
        raise ValueError("standard pixel PLL parameters changed")
    project = directory / "project"
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = {m.get("path"): m for m in tree.findall("module")}
    endpoint = modules.get("subsys_periph.fes_scanout")
    if endpoint is None or endpoint.get("kind") != "fes_de25_zx81_standard" or endpoint.get("version") != "1.0":
        raise ValueError("generated standard core component differs")
    if hps.component_ports(endpoint,"ready") != {"export": ("Input",1), "stream_ready": ("Input",1)}:
        raise ValueError("generated standard transport/presentation readiness differs")
    build_identity = scanout.endpoint_build_identity(project, endpoint, hps, inputs["standard_zx81"]["build_id"])
    for interface, bits in (("computer", 5), ("avs", 7)):
        expected = {"address": ("Input", bits), "read": ("Input", 1), "write": ("Input", 1),
            "byteenable": ("Input", 4), "writedata": ("Input", 32), "waitrequest": ("Output", 1),
            "readdata": ("Output", 32), "readdatavalid": ("Output", 1),
            "writeresponsevalid": ("Output", 1), "response": ("Output", 2)}
        if hps.component_ports(endpoint, interface) != expected:
            raise ValueError("generated standard core slave differs: " + interface)
    expected_master = {"address": ("Output", 32), "burstcount": ("Output", 7), "read": ("Output", 1),
        "byteenable": ("Output", 16), "waitrequest": ("Input", 1), "readdata": ("Input", 128),
        "readdatavalid": ("Input", 1), "response": ("Input", 2)}
    if hps.component_ports(endpoint, "avm") != expected_master:
        raise ValueError("generated standard read-only master differs")
    connections = {(c.get("kind"), c.get("start"), c.get("end")): c for c in tree.findall("connection")}
    for kind, start, end, value in (
        ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_scanout.clk_sys", 100000000),
        ("clock", "subsys_periph_fes_pixel_clk.out_clk", "subsys_periph_fes_scanout.clk", 74250000),
        ("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_scanout.computer", 0x10300),
    ):
        connection = connections.get((kind, start, end))
        parameter = "clockRateSysInfo" if kind == "clock" else "baseAddress"
        if connection is None or int(connection.findtext("parameter[@name='" + parameter + "']/value"), 0) != value:
            raise ValueError("generated standard core route differs: " + end)
    for start, end in (
        ("subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_scanout.system_reset"),
        ("subsys_periph_fes_scanout_pixel_reset.out_reset", "subsys_periph_fes_scanout.reset"),
    ):
        if ("reset", start, end) not in connections:
            raise ValueError("generated standard core reset differs: " + end)
    endpoint_parameters, _ = scanout.referenced_parameters(project, endpoint, hps)
    scanout.require_parameters(endpoint_parameters, {"EXPANSION_SOCKET": int(expected_expansion_socket),
                                                   "FIRMWARE_INIT": "zx8x.mif"}, "standard core")
    bridge_parameters = {}
    for name, width, address_bits, burst, depth in (("fes_ctrl_cdc", 32, 7, 1, 4), ("fes_mem_cdc", 128, 32, 64, 512)):
        module = modules.get("subsys_periph." + name)
        if module is None or module.get("kind") != "mm_ccb" or module.get("version") != "19.4.1":
            raise ValueError("generated standard core crossing differs: " + name)
        actual, _ = scanout.referenced_parameters(project, module, hps)
        bridge_parameters[name] = scanout.require_parameters(actual, {
            "DATA_WIDTH": width, "SYMBOL_WIDTH": 8, "ADDRESS_WIDTH": address_bits,
            "MAX_BURST_SIZE": burst, "COMMAND_FIFO_DEPTH": 4, "RESPONSE_FIFO_DEPTH": depth,
            "MASTER_SYNC_DEPTH": 2, "SLAVE_SYNC_DEPTH": 2, "ENABLE_RESPONSE": 1,
            "USE_AUTO_ADDRESS_WIDTH": 0, "SYNC_RESET": 0, "PIPELINE_ENABLE": 0}, name)
    actual_audio = display.pll_parameters((project / "fes_mailbox/av_pll.ip").read_bytes())
    if actual_audio != inputs["standard_zx81"]["audio_pll"]["parameters"]:
        raise ValueError("generation changed the selected stock audio PLL")
    return {"build_identity": build_identity,
        "standard_sdc_sha256": inputs["standard_zx81"]["standard_sdc_sha256"],
        "computer_offset": 0x10300, "bridge_parameters": bridge_parameters,
        "managed_fabric": scanout.verify_managed_fabric(directory, hps),
        "reset_timing": scanout.verify_generated_reset_timing(directory),
        "shared_fifo_resets": scanout.shared_reset_helper(directory).verify(directory),
        "hdmi_inputs": scanout.verify_hdmi_inputs(project, inputs, hps),
        "audio_pll_parameters_unchanged": True,
        "scope": "whole-GHRD-IP-generation", "fit_and_timing": "not-run", "hardware_acceptance": "not-run"}


def compile_standard_board(output, scanout, hps, display, tool):
    """Compile one checked standard design and retain Linux phase artifacts."""
    directory=output/"combined/hps"
    expected=json.loads((directory/"generation.json").read_text())["standard_zx81"]
    def verify():
        if verify_standard_board(output,scanout,hps,display)!=expected:
            raise ValueError("standard source/topology changed after generation")
    verify()
    artifact=hps.compile_project(directory,tool)
    verify()
    artifacts=hps.linux_configuration_artifacts(directory,tool,artifact,hps_initialization="HPS FIRST")
    record={"format":1,"classification":"unadmitted-HPS-first-standard-computer",
        "inputs_sha256":hps.digest(directory/"inputs.json"),
        "generation_sha256":hps.digest(directory/"generation.json"),
        "build_id":json.loads((directory/"inputs.json").read_text())["standard_zx81"]["build_id"],**artifacts,
        "production_admission":False,"hardware_acceptance":False}
    (directory/"linux-programming.json").write_text(json.dumps(record,indent=2,sort_keys=True)+"\n")
    verify()
    return artifact



def transport_constant(source, name):
    values = re.findall(r"^`define FES_DE25_COMPUTER_" + name + r" 32'h([0-9a-f]{8})$", source, re.M)
    if len(values) != 1: raise ValueError("missing/duplicate transport constant: " + name)
    return int(values[0], 16)


def prepare(archive, output=None, revision=None, *, hps_first=False, package_build_id=None):
    if package_build_id is not None and (not hps_first or not revision or
            not isinstance(package_build_id, str) or not re.fullmatch(r"[0-9a-f]{32}", package_build_id) or
            package_build_id == "0" * 32):
        raise ValueError("canonical computer requires committed HPS-first source and a nonzero build identity")
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
    return prepare_captured(archive, output, sources, selected, hps_first=hps_first,
                            package_build_id=package_build_id)


def prepare_captured(archive, output, sources, selected, *, hps_first=False, package_build_id=None):
    """Prepare a caller-owned private output from explicitly captured bytes."""
    if type(hps_first) is not bool:
        raise ValueError("HPS-first selection must be boolean")
    if package_build_id is not None and (not hps_first or not selected.get("fes_revision") or
            selected.get("source_classification") != "committed" or
            not isinstance(package_build_id, str) or not re.fullmatch(r"[0-9a-f]{32}", package_build_id) or
            package_build_id == "0" * 32):
        raise ValueError("canonical computer requires committed HPS-first source and a nonzero build identity")
    boot_order = "HPS FIRST" if hps_first else "AFTER INIT_DONE"
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
        files[name] = sources[core_source_path(name)]
    firmware_record = None
    rom_paths = ["sources/misteross/cores/fes-zx81/rtl/" + name for name in ('zx8x.hex', 'zx8x.mif')]
    if all(name in sources for name in rom_paths):
        files.update({Path(name).name: sources[name] for name in rom_paths})
    else:
        firmware = load(snapshot / BOARD / 'zx81/firmware.py', 'de25_computer_selected_firmware')
        generated, firmware_record = firmware.generate(sources, output / 'firmware')
        files.update(generated)
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
        "vendor_sha256": sha(archive_bytes), "system_hz": 100000000, "pixel_hz": 74250000,
        "hps_initialization": boot_order}, sort_keys=True).encode())[:32]
    top = (project / "golden_top.v").read_text()
    top = replace_once(top, ".ninit_done_ninit_done                     (ninit_done),", ".ninit_done_ninit_done                     (ninit_done),\n"
        ".h2f_warm_reset_handshake_reset_req (),\n.h2f_warm_reset_handshake_reset_ack (1'b1),")
    top = replace_once(top, "qsys_top soc_inst (", "wire computer_pixel_clk;\nwire [23:0] computer_rgb;\nwire computer_de, computer_hs, computer_vs, computer_video_ready;\nqsys_top soc_inst (\n"
        ".fes_computer_pixel_clk_clk(computer_pixel_clk),\n.fes_computer_video_rgb(computer_rgb),\n"
        ".fes_computer_video_de(computer_de), .fes_computer_video_hs(computer_hs), .fes_computer_video_vs(computer_vs),\n.fes_computer_video_ready(computer_video_ready),")
    top = replace_once(top, "endmodule", """
fes_de25_zx81_board fes_video_inst (
 .CLOCK0_50(CLOCK0_50), .ninit_done(ninit_done), .pixel_clk(computer_pixel_clk),
 .video_rgb(computer_rgb), .video_de(computer_de), .video_hs(computer_hs), .video_vs(computer_vs),
 .video_ready(computer_video_ready),
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
    if re.search(r"(?m)^\s*set_global_assignment\s+-name\s+HPS_INITIALIZATION\b",
                 (project / "golden_top.qsf").read_text()):
        raise ValueError("vendor already selects HPS initialization; review before overriding")
    with (project / "golden_top.qsf").open("a") as qsf:
        qsf.write("\n# FES HPS-controlled ZX81 diagnostic; no runtime admission.\n"
            "set_global_assignment -name VERILOG_MACRO QUARTUS=1\n"
            "set_global_assignment -name SEED 1\n"
            f'set_global_assignment -name HPS_INITIALIZATION "{boot_order}"\n'
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
    inputs["computer"] = {"hps_initialization": boot_order, "peripheral_offset": offset, "span": transport_constant(definition, "SPAN"),
        "generated_firmware": firmware_record,
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
    if package_build_id is not None:
        prepare_canonical(directory, hps, sources, snapshot, package_build_id)
    return output, hps, helpers


def prepare_canonical(directory, hps, sources, snapshot, build_id):
    project = directory / "project"
    local = project / "fes_mailbox"
    managed = load(snapshot / BOARD / "zx81-hps/managed_bram.py", "canonical_bram_preparation")
    dma = load(snapshot / BOARD / "scanout/linux_dma.py", "canonical_bram_debug_constraints")
    hdmi = load(snapshot / BOARD / "scanout/hdmi_inputs.py", "canonical_bram_hdmi")
    inputs = json.loads((directory / "inputs.json").read_text())
    files, hdmi_receipt = hdmi.derive((local / "I2C_HDMI_Config.v").read_bytes(),
                                     (local / "I2C_Controller.v").read_bytes())
    board = managed.hdmi_board((local / "board.v").read_text(),
        sources[BOARD + "scanout/fes_de25_scanout_board.v"].decode())
    (local / "board.v").write_text(board)
    for name, data in files.items():
        (local / name).write_bytes(data)
    qsf = (project / "golden_top.qsf").read_text()
    for before, after in (("I2C_HDMI_Config.v", "fes_de25_scanout_hdmi_config.v"),
                          ("I2C_Controller.v", "fes_de25_scanout_i2c_controller.v")):
        qsf = replace_once(qsf, "-name VERILOG_FILE fes_mailbox/" + before,
                           "-name VERILOG_FILE fes_mailbox/" + after)
        (local / before).unlink()
    qsf, count = re.subn(r"FES_DE25_COMPUTER_BUILD_ID=128'h[0-9a-f]{32}",
                         "FES_DE25_COMPUTER_BUILD_ID=128'h" + build_id, qsf)
    if count != 1:
        raise ValueError("canonical computer macro identity differs")
    (project / "golden_top.qsf").write_text(qsf)
    (directory / "prepared-computer.qsf").write_text(qsf)
    clock_source = sources[BOARD + "scanout/clocks.sdc"].decode()
    start = clock_source.index("# The two external inputs terminate")
    stop = clock_source.index("# Configuration release,", start)
    (local / "clocks.sdc").write_text(timed_computer_sdc(
        sources[BOARD + "zx81-hps/clocks.sdc"].decode()) + "\n" + clock_source[start:stop])
    hw = local / "fes_de25_computer_hw.tcl"
    hw.write_text(replace_once(hw.read_text(), "add_fileset QUARTUS_SYNTH QUARTUS_SYNTH\n",
        "add_parameter BUILD_ID STD_LOGIC_VECTOR 0x0\n"
        "set_parameter_property BUILD_ID WIDTH 128\n"
        "set_parameter_property BUILD_ID HDL_PARAMETER true\n"
        "set_parameter_property BUILD_ID AFFECTS_GENERATION false\n"
        "add_fileset QUARTUS_SYNTH QUARTUS_SYNTH\n"))
    script = project / "integrate-mailbox.tcl"
    script.write_text(replace_once(script.read_text(), "add_instance fes_computer fes_de25_computer 1.1\n",
        "add_instance fes_computer fes_de25_computer 1.1\n"
        "set_instance_parameter_value fes_computer BUILD_ID 0x" + build_id + "\n"))
    inputs["allowed_hps_parameter_changes"] = managed.configure(project, hps, dma)
    inputs["f2sdram_master_profile"] = "bram-computer"
    computer = inputs["computer"]
    computer.update(build_id=build_id, build_identity_scheme="canonical-functional-record-v2",
        managed_fabric=managed.POLICY, managed_verifier_sha256=sha(sources[BOARD + "zx81-hps/managed_bram.py"]),
        reset_timing=TIMED_RESET_RELEASE,
        inherited_probe_and_debug_masters=False, hdmi_inputs=hdmi_receipt,
        jtag_control_bridge_base=None, top_sha256=hps.digest(project / "golden_top.v"),
        integration_sha256=hps.digest(script), qsf_sha256=hps.digest(project / "golden_top.qsf"))
    computer["source_sha256"] = {name: hps.digest(local / name) for name in computer["source_sha256"]}
    computer["source_sha256"].update({name: sha(data) for name, data in files.items()})
    (directory / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")


def timed_computer_sdc(source):
    # The peripheral reset is already released by the same 100 MHz clock as
    # sys_release. Keep both pixel-domain ingress cuts and time system ingress.
    for pin in ("aclr", "clrn"):
        source = replace_once(source, "*|video|sys_release*|" + pin + " ", "")
    source = replace_once(source, "get_collection_size $owner_reset_pins] != 4",
                          "get_collection_size $owner_reset_pins] != 2")
    source = replace_once(source, "4 reset ingress pins", "2 pixel reset ingress pins")
    return replace_once(source,
        "# Raw epoch reset enters two per-domain release chains. Recovery/removal from\n"
        "# their locally synchronized outputs to owner state remain timed.",
        "# Peripheral reset release and sys_release share the 100 MHz system clock;\n"
        "# time that ingress and its downstream owner state. Only pixel ingress is asynchronous.")


def reset_timing_helper(directory):
    inputs = json.loads((directory / "inputs.json").read_text())
    computer = inputs.get("computer", {})
    if (inputs.get("f2sdram_master_profile") != "bram-computer" or
            computer.get("build_identity_scheme") != "canonical-functional-record-v2" or
            computer.get("hps_initialization") != "HPS FIRST" or
            computer.get("reset_timing") != TIMED_RESET_RELEASE):
        raise ValueError("same-clock computer reset timing requires canonical HPS-first BRAM ZX81")
    return load(directory.parent / "functional-source" / BOARD / "scanout/build.py",
                "canonical_bram_reset_timing")


def repair_generated_reset_timing(directory):
    scanout = reset_timing_helper(directory)
    project = directory / "project"
    originals = directory / "reset-timing-originals"
    originals.mkdir(exist_ok=False)
    files = {}
    for path in scanout.reset_sdc_files(project):
        name = path.relative_to(project).as_posix()
        original = path.read_bytes()
        changed = scanout.timed_vendor_reset_sdc(original, timed_instances=TIMED_RESET_INSTANCES)
        retained = originals / name
        retained.parent.mkdir(parents=True, exist_ok=True)
        retained.write_bytes(original)
        path.write_bytes(changed)
        files[name] = {"before_sha256": sha(original), "after_sha256": sha(changed)}
    receipt = {"policy": TIMED_RESET_RELEASE, "clock": "pll_inst|iopll_0_outclk0",
               "timed_instances": list(TIMED_RESET_INSTANCES), "files": files}
    (directory / "reset-timing.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    return verify_generated_reset_timing(directory)


def verify_system_reset_receivers(project):
    for name, clock, reset in (
            ("qsys_top/synth/qsys_top.v", "clk_100_out_clk_clk", "~rst_in_out_reset_reset"),
            ("peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v",
             "periph_clk_out_clk_clk", "~periph_rst_in_out_reset_reset")):
        source = re.sub(r"/\*.*?\*/|//[^\n]*", "", (project / name).read_text(), flags=re.S)
        blocks = re.findall(r"\baltera_reset_controller\s*#\((.*?)\)\s+rst_controller\s*\((.*?)\);", source, re.S)
        if len(blocks) != 1:
            raise ValueError("computer system reset receiver inventory differs: " + name)
        parameters, ports = ({key: value.strip() for key, value in re.findall(r"\.(\w+)\s*\(([^()]*)\)", text)}
                             for text in blocks[0])
        if (any(parameters.get(key) != value for key, value in {
                "NUM_RESET_INPUTS": "1", "OUTPUT_RESET_SYNC_EDGES": '"deassert"', "SYNC_DEPTH": "2"}.items()) or
                ports.get("clk") != clock or ports.get("reset_in0") != reset):
            raise ValueError("computer system reset receiver clock/source/release differs: " + name)


def verify_generated_reset_timing(directory):
    scanout = reset_timing_helper(directory)
    project = directory / "project"
    verify_system_reset_receivers(project)
    receipt = json.loads((directory / "reset-timing.json").read_text())
    names = {p.relative_to(project).as_posix() for p in scanout.reset_sdc_files(project)}
    if (set(receipt) != {"policy", "clock", "timed_instances", "files"} or
            receipt["policy"] != TIMED_RESET_RELEASE or receipt["clock"] != "pll_inst|iopll_0_outclk0" or
            receipt["timed_instances"] != list(TIMED_RESET_INSTANCES) or set(receipt["files"]) != names):
        raise ValueError("computer same-clock reset timing receipt differs from the selected profile")
    for name in names:
        original = (directory / "reset-timing-originals" / name).read_bytes()
        changed = scanout.timed_vendor_reset_sdc(original, timed_instances=TIMED_RESET_INSTANCES)
        if (receipt["files"][name] != {"before_sha256": sha(original), "after_sha256": sha(changed)} or
                (project / name).read_bytes() != changed):
            raise ValueError("generated computer reset constraints changed: " + name)
    raw = directory.parent / "functional-source" / BOARD
    scanout_sdc = (raw / "scanout/clocks.sdc").read_text()
    start = scanout_sdc.index("# The two external inputs terminate")
    stop = scanout_sdc.index("# Configuration release,", start)
    expected = timed_computer_sdc((raw / "zx81-hps/clocks.sdc").read_text()) + "\n" + scanout_sdc[start:stop]
    if (project / "fes_mailbox/clocks.sdc").read_text() != expected:
        raise ValueError("computer same-clock reset SDC differs from captured source")
    return receipt


def generated_computer_qsf(prepared):
    # The actual 26.3 Qsys run retires the duplicate vendor version and
    # registers exactly these three generated IPs. Retain/check every byte.
    return replace_once(prepared,
        'set_global_assignment -name LAST_QUARTUS_VERSION "25.1.1 Pro Edition"\n', '') + ''.join(
        'set_global_assignment -name IP_FILE peripheral_subsys/ip/peripheral_subsys/' + name + '.ip\n'
        for name in ('peripheral_subsys_fes_mailbox', 'peripheral_subsys_fes_computer_pixel_clk',
                     'peripheral_subsys_fes_computer'))


def verify_canonical(directory, hps):
    inputs = json.loads((directory / "inputs.json").read_text())
    computer = inputs["computer"]
    helper = hps.bram_computer_helper(inputs)
    project = directory / "project"
    for name, expected in computer["source_sha256"].items():
        if hps.digest(project / "fes_mailbox" / name) != expected:
            raise ValueError("canonical computer staged source differs: " + name)
    for name, field in (("golden_top.v", "top_sha256"),
                        ("integrate-mailbox.tcl", "integration_sha256")):
        if hps.digest(project / name) != computer[field]:
            raise ValueError("canonical computer project input differs: " + name)
    prepared = directory / "prepared-computer.qsf"
    if (hps.digest(prepared) != computer["qsf_sha256"] or
            (project / "golden_top.qsf").read_text() != generated_computer_qsf(prepared.read_text())):
        raise ValueError("canonical computer generated QSF differs from reviewed preparation")
    retained = directory.parent / "functional-source" / BOARD / "scanout/build.py"
    scanout = load(retained, "canonical_bram_identity_review")
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    components = [m for m in tree.findall("module") if m.get("path") == "subsys_periph.fes_computer"]
    if len(components) != 1:
        raise ValueError("canonical computer instance differs")
    identity = scanout.endpoint_build_identity(project, components[0], hps, computer["build_id"])
    hdmi = scanout.verify_hdmi_inputs(project, inputs, hps, profile="computer", board_file="board.v")
    dma = load(directory.parent / "functional-source" / BOARD / "scanout/linux_dma.py", "canonical_bram_sdc_review")
    return {"identity": identity, "fabric": helper.verify(project, hps.module_parameters, hps.digest),
            "hdmi_inputs": hdmi, "retired_debug_constraints": dma.verify_managed_debug_constraints(project),
            "reset_timing": verify_generated_reset_timing(directory)}


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
    if component is None or component.get("kind") != "fes_de25_computer" or component.get("version") != "1.1":
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
    inputs = json.loads((project.parent / "inputs.json").read_text())
    canonical = inputs.get("f2sdram_master_profile") == "bram-computer"
    regions = hps.peripheral_slave_map(lw.findtext("parameter[@name='addressMapSysInfo']/value"),
                                       None if canonical else hps.probe_base())
    region = [r for r in regions if r["name"] == "subsys_periph.fes_computer.avs"]
    if len(region) != 1 or region[0]["base"] != offset or region[0]["span"] != span: raise ValueError("computer slave span differs")
    if canonical:
        return {"instance": component.get("path"), "peripheral_slave_map": regions,
                "clock_hz": 100000000, "pixel_hz": 74250000,
                "canonical": verify_canonical(project.parent, hps), "physical_mmio": "not-established"}
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
    inputs = json.loads((directory / "inputs.json").read_text())
    if inputs.get("f2sdram_master_profile") == "bram-computer":
        repair_generated_reset_timing(directory)
    generation = json.loads((directory / "generation.json").read_text())
    generation["computer"] = verify_computer(project, hps)
    generation["computer"]["pll_reviews"] = {"system": pll_review(project / "ip/pll", 100000000),
                                            "pixel": pll_review(project / "sys_pll", 74250000)}
    (directory / "generation.json").write_text(json.dumps(generation, indent=2, sort_keys=True) + "\n")


def compile_canonical(output, hps, tool):
    directory = output / "hps"
    project = directory / "project"
    expected = json.loads((directory / "generation.json").read_text())["computer"]
    def review():
        actual = verify_computer(project, hps)
        if "canonical" not in actual:
            raise ValueError("canonical computer compilation requires its managed profile")
        actual["pll_reviews"] = {"system": pll_review(project / "ip/pll", 100000000),
                                 "pixel": pll_review(project / "sys_pll", 74250000)}
        if actual != expected:
            raise ValueError("canonical computer source/handoff differs from generation")
    review()
    artifact = hps.compile_project(directory, tool)
    review()
    package_hps_first(output, hps, tool, artifact)
    return artifact



def package_hps_first(output, hps, tool, artifact):
    """Bind the computer identity to explicit Linux phase artifacts."""
    directory = output / "hps"
    inputs = json.loads((directory / "inputs.json").read_text())
    artifacts = hps.linux_configuration_artifacts(directory, tool, artifact,
        hps_initialization=inputs["computer"].get("hps_initialization"))
    record = {"format": 1, "classification": "unadmitted-HPS-first-diagnostic",
              "fes_revision": inputs["fes_revision"], "computer_build_id": inputs["computer"]["build_id"],
              **artifacts, "hardware_programming": "not-performed", "boot_compatibility": "not-qualified",
              "Linux_load_acceptance": "not-qualified", "production_admission": False}
    (directory / "linux-programming.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "generate", "build"))
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--standard-core", action="store_true",
                        help="integrate the standard core with allocated display and stock audio (project/generate/build)")
    parser.add_argument("--hps-first", action="store_true",
                        help="prepare HPS-first diagnostic and separate Linux core RBF; never program flash")
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        selected = identity(); revision = selected.get("fes_revision") if args.action != "project" else None
        if args.action != "project" and not revision: raise ValueError("computer build requires clean committed source")
        if args.standard_core:
            output, scanout, combined, hps, display = prepare_standard_board(args.vendor_archive, args.output, revision)
            print("Standard ZX81 board integration: " + str(output), flush=True)
            if args.action != "project":
                if identity() != selected: raise ValueError("source changed before standard board generation")
                tool, version = hps.BOARD_BUILD.locate_quartus(args.quartus_root)
                generate_standard_board(output, scanout, combined, hps, display, tool, version)
                if args.action == "build":
                    compile_standard_board(output, scanout, hps, display, tool)
                if identity() != selected: raise ValueError("source changed during standard board build")
            return
        output, hps, helpers = prepare(args.vendor_archive, args.output, revision, hps_first=args.hps_first)
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
                if args.hps_first:
                    package_hps_first(output, hps, tool, artifact)
                print("Diagnostic SOF: " + str(artifact), flush=True)
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-zx81-hps: " + str(error) + "\n")


if __name__ == "__main__": main()
