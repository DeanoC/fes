#!/usr/bin/env python3
"""Export deterministic format-2 FES core packages."""

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
import subprocess
import sys
import tempfile
from pathlib import Path, PurePosixPath

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.source_repository import canonical_repository
from scripts.core_package import (
    MAX_MANIFEST_SIZE,
    MAX_PAYLOAD_SIZE,
    PackageError,
    _decode_manifest,
    _repository as _manifest_repository,
    _ustar_header,
    package_identity,
    read_package,
)


MAX_BUILD_RECORD_SIZE = 65_536
HEX40_RE = re.compile(r"[0-9a-f]{40}\Z")
HEX64_RE = re.compile(r"[0-9a-f]{64}\Z")
NAME_RE = re.compile(r"[a-z][a-z0-9_.-]{0,95}\Z")
AT_FDCWD = -100
RENAME_NOREPLACE = 1


class PackageExportError(ValueError):
    """Raised when build evidence or package publication is invalid."""


def _string(value: object, field: str, *, maximum: int = 4_096) -> str:
    if not isinstance(value, str) or not value or len(value.encode("utf-8")) > maximum:
        raise PackageExportError(f"{field} must be a nonempty string of at most {maximum} UTF-8 bytes")
    if any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in value):
        raise PackageExportError(f"{field} contains a control character")
    return value


def _relative_path(value: object, field: str) -> str:
    value = _string(value, field)
    path = PurePosixPath(value)
    if (
        path.is_absolute()
        or str(path) != value
        or not path.parts
        or any(part in ("", ".", "..") for part in path.parts)
        or "\\" in value
    ):
        raise PackageExportError(f"{field} must be a normalized relative POSIX path without traversal")
    return value


def _validate_build_record(fields: object) -> dict:
    expected = {
        "format",
        "repository",
        "revision",
        "recipe",
        "recipe_sha256",
        "abi_definition",
        "abi_definition_sha256",
        "dependencies",
        "tools",
        "parameters",
    }
    if isinstance(fields, dict) and fields.get("format") == 2:
        expected |= {"source_roots", "source_inputs", "source_path"}
    if not isinstance(fields, dict) or set(fields) != expected:
        raise PackageExportError("build record has missing or unrecognized fields")
    if type(fields["format"]) is not int or fields["format"] not in (1, 2):
        raise PackageExportError("build record format must be integer 1 or 2")
    try:
        _manifest_repository(fields["repository"])
    except PackageError as exc:
        raise PackageExportError(str(exc)) from exc
    revision = _string(fields["revision"], "revision")
    if HEX40_RE.fullmatch(revision) is None:
        raise PackageExportError("revision must be a full lowercase 40-hex Git commit")
    _relative_path(fields["recipe"], "recipe")
    _relative_path(fields["abi_definition"], "abi_definition")
    for field in ("recipe_sha256", "abi_definition_sha256"):
        digest = _string(fields[field], field)
        if HEX64_RE.fullmatch(digest) is None:
            raise PackageExportError(f"{field} must be 64 lowercase hexadecimal characters")

    if fields["format"] == 2:
        if fields["source_path"] != ".":
            _relative_path(fields["source_path"], "source_path")
        roots = fields["source_roots"]
        inputs = fields["source_inputs"]
        if not isinstance(roots, list) or not roots or any(not isinstance(p, str) for p in roots):
            raise PackageExportError("source_roots must be nonempty relative paths")
        if roots != sorted(set(roots)):
            raise PackageExportError("source_roots must be sorted and unique")
        for path in roots:
            _relative_path(path, "source root")
        if not isinstance(inputs, dict) or not inputs:
            raise PackageExportError("source_inputs must be nonempty")
        for path, digest in inputs.items():
            _relative_path(path, "source input")
            if not any(path == prefix or path.startswith(prefix + "/") for prefix in roots):
                raise PackageExportError("source input is outside declared roots")
            if not isinstance(digest, str) or HEX64_RE.fullmatch(digest) is None:
                raise PackageExportError("source input digest must be lowercase SHA256")
        for path_key, digest_key in (("recipe", "recipe_sha256"), ("abi_definition", "abi_definition_sha256")):
            if inputs.get(fields[path_key]) != fields[digest_key]:
                raise PackageExportError("source closure must include recipe and ABI bytes")

    dependencies = fields["dependencies"]
    if not isinstance(dependencies, dict):
        raise PackageExportError("dependencies must be a JSON object")
    for path, dependency_revision in dependencies.items():
        _relative_path(path, "dependency path")
        if not isinstance(dependency_revision, str) or HEX40_RE.fullmatch(dependency_revision) is None:
            raise PackageExportError(f"dependency {path} must name a full lowercase 40-hex commit")

    tools = fields["tools"]
    if not isinstance(tools, dict) or not tools:
        raise PackageExportError("tools must be a nonempty JSON object")
    for name, identity in tools.items():
        if not isinstance(name, str) or NAME_RE.fullmatch(name) is None:
            raise PackageExportError("tool names must be lowercase identifiers")
        _string(identity, f"tool {name}", maximum=1_024)

    parameters = fields["parameters"]
    if not isinstance(parameters, dict):
        raise PackageExportError("parameters must be a JSON object")
    for name, value in parameters.items():
        if not isinstance(name, str) or NAME_RE.fullmatch(name) is None:
            raise PackageExportError("parameter names must be lowercase identifiers")
        if type(value) not in (str, int, bool):
            raise PackageExportError(f"parameter {name} must be a string, integer, or Boolean")
        if isinstance(value, str):
            _string(value, f"parameter {name}", maximum=4_096)
        elif type(value) is int and not -(1 << 63) <= value < (1 << 63):
            raise PackageExportError(f"parameter {name} integer is outside signed 64-bit range")
    return fields


