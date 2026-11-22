"""Integrate a standard machine with its own clock into the managed DE25 GHRD.

The board, reset, HDMI, allocated-display and Linux memory recipe is the
managed MENU/ZX81 one, reused through its builders. A machine's build.py
supplies its component, sources and constraints; this selects them, with the
shared machine and audio PLL, the board SDRAM pins and live PCM. It never
programs a kit. A compile here is a diagnostic candidate: package export,
timing review and runtime admission are separate gates.
"""
from pathlib import Path
import argparse
import importlib.util
import json
import os
import re
import subprocess
import types
import zipfile

HERE = Path(__file__).resolve().parent
BOARD_DIR = HERE.parent
BOARD = "sources/misteross/boards/de25nano/"
COMMON = "sources/misteross/cores/fes-common/"
OFFSET = 0x10300
# The shared machine PLL on CLOCK1_50 (the Spectrum and the Atari ST): the
# machine clock and the audio master clock. A machine with clocks of its own
# names each of these in its spec: MACHINE_HZ, PLL_IP, the PLL_PARAMETERS its IP
# must keep, and the SDRAM_CLOCKS (conduit role, PLL output) it takes beside
# the SDRAM pins; and PLL_COMMENT, which says how its PLL makes them.
MACHINE_HZ = 52222222
PLL_IP = BOARD + "machine/machine_pll.ip"
PLL_PARAMETERS = {"gui_number_of_clocks": "2", "gui_output_clock_frequency0": "52.222222",
                  "gui_output_clock_frequency1": "12.287582"}
CLOCK_SPEC = ("MACHINE_HZ", "PLL_IP", "PLL_PARAMETERS", "SDRAM_CLOCKS", "PLL_COMMENT")
ENDPOINT = "soc_inst|subsys_periph|fes_scanout|fes_scanout"
# The shared scanout, HPS and reset verifiers recognise one standard-core
# integration record, under the key the ZX81 gave it. Renaming it there moves
# the MENU and ZX81 identities, so this record keeps the key and names its core.
RECORD = "standard_zx81"
# Files every machine component stages from the board's shared sources.
SHELL_FILES = ("fes_gp_crossing.v", "fes_firmware_upload.v", "fes_computer_mailbox.v",
               "fes_display_plane.v", "fes_menu_control.v", "fes_menu_video.v")


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


ZX81 = load(BOARD_DIR / "zx81-hps/build.py", "de25_machine_board_helpers")
ROOT = ZX81.ROOT
sha, identity, replace_once = ZX81.sha, ZX81.identity, ZX81.replace_once


def clocks(spec):
    """The machine's clock spec: its own, complete, or the shared PLL's."""
    named = [hasattr(spec, name) for name in CLOCK_SPEC]
    if not any(named):
        return MACHINE_HZ, PLL_IP, PLL_PARAMETERS, ()
    if not all(named):
        raise ValueError(spec.NAME + " names part of a machine clock spec")
    return spec.MACHINE_HZ, spec.PLL_IP, spec.PLL_PARAMETERS, tuple(spec.SDRAM_CLOCKS)


def selected_scanout(source):
    scanout = types.ModuleType("machine_selected_scanout")
    scanout.__file__ = str(BOARD_DIR / "scanout/build.py")
    exec(compile(source, scanout.__file__, "exec"), scanout.__dict__)
    return scanout


def module(sources, path, name):
    """A captured Python source as a module, so a committed build runs its own bytes."""
    result = types.ModuleType(name)
    result.__file__ = str(ROOT / path)
    exec(compile(sources[path], result.__file__, "exec"), result.__dict__)
    return result


