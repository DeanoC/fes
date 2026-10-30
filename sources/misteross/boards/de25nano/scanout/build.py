#!/usr/bin/env python3
"""Build the disabled-by-default DE25 allocated-buffer scanout diagnostic."""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
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
LOCAL = ("build.py", "integrate.tcl", "clocks.sdc", "fes_de25_scanout.v",
         "fes_de25_scanout_hw.tcl", "fes_de25_scanout_board.v",
         "generated/fes_de25_scanout.vh", "generated/fes_de25_scanout.h")


def sha(data): return hashlib.sha256(data).hexdigest()


def identity():
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=ROOT, text=True)
    return {"base_revision" if dirty else "fes_revision": revision,
            "source_classification": "working-copy-diagnostic" if dirty else "committed"}


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def capture(revision=None):
    def read(path):
        return (ROOT / path).read_bytes() if revision is None else subprocess.check_output(
            ["git", "show", revision + ":" + path], cwd=ROOT)
    path = BOARD + "combined/build.py"
    # Execute only the selected helper bytes to obtain its declared closure;
    # its imports have no project/tool actions. Later helpers run retained files.
    source = read(path)
    helper = types.ModuleType("scanout_selected_combined")
    helper.__file__ = str(ROOT / path)
    exec(compile(source, helper.__file__, "exec"), helper.__dict__)
    sources = helper.capture(revision)
    if sources[path] != source:
        raise ValueError("combined producer changed during capture")
    sources.update({BOARD + "scanout/" + name: read(BOARD + "scanout/" + name) for name in LOCAL})
    return sources


def replace_once(source, before, after):
    if source.count(before) != 1:
        raise ValueError("scanout preparation target changed: " + before[:100])
    return source.replace(before, after)


