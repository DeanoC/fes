#!/usr/bin/env python3
"""Quartus 17.0.2 SDRAM diagnostic at 100 or 130 MHz.

Set RAMTEST_MHZ=100 for the 100 MHz build; the default is 130 MHz. Each
build writes a comparison package, RBF and TimeQuest report in its own
directory. The OSS seal stays on the 50 MHz pin clock.
"""
from __future__ import annotations

import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.core_package import encode_manifest, read_package
from scripts.export_core_package import _archive_bytes
from scripts.quartus_tools import locate_quartus, quartus_version_line


ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
MHZ = os.environ.get("RAMTEST_MHZ", "130")
if MHZ not in ("100", "130"):
    raise ValueError("RAMTEST_MHZ must be 100 or 130")
OUTPUT_RELATIVE = Path("build/fes-ramtest-quartus" + ("-100" if MHZ == "100" else ""))
QSF_PINS = "cores/fes-ramtest/constraints.qsf"
I2C_SITE = "HPSINTERFACEPERIPHERALI2C_X52_Y60_N111"
# Stamped into the bitstream and the comparison package. The kit probe
# rejects a load when these differ.
BUILD_ID = hashlib.sha256(f"fes.ramtest quartus {MHZ} MHz".encode()).hexdigest()[:32]
VERILOG_SOURCES = (
    "cores/fes-pong/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_application_gp.v",
    "cores/fes-common/rtl/fes_video_720p.v",
    "cores/fes-ramtest/rtl/mem_channel.v",
    "cores/fes-ramtest/rtl/ram_font.v",
    "cores/fes-ramtest/rtl/ram_display.v",
    "cores/fes-ramtest/rtl/ram_pll.v",
    "cores/fes-ramtest/rtl/sdram_addon_port.v",
    "cores/fes-ramtest/rtl/hps_ddr_port.v",
    "cores/fes-ramtest/rtl/top.v",
)
PROJECT_SDC = (
    "# 50 MHz reference, video PLL, and selected memory/capture PLL clocks.\n"
    "# Cross-domain button and reset bits are synchronised.\n"
    "create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]\n"
    "derive_pll_clocks\n"
    "set_clock_groups -asynchronous \\\n"
    "    -group [get_clocks FPGA_CLK1_50] \\\n"
    "    -group [get_clocks {video_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk}] \\\n"
    "    -group [get_clocks {ram_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk}] \\\n"
    "    -group [get_clocks {ram_clock|pll|general[1].gpll~PLL_OUTPUT_COUNTER|divclk}] \\\n"
    "    -group [get_clocks {ram_clock|pll|general[2].gpll~PLL_OUTPUT_COUNTER|divclk}]\n"
)


class BuildError(ValueError):
    """Raised when the Quartus RAM-tester comparison cannot be compiled."""


def project_qsf(root: Path, project: Path) -> str:
    rel = Path(os.path.relpath(root, project)).as_posix()

    def assignment(kind: str, relative: str) -> str:
        return f'set_global_assignment -name {kind} "{rel}/{relative}"'

    lines = [
        'set_global_assignment -name FAMILY "Cyclone V"',
        f"set_global_assignment -name DEVICE {TARGET}",
        f"set_global_assignment -name TOP_LEVEL_ENTITY {TOP}",
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY "output_files"',
        "set_global_assignment -name GENERATE_RBF_FILE ON",
        "set_global_assignment -name IGNORE_PARTITIONS ON",
        "set_global_assignment -name NUM_PARALLEL_PROCESSORS ALL",
        "set_global_assignment -name SEED 1",
        "set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005",
        'set_global_assignment -name LAST_QUARTUS_VERSION "17.0.2 Lite Edition"',
        f'set_global_assignment -name SEARCH_PATH "{rel}/cores/fes-common/generated"',
        'set_global_assignment -name VERILOG_MACRO "QUARTUS"',
        'set_global_assignment -name VERILOG_MACRO "RAM_RATE_SWEEP"',
        f'set_global_assignment -name VERILOG_MACRO "RAM_{MHZ}_ONLY"',
        f'set_global_assignment -name VERILOG_MACRO "RAMTEST_BUILD_ID=128\'h{BUILD_ID}"',
        'set_global_assignment -name SDC_FILE "clocks.sdc"',
        (
            "set_instance_assignment -name HPS_LOCATION "
            f"{I2C_SITE} -entity {TOP} -to hdmi_i2c"
        ),
    ]
    lines.extend(
        f"set_instance_assignment -name FAST_OUTPUT_REGISTER ON -to {pin}"
        for pin in ("SDRAM_CKE", "SDRAM_nCS", "SDRAM_nRAS", "SDRAM_nCAS",
                    "SDRAM_nWE", "SDRAM_BA*", "SDRAM_A*")
    )
    lines.extend(assignment("VERILOG_FILE", source) for source in VERILOG_SOURCES)
    pins = (root / QSF_PINS).read_text(encoding="utf-8")
    return "\n".join(lines) + "\n\n" + pins + "\n"


def write_project(root: Path, output: Path) -> Path:
    project = output / "project"
    if project.exists():
        shutil.rmtree(project)
    project.mkdir(parents=True)
    (project / "top.qpf").write_text(
        'QUARTUS_VERSION = "17.0";\nPROJECT_REVISION = "top";\n',
        encoding="utf-8",
    )
    (project / "clocks.sdc").write_text(PROJECT_SDC, encoding="utf-8")
    (project / "top.qsf").write_text(project_qsf(root, project), encoding="utf-8")
    return project


