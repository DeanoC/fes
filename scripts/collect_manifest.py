#!/usr/bin/env python3
"""Collect deterministic, path-safe evidence for one build lane.

The collector intentionally has no third-party dependencies.  Inputs are
hashed as regular files and represented with repository-relative POSIX paths;
symlink components and paths outside the declared repository/build roots are
refused so a manifest cannot silently attest to a different source tree.
"""

from __future__ import annotations

import argparse
import datetime as _datetime
import hashlib
import json
import os
import platform
import subprocess
import sys
from pathlib import Path
from typing import Any, Sequence


SCRIPT_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_REPO_ROOT = SCRIPT_ROOT
TARGET_DEVICE = "5CSEBA6U23I7"
HASH_CHUNK_SIZE = 1024 * 1024


class ManifestError(ValueError):
    """Raised when evidence cannot be collected safely."""


def _absolute(path: Path, base: Path) -> Path:
    """Make *path* absolute without following symlinks."""

    if path.is_absolute():
        return Path(os.path.abspath(os.fspath(path)))
    return Path(os.path.abspath(os.fspath(base / path)))


def _is_within(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
    except ValueError:
        return False
    return True


def _contains_symlink(path: Path) -> bool:
    """Return whether an existing path component is a symlink."""

    current = Path(path.anchor)
    for component in path.parts[1:]:
        current /= component
        try:
            if current.is_symlink():
                return True
        except OSError as exc:
            raise ManifestError(f"cannot inspect path {path}: {exc}") from exc
    return False


def _root_path(value: Path, *, label: str, create: bool = False) -> Path:
    path = Path(value)
    absolute = _absolute(path, Path.cwd())
    if absolute.is_symlink():
        raise ManifestError(f"{label} root must not be a symlink: {absolute}")
    if not absolute.exists() and create:
        if _contains_symlink(absolute):
            raise ManifestError(f"{label} root path contains a symlink: {absolute}")
        try:
            absolute.mkdir(parents=True, exist_ok=True)
        except OSError as exc:
            raise ManifestError(f"cannot create {label} root {absolute}: {exc}") from exc
    try:
        resolved = absolute.resolve(strict=True)
    except FileNotFoundError as exc:
        raise ManifestError(f"{label} root is missing: {absolute}") from exc
    except OSError as exc:
        raise ManifestError(f"cannot inspect {label} root {absolute}: {exc}") from exc
    if not resolved.is_dir():
        raise ManifestError(f"{label} root is not a directory: {resolved}")
    return resolved


def _safe_path(
    value: Path,
    *,
    label: str,
    base: Path,
    allowed_roots: Sequence[Path],
    require_exists: bool,
) -> Path:
    """Resolve one input while preserving and checking its path spelling."""

    candidate = _absolute(Path(value), base)
    if _contains_symlink(candidate):
        raise ManifestError(f"{label} path contains a symlink: {value}")

    try:
        resolved = candidate.resolve(strict=False)
    except OSError as exc:
        raise ManifestError(f"cannot resolve {label} path {value}: {exc}") from exc

    if not any(_is_within(resolved, root) for root in allowed_roots):
        roots = ", ".join(str(root) for root in allowed_roots)
        raise ManifestError(f"{label} path is outside allowed roots ({roots}): {value}")

    if require_exists and not candidate.exists():
        raise ManifestError(f"missing {label}: {value}")
    return candidate


def _path_argument(value: Path, *, base: Path, fallback: Path | None = None) -> Path:
    """Anchor relative paths at the repository, with output-dir convenience."""

    if Path(value).is_absolute():
        return Path(value)
    primary = _absolute(Path(value), base)
    if fallback is not None:
        alternate = _absolute(Path(value), fallback)
        if not primary.exists() and alternate.exists():
            return alternate
    return primary


def _hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    try:
        with path.open("rb") as stream:
            while True:
                chunk = stream.read(HASH_CHUNK_SIZE)
                if not chunk:
                    break
                digest.update(chunk)
    except OSError as exc:
        raise ManifestError(f"cannot hash file {path}: {exc}") from exc
    return digest.hexdigest()


def _files_for_input(path: Path, *, label: str) -> list[Path]:
    """Return regular files below one source/artifact input in stable order."""

    try:
        if path.is_symlink():
            raise ManifestError(f"{label} path is a symlink: {path}")
        if path.is_file():
            return [path]
        if not path.is_dir():
            raise ManifestError(f"{label} is not a regular file or directory: {path}")
    except OSError as exc:
        raise ManifestError(f"cannot inspect {label} {path}: {exc}") from exc

    files: list[Path] = []
    for current, directory_names, file_names in os.walk(path, topdown=True, followlinks=False):
        current_path = Path(current)
        directory_names.sort()
        file_names.sort()
        # os.walk does not descend into symlinked directories when
        # followlinks=False, so reject them explicitly instead of silently
        # omitting them from the evidence.
        for name in directory_names:
            child = current_path / name
            if child.is_symlink():
                raise ManifestError(f"{label} tree contains a symlink: {child}")
        for name in file_names:
            child = current_path / name
            if child.is_symlink():
                raise ManifestError(f"{label} tree contains a symlink: {child}")
            if not child.is_file():
                raise ManifestError(f"{label} tree contains a non-regular file: {child}")
            files.append(child)
    return files


def _relative_label(path: Path, *, repo_root: Path, build_root: Path) -> str:
    if _is_within(path, repo_root):
        return path.relative_to(repo_root).as_posix()
    if _is_within(path, build_root):
        return Path("build", path.relative_to(build_root)).as_posix()
    raise ManifestError(f"cannot create repository-relative path for {path}")


def _file_records(
    values: Sequence[Path],
    *,
    label: str,
    base: Path,
    repo_root: Path,
    build_root: Path,
    fallback: Path | None = None,
    require_exists: bool = True,
    allow_directories: bool = True,
) -> list[dict[str, Any]]:
    records: dict[str, dict[str, Any]] = {}
    for value in values:
        candidate = _path_argument(Path(value), base=base, fallback=fallback)
        safe = _safe_path(
            candidate,
            label=label,
            base=base,
            allowed_roots=(repo_root, build_root),
            require_exists=require_exists,
        )
        if safe.is_dir() and not allow_directories:
            raise ManifestError(f"{label} is not a regular file: {value}")
        for path in _files_for_input(safe, label=label):
            relative = _relative_label(path, repo_root=repo_root, build_root=build_root)
            records[relative] = {"path": relative, "sha256": _hash_file(path)}
    return [records[key] for key in sorted(records)]


def _command_log_records(
    values: Sequence[Path],
    *,
    base: Path,
    fallback: Path | None,
    repo_root: Path,
    build_root: Path,
) -> tuple[list[dict[str, Any]], list[str]]:
    records: dict[str, dict[str, Any]] = {}
    commands: list[str] = []
    for value in values:
        candidate = _path_argument(Path(value), base=base, fallback=fallback)
        safe = _safe_path(
            candidate,
            label="command log",
            base=base,
            allowed_roots=(repo_root, build_root),
            require_exists=True,
        )
        if not safe.is_file() or safe.is_symlink():
            raise ManifestError(f"command log is not a regular file: {value}")
        relative = _relative_label(safe, repo_root=repo_root, build_root=build_root)
        records[relative] = {"path": relative, "sha256": _hash_file(safe)}
        try:
            text = safe.read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            raise ManifestError(f"cannot read command log {safe}: {exc}") from exc
        # run_logged.sh emits exactly one authenticated header as line one.
        # Later command:-prefixed lines may be tool output and are not
        # evidence of a command invocation.
        lines = text.splitlines()
        first_line = lines[0] if lines else ""
        if first_line.startswith("command:"):
            commands.append(first_line.rstrip("\r"))
    return [records[key] for key in sorted(records)], commands


def _timestamp() -> str:
    value = os.environ.get("SOURCE_DATE_EPOCH")
    try:
        if value is None:
            instant = _datetime.datetime.now(_datetime.timezone.utc)
        else:
            instant = _datetime.datetime.fromtimestamp(int(value), tz=_datetime.timezone.utc)
    except (OverflowError, OSError, ValueError) as exc:
        raise ManifestError(f"invalid SOURCE_DATE_EPOCH: {value!r}") from exc
    return instant.replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _host() -> dict[str, str]:
    return {
        "architecture": platform.machine(),
        "name": platform.node(),
        "os": platform.system(),
        "os_release": platform.release(),
        "python": platform.python_version(),
    }


def _git_state(repo_root: Path) -> dict[str, Any]:
    def run(*arguments: str) -> str | None:
        try:
            result = subprocess.run(
                ["git", "-C", str(repo_root), *arguments],
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
        except OSError:
            return None
        if result.returncode != 0:
            return None
        return result.stdout.strip()

    commit = run("rev-parse", "HEAD")
    status_text = run("status", "--porcelain=v1", "--untracked-files=all")
    if commit is None or status_text is None:
        return {"commit": commit, "dirty": None, "state": "unknown", "changes": []}
    changes = sorted(line for line in status_text.splitlines() if line)
    dirty = bool(changes)
    return {
        "commit": commit,
        "dirty": dirty,
        "state": "dirty" if dirty else "clean",
        "changes": changes,
    }


def _tool_pins(repo_root: Path) -> dict[str, dict[str, Any]]:
    scripts_dir = str(repo_root / "scripts")
    if scripts_dir not in sys.path:
        sys.path.insert(0, scripts_dir)
    try:
        from lockfile import load_lock

        pins = load_lock(repo_root / "toolchain.lock")
    except (ImportError, OSError, ValueError) as exc:
        raise ManifestError(f"cannot load toolchain.lock: {exc}") from exc
    return {
        name: {
            "commit": pin.commit,
            "order": pin.order,
            "rationale": pin.rationale,
            "repo": pin.repo,
        }
        for name, pin in sorted(pins.items())
    }


def collect_manifest(
    output_dir: Path,
    experiment: str,
    lane: str,
    target: str,
    source_paths: Sequence[Path] = (),
    command_log_paths: Sequence[Path] = (),
    artifact_paths: Sequence[Path] = (),
    *,
    repo_root: Path = DEFAULT_REPO_ROOT,
    build_root: Path | None = None,
    commands: Sequence[str] = (),
    manifest_path: Path | None = None,
) -> dict[str, Any]:
    """Collect and write one manifest, returning the JSON-compatible object."""

    if not experiment or not lane:
        raise ManifestError("experiment and lane must be non-empty")
    if target != TARGET_DEVICE:
        raise ManifestError(f"target must be {TARGET_DEVICE}, got {target!r}")

    repository = _root_path(Path(repo_root), label="repository")
    build = _root_path(
        Path(build_root) if build_root is not None else repository / "build",
        label="build",
        create=True,
    )
    output_candidate = _path_argument(Path(output_dir), base=repository)
    output = _safe_path(
        output_candidate,
        label="output directory",
        base=repository,
        allowed_roots=(build,),
        require_exists=False,
    )
    if output.exists() and not output.is_dir():
        raise ManifestError(f"output directory is not a directory: {output_dir}")
    try:
        output.mkdir(parents=True, exist_ok=True)
    except OSError as exc:
        raise ManifestError(f"cannot create output directory {output}: {exc}") from exc

    sources = _file_records(
        source_paths,
        label="source",
        base=repository,
        repo_root=repository,
        build_root=build,
    )
    artifacts = _file_records(
        artifact_paths,
        label="artifact",
        base=repository,
        fallback=output,
        repo_root=repository,
        build_root=build,
        allow_directories=False,
    )
    command_logs, logged_commands = _command_log_records(
        command_log_paths,
        base=repository,
        fallback=output,
        repo_root=repository,
        build_root=build,
    )
    all_commands = sorted(dict.fromkeys([*commands, *logged_commands]))

    manifest: dict[str, Any] = {
        "artifacts": artifacts,
        "command_logs": command_logs,
        "commands": all_commands,
        "experiment": experiment,
        "git": _git_state(repository),
        "host": _host(),
        "lane": lane,
        "schema": 1,
        "sources": sources,
        "target": target,
        "timestamp": _timestamp(),
        "tool_pins": _tool_pins(repository),
    }

    destination = Path(manifest_path) if manifest_path is not None else output / "manifest.json"
    if not destination.is_absolute():
        destination = _absolute(destination, output if manifest_path is not None else repository)
    destination = _safe_path(
        destination,
        label="manifest",
        base=repository,
        allowed_roots=(output,),
        require_exists=False,
    )
    if destination.exists() and destination.is_symlink():
        raise ManifestError(f"manifest path is a symlink: {destination}")
    if destination.exists() and not destination.is_file():
        raise ManifestError(f"manifest path is not a regular file: {destination}")
    try:
        destination.parent.mkdir(parents=True, exist_ok=True)
        encoded = json.dumps(
            manifest,
            ensure_ascii=False,
            indent=2,
            sort_keys=True,
        ) + "\n"
        destination.write_text(encoded, encoding="utf-8")
    except OSError as exc:
        raise ManifestError(f"cannot write manifest {destination}: {exc}") from exc
    return manifest


# A descriptive alias is useful to callers that distinguish construction from
# the side effect of writing manifest.json.
build_manifest = collect_manifest


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("positional", nargs="*", metavar="VALUE")
    parser.add_argument("-o", "--output-dir", dest="output_dir")
    parser.add_argument("--experiment")
    parser.add_argument("--lane")
    parser.add_argument("--target", default=TARGET_DEVICE)
    parser.add_argument("--repo-root", type=Path, default=DEFAULT_REPO_ROOT)
    parser.add_argument("--build-root", type=Path)
    parser.add_argument("--source", "--source-path", dest="sources", action="append", type=Path, default=[])
    parser.add_argument(
        "--command-log",
        "--command-log-path",
        "--log",
        dest="command_logs",
        action="append",
        type=Path,
        default=[],
    )
    parser.add_argument("--artifact", "--artifact-path", dest="artifacts", action="append", type=Path, default=[])
    parser.add_argument("--command", action="append", default=[])
    parser.add_argument("--manifest", type=Path)
    return parser


def _arguments(argv: Sequence[str] | None) -> argparse.Namespace:
    parser = _parser()
    arguments = parser.parse_args(argv)
    positional = list(arguments.positional)
    if positional:
        if len(positional) != 4:
            parser.error("positional form requires OUTPUT_DIR EXPERIMENT LANE TARGET")
        fields = ("output_dir", "experiment", "lane", "target")
        for field, value in zip(fields, positional):
            if getattr(arguments, field) is not None and getattr(arguments, field) != value:
                parser.error(f"conflicting positional and option values for {field}")
            setattr(arguments, field, value)
    missing = [field for field in ("output_dir", "experiment", "lane") if not getattr(arguments, field)]
    if missing:
        parser.error("missing required option(s): " + ", ".join("--" + field.replace("_", "-") for field in missing))
    return arguments


def main(argv: Sequence[str] | None = None) -> int:
    try:
        arguments = _arguments(argv)
        collect_manifest(
            Path(arguments.output_dir),
            arguments.experiment,
            arguments.lane,
            arguments.target,
            arguments.sources,
            arguments.command_logs,
            arguments.artifacts,
            repo_root=arguments.repo_root,
            build_root=arguments.build_root,
            commands=arguments.command,
            manifest_path=arguments.manifest,
        )
        return 0
    except ManifestError as exc:
        print(f"collect_manifest: {exc}", file=sys.stderr)
        return 2
    except (OSError, ValueError) as exc:
        print(f"collect_manifest: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
