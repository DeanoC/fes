#!/usr/bin/env python3
"""Prepare a Spectrum 48K project with the native Tenfoot HPS envelope."""
from pathlib import Path
import argparse
import json
import re
import sys
import xml.etree.ElementTree as ET

if not __package__:
    sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from boards.de25nano.spectrum import build as spectrum
from scripts import quartus_tools


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


def separate_scanout_reset(source):
    """Keep protocol enables and the physical reset on distinct registers."""
    replace_once = spectrum.board.replace_once
    source = replace_once(source,
        "    (* preserve, dont_merge *) reg first_lock_seen = 1'b0;",
        "    (* preserve, dont_merge *) reg first_lock_seen = 1'b0;\n"
        "    (* preserve, dont_merge *) reg scanout_release = 1'b0;")
    source = replace_once(source,
        "    assign scanout_reset_n = first_lock_seen;",
        "    // Mirror first-lock acceptance on the same edge. No protocol\n"
        "    // enable is driven from the register that asserts bridge reset.\n"
        "    always @(posedge system_clk or negedge top_reset_n)\n"
        "        if (!top_reset_n) scanout_release <= 1'b0;\n"
        "        else scanout_release <= first_lock_seen ||\n"
        "            (monitor_ready_sync[1] && system_ack == system_epoch_sync[1]);\n"
        "    assign scanout_reset_n = scanout_release;")
    return source


# The native HPS shell adds the probe alongside the same ten generated
# CDC/FIFO leaves as the managed shell. Pin its distinct generated parent.
NATIVE_FIFO_PARENT_SHA = "e67751d411819736ddc85738537268ca8720f279e6849cf7f39bd033d84e2d3d"


def native_shared_resets(directory, scanout):
    inputs = json.loads((directory / "inputs.json").read_text())
    path = directory / "project/fes_mailbox/shared_resets.py"
    if (inputs["native_spectrum_hps"]["scope"] != "native-spectrum48-project-only" or
            path.is_symlink() or scanout.sha(path.read_bytes()) != inputs["scanout"]["source_sha256"][path.name]):
        raise ValueError("native Spectrum shared FIFO reset helper differs from captured source")
    helper = scanout.load(path, "native_spectrum_shared_fifo_resets")
    if helper.POLICY != scanout.SHARED_FIFO_RESETS:
        raise ValueError("native Spectrum shared FIFO reset policy changed")
    helper.STANDARD_PARENT_SHA256 = NATIVE_FIFO_PARENT_SHA
    return helper


BOARD_JTAG_SHA256 = "02d78bf9bc14eea02092569a0639f6bd4f4b11e161e4fc6349c30128d640e17a"
BOARD_JTAG_CHECK = """
set fes_jtag_ports 0
foreach pin {altera_reserved_tck altera_reserved_tms altera_reserved_tdi altera_reserved_tdo} {
    incr fes_jtag_ports [get_collection_size [get_ports -nowarn $pin]]
}
if {$fes_jtag_ports == 4} {
    set_jtag_timing_constraints
} elseif {$fes_jtag_ports == 0 && ![get_collection_size [get_ports -nowarn {altera_reserved_ntrst}]]} {
    if {![string equal quartus_fit $::TimeQuestInfo(nameofexecutable)]} {
        derive_clock_uncertainty
    }
    post_message -type info "DE25 native board JTAG timing ports absent"
} else {
    error "DE25 native board JTAG timing ports partially present"
}
"""


def board_jtag_constraints(original):
    """Keep the complete vendor constraints whenever their physical endpoints exist."""
    board = spectrum.board
    if board.sha(original) != BOARD_JTAG_SHA256:
        raise ValueError("native Spectrum vendor board JTAG constraints changed")
    return board.replace_once(original.decode(), "\nset_jtag_timing_constraints\n", BOARD_JTAG_CHECK).encode()


