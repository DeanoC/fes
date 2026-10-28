#!/usr/bin/env python3
"""Prepare/regenerate the locked Terasic HPS mailbox diagnostic; never program it."""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import subprocess
import tempfile
import tomllib
import xml.etree.ElementTree as ET
import zipfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
DEFAULT_OUTPUT = HERE.parents[2] / "build/diagnostic/de25nano-hps-mailbox"
PROJECT = "golden_top"
DEVICE = "A5EB013BB23BE4SCS"
SPEC = importlib.util.spec_from_file_location("de25_blinky_builder", HERE.parent / "build.py")
BOARD_BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BOARD_BUILD)
SOURCES = ("build.py", "vendor.lock.toml", "integrate.tcl", "fes_de25_mailbox.v",
           "fes_de25_mailbox_hw.tcl", "generated/fes_de25_mailbox.vh",
           "generated/fes_de25_mailbox.hpp", "fes_de25_top_reset.v", "fes_de25_top.sdc",
           "fes_de25_ddr_probe.v", "fes_de25_ddr_probe_hw.tcl",
           "generated/fes_de25_ddr_probe.vh", "generated/fes_de25_ddr_probe.hpp")
EXCLUDED_DIRECTORIES = {"output_files", "dni", "db", "incremental_db", "greybox_tmp"}
SOURCE_SUFFIXES = {".qsys", ".ip", ".qpf", ".qsf", ".v", ".sv", ".svh", ".sdc",
                   ".tcl", ".hex", ".mif", ".info", ".sh", ".s", ".md", ".bat"}


def digest(path):
    value = hashlib.sha256()
    with Path(path).open("rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            value.update(chunk)
    return value.hexdigest()


def lock(source=None):
    data = tomllib.loads((HERE / "vendor.lock.toml").read_text() if source is None else source.decode())
    if (data.get("format") != 1 or data.get("device") != DEVICE or
            not re.fullmatch(r"[a-f0-9]{64}", data["archive"]["sha256"])):
        raise ValueError("invalid DE25 GHRD source lock")
    return data


def extract_vendor(archive, destination, selected=None):
    data = lock() if selected is None else selected
    archive = Path(archive)
    if not archive.is_file() or archive.is_symlink() or digest(archive) != data["archive"]["sha256"]:
        raise ValueError("vendor resource ZIP must be a regular file matching vendor.lock.toml")
    prefix = data["archive"]["prefix"]
    files = {}
    with zipfile.ZipFile(archive) as source:
        for info in source.infolist():
            if not info.filename.startswith(prefix) or info.is_dir():
                continue
            relative = PurePosixPath(info.filename[len(prefix):])
            if (not relative.parts or relative.is_absolute() or ".." in relative.parts or
                    "\\" in str(relative) or stat.S_IFMT(info.external_attr >> 16) not in (0, stat.S_IFREG)):
                raise ValueError("unsafe GHRD archive member: " + info.filename)
            if any(part in EXCLUDED_DIRECTORIES for part in relative.parts):
                continue
            # IP generation must use this installation rather than the vendor's
            # cached HDL, interface snapshots or generation stamps.
            if relative.suffix not in SOURCE_SUFFIXES or ("ip" in relative.parts and relative.suffix != ".ip"):
                continue
            name = relative.as_posix()
            if name in files:
                raise ValueError("duplicate GHRD archive member: " + name)
            target = destination / name
            target.parent.mkdir(parents=True, exist_ok=True)
            with source.open(info) as stream, target.open("xb") as output:
                shutil.copyfileobj(stream, output)
            files[name] = digest(target)
    for required in ("golden_top.qsf", "golden_top.v", "qsys_top.qsys",
                     "hps_subsys/hps_subsys.qsys", "peripheral_subsys/peripheral_subsys.qsys"):
        if required not in files:
            raise ValueError("vendor GHRD is missing " + required)
    return files


def mailbox_base(definition=None):
    if definition is None:
        definition = (HERE / "generated/fes_de25_mailbox.vh").read_text()
    match = re.search(r"^`define FES_DE25_MAILBOX_PERIPHERAL_OFFSET 32'h([a-f0-9]{8})$",
                      definition, re.M)
    if not match:
        raise ValueError("generated mailbox peripheral offset is missing")
    return int(match.group(1), 16)


def probe_base(definition=None):
    if definition is None:
        definition = (HERE / "generated/fes_de25_ddr_probe.vh").read_text()
    match = re.search(r"^`define FES_DE25_DDR_PROBE_PERIPHERAL_OFFSET 32'h([a-f0-9]{8})$", definition, re.M)
    if not match:
        raise ValueError("generated DDR probe peripheral offset is missing")
    return int(match.group(1), 16)


def tcl_word(value):
    value = str(value)
    for before, after in (("\\", "\\\\"), ("$", "\\$"), ("[", "\\["), ("]", "\\]"),
                          ('"', '\\"'), ("\n", "\\n"), ("\r", "\\r")):
        value = value.replace(before, after)
    return '"' + value + '"'


def source_identity():
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=normal"],
                                    cwd=ROOT, text=True)
    if dirty:
        return {"base_revision": revision, "source_classification": "working-copy-diagnostic"}
    return {"fes_revision": revision, "source_classification": "committed"}


