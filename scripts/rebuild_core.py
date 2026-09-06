#!/usr/bin/env python3
"""Compile a fetched core with explicit Quartus 17.0.2 and hash the RBF."""

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
from pathlib import Path

try:
    from .core_lock import DEFAULT_LOCK, CoreLockError, CorePin, load_lock
except ImportError:  # pragma: no cover - CLI script entry
    from core_lock import DEFAULT_LOCK, CoreLockError, CorePin, load_lock

ROOT = Path(__file__).resolve().parents[1]
QUARTUS_VERSION_RE = re.compile(r"(?<![0-9])17[.]0[.]2(?![0-9])")
BUILD_DATE_RE = re.compile(r"^[0-9]{6}$")
RBF_DATE_RE = re.compile(r"(20[0-9]{6})$")
CLOCK_BUILD_DATE_LINE = (
    'set buildDate "`define BUILD_DATE \\"[clock format [ clock seconds ] -format %y%m%d]\\""'
)
STAGED_BUILD_DATE_TCL = """if {[info exists ::env(MISTER_BUILD_DATE)] && $::env(MISTER_BUILD_DATE) ne ""} {
	set yymmdd $::env(MISTER_BUILD_DATE)
} else {
	set yymmdd [clock format [ clock seconds ] -format %y%m%d]
}
	set buildDate "`define BUILD_DATE \\"$yymmdd\\""
"""
STAGE_IGNORE_NAMES = {
    ".git",
    "db",
    "incremental_db",
    "output_files",
    ".qsys_edit",
    "greybox_tmp",
}
STAGE_IGNORE_SUFFIXES = (".rbf", ".sof", ".qws", ".jdi", ".smsg")


class RebuildError(RuntimeError):
    """Raised when a core rebuild cannot run or produce an RBF."""


def _symlinked_under(path: Path, root: Path) -> bool:
    """True if path is outside root or a component below root is a symlink."""

    absolute = Path(os.path.abspath(os.fspath(path)))
    root_abs = Path(os.path.abspath(os.fspath(root)))
    try:
        relative = absolute.relative_to(root_abs)
    except ValueError:
        return True
    current = root_abs
    for component in relative.parts:
        current /= component
        if current.is_symlink():
            return True
    return False