def capture(spec, revision=None):
    """The managed scanout closure plus the complete machine, media and audio closure."""
    def read(path):
        return (ROOT / path).read_bytes() if revision is None else subprocess.check_output(
            ["git", "show", revision + ":" + path], cwd=ROOT)
    sources = selected_scanout(read(BOARD + "scanout/build.py")).capture(revision)
    paths = set(spec.SOURCES)
    paths |= {BOARD + "shared-rtl/" + name for name in SHELL_FILES + ("fes_menu_reader.v", "fes_de25_machine_audio.v")}
    paths |= {BOARD + "machine/board.py", clocks(spec)[1],
              BOARD + "shared-rtl/generated/fes_firmware_upload.vh",
              BOARD + "zx81-hps/fes_de25_computer.v", BOARD + "zx81-hps/generated/fes_de25_computer.vh",
              BOARD + "zx81-hps/build.py", BOARD + "zx81/build.py", COMMON + "rtl/fes_display_cdc.v"}
    paths |= {COMMON + "rtl/" + name for name in ("fes_audio_output.v", "fes_audio_i2s.v")}
    paths |= {COMMON + "generated/" + name for name in
              ("fes_computer.vh", "fes_application.vh", "fes_display_plane.vh")}
    paths |= {BOARD + "scanout/" + name for name in
              ("fes_de25_session_display.v", "fes_de25_scanout.v", "generated/fes_de25_scanout.vh")}
    for path in sorted(paths):
        sources.setdefault(path, read(path))
    return sources


def component_files(spec, sources):
    """Every file of the Qsys component, and the record of each derivation."""
    display = module(sources, BOARD + "display/build.py", "machine_selected_display")
    files, derived = spec.component_files(sources)
    for name in SHELL_FILES:
        files[name] = sources[BOARD + "shared-rtl/" + name]
    files["fes_display_cdc.v"] = sources[COMMON + "rtl/fes_display_cdc.v"]
    files["fes_menu_reader_de25.v"], derived["fes_menu_reader_de25.v"] = display.retarget_menu_ram(
        sources[BOARD + "shared-rtl/fes_menu_reader.v"])
    for name in ("fes_computer.vh", "fes_application.vh", "fes_display_plane.vh"):
        files["generated/" + name] = sources[COMMON + "generated/" + name]
    files["generated/fes_firmware_upload.vh"] = sources[BOARD + "shared-rtl/generated/fes_firmware_upload.vh"]
    files["generated/fes_de25_computer.vh"] = sources[BOARD + "zx81-hps/generated/fes_de25_computer.vh"]
    for name in ("fes_de25_session_display.v", "fes_de25_scanout.v", "generated/fes_de25_scanout.vh"):
        files[name] = sources[BOARD + "scanout/" + name]
    return files, derived, display


def shared_resets(spec, scanout, directory):
    """The shared FIFO reset helper, reviewing this component's generated parent."""
    helper = scanout.shared_reset_helper(directory)
    helper.STANDARD_PARENT_SHA256 = spec.PARENT_SHA256
    return helper


def machine_pll(spec, display, data):
    """The machine's PLL: output 0 the machine clock, output 1 the audio master."""
    parameters = display.pll_parameters(data)
    expected = {"gui_reference_clock_frequency": "50.0", "gui_use_locked": "true",
                "system_info_device_component": "A5EB013BB23BE4SCS", **clocks(spec)[2]}
    if any(parameters.get(key) != value for key, value in expected.items()):
        raise ValueError("machine PLL contract changed")
    return parameters


