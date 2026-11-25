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
import sys
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
# Where the machine's memory lives: the board SDRAM unless its spec says
# BOARD_SDRAM = False, and a window in HPS DDR when it says WINDOW = True.
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


def board_sdram(spec):
    return getattr(spec, "BOARD_SDRAM", True)


def has_window(spec):
    return getattr(spec, "WINDOW", False)


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


def capture(spec, revision=None, *, native=False):
    """The managed scanout closure plus the complete machine, media and audio closure."""
    def read(path):
        return (ROOT / path).read_bytes() if revision is None else subprocess.check_output(
            ["git", "show", revision + ":" + path], cwd=ROOT)
    sources = selected_scanout(read(BOARD + "scanout/build.py")).capture(revision)
    paths = set(spec.SOURCES)
    if native:
        paths |= getattr(spec, "NATIVE_SOURCES", set())
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


def component_files(spec, sources, *, native=False):
    """Every file of the Qsys component, and the record of each derivation."""
    display = module(sources, BOARD + "display/build.py", "machine_selected_display")
    files, derived = spec.component_files(sources, native=True) if native else spec.component_files(sources)
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
TOP_WIRES = """wire [15:0] standard_audio_sample;
wire standard_exec_reset;
wire machine_clk, machine_audio_clk, machine_pll_locked;
"""
TOP_SDRAM_WIRES = """wire [15:0] fes_dram_dq_o;
wire [1:0] fes_dram_dqm;
wire fes_dram_dq_oe;
"""
SHARED_PLL_COMMENT = """// One PLL on CLOCK1_50 in place of the stock audio PLL, whose pin can feed no
// second one: x 188/15, then /12 for the machine's 52.224 MHz and /51 for the
// 12.288 MHz audio master clock, both -34 ppm and in the DE10 cores' ratio.
"""
TOP_PLL = SHARED_PLL_COMMENT + """machine_pll fes_machine_pll (
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
"""
TOP_SDRAM_PADS = """assign DRAM_DQ = fes_dram_dq_oe ? fes_dram_dq_o : 16'bz;
assign {DRAM_UDQM, DRAM_LDQM} = fes_dram_dqm;
"""
TOP_SOC = """qsys_top soc_inst (
.fes_scanout_audio_sample(standard_audio_sample), .fes_scanout_audio_reset(standard_exec_reset),
.fes_scanout_machine_clk_clk(machine_clk),"""
TOP_SDRAM_PORTS = """
.fes_scanout_sdram_clk(DRAM_CLK), .fes_scanout_sdram_cke(DRAM_CKE),
.fes_scanout_sdram_addr(DRAM_ADDR), .fes_scanout_sdram_ba(DRAM_BA),
.fes_scanout_sdram_cs_n(DRAM_CS_n), .fes_scanout_sdram_dqm(fes_dram_dqm),
.fes_scanout_sdram_ras_n(DRAM_RAS_n), .fes_scanout_sdram_cas_n(DRAM_CAS_n),
.fes_scanout_sdram_we_n(DRAM_WE_n), .fes_scanout_sdram_dq_i(DRAM_DQ),
.fes_scanout_sdram_dq_o(fes_dram_dq_o), .fes_scanout_sdram_dq_oe(fes_dram_dq_oe),"""
TOP_AFTER = TOP_WIRES + TOP_SDRAM_WIRES + TOP_PLL + TOP_SDRAM_PADS + TOP_SOC + TOP_SDRAM_PORTS


