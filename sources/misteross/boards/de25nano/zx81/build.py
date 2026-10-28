#!/usr/bin/env python3
"""Build an FPGA-only DE25 ZX81 BASIC/HDMI diagnostic; never program hardware."""
from __future__ import annotations

import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import tomllib
import zipfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[4]
MISTEROSS = ROOT / "sources/misteross"
SPEC = importlib.util.spec_from_file_location("de25_zx81_display_tools", HERE.parent / "display/build.py")
DISPLAY = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(DISPLAY)
DEVICE = DISPLAY.DEVICE
PROJECT = "de25nano_zx81"
sha = DISPLAY.sha
pll_parameters = DISPLAY.pll_parameters
source_identity = DISPLAY.source_identity
VENDOR_FILES = DISPLAY.VENDOR_FILES
SCS_GOLDEN = DISPLAY.SCS_GOLDEN
KEY_PORTS = {"KEY[0]", "KEY[1]"}
SOURCE_PATHS = {name: HERE / name for name in ("build.py", "top.v", "keyboard.v", "clocks.sdc")}
SOURCE_PATHS.update({"display_build.py": HERE.parent / "display/build.py",
    "board_build.py": HERE.parent / "build.py",
    "vendor.lock.toml": HERE.parent / "display/vendor.lock.toml",
    "fes_de25_top_reset.v": HERE.parent / "hps/fes_de25_top_reset.v"})
SOURCE_PATHS.update({name: MISTEROSS / "cores/fes-zx81/rtl" / name for name in (
    "zx81_machine.sv", "zx81_machine_clock.v", "zx81_dpram.v", "zx81_video_720p.v",
    "zx81_model.vh", "zx8x.mif", "zx8x.hex")})
SOURCE_PATHS.update({name: MISTEROSS / "cores/fes-common/rtl/z80" / name for name in (
    "fes_z80_alu.sv", "fes_z80_engine.sv", "fes_z80_bus.sv", "fes_z80_nmos.sv")})


def capture_sources(revision=None):
    return {name: path.read_bytes() if revision is None else subprocess.check_output(
        ["git", "show", revision + ":" + path.relative_to(ROOT).as_posix()], cwd=ROOT)
        for name, path in SOURCE_PATHS.items()}


def key_assignments(source):
    """Use only the exact two key location/IO rows from the SCS board reference."""
    rows, counts = [], {}
    for line in source.splitlines():
        match = re.fullmatch(r'(set_location_assignment\s+PIN_\w+|set_instance_assignment\s+-name IO_STANDARD\s+"[^"]+")\s+-to\s+(\S+)\s*', line)
        if match and match[2] in KEY_PORTS:
            rows.append(line)
            key = (match[2], match[1].split()[0])
            counts[key] = counts.get(key, 0) + 1
    if counts != {(port, kind): 1 for port in KEY_PORTS for kind in ("set_location_assignment", "set_instance_assignment")}:
        raise ValueError("vendor key pin/IO assignment set changed")
    return "\n".join(rows) + "\n"


def machine_pll(source):
    """Derive a separate machine PLL, preserving the vendor pixel PLL unchanged."""
    result, device_repair = DISPLAY.retarget_pll(source)
    replacements = ((b"<ipxact:library>sys_pll</ipxact:library>", b"<ipxact:library>machine_pll</ipxact:library>", 2),
                    (b"<ipxact:value>74250000</ipxact:value>", b"<ipxact:value>52224000</ipxact:value>", 1),
                    (b"&lt;value&gt;74250000&lt;/value&gt;", b"&lt;value&gt;52224000&lt;/value&gt;", 1),
                    (b"<ipxact:value>74.25</ipxact:value>", b"<ipxact:value>52.224</ipxact:value>", 1),
                    (b"<ipxact:value>13468.013</ipxact:value>", b"<ipxact:value>19148.284</ipxact:value>", 1))
    for before, after, count in replacements:
        if result.count(before) != count:
            raise ValueError("vendor machine PLL derivation contract changed")
        result = result.replace(before, after)
    return result, {"device_repair": device_repair, "module": "machine_pll",
        "reference_clock_hz": 50000000, "output_clock_hz": 52224000,
        "parameter_changes": {"gui_output_clock_frequency0": {"before": "74.25", "after": "52.224"},
                              "gui_output_clock_frequency_ps0": {"before": "13468.013", "after": "19148.284"}},
        "source_sha256": sha(source), "derived_sha256": sha(result)}