TOP_BEFORE = "qsys_top soc_inst ("
TOP_AFTER = """wire [15:0] standard_audio_sample;
wire standard_exec_reset;
wire machine_clk, machine_audio_clk, machine_pll_locked;
wire [15:0] fes_dram_dq_o;
wire [1:0] fes_dram_dqm;
wire fes_dram_dq_oe;
// One PLL on CLOCK1_50 in place of the stock audio PLL, whose pin can feed no
// second one: x 188/15, then /12 for the machine's 52.224 MHz and /51 for the
// 12.288 MHz audio master clock, both -34 ppm and in the DE10 cores' ratio.
machine_pll fes_machine_pll (
  .refclk(CLOCK1_50), .rst(!top_reset_n), .outclk_0(machine_clk), .outclk_1(machine_audio_clk),
  .locked(machine_pll_locked)
);
fes_de25_machine_audio fes_audio_inst (
  .source_clk(machine_clk), .audio_clk(machine_audio_clk), .locked(machine_pll_locked),
  .hold(standard_exec_reset), .fault(scanout_clock_fault),
  .left_sample(standard_audio_sample), .right_sample(standard_audio_sample),
  .HDMI_MCLK(HDMI_MCLK), .HDMI_SCLK(HDMI_SCLK),
  .HDMI_LRCLK(HDMI_LRCLK), .HDMI_I2S(HDMI_I2S)
);
assign DRAM_DQ = fes_dram_dq_oe ? fes_dram_dq_o : 16'bz;
assign {DRAM_UDQM, DRAM_LDQM} = fes_dram_dqm;
qsys_top soc_inst (
.fes_scanout_audio_sample(standard_audio_sample), .fes_scanout_audio_reset(standard_exec_reset),
.fes_scanout_machine_clk_clk(machine_clk),
.fes_scanout_sdram_clk(DRAM_CLK), .fes_scanout_sdram_cke(DRAM_CKE),
.fes_scanout_sdram_addr(DRAM_ADDR), .fes_scanout_sdram_ba(DRAM_BA),
.fes_scanout_sdram_cs_n(DRAM_CS_n), .fes_scanout_sdram_dqm(fes_dram_dqm),
.fes_scanout_sdram_ras_n(DRAM_RAS_n), .fes_scanout_sdram_cas_n(DRAM_CAS_n),
.fes_scanout_sdram_we_n(DRAM_WE_n), .fes_scanout_sdram_dq_i(DRAM_DQ),
.fes_scanout_sdram_dq_o(fes_dram_dq_o), .fes_scanout_sdram_dq_oe(fes_dram_dq_oe),"""
SHARED_PLL_COMMENT = """// One PLL on CLOCK1_50 in place of the stock audio PLL, whose pin can feed no
// second one: x 188/15, then /12 for the machine's 52.224 MHz and /51 for the
// 12.288 MHz audio master clock, both -34 ppm and in the DE10 cores' ratio.
"""


def top_after(spec):
    """The board top's machine PLL, audio and SDRAM pads for this machine.

    The shared PLL's top is TOP_AFTER exactly. A machine's own PLL replaces
    its comment, and its SDRAM clocks leave further PLL outputs for the
    component's SDRAM conduit.
    """
    if not hasattr(spec, "MACHINE_HZ"):
        return TOP_AFTER
    clocks(spec)
    top = replace_once(TOP_AFTER, SHARED_PLL_COMMENT, spec.PLL_COMMENT)
    if spec.SDRAM_CLOCKS:
        names = ", ".join("machine_sdram_" + role for role, _ in spec.SDRAM_CLOCKS)
        top = replace_once(top, "wire machine_clk, machine_audio_clk, machine_pll_locked;\n",
                           "wire machine_clk, machine_audio_clk, machine_pll_locked;\nwire " + names + ";\n")
        outputs = " ".join(f".outclk_{output}(machine_sdram_{role})," for role, output in spec.SDRAM_CLOCKS)
        top = replace_once(top, ".outclk_1(machine_audio_clk),\n",
                           ".outclk_1(machine_audio_clk),\n  " + outputs + "\n")
        top += "\n" + " ".join(f".fes_scanout_sdram_{role}(machine_sdram_{role}),"
                               for role, _ in spec.SDRAM_CLOCKS)
    return top


