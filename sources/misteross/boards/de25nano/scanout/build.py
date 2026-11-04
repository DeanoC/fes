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
LOCAL = ("build.py", "linux_dma.py", "hdmi_inputs.py", "shared_resets.py", "integrate.tcl", "clocks.sdc", "fes_de25_scanout.v",
         "fes_de25_scanout_hw.tcl", "fes_de25_scanout_board.v",
         "fes_de25_scanout_guard.v", "fes_de25_scanout_guard_hw.tcl",
         "generated/fes_de25_scanout.vh", "generated/fes_de25_scanout.h")
TIMED_RESET_RELEASE = "timed-system-reset-release-v3"
MANAGED_FABRIC = "managed-scanout-initiators-v3"
SHARED_FIFO_RESETS = "shared-fifo-reset-release-v2"
RESET_SDC_SHA256 = "8834ac8de6421bc2c052c2ac8d7a71127477590111c6daab3bf9c8623501d769"
TIMED_RESET_INSTANCES = ("soc_inst|rst_controller", "soc_inst|subsys_periph|rst_controller",
                         "soc_inst|rst_controller_002")


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


def timed_reset_release(inputs):
    scanout = inputs["scanout"]
    canonical = scanout["build_identity_scheme"] == "canonical-functional-record-v2"
    standard = (scanout["build_identity_scheme"] == "standard-board-integration-source-profile" and
                inputs.get("standard_zx81", {}).get("scope") == "standard-core-board-integration" and
                inputs["standard_zx81"].get("runtime_admission") == "not-qualified")
    policy = scanout.get("reset_timing")
    if canonical or standard:
        if (policy != TIMED_RESET_RELEASE or scanout["hps_initialization"] != "HPS FIRST" or
                inputs.get("f2sdram_master_profile") != "scanout-only" or
                scanout.get("managed_fabric") != MANAGED_FABRIC):
            raise ValueError("managed display requires the selected same-clock reset timing profile")
    elif policy is not None or scanout.get("managed_fabric") is not None:
        raise ValueError("same-clock reset timing requires the canonical MENU profile")
    return canonical or standard


def timed_scanout_sdc(source):
    # The system reset source and receiver both use the 100 MHz system PLL.
    # Keep the independent reference configuration and pixel bridge ingress cut.
    for pin in ("aclr", "clrn"):
        for stage in (0, 1):
            source = replace_once(source, "soc_inst|rst_controller_002|alt_rst_sync_uq1|"
                "altera_reset_synchronizer_int_chain[" + str(stage) + "]|" + pin, "")
    source = replace_once(source, "$scanout_clear_count != 6", "$scanout_clear_count != 4")
    source = replace_once(source, "(expected 6)", "(expected 4)")
    source = replace_once(source, "exactly six configuration", "exactly four configuration")
    return source + FIFO_RESET_SDC


# Canonical MENU only: the two peer reset levels enter the first delay D.
# Local parent release and every interstage/functional consumer remain timed.
FIFO_RESET_SDC = """
# FIFO release data crossings: only peer source to first D.
foreach {source head} {
 {soc_inst|rst_controller_001|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain_out} {soc_inst|subsys_periph|write_reset_sync|release_pipe[0]|d}
 {soc_inst|rst_controller_002|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain_out} {soc_inst|subsys_periph|read_reset_sync|release_pipe[0]|d}
} {
 set fifo_peer_source [get_registers $source]
 set fifo_peer_head [get_pins -compatibility_mode -nocase -nowarn $head]
 if {[get_collection_size $fifo_peer_source]!=1 || [get_collection_size $fifo_peer_head]!=1} {error "FIFO peer reset ingress changed"}
 set_false_path -from $fifo_peer_source -to $fifo_peer_head
}
"""


def timed_vendor_reset_sdc(original, *, timed_instances=TIMED_RESET_INSTANCES):
    if sha(original) != RESET_SDC_SHA256:
        raise ValueError("generated reset SDC differs from reviewed Quartus 26.3 source")
    # MENU has a third system receiver for its scanout bridge. BRAM ZX81
    # shares only the original top/peripheral pair; no other scope is reviewed.
    if timed_instances not in (TIMED_RESET_INSTANCES, TIMED_RESET_INSTANCES[:2]):
        raise ValueError("unreviewed same-clock reset receiver selection")
    source = original.decode()
    guard = "[lsearch -exact {" + " ".join(timed_instances) + "} [get_current_instance]] < 0"
    for pin in ("aclr", "clrn"):
        source = replace_once(source, "if {$" + pin + "_counter > 0} {",
                              "if {$" + pin + "_counter > 0 && " + guard + "} {")
    return source.encode()


def reset_sdc_files(project):
    files = sorted(project.rglob("altera_reset_controller.sdc"))
    if len(files) != 6 or any(p.is_symlink() for p in project.rglob("*") if p.name == "altera_reset_controller.sdc"):
        raise ValueError("generated reset SDC inventory differs from the reviewed six files")
    for path in files:
        if any(parent.is_symlink() for parent in path.parents if parent.is_relative_to(project)):
            raise ValueError("generated reset SDC must remain inside the private project")
    return files