def fes_sources(revision=None):
    paths = {name: HERE / name for name in SOURCES}
    paths["board_build.py"] = HERE.parent / "build.py"
    if revision is None:
        return {name: path.read_bytes() for name, path in paths.items()}
    if not re.fullmatch(r"[a-f0-9]{40}", revision):
        raise ValueError("selected FES revision must be a complete commit identity")
    return {name: subprocess.check_output(["git", "show", revision + ":" + path.relative_to(ROOT).as_posix()], cwd=ROOT)
            for name, path in paths.items()}


def correct_stale_constraints(project):
    path = project / "ghrd_timing.sdc"
    source = path.read_text()
    if source.count("[get_ports DDR4_REFCLK_p]") != 1:
        raise ValueError("vendor EMIF clock constraint changed; review it before building")
    top = (project / "golden_top.v").read_text()
    if not re.search(r"\.emif_hps_emif_ref_clk_0_clk\s*\(LPDDR4A_REFCLK_p\)", top):
        raise ValueError("vendor top no longer connects the reviewed EMIF reference port")
    parameters = module_parameters(project / "hps_subsys/ip/qsys_top/emif_io96b_hps.ip")
    frequencies = {value for key, value in parameters.items() if key.rsplit("/", 1)[-1] == "PHY_REFCLK_FREQ_MHZ"}
    automatic = {value for key, value in parameters.items() if key.rsplit("/", 1)[-1] == "PHY_REFCLK_FREQ_MHZ_AUTOSET_EN"}
    if frequencies != {"166.6666"} or automatic != {"false"}:
        raise ValueError("vendor EMIF reference frequency changed; review the clock contract")
    # Generated IOPLL SDC already creates its 50 MHz reference clock on this
    # input. A second MAIN_CLOCK is ignored by TimeQuest, not another clock.
    main_clock = "create_clock -name MAIN_CLOCK -period 20 [get_ports CLOCK2_50]"
    pll = module_parameters(project / "ip/pll.ip")
    if (source.splitlines().count(main_clock) != 1 or
            pll.get("gui_reference_clock_frequency") != "50.0" or
            pll.get("gui_output_clock_frequency0") != "100.0" or
            pll.get("gui_use_locked") != "true"):
        raise ValueError("vendor PLL/reference clock contract changed; review it before building")
    # The copied top connects LPDDR4A_REFCLK_p directly to the HPS EMIF
    # reference input; the copied EMIF IP fixes PHY_REFCLK_FREQ_MHZ=166.6666.
    # Keep that frequency. Delete only obsolete exceptions whose names have
    # no matching top/net; never retarget them onto active LED or reset nets.
    source = source.replace("[get_ports DDR4_REFCLK_p]", "[get_ports LPDDR4A_REFCLK_p]")
    source = source.replace("# 50MHz board input clock, 150MHz for EMIF refclk",
                            "# 50MHz board input and pinned 166.6666MHz LPDDR4A EMIF reference")
    lines = source.splitlines()
    removed = [line for line in lines if any(name in line for name in
               ("CPU_RESET_n", "LEDR[*]", "fpga_reset_n_debounced")) and line.startswith("set_false_path")]
    if len(removed) != 4:
        raise ValueError("vendor obsolete constraint set changed; review it before building")
    source = "\n".join(line for line in lines if line not in removed and line != main_clock) + "\n"
    path.write_text(source)
    return {"emif_clock_port": {"before": "DDR4_REFCLK_p", "after": "LPDDR4A_REFCLK_p",
                                "frequency_mhz": "166.6666"}, "removed_unmatched_exceptions": removed,
            "removed_duplicate_reference_clock": main_clock,
            "reference_clock_owner": "generated IOPLL SDC (50 MHz)"}


