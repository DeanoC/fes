#!/usr/bin/env python3
"""Identity, publication, and read-only verification for shared toolchains.

The cache is deliberately small in scope.  It does not build tools itself and
it does not know about FPGA outputs; bootstrap owns those operations.  This
module owns the stable paths and the ready contract consumed by bootstrap and
the authenticated build scripts.
"""

from __future__ import annotations

import contextlib
import dataclasses
import fcntl
import hashlib
import json
import os
import platform
import shutil
import stat
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any, Iterator, Mapping


SCHEMA_VERSION = 1
DEFAULT_GPU_ROUTER = "OFF"
DEFAULT_HIP_ARCHITECTURES = "gfx1100;gfx1201"
_HELD_LOCKS: set[tuple[str, str]] = set()
_RUNTIME_ENVIRONMENT = frozenset(
    {
        "PATH",
        "HOME",
        "USER",
        "LOGNAME",
        "TMPDIR",
        "SHELL",
        "TERM",
        "LANG",
        "LC_ALL",
        "TZ",
    }
)
_DISALLOWED_OVERRIDE_NAMES = frozenset(
    {
        "CC",
        "CXX",
        "CPPFLAGS",
        "CFLAGS",
        "CXXFLAGS",
        "LDFLAGS",
        "LD_LIBRARY_PATH",
        "PKG_CONFIG_PATH",
        "ROCM_PATH",
        "HIPCC",
        "CUDA_HOME",
        "CUDACXX",
        "CUDA_PATH",
        "PYTHON",
        "PYTHON_CONFIG",
    }
)
_ALLOWED_CMAKE_NAMES = frozenset(
    {
        "CMAKE_PREFIX_PATH",
        "CMAKE_GENERATOR",
        "CMAKE_TOOLCHAIN_FILE",
        "CMAKE_BUILD_TYPE",
        "CMAKE_C_COMPILER",
        "CMAKE_CXX_COMPILER",
        "CMAKE_HIP_COMPILER",
        "CMAKE_CUDA_COMPILER",
        "CMAKE_HIP_ARCHITECTURES",
        "CMAKE_CUDA_ARCHITECTURES",
        "CMAKE_SYSROOT",
        "CMAKE_MODULE_PATH",
        "CMAKE_FIND_ROOT_PATH",
        "CMAKE_EXE_LINKER_FLAGS",
        "CMAKE_SHARED_LINKER_FLAGS",
        "CMAKE_INSTALL_LIBDIR",
        "CMAKE_C_FLAGS",
        "CMAKE_CXX_FLAGS",
        "CMAKE_HIP_FLAGS",
        "CMAKE_CUDA_FLAGS",
    }
)


class CacheError(RuntimeError):
    """Raised when a shared cache request or ready slot is unsafe."""


@dataclasses.dataclass(frozen=True)
class ToolchainRequest:
    """All inputs that select one complete compiler lane."""

    root: Path
    lock_path: Path
    cache_root: Path
    gpu_router: str = DEFAULT_GPU_ROUTER
    hip_architectures: str = DEFAULT_HIP_ARCHITECTURES
    host_identity: Mapping[str, Any] = dataclasses.field(default_factory=dict)
    compiler_identity: Mapping[str, Any] = dataclasses.field(default_factory=dict)
    environment: Mapping[str, Any] = dataclasses.field(default_factory=dict)
    recipe_paths: tuple[Path, ...] = ()


@dataclasses.dataclass(frozen=True)
class ToolchainManifest:
    """Decoded ready.json data with paths represented as ``Path`` objects."""

    schema: int
    key: str
    slot: Path
    source: Path
    build: Path
    install: Path
    evidence: Path
    lock: Mapping[str, Any]
    recipes: Mapping[str, Any]
    host: Mapping[str, Any]
    compiler: Mapping[str, Any]
    configuration: Mapping[str, Any]
    lane: Mapping[str, Any]
    tools: Mapping[str, Mapping[str, Any]]
    files: Mapping[str, Mapping[str, Any]]


def _canonical(value: Any) -> bytes:
    try:
        encoded = json.dumps(
            value,
            sort_keys=True,
            separators=(",", ":"),
            ensure_ascii=False,
        ).encode("utf-8")
    except (TypeError, ValueError) as exc:
        raise CacheError(f"cache identity is not JSON-serializable: {exc}") from exc
    if b"\x00" in encoded or b"\n" in encoded or b"\r" in encoded:
        raise CacheError("cache identity contains an unsafe control character")
    return encoded


def _sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def _sha256_file(path: Path) -> str:
    try:
        with path.open("rb") as stream:
            digest = hashlib.sha256()
            for block in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(block)
            return digest.hexdigest()
    except OSError as exc:
        raise CacheError(f"cannot hash {path}: {exc}") from exc


def _regular_file(path: Path, label: str) -> None:
    try:
        info = path.lstat()
    except OSError as exc:
        raise CacheError(f"cannot inspect {label}: {path}: {exc}") from exc
    if not stat.S_ISREG(info.st_mode):
        raise CacheError(f"{label} must be a regular file: {path}")


def _recipe_paths(request: ToolchainRequest) -> tuple[Path, ...]:
    if request.recipe_paths:
        return tuple(Path(path) for path in request.recipe_paths)
    return (
        request.root / "scripts" / "bootstrap.sh",
        request.root / "scripts" / "lockfile.py",
        request.root / "scripts" / "toolchain_cache.py",
    )


def _recipe_digests(request: ToolchainRequest) -> list[str]:
    digests: list[str] = []
    for path in _recipe_paths(request):
        _regular_file(path, "recipe")
        digests.append(_sha256_file(path))
    return digests