def top_after(spec):
    """The board top's machine PLL, audio and, for a machine with board SDRAM, its pads.

    The shared PLL's top is TOP_AFTER exactly. A machine's own PLL replaces
    its comment, and its SDRAM clocks leave further PLL outputs for the
    component's SDRAM conduit. A machine without board SDRAM has neither
    the pads nor the conduit.
    """
    sdram, own = board_sdram(spec), hasattr(spec, "MACHINE_HZ")
    sdram_clocks = clocks(spec)[3]
    if sdram and not own:
        return TOP_AFTER
    top = (TOP_WIRES + (TOP_SDRAM_WIRES if sdram else "") + TOP_PLL + (TOP_SDRAM_PADS if sdram else "") +
           TOP_SOC + (TOP_SDRAM_PORTS if sdram else ""))
    if own:
        top = replace_once(top, SHARED_PLL_COMMENT, spec.PLL_COMMENT)
    if sdram_clocks:
        if not sdram:
            raise ValueError(spec.NAME + " names SDRAM clocks without board SDRAM")
        names = ", ".join("machine_sdram_" + role for role, _ in sdram_clocks)
        top = replace_once(top, "wire machine_clk, machine_audio_clk, machine_pll_locked;\n",
                           "wire machine_clk, machine_audio_clk, machine_pll_locked;\nwire " + names + ";\n")
        outputs = " ".join(f".outclk_{output}(machine_sdram_{role})," for role, output in sdram_clocks)
        top = replace_once(top, ".outclk_1(machine_audio_clk),\n",
                           ".outclk_1(machine_audio_clk),\n  " + outputs + "\n")
        top += "\n" + " ".join(f".fes_scanout_sdram_{role}(machine_sdram_{role}),"
                               for role, _ in sdram_clocks)
    return top


# The pinned GHRD already exports the active-high HPS Reset Manager output
# through both subsystems. Use it as another asynchronous source of the common
# reset on HPS-window machines. Keep the vendor PLL and HPS configuration
# inputs on ninit_done alone. Reset the master and its FIFOs, the F2SDRAM
# adapter and the grant together so a retained bitstream starts ungranted.
WINDOW_RESET_SOURCE = (
    "wire hps_reset;\n"
    "// HPS reset also discards the window and queued fabric requests.\n"
    "wire fabric_reset_requested = ninit_done | hps_reset;\n"
)


def connect_window_reset(top):
    top = replace_once(top, "wire                   top_reset_n;\n",
                       "wire                   top_reset_n;\n" + WINDOW_RESET_SOURCE)
    top = replace_once(top, ".ninit_done (ninit_done)", ".ninit_done (fabric_reset_requested)")
    return replace_once(top, ".reset_reset_n                             (top_reset_n),",
                        ".reset_reset_n                             (top_reset_n),\n"
                        ".h2f_reset_reset                           (hps_reset),")


def window_offset(sources):
    """The window control block's peripheral offset (fes.fabric.de25-hps-window)."""
    text = sources[BOARD + "pc386/generated/fes_de25_hps_window.vh"].decode()
    values = re.findall(r"^`define FES_DE25_HPS_WINDOW_PERIPHERAL_OFFSET 32'h([0-9a-f]{8})$", text, re.M)
    if len(values) != 1:
        raise ValueError("missing generated window constant: PERIPHERAL_OFFSET")
    return int(values[0], 16)


# A window in HPS DDR: the machine's one read/write master, crossed into the
# 100 MHz F2SDRAM side by its own bridge, and the window control block on
# the lightweight bridge. The bridge returns write responses (a write is
# complete only when answered) and holds every read beat the arbiter can owe.
WINDOW_BRIDGE = {"DATA_WIDTH": 128, "SYMBOL_WIDTH": 8, "ADDRESS_WIDTH": 32, "USE_AUTO_ADDRESS_WIDTH": 0,
                 "MAX_BURST_SIZE": 128, "COMMAND_FIFO_DEPTH": 4, "RESPONSE_FIFO_DEPTH": 512,
                 "MASTER_SYNC_DEPTH": 2, "SLAVE_SYNC_DEPTH": 2, "ENABLE_RESPONSE": 1, "SYNC_RESET": 0,
                 "PIPELINE_ENABLE": 0}