def repair_top_reset(project):
    """Patch only the reviewed vendor wrapper; preserve generated controllers."""
    path = project / "golden_top.v"
    source = path.read_text()
    replacements = (
        ("wire                   system_reset_n;",
         "wire                   pll_locked;\nwire                   top_reset_n;"),
        ("assign                 system_reset_n = ~ninit_done;",
         "// Configuration and PLL-lock qualification are inside fes_top_reset_inst."),
        (".locked   ()", ".locked   (pll_locked)"),
        (".reset_reset_n                             (system_reset_n)",
         ".reset_reset_n                             (top_reset_n)"),
        (".reset_n                                   (system_reset_n)",
         ".reset_n                                   (top_reset_n)"),
        ("always @(posedge system_clk_100_internal or negedge system_reset_n) begin\n  if (~system_reset_n)",
         "always @(posedge system_clk_100_internal or negedge top_reset_n) begin\n  if (~top_reset_n)"),
        ("// Debounce logic to clean out glitches within 1ms",
         "// Common reset qualifies PLL lock; generated controllers retain their own release.\n"
         "fes_de25_top_reset fes_top_reset_inst (\n"
         "  .clk (system_clk_100_internal),\n"
         "  .ninit_done (ninit_done),\n"
         "  .pll_locked (pll_locked),\n"
         "  .reset_n (top_reset_n)\n);\n\n"
         "// Debounce logic to clean out glitches within 1ms"),
    )
    if source.count(".rst      (ninit_done)") != 1:
        raise ValueError("vendor PLL reset connection changed; review the reset contract")
    for before, after in replacements:
        if source.count(before) != 1:
            raise ValueError("vendor top reset patch target changed: " + before)
        source = source.replace(before, after)
    if "system_reset_n" in source:
        raise ValueError("unreviewed vendor system_reset_n use remains")
    # The active outputs are physical, human-visible LEDs, not a clocked
    # receiver. Check the pinned locations/IO standard before selecting the
    # narrow output-port timing exceptions; do not invent an output delay.
    qsf = (project / "golden_top.qsf").read_text().splitlines()
    pins = {"LED[0]": "DF35", "LED[1]": "DJ32", "LED[2]": "DN22", "LED[7]": "DP30"}
    for port, pin in pins.items():
        for expected in (f"set_location_assignment PIN_{pin} -to {port}",
                         f'set_instance_assignment -name IO_STANDARD "1.1-V" -to {port}'):
            if qsf.count(expected) != 1:
                raise ValueError("vendor LED pin contract changed: " + expected)
    path.write_text(source)
    return {"asynchronous_source": "~ninit_done & pll_locked",
            "pll_reset": "ninit_done (unchanged)",
            "subsystem_reset": "top_reset_n; generated domain controllers unchanged",
            "top_reset": "fes_top_reset_inst: two-stage 100 MHz synchronized release",
            "top_reset_consumers": ["soc_inst.reset_reset_n", "debounce_inst", "heartbeat_count"],
            "asynchronous_led_output_pins": pins,
            "patches": [{"before": before, "after": after} for before, after in replacements]}