def _normalise_configuration(request: ToolchainRequest) -> dict[str, Any]:
    router = str(request.gpu_router or DEFAULT_GPU_ROUTER).upper()
    if router not in {"OFF", "HIP", "CUDA"}:
        raise CacheError(f"unsupported GPU router for shared cache: {router}")
    architectures = str(request.hip_architectures or DEFAULT_HIP_ARCHITECTURES)
    return {
        "gpu-router": router,
        "hip-architectures": architectures if router == "HIP" else "unused",
    }


def cache_key(request: ToolchainRequest) -> str:
    """Return a path-independent identity for *request*."""

    configuration = _normalise_configuration(request)
    recipe_digests = _recipe_digests(request)
    try:
        lock_bytes = request.lock_path.read_bytes()
    except OSError as exc:
        raise CacheError(f"cannot read toolchain lock {request.lock_path}: {exc}") from exc
    payload = {
        "schema": SCHEMA_VERSION,
        "lock-sha256": _sha256_bytes(lock_bytes),
        "recipes": recipe_digests,
        "host": dict(request.host_identity),
        "compiler": dict(request.compiler_identity),
        "environment": dict(request.environment),
        "configuration": configuration,
    }
    return _sha256_bytes(_canonical(payload))


def _absolute(path: Path, label: str) -> Path:
    path = Path(path)
    if not path.is_absolute():
        raise CacheError(f"{label} must be absolute: {path}")
    return path


def _check_no_symlink_components(path: Path, *, allow_missing: bool = True) -> None:
    """Reject symlink components without resolving away the evidence."""

    path = _absolute(path, "path")
    current = Path(path.anchor)
    relative = path.relative_to(current)
    components = relative.parts
    for index, component in enumerate(components):
        current /= component
        try:
            info = current.lstat()
        except FileNotFoundError:
            if allow_missing:
                return
            raise CacheError(f"missing path component: {current}")
        except OSError as exc:
            raise CacheError(f"cannot inspect path component {current}: {exc}") from exc
        if stat.S_ISLNK(info.st_mode):
            raise CacheError(f"cache path contains a symlink component: {current}")
        if index < len(components) - 1 and not stat.S_ISDIR(info.st_mode):
            raise CacheError(f"cache path component is not a directory: {current}")


def ensure_cache_root(path: Path) -> Path:
    """Validate/create a private same-user cache root."""

    path = _absolute(Path(path), "cache root")
    _check_no_symlink_components(path)
    try:
        if not path.exists():
            path.mkdir(mode=0o700, parents=True)
        info = path.lstat()
    except OSError as exc:
        raise CacheError(f"cannot create or inspect cache root {path}: {exc}") from exc
    if not stat.S_ISDIR(info.st_mode):
        raise CacheError(f"cache root is not a directory: {path}")
    if info.st_uid != os.getuid():
        raise CacheError(f"cache root is owned by uid {info.st_uid}, not uid {os.getuid()}: {path}")
    if stat.S_IMODE(info.st_mode) & 0o022:
        raise CacheError(f"cache root is writable by group/other: {path}")
    for child in (path / "slots", path / "locks"):
        _check_no_symlink_components(child)
        try:
            child.mkdir(mode=0o700, exist_ok=True)
            child_info = child.lstat()
        except OSError as exc:
            raise CacheError(f"cannot create cache directory {child}: {exc}") from exc
        if not stat.S_ISDIR(child_info.st_mode) or child_info.st_uid != os.getuid():
            raise CacheError(f"cache directory has unsafe ownership/type: {child}")
        if stat.S_IMODE(child_info.st_mode) & 0o022:
            # These directories are cache-owned control paths. Tightening an
            # owner-created mode does not relabel or remove any installation.
            try:
                child.chmod(0o700)
                child_info = child.lstat()
            except OSError as exc:
                raise CacheError(f"cache directory is writable by group/other: {child}") from exc
            if stat.S_IMODE(child_info.st_mode) & 0o022:
                raise CacheError(f"cache directory is writable by group/other: {child}")
    return path


def slot_path(request: ToolchainRequest) -> Path:
    return _absolute(Path(request.cache_root), "cache root") / "slots" / cache_key(request)


def ready_path(request: ToolchainRequest) -> Path:
    return slot_path(request) / "ready.json"


def lock_path(request: ToolchainRequest) -> Path:
    return _absolute(Path(request.cache_root), "cache root") / "locks" / f"{cache_key(request)}.lock"


def ensure_lock_file(request: ToolchainRequest) -> Path:
    """Create/check the per-key lock inode without following symlinks."""

    root = ensure_cache_root(request.cache_root)
    path = root / "locks" / f"{cache_key(request)}.lock"
    try:
        descriptor = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            info = os.fstat(descriptor)
            if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid():
                raise CacheError(f"cache lock has unsafe type/ownership: {path}")
            os.fchmod(descriptor, 0o600)
        finally:
            os.close(descriptor)
    except OSError as exc:
        raise CacheError(f"cannot create/check cache lock {path}: {exc}") from exc
    return path


@contextlib.contextmanager
def acquire_build_lock(request: ToolchainRequest) -> Iterator[None]:
    """Hold the exclusive per-key lock for the whole build/publication."""

    root = ensure_cache_root(request.cache_root)
    key = cache_key(request)
    path = root / "locks" / f"{key}.lock"
    try:
        descriptor = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    except AttributeError as exc:  # pragma: no cover - old non-POSIX Python
        raise CacheError("shared cache requires O_NOFOLLOW support") from exc
    except OSError as exc:
        raise CacheError(f"cannot open cache lock {path}: {exc}") from exc
    try:
        os.fchmod(descriptor, 0o600)
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX)
        except OSError as exc:
            raise CacheError(f"shared cache requires a working flock lock: {exc}") from exc
        _HELD_LOCKS.add((str(root), key))
        yield
    finally:
        _HELD_LOCKS.discard((str(root), key))
        try:
            fcntl.flock(descriptor, fcntl.LOCK_UN)
        finally:
            os.close(descriptor)


