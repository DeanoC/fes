#!/usr/bin/env python3
"""Prepare a Spectrum 48K project with the native Tenfoot HPS envelope."""
from pathlib import Path
import argparse
import json
import sys
import xml.etree.ElementTree as ET

import build as spectrum


def matched_video_sdc(source):
    """Use the same ADV7513 Rev. B receiver minima as the native MENU shell."""
    replace_once = spectrum.board.replace_once
    source = replace_once(source,
        "# Analog Devices ADV7513 Hardware User's Guide Rev. 0, p. 12, AC Specifications:\n"
        "# setup tVSU >= 1.0 ns and hold tVHLD >= 0.7 ns. No board trace delay is\n"
        "# documented, so the pin budget assumes zero external flight time.",
        "# Analog Devices ADV7513 Rev. B datasheet, AC Specifications, p. 3:\n"
        "# setup tVSU >= 1.8 ns and hold tVHLD >= 1.3 ns. As in the matched native\n"
        "# MENU shell these are receiver-only limits at zero board skew.")
    source = replace_once(source, "-clock HDMI_TX_CLK -max 1.000", "-clock HDMI_TX_CLK -max 1.800")
    source = replace_once(source, "-clock HDMI_TX_CLK -min -0.700", "-clock HDMI_TX_CLK -min -1.300")
    return source


def project(archive, output):
    board = spectrum.board
    output, scanout, _, hps, _ = board.prepare(spectrum, archive, output, model="native48", native_hps=True)
    directory = output / "combined/hps"
    project = directory / "project"
    script_path = project / "integrate-mailbox.tcl"
    script = script_path.read_text()
    hps_system = hps.tcl_word(project / "hps_subsys/hps_subsys.qsys")
    jtag_system = hps.tcl_word(project / "jtag_subsys/jtag_subsys.qsys")
    top_system = hps.tcl_word(project / "qsys_top.qsys")
    # Preserve native H2F/OCM, F2SDRAM without a fabric SMMU SID, and the
    # separate probe. Disable the unused coherent F2H port as native MENU does.
    configure = f'''load_system {hps_system}
remove_interface fpga2hps
remove_interface fpga2hps_clk
remove_interface fpga2hps_rst
load_component agilex_hps
if {{[get_component_parameter_value f2s_data_width] != 256 ||
    [get_component_parameter_value H2F_Width] != 128 ||
    [get_component_parameter_value f2sdram_SMMU] != 0}} {{error "native Spectrum vendor HPS parameters changed"}}
set_component_parameter_value f2s_data_width 0
save_component
save_system {hps_system}
load_system {jtag_system}
remove_interface hps_m_master
remove_instance hps_m
save_system {jtag_system}

'''
    refresh = "# Refresh the changed child interface"
    script = board.replace_once(script, refresh, configure + refresh)
    # A constant active-low reset holds the remaining debug masters. Keep the
    # native independent DDR probe and route scanout separately.
    parent = '''remove_instance ace5lite_cache_coherency_translator_0
add_instance fes_native_maintenance_reset altera_reset_bridge 19.2.0
set_instance_parameter_value fes_native_maintenance_reset ACTIVE_LOW_RESET 1
set_instance_parameter_value fes_native_maintenance_reset SYNCHRONOUS_EDGES none
set_instance_parameter_value fes_native_maintenance_reset USE_RESET_REQUEST 0
add_interface fes_native_maintenance_reset reset sink
set_interface_property fes_native_maintenance_reset EXPORT_OF fes_native_maintenance_reset.in_reset
remove_connection rst_in.out_reset/subsys_debug.reset
add_connection fes_native_maintenance_reset.out_reset subsys_debug.reset
'''
    save_top = f"save_system {top_system}\n"
    script_path.write_text(board.replace_once(script, save_top, parent + save_top))
    top_path = project / "golden_top.v"
    top_path.write_text(board.replace_once(top_path.read_text(), "qsys_top soc_inst (",
        "qsys_top soc_inst (\n.fes_native_maintenance_reset_reset_n(1'b0),"))
    inputs_path = directory / "inputs.json"
    inputs = json.loads(inputs_path.read_text())
    video_constraints = project / "fes_mailbox/clocks.sdc"
    # The native topology retains separate FIFO controllers. Time the
    # same-clock reset receivers without the managed FIFO peer projection.
    video_constraints.write_text(scanout.timed_scanout_reset_ingress(
        matched_video_sdc(video_constraints.read_text())))
    video_constraints_sha = board.sha(video_constraints.read_bytes())
    inputs["scanout"]["source_sha256"]["clocks.sdc"] = video_constraints_sha
    inputs[board.RECORD]["staged_source_sha256"]["fes_mailbox/clocks.sdc"] = video_constraints_sha
    inputs["native_spectrum_hps"] = {
        "scope": "native-spectrum48-project-only",
        "hps_parameters": {"f2s_data_width": 0, "H2F_Width": 128, "f2sdram_SMMU": False},
        "reader_masters": ["fes_scanout", "fes_ddr_probe"],
        "maintenance_reset_tied": True,
        "matched_video_sdc_sha256": video_constraints_sha,
        "generated_handoff_verified": False,
        "firmware_admission": False,
        "composer_sha256": board.sha(Path(__file__).read_bytes()),
    }
    inputs["scanout"]["compiled_top_sha256"] = board.sha(top_path.read_bytes())
    inputs["scanout"]["rendered_integration_sha256"] = board.sha(script_path.read_bytes())
    inputs_path.write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return output, hps, scanout