def window_script(offset):
    values = " ".join(f"{name} {value}" for name, value in WINDOW_BRIDGE.items())
    return ("add_connection fes_scanout_system_reset.out_reset fes_scanout.machine_reset\n"
            "add_connection pb_cpu_0.m0 fes_scanout.window\n"
            f"set_connection_parameter_value pb_cpu_0.m0/fes_scanout.window baseAddress {offset:#x}\n"
            "add_instance fes_win_cdc mm_ccb 19.4.1\n"
            f"foreach {{name value}} [list {values} ADDRESS_UNITS SYMBOLS] {{\n"
            "    set_instance_parameter_value fes_win_cdc $name $value\n}\n"
            "add_connection fes_machine_clk.out_clk fes_win_cdc.s0_clk\n"
            "add_connection periph_clk.out_clk fes_win_cdc.m0_clk\n"
            "add_connection fes_scanout_system_reset.out_reset fes_win_cdc.s0_reset\n"
            "add_connection fes_scanout_system_reset.out_reset fes_win_cdc.m0_reset\n"
            "add_connection fes_scanout.win fes_win_cdc.s0\n"
            "set_connection_parameter_value fes_scanout.win/fes_win_cdc.s0 baseAddress 0\n"
            "add_interface fes_win_mem avalon start\n"
            "set_interface_property fes_win_mem EXPORT_OF fes_win_cdc.m0\n")


WINDOW_TOP = ("add_connection subsys_periph.fes_win_mem subsys_hps.f2sdram_adapter_axi4_sub\n"
              "set_connection_parameter_value subsys_periph.fes_win_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0\n")


def prepare(spec, archive, output, revision=None, model=None, *, package_build_id=None, native_hps=False):
    """Select the standard machine in the managed GHRD; never deploy it.

    A producer passes the build identity of its record, which the machine then
    reports; without one the identity is a source profile, not a package seal.
    """
    model = model or next(iter(spec.MODELS))
    if model not in spec.MODELS:
        raise ValueError("unknown " + spec.NAME + " model")
    native_c64 = (spec.CORE_ID, model) == ("fes.c64", "nativepal")
    if native_hps and ((spec.CORE_ID, model) != ("fes.spectrum", "native48") and not native_c64 or package_build_id is not None):
        raise ValueError("native HPS is a Spectrum 48K project-only recipe")
    if native_c64 and not native_hps:
        raise ValueError("native C64 requires the contained HPS shell composer")
    sources = capture(spec, revision, native=native_c64)
    machine_hz, pll_ip, _, sdram_clocks = clocks(spec)
    scanout = selected_scanout(sources[BOARD + "scanout/build.py"])
    files, derived, _ = component_files(spec, sources, native=native_c64)
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
        hps_first=True, linux_dma=not native_hps, package_build_id=None if native_hps else build_id,
        window=has_window(spec))
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
    conduits = ("audio", "sdram") if board_sdram(spec) else ("audio",)
    anchor = "add_interface fes_scanout_mem avalon start\n"
    exports = "".join(f"add_interface fes_scanout_{name} conduit end\n"
                      f"set_interface_property fes_scanout_{name} EXPORT_OF fes_scanout.{name}\n"
                      for name in conduits)
    if has_window(spec):
        exports += window_script(window_offset(sources))
    script = replace_once(script, anchor, exports + anchor)
    anchor = ("add_interface fes_scanout_video conduit end\n"
              "set_interface_property fes_scanout_video EXPORT_OF subsys_periph.fes_scanout_video\n")
    exports = "".join(f"add_interface fes_scanout_{name} conduit end\n"
                      f"set_interface_property fes_scanout_{name} EXPORT_OF subsys_periph.fes_scanout_{name}\n"
                      for name in conduits)
    if has_window(spec):
        exports += WINDOW_TOP
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
    if has_window(spec):
        top = connect_window_reset(top)
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
    for pattern in ((r"set_location_assignment\s+PIN_V16\s+-to\s+CLOCK1_50\s*$",) +
                    ((r"set_location_assignment\s+PIN_\w+\s+-to\s+DRAM_CLK\s*$",
                      r"set_location_assignment\s+PIN_\w+\s+-to\s+DRAM_DQ\[15\]\s*$") if board_sdram(spec) else ())):
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
    if has_window(spec):
        inputs[RECORD]["window"] = {"contract": "fes.fabric.de25-hps-window 1.0",
                                    "control_offset": window_offset(sources), "bridge": dict(WINDOW_BRIDGE)}
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


