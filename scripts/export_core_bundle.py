#!/usr/bin/env python3
"""Export a sealed, content-addressed Mega Drive RBF bundle."""

from __future__ import annotations

import argparse
import ctypes
import errno
import hashlib
import json
import os
import re
import shutil
import stat
import sys
import tempfile
import subprocess
import tomllib
from dataclasses import asdict, dataclass
from pathlib import Path, PurePosixPath
from urllib.parse import urlparse

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.rebuild_core import validate_timing, RebuildError, SNES_FITTER_SEED
from scripts.core_lock import CoreLockError, CorePin, DEFAULT_LOCK, load_lock


CORE = "megadrive"
ABI = "mister"
ARTIFACT = "megadrive.rbf"
MANIFEST = "megadrive-rbf.toml"
RECIPE = "scripts/rebuild_core.py"
TOOLCHAIN = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
UPSTREAM_REPOSITORY = "https://github.com/MiSTer-devel/MegaDrive_MiSTer"
UPSTREAM_REVISION = "7365a137cfd8fa6f041e964d8b953159c0ec42d9"
SHA_RE = re.compile(r"^[0-9a-f]{40}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
AT_FDCWD = -100
RENAME_NOREPLACE = 1
COMPARE_FIELDS = frozenset(
    {
        "core",
        "commit",
        "project",
        "built_sha256",
        "built_size",
        "locked_sha256",
        "locked_size",
        "match",
        "quartus",
        "quartus_version",
        "built_rbf",
        "source",
        "build_date",
        "identical",
    }
)


class BundleExportError(ValueError):
    """Raised when a rebuild cannot be exported as a closed bundle."""


@dataclass(frozen=True)
class BundleManifest:
    format: int
    abi: str
    system: str
    artifact: str
    sha256: str
    size: int
    repository: str
    revision: str
    recipe: str
    recipe_sha256: str
    toolchain: str


def _reject_control(value: str, field: str) -> None:
    if not isinstance(value, str) or any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise BundleExportError(f"{field} must be a control-character-free string")


def _toml_string(value: str) -> str:
    return json.dumps(value, ensure_ascii=True)


def encode_manifest(value: BundleManifest) -> bytes:
    """Encode the fixed manifest schema as deterministic TOML."""

    if not isinstance(value.format, int) or isinstance(value.format, bool) or value.format != 1:
        raise BundleExportError("format must be 1")
    for field, item in asdict(value).items():
        if isinstance(item, str):
            _reject_control(item, field)
    parsed = urlparse(value.repository)
    if parsed.scheme != "https" or not parsed.netloc:
        raise BundleExportError("repository must be an HTTPS URL")
    if SHA_RE.fullmatch(value.revision) is None:
        raise BundleExportError("revision must be a 40-character lowercase hexadecimal SHA")
    for field in ("sha256", "recipe_sha256"):
        if SHA256_RE.fullmatch(getattr(value, field)) is None:
            raise BundleExportError(f"{field} must be 64 lowercase hexadecimal characters")
    if not isinstance(value.size, int) or isinstance(value.size, bool) or value.size <= 0:
        raise BundleExportError("size must be a positive integer")
    if value.abi != ABI or value.system not in ("megadrive", "snes", "pong") or value.artifact != value.system + ".rbf":
        raise BundleExportError("manifest ABI, system, and artifact are fixed")
    if value.recipe != ("scripts/build_pong.py" if value.system == "pong" else RECIPE):
        raise BundleExportError("manifest recipe is fixed")
    lines = [
        "format = 1",
        f"abi = {_toml_string(value.abi)}",
        f"system = {_toml_string(value.system)}",
        f"artifact = {_toml_string(value.artifact)}",
        f"sha256 = {_toml_string(value.sha256)}",
        f"size = {value.size}",
        f"repository = {_toml_string(value.repository)}",
        f"revision = {_toml_string(value.revision)}",
        f"recipe = {_toml_string(value.recipe)}",
        f"recipe_sha256 = {_toml_string(value.recipe_sha256)}",
        f"toolchain = {_toml_string(value.toolchain)}",
    ]
    return ("\n".join(lines) + "\n").encode("utf-8")


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _regular_file(path: Path, description: str) -> None:
    if path.is_symlink() or not path.is_file():
        raise BundleExportError(f"{description} must be a regular non-symlink file: {path}")


def _inside(path: Path, directory: Path, description: str) -> None:
    try:
        path.resolve(strict=False).relative_to(directory.resolve(strict=False))
    except ValueError as exc:
        raise BundleExportError(f"{description} escapes {directory}") from exc


def _comparison_path(value: object, root: Path, expected: Path, directory: Path, field: str) -> None:
    if not isinstance(value, str) or not value:
        raise BundleExportError(f"comparison {field} must be a non-empty path")
    path = Path(value)
    candidate = path if path.is_absolute() else root / path
    _inside(candidate, directory, f"comparison {field}")
    if candidate.resolve(strict=False) != expected.resolve(strict=False):
        raise BundleExportError(f"comparison {field} does not name {expected}")


def _validate_quartus_identity(value: object) -> None:
    if not isinstance(value, str):
        raise BundleExportError("comparison evidence is missing a Quartus executable")
    path = PurePosixPath(value)
    if not path.is_absolute() or str(path) != value:
        raise BundleExportError("comparison Quartus executable must be a canonical absolute path")
    parts = path.parts
    supported_layout = parts[-2:] == ("bin", "quartus_sh") or parts[-3:] == (
        "quartus",
        "bin",
        "quartus_sh",
    )
    if not supported_layout:
        raise BundleExportError("comparison Quartus executable is not a rebuild-core executable path")


def _validate_compare(
    pin: CorePin,
    root: Path,
    artifact: Path,
    compare_path: Path,
    snapshot_digest: str,
    snapshot_size: int,
) -> None:
    _regular_file(compare_path, "comparison evidence")
    try:
        compare = json.loads(compare_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BundleExportError(f"invalid comparison evidence: {compare_path}") from exc
    if not isinstance(compare, dict) or set(compare) != (COMPARE_FIELDS | {"fitter_seed", "timing", "timing_sha256", "recipe_sha256"} if pin.name == "snes" else COMPARE_FIELDS):
        raise BundleExportError("comparison evidence has missing or unrecognized fields")
    if pin.name == "snes":
        timing_path = artifact.parent / "project/output_files/SNES.sta.summary"
        if (compare["fitter_seed"] != SNES_FITTER_SEED or
                compare["recipe_sha256"] != _sha256(root / RECIPE) or
                compare["timing_sha256"] != _sha256(timing_path) or
                compare["timing"] != validate_timing(timing_path)):
            raise BundleExportError("stale SNES recipe or timing evidence")
    expected_match = snapshot_digest == pin.rbf_sha256 and snapshot_size == pin.rbf_size
    fixed = {
        "core": pin.name,
        "commit": pin.commit,
        "project": pin.project,
        "built_sha256": snapshot_digest,
        "built_size": snapshot_size,
        "locked_sha256": pin.rbf_sha256,
        "locked_size": pin.rbf_size,
        "match": expected_match,
        "identical": expected_match,
    }
    for field, expected in fixed.items():
        if compare[field] != expected:
            raise BundleExportError(f"stale comparison evidence: {field}")
    _validate_quartus_identity(compare["quartus"])
    if compare["quartus_version"] != TOOLCHAIN:
        raise BundleExportError("comparison evidence has an unexpected Quartus version")
    if compare["build_date"] is not None and not isinstance(compare["build_date"], str):
        raise BundleExportError("comparison build_date must be a string or null")
    rebuild_dir = root / "build" / "rebuild" / pin.name
    _comparison_path(compare["built_rbf"], root, artifact, rebuild_dir, "built_rbf")
    _comparison_path(
        compare["source"], root, root / "build" / "cores" / pin.name, root / "build" / "cores", "source"
    )


def _write_file(path: Path, data: bytes) -> None:
    with path.open("wb") as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    path.chmod(0o444)


def _publish_no_replace(temporary: Path, final: Path) -> None:
    """Publish a sealed directory only when its digest path remains absent."""

    libc = ctypes.CDLL(None, use_errno=True)
    try:
        renameat2 = libc.renameat2
    except AttributeError as exc:
        raise BundleExportError("no-replace directory publication is unavailable on this Linux host") from exc
    renameat2.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint)
    renameat2.restype = ctypes.c_int
    if renameat2(
        AT_FDCWD,
        os.fsencode(temporary),
        AT_FDCWD,
        os.fsencode(final),
        RENAME_NOREPLACE,
    ) == 0:
        return
    error = ctypes.get_errno()
    if error == errno.EEXIST:
        raise BundleExportError(f"bundle destination became occupied: {final}")
    raise OSError(error, os.strerror(error), final)


def _reuse_existing(final: Path, snapshot: bytes, manifest: bytes, artifact_name: str = ARTIFACT, manifest_name: str = MANIFEST) -> Path:
    if final.is_symlink() or not final.is_dir():
        raise BundleExportError(f"bundle destination is not a directory: {final}")
    if stat.S_IMODE(final.stat().st_mode) & 0o222:
        raise BundleExportError(f"existing bundle directory is writable: {final}")
    expected = {artifact_name, manifest_name}
    if {path.name for path in final.iterdir()} != expected:
        raise BundleExportError(f"existing bundle directory is partial or unexpected: {final}")
    for name in expected:
        path = final / name
        _regular_file(path, "existing bundle content")
        if stat.S_IMODE(path.stat().st_mode) & 0o222:
            raise BundleExportError(f"existing bundle content is writable: {path}")
    if (final / artifact_name).read_bytes() != snapshot or (final / manifest_name).read_bytes() != manifest:
        raise BundleExportError(f"existing bundle content differs: {final}")
    return final


def export_bundle(pin: CorePin, root: Path) -> Path:
    """Validate a Mega Drive rebuild and export its sealed digest bundle."""

    identities = {CORE: (UPSTREAM_REPOSITORY, UPSTREAM_REVISION), "snes": ("https://github.com/MiSTer-devel/SNES_MiSTer", "93d359e6f23c734ae3928984e88bed1d9b53cbac")}
    if identities.get(pin.name) != (pin.repo, pin.commit):
        raise BundleExportError("pin does not name the authoritative Mega Drive upstream revision")
    root = Path(root).resolve()
    artifact = root / "build" / "rebuild" / pin.name / f"{pin.name}.rbf"
    compare_path = artifact.with_name("compare.json")
    recipe_path = root / RECIPE
    _regular_file(artifact, "rebuild artifact")
    _regular_file(recipe_path, "rebuild recipe")
    snapshot = artifact.read_bytes()
    digest = _sha256_bytes(snapshot)
    _validate_compare(pin, root, artifact, compare_path, digest, len(snapshot))
    manifest = encode_manifest(
        BundleManifest(
            format=1,
            abi=ABI,
            system=pin.name,
            artifact=f"{pin.name}.rbf",
            sha256=digest,
            size=len(snapshot),
            repository=pin.repo,
            revision=pin.commit,
            recipe=RECIPE,
            recipe_sha256=_sha256(recipe_path),
            toolchain=TOOLCHAIN,
        )
    )
    return publish_bundle(root, pin.name, snapshot, manifest)


def publish_bundle(root: Path, system: str, snapshot: bytes, manifest: bytes) -> Path:
    artifact_name, manifest_name = system + ".rbf", system + "-rbf.toml"
    parent = root / "build" / "bundles" / system
    parent.mkdir(parents=True, exist_ok=True)
    final = parent / _sha256_bytes(snapshot)
    if final.exists() or final.is_symlink():
        return _reuse_existing(final, snapshot, manifest, artifact_name, manifest_name)
    temporary = Path(tempfile.mkdtemp(prefix=".export-", dir=parent))
    try:
        _write_file(temporary / artifact_name, snapshot)
        _write_file(temporary / manifest_name, manifest)
        temporary.chmod(0o555)
        _publish_no_replace(temporary, final)
    except Exception:
        if temporary.exists():
            temporary.chmod(0o755)
            shutil.rmtree(temporary)
        raise
    return final


def export_pong(root: Path) -> Path:
    root = Path(root).resolve()
    if subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=all"], cwd=root).strip():
        raise BundleExportError("Pong export requires a clean committed source tree")
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
    from scripts.build_pong import LOCAL_SOURCES
    work = root / "build/rebuild/pong"
    inputs_path, receipt_path = work / "inputs.json", work / "build.json"
    for path in (inputs_path, receipt_path, work / "pong.rbf"):
        _regular_file(path, "Pong build evidence")
    inputs = json.loads(inputs_path.read_text())
    receipt = json.loads(receipt_path.read_text())
    pin = tomllib.loads((root / "cores/pong/framework.toml").read_text())
    if inputs.get("format") != 1 or inputs.get("system") != "pong" or inputs.get("abi") != ABI:
        raise BundleExportError("invalid Pong input identity")
    if inputs.get("sources") != {name: _sha256(root / name) for name in LOCAL_SOURCES}:
        raise BundleExportError("Pong source inputs differ from committed source tree")
    if inputs.get("framework") != pin or receipt.get("framework") != pin or receipt.get("inputs_sha256") != _sha256(inputs_path):
        raise BundleExportError("stale Pong framework or input evidence")
    snapshot = (work / "pong.rbf").read_bytes()
    timing_path = work / "project/output_files/Pong.sta.summary"
    if (receipt.get("artifact") != "pong.rbf" or receipt.get("sha256") != _sha256_bytes(snapshot)
            or receipt.get("size") != len(snapshot) or receipt.get("quartus_version") != TOOLCHAIN
            or receipt.get("timing_sha256") != _sha256(timing_path)
            or receipt.get("timing") != validate_timing(timing_path)):
        raise BundleExportError("stale Pong artifact or timing evidence")
    manifest = encode_manifest(BundleManifest(1, ABI, "pong", "pong.rbf", _sha256_bytes(snapshot),
        len(snapshot), "https://github.com/DeanoC/misteross", revision, "scripts/build_pong.py",
        _sha256(root / "scripts/build_pong.py"), TOOLCHAIN))
    return publish_bundle(root, "pong", snapshot, manifest)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--core", default=CORE)
    parser.add_argument("--lock", type=Path, default=DEFAULT_LOCK)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args(argv)
    try:
        pins = load_lock(args.lock)
        if args.core == "pong":
            print(export_pong(args.root))
            return 0
        if args.core not in pins:
            raise BundleExportError(f"unknown core {args.core}")
        print(export_bundle(pins[args.core], args.root))
    except (CoreLockError, BundleExportError, RebuildError, OSError, ValueError) as exc:
        print(f"export-core-bundle: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