def generate(output, hps, scanout, quartus_root):
    directory = output / "combined/hps"
    project = directory / "project"
    tool, version = hps.BOARD_BUILD.locate_quartus(quartus_root)
    qsys = tool.parents[2] / "qsys/bin/qsys-script"
    (directory / "quartus-version.txt").write_text(version + "\n")
    hps.run_logged([qsys, "--quartus-project=golden_top", "--search-path=custom_ip/**/*,fes_mailbox,$",
                    "--script=integrate-mailbox.tcl"], project, directory / "integration.log")
    generator = tool.with_name("quartus_ipgenerate")
    hps.run_logged([generator, "--clear_ip_generation_dirs", "golden_top"],
                   project, directory / "clear-generation.log")
    hps.run_logged([generator, "--generate_project_ip_files", "--synthesis=verilog", "golden_top"],
                   project, directory / "ip-generation.log")
    scanout.write_vendor_reset_timing(directory)
    verify_handoff(output, hps, scanout)


def spectrum_control_handoff(project, tree, hps, scanout, selected_build, probe):
    """Bind the elaborated lightweight bridge decode to the Spectrum build."""
    expected = {
        "subsys_periph.fes_scanout.computer": (0x10300, 0x20),
        "subsys_periph.fes_scanout_guard.avs": (0x10200, 0x100),
        "subsys_periph.fes_ddr_probe.avs": (0x10100, 0x80),
    }
    regions = [r for r in probe["peripheral_slave_map"] if r["name"] in expected]
    observed = {r["name"]: (r["base"], r["span"]) for r in regions}
    if len(regions) != len(expected) or observed != expected:
        raise ValueError("native Spectrum generated control address map differs")
    for slave, (offset, _) in expected.items():
        end = slave.replace(".", "_", 1)
        routes = [c for c in tree.findall("connection") if c.get("kind") == "avalon" and c.get("end") == end]
        if (len(routes) != 1 or routes[0].get("start") != "subsys_periph_pb_cpu_0.m0" or
                int(routes[0].findtext("parameter[@name='baseAddress']/value", "-1"), 0) != offset):
            raise ValueError("native Spectrum generated control bridge route differs: " + slave)
    expected_modules = {"subsys_periph.fes_scanout": spectrum.COMPONENT,
                        "subsys_periph.fes_scanout_guard": "fes_de25_scanout_guard"}
    modules = [m for m in tree.findall("module") if m.get("path") in expected_modules]
    kinds = {m.get("path"): (m.get("kind"), m.get("version")) for m in modules}
    if len(modules) != len(expected_modules) or kinds != {p: (k, "1.0") for p, k in expected_modules.items()}:
        raise ValueError("native Spectrum generated control components differ")
    endpoint = next(m for m in modules if m.get("path") == "subsys_periph.fes_scanout")
    build = scanout.endpoint_build_identity(project, endpoint, hps, selected_build)
    return {"endpoint": build, "regions": regions}