def _derive(source, replacements, label):
    result = source
    for before, after, count in replacements:
        if result.count(before) != count:
            raise ValueError(label + " adaptation contract changed")
        result = result.replace(before, after)
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
        "changes": [{"before": a.decode(), "after": b.decode(), "count": n} for a, b, n in replacements]}


def retarget_ram(source):
    return _derive(source, ((b'intended_device_family = "Cyclone V"', b'intended_device_family = "Agilex 5"', 1),
        (b'ram_block_type = "M10K"', b'ram_block_type = "M20K"', 1),
        (b'ramstyle = "M10K"', b'ramstyle = "M20K"', 1)), "ZX81 dual-port RAM")


def retarget_video(source):
    return _derive(source, ((b'(* ramstyle = "M10K" *) reg fb [0:(270 * 512) - 1];',
        b'(* ramstyle = "M20K" *) reg fb [0:(270 * 512) - 1];', 1),), "ZX81 video RAM")


def prepare(archive, output=None, revision=None):
    sources = capture_sources(revision)
    lock = tomllib.loads(sources["vendor.lock.toml"].decode())
    archive = Path(archive)
    if lock["format"] != 1 or lock["device"] != DEVICE or not archive.is_file() or archive.is_symlink():
        raise ValueError("vendor ZIP must be a regular file matching display/vendor.lock.toml")
    archive_bytes = archive.read_bytes()
    if sha(archive_bytes) != lock["archive"]["sha256"]:
        raise ValueError("vendor ZIP must match display/vendor.lock.toml")
    with zipfile.ZipFile(io.BytesIO(archive_bytes)) as package:
        prefix = lock["archive"]["prefix"]
        if any(package.namelist().count(name) != 1 for name in [SCS_GOLDEN, *(prefix + name for name in VENDOR_FILES)]):
            raise ValueError("missing/duplicate vendor display source")
        vendor = {name: package.read(prefix + name) for name in VENDOR_FILES}
        baseline = package.read(SCS_GOLDEN).decode()
    pins = DISPLAY.pin_assignments(vendor["golden_top.qsf"].decode())
    if DISPLAY.pin_assignments(baseline) != pins or not re.search(r"-name DEVICE " + DEVICE + r"\s", baseline):
        raise ValueError("HDMI reference pins differ from the SCS golden board")
    pins += key_assignments(baseline)
    pixel, pixel_repair = DISPLAY.retarget_pll(vendor["sys_pll.ip"])
    machine, machine_repair = machine_pll(vendor["sys_pll.ip"])
    reset, reset_repair = DISPLAY.retarget_reset(vendor["agilex_reset_release.ip"])
    config = vendor["HDMI_I2C/I2C_HDMI_Config.v"].decode()
    expected = (r"parameter\s+CLK_Freq\s*=\s*50000000\s*;", r"parameter\s+I2C_Freq\s*=\s*20000\s*;",
                r"if\(\s*mI2C_CLK_DIV\s*<\s*\(CLK_Freq/I2C_Freq\)\s*\)",
                r"mI2C_CLK_DIV\s*<=\s*0\s*;\s*mI2C_CTRL_CLK\s*<=\s*~mI2C_CTRL_CLK")
    if any(not re.search(pattern, config) for pattern in expected):
        raise ValueError("vendor I2C clock divider contract changed")
    ram, ram_repair = retarget_ram(sources["zx81_dpram.v"])
    video, video_repair = retarget_video(sources["zx81_video_720p.v"])
    if output is None:
        directory = MISTEROSS / "build/diagnostic/de25nano-zx81"
        directory.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix="run-", dir=directory))
    else:
        output = Path(output).resolve()
        output.mkdir(parents=True, exist_ok=False)
    project = output / "project"
    project.mkdir()
    for name, data in vendor.items():
        path = output / "vendor" / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    (output / "vendor/scs-golden.qsf").write_text(baseline)
    for name, data in sources.items():
        (project / name).write_bytes(data)
    for name, data in {"sys_pll.ip": pixel, "machine_pll.ip": machine, "agilex_reset_release.ip": reset,
        "I2C_HDMI_Config.v": vendor["HDMI_I2C/I2C_HDMI_Config.v"], "I2C_Controller.v": vendor["HDMI_I2C/I2C_Controller.v"],
        "zx81_dpram_de25.v": ram, "zx81_video_720p_de25.v": video}.items():
        (project / name).write_bytes(data)
    (project / "pins.qsf").write_text(pins)
    (project / (PROJECT + ".qpf")).write_text(f'QUARTUS_VERSION = "26.3.0"\nPROJECT_REVISION = "{PROJECT}"\n')
    assignments = (("FAMILY", '"Agilex 5"'), ("DEVICE", DEVICE), ("TOP_LEVEL_ENTITY", PROJECT),
        ("PROJECT_OUTPUT_DIRECTORY", "output_files"), ("NUM_PARALLEL_PROCESSORS", "2"), ("SEED", "1"),
        ("STRATIXV_CONFIGURATION_SCHEME", '"ACTIVE SERIAL X4"'), ("ACTIVE_SERIAL_CLOCK", "AS_FREQ_125MHZ"),
        ("DEVICE_INITIALIZATION_CLOCK", "OSC_CLK_1_125MHZ"), ("RESERVE_ALL_UNUSED_PINS", '"AS INPUT TRI-STATED"'),
        ("VERILOG_MACRO", "QUARTUS=1"), ("IP_FILE", "sys_pll.ip"), ("IP_FILE", "machine_pll.ip"),
        ("IP_FILE", "agilex_reset_release.ip"), ("SDC_FILE", "clocks.sdc"))
    verilog = ("top.v", "keyboard.v", "fes_de25_top_reset.v", "I2C_HDMI_Config.v", "I2C_Controller.v", "zx81_machine_clock.v",
               "zx81_dpram_de25.v", "zx81_video_720p_de25.v")
    systemverilog = ("fes_z80_alu.sv", "fes_z80_engine.sv", "fes_z80_bus.sv", "fes_z80_nmos.sv", "zx81_machine.sv")
    (project / (PROJECT + ".qsf")).write_text("\n".join(
        "set_global_assignment -name " + key + " " + value for key, value in assignments) + "\n" + "\n".join(
        "set_global_assignment -name " + kind + " " + name for kind, names in (
            ("VERILOG_FILE", verilog), ("SYSTEMVERILOG_FILE", systemverilog)) for name in names) + "\nsource pins.qsf\n")
    identity = {"fes_revision": revision, "source_classification": "committed"} if revision else source_identity()
    if revision is None:
        identity = {"base_revision": identity.get("fes_revision", identity.get("base_revision")), "source_classification": "working-copy-diagnostic"}
    inputs = {**identity, "board": "de25_nano", "device": DEVICE, "diagnostic_only": True,
        "source_sha256": {name: sha(data) for name, data in sources.items()}, "vendor_archive": lock["archive"],
        "vendor_source_sha256": {name: sha(data) for name, data in vendor.items()},
        "reviewed_pixel_pll_device_repair": pixel_repair, "reviewed_machine_pll_derivation": machine_repair,
        "reviewed_reset_release_device_repair": reset_repair, "reviewed_ram_derivation": ram_repair,
        "reviewed_video_ram_derivation": video_repair, "system_clock_hz": 52224000,
        "video": {"pixel_clock_hz": 74250000, "h_total": 1650, "v_total": 750},
        "hps": False, "ddr": False, "gp": False, "runtime_package": False,
        "boot_firmware_compatibility": "not-qualified", "hardware_acceptance": "not-run",
        "prepared_project_sha256": {path.name: sha(path.read_bytes()) for path in project.iterdir() if path.is_file()}}
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
    return output