def encode_build_record(fields: dict) -> bytes:
    """Encode deterministic pre-synthesis inputs as canonical UTF-8 JSON."""

    _validate_build_record(fields)
    encoded = json.dumps(fields, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode("utf-8") + b"\n"
    if len(encoded) > MAX_BUILD_RECORD_SIZE:
        raise PackageExportError(f"build record exceeds {MAX_BUILD_RECORD_SIZE} bytes")
    return encoded


def build_identity(record: bytes) -> str:
    """Return the 128-bit build correlation ID derived before synthesis."""

    if not isinstance(record, bytes):
        raise TypeError("build record must be bytes")
    # Version 1 historically hashes arbitrary bytes; retain that exact API.
    try:
        version = json.loads(record).get("format")
    except (ValueError, AttributeError, UnicodeDecodeError):
        version = None
    if version != 2:
        return hashlib.sha256(record).hexdigest()[:32]
    fields = _decode_build_record(record)
    functional = {key: value for key, value in fields.items() if key not in ("repository", "revision", "source_path")}
    encoded = json.dumps(functional, ensure_ascii=False, separators=(",", ":"), sort_keys=True).encode("utf-8")
    return hashlib.sha256(b"fes-functional-inputs-v2\0" + encoded).hexdigest()[:32]


def _reject_float(value: str) -> None:
    raise PackageExportError("build record numbers must be integers")


def _reject_constant(value: str) -> None:
    raise PackageExportError("build record contains a non-finite number")


def _reject_duplicate_pairs(pairs: list[tuple[str, object]]) -> dict:
    result: dict = {}
    for key, value in pairs:
        if key in result:
            raise PackageExportError(f"duplicate build record key: {key}")
        result[key] = value
    return result


def _decode_build_record(record: bytes) -> dict:
    if not 1 <= len(record) <= MAX_BUILD_RECORD_SIZE:
        raise PackageExportError(f"build record size must be 1 through {MAX_BUILD_RECORD_SIZE} bytes")
    try:
        text = record.decode("utf-8")
        fields = json.loads(
            text,
            object_pairs_hook=_reject_duplicate_pairs,
            parse_float=_reject_float,
            parse_constant=_reject_constant,
        )
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise PackageExportError("build record must be valid UTF-8 JSON") from exc
    _validate_build_record(fields)
    if encode_build_record(fields) != record:
        raise PackageExportError("build record is not canonical JSON")
    return fields


def _snapshot_regular(path: Path, maximum: int, field: str) -> bytes:
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    try:
        fd = os.open(path, flags)
    except OSError as exc:
        raise PackageExportError(f"cannot open {field}: {path}") from exc
    try:
        metadata = os.fstat(fd)
        if not stat.S_ISREG(metadata.st_mode) or not 1 <= metadata.st_size <= maximum:
            raise PackageExportError(f"{field} must be a regular file of 1 through {maximum} bytes")
        chunks: list[bytes] = []
        remaining = metadata.st_size
        while remaining:
            chunk = os.read(fd, min(remaining, 1024 * 1024))
            if not chunk:
                raise PackageExportError(f"{field} was truncated while reading")
            chunks.append(chunk)
            remaining -= len(chunk)
        if os.read(fd, 1):
            raise PackageExportError(f"{field} grew while reading")
        return b"".join(chunks)
    finally:
        os.close(fd)


def _git(root: Path, *arguments: str) -> str:
    try:
        result = subprocess.run(
            ["git", "-C", str(root), *arguments],
            text=True,
            capture_output=True,
            env=dict(os.environ, GIT_LITERAL_PATHSPECS="1", GIT_NO_LAZY_FETCH="1", GIT_OPTIONAL_LOCKS="0"),
            check=True,
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        detail = exc.stderr.strip() if isinstance(exc, subprocess.CalledProcessError) and exc.stderr else str(exc)
        raise PackageExportError(f"Git input verification failed: {detail}") from exc
    return result.stdout.strip()


def _require_clean_repository(root: Path, revision: str, *, repository: str | None = None, allow_subdirectory: bool = False) -> None:
    actual_root = Path(_git(root, "rev-parse", "--show-toplevel")).resolve()
    if actual_root != root.resolve() and not (allow_subdirectory and root.resolve().is_relative_to(actual_root)):
        raise PackageExportError(f"build input is not rooted at its declared checkout: {root}")
    if _git(root, "rev-parse", "HEAD") != revision:
        raise PackageExportError(f"build input checkout is not at pinned revision: {root}")
    if _git(root, "status", "--porcelain", "--untracked-files=all", "--", "."):
        raise PackageExportError(f"build input checkout is not clean: {root}")
    if repository is not None:
        remotes = _git(root, "remote", "get-url", "--all", "origin").splitlines()
        try:
            remotes = [canonical_repository(remote) for remote in remotes]
        except ValueError as exc:
            raise PackageExportError(str(exc)) from exc
        if repository not in remotes:
            raise PackageExportError("main checkout origin does not match build.repository")


def _checked_input(root: Path, relative: str, field: str) -> Path:
    current = root
    for part in PurePosixPath(relative).parts:
        current = current / part
        try:
            metadata = current.lstat()
        except OSError as exc:
            raise PackageExportError(f"missing {field}: {relative}") from exc
        if stat.S_ISLNK(metadata.st_mode):
            raise PackageExportError(f"{field} must not contain symlinks: {relative}")
    if not stat.S_ISREG(current.stat().st_mode):
        raise PackageExportError(f"{field} must be a regular file: {relative}")
    return current


def _require_tracked(root: Path, relative: str, field: str) -> None:
    try:
        _git(root, "ls-files", "--error-unmatch", "--", relative)
    except PackageExportError as exc:
        raise PackageExportError(f"{field} is not tracked by the pinned source commit: {relative}") from exc


def source_input_closure(root: Path, roots: list[str]) -> dict[str, str]:
    """Hash every tracked regular file in declared modules, rejecting path escapes.

    The producer chooses conservative module roots, not an optimistic handpicked
    source list. Export repeats this enumeration, detecting additions/deletions.
    """
    result = {}
    for prefix in roots:
        _relative_path(prefix, "source root")
        if _git(root, "ls-files", "--others", "--exclude-standard", "--", prefix):
            raise PackageExportError(f"source root contains untracked inputs: {prefix}")
        entries = _git(root, "ls-files", "--stage", "-z", "--", prefix).split("\0")
        members = 0
        for entry in filter(None, entries):
            metadata, path = entry.split("\t", 1)
            mode, _, stage = metadata.split()
            if mode not in ("100644", "100755") or stage != "0":
                raise PackageExportError("source closure requires regular tracked files")
            _relative_path(path, "source input")
            if not (path == prefix or path.startswith(prefix + "/")):
                raise PackageExportError("source input is outside declared root")
            result[path] = hashlib.sha256(_checked_input(root, path, "source input").read_bytes()).hexdigest()
            members += 1
        if not members:
            raise PackageExportError(f"source root has no tracked inputs: {prefix}")
    return dict(sorted(result.items()))


def functional_record_fields(root: Path, fields: dict, roots: list[str], execution: dict) -> dict:
    """Add the shared v2 envelope to a producer's existing parameters."""
    from scripts.functional_execution import execution_digest
    if execution is None:
        raise PackageExportError("functional identity requires controlled execution inputs")
    git_root = Path(_git(root, "rev-parse", "--show-toplevel")).resolve()
    result = dict(fields, format=2, source_path=Path(root).resolve().relative_to(git_root).as_posix(),
                  source_roots=sorted(roots), source_inputs=source_input_closure(root, sorted(roots)))
    result["parameters"] = dict(fields["parameters"], execution_sha256=execution_digest(execution),
                                gpu_device=execution["gpu_device"])
    return result


def verify_record_source_at_revision(root: Path, record: bytes) -> dict:
    """Verify immutable recorded source bytes using Git objects, without checkout.

    This proves the source closure at the recorded commit, not current tool or
    hardware compatibility. The caller separately checks the payload/manifest
    and matches the functional record to its authenticated current selection.
    Missing history fails closed; promisor fetches are disabled.
    """
    fields = _decode_build_record(record)
    if fields["format"] != 2:
        raise PackageExportError("historical functional verification requires record format 2")
    git_root = Path(_git(root, "rev-parse", "--show-toplevel"))
    prefix = "" if fields["source_path"] == "." else fields["source_path"] + "/"
    revision = fields["revision"]
    def object_bytes(*args):
        result = subprocess.run(["git", "-C", str(git_root), *args], capture_output=True,
                                env=dict(os.environ, GIT_NO_LAZY_FETCH="1", GIT_OPTIONAL_LOCKS="0",
                                         GIT_TERMINAL_PROMPT="0", GIT_LITERAL_PATHSPECS="1"), check=False)
        if result.returncode:
            raise PackageExportError("recorded source history is unavailable")
        return result.stdout
    object_bytes("cat-file", "-e", revision + "^{commit}")
    actual = {}
    for source_root in fields["source_roots"]:
        entries = object_bytes("ls-tree", "-r", "-z", "--full-tree", revision, "--", prefix + source_root)
        if not entries:
            raise PackageExportError("recorded source root is missing")
        for entry in filter(None, entries.split(b"\0")):
            metadata, raw_path = entry.split(b"\t", 1)
            mode, kind, oid = metadata.split()
            if mode not in (b"100644", b"100755") or kind != b"blob":
                raise PackageExportError("recorded source closure contains nonregular inputs")
            path = raw_path.decode("utf-8")
            if not path.startswith(prefix):
                raise PackageExportError("recorded source is outside module")
            relative = path[len(prefix):]
            _relative_path(relative, "recorded source input")
            actual[relative] = hashlib.sha256(object_bytes("cat-file", "blob", oid.decode())).hexdigest()
    if actual != fields["source_inputs"]:
        raise PackageExportError("recorded source closure differs from Git revision")
    for relative, expected in fields["dependencies"].items():
        entry = object_bytes("ls-tree", revision, "--", prefix + relative).split()
        if len(entry) != 4 or entry[0] != b"160000" or entry[2].decode() != expected:
            raise PackageExportError("historical dependency is not the recorded Git submodule")
    return fields


def _verify_build_evidence(payload: Path, record: bytes, fields: dict, manifest_fields: dict) -> Path:
    try:
        root = Path(_git(payload.parent, "rev-parse", "--show-toplevel")).resolve()
    except PackageExportError as exc:
        raise PackageExportError("RBF must be contained by a pinned Git checkout") from exc
    if fields["format"] == 2 and fields["source_path"] != ".":
        for part in PurePosixPath(fields["source_path"]).parts:
            root = root / part
            if root.is_symlink() or not root.is_dir():
                raise PackageExportError("source_path must name a real module directory")
        if not payload.resolve().is_relative_to(root.resolve()):
            raise PackageExportError("RBF is outside its declared source module")
    build = manifest_fields["build"]
    for key in ("repository", "revision", "recipe_sha256"):
        if fields[key] != build[key]:
            raise PackageExportError(f"build record {key} does not match manifest")
    if build["id"] != build_identity(record):
        raise PackageExportError("manifest build.id does not match canonical build-input record")

    recipe = _checked_input(root, fields["recipe"], "recipe")
    abi_definition = _checked_input(root, fields["abi_definition"], "ABI definition")
    _require_tracked(root, fields["recipe"], "recipe")
    _require_tracked(root, fields["abi_definition"], "ABI definition")
    if hashlib.sha256(recipe.read_bytes()).hexdigest() != fields["recipe_sha256"]:
        raise PackageExportError("recipe bytes do not match build-input record")
    if hashlib.sha256(abi_definition.read_bytes()).hexdigest() != fields["abi_definition_sha256"]:
        raise PackageExportError("ABI definition bytes do not match build-input record")
    _require_clean_repository(root, fields["revision"], repository=fields["repository"],
                              **({"allow_subdirectory": True} if fields["format"] == 2 else {}))

    if fields["format"] == 2 and source_input_closure(root, fields["source_roots"]) != fields["source_inputs"]:
        raise PackageExportError("source closure differs from build-input record")

    for relative, revision in fields["dependencies"].items():
        dependency = root.joinpath(*PurePosixPath(relative).parts)
        for candidate in (root.joinpath(*PurePosixPath(relative).parts[:index]) for index in range(1, len(PurePosixPath(relative).parts) + 1)):
            try:
                if stat.S_ISLNK(candidate.lstat().st_mode):
                    raise PackageExportError(f"dependency path must not contain symlinks: {relative}")
            except OSError as exc:
                raise PackageExportError(f"missing dependency checkout: {relative}") from exc
        _require_clean_repository(dependency, revision)
    return root


def _archive_bytes(manifest: bytes, payload: bytes) -> bytes:
    def member(name: str, data: bytes) -> bytes:
        return _ustar_header(name, len(data)) + data + b"\0" * ((-len(data)) % 512)

    return member("manifest.toml", manifest) + member("core.rbf", payload) + b"\0" * 1024


def _write_sealed(path: Path, data: bytes) -> None:
    with path.open("xb") as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    path.chmod(0o444)


def _publish_no_replace(temporary: Path, final: Path) -> None:
    libc = ctypes.CDLL(None, use_errno=True)
    try:
        renameat2 = libc.renameat2
    except AttributeError as exc:
        raise PackageExportError("no-replace publication is unavailable on this Linux host") from exc
    renameat2.argtypes = (ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint)
    renameat2.restype = ctypes.c_int
    if renameat2(AT_FDCWD, os.fsencode(temporary), AT_FDCWD, os.fsencode(final), RENAME_NOREPLACE) == 0:
        return
    error = ctypes.get_errno()
    if error == errno.EEXIST:
        raise PackageExportError(f"package destination became occupied: {final}")
    raise OSError(error, os.strerror(error), final)


def _is_read_only(path: Path) -> bool:
    return stat.S_IMODE(path.stat().st_mode) & 0o222 == 0


def _reuse_existing(directory: Path, archive: Path, evidence: Path, manifest: bytes, payload: bytes, archive_bytes: bytes, record: bytes) -> Path:
    if not all(path.exists() and not path.is_symlink() for path in (directory, archive, evidence)):
        raise PackageExportError("existing package export is incomplete or contains a symlink")
    if not all(_is_read_only(path) for path in (directory, archive, evidence)):
        raise PackageExportError("existing package export is writable")
    members = (directory / "manifest.toml", directory / "core.rbf")
    if not all(path.exists() and not path.is_symlink() and _is_read_only(path) for path in members):
        raise PackageExportError("existing package members are missing, linked, or writable")
    package = read_package(directory)
    if package.manifest_bytes != manifest or package.payload_bytes != payload:
        raise PackageExportError("existing package directory differs")
    if _snapshot_regular(archive, 33 * 1024 * 1024, "existing archive") != archive_bytes:
        raise PackageExportError("existing package archive differs")
    if _snapshot_regular(evidence, MAX_BUILD_RECORD_SIZE, "existing build evidence") != record:
        raise PackageExportError("existing package build evidence differs")
    return directory


def export_package(manifest: bytes, payload: Path, destination: Path) -> Path:
    """Verify pinned build inputs and publish a content-addressed directory and archive."""

    if not isinstance(manifest, bytes):
        raise TypeError("manifest must be bytes")
    payload = Path(payload)
    snapshot = _snapshot_regular(payload, MAX_PAYLOAD_SIZE, "RBF payload")
    try:
        manifest_fields = _decode_manifest(manifest, snapshot)
    except PackageError as exc:
        raise PackageExportError(str(exc)) from exc
    record_path = payload.with_name("build-inputs.json")
    record = _snapshot_regular(record_path, MAX_BUILD_RECORD_SIZE, "build-input record")
    record_fields = _decode_build_record(record)
    root = _verify_build_evidence(payload, record, record_fields, manifest_fields)

    package_id = package_identity(manifest, snapshot)
    store = Path(destination)
    store.mkdir(parents=True, exist_ok=True)
    if store.is_symlink() or not store.is_dir():
        raise PackageExportError(f"output store must be a non-symlink directory: {store}")
    directory = store / package_id
    archive = directory.with_suffix(".fcore")
    evidence = directory.with_suffix(".build-inputs.json")
    archive_data = _archive_bytes(manifest, snapshot)
    if any(path.exists() or path.is_symlink() for path in (directory, archive, evidence)):
        return _reuse_existing(directory, archive, evidence, manifest, snapshot, archive_data, record)

    staging = Path(tempfile.mkdtemp(prefix=".export-files-", dir=store))
    staged_directory = Path(tempfile.mkdtemp(prefix=".export-package-", dir=store))
    staged_archive = staging / "package.fcore"
    staged_evidence = staging / "build-inputs.json"
    published: list[Path] = []
    try:
        _write_sealed(staged_directory / "manifest.toml", manifest)
        _write_sealed(staged_directory / "core.rbf", snapshot)
        staged_directory.chmod(0o555)
        _write_sealed(staged_archive, archive_data)
        _write_sealed(staged_evidence, record)
        for source, final in ((staged_archive, archive), (staged_evidence, evidence), (staged_directory, directory)):
            _publish_no_replace(source, final)
            published.append(final)
        store_fd = os.open(store, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(store_fd)
        finally:
            os.close(store_fd)
        _require_clean_repository(root, record_fields["revision"], repository=record_fields["repository"],
                                  **({"allow_subdirectory": True} if record_fields["format"] == 2 else {}))
        if record_fields["format"] == 2 and source_input_closure(root, record_fields["source_roots"]) != record_fields["source_inputs"]:
            raise PackageExportError("source closure changed during export")
        for relative, revision in record_fields["dependencies"].items():
            _require_clean_repository(root.joinpath(*PurePosixPath(relative).parts), revision)
    except Exception:
        for path in reversed(published):
            try:
                if path.is_dir():
                    path.chmod(0o755)
                    shutil.rmtree(path)
                else:
                    path.chmod(0o644)
                    path.unlink()
            except OSError:
                pass
        raise
    finally:
        if staged_directory.exists():
            staged_directory.chmod(0o755)
            shutil.rmtree(staged_directory)
        if staging.exists():
            shutil.rmtree(staging)

    return directory


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--rbf", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path, help="content-addressed package store")
    arguments = parser.parse_args(argv)
    try:
        manifest = _snapshot_regular(arguments.manifest, MAX_MANIFEST_SIZE, "manifest")
        print(export_package(manifest, arguments.rbf, arguments.output))
    except (OSError, PackageExportError, PackageError) as exc:
        print(f"export-core-package: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