def repair_generated_reset_timing(directory):
    if not timed_reset_release(json.loads((directory / "inputs.json").read_text())):
        raise ValueError("same-clock reset repair requires canonical MENU")
    project = directory / "project"
    originals = directory / "reset-timing-originals"
    originals.mkdir(exist_ok=False)
    files = {}
    for path in reset_sdc_files(project):
        relative = path.relative_to(project).as_posix()
        original = path.read_bytes()
        changed = timed_vendor_reset_sdc(original)
        retained = originals / relative
        retained.parent.mkdir(parents=True, exist_ok=True)
        retained.write_bytes(original)
        path.write_bytes(changed)
        files[relative] = {"before_sha256": sha(original), "after_sha256": sha(changed)}
    record = {"policy": TIMED_RESET_RELEASE, "clock": "pll_inst|iopll_0_outclk0",
              "timed_instances": list(TIMED_RESET_INSTANCES), "files": files}
    (directory / "reset-timing.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    return verify_generated_reset_timing(directory)


def verify_generated_reset_timing(directory):
    inputs = json.loads((directory / "inputs.json").read_text())
    if not timed_reset_release(inputs):
        raise ValueError("same-clock reset verification requires canonical MENU")
    record = json.loads((directory / "reset-timing.json").read_text())
    project = directory / "project"
    raw = directory.parents[1] / "functional-source" / BOARD / "scanout/clocks.sdc"
    expected_sdc = timed_scanout_sdc(raw.read_text()).encode()
    if ((project / "fes_mailbox/clocks.sdc").read_bytes() != expected_sdc or
            inputs["scanout"]["source_sha256"]["clocks.sdc"] != sha(expected_sdc)):
        raise ValueError("canonical MENU same-clock reset SDC differs from captured source")
    verify_reset_synchronizers(project)
    names = {p.relative_to(project).as_posix() for p in reset_sdc_files(project)}
    if (set(record) != {"policy", "clock", "timed_instances", "files"} or
            record["policy"] != TIMED_RESET_RELEASE or record["clock"] != "pll_inst|iopll_0_outclk0" or
            record["timed_instances"] != list(TIMED_RESET_INSTANCES) or set(record["files"]) != names):
        raise ValueError("same-clock reset timing receipt differs from the selected profile")
    for name in names:
        original = (directory / "reset-timing-originals" / name).read_bytes()
        changed = timed_vendor_reset_sdc(original)
        if (record["files"][name] != {"before_sha256": sha(original), "after_sha256": sha(changed)} or
                (project / name).read_bytes() != changed):
            raise ValueError("generated same-clock reset constraints changed: " + name)
    return record


def build_identity(sources, hps_first, linux_dma):
    # Functional source and selected hardware profile, independent of a docs
    # commit or checkout path. Payload bytes retain their separate RBF digest.
    value = sha(json.dumps({"sources": {n: sha(d) for n, d in sources.items()},
        "hps_first": hps_first, "linux_dma": linux_dma},
        sort_keys=True, separators=(",", ":")).encode())[:32]
    if value == "0" * 32:
        raise ValueError("zero scanout build identity")
    return value


def prepare(archive, output=None, revision=None, *, hps_first=False, linux_dma=False,
            package_build_id=None):
    if type(hps_first) is not bool:
        raise ValueError("HPS-first selection must be boolean")
    if type(linux_dma) is not bool or (linux_dma and not hps_first):
        raise ValueError("Linux DMA requires explicit HPS-first selection")
    if package_build_id is not None and (not revision or not linux_dma or
            not isinstance(package_build_id, str) or
            not re.fullmatch(r"[0-9a-f]{32}", package_build_id) or package_build_id == "0" * 32):
        raise ValueError("package build identity requires committed Linux-DMA source and nonzero 128-bit ID")
    boot_order = "HPS FIRST" if hps_first else "AFTER INIT_DONE"
    sources = capture(revision)
    build_id = package_build_id or build_identity(sources, hps_first, linux_dma)
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
    if package_build_id is not None:
        path = local / "clocks.sdc"
        path.write_text(timed_scanout_sdc(path.read_text()))
    hdmi = load(snapshot / BOARD / "scanout/hdmi_inputs.py", "scanout_retained_hdmi_inputs")
    vendor_hdmi = {name: (project / "fes_combined" / name).read_bytes()
                   for name in ("I2C_HDMI_Config.v", "I2C_Controller.v")}
    original_hdmi = output / "hdmi-vendor-inputs"
    original_hdmi.mkdir()
    for name, data in vendor_hdmi.items():
        (original_hdmi / name).write_bytes(data)
    hdmi_files, hdmi_inputs = hdmi.derive(vendor_hdmi["I2C_HDMI_Config.v"], vendor_hdmi["I2C_Controller.v"])
    for name, data in hdmi_files.items():
        (local / name).write_bytes(data)
    # The endpoint's Qsys fileset owns the actual generic reader/video RTL.
    for name in ("fes_menu_reader_de25.v", "fes_menu_video.v"):
        (local / name).write_bytes((project / "fes_combined" / name).read_bytes())
    for name in ("video.v", "fes_menu_reader_de25.v", "fes_menu_video.v", "fes_menu_pattern_memory.v"):
        (project / "fes_combined" / name).unlink()
    qsf = (project / "golden_top.qsf").read_text()
    for before, after in (("I2C_HDMI_Config.v", "fes_de25_scanout_hdmi_config.v"),
                          ("I2C_Controller.v", "fes_de25_scanout_i2c_controller.v")):
        qsf = replace_once(qsf, "set_global_assignment -name VERILOG_FILE fes_combined/" + before + "\n",
                           "set_global_assignment -name VERILOG_FILE fes_mailbox/" + after + "\n")
        (project / "fes_combined" / before).unlink()
    for name in ("video.v", "fes_menu_reader_de25.v", "fes_menu_video.v", "fes_menu_pattern_memory.v"):
        qsf = replace_once(qsf, "set_global_assignment -name VERILOG_FILE fes_combined/" + name + "\n", "")
    qsf = replace_once(qsf, "set_global_assignment -name SDC_FILE fes_combined/clocks.sdc\n",
                       "set_global_assignment -name SDC_FILE fes_mailbox/clocks.sdc\n")
    (project / "fes_combined/clocks.sdc").unlink()
    qsf += "set_global_assignment -name VERILOG_FILE fes_mailbox/fes_de25_scanout_board.v\n"
    if re.search(r"(?m)^\s*set_global_assignment\s+-name\s+HPS_INITIALIZATION\b", qsf):
        raise ValueError("upstream already selects HPS initialization; review before overriding")
    qsf += f'set_global_assignment -name HPS_INITIALIZATION "{boot_order}"\n'
    if package_build_id is not None:
        if re.search(r"(?m)^\s*set_global_assignment\s+-name\s+SEED\b", qsf):
            raise ValueError("upstream already selects a seed; review before package compilation")
        qsf += "set_global_assignment -name SEED 1\n"
    (project / "golden_top.qsf").write_text(qsf)
    top = (project / "golden_top.v").read_text()
    top = replace_once(top, "fes_de25_combined_video #(.MENU_PATTERN(1'b1)) fes_video_inst (",
        "fes_de25_scanout_board fes_video_inst (\n"
        "  .system_clk(system_clk_100_internal), .top_reset_n(top_reset_n),\n"
        "  .pixel_clk(scanout_pixel_clk), .scanout_reset_n(scanout_reset_n), .clock_fault(scanout_clock_fault), .display_ready(scanout_display_ready),\n"
        "  .video_rgb(scanout_rgb), .video_de(scanout_de), .video_hs(scanout_hs), .video_vs(scanout_vs),")
    top = replace_once(top, "qsys_top soc_inst (", "wire scanout_pixel_clk, scanout_reset_n, scanout_clock_fault, scanout_display_ready;\n"
        "wire [23:0] scanout_rgb;\nwire scanout_de, scanout_hs, scanout_vs;\nqsys_top soc_inst (\n"
        ".fes_scanout_pixel_clk_clk (scanout_pixel_clk),\n"
        ".fes_scanout_reset_reset_n (scanout_reset_n),\n"
        ".fes_scanout_clock_fault_export (scanout_clock_fault),\n"
        ".fes_scanout_ready_export (scanout_display_ready),\n"
        ".fes_scanout_video_rgb (scanout_rgb), .fes_scanout_video_de (scanout_de),\n"
        ".fes_scanout_video_hs (scanout_hs), .fes_scanout_video_vs (scanout_vs),")
    (project / "golden_top.v").write_text(top)
    definition = sources[BOARD + "scanout/generated/fes_de25_scanout.vh"].decode()
    offset = re.search(r"^`define FES_DE25_SCANOUT_PERIPHERAL_OFFSET 32'h([0-9a-f]{8})$", definition, re.M)
    if not offset: raise ValueError("missing generated scanout register offset")
    addition = sources[BOARD + "scanout/integrate.tcl"].decode().replace("@SCANOUT_BASE@", hex(int(offset[1], 16)))
    addition = addition.replace("@SCANOUT_BUILD_ID@", "0x" + build_id)
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
    exports += "add_instance fes_scanout_pixel_clock altera_clock_bridge 19.2.0\n"
    exports += "set_instance_parameter_value fes_scanout_pixel_clock EXPLICIT_CLOCK_RATE 74250000\n"
    exports += "add_connection fes_scanout_pixel_clock.out_clk subsys_periph.fes_scanout_pixel_clk\n"
    exports += "add_interface fes_scanout_pixel_clk clock sink\n"
    exports += "set_interface_property fes_scanout_pixel_clk EXPORT_OF fes_scanout_pixel_clock.in_clk\n"
    for name in ("fes_scanout_video", "fes_scanout_clock_fault", "fes_scanout_ready"):
        exports += "add_interface " + name + " conduit end\nset_interface_property " + name + " EXPORT_OF subsys_periph." + name + "\n"
    script = replace_once(script, before, exports + before)
    dma_profile, parameter_changes = None, {}
    if linux_dma:
        dma = load(snapshot / BOARD / "scanout/linux_dma.py", "scanout_retained_linux_dma")
        prefix, dma_profile, parameter_changes = dma.configure(project, definition, hps,
                                                               managed_fabric=package_build_id is not None)
        script = dma.isolate_script(script, before)
        if package_build_id is not None:
            if dma.MANAGED_FABRIC != MANAGED_FABRIC:
                raise ValueError("managed fabric producer policy differs")
            script = dma.isolate_managed_fabric_script(script, before)
            dma.retire_managed_jtag_constraints(project)
        script = replace_once(script, "package require -exact qsys 26.3\n", "package require -exact qsys 26.3\n" + prefix)
    (project / "integrate-mailbox.tcl").write_text(script)
    inputs = json.loads(original_inputs)
    inputs.pop("base_revision", None); inputs.update(selected)
    inputs["functional_snapshot_sha256"] = {name: sha(data) for name, data in sources.items()}
    inputs["combined_display"] = {name: value for name, value in inputs["combined_display"].items()
        if name in ("vendor_source_sha256", "reviewed_pll_device_repair", "reviewed_menu_ram_hint_repair",
                    "pixel_pll_parameters", "pixel_clock_hz", "i2c_internal_clock", "configuration_release",
                    "warm_reset_ack_n", "debug_paths")}
    inputs["combined_display"].update(mode="allocated-buffer-scanout", ddr_scanout=True, runtime_menu=False)
    inputs["scanout"] = {"build_id": build_id,
        "hdmi_inputs": hdmi_inputs,
        "build_identity_scheme": "canonical-functional-record-v2" if package_build_id else "diagnostic-source-profile",
        "hps_initialization": boot_order, "peripheral_offset": int(offset[1], 16), "default_enabled": False,
        "pixel_hz": 74250000, "system_hz": 100000000, "control_bridge": "system100 bounded guard -> mm_ccb19.4.1 DATA32 RESPONSE1",
        "memory_bridge": "mm_ccb19.4.1 DATA128 RESPONSE1 MAX16", "allocation_bytes": 8388608,
        "reset_release": {"assertion": "asynchronous",
            "deassertion": {"pixel": "synchronous to 74250000 Hz", "peripheral": "synchronous to 100000000 Hz"}},
        "source_sha256": {staged_names[name]: hps.digest(local / staged_names[name]) for name in LOCAL} |
                         {name: hps.digest(local / name) for name in ("fes_menu_reader_de25.v", "fes_menu_video.v")},
        "compiled_top_sha256": hps.digest(project / "golden_top.v"), "rendered_integration_sha256": hps.digest(project / "integrate-mailbox.tcl"),
        "clock_loss": "sticky fault/blank/drain when pixel resumes; never reset live bridges",
        "global_reset_ownership": "not-qualified; active-low warm acknowledgement remains inactive1",
        "dma_admission": "not-qualified; SID/firewall/firmware/physical lifetime gates remain"}
    if package_build_id is not None:
        inputs["scanout"]["reset_timing"] = TIMED_RESET_RELEASE
        inputs["scanout"]["managed_fabric"] = MANAGED_FABRIC
        inputs["scanout"]["shared_fifo_resets"] = SHARED_FIFO_RESETS
    if dma_profile is not None:
        inputs["scanout"]["linux_dma"] = dma_profile
        inputs["allowed_hps_parameter_changes"] = parameter_changes
        inputs["f2sdram_master_profile"] = "scanout-only"
        inputs["combined_display"]["debug_paths"] = "JTAG F2SDRAM master/window removed; other JTAG/OCM/F2H paths preserved"
        if package_build_id is not None:
            inputs["combined_display"]["debug_paths"] = "JTAG Avalon masters and F2H translator removed, unused native F2H disabled; HPS control/OCM and JTAG FPGA configuration preserved"
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


def verify_reset_synchronizers(project):
    """Bind retained fault/visibility state to exact first-D CDC constraints."""
    def compact(text):
        return re.sub(r"\s+", "", re.sub(r"//[^\n]*|#[^\n]*", "", text))

    board = compact((project / "fes_mailbox/fes_de25_scanout_board.v").read_text())
    protocol = (
        "fes_de25_top_resetconfig_reset(.clk(CLOCK0_50),.ninit_done(ninit_done||!top_reset_n),"
        ".pll_locked(pixel_locked),.reset_n(config_reset_n));",
        "if(!config_reset_n)beginvisible_allow<=0;if(!pending)beginepoch<=!epoch;pending<=1;end"
        "endelseif(pending)beginvisible_allow<=0;"
        "if(system_ack_sync[1]==epoch&&pixel_ack_sync[1]==epoch&&!pixel_active_sync[1])pending<=0;"
        "endelsevisible_allow<=1;",
        "always@(posedgesystem_clk)beginsystem_epoch_sync<={system_epoch_sync[0],epoch};"
        "if(!first_lock_seen)system_ack<=system_epoch_sync[1];end",
        "if(!first_lock_seen)beginif(monitor_ready_sync[1]&&system_ack==system_epoch_sync[1])"
        "first_lock_seen<=1;endelseif(!monitor_ready_sync[1]||system_epoch_sync[1]!=system_ack)fault<=1;",
        "always@(posedgeCLOCK0_50ornegedgeconfig_reset_n)if(!config_reset_n)monitor_ready_source<=0;"
        "elsemonitor_ready_source<=system_ack_sync[1]==epoch;",
        "if(!qualified_sync[1]||!visible_sync[1]||!allow_sync[1]||pixel_epoch_sync[1]!=pixel_ack)begin"
        "video_active<=0;if(!allow_sync[1]&&!visible_line)pixel_ack<=pixel_epoch_sync[1];"
        "if(boundary)beginvisible_line<=0;if(!allow_sync[1])pixel_ack<=pixel_epoch_sync[1];end"
        "endelseif(boundary)beginvisible_line<=1;video_active<=1;end",
        "always@(posedgesystem_clkornegedgetop_reset_n)if(!top_reset_n)visible_request<=1'b0;"
        "elsevisible_request<=first_lock_seen&&monitor_ready_sync[1]&&!fault&&hdmi_ready_sync[1];",
        "assigndisplay_ready=video_active&&visible_line;",
        "assignscanout_reset_n=first_lock_seen;", "assignclock_fault=fault;",
    )
    if any(token not in board for token in protocol) or re.search(r"system_ack<=\d|system_ack<=1'b", board):
        raise ValueError("scanout needs a retained fault token, cold-only ACK and unchanged live bridge reset")
    if ("always@(posedgepixel_clk)begin" not in board or
            [event for event in re.findall(r"always@\(([^)]+)\)", board) if "pixel_clk" in event] != ["posedgepixel_clk"] or
            "visible_release" in board or "lock_qualifier" in board):
        raise ValueError("scanout visibility must retain ordinary pixel data registers without another reset chain")
    if ("wirepin_allow=config_reset_n&&visible_allow;" not in board or
            "assignHDMI_TX_D=pin_allow&&video_active&&visible_line?video_rgb:24'd0;" not in board or
            "assignHDMI_TX_DE=pin_allow&&video_active&&visible_line&&video_de;" not in board or
            "wireboundary=previous_hs&&!video_hs&&!video_vs;" not in board):
        raise ValueError("scanout HDMI pin blanking and complete-sync visibility boundary changed")
    crossings = (
        ("system_ack", "fes_video_inst|system_ack", "system_ack_sync", "system_ack"),
        ("pixel_ack", "fes_video_inst|pixel_ack", "pixel_ack_sync", "pixel_ack"),
        ("pixel_active", "fes_video_inst|video_active", "pixel_active_sync", "video_active"),
        ("system_epoch", "fes_video_inst|epoch", "system_epoch_sync", "epoch"),
        ("monitor_ready", "fes_video_inst|monitor_ready_source", "monitor_ready_sync", "monitor_ready_source"),
        ("visible_allow", "fes_video_inst|visible_allow", "allow_sync", "visible_allow"),
        ("pixel_epoch", "fes_video_inst|epoch", "pixel_epoch_sync", "epoch"),
        ("pixel_qualified", "fes_video_inst|config_reset|sync_regs[1]", "qualified_sync", "config_reset_n"),
    )
    for _, _, head, source in crossings:
        if f"{head}<={{{head}[0],{source}}};" not in board:
            raise ValueError("scanout retained synchronizer data connection changed: " + head)
    raw = re.sub(r"#[^\n]*", "", (project / "fes_mailbox/clocks.sdc").read_text())
    sdc = compact(raw)
    table = re.findall(r"foreach\s+\{name source head\}\s+\{(.*?)\}\s*\{", raw, re.S)
    if len(table) != 1:
        raise ValueError("scanout retained CDC ingress table missing/ambiguous")
    parsed = [re.fullmatch(r"\s*(\w+)\s+\{([^{}]+)\}\s+(\w+)\s*", line) for line in table[0].splitlines() if line.strip()]
    if not all(parsed) or [m.groups() for m in parsed] != [row[:3] for row in crossings]:
        raise ValueError("scanout retained CDC ingress table must bind exactly eight registered sources to stage0")
    required = (
        "setretained_source[get_registers$source]",
        "setretained_head[get_pins-compatibility_mode-nocase-nowarn[format{fes_video_inst|%s[0]|d}$head]]",
        "if{[get_collection_size$retained_source]!=1||[get_collection_size$retained_head]!=1}",
        "if{[get_collection_size$scanout_visible_clear]!=0}",
    )
    if any(token not in sdc for token in required):
        raise ValueError("scanout retained CDC requires unique source/stage0 D and no pixel-data asynchronous clears")
    for name, source, target in (
        ("visible", "fes_video_inst|visible_request", "fes_video_inst|visible_sync[0]|d"),
        ("ready", "fes_video_inst|hdmi_ready_source", "fes_video_inst|hdmi_ready_sync[0]|d"),
        ("fault", "fes_video_inst|fault", "soc_inst|subsys_periph|fes_scanout|fes_scanout|clock_fault_meta|d"),
    ):
        if (f"setscanout_{name}_source[get_registers{{{source}}}]" not in sdc or
                target not in sdc or
                f"if{{[get_collection_size$scanout_{name}_source]!=1||[get_collection_size$scanout_{name}_head]!=1}}" not in sdc):
            raise ValueError("scanout registered " + name + " source to stage0 D changed")
    timed_system = timed_reset_release(json.loads((project.parent / "inputs.json").read_text()))
    fifo_sdc = compact(re.sub(r"#[^\n]*", "", FIFO_RESET_SDC))
    if (sdc.count(fifo_sdc) != (1 if timed_system else 0)):
        raise ValueError("scanout shared FIFO reset requires exactly two peer-to-first-D crossings")
    allowed_cuts = ["set_false_path -to $scanout_aclr", "set_false_path -to $scanout_clrn",
                    "set_false_path -from $hdmi_input -to $hdmi_input_head",
                    "set_false_path -from $retained_source -to $retained_head"] + [
        f"set_false_path -from $scanout_{name}_source -to $scanout_{name}_head" for name in ("fault", "visible", "ready")]
    if timed_system:
        allowed_cuts.append("set_false_path -from $fifo_peer_source -to $fifo_peer_head")
    cuts = [" ".join(v.split()) for v in re.findall(r"set_false_path[^;\n}]*", raw)]
    if sorted(cuts) != sorted(allowed_cuts) or any(cmd in sdc for cmd in ("set_clock_groups", "set_disable_timing", "set_multicycle_path")):
        raise ValueError("scanout SDC must cut only reviewed ingress; interstage/consumer paths stay timed")
    controllers = ("rst_controller_001",) if timed_system else ("rst_controller_001", "rst_controller_002")
    for pin in ("aclr", "clrn"):
        targets = {f"fes_video_inst|config_reset|sync_regs*|{pin}"} | {
            f"soc_inst|{controller}|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[{stage}]|{pin}"
            for controller in controllers for stage in (0, 1)}
        found = re.findall(r"set\s+scanout_" + pin + r"\s+\[get_pins[^{]*\{([^}]+)\}\]", raw)
        if len(found) != 1 or set(found[0].split()) != targets or len(found[0].split()) != len(targets):
            raise ValueError("scanout SDC configuration/pixel-bridge clear targets changed: " + pin)
    clear_pins = 4 if timed_system else 6
    if "if{$scanout_clear_count!=" + str(clear_pins) + "}" not in sdc:
        raise ValueError("scanout SDC clear-pin cardinality changed")
    return {"pixel_clock": "74250000 Hz", "system_clock": "100000000 Hz",
            "assertion": "asynchronous", "deassertion": "synchronous per domain",
            "hdmi_data_de_gate": "retained pixel data; immediate qualification blanking and complete-sync rearm",
            "visibility_protocol": "retained-fault-token-v1", "fault_latency_system_edges": 3,
            "readiness_loss_pixel_edges": 3, "retained_cdc_ingresses": len(crossings),
            "fifo_peer_cdc_ingresses": 2 if timed_system else 0, "sdc_clear_pins": clear_pins}


def endpoint_build_identity(project, endpoint, hps, expected):
    if not isinstance(expected, str) or not re.fullmatch(r"[0-9a-f]{32}", expected) or expected == "0" * 32:
        raise ValueError("invalid selected scanout build identity")
    actual = endpoint.findtext("parameter[@name='BUILD_ID']/value")
    logical = None
    if actual is None:
        parameters, logical = referenced_parameters(project, endpoint, hps)
        actual = parameters.get("BUILD_ID")
    text = str(actual).strip()
    if re.fullmatch(r"128'[hH][0-9a-fA-F]{1,32}", text):
        number = int(text[5:], 16)
    elif re.fullmatch(r"0[xX][0-9a-fA-F]{1,32}", text):
        number = int(text[2:], 16)
    elif re.fullmatch(r"[0-9]+", text):
        number = int(text, 10)
    else:
        raise ValueError("unsupported generated scanout build identity")
    if not 0 < number < (1 << 128) or number != int(expected, 16):
        raise ValueError("generated scanout build identity differs")
    return {"build_id": expected, "parameter": actual, "logical_view": logical}


def verify_handoff(project, hps):
    reset_synchronizers = verify_reset_synchronizers(project)
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = {m.get("path"): m for m in tree.findall("module")}
    endpoint = modules.get("subsys_periph.fes_scanout")
    if endpoint is None or endpoint.get("kind") != "fes_de25_scanout" or endpoint.get("version") != "1.0":
        raise ValueError("generated scanout endpoint differs")
    if hps.component_ports(endpoint, "ready") != {"export": ("Input", 1)}:
        raise ValueError("generated scanout display readiness conduit differs")
    build_id = json.loads((project.parent / "inputs.json").read_text())["scanout"]["build_id"]
    build_identity = endpoint_build_identity(project, endpoint, hps, build_id)
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
    routes = (("subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_scanout_guard.avs"),
              ("subsys_periph_fes_scanout_guard.avm", "subsys_periph_fes_ctrl_cdc.s0"),
              ("subsys_periph_fes_ctrl_cdc.m0", "subsys_periph_fes_scanout.avs"),
              ("subsys_periph_fes_scanout.avm", "subsys_periph_fes_mem_cdc.s0"),
              ("subsys_periph_fes_mem_cdc.m0", "subsys_hps_f2sdram_adapter.axi4_sub"))
    definition = (project / "fes_mailbox/generated/fes_de25_scanout.vh").read_text()
    offset = int(re.search(r"^`define FES_DE25_SCANOUT_PERIPHERAL_OFFSET 32'h([0-9a-f]{8})$", definition, re.M)[1], 16)
    for index, (start, end) in enumerate(routes):
        item = connections.get(("avalon", start, end))
        if item is None or int(item.findtext("parameter[@name='baseAddress']/value"), 0) != (offset if index == 0 else 0):
            raise ValueError("generated scanout control/memory route differs: " + start)
    pixel_clock_bridge = modules.get("fes_scanout_pixel_clock")
    if (pixel_clock_bridge is None or pixel_clock_bridge.get("kind") != "altera_clock_bridge" or
            ("clock", "fes_scanout_pixel_clock.out_clk", "subsys_periph_fes_pixel_clk.in_clk") not in connections):
        raise ValueError("generated top-level scanout clock bridge differs")
    clocks = (("subsys_periph_periph_clk.out_clk", "fes_scanout_guard.clk", 100000000),
              ("subsys_periph_periph_clk.out_clk", "fes_ctrl_cdc.s0_clk", 100000000),
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
        ("subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_scanout_guard.reset"),
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
    for name, width, address_bits, burst, depth in (("fes_ctrl_cdc", 32, 7, 1, 4), ("fes_mem_cdc", 128, 32, 16, 256)):
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
    guard = modules.get("subsys_periph.fes_scanout_guard")
    if guard is None or guard.get("kind") != "fes_de25_scanout_guard" or guard.get("version") != "1.0":
        raise ValueError("generated scanout system-clock guard differs")
    for interface, direction, address_bits in (("avs", "Input", 8), ("avm", "Output", 7)):
        actual_ports = hps.component_ports(guard, interface)
        reverse = "Output" if direction == "Input" else "Input"
        expected_ports = {"address": (direction, address_bits), "read": (direction, 1),
            "write": (direction, 1), "byteenable": (direction, 4), "writedata": (direction, 32),
            "waitrequest": (reverse, 1), "readdata": (reverse, 32), "readdatavalid": (reverse, 1),
            "writeresponsevalid": (reverse, 1), "response": (reverse, 2)}
        if actual_ports != expected_ports:
            raise ValueError("generated scanout guard ports differ: " + interface)
    # The inherited handoff check already validates all elaborated LW regions
    # pairwise for overlap. Require the actual scanout leaf region too.
    _, probe = hps.verify_memory_handoff(project, json.loads((project.parent / "inputs.json").read_text()))
    regions = [r for r in probe["peripheral_slave_map"] if r["name"] == "subsys_periph.fes_scanout_guard.avs"]
    if len(regions) != 1 or regions[0]["base"] != offset or regions[0]["span"] != 256:
        raise ValueError("generated scanout LW leaf region differs")
    return {"instance": "subsys_periph.fes_scanout", "build_identity": build_identity, "peripheral_offset": offset,
        "pixel_hz": 74250000, "memory_hz": 100000000, "bridge_parameters": bridge_parameters,
        "read_only_master_ports": expected_master, "csr_write_completion": True, "control_guard": "100 MHz bounded, sticky poison, no cancellation or drain inference",
        "peripheral_slave_map": probe["peripheral_slave_map"], "ddr_scanout": True,
        "top_reset_source": "fes_scanout_top_reset.out_reset -> pixel and 100 MHz reset bridge inputs",
        "reset_bridges": reset_bridges,
        "reset_synchronizers": reset_synchronizers,
        "top_pixel_clock_bridge": "74.25 MHz clock bridge drives the nested pixel-clock sink",
        "runtime_admission": "not-qualified", "global_reset_ownership": "not-qualified"}


def generate(output, combined, hps, display, tool, version):
    combined.generate(output / "combined", hps, display, tool, version)
    directory = output / "combined/hps"
    if timed_reset_release(json.loads((directory / "inputs.json").read_text())):
        repair_generated_reset_timing(directory)
        shared_reset_helper(directory).apply(directory)
    verify_generated(output, hps)


def verify_generated(output, hps, verifier_revision=None):
    directory = output / "combined/hps"
    path = directory / "generation.json"
    generation = json.loads(path.read_text())
    generation["combined_display"]["ddr_scanout"] = True
    generation["scanout"] = verify_handoff(directory / "project", hps)
    inputs = json.loads((directory / "inputs.json").read_text())
    if timed_reset_release(inputs):
        generation["scanout"]["reset_timing"] = verify_generated_reset_timing(directory)
        generation["scanout"]["shared_fifo_resets"] = shared_reset_helper(directory).verify(directory)
    generation["scanout"]["hdmi_inputs"] = verify_hdmi_inputs(directory / "project", inputs, hps)
    if timed_reset_release(inputs):
        generation["scanout"]["managed_fabric"] = verify_managed_fabric(directory, hps)
    selected = json.loads((directory / "inputs.json").read_text())["scanout"].get("linux_dma")
    if selected is not None:
        helper = load(directory / "project/fes_mailbox/linux_dma.py", "scanout_generated_linux_dma")
        generation["scanout"]["linux_dma"] = helper.verify(directory / "project", hps, selected)
    if verifier_revision is not None:
        if not re.fullmatch(r"[0-9a-f]{40}", verifier_revision):
            raise ValueError("enhanced verifier requires a full committed identity")
        generation["post_generation_verifier"] = {"fes_revision": verifier_revision,
            "build_py_sha256": hps.digest(HERE / "build.py"),
            "producer_revision": json.loads((directory / "inputs.json").read_text())["fes_revision"],
            "scope": "verification only; generated IP, HDL, constraints and producer inputs unchanged"}
    path.write_text(json.dumps(generation, indent=2, sort_keys=True) + "\n")


def managed_fabric_helper(directory):
    inputs = json.loads((directory / "inputs.json").read_text())
    if not timed_reset_release(inputs):
        raise ValueError("managed fabric requires canonical MENU")
    path = directory / "project/fes_mailbox/linux_dma.py"
    if sha(path.read_bytes()) != inputs["scanout"]["source_sha256"]["linux_dma.py"]:
        raise ValueError("managed fabric verifier differs from captured source")
    helper = load(path, "scanout_selected_managed_fabric")
    if helper.MANAGED_FABRIC != MANAGED_FABRIC:
        raise ValueError("managed fabric verifier policy differs")
    return helper


def verify_managed_fabric(directory, hps):
    return managed_fabric_helper(directory).verify_managed_fabric(directory / "project", hps)


def shared_reset_helper(directory):
    inputs = json.loads((directory / "inputs.json").read_text())
    if not timed_reset_release(inputs) or inputs["scanout"].get("shared_fifo_resets") != SHARED_FIFO_RESETS:
        raise ValueError("shared FIFO resets require the canonical MENU policy")
    path = directory / "project/fes_mailbox/shared_resets.py"
    if sha(path.read_bytes()) != inputs["scanout"]["source_sha256"]["shared_resets.py"]:
        raise ValueError("shared FIFO reset helper differs from captured source")
    helper = load(path, "scanout_selected_shared_resets")
    if helper.POLICY != SHARED_FIFO_RESETS:
        raise ValueError("shared FIFO reset helper policy differs")
    return helper


def compile_project(output, hps, display, tool):
    directory = output / "combined/hps"
    inputs = json.loads((directory / "inputs.json").read_text())
    managed_fabric = None
    if timed_reset_release(inputs):
        verify_generated_reset_timing(directory)
        sharing = shared_reset_helper(directory).verify(directory)
        if json.loads((directory / "generation.json").read_text())["scanout"]["shared_fifo_resets"] != sharing:
            raise ValueError("shared FIFO reset wiring differs from the generated receipt")
        managed_fabric = verify_managed_fabric(directory, hps)
        if json.loads((directory / "generation.json").read_text())["scanout"]["managed_fabric"] != managed_fabric:
            raise ValueError("managed fabric differs from the generated receipt")
    artifact = hps.compile_project(directory, tool)
    if timed_reset_release(inputs):
        checked = verify_generated_reset_timing(directory)
        if json.loads((directory / "generation.json").read_text())["scanout"]["reset_timing"] != checked:
            raise ValueError("compiled reset timing differs from the generated receipt")
        sharing = shared_reset_helper(directory).verify(directory)
        if json.loads((directory / "generation.json").read_text())["scanout"]["shared_fifo_resets"] != sharing:
            raise ValueError("compiled shared FIFO reset wiring differs from the generated receipt")
        managed_fabric = verify_managed_fabric(directory, hps)
        if json.loads((directory / "generation.json").read_text())["scanout"]["managed_fabric"] != managed_fabric:
            raise ValueError("compiled managed fabric differs from the generated receipt")
    verify_hdmi_inputs(directory / "project", inputs, hps)
    expected = inputs["combined_display"]["pixel_pll_parameters"]
    actual = display.pll_parameters((directory / "project/fes_combined/sys_pll.ip").read_bytes())
    record = json.loads((directory / "build.json").read_text())
    record["post_compile_pixel_pll_parameters_unchanged"] = actual == expected
    record["scanout_generation_sha256"] = hps.digest(directory / "generation.json")
    record["scanout"] = {"build_id": inputs["scanout"]["build_id"], "default_enabled": False, "ddr_scanout": True, "runtime_admission": "not-qualified",
                         "f2sdram_master_profile": inputs.get("f2sdram_master_profile", "diagnostic"),
                         "linux_dma": inputs["scanout"].get("linux_dma"),
                         **({"managed_fabric": managed_fabric} if managed_fabric is not None else {})}
    (directory / "build.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    if actual != expected:
        raise ValueError("compile changed pixel PLL parameters; retained failed validation receipt")
    if inputs["scanout"]["hps_initialization"] == "HPS FIRST":
        artifacts = hps.linux_configuration_artifacts(directory, tool, artifact,
            hps_initialization=inputs["scanout"]["hps_initialization"])
        record = {"format": 1, "classification": "unadmitted-HPS-first-scanout",
                  "fes_revision": inputs["fes_revision"],
                  "inputs_sha256": hps.digest(directory / "inputs.json"),
                  "generation_sha256": hps.digest(directory / "generation.json"),
                  "f2sdram_master_profile": inputs.get("f2sdram_master_profile", "diagnostic"),
                  "linux_dma": inputs["scanout"].get("linux_dma"),
                  **({"managed_fabric": managed_fabric} if managed_fabric is not None else {}),
                  "scanout_source_sha256": sha(json.dumps(inputs["functional_snapshot_sha256"], sort_keys=True).encode()),
                  "scanout_build_id": inputs["scanout"]["build_id"],
                  **artifacts, "hardware_programming": "not-performed", "boot_compatibility": "not-qualified",
                  "Linux_load_acceptance": "not-qualified", "production_admission": False}
        (directory / "linux-programming.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    return artifact


def verify_hdmi_inputs(project, inputs, hps, *, profile="scanout", board_file="fes_de25_scanout_board.v"):
    expected = inputs[profile]["hdmi_inputs"]
    names = {"fes_de25_scanout_hdmi_config.v", "fes_de25_scanout_i2c_controller.v"}
    if set(expected["derived_source_sha256"]) != names:
        raise ValueError("HDMI derived source inventory differs")
    for name, digest in expected["derived_source_sha256"].items():
        if hps.digest(project / "fes_mailbox" / name) != digest:
            raise ValueError("HDMI derived source changed: " + name)
    board = (project / "fes_mailbox" / board_file).read_text()
    board = re.sub(r"\s+", "", re.sub(r"//[^\n]*", "", board))
    if "always@(posedgeCLOCK0_50ornegedgeconfig_reset_n)beginif(!config_reset_n)beginhdmi_sda_sync<=2'b11;hdmi_int_sync<=2'b11;endelsebegin" not in board:
        raise ValueError("HDMI input synchronizers require the 50 MHz reset/clock domain")
    for name, pin in (("hdmi_sda_sync", "HDMI_I2C_SDA"), ("hdmi_int_sync", "HDMI_TX_INT")):
        if name + "<={" + name + "[0]," + pin + "};" not in board:
            raise ValueError("HDMI input first-stage binding changed")
    for binding in (".I2C_SDAT_IN(hdmi_sda_sync[1])", ".HDMI_TX_INT(hdmi_int_sync[1])"):
        if binding not in board:
            raise ValueError("HDMI input controller bypassed synchronizer")
    return {**expected, "input_clock_hz": 50000000, "input_stages": 2,
            "physical_metastability_acceptance": "not-qualified"}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "generate", "build"))
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--linux-dma", action="store_true",
                        help="select the unadmitted fixed-SMMU-stream profile; requires --hps-first")
    parser.add_argument("--hps-first", action="store_true",
                        help="capture HPS-first startup and produce explicit Linux phase artifacts")
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        selected = identity(); revision = selected.get("fes_revision") if args.action != "project" else None
        if args.action != "project" and not revision: raise ValueError("scanout generation/build requires clean committed source")
        output, combined, hps, display = prepare(args.vendor_archive, args.output, revision, hps_first=args.hps_first, linux_dma=args.linux_dma)
        print("Scanout diagnostic: " + str(output), flush=True)
        if args.action != "project":
            if identity() != selected: raise ValueError("source identity changed before generation")
            tool, version = hps.BOARD_BUILD.locate_quartus(args.quartus_root)
            generate(output, combined, hps, display, tool, version)
            if args.action == "build": print("Diagnostic SOF: " + str(compile_project(output, hps, display, tool)), flush=True)
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-scanout: " + str(error) + "\n")


if __name__ == "__main__": main()
