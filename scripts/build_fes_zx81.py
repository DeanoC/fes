#!/usr/bin/env python3
"""Build and seal the Quartus bring-up FES ZX81 package."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package
from scripts.rebuild_core import (
    RebuildError,
    locate_quartus,
    quartus_version_line,
)


ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
OUTPUT_RELATIVE = Path("build/fes-zx81-quartus")
RECIPE = "scripts/build_fes_zx81.py"
ABI_DEFINITION = "cores/fes-zx81/generated/fes_simple_computer.vh"
QSF_PINS = "cores/fes-zx81/constraints.qsf"
SDC = "cores/fes-zx81/clocks.sdc"
ROM_HEX = "cores/fes-zx81/rtl/zx8x.hex"
ROM_MIF = "cores/fes-zx81/rtl/zx8x.mif"
VERILOG_SOURCES = (
    "cores/fes-zx81/rtl/sys_pll.v",
    "cores/fes-zx81/rtl/pixel_pll.v",
    "cores/fes-zx81/rtl/fes_computer_gp.v",
    "cores/fes-zx81/rtl/zx81_dpram.v",
    "cores/fes-zx81/rtl/zx81_video_720p.v",
    "cores/fes-zx81/rtl/top.v",
)
SYSTEMVERILOG_SOURCES = (
    "cores/fes-zx81/rtl/zx81_machine.sv",
)
VHDL_SOURCES = (
    "cores/fes-zx81/rtl/t80/T80_Reg.vhd",
    "cores/fes-zx81/rtl/t80/T80_ALU.vhd",
    "cores/fes-zx81/rtl/t80/T80_MCode.vhd",
    "cores/fes-zx81/rtl/t80/T80.vhd",
    "cores/fes-zx81/rtl/t80/T80pa.vhd",
)
PINNED_INPUTS = (
    RECIPE,
    ABI_DEFINITION,
    QSF_PINS,
    SDC,
    ROM_HEX,
    ROM_MIF,
    *VERILOG_SOURCES,
    *SYSTEMVERILOG_SOURCES,
    *VHDL_SOURCES,
)
HEX32_RE = re.compile(r"[0-9a-f]{32}\Z")
HEX40_RE = re.compile(r"[0-9a-f]{40}\Z")
HEX64_RE = re.compile(r"[0-9a-f]{64}\Z")
I2C_SITE = "HPSINTERFACEPERIPHERALI2C_X52_Y60_N111"
TIMING_CATEGORIES = ("Setup", "Hold", "Recovery", "Removal", "Minimum Pulse Width")
WORST_SLACK_RE = re.compile(
    r"Worst-case Slack\s*;\s*([0-9.]+)\s*;\s*([0-9.]+)\s*;\s*([0-9.]+)\s*;\s*([0-9.]+)\s*;\s*([0-9.]+)"
)
DESIGN_TNS_RE = re.compile(
    r"Design-wide TNS\s*;\s*([0-9.]+)\s*;\s*([0-9.]+)\s*;\s*([0-9.]+)\s*;\s*([0-9.]+)\s*;\s*([0-9.]+)"
)


class BuildError(ValueError):
    """Raised when the Quartus ZX81 recipe cannot produce a sealed package."""


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _git(root: Path, *arguments: str) -> str:
    try:
        result = subprocess.run(
            ["git", "-C", str(root), *arguments],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=True,
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        detail = exc.stderr.strip() if isinstance(exc, subprocess.CalledProcessError) else str(exc)
        raise BuildError(f"Git source verification failed: {detail}") from exc
    return result.stdout.strip()


def _contains_symlink(root: Path, relative: str) -> bool:
    current = root
    for part in Path(relative).parts:
        current /= part
        if current.is_symlink():
            return True
    return False


def _regular_input(root: Path, relative: str) -> Path:
    path = root / relative
    if _contains_symlink(root, relative) or not path.is_file():
        raise BuildError(f"pinned input must be a regular non-symlink file: {relative}")
    return path


def require_clean_source(root: Path) -> tuple[str, str]:
    root = Path(root).resolve()
    actual_root = Path(_git(root, "rev-parse", "--show-toplevel")).resolve()
    if actual_root != root:
        raise BuildError(f"source root does not match Git checkout root: {root}")
    revision = _git(root, "rev-parse", "HEAD")
    if HEX40_RE.fullmatch(revision) is None:
        raise BuildError("source HEAD is not a full lowercase Git commit")
    if _git(root, "status", "--porcelain", "--untracked-files=all"):
        raise BuildError("source checkout must be clean before build and export")
    repositories = _git(root, "remote", "get-url", "--all", "origin").splitlines()
    if len(repositories) != 1:
        raise BuildError("source checkout must have exactly one origin URL")
    for relative in PINNED_INPUTS:
        _regular_input(root, relative)
        try:
            _git(root, "ls-files", "--error-unmatch", "--", relative)
        except BuildError as exc:
            raise BuildError(f"pinned build input is not tracked: {relative}") from exc
    return repositories[0], revision


def authenticate_quartus(root: Path) -> tuple[Path, Path, str, str]:
    try:
        quartus_root, quartus_sh = locate_quartus(os.environ.get("QUARTUS_ROOTDIR", ""), root)
        version = quartus_version_line(quartus_sh)
    except RebuildError as exc:
        raise BuildError(str(exc)) from exc
    digest = _sha256(quartus_sh)
    identity = f"version=17.0.2; sha256={digest}"
    return quartus_root, quartus_sh, version, identity


def create_build_record(
    root: Path,
    repository: str,
    revision: str,
    tool_identities: Mapping[str, str],
) -> bytes:
    root = Path(root)
    fields = {
        "format": 1,
        "repository": repository,
        "revision": revision,
        "recipe": RECIPE,
        "recipe_sha256": _sha256(_regular_input(root, RECIPE)),
        "abi_definition": ABI_DEFINITION,
        "abi_definition_sha256": _sha256(_regular_input(root, ABI_DEFINITION)),
        "dependencies": {},
        "tools": dict(tool_identities),
        "parameters": {
            "compiler": "quartus-17.0.2",
            "device": TARGET,
            "pixel_clock_hz": 74_250_000,
            "reference_clock_hz": 50_000_000,
            "seed": 1,
            "sys_clock_hz": 52_000_000,
            "top": TOP,
        },
    }
    return encode_build_record(fields)


def project_qsf(root: Path, project: Path, build_id: str) -> str:
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    root = Path(root).resolve()
    project = Path(project).resolve()
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
        f'set_global_assignment -name SEARCH_PATH "{rel}/cores/fes-zx81/generated"',
        'set_global_assignment -name VERILOG_MACRO "QUARTUS=1"',
        f'set_global_assignment -name VERILOG_MACRO "FES_ZX81_BUILD_ID=128\'h{build_id}"',
        assignment("SDC_FILE", SDC),
    ]
    for source in VHDL_SOURCES:
        lines.append(assignment("VHDL_FILE", source))
    for source in VERILOG_SOURCES:
        lines.append(assignment("VERILOG_FILE", source))
    for source in SYSTEMVERILOG_SOURCES:
        lines.append(assignment("SYSTEMVERILOG_FILE", source))
    pins = _regular_input(root, QSF_PINS).read_text(encoding="utf-8")
    return "\n".join(lines) + "\n\n" + pins + "\n"


def write_project(root: Path, output: Path, build_id: str) -> Path:
    root = Path(root).resolve()
    output = Path(output).resolve()
    if output != root / OUTPUT_RELATIVE:
        raise BuildError(f"FES ZX81 Quartus output must be {root / OUTPUT_RELATIVE}")
    project = output / "project"
    if project.exists():
        shutil.rmtree(project)
    project.mkdir(parents=True)
    (project / "top.qpf").write_text(
        'QUARTUS_VERSION = "17.0";\nPROJECT_REVISION = "top";\n',
        encoding="utf-8",
    )
    (project / "top.qsf").write_text(project_qsf(root, project, build_id), encoding="utf-8")
    rom_dir = project / "cores" / "fes-zx81" / "rtl"
    rom_dir.mkdir(parents=True)
    shutil.copyfile(_regular_input(root, ROM_HEX), rom_dir / "zx8x.hex")
    shutil.copyfile(_regular_input(root, ROM_MIF), rom_dir / "zx8x.mif")
    return project


def compile_command(quartus_sh: Path) -> tuple[str, ...]:
    return (str(quartus_sh), "--flow", "compile", TOP)


def _write_atomic(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    except Exception:
        temporary.unlink(missing_ok=True)
        raise


def _prepare_output(root: Path) -> Path:
    output = root / OUTPUT_RELATIVE
    output.mkdir(parents=True, exist_ok=True)
    return output


def require_clocks(sta_text: str) -> None:
    if "52.0" not in sta_text and "52.00" not in sta_text:
        raise BuildError("timing report does not mention the 52 MHz system clock")
    if "74.25" not in sta_text and "74.27" not in sta_text:
        raise BuildError("timing report does not mention the 74.25 MHz pixel clock")


def validate_timing_report(sta_text: str) -> list[dict[str, object]]:
    slack_match = WORST_SLACK_RE.search(sta_text)
    tns_match = DESIGN_TNS_RE.search(sta_text)
    if slack_match is None or tns_match is None:
        raise BuildError("timing summary lacks required analysis categories")
    result = []
    for name, slack_text, tns_text in zip(TIMING_CATEGORIES, slack_match.groups(), tns_match.groups()):
        try:
            slack, tns = float(slack_text), float(tns_text)
        except ValueError as exc:
            raise BuildError("invalid timing number") from exc
        if not all(math.isfinite(value) and value >= 0 for value in (slack, tns)):
            raise BuildError("timing summary contains an unsupported or failing result")
        result.append({"type": name, "slack_ns": slack, "tns_ns": tns})
    return result


def validate_quartus_evidence(output: Path, project: Path) -> dict:
    rbf_built = project / "output_files" / "top.rbf"
    sta = project / "output_files" / "top.sta.rpt"
    if rbf_built.is_symlink() or not rbf_built.is_file() or not 1 <= rbf_built.stat().st_size <= MAX_PAYLOAD_SIZE:
        raise BuildError(f"RBF must be a nonempty bounded regular file: {rbf_built}")
    if sta.is_symlink() or not sta.is_file():
        raise BuildError(f"missing TimeQuest report: {sta}")
    sta_text = sta.read_text(encoding="utf-8", errors="replace")
    timing = validate_timing_report(sta_text)
    require_clocks(sta_text)
    qsf_text = (project / "top.qsf").read_text(encoding="utf-8")
    if I2C_SITE not in qsf_text:
        raise BuildError("Quartus project does not pin the FES Pong HDMI I2C site")
    shutil.copyfile(rbf_built, output / "core.rbf")
    shutil.copyfile(sta, output / "top.sta.rpt")
    rbf = output / "core.rbf"
    return {
        "status": "pass",
        "timing": timing,
        "rbf": {"sha256": _sha256(rbf), "size": rbf.stat().st_size},
    }


def _manifest(record: bytes, evidence: dict, repository: str, revision: str, toolchain: str) -> bytes:
    record_fields = json.loads(record)
    rbf = evidence["rbf"]
    fields = {
        "format": 2,
        "core": {
            "id": "fes.zx81",
            "name": "FES ZX81",
            "description": "Quartus bring-up ZX81 computer for the FES simple-computer ABI",
            "version": "1.0.0",
        },
        "target": {
            "platform": "de10_nano",
            "device": TARGET,
            "programming_profile": "fes-gp-v1",
        },
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.simple-computer", "major": 1, "minor": 0},
        "interfaces": [
            {"id": "fes.keyboard", "major": 1, "minor": 0, "required": True},
            {"id": "fes.video.fixed-720p60", "major": 1, "minor": 0, "required": True},
            {"id": "fes.media.blob", "major": 1, "minor": 0, "required": True},
        ],
        "build": {
            "id": build_identity(record),
            "repository": repository,
            "revision": revision,
            "recipe_sha256": record_fields["recipe_sha256"],
            "toolchain": toolchain,
        },
    }
    return encode_manifest(fields)


def _invalidate_failed_artifact(output: Path) -> None:
    for name in ("core.rbf", "manifest.toml", "build-summary.json"):
        path = output / name
        if path.is_symlink() or path.is_file():
            path.unlink()
        elif path.exists():
            raise BuildError(f"cannot invalidate non-file failed build output: {path}")


def _run_quartus(quartus_sh: Path, quartus_root: Path, project: Path, log: Path) -> None:
    env = os.environ.copy()
    env["QUARTUS_ROOTDIR"] = str(quartus_root)
    qsys = quartus_sh.parent.parent / "sopc_builder" / "bin"
    if qsys.is_dir():
        env["QSYS_ROOTDIR"] = str(qsys)
    runner = ROOT / "scripts" / "run_logged.sh"
    result = subprocess.run(
        [str(runner), str(log), *compile_command(quartus_sh)],
        cwd=project,
        env=env,
        check=False,
    )
    if result.returncode != 0:
        raise BuildError(f"Quartus compile failed (exit {result.returncode}); log: {log}")


def build(
    root: Path = ROOT,
    package_store: Path | None = None,
    *,
    print_commands: bool = False,
) -> Path:
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"FES ZX81 package store must be {root / 'build/packages'}")
    repository, revision = require_clean_source(root)
    quartus_root, quartus_sh, version, identity = authenticate_quartus(root)
    identities = {"quartus_sh": identity}
    record = create_build_record(root, repository, revision, identities)
    output = _prepare_output(root)
    _write_atomic(output / "build-inputs.json", record)
    build_id = build_identity(record)
    project = write_project(root, output, build_id)
    command = compile_command(quartus_sh)
    if print_commands:
        print(f"quartus_version: {version}")
        print(f"build_id: {build_id}")
        print(f"project: {project}")
        print("command:", " ".join(command))
        print(f"output: {output / 'core.rbf'}")
        return project
    try:
        _run_quartus(quartus_sh, quartus_root, project, output / "quartus.log")
        (output / "quartus-version.log").write_text(version + "\n", encoding="utf-8")
        evidence = validate_quartus_evidence(output, project)
        toolchain = f"Quartus Prime Lite 17.0.2 ({identity})"
        evidence.update(
            {
                "build_id": build_id,
                "device": TARGET,
                "inputs": {relative: _sha256(root / relative) for relative in sorted(PINNED_INPUTS)},
                "tools": identities,
                "top": TOP,
                "toolchain": toolchain,
            }
        )
        _write_atomic(
            output / "build-summary.json",
            (json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8"),
        )
        manifest = _manifest(record, evidence, repository, revision, toolchain)
        _write_atomic(output / "manifest.toml", manifest)
        final_repository, final_revision = require_clean_source(root)
        if (final_repository, final_revision) != (repository, revision):
            raise BuildError("source identity changed during build")
        return export_package(manifest, output / "core.rbf", package_store)
    except Exception:
        _invalidate_failed_artifact(output)
        raise


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument(
        "--print-commands",
        action="store_true",
        help="validate Quartus and a clean tree without compiling",
    )
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    arguments = _parser().parse_args(argv)
    try:
        print(build(arguments.root, arguments.package_output, print_commands=arguments.print_commands))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-zx81-quartus: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