def module_parameters(path):
    namespace = {"ip": "http://www.accellera.org/XMLSchema/IPXACT/1685-2014",
                 "altera": "http://www.altera.com/XMLSchema/IPXact2014/extensions"}
    tree = ET.parse(path)
    parameters = {item.get("parameterId"): item.findtext("ip:value", namespaces=namespace) or ""
            for item in tree.findall(".//altera:altera_module_parameters/ip:parameters/ip:parameter", namespace)}
    for module in tree.findall(".//altera:sub_module", namespace):
        prefix = module.get("{" + namespace["altera"] + "}name")
        for item in module.findall("ip:parameters/ip:parameter", namespace):
            parameters["submodule/" + prefix + "/" + item.get("parameterId")] = item.findtext("ip:value", namespaces=namespace) or ""
    return parameters


def peripheral_slave_map(project, mailbox_offset, probe_offset):
    namespace = {"ip": "http://www.accellera.org/XMLSchema/IPXACT/1685-2014"}
    source = ET.parse(project / "peripheral_subsys/peripheral_subsys.qsys")
    spaces = [s for s in source.findall(".//ip:addressSpace", namespace)
              if s.findtext("ip:name", namespaces=namespace) == "pb_cpu_0.m0"]
    if len(spaces) != 1:
        raise ValueError("vendor peripheral master address map changed")
    regions = [{"name": s.findtext("ip:name", namespaces=namespace),
                "base": int(s.findtext("ip:addressOffset", namespaces=namespace), 0),
                "span": int(s.findtext("ip:range", namespaces=namespace), 0)}
               for s in spaces[0].findall("ip:segments/ip:segment", namespace)]
    if not regions:
        raise ValueError("vendor peripheral slave map is empty")
    regions += [{"name": "fes_mailbox.avs", "base": mailbox_offset, "span": 32},
                {"name": "fes_ddr_probe.avs", "base": probe_offset, "span": 128}]
    regions.sort(key=lambda r: r["base"])
    for index, region in enumerate(regions):
        end = region["base"] + region["span"]
        if region["span"] <= 0 or region["base"] < 0 or end > 1 << 32:
            raise ValueError("invalid vendor peripheral slave region")
        if index + 1 < len(regions) and end > regions[index + 1]["base"]:
            raise ValueError("overlapping peripheral slave regions: " + region["name"])
    return regions