def validate_timing(sta_text: str) -> None:
    for needle in ("74.27 MHz",):
        if needle not in sta_text:
            raise BuildError(f"timing report does not mention {needle}")
    if re.search(rf"\b{MHZ}(?:\.\d+)? MHz", sta_text) is None:
        raise BuildError(f"timing report does not mention {MHZ} MHz")
    # Same-clock memory slack is the rate result. The pixel path is reported
    # separately; a miss there does not hide a memory miss.
    setup = re.search(
        r"Slow 1100mV 100C Model Setup Summary.*?Clock\s+; Slack(.*?)\n\n",
        sta_text,
        re.S,
    )
    if setup is None:
        raise BuildError("timing report lacks the slow-corner setup summary")
    memory_rows = [
        line for line in setup.group(1).splitlines()
        if ("ram_clock" in line and "divclk" in line)
        or line.strip().startswith("; FPGA_CLK1_50")
    ]
    if len(memory_rows) < 2:
        raise BuildError(f"timing report lacks the 50 and {MHZ} MHz setup rows")
    for line in memory_rows:
        slack = float(line.split(";")[2])
        print(f"memory setup slack {slack:.3f} ns")
        if slack < 0:
            raise BuildError(f"memory setup slack is {slack} ns: {line.strip()}")
    pixel_rows = [
        line for line in setup.group(1).splitlines()
        if "video_clock" in line and "divclk" in line
    ]
    if len(pixel_rows) != 1:
        raise BuildError("timing report lacks the pixel setup row")
    pixel_slack = float(pixel_rows[0].split(";")[2])
    print(f"pixel setup slack {pixel_slack:.3f} ns")
    if pixel_slack < 0:
        raise BuildError(f"pixel setup slack is {pixel_slack} ns")


def compile(root: Path | None = None) -> Path:
    root = (root or ROOT).resolve()
    quartus_root, quartus_sh = locate_quartus(os.environ.get("QUARTUS_ROOTDIR", ""), root)
    version = quartus_version_line(quartus_sh)
    output = root / OUTPUT_RELATIVE
    output.mkdir(parents=True, exist_ok=True)
    project = write_project(root, output)
    env = os.environ.copy()
    env["QUARTUS_ROOTDIR"] = str(quartus_root)
    log = output / "quartus.log"
    runner = root / "scripts" / "run_logged.sh"
    result = subprocess.run(
        [str(runner), str(log), str(quartus_sh), "--flow", "compile", TOP],
        cwd=project,
        env=env,
        check=False,
    )
    if result.returncode != 0:
        raise BuildError(f"Quartus compile failed (exit {result.returncode}); log: {log}")
    sta_built = project / "output_files" / "top.sta.rpt"
    rbf_built = project / "output_files" / "top.rbf"
    if not sta_built.is_file() or not rbf_built.is_file():
        raise BuildError("Quartus compile did not write the timing report and RBF")
    sta_text = sta_built.read_text(encoding="utf-8", errors="replace")
    validate_timing(sta_text)
    shutil.copyfile(sta_built, output / "top.sta.rpt")
    shutil.copyfile(rbf_built, output / "core.rbf")
    (output / "quartus-version.log").write_text(version + "\n", encoding="utf-8")
    package = write_package(root, output / "core.rbf", output / "comparison.fcore")
    print(version)
    print(f"rbf: {output / 'core.rbf'}")
    print(f"timing: {output / 'top.sta.rpt'}")
    print(f"package: {package}")
    return output


def write_package(root: Path, rbf_path: Path, archive_path: Path) -> Path:
    """Publish a comparison package whose build id matches the bitstream."""

    rbf = rbf_path.read_bytes()
    revision = subprocess.check_output(
        ["git", "-C", str(root.parents[1]), "rev-parse", "HEAD"], text=True
    ).strip()
    recipe = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    manifest = encode_manifest({
        "format": 2,
        "core": {
            "id": "fes.ramtest",
            "name": "FES RAM Tester",
            "description": f"Quartus comparison that pattern-tests the SDRAM addon at {MHZ} MHz",
            "version": "1.0.0",
        },
        "target": {"platform": "de10_nano", "device": TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", "size": len(rbf), "sha256": hashlib.sha256(rbf).hexdigest()},
        "abi": {"id": "fes.application", "major": 1, "minor": 0},
        "interfaces": [
            {"id": "fes.video.fixed-720p60", "major": 1, "minor": 0, "required": True},
            {"id": "fes.gamepad", "major": 1, "minor": 0, "required": True},
        ],
        "build": {
            "id": BUILD_ID,
            "repository": "https://github.com/DeanoC/fes",
            "revision": revision,
            "recipe_sha256": recipe,
            "toolchain": "quartus 17.0.2",
        },
    })
    archive_path.write_bytes(_archive_bytes(manifest, rbf))
    if read_package(archive_path).fields["build"]["id"] != BUILD_ID:
        raise BuildError("comparison package build id does not match the bitstream")
    return archive_path


def main() -> None:
    compile()


if __name__ == "__main__":
    main()