def _relative_link_target(path: Path, root: Path) -> str:
    try:
        target = os.readlink(path)
    except OSError as exc:
        raise CacheError(f"cannot read symlink {path}: {exc}") from exc
    if not target or os.path.isabs(target) or "\x00" in target or "\n" in target or "\r" in target:
        raise CacheError(f"unsafe symlink target at {path}: {target!r}")
    resolved = (path.parent / target).resolve(strict=False)
    root_resolved = root.resolve(strict=False)
    try:
        resolved.relative_to(root_resolved)
    except ValueError as exc:
        raise CacheError(f"symlink escapes cache tree: {path} -> {target}") from exc
    if not resolved.exists() or not resolved.is_file():
        raise CacheError(f"symlink is dangling or does not resolve to a file: {path} -> {target}")
    return target


def _scan_tree(root: Path, label: str) -> dict[str, dict[str, Any]]:
    """Scan regular files and supported internal links below one root."""

    root = Path(root)
    try:
        root_info = root.lstat()
    except OSError as exc:
        raise CacheError(f"cannot inspect {label} root {root}: {exc}") from exc
    if not stat.S_ISDIR(root_info.st_mode):
        raise CacheError(f"{label} root must be a directory: {root}")

    result: dict[str, dict[str, Any]] = {}

    def visit(current: Path, relative: Path) -> None:
        try:
            entries = sorted(current.iterdir(), key=lambda item: item.name)
        except OSError as exc:
            raise CacheError(f"cannot enumerate {label} directory {current}: {exc}") from exc
        for entry in entries:
            entry_relative = relative / entry.name
            key = f"{label}/{entry_relative.as_posix()}"
            try:
                info = entry.lstat()
            except OSError as exc:
                raise CacheError(f"cannot inspect {label} entry {entry}: {exc}") from exc
            if stat.S_ISDIR(info.st_mode):
                visit(entry, entry_relative)
            elif stat.S_ISREG(info.st_mode):
                result[key] = {
                    "type": "file",
                    "sha256": _sha256_file(entry),
                    "size": info.st_size,
                    "mode": stat.S_IMODE(info.st_mode),
                }
            elif stat.S_ISLNK(info.st_mode):
                result[key] = {
                    "type": "symlink",
                    "target": _relative_link_target(entry, root),
                    "mode": stat.S_IMODE(info.st_mode),
                }
            else:
                raise CacheError(f"unsupported {label} entry type: {entry}")

    visit(root, Path())
    return result


def _safe_child(root: Path, relative: str, label: str) -> Path:
    candidate = Path(relative)
    if candidate.is_absolute() or not relative or any(part in {"", ".", ".."} for part in candidate.parts):
        raise CacheError(f"{label} must be a clean relative path: {relative!r}")
    path = root / candidate
    try:
        path.relative_to(root)
    except ValueError as exc:
        raise CacheError(f"{label} escapes its root: {relative!r}") from exc
    return path


def _manifest_dict(manifest: ToolchainManifest) -> dict[str, Any]:
    return {
        "schema": manifest.schema,
        "key": manifest.key,
        "slot": str(manifest.slot),
        "source": str(manifest.source),
        "build": str(manifest.build),
        "install": str(manifest.install),
        "evidence": str(manifest.evidence),
        "lock": dict(manifest.lock),
        "recipes": dict(manifest.recipes),
        "host": dict(manifest.host),
        "compiler": dict(manifest.compiler),
        "configuration": dict(manifest.configuration),
        "lane": dict(manifest.lane),
        "tools": {name: dict(value) for name, value in manifest.tools.items()},
        "files": {name: dict(value) for name, value in manifest.files.items()},
    }


def _manifest_from_dict(data: Mapping[str, Any]) -> ToolchainManifest:
    required = {
        "schema",
        "key",
        "slot",
        "source",
        "build",
        "install",
        "evidence",
        "lock",
        "recipes",
        "host",
        "compiler",
        "configuration",
        "lane",
        "tools",
        "files",
    }
    if set(data) != required:
        raise CacheError("ready manifest fields are not exact")
    try:
        schema = int(data["schema"])
    except (TypeError, ValueError) as exc:
        raise CacheError("ready manifest schema is invalid") from exc
    if schema != SCHEMA_VERSION:
        raise CacheError(f"unsupported ready manifest schema: {schema}")
    if not isinstance(data["key"], str) or len(data["key"]) != 64:
        raise CacheError("ready manifest key is invalid")
    for field in ("slot", "source", "build", "install", "evidence"):
        if not isinstance(data[field], str) or not Path(data[field]).is_absolute():
            raise CacheError(f"ready manifest {field} path is invalid")
    for field in ("lock", "recipes", "host", "compiler", "configuration", "lane", "tools", "files"):
        if not isinstance(data[field], dict):
            raise CacheError(f"ready manifest {field} must be an object")
    return ToolchainManifest(
        schema=schema,
        key=data["key"],
        slot=Path(data["slot"]),
        source=Path(data["source"]),
        build=Path(data["build"]),
        install=Path(data["install"]),
        evidence=Path(data["evidence"]),
        lock=data["lock"],
        recipes=data["recipes"],
        host=data["host"],
        compiler=data["compiler"],
        configuration=data["configuration"],
        lane=data["lane"],
        tools=data["tools"],
        files=data["files"],
    )