def prepare_project(archive, output=None, revision=None):
    # Read the selected commit's blobs once. Later working-tree or HEAD changes
    # cannot alter the RTL, vendor lock, helper, offset or integration script.
    sources = fes_sources(revision)
    selected = lock(sources["vendor.lock.toml"])
    offset = mailbox_base(sources["generated/fes_de25_mailbox.vh"].decode())
    probe_offset = probe_base(sources["generated/fes_de25_ddr_probe.vh"].decode())
    helper_version = re.search(rb'^QUARTUS_VERSION = "([0-9.]+)"$', sources["board_build.py"], re.M)
    if not helper_version or helper_version.group(1).decode() != BOARD_BUILD.QUARTUS_VERSION:
        raise ValueError("selected board builder version differs from the invoking compiler helper")
    if revision is None:
        identity = source_identity()
        identity = {"base_revision": identity.get("fes_revision", identity.get("base_revision")),
                    "source_classification": "working-copy-diagnostic"}
    else:
        identity = {"fes_revision": revision, "source_classification": "committed"}
    if output is None:
        DEFAULT_OUTPUT.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix="run-", dir=DEFAULT_OUTPUT))
    else:
        output = Path(output).resolve()
        output.mkdir(parents=True, exist_ok=False)
    project = output / "project"
    project.mkdir()
    vendor_files = extract_vendor(archive, project, selected)
    (output / "vendor-inputs.json").write_text(json.dumps({
        "archive": selected["archive"], "source_sha256": vendor_files,
    }, indent=2, sort_keys=True) + "\n")
    # Qsys and Quartus operate only on this private copy. Preserve all vendor
    # HPS, EMIF and boot data; compiler-generated products are never imported.
    if f"-name DEVICE {DEVICE}" not in (project / "golden_top.qsf").read_text():
        raise ValueError("vendor GHRD device differs from the selected board")
    slave_map = peripheral_slave_map(project, offset, probe_offset)
    constraints = correct_stale_constraints(project)
    reset = repair_top_reset(project)
    local = project / "fes_mailbox"
    local.mkdir()
    for name, data in sources.items():
        target = local / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
    integration = (local / "integrate.tcl").read_text().replace("@MAILBOX_BASE@", hex(offset))
    integration = integration.replace("@PROBE_BASE@", hex(probe_offset))
    integration = integration.replace("@PERIPHERAL_SYSTEM@", tcl_word(project / "peripheral_subsys/peripheral_subsys.qsys"))
    integration = integration.replace("@TOP_SYSTEM@", tcl_word(project / "qsys_top.qsys"))
    (project / "integrate-mailbox.tcl").write_text(integration)
    with (project / "golden_top.qsf").open("a") as source:
        source.write('\n# FES diagnostic compiler selection; hardware settings remain vendor-owned.\n'
                     f'set_global_assignment -name LAST_QUARTUS_VERSION "{BOARD_BUILD.QUARTUS_VERSION} Pro Edition"\n'
                     'set_global_assignment -name NUM_PARALLEL_PROCESSORS 2\n'
                     'set_global_assignment -name IP_SEARCH_PATHS "custom_ip/**/*;fes_mailbox"\n'
                     'set_global_assignment -name VERILOG_FILE fes_mailbox/fes_de25_top_reset.v\n'
                     'set_global_assignment -name SDC_FILE fes_mailbox/fes_de25_top.sdc\n')
    inputs = {"board": "de25_nano", "device": DEVICE, "diagnostic_only": True,
              **identity,
              "quartus_version": BOARD_BUILD.QUARTUS_VERSION,
              "vendor_archive_sha256": selected["archive"]["sha256"],
              "source_sha256": {name: digest(local / name) for name in sources},
              "mailbox_peripheral_offset": offset,
              "probe_peripheral_offset": probe_offset,
              "peripheral_slave_map": slave_map,
              "probe_dma_admission": "descriptor-bounds-only; allocation/translation/lifetime not-qualified",
              "reviewed_constraint_repairs": constraints,
              "reviewed_top_reset_repairs": reset,
              "vendor_hps_emif_parameters": {
                  name: module_parameters(project / name) for name in vendor_files
                  if name.startswith("hps_subsys/") and name.endswith(".ip")},
              "physical_mailbox_address": "not-established",
              "boot_firmware_compatibility": "not-qualified"}
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return output


def run_logged(argv, directory, log, env=None):
    print("+ " + " ".join(map(str, argv)), flush=True)
    with Path(log).open("w") as stream:
        result = subprocess.run(list(map(str, argv)), cwd=directory, stdout=stream,
                                stderr=subprocess.STDOUT, check=False, env=env)
    if result.returncode:
        raise ValueError(f"command failed ({result.returncode}); retained {log}")


def verify_mailbox_handoff(project):
    offset = mailbox_base((project / "fes_mailbox/generated/fes_de25_mailbox.vh").read_text())
    path = project / "qsys_top/qsys_top.sopcinfo"
    tree = ET.parse(path)
    modules = [item for item in tree.findall("module")
               if item.get("path") == "subsys_periph.fes_mailbox"]
    if len(modules) != 1 or modules[0].get("kind") != "fes_de25_mailbox" or modules[0].get("version") != "1.0":
        raise ValueError("generated top system lacks the selected mailbox instance")
    connections = {(item.get("kind"), item.get("start"), item.get("end")): item
                   for item in tree.findall("connection")}
    required = (("avalon", "subsys_hps_agilex_hps.lwhps2fpga", "subsys_periph_pb_cpu_0.s0"),
                ("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_mailbox.avs"),
                ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_mailbox.clk"),
                ("reset", "subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_mailbox.reset"))
    if any(identity not in connections for identity in required):
        raise ValueError("generated mailbox bridge/clock/reset connectivity differs")
    base = connections[required[1]].findtext("parameter[@name='baseAddress']/value")
    rate = connections[required[2]].findtext("parameter[@name='clockRateSysInfo']/value")
    if int(base, 0) != offset or rate != "100000000":
        raise ValueError("generated mailbox offset or vendor clock rate differs")
    return {"mailbox_instance": modules[0].get("path"), "peripheral_offset": offset,
            "clock_hz": int(rate), "physical_mailbox_address": "not-established",
            "top_system_handoff": str(path.relative_to(project)), "sha256": digest(path)}