def _run_git(args: list[str], cwd: Path) -> str:
    result = subprocess.run(
        ["git", *args],
        cwd=cwd,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        raise RebuildError(detail or f"git {' '.join(args)} failed")
    return result.stdout


def locate_quartus(quartus_root: str, repo_root: Path) -> tuple[Path, Path]:
    if not quartus_root.strip():
        raise RebuildError(
            "Quartus rebuild unavailable; OSS and simulation remain usable "
            "(set QUARTUS_ROOTDIR to an installed Quartus 17.0.2 tree)"
        )
    root = Path(quartus_root)
    if not root.is_absolute():
        root = repo_root / root
    if any(character.isspace() for character in str(root)):
        raise RebuildError(
            f"Quartus rebuild unavailable; QUARTUS_ROOTDIR must not contain spaces: {root}"
        )
    if not root.is_dir() or root.is_symlink():
        raise RebuildError(
            f"Quartus rebuild unavailable; QUARTUS_ROOTDIR is not a directory: {root}"
        )
    for candidate in (root / "quartus" / "bin" / "quartus_sh", root / "bin" / "quartus_sh"):
        if (
            candidate.is_file()
            and not candidate.is_symlink()
            and os.access(candidate, os.X_OK)
            and not _symlinked_under(candidate, root)
        ):
            return root, candidate
    raise RebuildError(
        "Quartus rebuild unavailable; expected quartus_sh below "
        "QUARTUS_ROOTDIR/bin or QUARTUS_ROOTDIR/quartus/bin"
    )


def quartus_version_line(quartus_sh: Path) -> str:
    result = subprocess.run(
        [str(quartus_sh), "--version"],
        text=True,
        capture_output=True,
        check=False,
    )
    output = (result.stdout or "") + (result.stderr or "")
    if result.returncode != 0:
        raise RebuildError(
            f"Quartus rebuild unavailable; could not execute {quartus_sh} --version"
        )
    matches = [line for line in output.splitlines() if QUARTUS_VERSION_RE.search(line)]
    if len(matches) != 1:
        raise RebuildError(
            "Quartus rebuild requires exactly one line containing exact version 17.0.2"
        )
    return matches[0]


def require_fetch(pin: CorePin, source_dir: Path, root: Path) -> Path:
    if not source_dir.is_dir() or source_dir.is_symlink() or _symlinked_under(source_dir, root):
        raise RebuildError(
            f"missing fetched core tree; run make fetch-core CORE={pin.name} first: {source_dir}"
        )
    git_dir = source_dir / ".git"
    if not git_dir.exists():
        raise RebuildError(f"source path exists but is not a Git checkout: {source_dir}")
    dirty = _run_git(
        ["status", "--porcelain=v1", "--untracked-files=all"],
        cwd=source_dir,
    ).strip()
    if dirty:
        raise RebuildError(
            f"source checkout is dirty; clean it manually before rebuild: {source_dir}"
        )
    head = _run_git(["rev-parse", "HEAD"], cwd=source_dir).strip()
    if head != pin.commit:
        raise RebuildError(
            f"source checkout {pin.name} is at {head}, expected locked commit {pin.commit}"
        )
    project = source_dir / pin.project
    if not project.is_file() or project.is_symlink():
        raise RebuildError(f"missing locked Quartus project: {project}")
    return project


def _stage_ignore(directory: str, names: list[str]) -> set[str]:
    ignored: set[str] = set()
    for name in names:
        if name in STAGE_IGNORE_NAMES:
            ignored.add(name)
            continue
        lower = name.lower()
        if lower.endswith(STAGE_IGNORE_SUFFIXES):
            ignored.add(name)
            continue
        candidate = Path(directory) / name
        if candidate.is_symlink():
            ignored.add(name)
    return ignored


def derive_build_date(rbf_path: str) -> str | None:
    """Return YYMMDD from a release name such as MegaDrive_20260603.rbf."""

    match = RBF_DATE_RE.search(Path(rbf_path).stem)
    if match is None:
        return None
    return match.group(1)[2:]


def resolve_build_date(pin: CorePin, override: str | None) -> str | None:
    if override is None:
        override = os.environ.get("BUILD_DATE", "").strip() or None
    if override:
        if BUILD_DATE_RE.fullmatch(override) is None:
            raise RebuildError(f"BUILD_DATE must be YYMMDD: {override}")
        return override
    return derive_build_date(pin.rbf_path)


def pin_staged_build_date(project_dir: Path, yymmdd: str) -> None:
    """Honor MISTER_BUILD_DATE in the staged copy only; do not edit the fetch."""

    tcl = project_dir / "sys" / "build_id.tcl"
    if not tcl.is_file():
        return
    text = tcl.read_text(encoding="utf-8")
    if CLOCK_BUILD_DATE_LINE not in text:
        raise RebuildError(f"staged build_id.tcl does not contain the clock stamp line: {tcl}")
    tcl.write_text(text.replace(CLOCK_BUILD_DATE_LINE, STAGED_BUILD_DATE_TCL, 1), encoding="utf-8")
    (project_dir / "build_id.v").write_text(
        f'`define BUILD_DATE "{yymmdd}"',
        encoding="utf-8",
    )


def stage_project(source_dir: Path, project_dir: Path, root: Path) -> None:
    if project_dir.exists() or project_dir.is_symlink():
        if project_dir.is_symlink() or _symlinked_under(project_dir, root):
            raise RebuildError(f"rebuild project path is a symlink: {project_dir}")
        if not project_dir.is_dir():
            raise RebuildError(f"rebuild project path is not a directory: {project_dir}")
        shutil.rmtree(project_dir)
    shutil.copytree(source_dir, project_dir, symlinks=False, ignore=_stage_ignore)
    if _symlinked_under(project_dir, root):
        raise RebuildError(f"staged project contains a symlink component: {project_dir}")


def _qsys_root(quartus_sh: Path) -> Path:
    return quartus_sh.parent.parent / "sopc_builder" / "bin"


def compile_project(
    pin: CorePin,
    project_dir: Path,
    quartus_sh: Path,
    quartus_root: Path,
    log: Path,
    *,
    print_commands: bool,
    build_date: str | None = None,
) -> Path:
    revision = Path(pin.project).stem
    command = [str(quartus_sh), "--flow", "compile", revision]
    if print_commands:
        print("command:", " ".join(command))
        return project_dir / "output_files" / f"{revision}.rbf"
    env = os.environ.copy()
    env["QUARTUS_ROOTDIR"] = str(quartus_root)
    if build_date:
        env["MISTER_BUILD_DATE"] = build_date
    qsys = env.get("QSYS_ROOTDIR", "").strip()
    default_qsys = _qsys_root(quartus_sh)
    if not qsys and default_qsys.is_dir():
        env["QSYS_ROOTDIR"] = str(default_qsys)
    runner = Path(__file__).resolve().parent / "run_logged.sh"
    result = subprocess.run(
        [str(runner), str(log), *command],
        cwd=project_dir,
        env=env,
        check=False,
    )
    if result.returncode != 0:
        raise RebuildError(
            f"Quartus compile failed for {pin.name} (exit {result.returncode}); log: {log}"
        )
    rbf = project_dir / "output_files" / f"{revision}.rbf"
    if not rbf.is_file() or rbf.is_symlink() or rbf.stat().st_size <= 0:
        raise RebuildError(f"Quartus compile did not produce {rbf}")
    return rbf


def compare_rbf(pin: CorePin, built: Path) -> dict[str, object]:
    data = built.read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    size = len(data)
    match = digest == pin.rbf_sha256 and size == pin.rbf_size
    return {
        "core": pin.name,
        "commit": pin.commit,
        "project": pin.project,
        "built_sha256": digest,
        "built_size": size,
        "locked_sha256": pin.rbf_sha256,
        "locked_size": pin.rbf_size,
        "match": match,
    }


SNES_FITTER_SEED = 3


def pin_fitter_seed(qsf: Path, seed: int) -> None:
    text = qsf.read_text()
    text, count = re.subn(r"(?m)^set_global_assignment -name SEED [0-9]+$",
                         f"set_global_assignment -name SEED {seed}", text)
    if count != 1:
        raise RebuildError("expected exactly one fitter seed assignment")
    qsf.write_text(text)


def validate_timing(path: Path) -> list[dict[str, object]]:
    """Require complete finite, nonnegative TimeQuest summary results."""
    text = path.read_text()
    blocks = re.findall(r"(?m)^Type\s*:\s*(.+)\nSlack\s*:\s*(\S+)\nTNS\s*:\s*(\S+)", text)
    if len(blocks) != len(re.findall(r"(?m)^Type\s*:", text)):
        raise RebuildError("malformed timing summary")
    result, categories = [], set()
    for kind, slack_text, tns_text in blocks:
        category = next((name for name in ("Setup", "Hold", "Recovery", "Removal", "Minimum Pulse Width") if kind == name or kind.startswith(name + " ")), None)
        try:
            slack, tns = float(slack_text), float(tns_text)
        except ValueError as exc:
            raise RebuildError("invalid timing number") from exc
        if category is None or not all(math.isfinite(x) and x >= 0 for x in (slack, tns)):
            raise RebuildError("timing summary contains an unsupported or failing result")
        categories.add(category)
        result.append({"type": kind, "slack_ns": slack, "tns_ns": tns})
    if not {"Setup", "Hold", "Recovery", "Removal", "Minimum Pulse Width"} <= categories:
        raise RebuildError("timing summary lacks required analysis categories")
    return result


def rebuild(
    pin: CorePin,
    root: Path,
    *,
    print_commands: bool = False,
    build_date: str | None = None,
) -> dict[str, object]:
    recipe_digest = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    source_dir = root / "build" / "cores" / pin.name
    work_dir = root / "build" / "rebuild" / pin.name
    project_dir = work_dir / "project"
    artifact = work_dir / f"{pin.name}.rbf"
    log = work_dir / "quartus.log"
    version_log = work_dir / "quartus-version.log"
    compare_path = work_dir / "compare.json"
    require_fetch(pin, source_dir, root)
    quartus_root_env = os.environ.get("QUARTUS_ROOTDIR", "")
    quartus_root, quartus_sh = locate_quartus(quartus_root_env, root)
    version = quartus_version_line(quartus_sh)
    resolved_date = resolve_build_date(pin, build_date)
    date_label = resolved_date or "clock"
    if print_commands:
        print(f"core: {pin.name}")
        print(f"commit: {pin.commit}")
        print(f"project: {pin.project}")
        print(f"source: {source_dir}")
        print(f"work: {project_dir}")
        print(f"output: {artifact}")
        print(f"quartus_version: {version}")
        print(f"build_date: {date_label}")
        compile_project(
            pin,
            project_dir,
            quartus_sh,
            quartus_root,
            log,
            print_commands=True,
            build_date=resolved_date,
        )
        return {"core": pin.name, "print_commands": True, "build_date": resolved_date}
    work_dir.mkdir(parents=True, exist_ok=True)
    version_log.write_text(version + "\n", encoding="utf-8")
    stage_project(source_dir, project_dir, root)
    if resolved_date:
        pin_staged_build_date(project_dir, resolved_date)
    if pin.name == "snes":
        pin_fitter_seed(project_dir / "SNES.qsf", SNES_FITTER_SEED)
    compare_path.unlink(missing_ok=True)
    built = compile_project(
        pin,
        project_dir,
        quartus_sh,
        quartus_root,
        log,
        print_commands=False,
        build_date=resolved_date,
    )
    timing_path = project_dir / "output_files" / f"{Path(pin.project).stem}.sta.summary"
    timing = validate_timing(timing_path) if pin.name == "snes" else None
    shutil.copy2(built, artifact)
    report = compare_rbf(pin, artifact)
    report["quartus"] = str(quartus_sh)
    report["quartus_version"] = version
    report["built_rbf"] = str(artifact)
    report["source"] = str(source_dir)
    report["build_date"] = resolved_date
    report["identical"] = bool(report["match"])
    report["recipe_sha256"] = recipe_digest
    if pin.name == "snes":
        report.update(fitter_seed=SNES_FITTER_SEED, timing=timing,
                      timing_sha256=hashlib.sha256(timing_path.read_bytes()).hexdigest(),
                      recipe_sha256=recipe_digest)
    compare_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    identical = "yes" if report["identical"] else "no"
    print(
        f"ok {pin.name} rebuild {version}\n"
        f"upstream sha256 {report['locked_sha256']} size {report['locked_size']}\n"
        f"rebuild sha256 {report['built_sha256']} size {report['built_size']}\n"
        f"identical: {identical}"
    )
    return report


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--core", default="megadrive")
    parser.add_argument("--lock", type=Path, default=DEFAULT_LOCK)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument(
        "--print-commands",
        action="store_true",
        help="validate Quartus and the fetched tree without compiling",
    )
    parser.add_argument(
        "--build-date",
        default=None,
        help="YYMMDD stamp for sys/build_id.tcl (default: date in the locked rbf_path)",
    )
    args = parser.parse_args(argv)
    try:
        pins = load_lock(args.lock)
        if args.core not in pins:
            raise RebuildError(f"unknown core {args.core}")
        rebuild(
            pins[args.core],
            args.root,
            print_commands=args.print_commands,
            build_date=args.build_date,
        )
    except (CoreLockError, RebuildError) as exc:
        print(f"rebuild-core: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