JTAG_STREAM_SDC = "altera_avalon_st_jtag_interface.sdc"
JTAG_STREAM_SDC_SHA = "cac36d552c963acf54fa79b8cf14c211e99d449f45f5e87b095bb4ba1fa91e40"
JTAG_STREAM_EXCEPTION = (
    "set_false_path -from [get_registers *altera_jtag_src_crosser:*|sink_data_buffer*] "
    "-to [get_registers *altera_jtag_src_crosser:*|src_data*]"
)
JTAG_STREAM_CHECK = """set fes_jtag_sink [get_registers -nowarn {*altera_jtag_src_crosser:*|sink_data_buffer*}]
set fes_jtag_source [get_registers -nowarn {*altera_jtag_src_crosser:*|src_data*}]
set fes_jtag_sink_count [get_collection_size $fes_jtag_sink]
set fes_jtag_source_count [get_collection_size $fes_jtag_source]
if {$fes_jtag_sink_count == 0 && $fes_jtag_source_count == 0} {
    post_message -type info "DE25 native JTAG streaming crossing absent from the fitted design"
} elseif {$fes_jtag_sink_count == 0 || $fes_jtag_source_count == 0} {
    error "DE25 native JTAG streaming crossing is missing one side"
} else {
    set_false_path -from $fes_jtag_sink -to $fes_jtag_source
}
unset fes_jtag_sink fes_jtag_source fes_jtag_sink_count fes_jtag_source_count"""


def jtag_stream_constraints(directory, *, generate=False):
    """Preserve the vendor exception for every live JTAG streaming crossing."""
    project = directory / "project"
    paths = sorted(project.rglob(JTAG_STREAM_SDC))
    owners = {p.relative_to(project).parts[:4] for p in paths}
    expected = {("hps_subsys", "ip", "qsys_top", "emif_io96b_hps")}
    if (len(paths) != 1 or owners != expected or
            any(not p.is_file() or p.resolve() != p for p in paths)):
        raise ValueError("native Spectrum JTAG streaming SDC inventory differs")
    receipt = {}
    originals = directory / "jtag-stream-originals"
    for path in paths:
        relative = path.relative_to(project)
        original_path = originals / relative
        original = (path if generate else original_path).read_bytes()
        if spectrum.board.sha(original) != JTAG_STREAM_SDC_SHA:
            raise ValueError("native Spectrum generated JTAG streaming constraints changed")
        checked = spectrum.board.replace_once(original.decode(), JTAG_STREAM_EXCEPTION, JTAG_STREAM_CHECK).encode()
        if generate:
            original_path.parent.mkdir(parents=True, exist_ok=True)
            with original_path.open("xb") as stream:
                stream.write(original)
            path.write_bytes(checked)
        elif path.read_bytes() != checked:
            raise ValueError("native Spectrum JTAG streaming constraint derivation changed")
        receipt[str(relative)] = spectrum.board.sha(checked)
    return receipt


def project(archive, output, *, revision=None, selected_id=None, spectrum_spec=spectrum, model="native48"):
    board = spectrum_spec.board
    if selected_id is not None and (not revision or not re.fullmatch(r"[0-9a-f]{32}", selected_id) or
                                    selected_id == "0" * 32):
        raise ValueError("native Spectrum package projection requires a committed build identity")
    output, scanout, _, hps, _ = board.prepare(spectrum_spec, archive, output, revision,
                                               model=model, native_hps=True)
    directory = output / "combined/hps"
    project = directory / "project"
    script_path = project / "integrate-mailbox.tcl"
    script = script_path.read_text()
    inputs_path = directory / "inputs.json"
    inputs = json.loads(inputs_path.read_text())
    if selected_id is not None:
        base_id = inputs["scanout"]["build_id"]
        script = board.replace_once(script, f"set_instance_parameter_value fes_scanout BUILD_ID 0x{base_id}\n",
                                    f"set_instance_parameter_value fes_scanout BUILD_ID 0x{selected_id}\n")
        inputs["scanout"]["build_id"] = selected_id
        inputs["scanout"]["build_identity_scheme"] = "canonical-standard-computer-v2"
        inputs[board.RECORD]["build_id"] = selected_id
    hps_system = hps.tcl_word(project / "hps_subsys/hps_subsys.qsys")
    top_system = hps.tcl_word(project / "qsys_top.qsys")
    # Preserve the H2F configuration, F2SDRAM without a fabric SMMU SID,
    # and the separate probe. Disable the unused coherent F2H port as MENU does.
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