def component_ports(module, interface):
    data = module.findtext("parameter[@name='componentDefinition']/value")
    if not data:
        raise ValueError("generated component lacks interface definition: " + module.get("path", ""))
    definition = ET.fromstring(data)
    points = [item for item in definition.findall("boundary/interfaces/interface") if item.findtext("name") == interface]
    if len(points) != 1:
        raise ValueError("generated component interface changed: " + interface)
    return {p.findtext("role"): (p.findtext("direction"), int(p.findtext("width"))) for p in points[0].findall("ports/port")}


def verify_probe_handoff(project):
    offset = probe_base((project / "fes_mailbox/generated/fes_de25_ddr_probe.vh").read_text())
    tree = ET.parse(project / "qsys_top/qsys_top.sopcinfo")
    modules = {m.get("path"): m for m in tree.findall("module")}
    probe = modules.get("subsys_periph.fes_ddr_probe")
    adapter = modules.get("subsys_hps.f2sdram_adapter")
    if probe is None or probe.get("kind") != "fes_de25_ddr_probe" or probe.get("version") != "1.0" or adapter is None:
        raise ValueError("generated DDR probe/adapter instances differ")
    connections = {(c.get("kind"), c.get("start"), c.get("end")): c for c in tree.findall("connection")}
    required = (
        ("avalon", "subsys_periph_pb_cpu_0.m0", "subsys_periph_fes_ddr_probe.avs"),
        ("avalon", "subsys_periph_fes_ddr_probe.avm", "subsys_hps_f2sdram_adapter.axi4_sub"),
        ("avalon", "subsys_hps_f2sdram_adapter.axi4_man", "subsys_hps_agilex_hps.f2sdram"),
        ("avalon", "ext_hps_f2sdram_master.expanded_master", "subsys_hps_f2sdram_adapter.axi4_sub"),
        ("clock", "subsys_periph_periph_clk.out_clk", "subsys_periph_fes_ddr_probe.clk"),
        ("reset", "subsys_periph_periph_rst_in.out_reset", "subsys_periph_fes_ddr_probe.reset"))
    if any(key not in connections for key in required):
        raise ValueError("generated DDR probe bridge/clock/reset/JTAG path differs")
    def parameter(connection, name):
        return connections[connection].findtext("parameter[@name='" + name + "']/value")
    if (int(parameter(required[0], "baseAddress"), 0) != offset or
            int(parameter(required[1], "baseAddress"), 0) != 0 or
            parameter(required[4], "clockRateSysInfo") != "100000000"):
        raise ValueError("generated DDR probe offset/clock/adapter base differs")
    ports = component_ports(probe, "avm")
    expected = {"address": ("Output", 32), "burstcount": ("Output", 5), "read": ("Output", 1),
                "byteenable": ("Output", 16), "waitrequest": ("Input", 1), "readdata": ("Input", 128),
                "readdatavalid": ("Input", 1), "response": ("Input", 2)}
    if ports != expected:
        raise ValueError("generated DDR probe read-only/response interface differs")
    target = component_ports(adapter, "axi4_sub")
    if target.get("rdata") != ("Output", 256) or target.get("araddr") != ("Input", 32) or target.get("rresp") != ("Output", 2):
        raise ValueError("generated vendor F2SDRAM width/address/response contract differs")
    return {"instance": probe.get("path"), "peripheral_offset": offset,
            "clock_hz": 100000000, "read_master_data_bits": 128, "dma_address_bits": 32,
            "response_bits": 2, "vendor_f2sdram_data_bits": 256,
            "physical_ddr_address": "not-established", "dma_admission": "not-qualified"}