def compile_project(output, tool, version):
    project = output / "project"
    (output / "quartus-version.txt").write_text(version + "\n")
    generate = [tool.with_name("quartus_ipgenerate"), "--generate_project_ip_files", "--synthesis=verilog", PROJECT]
    ip_names = ("sys_pll.ip", "machine_pll.ip", "agilex_reset_release.ip")
    expected = {name: pll_parameters((project / name).read_bytes()) for name in ip_names}
    DISPLAY.run(generate, project, output / "ip-generation.log")
    changes = {name: {key: {"before": params.get(key), "after": actual.get(key)}
                     for key in params.keys() | actual.keys() if params.get(key) != actual.get(key)}
               for name, params in expected.items()
               if (actual := pll_parameters((project / name).read_bytes())) != params}
    (output / "ip-parameter-review.json").write_text(json.dumps(changes, indent=2, sort_keys=True) + "\n")
    if changes:
        raise ValueError("IP generation changed reviewed PLL/reset parameters")
    command = [tool, "--flow", "compile", PROJECT]
    DISPLAY.run(command, project, output / "compile.log")
    if any(pll_parameters((project / name).read_bytes()) != params for name, params in expected.items()):
        raise ValueError("compile changed reviewed PLL/reset parameters")
    artifact = project / "output_files" / (PROJECT + ".sof")
    if not artifact.is_file() or not artifact.stat().st_size:
        raise ValueError("Quartus produced no nonempty ZX81 SOF")
    receipt = {"fes_revision": json.loads((output / "inputs.json").read_text())["fes_revision"],
        "artifact": str(artifact.relative_to(output)), "sha256": sha(artifact.read_bytes()),
        "inputs_sha256": sha((output / "inputs.json").read_bytes()), "quartus_version": version,
        "command": list(map(str, command)), "ip_generation_command": list(map(str, generate)),
        "reports_sha256": {str(path.relative_to(project)): sha(path.read_bytes()) for path in (project / "output_files").iterdir()
                            if path.suffix in (".rpt", ".summary", ".pin")},
        "generated_sdc_sha256": {str(path.relative_to(project)): sha(path.read_bytes()) for path in project.rglob("*.sdc")},
        "diagnostic_only": True, "timing_acceptance": "not-qualified", "hardware_acceptance": "not-run",
        "boot_firmware_compatibility": "not-qualified"}
    (output / "build.json").write_text(json.dumps(receipt, indent=2, sort_keys=True) + "\n")
    return artifact


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "build"))
    parser.add_argument("--vendor-archive", type=Path, required=True)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        identity = source_identity()
        revision = identity.get("fes_revision") if args.action == "build" else None
        if args.action == "build" and revision is None:
            raise ValueError("ZX81 build requires clean committed source")
        tool, version = DISPLAY.TOOLS.locate_quartus(args.quartus_root) if revision else (None, None)
        output = prepare(args.vendor_archive, args.output, revision)
        print("Diagnostic project: " + str(output), flush=True)
        if tool:
            if source_identity() != identity:
                raise ValueError("source identity changed before IP generation")
            print("Diagnostic SOF: " + str(compile_project(output, tool, version)), flush=True)
    except (OSError, ValueError, KeyError, zipfile.BadZipFile, subprocess.CalledProcessError) as error:
        parser.exit(1, "de25-zx81: " + str(error) + "\n")


if __name__ == "__main__":
    main()