def _check_readonly(root: Path, label: str) -> None:
    for current, directories, files in os.walk(root, topdown=True, followlinks=False):
        current_path = Path(current)
        try:
            current_info = current_path.lstat()
        except OSError as exc:
            raise CacheError(f"cannot inspect {label} directory {current_path}: {exc}") from exc
        if stat.S_IMODE(current_info.st_mode) & 0o222:
            raise CacheError(f"published {label} directory is writable: {current_path}")
        for name in directories:
            path = current_path / name
            if path.is_symlink():
                # The scanner will reject directory links; keep the error clear.
                raise CacheError(f"published {label} directory is a symlink: {path}")
        for name in files:
            path = current_path / name
            info = path.lstat()
            if stat.S_ISLNK(info.st_mode):
                continue
            if stat.S_IMODE(info.st_mode) & 0o222:
                raise CacheError(f"published {label} file is writable: {path}")


def _freeze_tree(root: Path) -> None:
    for current, directories, files in os.walk(root, topdown=False, followlinks=False):
        current_path = Path(current)
        for name in files:
            path = current_path / name
            info = path.lstat()
            if not stat.S_ISLNK(info.st_mode):
                path.chmod(stat.S_IMODE(info.st_mode) & ~0o222)
        for name in directories:
            path = current_path / name
            info = path.lstat()
            if not stat.S_ISLNK(info.st_mode):
                path.chmod(stat.S_IMODE(info.st_mode) & ~0o222)
    info = root.lstat()
    root.chmod(stat.S_IMODE(info.st_mode) & ~0o222)


def _check_slot_paths(request: ToolchainRequest, slot: Path) -> tuple[Path, Path, Path, Path]:
    expected = slot_path(request)
    if slot != expected:
        raise CacheError(f"slot path is not the request's stable slot: {slot} != {expected}")
    _check_no_symlink_components(expected, allow_missing=False)
    source, build, install, evidence = (expected / name for name in ("src", "build", "install", "evidence"))
    for path, label in ((source, "source"), (build, "build"), (install, "install"), (evidence, "evidence")):
        _check_no_symlink_components(path, allow_missing=False)
        try:
            if not path.is_dir():
                raise CacheError(f"{label} path is not a directory: {path}")
        except OSError as exc:
            raise CacheError(f"cannot inspect {label} path {path}: {exc}") from exc
        info = path.lstat()
        if info.st_uid != os.getuid():
            raise CacheError(f"{label} path is not owned by the cache user: {path}")
    return source, build, install, evidence