def verify_handoff(output, hps, scanout):
    directory = output / "combined/hps"
    project = directory / "project"
    inputs = json.loads((directory / "inputs.json").read_text())
    video_constraints = project / "fes_mailbox/clocks.sdc"
    video_constraints_sha = hps.digest(video_constraints)
    if video_constraints_sha != inputs["native_spectrum_hps"]["matched_video_sdc_sha256"]:
        raise ValueError("native Spectrum matched HDMI constraints changed")
    reset_timing = scanout.verify_vendor_reset_timing(directory)
    expected = inputs["vendor_hps_emif_parameters"]
    hps_ip = "hps_subsys/ip/hps_subsys/agilex_hps.ip"
    native = expected[hps_ip]
    if (native["f2s_data_width"], native["H2F_Width"], native["f2sdram_SMMU"]) != ("256", "128", "false"):
        raise ValueError("native Spectrum inherited HPS parameters changed")
    native["f2s_data_width"] = "0"
    defaults = ("ED_en_sim_gen", "FPGA_EMAC0_tx_clk_en", "FPGA_EMAC1_tx_clk_en",
                "FPGA_EMAC2_tx_clk_en", "FPGA_EMAC_PTP_clk_en")
    if any(key in native for key in defaults):
        raise ValueError("native Spectrum inherited compiler defaults changed")
    native.update(dict.fromkeys(defaults, "true"))
    for name, parameters in expected.items():
        if hps.module_parameters(project / name) != parameters:
            raise ValueError("native Spectrum generated HPS/EMIF parameters differ: " + name)
    vendor = json.loads((directory / "vendor-inputs.json").read_text())
    for name, digest in vendor["source_sha256"].items():
        if name.startswith("software/") and hps.digest(project / name) != digest:
            raise ValueError("native Spectrum generation changed vendor boot data: " + name)
    handoff = project / "qsys_top/qsys_top.sopcinfo"
    tree = ET.parse(handoff)
    routes = [(c.get("kind"), c.get("start"), c.get("end")) for c in tree.findall("connection")]
    expected_readers = {
        "ext_hps_f2sdram_master.expanded_master",
        "subsys_periph_fes_ddr_probe.avm", "subsys_periph_fes_mem_cdc.m0",
    }
    readers = [start for _, start, end in routes if end == "subsys_hps_f2sdram_adapter.axi4_sub"]
    if len(readers) != len(expected_readers) or set(readers) != expected_readers:
        raise ValueError("native Spectrum generated DDR master inventory differs")
    expected_reset = {
        "subsys_debug_jtag_rst_in.in_reset": "fes_native_maintenance_reset.out_reset",
        "subsys_debug_fpga_m.clk_reset": "subsys_debug_jtag_rst_in.out_reset",
        "subsys_debug_hps_f2sdram.clk_reset": "subsys_debug_jtag_rst_in.out_reset",
    }
    observed_reset = [(end, start) for kind, start, end in routes
                      if kind == "reset" and end.startswith("subsys_debug_")]
    if len(observed_reset) != len(expected_reset) or dict(observed_reset) != expected_reset:
        raise ValueError("native Spectrum maintenance reset route differs")
    # Check the synthesized wrapper still connects the constant source to
    # exactly the reset bridge that owns the maintenance subsystem.
    top = (project / "golden_top.v").read_text()
    netlist = (project / "qsys_top/synth/qsys_top.v").read_text()
    if (top.count(".fes_native_maintenance_reset_reset_n(1'b0)") != 1 or
            netlist.count("(fes_native_maintenance_reset_reset_n)") != 1 or
            netlist.count("(fes_native_maintenance_reset_out_reset_reset)") < 2):
        raise ValueError("native Spectrum maintenance reset binding differs")
    probe = hps.verify_probe_handoff(project)
    computer = spectrum_control_handoff(project, tree, hps, scanout,
                                        inputs[spectrum.board.RECORD]["build_id"], probe)
    result = {"scope": "native-spectrum48-generated-hps-diagnostic-v1",
              "hps_parameters": expected, "ddr_ingress": sorted(readers),
              "mailbox": hps.verify_mailbox_handoff(project),
              "probe": probe, "spectrum_control": computer,
              "handoff_sha256": hps.digest(handoff),
              "matched_video_sdc_sha256": video_constraints_sha, "reset_timing": reset_timing,
              "firmware_admission": False, "package_signoff": False}
    (directory / "native-spectrum-handoff.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vendor-archive", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--quartus-root", help="generate and check IP under the caller's shared Quartus lock")
    args = parser.parse_args(argv)
    try:
        output, hps, scanout = project(args.vendor_archive, args.output)
        if args.quartus_root:
            generate(output, hps, scanout, args.quartus_root)
        print(output)
    except (OSError, ValueError) as error:
        parser.exit(1, f"native Spectrum project: {error}\n")


if __name__ == "__main__":
    main(sys.argv[1:])