def prepare(archive, output=None, revision=None):
    sources = capture(revision)
    selected = {"fes_revision": revision, "source_classification": "committed"} if revision else identity()
    if revision is None:
        selected = {"base_revision": selected.get("fes_revision", selected.get("base_revision")),
                    "source_classification": "working-copy-diagnostic"}
    if output is None:
        base = ROOT / "sources/misteross/build/diagnostic/de25nano-scanout"
        base.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix="run-", dir=base))
    else:
        output = Path(output).resolve(); output.mkdir(parents=True, exist_ok=False)
    snapshot = output / "functional-source"
    for name, data in sources.items():
        target = snapshot / name; target.parent.mkdir(parents=True, exist_ok=True); target.write_bytes(data)
    combined = load(snapshot / BOARD / "combined/build.py", "scanout_retained_combined")
    combined.identity = lambda: selected.copy()
    upstream, hps, display = combined.prepare(archive, "menu-pattern", output / "combined")
    directory = upstream / "hps"; project = directory / "project"
    original_inputs = (directory / "inputs.json").read_bytes()
    (output / "upstream-preparation-inputs.json").write_bytes(original_inputs)
    local = project / "fes_mailbox"
    staged_names = {name: "scanout_" + name if name in ("build.py", "integrate.tcl") else name for name in LOCAL}
    for name in LOCAL:
        target = local / staged_names[name]; target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(sources[BOARD + "scanout/" + name])
    # The endpoint's Qsys fileset owns the actual generic reader/video RTL.
    for name in ("fes_menu_reader_de25.v", "fes_menu_video.v"):
        (local / name).write_bytes((project / "fes_combined" / name).read_bytes())
    for name in ("video.v", "fes_menu_reader_de25.v", "fes_menu_video.v", "fes_menu_pattern_memory.v"):
        (project / "fes_combined" / name).unlink()
    qsf = (project / "golden_top.qsf").read_text()
    for name in ("video.v", "fes_menu_reader_de25.v", "fes_menu_video.v", "fes_menu_pattern_memory.v"):
        qsf = replace_once(qsf, "set_global_assignment -name VERILOG_FILE fes_combined/" + name + "\n", "")
    qsf = replace_once(qsf, "set_global_assignment -name SDC_FILE fes_combined/clocks.sdc\n",
                       "set_global_assignment -name SDC_FILE fes_mailbox/clocks.sdc\n")
    (project / "fes_combined/clocks.sdc").unlink()
    qsf += "set_global_assignment -name VERILOG_FILE fes_mailbox/fes_de25_scanout_board.v\n"
    (project / "golden_top.qsf").write_text(qsf)
    top = (project / "golden_top.v").read_text()
    top = replace_once(top, "fes_de25_combined_video #(.MENU_PATTERN(1'b1)) fes_video_inst (",
        "fes_de25_scanout_board fes_video_inst (\n"
        "  .system_clk(system_clk_100_internal), .top_reset_n(top_reset_n),\n"
        "  .pixel_clk(scanout_pixel_clk), .scanout_reset_n(scanout_reset_n), .clock_fault(scanout_clock_fault),\n"
        "  .video_rgb(scanout_rgb), .video_de(scanout_de), .video_hs(scanout_hs), .video_vs(scanout_vs),")
    top = replace_once(top, "qsys_top soc_inst (", "wire scanout_pixel_clk, scanout_reset_n, scanout_clock_fault;\n"
        "wire [23:0] scanout_rgb;\nwire scanout_de, scanout_hs, scanout_vs;\nqsys_top soc_inst (\n"
        ".fes_scanout_pixel_clk_clk (scanout_pixel_clk),\n"
        ".fes_scanout_reset_reset_n (scanout_reset_n),\n"
        ".fes_scanout_clock_fault_export (scanout_clock_fault),\n"
        ".fes_scanout_video_rgb (scanout_rgb), .fes_scanout_video_de (scanout_de),\n"
        ".fes_scanout_video_hs (scanout_hs), .fes_scanout_video_vs (scanout_vs),")
    (project / "golden_top.v").write_text(top)
    definition = sources[BOARD + "scanout/generated/fes_de25_scanout.vh"].decode()
    offset = re.search(r"^`define FES_DE25_SCANOUT_PERIPHERAL_OFFSET 32'h([0-9a-f]{8})$", definition, re.M)
    if not offset: raise ValueError("missing generated scanout register offset")
    addition = sources[BOARD + "scanout/integrate.tcl"].decode().replace("@SCANOUT_BASE@", hex(int(offset[1], 16)))
    script = (project / "integrate-mailbox.tcl").read_text()
    script = replace_once(script, "save_system $peripheral_system\n\n# Refresh", addition + "\nsave_system $peripheral_system\n\n# Refresh")
    anchor = "set_connection_parameter_value subsys_periph.fes_probe_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0\n"
    script = replace_once(script, anchor, anchor + "add_connection subsys_periph.fes_scanout_mem subsys_hps.f2sdram_adapter_axi4_sub\n"
        "set_connection_parameter_value subsys_periph.fes_scanout_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0\n")
    # A real top-level reset source is required by the inserted memory
    # interconnect. Merely exporting the child sink leaves its adapters without
    # a source identity. Keep the same external common cold/first-lock signal.
    before = "save_system " + hps.tcl_word(project / "qsys_top.qsys") + "\n"
    exports = "add_instance fes_scanout_top_reset altera_reset_bridge 19.2.0\n"
    for name, value in (("ACTIVE_LOW_RESET", "1"), ("SYNCHRONOUS_EDGES", "none"),
                        ("O_SYNCHRONOUS_EDGES", "none"), ("NUM_RESET_OUTPUTS", "1"), ("USE_RESET_REQUEST", "0")):
        exports += "set_instance_parameter_value fes_scanout_top_reset " + name + " " + value + "\n"
    exports += "add_connection fes_scanout_top_reset.out_reset subsys_periph.fes_scanout_pixel_reset\n"
    exports += "add_connection fes_scanout_top_reset.out_reset subsys_periph.fes_scanout_system_reset\n"
    exports += "add_interface fes_scanout_reset reset sink\nset_interface_property fes_scanout_reset EXPORT_OF fes_scanout_top_reset.in_reset\n"
    for name, kind in (("fes_scanout_pixel_clk", "clock sink"),
                       ("fes_scanout_video", "conduit end"), ("fes_scanout_clock_fault", "conduit end")):
        exports += "add_interface " + name + " " + kind + "\nset_interface_property " + name + " EXPORT_OF subsys_periph." + name + "\n"
    script = replace_once(script, before, exports + before)
    (project / "integrate-mailbox.tcl").write_text(script)
    inputs = json.loads(original_inputs)
    inputs.pop("base_revision", None); inputs.update(selected)
    inputs["functional_snapshot_sha256"] = {name: sha(data) for name, data in sources.items()}
    inputs["combined_display"] = {name: value for name, value in inputs["combined_display"].items()
        if name in ("vendor_source_sha256", "reviewed_pll_device_repair", "reviewed_menu_ram_hint_repair",
                    "pixel_pll_parameters", "pixel_clock_hz", "i2c_internal_clock", "configuration_release",
                    "warm_reset_ack_n", "debug_paths")}
    inputs["combined_display"].update(mode="allocated-buffer-scanout", ddr_scanout=True, runtime_menu=False)
    inputs["scanout"] = {"peripheral_offset": int(offset[1], 16), "default_enabled": False,
        "pixel_hz": 74250000, "system_hz": 100000000, "control_bridge": "mm_ccb19.4.1 DATA32 RESPONSE1",
        "memory_bridge": "mm_ccb19.4.1 DATA128 RESPONSE1 MAX16", "allocation_bytes": 8388608,
        "reset_release": {"assertion": "asynchronous",
            "deassertion": {"pixel": "synchronous to 74250000 Hz", "peripheral": "synchronous to 100000000 Hz"}},
        "source_sha256": {staged_names[name]: hps.digest(local / staged_names[name]) for name in LOCAL} |
                         {name: hps.digest(local / name) for name in ("fes_menu_reader_de25.v", "fes_menu_video.v")},
        "compiled_top_sha256": hps.digest(project / "golden_top.v"), "rendered_integration_sha256": hps.digest(project / "integrate-mailbox.tcl"),
        "clock_loss": "sticky fault/blank/drain when pixel resumes; never reset live bridges",
        "global_reset_ownership": "not-qualified; active-low warm acknowledgement remains inactive1",
        "dma_admission": "not-qualified; SID/firewall/firmware/physical lifetime gates remain"}
    (directory / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    (output / "snapshot.json").write_text(json.dumps({**selected, "source_sha256": inputs["functional_snapshot_sha256"],
        "hps_output": "combined/hps", "upstream_preparation_inputs_sha256": sha(original_inputs)}, indent=2, sort_keys=True) + "\n")
    return output, combined, hps, display


def referenced_parameters(project, module, hps):
    # Generic-component SOPCINFO points to the real IP-XACT logical view; it
    # does not duplicate the IP's module parameters as SOPCINFO parameters.
    view = module.findtext("parameter[@name='logicalView']/value")
    if not view or Path(view).is_absolute() or ".." in Path(view).parts or Path(view).suffix != ".ip":
        raise ValueError("invalid generated component logical view")
    matches = [path for path in project.rglob(Path(view).name) if path.is_file() and not path.is_symlink() and
               (path.relative_to(project).as_posix() == view or path.relative_to(project).as_posix().endswith("/" + view))]
    if len(matches) != 1:
        raise ValueError("generated logical view is missing or ambiguous: " + view)
    return hps.module_parameters(matches[0]), {"path": str(matches[0].relative_to(project)), "sha256": hps.digest(matches[0])}


def require_parameters(actual, expected, name):
    normalized = {key: "1" if value == "true" else "0" if value == "false" else value for key, value in actual.items()}
    if any(normalized.get(key) != str(value) for key, value in expected.items()):
        raise ValueError("generated component parameters differ: " + name + " " + str({key: actual.get(key) for key in expected}))
    return {key: actual[key] for key in expected}


def verify_reset_bridge(project, module, hps, synchronous_edges="NONE"):
    if module is None or module.get("kind") != "altera_reset_bridge" or module.get("version") != "19.2.0":
        raise ValueError("generated scanout reset bridge version/kind differs")
    actual, source = referenced_parameters(project, module, hps)
    checked = require_parameters(actual, {"ACTIVE_LOW_RESET": 1,
        "SYNCHRONOUS_EDGES": synchronous_edges.lower(),
        "NUM_RESET_OUTPUTS": 1, "USE_RESET_REQUEST": 0}, module.get("path"))
    definition = ET.fromstring(module.findtext("parameter[@name='componentDefinition']/value"))
    for name, direction in (("in_reset", "Input"), ("out_reset", "Output")):
        points = [item for item in definition.findall("boundary/interfaces/interface") if item.findtext("name") == name]
        if len(points) != 1 or hps.component_ports(module, name) != {"reset_n": (direction, 1)}:
            raise ValueError("generated scanout reset bridge polarity differs")
        edges = points[0].findtext("parameters/parameterValueMap/entry[key='synchronousEdges']/value")
        if edges != synchronous_edges:
            raise ValueError("generated scanout reset bridge synchronization differs")
    # O_SYNCHRONOUS_EDGES is DERIVED (not stored in .ip). Verify the actual
    # elaborated output interface rather than substituting a guessed default.
    checked["O_SYNCHRONOUS_EDGES"] = synchronous_edges.lower() + \
        " (elaborated output synchronousEdges=" + synchronous_edges + ")"
    return {"parameters": checked, "logical_view": source}


def verify_handoff(project, hps):
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = {m.get("path"): m for m in tree.findall("module")}
    endpoint = modules.get("subsys_periph.fes_scanout")
    if endpoint is None or endpoint.get("kind") != "fes_de25_scanout" or endpoint.get("version") != "1.0":
        raise ValueError("generated scanout endpoint differs")
    expected_master = {"address": ("Output", 32), "burstcount": ("Output", 5), "read": ("Output", 1),
        "byteenable": ("Output", 16), "waitrequest": ("Input", 1), "readdata": ("Input", 128),
        "readdatavalid": ("Input", 1), "response": ("Input", 2)}
    if hps.component_ports(endpoint, "avm") != expected_master:
        raise ValueError("generated scanout read-only response-aware master differs")
    control_ports = hps.component_ports(endpoint, "avs")
    if (control_ports.get("address") != ("Input", 7) or control_ports.get("readdata") != ("Output", 32) or
            control_ports.get("writeresponsevalid") != ("Output", 1) or control_ports.get("response") != ("Output", 2)):
        raise ValueError("generated scanout CSR completion/width differs")
    connections = {(c.get("kind"), c.get("start"), c.get("end")): c for c in tree.findall("connection")}
    routes = (("subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_ctrl_cdc.s0"),
              ("subsys_periph_fes_ctrl_cdc.m0", "subsys_periph_fes_scanout.avs"),
              ("subsys_periph_fes_scanout.avm", "subsys_periph_fes_mem_cdc.s0"),
              ("subsys_periph_fes_mem_cdc.m0", "subsys_hps_f2sdram_adapter.axi4_sub"))
    definition = (project / "fes_mailbox/generated/fes_de25_scanout.vh").read_text()
    offset = int(re.search(r"^`define FES_DE25_SCANOUT_PERIPHERAL_OFFSET 32'h([0-9a-f]{8})$", definition, re.M)[1], 16)
    for index, (start, end) in enumerate(routes):
        item = connections.get(("avalon", start, end))
        if item is None or int(item.findtext("parameter[@name='baseAddress']/value"), 0) != (offset if index == 0 else 0):
            raise ValueError("generated scanout control/memory route differs: " + start)
    clocks = (("subsys_periph_periph_clk.out_clk", "fes_ctrl_cdc.s0_clk", 100000000),
              ("subsys_periph_fes_pixel_clk.out_clk", "fes_ctrl_cdc.m0_clk", 74250000),
              ("subsys_periph_fes_pixel_clk.out_clk", "fes_mem_cdc.s0_clk", 74250000),
              ("subsys_periph_periph_clk.out_clk", "fes_mem_cdc.m0_clk", 100000000),
              ("subsys_periph_fes_pixel_clk.out_clk", "fes_scanout.clk", 74250000),
              ("subsys_periph_fes_pixel_clk.out_clk", "fes_scanout_pixel_reset.clk", 74250000),
              ("subsys_periph_periph_clk.out_clk", "fes_scanout_system_reset.clk", 100000000))
    for start, end, rate in clocks:
        item = connections.get(("clock", start, "subsys_periph_" + end))
        if item is None or item.findtext("parameter[@name='clockRateSysInfo']/value") != str(rate):
            raise ValueError("generated scanout clock differs: " + end)
    reset_routes = (
        ("subsys_periph_fes_scanout_system_reset.out_reset", "subsys_periph_fes_ctrl_cdc.s0_reset"),
        ("subsys_periph_fes_scanout_pixel_reset.out_reset", "subsys_periph_fes_ctrl_cdc.m0_reset"),
        ("subsys_periph_fes_scanout_pixel_reset.out_reset", "subsys_periph_fes_mem_cdc.s0_reset"),
        ("subsys_periph_fes_scanout_system_reset.out_reset", "subsys_periph_fes_mem_cdc.m0_reset"),
        ("subsys_periph_fes_scanout_pixel_reset.out_reset", "subsys_periph_fes_scanout.reset"))
    for start, end in reset_routes:
        if ("reset", start, end) not in connections:
            raise ValueError("generated scanout domain reset differs: " + end)
    reset_source = modules.get("fes_scanout_top_reset")
    top_reset_routes = (
        ("reset", "fes_scanout_top_reset.out_reset", "subsys_periph_fes_scanout_pixel_reset.in_reset"),
        ("reset", "fes_scanout_top_reset.out_reset", "subsys_periph_fes_scanout_system_reset.in_reset"))
    if (reset_source is None or reset_source.get("kind") != "altera_reset_bridge" or
            any(route not in connections for route in top_reset_routes)):
        raise ValueError("generated top scanout interconnect reset source differs")
    reset_bridges = {
        "fes_scanout_top_reset": verify_reset_bridge(project, reset_source, hps),
        "subsys_periph.fes_scanout_pixel_reset": verify_reset_bridge(
            project, modules.get("subsys_periph.fes_scanout_pixel_reset"), hps, "DEASSERT"),
        "subsys_periph.fes_scanout_system_reset": verify_reset_bridge(
            project, modules.get("subsys_periph.fes_scanout_system_reset"), hps, "DEASSERT")}
    bridge_parameters = {}
    for name, width, address_bits, burst, depth in (("fes_ctrl_cdc", 32, 7, 1, 4), ("fes_mem_cdc", 128, 32, 16, 32)):
        module = modules.get("subsys_periph." + name)
        if module is None or module.get("kind") != "mm_ccb" or module.get("version") != "19.4.1":
            raise ValueError("generated clock crossing component differs: " + name)
        expected = {"DATA_WIDTH": width, "SYMBOL_WIDTH": 8, "ADDRESS_WIDTH": address_bits,
                    "MAX_BURST_SIZE": burst, "COMMAND_FIFO_DEPTH": 4, "RESPONSE_FIFO_DEPTH": depth,
                    "MASTER_SYNC_DEPTH": 2, "SLAVE_SYNC_DEPTH": 2, "ENABLE_RESPONSE": 1,
                    "USE_AUTO_ADDRESS_WIDTH": 0, "SYNC_RESET": 0, "PIPELINE_ENABLE": 0}
        actual, source = referenced_parameters(project, module, hps)
        checked = require_parameters(actual, expected, name)
        bridge_parameters[name] = {"parameters": checked, "logical_view": source}
    # The inherited handoff check already validates all elaborated LW regions
    # pairwise for overlap. Require the actual scanout leaf region too.
    probe = hps.verify_probe_handoff(project)
    regions = [r for r in probe["peripheral_slave_map"] if r["name"] == "subsys_periph.fes_scanout.avs"]
    if len(regions) != 1 or regions[0]["base"] != offset or regions[0]["span"] != 128:
        raise ValueError("generated scanout LW leaf region differs")
    return {"instance": "subsys_periph.fes_scanout", "peripheral_offset": offset,
        "pixel_hz": 74250000, "memory_hz": 100000000, "bridge_parameters": bridge_parameters,
        "read_only_master_ports": expected_master, "csr_write_completion": True,
        "peripheral_slave_map": probe["peripheral_slave_map"], "ddr_scanout": True,
        "top_reset_source": "fes_scanout_top_reset.out_reset -> pixel and 100 MHz reset bridge inputs",
        "reset_bridges": reset_bridges,
        "runtime_admission": "not-qualified", "global_reset_ownership": "not-qualified"}


def generate(output, combined, hps, display, tool, version):
    combined.generate(output / "combined", hps, display, tool, version)
    verify_generated(output, hps)


def verify_generated(output, hps, verifier_revision=None):
    directory = output / "combined/hps"
    path = directory / "generation.json"
    generation = json.loads(path.read_text())
    generation["combined_display"]["ddr_scanout"] = True
    generation["scanout"] = verify_handoff(directory / "project", hps)
    if verifier_revision is not None:
        if not re.fullmatch(r"[0-9a-f]{40}", verifier_revision):
            raise ValueError("enhanced verifier requires a full committed identity")
        generation["post_generation_verifier"] = {"fes_revision": verifier_revision,
            "build_py_sha256": hps.digest(HERE / "build.py"),
            "producer_revision": json.loads((directory / "inputs.json").read_text())["fes_revision"],
            "scope": "verification only; generated IP, HDL, constraints and producer inputs unchanged"}
    path.write_text(json.dumps(generation, indent=2, sort_keys=True) + "\n")


def compile_project(output, hps, display, tool):
    directory = output / "combined/hps"
    artifact = hps.compile_project(directory, tool)
    inputs = json.loads((directory / "inputs.json").read_text())
    expected = inputs["combined_display"]["pixel_pll_parameters"]
    actual = display.pll_parameters((directory / "project/fes_combined/sys_pll.ip").read_bytes())
    record = json.loads((directory / "build.json").read_text())
    record["post_compile_pixel_pll_parameters_unchanged"] = actual == expected
    record["scanout_generation_sha256"] = hps.digest(directory / "generation.json")
    record["scanout"] = {"default_enabled": False, "ddr_scanout": True, "runtime_admission": "not-qualified"}
    (directory / "build.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    if actual != expected:
        raise ValueError("compile changed pixel PLL parameters; retained failed validation receipt")
    return artifact


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "generate", "build"))
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        selected = identity(); revision = selected.get("fes_revision") if args.action != "project" else None
        if args.action != "project" and not revision: raise ValueError("scanout generation/build requires clean committed source")
        output, combined, hps, display = prepare(args.vendor_archive, args.output, revision)
        print("Scanout diagnostic: " + str(output), flush=True)
        if args.action != "project":
            if identity() != selected: raise ValueError("source identity changed before generation")
            tool, version = hps.BOARD_BUILD.locate_quartus(args.quartus_root)
            generate(output, combined, hps, display, tool, version)
            if args.action == "build": print("Diagnostic SOF: " + str(compile_project(output, hps, display, tool)), flush=True)
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-scanout: " + str(error) + "\n")


if __name__ == "__main__": main()