def prepare(spec, archive, output, revision=None, model=None, *, package_build_id=None, native_hps=False):
    """Select the standard machine in the managed GHRD; never deploy it.

    A producer passes the build identity of its record, which the machine then
    reports; without one the identity is a source profile, not a package seal.
    """
    model = model or next(iter(spec.MODELS))
    if model not in spec.MODELS:
        raise ValueError("unknown " + spec.NAME + " model")
    if native_hps and (spec.CORE_ID != "fes.spectrum" or model != "native48" or package_build_id is not None):
        raise ValueError("native HPS is a Spectrum 48K project-only recipe")
    sources = capture(spec, revision)
    machine_hz, pll_ip, _, sdram_clocks = clocks(spec)
    scanout = selected_scanout(sources[BOARD + "scanout/build.py"])
    files, derived, _ = component_files(spec, sources)
    if package_build_id is not None and (not revision or not isinstance(package_build_id, str) or
            not re.fullmatch(r"[0-9a-f]{32}", package_build_id) or package_build_id == "0" * 32):
        raise ValueError("machine package identity requires committed source and a nonzero 128-bit ID")
    build_id = package_build_id or sha(json.dumps(
        {"sources": {name: sha(data) for name, data in sorted(sources.items())},
         "model": model}, sort_keys=True).encode())[:32]
    scanout.capture = lambda revision=None: sources.copy()
    observed = identity() if revision is None else {}
    selected_revision = revision or observed.get("base_revision", observed.get("fes_revision"))
    output, combined, hps, display = scanout.prepare(archive, output, selected_revision,
        hps_first=True, linux_dma=not native_hps, package_build_id=None if native_hps else build_id)
    if native_hps:
        # The projection uses the diagnostic scanout's retained vendor HPS,
        # JTAG and probe. Adopt the project-only identity produced by it.
        build_id = json.loads((output / "combined/hps/inputs.json").read_text())["scanout"]["build_id"]
    directory = output / "combined/hps"
    project = directory / "project"
    local = project / "fes_mailbox"
    for name, data in files.items():
        path = local / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    (local / "machine-clocks.sdc").write_bytes(sources[spec.SDC])
    # Memory initialisation a machine's sources read by bare name.
    for name, data in spec.project_files(sources).items():
        (project / name).write_bytes(data)
    # The standalone reader component definition is unused; its implementation
    # remains selected by the machine component's fileset.
    (local / "fes_de25_scanout_hw.tcl").unlink()
    script_path = project / "integrate-mailbox.tcl"
    script = script_path.read_text()
    for before, after in (
        ("add_instance fes_scanout fes_de25_scanout 1.0", "add_instance fes_scanout " + spec.COMPONENT + " 1.0"),
        ("add_connection fes_pixel_clk.out_clk fes_scanout.clk", "add_connection fes_pixel_clk.out_clk fes_scanout.clk\n"
            "add_connection periph_clk.out_clk fes_scanout.clk_sys\n"
            "add_connection periph_rst_in.out_reset fes_scanout.system_reset\n"
            "add_connection pb_cpu_0.m0 fes_scanout.computer\n"
            f"set_connection_parameter_value pb_cpu_0.m0/fes_scanout.computer baseAddress {OFFSET:#x}\n"
            "add_instance fes_machine_clk altera_clock_bridge 19.2.0\n"
            f"set_instance_parameter_value fes_machine_clk EXPLICIT_CLOCK_RATE {machine_hz}\n"
            "add_interface fes_scanout_machine_clk clock sink\n"
            "set_interface_property fes_scanout_machine_clk EXPORT_OF fes_machine_clk.in_clk\n"
            "add_connection fes_machine_clk.out_clk fes_scanout.machine_clk"),
    ):
        script = replace_once(script, before, after)
    anchor = "add_interface fes_scanout_mem avalon start\n"
    exports = "".join(f"add_interface fes_scanout_{name} conduit end\n"
                      f"set_interface_property fes_scanout_{name} EXPORT_OF fes_scanout.{name}\n"
                      for name in ("audio", "sdram"))
    script = replace_once(script, anchor, exports + anchor)
    anchor = ("add_interface fes_scanout_video conduit end\n"
              "set_interface_property fes_scanout_video EXPORT_OF subsys_periph.fes_scanout_video\n")
    exports = "".join(f"add_interface fes_scanout_{name} conduit end\n"
                      f"set_interface_property fes_scanout_{name} EXPORT_OF subsys_periph.fes_scanout_{name}\n"
                      for name in ("audio", "sdram"))
    exports += ("add_instance fes_scanout_machine_clock altera_clock_bridge 19.2.0\n"
                f"set_instance_parameter_value fes_scanout_machine_clock EXPLICIT_CLOCK_RATE {machine_hz}\n"
                "add_connection fes_scanout_machine_clock.out_clk subsys_periph.fes_scanout_machine_clk\n"
                "add_interface fes_scanout_machine_clk clock sink\n"
                "set_interface_property fes_scanout_machine_clk EXPORT_OF fes_scanout_machine_clock.in_clk\n")
    script_path.write_text(replace_once(script, anchor, anchor + exports))
    top_path = project / "golden_top.v"
    top = top_path.read_text()
    for pin in ("HDMI_MCLK", "HDMI_SCLK", "HDMI_LRCLK", "HDMI_I2S"):
        top = replace_once(top, f".{pin}({pin})", f".{pin}()")
    top_path.write_text(replace_once(top, TOP_BEFORE, top_after(spec)))
    machine_ip = sources[pll_ip]
    machine_parameters = machine_pll(spec, display, machine_ip)
    (local / "machine_pll.ip").write_bytes(machine_ip)
    audio = [BOARD + "shared-rtl/fes_de25_machine_audio.v"] + [
        COMMON + "rtl/" + name for name in ("fes_audio_output.v", "fes_audio_i2s.v")]
    for name in audio:
        (local / Path(name).name).write_bytes(sources[name])
    qsf_path = project / "golden_top.qsf"
    # The PLL goes ahead of the constraints: they name the clocks its IP declares.
    qsf = replace_once(qsf_path.read_text(),
        "set_global_assignment -name SDC_FILE fes_mailbox/clocks.sdc\n",
        "set_global_assignment -name IP_FILE fes_mailbox/machine_pll.ip\n"
        "set_global_assignment -name SDC_FILE fes_mailbox/machine-clocks.sdc\n")
    qsf += "\n"
    if re.search(r"(?m)^\s*set_global_assignment\s+-name\s+VERILOG_MACRO\s+", qsf):
        raise ValueError("upstream already selects Verilog macros; review before adding the model")
    qsf += "set_global_assignment -name VERILOG_MACRO QUARTUS=1\n"
    if spec.MODELS[model]:
        qsf += f"set_global_assignment -name VERILOG_MACRO {spec.MODELS[model]}=1\n"
    for name in audio:
        qsf += "set_global_assignment -name VERILOG_FILE fes_mailbox/" + Path(name).name + "\n"
    # A machine's own synthesis assignments, each on a node under its component.
    for option, value, node in spec.ASSIGNMENTS:
        qsf += f'set_instance_assignment -name {option} {value} -to "{ENDPOINT}|{node}"\n'
    for pattern in (r"set_location_assignment\s+PIN_V16\s+-to\s+CLOCK1_50\s*$",
                    r"set_location_assignment\s+PIN_\w+\s+-to\s+DRAM_CLK\s*$",
                    r"set_location_assignment\s+PIN_\w+\s+-to\s+DRAM_DQ\[15\]\s*$"):
        if not re.search(pattern, qsf, re.M):
            raise ValueError("GHRD clock or SDRAM pin assignment is missing: " + pattern)
    qsf_path.write_text(qsf)
    inputs_path = directory / "inputs.json"
    inputs = json.loads(inputs_path.read_text())
    if revision is None:
        inputs.pop("fes_revision", None)
        inputs.update(base_revision=selected_revision, source_classification="working-copy-diagnostic")
    inputs[RECORD] = {
        "core": spec.CORE_ID, "build_id": build_id, "model": model, "expansion_socket": False,
        "computer_offset": OFFSET, "machine_clock_hz": machine_hz,
        "scope": "standard-core-board-integration", "runtime_admission": "not-qualified",
        "standard_sdc_sha256": sha((local / "machine-clocks.sdc").read_bytes()),
        "derivations": derived,
        "machine_pll": {"sha256": sha(machine_ip), "parameters": machine_parameters},
        "staged_source_sha256": ZX81.standard_staged_sources(project)}
    if sdram_clocks:
        inputs[RECORD]["sdram_clocks"] = {role: output for role, output in sdram_clocks}
    inputs["scanout"]["build_identity_scheme"] = (
        "canonical-standard-computer-v2" if package_build_id else "standard-board-integration-source-profile")
    inputs["scanout"]["compiled_top_sha256"] = sha(top_path.read_bytes())
    inputs["scanout"]["rendered_integration_sha256"] = sha(script_path.read_bytes())
    inputs_path.write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    snapshot_path = output / "snapshot.json"
    snapshot = json.loads(snapshot_path.read_text())
    if revision is None:
        snapshot.pop("fes_revision", None)
        snapshot.update(base_revision=selected_revision, source_classification="working-copy-diagnostic")
    snapshot[RECORD] = inputs[RECORD]
    snapshot_path.write_text(json.dumps(snapshot, indent=2, sort_keys=True) + "\n")
    return output, scanout, combined, hps, display