'''
    refresh = "# Refresh the changed child interface"
    script = board.replace_once(script, refresh, configure + refresh)
    # Managed debug JTAG masters are unused in this contained native role.
    # Removing their subsystem also removes the independent JTAG reset chains.
    parent = """remove_instance ace5lite_cache_coherency_translator_0
remove_instance subsys_debug
remove_instance ext_hps_f2sdram_master
"""
    # Native C64 needs the vendor OCM blocks for its disk and video buffers.
    # The native runtime uses the lightweight aperture and HPS DDR. Leave the
    # full HPS bridge configuration intact while removing its only slave.
    omit_ocm = model == "nativepal" and inputs[board.RECORD]["core"] == "fes.c64"
    if omit_ocm:
        parent += """foreach connection {subsys_hps.hps2fpga/ocm.axi_s1 clk_100.out_clk/ocm.clk1 rst_in.out_reset/ocm.reset1} {
    remove_connection $connection
}
remove_instance ocm
"""
    save_top = f"save_system {top_system}\n"
    script_path.write_text(board.replace_once(script, save_top, parent + save_top))
    qsf = project / "golden_top.qsf"
    assignments = qsf.read_text()
    unused_assignments = (
        ("QSYS_FILE", "jtag_subsys/jtag_subsys.qsys"),
        ("IP_FILE", "ip/qsys_top/ext_hps_f2sdram_master.ip"),
        *[("IP_FILE", f"jtag_subsys/ip/jtag_subsys/{name}.ip")
          for name in ("fpga_m", "hps_m", "hps_f2sdram", "jtag_clk", "jtag_rst_in")],
    )
    if omit_ocm:
        unused_assignments += (("IP_FILE", "ip/qsys_top/ocm.ip"),)
    for setting, relative in unused_assignments:
        assignments = board.replace_once(assignments, f"set_global_assignment -name {setting} {relative}\n", "")
    qsf.write_text(assignments)
    board_jtag = project / "jtag.sdc"
    original_jtag = board_jtag.read_bytes()
    (directory / "board-jtag-original.sdc").write_bytes(original_jtag)
    board_jtag.write_bytes(board_jtag_constraints(original_jtag))
    top_path = project / "golden_top.v"
    video_constraints = project / "fes_mailbox/clocks.sdc"
    video_constraints.write_text(scanout.timed_scanout_sdc(
        matched_video_sdc(video_constraints.read_text())))
    video_constraints_sha = board.sha(video_constraints.read_bytes())
    video_board = project / "fes_mailbox/fes_de25_scanout_board.v"
    video_board.write_text(separate_scanout_reset(video_board.read_text()))
    video_board_sha = board.sha(video_board.read_bytes())
    inputs["scanout"]["source_sha256"][video_board.name] = video_board_sha
    inputs[board.RECORD]["staged_source_sha256"]["fes_mailbox/" + video_board.name] = video_board_sha
    inputs["scanout"]["shared_fifo_resets"] = scanout.SHARED_FIFO_RESETS
    inputs["scanout"]["source_sha256"]["clocks.sdc"] = video_constraints_sha
    inputs[board.RECORD]["staged_source_sha256"]["fes_mailbox/clocks.sdc"] = video_constraints_sha
    inputs["native_spectrum_hps"] = {
        "scope": "native-spectrum48-project-only",
        "hps_parameters": {"f2s_data_width": 0, "H2F_Width": 128, "f2sdram_SMMU": False},
        "reader_masters": ["fes_scanout", "fes_ddr_probe"],
        "maintenance_masters": "absent",
        "board_jtag_sdc_sha256": board.sha(board_jtag.read_bytes()),
        "matched_video_sdc_sha256": video_constraints_sha,
        "scanout_board_sha256": video_board_sha,
        "generated_handoff_verified": False,
        "firmware_admission": False,
        "composer_sha256": board.sha(Path(__file__).read_bytes()),
    }
    inputs["scanout"]["compiled_top_sha256"] = board.sha(top_path.read_bytes())
    inputs["scanout"]["rendered_integration_sha256"] = board.sha(script_path.read_bytes())
    inputs_path.write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    snapshot_path = output / "snapshot.json"
    snapshot = json.loads(snapshot_path.read_text())
    snapshot[board.RECORD] = inputs[board.RECORD]
    snapshot["native_spectrum_hps"] = inputs["native_spectrum_hps"]
    snapshot_path.write_text(json.dumps(snapshot, indent=2, sort_keys=True) + "\n")
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
    scanout.write_vendor_reset_timing(directory, no_debug=True)
    native_shared_resets(directory, scanout).apply(directory)
    jtag_stream_constraints(directory, generate=True)
    verify_handoff(output, hps, scanout)


def spectrum_control_handoff(project, tree, hps, scanout, selected_build, probe, component=spectrum.COMPONENT):
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
    expected_modules = {"subsys_periph.fes_scanout": component,
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
    video_board = project / "fes_mailbox/fes_de25_scanout_board.v"
    video_board_sha = hps.digest(video_board)
    if video_board_sha != inputs["native_spectrum_hps"]["scanout_board_sha256"]:
        raise ValueError("native Spectrum scanout reset source changed")
    board_jtag = project / "jtag.sdc"
    board_jtag_sha = hps.digest(board_jtag)
    if (board_jtag.read_bytes() != board_jtag_constraints((directory / "board-jtag-original.sdc").read_bytes()) or
            board_jtag_sha != inputs["native_spectrum_hps"]["board_jtag_sdc_sha256"]):
        raise ValueError("native Spectrum board JTAG constraint derivation changed")
    reset_timing = scanout.verify_vendor_reset_timing(directory, no_debug=True)
    fifo_resets = native_shared_resets(directory, scanout).verify(directory)
    jtag_timing = jtag_stream_constraints(directory)
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
        "subsys_periph_fes_ddr_probe.avm", "subsys_periph_fes_mem_cdc.m0",
    }
    readers = [start for _, start, end in routes if end == "subsys_hps_f2sdram_adapter.axi4_sub"]
    if len(readers) != len(expected_readers) or set(readers) != expected_readers:
        raise ValueError("native Spectrum generated DDR master inventory differs")
    forbidden = ("subsys_debug", "ext_hps_f2sdram_master")
    if (any(module.get("path", "").startswith(forbidden) for module in tree.findall("module")) or
            any(start.startswith(forbidden) or end.startswith(forbidden) for _, start, end in routes)):
        raise ValueError("native Spectrum generated an unowned maintenance master")
    probe = hps.verify_probe_handoff(project, maintenance=False)
    selected = inputs[spectrum.board.RECORD]
    component = {
        ("fes.c64", "nativepal"): "fes_de25_c64_standard",
        ("fes.spectrum", "native48"): spectrum.COMPONENT,
    }.get((selected["core"], selected["model"]))
    if component is None:
        raise ValueError("unsupported native upload shell endpoint")
    if component == "fes_de25_c64_standard":
        if (any(module.get("path") == "ocm" for module in tree.findall("module")) or
                any(start.startswith("ocm.") or end.startswith("ocm.") for _, start, end in routes)):
            raise ValueError("native C64 retained vendor FPGA on-chip RAM")
    computer = spectrum_control_handoff(project, tree, hps, scanout, selected["build_id"], probe, component)
    result = {"scope": "native-spectrum48-generated-hps-diagnostic-v1",
              "hps_parameters": expected, "ddr_ingress": sorted(readers),
              "mailbox": hps.verify_mailbox_handoff(project),
              "probe": probe, "spectrum_control": computer,
              "handoff_sha256": hps.digest(handoff),
              "matched_video_sdc_sha256": video_constraints_sha, "reset_timing": reset_timing,
              "scanout_board_sha256": video_board_sha, "board_jtag_sdc_sha256": board_jtag_sha,
              "jtag_stream_sdc_sha256": jtag_timing,
              "shared_fifo_resets": fifo_resets,
              "firmware_admission": False, "package_signoff": False}
    if component == "fes_de25_c64_standard":
        result["vendor_fpga_ocm"] = "absent"
    (directory / "native-spectrum-handoff.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    return result


def compile_package_candidate(output, hps, scanout, tool):
    """Produce reviewed-chain inputs for the ordinary package gate."""
    directory = output / "combined/hps"
    before = verify_handoff(output, hps, scanout)
    artifact = hps.compile_project(directory, tool)
    if verify_handoff(output, hps, scanout) != before:
        raise ValueError("native Spectrum handoff changed during compile")
    exported = hps.linux_configuration_artifacts(directory, tool, artifact, hps_initialization="HPS FIRST")
    if verify_handoff(output, hps, scanout) != before:
        raise ValueError("native Spectrum handoff changed during phase export")
    (directory / "generation.json").write_text(json.dumps({
        "scope": "native-spectrum48-package-candidate-v1",
        "handoff_sha256": hps.digest(directory / "native-spectrum-handoff.json"),
        **exported,
    }, indent=2, sort_keys=True) + "\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--vendor-archive", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--quartus-root", help="generate and check IP under the caller's shared Quartus lock")
    args = parser.parse_args(argv)
    try:
        output, hps, scanout = project(args.vendor_archive, args.output)
        if args.quartus_root:
            with quartus_tools.quartus_lease("pro", label="native Spectrum IP generation"):
                generate(output, hps, scanout, args.quartus_root)
        print(output)
    except (OSError, ValueError) as error:
        parser.exit(1, f"native Spectrum project: {error}\n")


if __name__ == "__main__":
    main(sys.argv[1:])