def publish_ready(
    request: ToolchainRequest,
    *,
    tools: Mapping[str, Mapping[str, Any]],
    _lock_held: bool = False,
) -> ToolchainManifest:
    """Verify a completed slot, freeze consumer trees, and atomically publish it."""

    root = ensure_cache_root(request.cache_root)
    key = cache_key(request)
    if not _lock_held and (str(root), key) not in _HELD_LOCKS:
        raise CacheError("ready publication must run under the per-key build lock")
    slot = slot_path(request)
    source, build, install, evidence = _check_slot_paths(request, slot)
    ready = slot / "ready.json"
    if ready.exists() or ready.is_symlink():
        raise CacheError(f"ready manifest already exists; refusing to relabel slot: {ready}")
    if not tools:
        raise CacheError("cannot publish a toolchain with no authenticated tools")
    if not any(evidence.iterdir()):
        raise CacheError("cannot publish a toolchain without consumer evidence")

    # Verify the requested executable records before changing permissions.
    tool_records: dict[str, dict[str, Any]] = {}
    for name, supplied in sorted(tools.items()):
        if not isinstance(supplied, Mapping):
            raise CacheError(f"tool record is malformed: {name}")
        binary_name = supplied.get("binary")
        commit = supplied.get("commit")
        identity = supplied.get("identity")
        if not isinstance(binary_name, str) or not isinstance(commit, str) or not commit:
            raise CacheError(f"tool record has no binary/commit: {name}")
        if not isinstance(identity, str) or not identity.strip():
            raise CacheError(f"tool record has no identity output: {name}")
        binary = _safe_child(install, binary_name, f"{name} binary")
        try:
            info = binary.lstat()
        except OSError as exc:
            raise CacheError(f"missing tool binary {binary}: {exc}") from exc
        if stat.S_ISLNK(info.st_mode):
            _relative_link_target(binary, install)
        elif not stat.S_ISREG(info.st_mode):
            raise CacheError(f"tool binary is not a regular file or internal link: {binary}")
        tool_records[name] = {
            "binary": binary_name,
            "path": f"install/{Path(binary_name).as_posix()}",
            "commit": commit,
            "identity": identity,
            "sha256": _sha256_file(binary),
        }

    # The final paths are used while compiling; only the consumer trees are
    # frozen.  This also makes mode values in ready.json the post-publication
    # values rather than transient build modes.
    _freeze_tree(install)
    _freeze_tree(evidence)
    files = {}
    files.update(_scan_tree(install, "install"))
    files.update(_scan_tree(evidence, "evidence"))
    _check_readonly(install, "install")
    _check_readonly(evidence, "evidence")
    lock_bytes = request.lock_path.read_bytes()
    manifest = ToolchainManifest(
        schema=SCHEMA_VERSION,
        key=key,
        slot=slot,
        source=source,
        build=build,
        install=install,
        evidence=evidence,
        lock={"sha256": _sha256_bytes(lock_bytes)},
        recipes={str(index): digest for index, digest in enumerate(_recipe_digests(request))},
        host=dict(request.host_identity),
        compiler=dict(request.compiler_identity),
        configuration=_normalise_configuration(request),
        lane={"kind": "shared", "key": key, "install": str(install)},
        tools=tool_records,
        files=files,
    )
    data = _canonical(_manifest_dict(manifest)) + b"\n"
    temporary = slot / ".ready.json.tmp"
    try:
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, ready)
        ready.chmod(0o444)
        directory_fd = os.open(slot, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
    except OSError as exc:
        try:
            temporary.unlink()
        except OSError:
            pass
        raise CacheError(f"cannot atomically publish ready manifest {ready}: {exc}") from exc
    return manifest


def _verify_files(manifest: ToolchainManifest) -> None:
    actual: dict[str, dict[str, Any]] = {}
    actual.update(_scan_tree(manifest.install, "install"))
    actual.update(_scan_tree(manifest.evidence, "evidence"))
    if actual != dict(manifest.files):
        raise CacheError("published install/evidence closure differs from ready manifest")
    _check_readonly(manifest.install, "install")
    _check_readonly(manifest.evidence, "evidence")
    # Every link must resolve to another manifest-covered entry in its own root.
    for key, record in actual.items():
        if record.get("type") != "symlink":
            continue
        prefix, relative = key.split("/", 1)
        root = manifest.install if prefix == "install" else manifest.evidence
        target = (root / relative).parent / record["target"]
        resolved = target.resolve(strict=False)
        try:
            resolved.relative_to(root.resolve(strict=False))
        except ValueError as exc:
            raise CacheError(f"published symlink escapes its root: {key}") from exc
        target_key = f"{prefix}/{resolved.relative_to(root.resolve(strict=False)).as_posix()}"
        if target_key not in actual:
            raise CacheError(f"published symlink target is not manifest-covered: {key}")


def verify_ready(request: ToolchainRequest) -> ToolchainManifest:
    """Verify an existing ready slot without modifying any cache file."""

    expected_slot = slot_path(request)
    ready = expected_slot / "ready.json"
    _check_no_symlink_components(expected_slot, allow_missing=False)
    try:
        info = ready.lstat()
    except OSError as exc:
        raise CacheError(f"ready manifest is missing: {ready}") from exc
    if not stat.S_ISREG(info.st_mode):
        raise CacheError(f"ready manifest is not a regular file: {ready}")
    try:
        data = json.loads(ready.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise CacheError(f"cannot read ready manifest {ready}: {exc}") from exc
    if not isinstance(data, dict):
        raise CacheError("ready manifest must be a JSON object")
    manifest = _manifest_from_dict(data)
    if manifest.key != cache_key(request):
        raise CacheError("ready manifest key does not match the current request")
    if manifest.slot != expected_slot:
        raise CacheError("ready manifest is non-relocatable and points at another slot")
    expected_paths = {
        "source": expected_slot / "src",
        "build": expected_slot / "build",
        "install": expected_slot / "install",
        "evidence": expected_slot / "evidence",
    }
    for name, expected in expected_paths.items():
        if getattr(manifest, name) != expected:
            raise CacheError(f"ready manifest {name} path is not the stable slot path")
    lock_digest = _sha256_file(request.lock_path)
    if manifest.lock.get("sha256") != lock_digest:
        raise CacheError("ready manifest lock digest does not match the request")
    expected_recipes = {str(index): digest for index, digest in enumerate(_recipe_digests(request))}
    if dict(manifest.recipes) != expected_recipes:
        raise CacheError("ready manifest recipe digest does not match the request")
    if dict(manifest.configuration) != _normalise_configuration(request):
        raise CacheError("ready manifest GPU configuration does not match the request")
    if manifest.lane.get("kind") != "shared" or manifest.lane.get("key") != manifest.key:
        raise CacheError("ready manifest lane attestation is invalid")
    if manifest.lane.get("install") != str(manifest.install):
        raise CacheError("ready manifest lane prefix is invalid")
    _check_slot_paths(request, expected_slot)
    _verify_files(manifest)
    for name, record in manifest.tools.items():
        if not isinstance(record, Mapping):
            raise CacheError(f"ready manifest tool record is malformed: {name}")
        binary_name = record.get("binary")
        if not isinstance(binary_name, str):
            raise CacheError(f"ready manifest tool binary is missing: {name}")
        binary = _safe_child(manifest.install, binary_name, f"{name} binary")
        if record.get("sha256") != _sha256_file(binary):
            raise CacheError(f"ready manifest tool digest changed: {name}")
    return manifest


def resolve_ready(request: ToolchainRequest) -> ToolchainManifest:
    """Alias used by consumers to make the read-only intent explicit."""

    return verify_ready(request)


def stage_evidence(request: ToolchainRequest, build_root: Path) -> Path:
    """Copy only small authentication/configuration records into the slot."""

    slot = slot_path(request)
    evidence = slot / "evidence"
    _check_no_symlink_components(evidence)
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    build_root = Path(build_root)
    if not build_root.is_absolute():
        raise CacheError(f"build evidence root must be absolute: {build_root}")
    for tool_dir in sorted(build_root.iterdir(), key=lambda item: item.name):
        if not tool_dir.is_dir() or tool_dir.is_symlink():
            continue
        for entry in sorted(tool_dir.iterdir(), key=lambda item: item.name):
            if entry.name.startswith((".built-", ".digest-", ".identity-", ".config-")) or entry.name in {
                "CMakeCache.txt",
                "build.ninja",
            }:
                if entry.is_symlink() or not entry.is_file():
                    raise CacheError(f"unsupported evidence entry: {entry}")
                destination = evidence / tool_dir.name / entry.name
                destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                shutil.copy2(entry, destination)
    if not any(evidence.iterdir()):
        raise CacheError(f"no tool authentication evidence found below {build_root}")
    return evidence


def _command_identity(command: str, args: tuple[str, ...] = ("--version",)) -> dict[str, Any]:
    executable = shutil.which(command)
    if executable is None:
        raise CacheError(f"required host command is missing: {command}")
    path = Path(executable)
    try:
        resolved = path.resolve(strict=True)
    except OSError as exc:
        raise CacheError(f"cannot resolve host command {command}: {path}: {exc}") from exc
    _regular_file(resolved, "host command")
    try:
        result = subprocess.run(
            [str(resolved), *args],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=15,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise CacheError(f"cannot probe host command {command}: {exc}") from exc
    return {
        "path": str(resolved),
        "sha256": _sha256_file(resolved),
        "version": result.stdout,
        "returncode": result.returncode,
    }


def host_identity() -> dict[str, Any]:
    """Collect the supported powerboat host identity for a cache request."""

    if platform.system() != "Linux" or platform.machine() != "x86_64":
        raise CacheError("shared toolchain cache supports Linux x86-64 only")
    libc_name, libc_version = platform.libc_ver()
    if libc_name.lower() != "glibc":
        raise CacheError("shared toolchain cache supports glibc Linux hosts only")
    return {
        "system": platform.system(),
        "release": platform.release(),
        "machine": platform.machine(),
        "libc": f"{libc_name}-{libc_version}",
        "python": platform.python_version(),
    }


def compiler_inventory(gpu_router: str) -> dict[str, Any]:
    """Probe required build commands and the selected accelerator compiler."""

    commands = {
        name: _command_identity(name)
        for name in (
            "git",
            "cc",
            "c++",
            "cmake",
            "ninja",
            "make",
            "perl",
            "python3",
            "python3-config",
            "pkg-config",
            "autoconf",
            "flex",
            "bison",
            "help2man",
        )
    }
    router = str(gpu_router or DEFAULT_GPU_ROUTER).upper()
    if router == "HIP":
        commands["hipcc"] = _command_identity("hipcc")
    elif router == "CUDA":
        commands["nvcc"] = _command_identity("nvcc", ("--version",))
    return {"commands": commands, "dependencies": _dependency_inventory(commands)}


def validate_shared_environment(
    values: Mapping[str, str] | None = None,
    *,
    gpu_router: str | None = None,
    hip_architectures: str | None = None,
    allow_resolved_paths: bool = False,
) -> None:
    """Reject build-affecting overrides outside the v1 shared contract."""

    values = dict(os.environ if values is None else values)
    router = str(gpu_router or values.get("FES_TOOLCHAIN_GPU_ROUTER", DEFAULT_GPU_ROUTER)).upper()
    architectures = str(
        hip_architectures
        or values.get("FES_TOOLCHAIN_HIP_ARCHITECTURES", DEFAULT_HIP_ARCHITECTURES)
    )
    for name, value in values.items():
        if not value or name in _RUNTIME_ENVIRONMENT:
            continue
        if name.startswith("LC_"):
            continue
        if name in {"FES_TOOLCHAIN_CACHE_ROOT", "FES_TOOLCHAIN_LOCKFILE", "FES_TOOLCHAIN_ROOT", "FES_TOOLCHAIN_GPU_ROUTER", "FES_TOOLCHAIN_HIP_ARCHITECTURES", "FES_TOOLCHAIN_CACHE_RESOLVED"}:
            continue
        if name in {"TOOLCHAIN_ROOT", "TOOLCHAIN_INSTALL", "TOOLCHAIN_BUILD"}:
            if allow_resolved_paths and name in {"TOOLCHAIN_ROOT", "TOOLCHAIN_INSTALL", "TOOLCHAIN_BUILD"}:
                continue
            raise CacheError(f"shared cache rejects local toolchain override: {name}")
        if name in {"CMAKE_HIP_ARCHITECTURES"} and router == "HIP" and value == architectures:
            continue
        if name in {"ROCM_PATH", "HIPCC"} and router == "HIP":
            continue
        if name in {"CUDA_HOME", "CUDACXX", "CUDA_PATH"} and router == "CUDA":
            continue
        if name in _DISALLOWED_OVERRIDE_NAMES:
            if allow_resolved_paths and name in {"LD_LIBRARY_PATH", "PKG_CONFIG_PATH"}:
                continue
            raise CacheError(f"shared cache v1 rejects compiler/configuration override: {name}")
        if name.startswith("CMAKE_"):
            if name in _ALLOWED_CMAKE_NAMES and allow_resolved_paths and name in {
                "CMAKE_PREFIX_PATH",
                "CMAKE_INSTALL_LIBDIR",
            }:
                continue
            raise CacheError(f"shared cache v1 rejects unsupported CMake override: {name}")
        if name in {"MAKEFLAGS", "MFLAGS", "NINJAFLAGS", "NINJA_STATUS", "LD_PRELOAD", "SOURCE_DATE_EPOCH"} or name.startswith(("GIT_", "CCACHE_", "DISTCC_")):
            raise CacheError(f"shared cache rejects unsupported build environment: {name}")
        # Unknown variables are scrubbed from child builds.  Refuse them only
        # when they are an explicit toolchain control; ordinary process
        # variables remain harmless plumbing.


def _request_environment(values: Mapping[str, str]) -> dict[str, str]:
    return {
        name: values[name]
        for name in sorted(values)
        if name in {"FES_TOOLCHAIN_GPU_ROUTER", "FES_TOOLCHAIN_HIP_ARCHITECTURES"}
        or name == "CMAKE_HIP_ARCHITECTURES"
        or name in {"ROCM_PATH", "HIPCC", "CUDA_HOME", "CUDACXX", "CUDA_PATH"}
    }


def _probe_command(path: str, arguments: list[str], *, label: str) -> str:
    try:
        result = subprocess.run(
            [path, *arguments],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=30,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise CacheError(f"cannot run {label} probe: {exc}") from exc
    if result.returncode != 0:
        raise CacheError(f"{label} probe failed ({result.returncode}): {result.stdout.strip()}")
    return result.stdout


def _dependency_inventory(commands: Mapping[str, Mapping[str, Any]]) -> dict[str, Any]:
    """Capture the host dependencies used by bootstrap's real probes.

    Boost and Eigen deliberately use header/CMake probes.  They are not
    required to have pkg-config metadata on powerboat.
    """

    pkg_config = str(commands["pkg-config"]["path"])
    packages: dict[str, dict[str, str]] = {}
    for package in ("libffi", "readline", "tcl", "zlib", "liblzma", "libusb-1.0", "libftdi1"):
        packages[package] = {
            "modversion": _probe_command(pkg_config, ["--modversion", package], label=f"pkg-config {package}"),
            "cflags": _probe_command(pkg_config, ["--cflags", package], label=f"pkg-config {package} cflags"),
            "libs": _probe_command(pkg_config, ["--libs", package], label=f"pkg-config {package} libs"),
        }

    cc = str(commands["cc"]["path"])
    cxx = str(commands["c++"]["path"])
    python_config = str(commands["python3-config"]["path"])
    python_includes = _probe_command(python_config, ["--includes"], label="python3-config")
    headers: dict[str, dict[str, Any]] = {}
    for compiler, language, header, package, extra in (
        (cc, "c", "Python.h", None, python_includes.split()),
        (cc, "c", "ffi.h", "libffi", []),
        (cc, "c", "readline/readline.h", "readline", []),
        (cc, "c", "tcl.h", "tcl", []),
        (cc, "c", "zlib.h", "zlib", []),
        (cc, "c", "lzma.h", "liblzma", []),
        (cc, "c", "libusb-1.0/libusb.h", "libusb-1.0", []),
        (cc, "c", "libftdi1/ftdi.h", "libftdi1", []),
        (cxx, "c++", "boost/version.hpp", None, []),
    ):
        if package is not None:
            extra = packages[package]["cflags"].split()
        source = f"#include <{header}>\nint main(void) {{ return 0; }}\n"
        try:
            result = subprocess.run(
                [compiler, *extra, "-x", language, "-fsyntax-only", "-"],
                input=source,
                check=False,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                encoding="utf-8",
                errors="replace",
                timeout=30,
            )
        except (OSError, subprocess.SubprocessError) as exc:
            raise CacheError(f"cannot run header probe for {header}: {exc}") from exc
        if result.returncode != 0:
            raise CacheError(f"required header probe failed for {header}: {result.stdout.strip()}")
        headers[header] = {"compiler": compiler, "output": result.stdout}

    cmake = str(commands["cmake"]["path"])
    cmake_probes: dict[str, str] = {}
    projects = {
        "boost-components": (
            "find_package(Boost REQUIRED COMPONENTS program_options iostreams thread)\n"
            "add_executable(probe main.cpp)\n"
            "target_link_libraries(probe PRIVATE Boost::program_options Boost::iostreams Boost::thread)\n",
            "#include <boost/program_options.hpp>\n#include <boost/iostreams/device/array.hpp>\n#include <boost/thread.hpp>\nint main() { return 0; }\n",
        ),
        "eigen3": (
            "find_package(Eigen3 REQUIRED NO_MODULE)\n"
            "add_executable(probe main.cpp)\n"
            "target_link_libraries(probe PRIVATE Eigen3::Eigen)\n",
            "#include <Eigen/Core>\nint main() { Eigen::Vector3f value; return value.size(); }\n",
        ),
    }
    for name, (find_text, source_text) in projects.items():
        with tempfile.TemporaryDirectory(prefix="misteross-cache-probe-") as directory:
            probe_root = Path(directory)
            (probe_root / "CMakeLists.txt").write_text(
                "cmake_minimum_required(VERSION 3.16)\nproject(probe LANGUAGES CXX)\n" + find_text,
                encoding="utf-8",
            )
            (probe_root / "main.cpp").write_text(source_text, encoding="utf-8")
            try:
                result = subprocess.run(
                    [cmake, "-S", str(probe_root), "-B", str(probe_root / "build"), "-G", "Ninja"],
                    check=False,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                    encoding="utf-8",
                    errors="replace",
                    timeout=60,
                )
            except (OSError, subprocess.SubprocessError) as exc:
                raise CacheError(f"cannot run {name} CMake probe: {exc}") from exc
            if result.returncode != 0:
                raise CacheError(f"required {name} CMake probe failed: {result.stdout.strip()}")
            cmake_probes[name] = result.stdout
    return {
        "pkg-config": packages,
        "headers": headers,
        "cmake": cmake_probes,
        "python-config": python_includes,
    }


def request_from_environment(
    root: Path,
    lock_path: Path | None = None,
    *,
    cache_root: Path | None = None,
) -> ToolchainRequest:
    """Build a request from the opt-in environment after host probing."""

    root = Path(root).resolve()
    selected_lock = lock_path or Path(os.environ.get("FES_TOOLCHAIN_LOCKFILE", root / "toolchain.lock"))
    selected_lock = Path(selected_lock)
    if not selected_lock.is_absolute():
        selected_lock = root / selected_lock
    selected_cache = cache_root or os.environ.get("FES_TOOLCHAIN_CACHE_ROOT")
    if not selected_cache:
        raise CacheError("FES_TOOLCHAIN_CACHE_ROOT is not set; shared lane is not selected")
    router = os.environ.get("FES_TOOLCHAIN_GPU_ROUTER", DEFAULT_GPU_ROUTER).upper()
    architectures = os.environ.get("FES_TOOLCHAIN_HIP_ARCHITECTURES", DEFAULT_HIP_ARCHITECTURES)
    allow_resolved_paths = os.environ.get("FES_TOOLCHAIN_CACHE_RESOLVED") == "1"
    validate_shared_environment(
        os.environ,
        gpu_router=router,
        hip_architectures=architectures,
        allow_resolved_paths=allow_resolved_paths,
    )
    environment = _request_environment(os.environ)
    host = host_identity()
    compiler = compiler_inventory(router)
    return ToolchainRequest(
        root=root,
        lock_path=selected_lock,
        cache_root=Path(selected_cache),
        gpu_router=router,
        hip_architectures=architectures,
        host_identity=host,
        compiler_identity=compiler,
        environment=environment,
    )


def _load_request_arguments(argv: list[str]) -> ToolchainRequest:
    import argparse

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--lock", dest="lock_path", type=Path)
    parser.add_argument("--cache", dest="cache_root", type=Path)
    parser.add_argument("--gpu-router", default=None)
    parser.add_argument("--hip-architectures", default=None)
    parser.add_argument("command", choices=("init", "plan", "verify", "stage-evidence", "publish"))
    parser.add_argument("--field", choices=("key", "slot", "source", "build", "install", "evidence", "lock"))
    parser.add_argument("--build-root", type=Path)
    parser.add_argument("--format", choices=("json", "lines"), default="json")
    args = parser.parse_args(argv)
    environment = os.environ.copy()
    if args.gpu_router is not None:
        environment["FES_TOOLCHAIN_GPU_ROUTER"] = args.gpu_router
    if args.hip_architectures is not None:
        environment["FES_TOOLCHAIN_HIP_ARCHITECTURES"] = args.hip_architectures
    with _temporary_environment(environment):
        request = request_from_environment(
            args.root,
            args.lock_path,
            cache_root=args.cache_root,
        )
    return request, args


@contextlib.contextmanager
def _temporary_environment(values: Mapping[str, str]) -> Iterator[None]:
    previous = os.environ.copy()
    os.environ.clear()
    os.environ.update(values)
    try:
        yield
    finally:
        os.environ.clear()
        os.environ.update(previous)


def _main(argv: list[str]) -> int:
    try:
        request, args = _load_request_arguments(argv)
        if args.command == "init":
            ensure_cache_root(request.cache_root)
            ensure_lock_file(request)
        elif args.command == "plan":
            values = {
                "key": cache_key(request),
                "slot": slot_path(request),
                "source": slot_path(request) / "src",
                "build": slot_path(request) / "build",
                "install": slot_path(request) / "install",
                "evidence": slot_path(request) / "evidence",
                "lock": lock_path(request),
            }
            if args.field:
                print(values[args.field])
            elif args.format == "lines":
                for name, value in values.items():
                    print(f"{name}\t{value}")
            else:
                print(json.dumps({name: str(value) for name, value in values.items()}, sort_keys=True))
        elif args.command == "verify":
            manifest = verify_ready(request)
            print(json.dumps({"key": manifest.key, "install": str(manifest.install)}, sort_keys=True))
        elif args.command == "stage-evidence":
            if args.build_root is None:
                raise CacheError("stage-evidence requires --build-root")
            print(stage_evidence(request, args.build_root))
        elif args.command == "publish":
            # Bootstrap writes the identity files first; derive the tool records
            # from those files so the shell does not need to serialize JSON.
            tool_binaries = {
                "yosys": ("yosys", "yosys"),
                "mistral": ("mistral", "mistral-cv"),
                "nextpnr": ("nextpnr", "nextpnr-mistral"),
                "verilator": ("verilator", "verilator"),
                "openfpgaloader": ("openfpgaloader", "openFPGALoader"),
            }
            # lockfile.py is intentionally imported only for this publishing
            # path; the key itself remains the raw lock content digest.
            from scripts.lockfile import load_lock

            pins = load_lock(request.lock_path)
            install = slot_path(request) / "install"
            build = slot_path(request) / "build"
            tools: dict[str, dict[str, Any]] = {}
            for lock_name, (build_name, binary_name) in tool_binaries.items():
                pin = pins[lock_name]
                identity_path = build / build_name / f".identity-{pin.commit}.txt"
                digest_path = build / build_name / f".digest-{pin.commit}.sha256"
                if not identity_path.is_file() or not digest_path.is_file():
                    raise CacheError(f"missing bootstrap identity evidence for {lock_name}")
                tools[lock_name] = {
                    "binary": f"bin/{binary_name}",
                    "commit": pin.commit,
                    "identity": identity_path.read_text(encoding="utf-8").strip(),
                }
            # bootstrap owns the kernel flock for this process tree.  Taking
            # another blocking flock here would deadlock; the private flag is
            # only used by this child after the shell has acquired that lock.
            manifest = publish_ready(request, tools=tools, _lock_held=True)
            print(json.dumps({"key": manifest.key, "install": str(manifest.install)}, sort_keys=True))
        return 0
    except CacheError as exc:
        print(f"toolchain-cache: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":  # pragma: no cover - exercised by shell bootstrap
    raise SystemExit(_main(sys.argv[1:]))