def verify(spec, output, scanout, hps, display):
    """Check the generated system against the selected machine integration."""
    import xml.etree.ElementTree as ET
    directory = output / "combined/hps"
    inputs = json.loads((directory / "inputs.json").read_text())
    project = directory / "project"
    selected = inputs[RECORD]
    if (inputs["scanout"]["build_id"] != selected["build_id"] or
            sha((project / "golden_top.v").read_bytes()) != inputs["scanout"]["compiled_top_sha256"] or
            sha((project / "integrate-mailbox.tcl").read_bytes()) != inputs["scanout"]["rendered_integration_sha256"] or
            ZX81.standard_staged_sources(project) != selected["staged_source_sha256"]):
        raise ValueError("machine compiled top, integration, identity or staged source changed")
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = {m.get("path"): m for m in tree.findall("module")}
    endpoint = modules.get("subsys_periph.fes_scanout")
    if endpoint is None or endpoint.get("kind") != spec.COMPONENT or endpoint.get("version") != "1.0":
        raise ValueError("generated machine component differs")
    build_identity = scanout.endpoint_build_identity(project, endpoint, hps, selected["build_id"])
    sdram = hps.component_ports(endpoint, "sdram")
    expected = {name: ("Output", width) for name, width in
                (("clk", 1), ("cke", 1), ("addr", 13), ("ba", 2), ("cs_n", 2), ("dqm", 2), ("ras_n", 1),
                 ("cas_n", 1), ("we_n", 1), ("dq_o", 16), ("dq_oe", 1))}
    expected["dq_i"] = ("Input", 16)
    expected.update({role: ("Input", 1) for role, _ in clocks(spec)[3]})
    if sdram != expected:
        raise ValueError("generated machine SDRAM pin geometry differs")
    connections = {(c.get("kind"), c.get("start"), c.get("end")): c for c in tree.findall("connection")}
    for kind, start, end, value in (
        ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_scanout.clk_sys", 100000000),
        ("clock", "subsys_periph_fes_pixel_clk.out_clk", "subsys_periph_fes_scanout.clk", 74250000),
        ("clock", "subsys_periph_fes_machine_clk.out_clk", "subsys_periph_fes_scanout.machine_clk", clocks(spec)[0]),
        ("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_scanout.computer", OFFSET),
    ):
        connection = connections.get((kind, start, end))
        parameter = "clockRateSysInfo" if kind == "clock" else "baseAddress"
        if connection is None or int(connection.findtext("parameter[@name='" + parameter + "']/value"), 0) != value:
            raise ValueError("generated machine route differs: " + end)
    for start, end in (
        ("subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_scanout.system_reset"),
        ("subsys_periph_fes_scanout_pixel_reset.out_reset", "subsys_periph_fes_scanout.reset"),
    ):
        if ("reset", start, end) not in connections:
            raise ValueError("generated machine reset differs: " + end)
    if (display.pll_parameters((project / "fes_mailbox/machine_pll.ip").read_bytes()) !=
            selected["machine_pll"]["parameters"]):
        raise ValueError("generation changed the machine PLL")
    return {"build_identity": build_identity, "standard_sdc_sha256": selected["standard_sdc_sha256"],
            "computer_offset": OFFSET, "managed_fabric": scanout.verify_managed_fabric(directory, hps),
            "reset_timing": scanout.verify_generated_reset_timing(directory),
            "shared_fifo_resets": shared_resets(spec, scanout, directory).verify(directory),
            "hdmi_inputs": scanout.verify_hdmi_inputs(project, inputs, hps),
            "scope": "whole-GHRD-IP-generation", "fit_and_timing": "not-run", "hardware_acceptance": "not-run"}


