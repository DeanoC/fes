#!/usr/bin/env python3
"""Prepare or compile the standalone DE25-Nano LED diagnostic; never program it."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

BOARD = Path(__file__).resolve().parent
DEFAULT_OUTPUT = BOARD.parents[1] / "build/diagnostic/de25nano"
DEVICE = "A5EB013BB23BE4SCS"
PROJECT = "de25nano_blinky"
QUARTUS_VERSION = "25.1.1"
SOURCES = ("blinky.v", "pins.qsf", "clocks.sdc", "reset_release.ip", "build.py")


class BuildError(ValueError):
    """The diagnostic could not be prepared or compiled."""


def prepare_project(output: Path | None = None) -> Path:
    if output is None:
        DEFAULT_OUTPUT.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix="run-", dir=DEFAULT_OUTPUT))
    else:
        output = output.resolve()
        output.mkdir(parents=True, exist_ok=False)
    for name in SOURCES:
        shutil.copyfile(BOARD / name, output / name)
    (output / f"{PROJECT}.qpf").write_text(
        f'QUARTUS_VERSION = "{QUARTUS_VERSION}"\nPROJECT_REVISION = "{PROJECT}"\n',
        encoding="utf-8",
    )
    (output / f"{PROJECT}.qsf").write_text(
        'set_global_assignment -name FAMILY "Agilex 5"\n'
        f'set_global_assignment -name DEVICE {DEVICE}\n'
        f'set_global_assignment -name TOP_LEVEL_ENTITY {PROJECT}\n'
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY output_files\n'
        f'set_global_assignment -name LAST_QUARTUS_VERSION "{QUARTUS_VERSION} Pro Edition"\n'
        'set_global_assignment -name SEED 1\n'
        'set_global_assignment -name STRATIXV_CONFIGURATION_SCHEME "ACTIVE SERIAL X4"\n'
        'set_global_assignment -name ACTIVE_SERIAL_CLOCK AS_FREQ_125MHZ\n'
        'set_global_assignment -name DEVICE_INITIALIZATION_CLOCK OSC_CLK_1_125MHZ\n'
        'set_global_assignment -name RESERVE_ALL_UNUSED_PINS "AS INPUT TRI-STATED"\n'
        'set_global_assignment -name IP_FILE reset_release.ip\n'
        'set_global_assignment -name VERILOG_FILE blinky.v\n'
        'set_global_assignment -name SDC_FILE clocks.sdc\n'
        'source pins.qsf\n',
        encoding="utf-8",
    )
    inputs = {
        "board": "de25_nano", "device": DEVICE, "diagnostic_only": True,
        "quartus_version": QUARTUS_VERSION, "quartus_edition": "Pro Edition",
        "source_sha256": {name: hashlib.sha256((output / name).read_bytes()).hexdigest()
                          for name in SOURCES},
    }
    (output / "inputs.json").write_text(json.dumps(inputs, indent=2) + "\n", encoding="utf-8")
    return output


def locate_quartus(root: str) -> tuple[Path, str]:
    if not root.strip():
        raise BuildError("set QUARTUS_ROOTDIR to an explicit Quartus 25.1.1 Pro installation")
    directory = Path(root).expanduser().resolve()
    candidates = (directory / "bin/quartus_sh", directory / "quartus/bin/quartus_sh")
    tool = next((path for path in candidates if path.is_file() and os.access(path, os.X_OK)), None)
    if tool is None:
        raise BuildError("QUARTUS_ROOTDIR has no executable bin/quartus_sh or quartus/bin/quartus_sh")
    result = subprocess.run([str(tool), "--version"], text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, check=False)
    version = result.stdout.strip()
    if (result.returncode or not re.search(r"\bVersion 25\.1\.1(?![0-9.])", version)
            or "Pro Edition" not in version):
        raise BuildError(f"requires Quartus {QUARTUS_VERSION} Pro Edition; reported: {version}")
    return tool, version


def compile_project(output: Path, tool: Path, version: str) -> Path:
    ip_generator = tool.with_name("quartus_ipgenerate")
    if not ip_generator.is_file() or not os.access(ip_generator, os.X_OK):
        raise BuildError(f"Quartus installation has no executable {ip_generator}")
    (output / "quartus-version.txt").write_text(version + "\n", encoding="utf-8")
    ip_command = [str(ip_generator), "--generate_project_ip_files", "--synthesis=verilog", PROJECT]
    with (output / "ip-generation.log").open("w", encoding="utf-8") as log:
        result = subprocess.run(ip_command, cwd=output, stdout=log, stderr=subprocess.STDOUT,
                                check=False)
    if result.returncode:
        raise BuildError(f"Quartus IP generation failed ({result.returncode}); retained {output / 'ip-generation.log'}")
    command = [str(tool), "--flow", "compile", PROJECT]
    with (output / "compile.log").open("w", encoding="utf-8") as log:
        result = subprocess.run(command, cwd=output, stdout=log, stderr=subprocess.STDOUT,
                                check=False)
    if result.returncode:
        raise BuildError(f"Quartus compilation failed ({result.returncode}); retained {output / 'compile.log'}")
    artifact = output / "output_files" / f"{PROJECT}.sof"
    if not artifact.is_file() or not artifact.stat().st_size:
        raise BuildError(f"Quartus produced no nonempty SOF; retained {output}")
    record = {
        "board": "de25_nano", "device": DEVICE, "diagnostic_only": True,
        "quartus_version": version, "ip_generation_command": ip_command, "command": command,
        "artifact": artifact.relative_to(output).as_posix(),
        "sha256": hashlib.sha256(artifact.read_bytes()).hexdigest(),
        "timing_acceptance": "not-qualified", "hardware_acceptance": "not-run",
    }
    (output / "build.json").write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    return artifact


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("project", "build"))
    parser.add_argument("--output", type=Path, help="new directory; existing outputs are never overwritten")
    parser.add_argument("--quartus-root", default=os.environ.get("QUARTUS_ROOTDIR", ""))
    args = parser.parse_args(argv)
    try:
        tool, version = locate_quartus(args.quartus_root) if args.action == "build" else (None, None)
        output = prepare_project(args.output)
        print(f"Diagnostic project: {output}", flush=True)
        if tool is not None:
            print(f"Diagnostic SOF: {compile_project(output, tool, version)}")
        return 0
    except (OSError, BuildError) as error:
        parser.exit(1, f"de25nano: {error}\n")


if __name__ == "__main__":
    raise SystemExit(main())