def window_routes(offset, machine_hz):
    return (("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_scanout.window", offset),
            ("avalon", "subsys_periph_fes_scanout.win", "subsys_periph_fes_win_cdc.s0", 0),
            ("clock", "subsys_periph_fes_machine_clk.out_clk", "subsys_periph_fes_win_cdc.s0_clk", machine_hz),
            ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_win_cdc.m0_clk", 100000000))


def verify_window_reset(project, modules, connections):
    for module in ("subsys_hps", "subsys_hps.agilex_hps"):
        interfaces = modules[module].findall("interface[@name='h2f_reset'][@kind='reset_source']")
        if len(interfaces) != 1 or [
            (port.findtext("role"), port.findtext("direction"), port.findtext("width"))
            for port in interfaces[0].findall("port")
        ] != [("reset", "Output", "1")]:
            raise ValueError("generated HPS reset output differs: " + module)
    generated = (project / "qsys_top/synth/qsys_top.v").read_text()
    if len(re.findall(r"\.h2f_reset_reset\s*\(h2f_reset_reset\)", generated)) != 1:
        raise ValueError("generated parent does not pass the HPS reset through")
    top = (project / "golden_top.v").read_text()
    if any(top.count(fragment) != 1 for fragment in (
        WINDOW_RESET_SOURCE, ".h2f_reset_reset                           (hps_reset),",
        ".ninit_done (fabric_reset_requested)", ".reset_reset_n                             (top_reset_n),",
        ".fes_scanout_reset_reset_n (scanout_reset_n)",
        ".system_clk(system_clk_100_internal), .top_reset_n(top_reset_n)",
        ".pixel_clk(scanout_pixel_clk), .scanout_reset_n(scanout_reset_n)",
    )):
        raise ValueError("window reset is disconnected from the generated board top")
    scanout = (project / "fes_mailbox/fes_de25_scanout_board.v").read_text()
    if ("always @(posedge system_clk or negedge top_reset_n)" not in scanout or
            "assign scanout_reset_n = first_lock_seen;" not in scanout):
        raise ValueError("window FIFO reset no longer follows the common fabric reset")
    for start, end in (
        ("rst_in.out_reset", "subsys_periph_periph_rst_in.in_reset"),
        ("rst_in.out_reset", "subsys_hps_f2sdram_adapter.reset"),
        ("rst_in.out_reset", "subsys_hps_agilex_hps.f2sdram_axi_reset"),
        ("fes_scanout_top_reset.out_reset", "subsys_periph_fes_scanout_system_reset.in_reset"),
        ("subsys_periph_fes_scanout_system_reset.out_reset", "subsys_periph_fes_scanout.machine_reset"),
    ):
        if ("reset", start, end) not in connections:
            raise ValueError("generated window reset path differs: " + end)
    return "HPS Reset Manager output into common fabric reset"


def verify_window(project, modules, endpoint, connections, hps, scanout):
    """The window's master, control slave and bridge, as window_script made them."""
    master = {"address": ("Output", 32), "burstcount": ("Output", 8), "read": ("Output", 1), "write": ("Output", 1),
              "writedata": ("Output", 128), "byteenable": ("Output", 16), "waitrequest": ("Input", 1),
              "readdata": ("Input", 128), "readdatavalid": ("Input", 1), "writeresponsevalid": ("Input", 1),
              "response": ("Input", 2)}
    if hps.component_ports(endpoint, "win") != master:
        raise ValueError("generated window master differs")
    control = hps.component_ports(endpoint, "window")
    if (control.get("address") != ("Input", 6) or control.get("readdata") != ("Output", 32) or
            control.get("writeresponsevalid") != ("Output", 1) or control.get("response") != ("Output", 2)):
        raise ValueError("generated window control slave differs")
    bridge = modules.get("subsys_periph.fes_win_cdc")
    if bridge is None or bridge.get("kind") != "mm_ccb" or bridge.get("version") != "19.4.1":
        raise ValueError("generated window bridge differs")
    actual, source = scanout.referenced_parameters(project, bridge, hps)
    checked = scanout.require_parameters(actual, {**WINDOW_BRIDGE, "ADDRESS_UNITS": "SYMBOLS"}, "fes_win_cdc")
    for start, end in (("subsys_periph_fes_scanout_system_reset.out_reset", "subsys_periph_fes_win_cdc.s0_reset"),
                       ("subsys_periph_fes_scanout_system_reset.out_reset", "subsys_periph_fes_win_cdc.m0_reset")):
        if ("reset", start, end) not in connections:
            raise ValueError("generated window bridge reset differs: " + end)
    # As generation.json keeps it, so the check before compiling compares like with like.
    return {"master_ports": {role: list(port) for role, port in master.items()},
            "bridge": {"parameters": checked, "logical_view": source},
            "reset": verify_window_reset(project, modules, connections)}


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
    if board_sdram(spec):
        sdram = hps.component_ports(endpoint, "sdram")
        expected = {name: ("Output", width) for name, width in
                    (("clk", 1), ("cke", 1), ("addr", 13), ("ba", 2), ("cs_n", 2), ("dqm", 2), ("ras_n", 1),
                     ("cas_n", 1), ("we_n", 1), ("dq_o", 16), ("dq_oe", 1))}
        expected["dq_i"] = ("Input", 16)
        expected.update({role: ("Input", 1) for role, _ in clocks(spec)[3]})
        if sdram != expected:
            raise ValueError("generated machine SDRAM pin geometry differs")
    else:
        definition = ET.fromstring(endpoint.findtext("parameter[@name='componentDefinition']/value") or "<x/>")
        if any(item.findtext("name") == "sdram" for item in definition.findall("boundary/interfaces/interface")):
            raise ValueError("generated machine has SDRAM pins it does not declare")
    connections = {(c.get("kind"), c.get("start"), c.get("end")): c for c in tree.findall("connection")}
    for kind, start, end, value in (
        ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_scanout.clk_sys", 100000000),
        ("clock", "subsys_periph_fes_pixel_clk.out_clk", "subsys_periph_fes_scanout.clk", 74250000),
        ("clock", "subsys_periph_fes_machine_clk.out_clk", "subsys_periph_fes_scanout.machine_clk", clocks(spec)[0]),
        ("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_scanout.computer", OFFSET),
    ) + (window_routes(selected["window"]["control_offset"], clocks(spec)[0]) if has_window(spec) else ()):
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
    window = verify_window(project, modules, endpoint, connections, hps, scanout) if has_window(spec) else None
    return {"build_identity": build_identity, "standard_sdc_sha256": selected["standard_sdc_sha256"],
            **({"window": window} if window is not None else {}),
            "computer_offset": OFFSET, "managed_fabric": scanout.verify_managed_fabric(directory, hps),
            "reset_timing": scanout.verify_generated_reset_timing(directory),
            "shared_fifo_resets": shared_resets(spec, scanout, directory).verify(directory),
            "hdmi_inputs": scanout.verify_hdmi_inputs(project, inputs, hps),
            "scope": "whole-GHRD-IP-generation", "fit_and_timing": "not-run", "hardware_acceptance": "not-run"}


def quartus_lease():
    """This host's Quartus Pro lease (scripts/quartus_tools.quartus_lease).

    A board's generation, fit and programming-file steps share the compile
    host's memory with every other Quartus job, so they take the same lease
    the i386 fits and the x87 envelope take. It waits; it never interrupts
    another owner's job.
    """
    root = str(ROOT / "sources/misteross")
    if root not in sys.path:
        sys.path.insert(0, root)
    from scripts.quartus_tools import quartus_lease as lease
    return lease("pro", label="DE25 machine board")


def generate(spec, output, scanout, combined, hps, display, tool, version):
    directory = output / "combined/hps"
    inputs = json.loads((directory / "inputs.json").read_text())
    if inputs.get("source_classification") != "committed" or not inputs.get("fes_revision"):
        raise ValueError("machine board generation requires committed source")
    with quartus_lease():
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
    with quartus_lease():
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