def generate(spec, output, scanout, combined, hps, display, tool, version):
    directory = output / "combined/hps"
    inputs = json.loads((directory / "inputs.json").read_text())
    if inputs.get("source_classification") != "committed" or not inputs.get("fes_revision"):
        raise ValueError("machine board generation requires committed source")
    combined.generate(output / "combined", hps, display, tool, version)
    scanout.repair_generated_reset_timing(directory)
    shared_resets(spec, scanout, directory).apply(directory)
    path = directory / "generation.json"
    generation = json.loads(path.read_text())
    generation[RECORD] = verify(spec, output, scanout, hps, display)
    path.write_text(json.dumps(generation, indent=2, sort_keys=True) + "\n")


def compile_board(spec, output, scanout, hps, display, tool):
    """Compile one checked design and retain its Linux phase artifacts."""
    directory = output / "combined/hps"
    expected = json.loads((directory / "generation.json").read_text())[RECORD]
    def check():
        if verify(spec, output, scanout, hps, display) != expected:
            raise ValueError("machine source/topology changed after generation")
    check()
    artifact = hps.compile_project(directory, tool)
    check()
    artifacts = hps.linux_configuration_artifacts(directory, tool, artifact, hps_initialization="HPS FIRST")
    record = {"format": 1, "classification": "unadmitted-HPS-first-standard-computer",
              "inputs_sha256": hps.digest(directory / "inputs.json"),
              "generation_sha256": hps.digest(directory / "generation.json"),
              "build_id": json.loads((directory / "inputs.json").read_text())[RECORD]["build_id"],
              **artifacts, "production_admission": False, "hardware_acceptance": False}
    (directory / "linux-programming.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    check()
    return artifact


def main(spec, argv=None):
    parser = argparse.ArgumentParser(description=spec.__doc__)
    parser.add_argument("action", choices=("project", "generate", "build"))
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--model", choices=tuple(spec.MODELS), default=next(iter(spec.MODELS)))
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        selected = identity()
        revision = selected.get("fes_revision") if args.action != "project" else None
        if args.action != "project" and not revision:
            raise ValueError("a board build requires clean committed source")
        output, scanout, combined, hps, display = prepare(spec, args.vendor_archive, args.output, revision, args.model)
        print("Standard " + spec.NAME + " board integration: " + str(output), flush=True)
        if args.action != "project":
            tool, version = hps.BOARD_BUILD.locate_quartus(args.quartus_root)
            generate(spec, output, scanout, combined, hps, display, tool, version)
            if args.action == "build":
                print(spec.NAME + " SOF: " + str(compile_board(spec, output, scanout, hps, display, tool)), flush=True)
            if identity() != selected:
                raise ValueError("source changed during the board build")
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-" + spec.DIRECTORY + ": " + str(error) + "\n")