def generate_project(output, tool, version):
    project = output / "project"
    install = tool.parents[2]
    qsys = install / "qsys/bin/qsys-script"
    if not qsys.is_file() or not os.access(qsys, os.X_OK):
        raise ValueError("configured Quartus installation has no qsys/bin/qsys-script")
    (output / "quartus-version.txt").write_text(version + "\n")
    run_logged([qsys, "--quartus-project=golden_top", "--search-path=custom_ip/**/*,fes_mailbox,$",
                "--script=integrate-mailbox.tcl"], project, output / "integration.log",
               env=os.environ.copy())
    generator = tool.with_name("quartus_ipgenerate")
    run_logged([generator, "--clear_ip_generation_dirs", PROJECT],
               project, output / "clear-generation.log")
    run_logged([generator, "--generate_project_ip_files", "--synthesis=verilog", PROJECT],
               project, output / "ip-generation.log")
    inputs = json.loads((output / "inputs.json").read_text())
    changes = {}
    for name, expected in inputs["vendor_hps_emif_parameters"].items():
        actual = module_parameters(project / name)
        delta = {key: {"before": value, "after": actual.get(key)}
                 for key, value in expected.items() if actual.get(key) != value}
        if delta:
            changes[name] = delta
    (output / "hps-emif-parameter-review.json").write_text(json.dumps({
        "changed_original_module_parameters": changes,
        "boot_firmware_compatibility": "not-qualified",
    }, indent=2, sort_keys=True) + "\n")
    if changes:
        raise ValueError("generation changed original HPS/EMIF parameters; retained review report")
    vendor = json.loads((output / "vendor-inputs.json").read_text())
    for name, expected in vendor["source_sha256"].items():
        if name.startswith("software/") and digest(project / name) != expected:
            raise ValueError("generation changed vendor boot data: " + name)
    handoff = verify_mailbox_handoff(project)
    handoff["ddr_probe"] = verify_probe_handoff(project)
    (output / "generation.json").write_text(json.dumps(handoff, indent=2, sort_keys=True) + "\n")


def compile_project(output, tool):
    project = output / "project"
    run_logged([tool, "--flow", "compile", PROJECT], project, output / "compile.log")
    artifact = project / "output_files/golden_top.sof"
    if not artifact.is_file() or not artifact.stat().st_size:
        raise ValueError("Quartus produced no nonempty HPS diagnostic SOF")
    files = {str(path.relative_to(project)): digest(path) for path in project.rglob("*")
             if path.is_file() and (path.suffix in (".sdc", ".sopcinfo") or "handoff" in str(path))}
    reports = {str(path.relative_to(project)): digest(path) for path in (project / "output_files").glob("*")
               if path.is_file() and path.suffix in (".rpt", ".summary")}
    record = {"diagnostic_only": True, "board": "de25_nano", "device": DEVICE,
              "fes_revision": json.loads((output / "inputs.json").read_text()).get("fes_revision"),
              "artifact": str(artifact.relative_to(output)), "sha256": digest(artifact),
              "quartus_version": (output / "quartus-version.txt").read_text().strip(),
              "command": [str(tool), "--flow", "compile", PROJECT],
              "inputs_sha256": digest(output / "inputs.json"),
              "report_sha256": reports,
              "generated_handoff_and_sdc_sha256": files,
              "timing_acceptance": "not-qualified", "hardware_acceptance": "not-run",
              "boot_firmware_compatibility": "not-qualified"}
    (output / "build.json").write_text(json.dumps(record, indent=2, sort_keys=True) + "\n")
    return artifact


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "generate", "build"))
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        identity = source_identity()
        tool, version = BOARD_BUILD.locate_quartus(args.quartus_root) if args.action != "project" else (None, None)
        if args.action != "project" and identity["source_classification"] != "committed":
            raise ValueError("HPS generation/build requires clean committed source; project permits working-copy inspection")
        revision = identity.get("fes_revision") if args.action != "project" else None
        output = prepare_project(args.vendor_archive, args.output, revision)
        print("Diagnostic project: " + str(output), flush=True)
        if tool is not None:
            if source_identity() != identity:
                raise ValueError("source identity changed while preparing the project; IP generation was not started")
            generate_project(output, tool, version)
        if args.action == "build":
            print("Diagnostic SOF: " + str(compile_project(output, tool)))
        return 0
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-hps: " + str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
