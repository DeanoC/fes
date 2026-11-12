#!/usr/bin/env python3
"""Identity, publication, verification and pruning of shared tool installs.

The cache holds one install per tool, commit and build configuration under
``<cache>/tools/<tool>/<key>``. A lock does not own a slot: it resolves to the
installs of the five tools it pins, so locks that pin the same Yosys share one
Yosys install. The key reads only each pin's repo and commit, the tool's
``build_<tool>`` function in bootstrap.sh, the host and the compiler
inventory; only nextpnr adds the GPU lane and the Mistral pin it compiles
against.

This module does not build tools; bootstrap owns that. It owns the stable
paths, the ready.json contract consumed by bootstrap and the authenticated
build scripts, and the supported prune.
"""

from __future__ import annotations

import contextlib
import dataclasses
import fcntl
import hashlib
import json
import os
import platform
import re
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any, Iterator, Mapping


SCHEMA_VERSION = 2
TOOLS = ("yosys", "mistral", "nextpnr", "verilator", "openfpgaloader")
# nextpnr-mistral compiles the Mistral database from MISTRAL_ROOT, so its
# install depends on that pin as well as its own.
TOOL_SOURCE_DEPENDENCIES = {"nextpnr": ("mistral",)}
# Only these tools take the GPU lane and the accelerator compiler.
GPU_TOOLS = frozenset({"nextpnr"})
ACCELERATOR_COMMANDS = frozenset({"hipcc", "nvcc"})
# The Mistral install carries the database source files producers pin
# (rom_map.DATABASE_FILES), since src/ is deleted after publication.
MISTRAL_DATABASE = "share/mistral/source"
DEFAULT_GPU_ROUTER = "OFF"
DEFAULT_HIP_ARCHITECTURES = "gfx1100;gfx1201"
# Native sm_86 code (RTX 3070/3090) runs without PTX JIT.  JIT fails when the
# installed driver is older than the CUDA toolkit that produced the PTX.
DEFAULT_CUDA_ARCHITECTURES = "86"
# One or more CMake CUDA_ARCHITECTURES entries such as ``86``, ``75;86`` or
# ``86-real``.  ``native`` and ``all`` are not reproducible identities.
_CUDA_ARCHITECTURE = r"[0-9]+[a-z]?(?:-real|-virtual)?"
_CUDA_ARCHITECTURES_RE = re.compile(rf"{_CUDA_ARCHITECTURE}(?:;{_CUDA_ARCHITECTURE})*")
TOOL_BINARY_NAMES = {
    "yosys": "yosys",
    "mistral": "mistral-cv",
    "nextpnr": "nextpnr-mistral",
    "verilator": "verilator",
    "openfpgaloader": "openFPGALoader",
}
_HELD_LOCKS: set[tuple[str, str]] = set()
# Shared reader locks held until process exit, by lock file path.
_READER_LOCKS: dict[str, int] = {}
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
    """Raised when a shared cache request or ready install is unsafe."""


@dataclasses.dataclass(frozen=True)
class ToolchainRequest:
    """All inputs that select the installs one lock resolves to."""

    root: Path
    lock_path: Path
    cache_root: Path
    gpu_router: str = DEFAULT_GPU_ROUTER
    hip_architectures: str = DEFAULT_HIP_ARCHITECTURES
    cuda_architectures: str = DEFAULT_CUDA_ARCHITECTURES
    host_identity: Mapping[str, Any] = dataclasses.field(default_factory=dict)
    compiler_identity: Mapping[str, Any] = dataclasses.field(default_factory=dict)
    environment: Mapping[str, Any] = dataclasses.field(default_factory=dict)
    # The recipe whose build_<tool> functions key each install; defaults to
    # scripts/bootstrap.sh below ``root``.
    bootstrap_path: Path | None = None


@dataclasses.dataclass(frozen=True)
class InstallManifest:
    """Decoded ready.json of one tool install."""

    schema: int
    key: str
    tool: str
    directory: Path
    install: Path
    evidence: Path
    build: Mapping[str, Any]
    record: Mapping[str, Any]
    files: Mapping[str, Mapping[str, Any]]

    @property
    def binary(self) -> Path:
        """The absolute path of the authenticated executable."""

        return self.directory / self.record["path"]

    @property
    def configuration(self) -> Mapping[str, Any] | None:
        """The GPU lane of a GPU tool (nextpnr); ``None`` for the others."""

        return self.build.get("configuration")


@dataclasses.dataclass(frozen=True)
class ToolchainResolution:
    """The verified install of every tool a lock pins."""

    tools: Mapping[str, InstallManifest]


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


def _check_tool(tool: str) -> str:
    if tool not in TOOLS:
        raise CacheError(f"unknown toolchain tool: {tool!r}")
    return tool


def _lockfile_module(root: Path):
    """Load the repository's validating lock parser."""

    import importlib.util

    parser_path = Path(root) / "scripts" / "lockfile.py"
    spec = importlib.util.spec_from_file_location("misteross_cache_lockfile", parser_path)
    if spec is None or spec.loader is None:
        raise CacheError(f"cannot load lockfile parser below {root}")
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    try:
        spec.loader.exec_module(module)
    except Exception as exc:
        raise CacheError(f"cannot load lockfile parser below {root}: {exc}") from exc
    return module


def _pins_from_lock(module, path: Path) -> dict[str, dict[str, str]]:
    try:
        pins = module.load_lock(path)
    except Exception as exc:
        raise CacheError(f"cannot validate toolchain lock {path}: {exc}") from exc
    # Only the pins key an install. The rationale and order fields are prose
    # and sequencing; editing them must not rebuild anything.
    return {tool: {"repo": pins[tool].repo, "commit": pins[tool].commit} for tool in TOOLS}


def locked_pins(request: ToolchainRequest) -> dict[str, dict[str, str]]:
    """Return each tool's repo and commit from the selected lock."""

    return _pins_from_lock(_lockfile_module(request.root), request.lock_path)


def _normalise_configuration(request: ToolchainRequest) -> dict[str, Any]:
    router = str(request.gpu_router or DEFAULT_GPU_ROUTER).upper()
    if router not in {"OFF", "HIP", "CUDA"}:
        raise CacheError(f"unsupported GPU router for shared cache: {router}")
    architectures = str(request.hip_architectures or DEFAULT_HIP_ARCHITECTURES)
    configuration = {
        "gpu-router": router,
        "hip-architectures": architectures if router == "HIP" else "unused",
    }
    # Only the CUDA lane carries this field, so a CUDA architecture list does
    # not fork the HIP or OFF nextpnr installs.
    if router == "CUDA":
        configuration["cuda-architectures"] = validated_cuda_architectures(
            request.cuda_architectures
        )
    return configuration


def validated_cuda_architectures(value: str | None) -> str:
    """Return an explicit CMake CUDA architecture list or raise ``CacheError``."""

    architectures = str(value or DEFAULT_CUDA_ARCHITECTURES)
    if _CUDA_ARCHITECTURES_RE.fullmatch(architectures) is None:
        raise CacheError(
            "CUDA architectures must be explicit CMake CUDA_ARCHITECTURES entries "
            f"such as 86 or 75;86, not {architectures!r}"
        )
    return architectures


def configuration_text(configuration: Mapping[str, Any]) -> str:
    """Return the nextpnr ``.config-*.txt`` attestation line for a lane."""

    text = (
        f"gpu-router={configuration['gpu-router']}; "
        f"hip-architectures={configuration['hip-architectures']}"
    )
    if "cuda-architectures" in configuration:
        text += f"; cuda-architectures={configuration['cuda-architectures']}"
    return text


def tool_recipe(text: str, tool: str) -> str:
    """Return bootstrap.sh's ``build_<tool>`` function, the tool's build recipe."""

    _check_tool(tool)
    match = re.search(rf"^build_{tool}\(\) \{{\n.*?^\}}\n", text, re.MULTILINE | re.DOTALL)
    if match is None:
        raise CacheError(f"bootstrap recipe has no build_{tool} function")
    return match.group(0)


def _recipe_digest(request: ToolchainRequest, tool: str) -> str:
    path = Path(request.bootstrap_path) if request.bootstrap_path else request.root / "scripts" / "bootstrap.sh"
    _regular_file(path, "bootstrap recipe")
    try:
        text = path.read_text(encoding="utf-8")
    except (OSError, UnicodeError) as exc:
        raise CacheError(f"cannot read bootstrap recipe {path}: {exc}") from exc
    return _sha256_bytes(tool_recipe(text, tool).encode("utf-8"))


def _base_compiler(compiler: Mapping[str, Any]) -> dict[str, Any]:
    """The compiler inventory without the accelerator compiler."""

    result = dict(compiler)
    commands = result.get("commands")
    if isinstance(commands, Mapping):
        result["commands"] = {
            name: value for name, value in commands.items() if name not in ACCELERATOR_COMMANDS
        }
    return result


def _accelerator_compiler(compiler: Mapping[str, Any]) -> dict[str, Any]:
    commands = compiler.get("commands")
    if not isinstance(commands, Mapping):
        return {}
    return {name: value for name, value in commands.items() if name in ACCELERATOR_COMMANDS}


def _build_identity(
    request: ToolchainRequest, tool: str, pins: Mapping[str, Mapping[str, str]]
) -> dict[str, Any]:
    configuration = _normalise_configuration(request)
    identity: dict[str, Any] = {
        "pin": dict(pins[tool]),
        "recipe": _recipe_digest(request, tool),
        "host": dict(request.host_identity),
        "compiler": _base_compiler(request.compiler_identity),
    }
    dependencies = TOOL_SOURCE_DEPENDENCIES.get(tool, ())
    if dependencies:
        identity["sources"] = {name: dict(pins[name]) for name in dependencies}
    if tool in GPU_TOOLS:
        identity["configuration"] = configuration
        identity["accelerator"] = _accelerator_compiler(request.compiler_identity)
        identity["environment"] = dict(request.environment)
    return identity


def build_identity(request: ToolchainRequest, tool: str) -> dict[str, Any]:
    """Return everything that selects *tool*'s install for *request*.

    That is the tool's pin, its ``build_<tool>`` recipe, the host and the
    compiler inventory. Only nextpnr adds the GPU lane, the accelerator
    compiler and backend environment, and the Mistral pin it compiles
    against. The lock's other text never enters it.
    """

    _check_tool(tool)
    return _build_identity(request, tool, locked_pins(request))


def _key(tool: str, identity: Mapping[str, Any]) -> str:
    return _sha256_bytes(_canonical({"schema": SCHEMA_VERSION, "tool": tool, "build": dict(identity)}))


def tool_key(request: ToolchainRequest, tool: str) -> str:
    """Return the path-independent key of *tool*'s install."""

    return _key(tool, build_identity(request, tool))


def tool_keys(request: ToolchainRequest) -> dict[str, str]:
    """Return every tool's install key for *request*."""

    pins = locked_pins(request)
    return {tool: _key(tool, _build_identity(request, tool, pins)) for tool in TOOLS}


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


def _ensure_private_directory(path: Path) -> None:
    _check_no_symlink_components(path)
    try:
        path.mkdir(mode=0o700, exist_ok=True)
        info = path.lstat()
    except OSError as exc:
        raise CacheError(f"cannot create cache directory {path}: {exc}") from exc
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid():
        raise CacheError(f"cache directory has unsafe ownership/type: {path}")
    if stat.S_IMODE(info.st_mode) & 0o022:
        # These directories are cache-owned control paths. Tightening an
        # owner-created mode does not relabel or remove any installation.
        try:
            path.chmod(0o700)
            info = path.lstat()
        except OSError as exc:
            raise CacheError(f"cache directory is writable by group/other: {path}") from exc
        if stat.S_IMODE(info.st_mode) & 0o022:
            raise CacheError(f"cache directory is writable by group/other: {path}")


def ensure_cache_root(path: Path) -> Path:
    """Validate/create a private same-user cache root."""

    path = _absolute(Path(path), "cache root")
    _check_no_symlink_components(path)
    try:
        # Concurrent bootstraps may race to create the root; lstat validates it.
        path.mkdir(mode=0o700, parents=True, exist_ok=True)
        info = path.lstat()
    except OSError as exc:
        raise CacheError(f"cannot create or inspect cache root {path}: {exc}") from exc
    if not stat.S_ISDIR(info.st_mode):
        raise CacheError(f"cache root is not a directory: {path}")
    if info.st_uid != os.getuid():
        raise CacheError(f"cache root is owned by uid {info.st_uid}, not uid {os.getuid()}: {path}")
    if stat.S_IMODE(info.st_mode) & 0o022:
        raise CacheError(f"cache root is writable by group/other: {path}")
    for child in (path / "tools", path / "locks"):
        _ensure_private_directory(child)
    for tool in TOOLS:
        _ensure_private_directory(path / "tools" / tool)
    return path


def install_directory(request: ToolchainRequest, tool: str) -> Path:
    """Return *tool*'s install directory: ``<cache>/tools/<tool>/<key>``."""

    return _absolute(Path(request.cache_root), "cache root") / "tools" / tool / tool_key(request, tool)


def ready_path(request: ToolchainRequest, tool: str) -> Path:
    return install_directory(request, tool) / "ready.json"


def lock_path(request: ToolchainRequest, tool: str) -> Path:
    return _absolute(Path(request.cache_root), "cache root") / "locks" / f"{tool_key(request, tool)}.lock"


def _open_lock(path: Path) -> int:
    try:
        descriptor = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    except AttributeError as exc:  # pragma: no cover - old non-POSIX Python
        raise CacheError("shared cache requires O_NOFOLLOW support") from exc
    except OSError as exc:
        raise CacheError(f"cannot open cache lock {path}: {exc}") from exc
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid():
            raise CacheError(f"cache lock has unsafe type/ownership: {path}")
        os.fchmod(descriptor, 0o600)
    except BaseException:
        os.close(descriptor)
        raise
    return descriptor


def ensure_lock_file(request: ToolchainRequest, tool: str) -> Path:
    """Create/check the per-install lock inode without following symlinks."""

    root = ensure_cache_root(request.cache_root)
    path = root / "locks" / f"{tool_key(request, tool)}.lock"
    os.close(_open_lock(path))
    return path


@contextlib.contextmanager
def acquire_build_lock(request: ToolchainRequest, tool: str) -> Iterator[None]:
    """Hold the exclusive per-install lock for a whole build/publication."""

    root = ensure_cache_root(request.cache_root)
    key = tool_key(request, tool)
    descriptor = _open_lock(root / "locks" / f"{key}.lock")
    try:
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


def _hold_install(request: ToolchainRequest, tool: str) -> None:
    """Take a shared lock on *tool*'s install for the rest of this process.

    A prune takes the exclusive lock without waiting and skips an install
    it cannot lock, so an install stays on disk while any process that
    resolved it is running. The lock file's mtime is the install's last use.
    """

    ready = ready_path(request, tool)
    if not ready.is_file():
        # Fail here rather than wait behind a build that holds the lock.
        raise CacheError(f"{tool} install is not ready: {ready}")
    path = ensure_lock_file(request, tool)
    held = _READER_LOCKS.get(str(path))
    if held is None:
        descriptor = _open_lock(path)
        try:
            fcntl.flock(descriptor, fcntl.LOCK_SH)
        except OSError as exc:
            os.close(descriptor)
            raise CacheError(f"cannot take the shared lock on {path}: {exc}") from exc
        _READER_LOCKS[str(path)] = descriptor
    try:
        os.utime(path)
    except OSError as exc:
        raise CacheError(f"cannot record the last use of {path}: {exc}") from exc


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


_MANIFEST_FIELDS = frozenset(
    {"schema", "key", "tool", "directory", "install", "evidence", "build", "record", "files"}
)
_RECORD_FIELDS = frozenset({"binary", "path", "commit", "identity", "sha256"})


def _manifest_dict(manifest: InstallManifest) -> dict[str, Any]:
    return {
        "schema": manifest.schema,
        "key": manifest.key,
        "tool": manifest.tool,
        "directory": str(manifest.directory),
        "install": str(manifest.install),
        "evidence": str(manifest.evidence),
        "build": dict(manifest.build),
        "record": dict(manifest.record),
        "files": {name: dict(value) for name, value in manifest.files.items()},
    }


def _manifest_from_dict(data: Mapping[str, Any]) -> InstallManifest:
    if set(data) != _MANIFEST_FIELDS:
        raise CacheError("ready manifest fields are not exact")
    try:
        schema = int(data["schema"])
    except (TypeError, ValueError) as exc:
        raise CacheError("ready manifest schema is invalid") from exc
    if schema != SCHEMA_VERSION:
        raise CacheError(f"unsupported ready manifest schema: {schema}")
    if not isinstance(data["key"], str) or len(data["key"]) != 64:
        raise CacheError("ready manifest key is invalid")
    if data["tool"] not in TOOLS:
        raise CacheError("ready manifest tool is invalid")
    for field in ("directory", "install", "evidence"):
        if not isinstance(data[field], str) or not Path(data[field]).is_absolute():
            raise CacheError(f"ready manifest {field} path is invalid")
    for field in ("build", "record", "files"):
        if not isinstance(data[field], dict):
            raise CacheError(f"ready manifest {field} must be an object")
    return InstallManifest(
        schema=schema,
        key=data["key"],
        tool=data["tool"],
        directory=Path(data["directory"]),
        install=Path(data["install"]),
        evidence=Path(data["evidence"]),
        build=data["build"],
        record=data["record"],
        files=data["files"],
    )


def read_manifest(path: Path) -> InstallManifest:
    """Decode one ready.json without checking it against a request."""

    path = Path(path)
    try:
        info = path.lstat()
    except OSError as exc:
        raise CacheError(f"ready manifest is missing: {path}") from exc
    if not stat.S_ISREG(info.st_mode):
        raise CacheError(f"ready manifest is not a regular file: {path}")
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise CacheError(f"cannot read ready manifest {path}: {exc}") from exc
    if not isinstance(data, dict):
        raise CacheError("ready manifest must be a JSON object")
    return _manifest_from_dict(data)


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


def _check_install_paths(directory: Path) -> tuple[Path, Path]:
    _check_no_symlink_components(directory, allow_missing=False)
    install, evidence = directory / "install", directory / "evidence"
    for path, label in ((install, "install"), (evidence, "evidence")):
        _check_no_symlink_components(path, allow_missing=False)
        try:
            if not path.is_dir():
                raise CacheError(f"{label} path is not a directory: {path}")
            info = path.lstat()
        except OSError as exc:
            raise CacheError(f"cannot inspect {label} path {path}: {exc}") from exc
        if info.st_uid != os.getuid():
            raise CacheError(f"{label} path is not owned by the cache user: {path}")
    return install, evidence


def publish_install(
    request: ToolchainRequest,
    tool: str,
    *,
    identity: str,
    external_lock_fd: int | None = None,
) -> InstallManifest:
    """Verify a completed install, freeze it, and atomically publish ready.json."""

    _check_tool(tool)
    root = ensure_cache_root(request.cache_root)
    pins = locked_pins(request)
    build = _build_identity(request, tool, pins)
    key = _key(tool, build)
    if external_lock_fd is None:
        if (str(root), key) not in _HELD_LOCKS:
            raise CacheError("ready publication must run under the per-install build lock")
    else:
        assert_external_lock(root / "locks" / f"{key}.lock", external_lock_fd)
    directory = root / "tools" / tool / key
    install, evidence = _check_install_paths(directory)
    ready = directory / "ready.json"
    if ready.exists() or ready.is_symlink():
        raise CacheError(f"ready manifest already exists; refusing to relabel install: {ready}")
    if not isinstance(identity, str) or not identity.strip():
        raise CacheError(f"tool record has no identity output: {tool}")
    if not any(evidence.iterdir()):
        raise CacheError("cannot publish a tool without consumer evidence")
    binary_name = f"bin/{TOOL_BINARY_NAMES[tool]}"
    binary = _safe_child(install, binary_name, f"{tool} binary")
    try:
        info = binary.lstat()
    except OSError as exc:
        raise CacheError(f"missing tool binary {binary}: {exc}") from exc
    if stat.S_ISLNK(info.st_mode):
        _relative_link_target(binary, install)
    elif not stat.S_ISREG(info.st_mode):
        raise CacheError(f"tool binary is not a regular file or internal link: {binary}")
    if tool == "mistral" and not (install / MISTRAL_DATABASE).is_dir():
        raise CacheError(f"Mistral install has no database source: {install / MISTRAL_DATABASE}")
    record = {
        "binary": binary_name,
        "path": f"install/{binary_name}",
        "commit": pins[tool]["commit"],
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
    manifest = InstallManifest(
        schema=SCHEMA_VERSION,
        key=key,
        tool=tool,
        directory=directory,
        install=install,
        evidence=evidence,
        build=build,
        record=record,
        files=files,
    )
    data = _canonical(_manifest_dict(manifest)) + b"\n"
    temporary = directory / ".ready.json.tmp"
    try:
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, ready)
        ready.chmod(0o444)
        directory_fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
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


def assert_external_lock(path: Path, fd: int) -> None:
    """Prove an inherited descriptor owns the expected kernel flock.

    Bootstrap keeps the descriptor open while its child Python process seals
    the install.  ``fdinfo`` identifies a lock attached to this exact inherited
    open-file description; a separately opened descriptor must then be unable
    to acquire the same inode.  A bare boolean or an inherited descriptor that
    was merely opened is insufficient and fails closed.
    """

    if not isinstance(fd, int) or fd < 0:
        raise CacheError("external lock descriptor is invalid")
    path = Path(path)
    try:
        expected = path.stat(follow_symlinks=False)
        actual = os.fstat(fd)
    except OSError as exc:
        raise CacheError(f"cannot inspect external lock descriptor: {exc}") from exc
    if not stat.S_ISREG(expected.st_mode) or not stat.S_ISREG(actual.st_mode):
        raise CacheError("external lock path and descriptor must be regular files")
    if (expected.st_dev, expected.st_ino) != (actual.st_dev, actual.st_ino):
        raise CacheError("external lock descriptor does not refer to the request lock file")

    device = f"{os.major(expected.st_dev):02x}:{os.minor(expected.st_dev):02x}"
    fdinfo = Path(f"/proc/self/fdinfo/{fd}")
    try:
        fdinfo_text = fdinfo.read_text(encoding="utf-8")
    except OSError as exc:
        raise CacheError(f"cannot inspect inherited lock descriptor {fd}: {exc}") from exc
    lock_token = f"{device}:{expected.st_ino}"
    if not any(
        line.startswith("lock:")
        and "FLOCK" in line
        and "WRITE" in line
        and lock_token in line
        for line in fdinfo_text.splitlines()
    ):
        raise CacheError("external lock descriptor is not holding a flock on the request lock file")

    try:
        probe_fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    except OSError as exc:
        raise CacheError(f"cannot open a fresh lock probe descriptor: {exc}") from exc
    try:
        try:
            fcntl.flock(probe_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return
        else:
            fcntl.flock(probe_fd, fcntl.LOCK_UN)
            raise CacheError("external lock descriptor is not serialized by a held flock")
    finally:
        os.close(probe_fd)


def _verify_files(manifest: InstallManifest) -> None:
    actual: dict[str, dict[str, Any]] = {}
    actual.update(_scan_tree(manifest.install, "install"))
    actual.update(_scan_tree(manifest.evidence, "evidence"))
    if actual != dict(manifest.files):
        raise CacheError(f"published {manifest.tool} install/evidence closure differs from ready manifest")
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


def _verify_install(
    request: ToolchainRequest, tool: str, pins: Mapping[str, Mapping[str, str]]
) -> InstallManifest:
    build = _build_identity(request, tool, pins)
    key = _key(tool, build)
    expected_directory = _absolute(Path(request.cache_root), "cache root") / "tools" / tool / key
    _check_no_symlink_components(expected_directory, allow_missing=False)
    manifest = read_manifest(expected_directory / "ready.json")
    if manifest.key != key or manifest.tool != tool:
        raise CacheError(f"{tool} ready manifest key does not match the current request")
    if manifest.directory != expected_directory:
        raise CacheError(f"{tool} ready manifest is non-relocatable and points at another install")
    if manifest.install != expected_directory / "install" or manifest.evidence != expected_directory / "evidence":
        raise CacheError(f"{tool} ready manifest paths are not the stable install paths")
    if dict(manifest.build) != build:
        raise CacheError(f"{tool} ready manifest build identity does not match the request")
    record = manifest.record
    if set(record) != _RECORD_FIELDS:
        raise CacheError(f"ready manifest tool record fields are invalid: {tool}")
    expected_binary = f"bin/{TOOL_BINARY_NAMES[tool]}"
    if record.get("binary") != expected_binary or record.get("path") != f"install/{expected_binary}":
        raise CacheError(f"ready manifest tool path is invalid: {tool}")
    if record.get("commit") != pins[tool]["commit"]:
        raise CacheError(f"ready manifest tool commit does not match the selected lock: {tool}")
    if not isinstance(record.get("identity"), str) or not record["identity"].strip():
        raise CacheError(f"ready manifest tool identity is missing: {tool}")
    if not isinstance(record.get("sha256"), str) or len(record["sha256"]) != 64:
        raise CacheError(f"ready manifest tool digest is invalid: {tool}")
    _check_install_paths(expected_directory)
    _verify_files(manifest)
    binary = _safe_child(manifest.install, expected_binary, f"{tool} binary")
    if record.get("sha256") != _sha256_file(binary):
        raise CacheError(f"ready manifest tool digest changed: {tool}")
    return manifest


def verify_install(request: ToolchainRequest, tool: str) -> InstallManifest:
    """Verify one tool's ready install without modifying any cache file."""

    _check_tool(tool)
    return _verify_install(request, tool, locked_pins(request))


def resolve_install(request: ToolchainRequest, tool: str, *, hold: bool = True) -> InstallManifest:
    """Verify one tool's install, holding it like ``resolve_ready`` does."""

    _check_tool(tool)
    if hold:
        _hold_install(request, tool)
    return verify_install(request, tool)


def resolve_ready(request: ToolchainRequest, *, hold: bool = True) -> ToolchainResolution:
    """Verify the install of every tool the request's lock pins.

    With *hold* (the default for consumers) each install's lock is held
    shared until this process exits, so a prune cannot remove it while the
    caller uses it, and the lock's mtime records the use.
    """

    pins = locked_pins(request)
    installs: dict[str, InstallManifest] = {}
    for tool in TOOLS:
        if hold:
            _hold_install(request, tool)
        installs[tool] = _verify_install(request, tool, pins)
    return ToolchainResolution(tools=installs)


def verify_ready(request: ToolchainRequest) -> ToolchainResolution:
    """Re-verify every install without taking or refreshing any lock."""

    return resolve_ready(request, hold=False)


def stage_evidence(request: ToolchainRequest, tool: str, build_root: Path) -> Path:
    """Copy only small authentication/configuration records into the install."""

    _check_tool(tool)
    evidence = install_directory(request, tool) / "evidence"
    _check_no_symlink_components(evidence)
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    build_root = Path(build_root)
    if not build_root.is_absolute():
        raise CacheError(f"build evidence root must be absolute: {build_root}")
    tool_dir = build_root / tool
    if tool_dir.is_symlink() or not tool_dir.is_dir():
        raise CacheError(f"no build directory for {tool}: {tool_dir}")
    for entry in sorted(tool_dir.iterdir(), key=lambda item: item.name):
        if entry.name.startswith((".built-", ".digest-", ".identity-", ".config-")) or entry.name in {
            "CMakeCache.txt",
            "build.ninja",
        }:
            if entry.is_symlink() or not entry.is_file():
                raise CacheError(f"unsupported evidence entry: {entry}")
            shutil.copy2(entry, evidence / entry.name)
    if not any(evidence.iterdir()):
        raise CacheError(f"no {tool} authentication evidence found below {tool_dir}")
    return evidence

def _command_identity(
    command: str,
    args: tuple[str, ...] = ("--version",),
    *,
    environment: Mapping[str, str] | None = None,
) -> dict[str, Any]:
    probe_environment = None if environment is None else dict(environment)
    executable = shutil.which(command, path=None if probe_environment is None else probe_environment.get("PATH"))
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
            env=probe_environment,
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
        "machine": platform.machine(),
        "libc": f"{libc_name}-{libc_version}",
        "python": platform.python_version(),
    }


def _controlled_probe_environment(values: Mapping[str, str], router: str) -> dict[str, str]:
    """Return the same environment policy used by a shared build child."""

    controlled = {
        name: values[name]
        for name in sorted(values)
        if name in _RUNTIME_ENVIRONMENT and values.get(name)
    }
    # Tool version/help and CMake diagnostics must not vary with the caller's
    # locale or timezone.  These values are also what bootstrap exports for
    # the actual compiler children.
    controlled.update({"LANG": "C", "LC_ALL": "C", "TZ": "UTC"})
    if router == "HIP":
        for name in ("ROCM_PATH", "HIPCC"):
            if values.get(name):
                controlled[name] = values[name]
    elif router == "CUDA":
        for name in ("CUDA_HOME", "CUDACXX", "CUDA_PATH"):
            if values.get(name):
                controlled[name] = values[name]
    return controlled


def compiler_inventory(
    gpu_router: str,
    *,
    environment: Mapping[str, str] | None = None,
) -> dict[str, Any]:
    """Probe required build commands and the selected accelerator compiler."""

    router = str(gpu_router or DEFAULT_GPU_ROUTER).upper()
    probe_environment = _controlled_probe_environment(
        dict(os.environ if environment is None else environment), router
    )
    commands = {
        name: _command_identity(name, environment=probe_environment)
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
    if router == "HIP":
        commands["hipcc"] = _command_identity("hipcc", environment=probe_environment)
    elif router == "CUDA":
        commands["nvcc"] = _command_identity("nvcc", ("--version",), environment=probe_environment)
    return {
        "commands": commands,
        "dependencies": _dependency_inventory(commands, environment=probe_environment),
    }


def validate_shared_environment(
    values: Mapping[str, str] | None = None,
    *,
    gpu_router: str | None = None,
    hip_architectures: str | None = None,
    cuda_architectures: str | None = None,
    allow_resolved_paths: bool = False,
) -> None:
    """Reject build-affecting overrides outside the v1 shared contract."""

    values = dict(os.environ if values is None else values)
    router = str(gpu_router or values.get("FES_TOOLCHAIN_GPU_ROUTER", DEFAULT_GPU_ROUTER)).upper()
    architectures = str(
        hip_architectures
        or values.get("FES_TOOLCHAIN_HIP_ARCHITECTURES", DEFAULT_HIP_ARCHITECTURES)
    )
    cuda_selected = str(
        cuda_architectures
        or values.get("FES_TOOLCHAIN_CUDA_ARCHITECTURES", DEFAULT_CUDA_ARCHITECTURES)
    )
    for name, value in values.items():
        if not value or name in _RUNTIME_ENVIRONMENT:
            continue
        if name.startswith("LC_"):
            continue
        if name in {"FES_TOOLCHAIN_CACHE_ROOT", "FES_TOOLCHAIN_LOCKFILE", "FES_TOOLCHAIN_ROOT", "FES_TOOLCHAIN_GPU_ROUTER", "FES_TOOLCHAIN_HIP_ARCHITECTURES", "FES_TOOLCHAIN_CUDA_ARCHITECTURES", "FES_TOOLCHAIN_CACHE_RESOLVED"}:
            continue
        if name in {"TOOLCHAIN_ROOT", "TOOLCHAIN_INSTALL", "TOOLCHAIN_BUILD"}:
            if allow_resolved_paths and name in {"TOOLCHAIN_ROOT", "TOOLCHAIN_INSTALL", "TOOLCHAIN_BUILD"}:
                continue
            raise CacheError(f"shared cache rejects local toolchain override: {name}")
        if name in {"CMAKE_HIP_ARCHITECTURES", "HIPARCHS"} and router == "HIP" and value == architectures:
            continue
        if name in {"CMAKE_CUDA_ARCHITECTURES", "CUDAARCHS"} and router == "CUDA" and value == cuda_selected:
            continue
        if name in {"CUDAARCHS", "HIPARCHS"}:
            # CMake reads these as architecture defaults, so any other value
            # would build a different binary under the same install key.
            raise CacheError(f"shared cache v1 rejects unrequested CMake architecture selector: {name}")
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
        if name in {
            "MAKEFLAGS",
            "MFLAGS",
            "NINJAFLAGS",
            "NINJA_STATUS",
            "LD_PRELOAD",
            "SOURCE_DATE_EPOCH",
            "GIT_DIR",
            "GIT_WORK_TREE",
            "GIT_SSH_COMMAND",
        } or name.startswith(("GIT_CONFIG_", "CCACHE_", "DISTCC_")):
            raise CacheError(f"shared cache rejects unsupported build environment: {name}")
        # Unknown variables are scrubbed from child builds.  Refuse them only
        # when they are an explicit toolchain control; ordinary process
        # variables remain harmless plumbing.


def _request_environment(values: Mapping[str, str]) -> dict[str, str]:
    # Lane selectors are represented canonically by ToolchainRequest's
    # ``gpu_router``/``hip_architectures``/``cuda_architectures`` fields and therefore must not also
    # be keyed as raw environment presence.  Keeping them here would make an
    # omitted default (OFF or the default HIP list) differ from the explicit
    # spelling used by bootstrap's command line.  Backend roots and compiler
    # selectors remain environment identity because they can change the
    # actual accelerator toolchain behind an otherwise identical lane.
    return {
        name: values[name]
        for name in sorted(values)
        if name in {"ROCM_PATH", "HIPCC", "CUDA_HOME", "CUDACXX", "CUDA_PATH"}
    }


def _probe_command(
    path: str,
    arguments: list[str],
    *,
    label: str,
    environment: Mapping[str, str] | None = None,
) -> str:
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
            env=None if environment is None else dict(environment),
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise CacheError(f"cannot run {label} probe: {exc}") from exc
    if result.returncode != 0:
        raise CacheError(f"{label} probe failed ({result.returncode}): {result.stdout.strip()}")
    return result.stdout


def _content_record(path: Path) -> dict[str, Any]:
    """Return a path-independent content identity for one dependency file."""

    try:
        resolved = Path(path).resolve(strict=True)
    except OSError as exc:
        raise CacheError(f"cannot resolve dependency file {path}: {exc}") from exc
    _regular_file(resolved, "dependency file")
    try:
        size = resolved.stat().st_size
    except OSError as exc:
        raise CacheError(f"cannot stat dependency file {resolved}: {exc}") from exc
    return {
        "path": str(resolved),
        "sha256": _sha256_file(resolved),
        "size": size,
    }


def _dependency_file_paths(depfile: Path) -> list[Path]:
    try:
        text = depfile.read_text(encoding="utf-8")
    except OSError as exc:
        raise CacheError(f"cannot read compiler dependency file {depfile}: {exc}") from exc
    text = text.replace("\\\n", " ")
    if ":" not in text:
        raise CacheError(f"compiler dependency file has no target: {depfile}")
    try:
        tokens = shlex.split(text.split(":", 1)[1], posix=True)
    except ValueError as exc:
        raise CacheError(f"cannot parse compiler dependency file {depfile}: {exc}") from exc
    return [Path(token) for token in tokens if token]


def _compiler_dependency_records(
    compiler: str,
    language: str,
    source: str,
    extra_flags: list[str],
    *,
    environment: Mapping[str, str] | None = None,
) -> list[dict[str, Any]]:
    """Fingerprint the files resolved by a compiler preprocessor probe."""

    with tempfile.TemporaryDirectory(prefix="misteross-cache-deps-") as directory:
        probe_root = Path(directory)
        source_path = probe_root / "probe.source"
        depfile = probe_root / "probe.d"
        source_path.write_text(source, encoding="utf-8")
        try:
            result = subprocess.run(
                [
                    compiler,
                    *extra_flags,
                    "-M",
                    "-MF",
                    str(depfile),
                    "-MT",
                    "misteross-cache-probe",
                    "-x",
                    language,
                    str(source_path),
                ],
                check=False,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                encoding="utf-8",
                errors="replace",
                timeout=60,
                env=None if environment is None else dict(environment),
            )
        except (OSError, subprocess.SubprocessError) as exc:
            raise CacheError(f"cannot fingerprint compiler dependencies: {exc}") from exc
        if result.returncode != 0:
            raise CacheError(f"compiler dependency probe failed: {result.stdout.strip()}")
        if not depfile.is_file():
            raise CacheError("compiler dependency probe did not produce a dependency file")

        source_resolved = source_path.resolve()
        records: dict[str, dict[str, Any]] = {}
        for candidate in _dependency_file_paths(depfile):
            if not candidate.is_absolute():
                candidate = depfile.parent / candidate
            try:
                resolved = candidate.resolve(strict=True)
            except OSError as exc:
                raise CacheError(f"compiler dependency file names missing file {candidate}: {exc}") from exc
            if resolved == source_resolved:
                continue
            record = _content_record(resolved)
            records[record["path"]] = record
        if not records:
            raise CacheError("compiler dependency probe resolved no dependency files")
        return [records[path] for path in sorted(records)]


def _flag_tokens(value: str, label: str) -> list[str]:
    try:
        return shlex.split(value, posix=True)
    except ValueError as exc:
        raise CacheError(f"cannot parse {label} flags: {exc}") from exc


def _compiler_library_file(
    compiler: str,
    filename: str,
    search_dirs: list[Path],
    *,
    environment: Mapping[str, str] | None = None,
) -> Path | None:
    for directory in search_dirs:
        candidate = directory / filename
        if candidate.is_file():
            return candidate

    try:
        result = subprocess.run(
            [compiler, f"-print-file-name={filename}"],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=15,
            env=None if environment is None else dict(environment),
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise CacheError(f"cannot resolve library {filename}: {exc}") from exc
    if result.returncode != 0:
        raise CacheError(f"cannot resolve library {filename}: {result.stdout.strip()}")
    resolved = result.stdout.strip()
    if not resolved or resolved == filename:
        return None
    candidate = Path(resolved)
    return candidate if candidate.is_file() else None


def _library_content_records(
    compiler: str,
    flags: str,
    *,
    environment: Mapping[str, str] | None = None,
) -> list[dict[str, Any]]:
    """Resolve and fingerprint libraries named by one pkg-config result."""

    tokens = _flag_tokens(flags, "pkg-config --libs")
    search_dirs = [Path(token[2:]) for token in tokens if token.startswith("-L") and token[2:]]
    records: dict[str, dict[str, Any]] = {}
    for token in tokens:
        if token.startswith("-l:"):
            filenames = [token[3:]]
        elif token.startswith("-l") and token[2:]:
            name = token[2:]
            filenames = [f"lib{name}.so", f"lib{name}.a"]
        elif token.startswith("/"):
            path = Path(token)
            if path.is_file():
                record = _content_record(path)
                records[record["path"]] = record
            continue
        else:
            continue

        path = None
        for filename in filenames:
            path = _compiler_library_file(
                compiler,
                filename,
                search_dirs,
                environment=environment,
            )
            if path is not None:
                break
        if path is None:
            raise CacheError(f"cannot resolve pkg-config library flag: {token}")
        record = _content_record(path)
        records[record["path"]] = record
    return [records[path] for path in sorted(records)]


def _cmake_include_flags(value: str) -> list[str]:
    flags: list[str] = []
    for raw_path in value.split(";"):
        path = raw_path.strip()
        if not path or "$<" in path:
            continue
        candidate = Path(path)
        if candidate.is_dir():
            flags.append(f"-I{candidate}")
    return flags


def _cmake_library_records(value: str) -> list[dict[str, Any]]:
    records: dict[str, dict[str, Any]] = {}
    for raw_path in value.split(";"):
        path = raw_path.strip()
        if not path or "$<" in path or path.endswith("-NOTFOUND"):
            continue
        candidate = Path(path)
        if not candidate.is_absolute() or not candidate.is_file():
            continue
        record = _content_record(candidate)
        records[record["path"]] = record
    return [records[path] for path in sorted(records)]


def _dependency_inventory(
    commands: Mapping[str, Mapping[str, Any]],
    *,
    environment: Mapping[str, str] | None = None,
) -> dict[str, Any]:
    """Capture the host dependencies used by bootstrap's real probes.

    Boost and Eigen deliberately use header/CMake probes.  They are not
    required to have pkg-config metadata on powerboat.
    """

    pkg_config = str(commands["pkg-config"]["path"])
    packages: dict[str, dict[str, str]] = {}
    library_content: dict[str, list[dict[str, Any]]] = {}
    for package in ("libffi", "readline", "tcl", "zlib", "liblzma", "libusb-1.0", "libftdi1"):
        packages[package] = {
            "modversion": _probe_command(
                pkg_config, ["--modversion", package], label=f"pkg-config {package}", environment=environment
            ),
            "cflags": _probe_command(
                pkg_config, ["--cflags", package], label=f"pkg-config {package} cflags", environment=environment
            ),
            "libs": _probe_command(
                pkg_config, ["--libs", package], label=f"pkg-config {package} libs", environment=environment
            ),
        }
        library_content[package] = _library_content_records(
            str(commands["cc"]["path"]),
            packages[package]["libs"],
            environment=environment,
        )

    cc = str(commands["cc"]["path"])
    cxx = str(commands["c++"]["path"])
    python_config = str(commands["python3-config"]["path"])
    python_includes = _probe_command(python_config, ["--includes"], label="python3-config")
    headers: dict[str, dict[str, Any]] = {}
    header_content: dict[str, list[dict[str, Any]]] = {}
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
                env=None if environment is None else dict(environment),
            )
        except (OSError, subprocess.SubprocessError) as exc:
            raise CacheError(f"cannot run header probe for {header}: {exc}") from exc
        if result.returncode != 0:
            raise CacheError(f"required header probe failed for {header}: {result.stdout.strip()}")
        headers[header] = {"compiler": compiler, "status": "ok"}
        header_content[header] = _compiler_dependency_records(
            compiler,
            language,
            source,
            list(extra),
            environment=environment,
        )

    cmake = str(commands["cmake"]["path"])
    cmake_probes: dict[str, dict[str, str]] = {}
    projects = {
        "boost-components": (
            "find_package(Boost REQUIRED COMPONENTS program_options iostreams thread)\n"
            "add_executable(probe main.cpp)\n"
            "target_link_libraries(probe PRIVATE Boost::program_options Boost::iostreams Boost::thread)\n",
            "#include <boost/program_options.hpp>\n#include <boost/iostreams/device/array.hpp>\n#include <boost/thread.hpp>\nint main() { return 0; }\n",
            "Boost_VERSION;Boost_VERSION_STRING;Boost_DIR",
            "Boost::program_options;Boost::iostreams;Boost::thread",
        ),
        "eigen3": (
            "find_package(Eigen3 REQUIRED NO_MODULE)\n"
            "add_executable(probe main.cpp)\n"
            "target_link_libraries(probe PRIVATE Eigen3::Eigen)\n",
            "#include <Eigen/Core>\nint main() { Eigen::Vector3f value; return value.size(); }\n",
            "Eigen3_VERSION_STRING;EIGEN3_VERSION_STRING;Eigen3_DIR",
            "Eigen3::Eigen",
        ),
    }
    cmake_content: dict[str, list[dict[str, Any]]] = {}
    cmake_library_content: dict[str, list[dict[str, Any]]] = {}
    for name, (find_text, source_text, variables, targets) in projects.items():
        target_lines = ""
        target_values: list[str] = []
        for index, target in enumerate(targets.split(";")):
            target_lines += (
                f"get_target_property(probe_link_{index} {target} IMPORTED_LOCATION)\n"
                f"get_target_property(probe_link_{index}_release {target} IMPORTED_LOCATION_RELEASE)\n"
                f"get_target_property(probe_link_{index}_noconfig {target} IMPORTED_LOCATION_NOCONFIG)\n"
            )
            target_values.extend(
                [
                    f"${{probe_link_{index}}}",
                    f"${{probe_link_{index}_release}}",
                    f"${{probe_link_{index}_noconfig}}",
                ]
            )
        with tempfile.TemporaryDirectory(prefix="misteross-cache-probe-") as directory:
            probe_root = Path(directory)
            result_file = probe_root / "identity.txt"
            (probe_root / "CMakeLists.txt").write_text(
                "cmake_minimum_required(VERSION 3.16)\n"
                "project(probe LANGUAGES CXX)\n"
                + find_text
                + f"file(WRITE \"{result_file}\" \"probe={name}\\n\")\n"
                + f"get_target_property(probe_include_dirs {targets.split(';')[0]} INTERFACE_INCLUDE_DIRECTORIES)\n"
                + target_lines
                + f"file(APPEND \"{result_file}\" \"include-dirs=${{probe_include_dirs}}\\n\")\n"
                + f"file(APPEND \"{result_file}\" \"link-libraries={';'.join(target_values)}\\n\")\n"
                + "\n".join(
                    f"if(DEFINED {variable})\nfile(APPEND \"{result_file}\" \"{variable}=${{{variable}}}\\n\")\nendif()"
                    for variable in variables.split(";")
                )
                + "\n",
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
                    env=None if environment is None else dict(environment),
                )
            except (OSError, subprocess.SubprocessError) as exc:
                raise CacheError(f"cannot run {name} CMake probe: {exc}") from exc
            if result.returncode != 0:
                raise CacheError(f"required {name} CMake probe failed")
            try:
                records = {}
                for line in result_file.read_text(encoding="utf-8").splitlines():
                    if "=" in line:
                        key, value = line.split("=", 1)
                        records[key] = value
            except OSError as exc:
                raise CacheError(f"required {name} CMake probe did not write identity") from exc
            cmake_probes[name] = records
            cmake_content[name] = _compiler_dependency_records(
                cxx,
                "c++",
                source_text,
                _cmake_include_flags(records.get("include-dirs", "")),
                environment=environment,
            )
            cmake_library_content[name] = _cmake_library_records(records.get("link-libraries", ""))
    return {
        "pkg-config": packages,
        "headers": headers,
        "cmake": cmake_probes,
        "python-config": python_includes.strip(),
        "content": {
            "headers": header_content,
            "libraries": library_content,
            "cmake": cmake_content,
            "cmake-libraries": cmake_library_content,
        },
    }


def request_from_environment(
    root: Path,
    lock_path: Path | None = None,
    *,
    cache_root: Path | None = None,
    gpu_router: str | None = None,
    hip_architectures: str | None = None,
    cuda_architectures: str | None = None,
) -> ToolchainRequest:
    """Build a request from the opt-in environment after host probing.

    ``gpu_router``, ``hip_architectures`` and ``cuda_architectures`` may be supplied by a consumer
    explicitly (Coleco's HIP lane does this) so selecting a lane does not
    require mutating the process environment.  Unspecified values retain the
    existing environment/default precedence.
    """

    root = Path(root).resolve()
    selected_lock = lock_path or Path(os.environ.get("FES_TOOLCHAIN_LOCKFILE", root / "toolchain.lock"))
    selected_lock = Path(selected_lock)
    if not selected_lock.is_absolute():
        selected_lock = root / selected_lock
    selected_cache = cache_root or os.environ.get("FES_TOOLCHAIN_CACHE_ROOT")
    if not selected_cache:
        raise CacheError("FES_TOOLCHAIN_CACHE_ROOT is not set; shared lane is not selected")
    router = str(
        gpu_router
        if gpu_router is not None
        else os.environ.get("FES_TOOLCHAIN_GPU_ROUTER", DEFAULT_GPU_ROUTER)
    ).upper()
    architectures = str(
        hip_architectures
        if hip_architectures is not None
        else os.environ.get("FES_TOOLCHAIN_HIP_ARCHITECTURES", DEFAULT_HIP_ARCHITECTURES)
    )
    cuda_selected = str(
        cuda_architectures
        if cuda_architectures is not None
        else os.environ.get("FES_TOOLCHAIN_CUDA_ARCHITECTURES", DEFAULT_CUDA_ARCHITECTURES)
    )
    # The resolver marker is internal plumbing for consumers after a manifest
    # has been selected.  It is never an authorization to accept a caller's
    # compiler/search-path overrides: the request must validate the original
    # environment before any generated cache paths are introduced.
    validate_shared_environment(
        os.environ,
        gpu_router=router,
        hip_architectures=architectures,
        cuda_architectures=cuda_selected,
        allow_resolved_paths=False,
    )
    environment = _request_environment(os.environ)
    host = host_identity()
    compiler = compiler_inventory(router, environment=os.environ)
    return ToolchainRequest(
        root=root,
        lock_path=selected_lock,
        cache_root=Path(selected_cache),
        gpu_router=router,
        hip_architectures=architectures,
        cuda_architectures=cuda_selected,
        host_identity=host,
        compiler_identity=compiler,
        environment=environment,
    )


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



def _lock_is_free(path: Path) -> bool:
    """Whether nobody holds *path*'s flock right now (a missing file is free)."""

    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    except FileNotFoundError:
        return True
    except OSError as exc:
        raise CacheError(f"cannot open cache lock {path}: {exc}") from exc
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return False
        fcntl.flock(descriptor, fcntl.LOCK_UN)
        return True
    finally:
        os.close(descriptor)


def _tree_bytes(path: Path) -> int:
    total = 0
    for current, directories, files in os.walk(path, followlinks=False):
        for name in (*directories, *files):
            try:
                total += os.lstat(os.path.join(current, name)).st_blocks * 512
            except OSError:
                pass
    return total


def _remove_tree(path: Path) -> None:
    """Delete a published (read-only) tree that has been renamed aside."""

    for current, directories, _files in os.walk(path, followlinks=False):
        os.chmod(current, 0o700)
        for name in directories:
            child = os.path.join(current, name)
            if not os.path.islink(child):
                os.chmod(child, 0o700)
    shutil.rmtree(path)


def _git(root: Path, *arguments: str) -> str:
    try:
        return subprocess.run(
            ["git", "-C", str(root), *arguments],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        ).stdout
    except (OSError, subprocess.CalledProcessError) as exc:
        detail = exc.stderr.strip() if isinstance(exc, subprocess.CalledProcessError) else str(exc)
        raise CacheError(f"git {' '.join(arguments)} failed: {detail}") from exc


def referenced_pins(root: Path, ref: str) -> set[tuple[str, bytes]]:
    """Return the (tool, pins) pairs every ``toolchain.lock`` at *ref* selects.

    A nextpnr pair also carries the Mistral pin it compiles against. The
    locks are read from Git objects, so the worktree's edits do not count.
    """

    root = Path(root).resolve()
    top = Path(_git(root, "rev-parse", "--show-toplevel").strip())
    prefix = root.relative_to(top).as_posix()
    names = [
        name
        for name in _git(top, "ls-tree", "-r", "--name-only", ref, "--", prefix or ".").splitlines()
        if Path(name).name == "toolchain.lock"
    ]
    if not names:
        raise CacheError(f"no toolchain.lock found at {ref}; refusing to prune without references")
    module = _lockfile_module(root)
    result: set[tuple[str, bytes]] = set()
    with tempfile.TemporaryDirectory(prefix="misteross-prune-") as directory:
        for index, name in enumerate(names):
            path = Path(directory) / f"{index}.lock"
            path.write_text(_git(top, "show", f"{ref}:{name}"), encoding="utf-8")
            pins = _pins_from_lock(module, path)
            for tool in TOOLS:
                result.add((tool, _canonical(_install_pins(tool, pins))))
    return result


def _install_pins(tool: str, pins: Mapping[str, Mapping[str, str]]) -> dict[str, Any]:
    return {
        "pin": dict(pins[tool]),
        "sources": {name: dict(pins[name]) for name in TOOL_SOURCE_DEPENDENCIES.get(tool, ())},
    }


def _manifest_pins(manifest: InstallManifest) -> bytes:
    return _canonical(
        {"pin": dict(manifest.build.get("pin", {})), "sources": dict(manifest.build.get("sources", {}))}
    )


def prune(
    cache_root: Path,
    root: Path,
    *,
    keep_days: float,
    ref: str = "origin/main",
    apply: bool = False,
    legacy_slots: bool = False,
    host: Mapping[str, Any] | None = None,
    compiler: Mapping[str, Any] | None = None,
    now: float | None = None,
    output=None,
) -> dict[str, list[str]]:
    """Remove installs that no lock on *ref* needs and nobody used lately.

    An install is kept when its pins appear in a ``toolchain.lock`` at *ref*
    and it was built on this host with this compiler inventory, or when its
    lock file (stamped by every consumer and build) is younger than
    *keep_days*. A held lock always keeps it. A directory without ready.json
    whose lock is free is a failed build and is removed. Legacy per-lock
    ``slots/`` are only considered with *legacy_slots*; their readers took
    no lock, so their binaries' access times stand in for last use.
    Nothing is removed unless *apply* is set.
    """

    output = sys.stdout if output is None else output
    cache_root = _absolute(Path(cache_root), "cache root")
    _check_no_symlink_components(cache_root, allow_missing=False)
    ensure_cache_root(cache_root)
    if keep_days < 0:
        raise CacheError("keep-days must not be negative")
    now = time.time() if now is None else now
    horizon = now - keep_days * 86400
    references = referenced_pins(root, ref)
    host = host_identity() if host is None else host
    compiler = _base_compiler(compiler_inventory(DEFAULT_GPU_ROUTER) if compiler is None else compiler)
    report: dict[str, list[str]] = {"kept": [], "removed": [], "busy": []}
    freed = 0

    def remove(path: Path, lock: Path, label: str, reason: str) -> None:
        nonlocal freed
        size = _tree_bytes(path)
        if not apply:
            if _lock_is_free(lock):
                freed += size
                print(f"would remove: {label} ({reason}, {size / 2**30:.1f} GiB)", file=output)
                report["removed"].append(label)
            else:
                print(f"busy, would skip: {label}", file=output)
                report["busy"].append(label)
            return
        descriptor = _open_lock(lock)
        try:
            try:
                fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                print(f"busy, skipped: {label}", file=output)
                report["busy"].append(label)
                return
            aside = path.with_name(f".prune-{path.name}-{os.getpid()}")
            os.rename(path, aside)
            _remove_tree(aside)
        finally:
            os.close(descriptor)
        freed += size
        print(f"removed: {label} ({reason}, {size / 2**30:.1f} GiB)", file=output)
        report["removed"].append(label)

    tools_root = cache_root / "tools"
    for tool in TOOLS:
        parent = tools_root / tool
        if not parent.is_dir():
            continue
        for directory in sorted(parent.iterdir()):
            if directory.name.startswith(".") or directory.is_symlink() or not directory.is_dir():
                continue
            label = f"tools/{tool}/{directory.name}"
            lock = cache_root / "locks" / f"{directory.name}.lock"
            ready = directory / "ready.json"
            if not ready.is_file():
                remove(directory, lock, label, "partial build")
                continue
            try:
                manifest = read_manifest(ready)
            except CacheError as exc:
                remove(directory, lock, label, f"unreadable manifest: {exc}")
                continue
            if (
                (tool, _manifest_pins(manifest)) in references
                and manifest.build.get("host") == dict(host)
                and manifest.build.get("compiler") == compiler
            ):
                print(f"kept: {label} (pinned at {ref})", file=output)
                report["kept"].append(label)
                continue
            try:
                used = (lock if lock.exists() else ready).stat().st_mtime
            except OSError:
                used = 0.0
            if used >= horizon:
                print(f"kept: {label} (used {(now - used) / 86400:.1f} days ago)", file=output)
                report["kept"].append(label)
                continue
            remove(directory, lock, label, f"unused for {(now - used) / 86400:.1f} days")

    slots = cache_root / "slots"
    if legacy_slots and slots.is_dir():
        for directory in sorted(slots.iterdir()):
            if directory.name.startswith(".") or directory.is_symlink() or not directory.is_dir():
                continue
            label = f"slots/{directory.name}"
            used = 0.0
            for path in (directory / "ready.json", *sorted((directory / "install" / "bin").glob("*"))):
                try:
                    info = path.lstat()
                except OSError:
                    continue
                used = max(used, info.st_atime, info.st_mtime)
            if used >= horizon:
                print(f"kept: {label} (legacy slot used {(now - used) / 86400:.1f} days ago)", file=output)
                report["kept"].append(label)
                continue
            remove(directory, cache_root / "locks" / f"{directory.name}.lock", label, "legacy slot")
    if apply:
        print(f"freed {freed / 2**30:.1f} GiB", file=output)
    else:
        print(f"would free {freed / 2**30:.1f} GiB; dry run, pass --apply to remove", file=output)
    return report


def _load_request_arguments(argv: list[str]):
    import argparse

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", required=True, type=Path)
    parser.add_argument("--lock", dest="lock_path", type=Path)
    parser.add_argument("--cache", dest="cache_root", type=Path)
    parser.add_argument("--gpu-router", default=None)
    parser.add_argument("--hip-architectures", default=None)
    parser.add_argument("--cuda-architectures", default=None)
    parser.add_argument("--tool", choices=TOOLS, help="act on one tool's install (verify, stage-evidence, publish)")
    parser.add_argument("command", choices=("init", "plan", "verify", "stage-evidence", "publish"))
    parser.add_argument("--build-root", type=Path)
    parser.add_argument("--format", choices=("json", "lines"), default="json")
    parser.add_argument("--expect-key", help="require the recomputed --tool key to match this value")
    parser.add_argument(
        "--lock-fd",
        type=int,
        help="inherited descriptor whose held flock must be proven for publish",
    )
    args = parser.parse_args(argv)
    environment = os.environ.copy()
    if args.gpu_router is not None:
        environment["FES_TOOLCHAIN_GPU_ROUTER"] = args.gpu_router
    if args.hip_architectures is not None:
        environment["FES_TOOLCHAIN_HIP_ARCHITECTURES"] = args.hip_architectures
    if args.cuda_architectures is not None:
        environment["FES_TOOLCHAIN_CUDA_ARCHITECTURES"] = args.cuda_architectures
    with _temporary_environment(environment):
        request = request_from_environment(
            args.root,
            args.lock_path,
            cache_root=args.cache_root,
        )
    return request, args


def _prune_main(argv: list[str]) -> int:
    import argparse

    parser = argparse.ArgumentParser(prog="toolchain_cache.py prune", description=prune.__doc__)
    parser.add_argument("--root", required=True, type=Path, help="the misteross tree whose locks are read")
    parser.add_argument("--cache", dest="cache_root", required=True, type=Path)
    parser.add_argument("--keep-days", type=float, default=14.0)
    parser.add_argument("--ref", default="origin/main", help="the Git ref whose toolchain.lock files are kept")
    parser.add_argument("--apply", action="store_true", help="remove; without it, only report")
    parser.add_argument("--legacy-slots", action="store_true", help="also prune per-lock slots/ from schema 1")
    args = parser.parse_args(argv)
    prune(
        args.cache_root,
        args.root,
        keep_days=args.keep_days,
        ref=args.ref,
        apply=args.apply,
        legacy_slots=args.legacy_slots,
    )
    return 0


def _main(argv: list[str]) -> int:
    try:
        if argv[:1] == ["prune"]:
            return _prune_main(argv[1:])
        request, args = _load_request_arguments(argv)
        if args.command in {"stage-evidence", "publish"} and args.tool is None:
            raise CacheError(f"{args.command} requires --tool")
        if args.expect_key is not None:
            if args.tool is None:
                raise CacheError("--expect-key requires --tool")
            actual_key = tool_key(request, args.tool)
            if actual_key != args.expect_key:
                raise CacheError(
                    f"request key changed across cache phases: expected {args.expect_key}, got {actual_key}"
                )
        selected = (args.tool,) if args.tool else TOOLS
        if args.command == "init":
            for tool in selected:
                ensure_lock_file(request, tool)
        elif args.command == "plan":
            keys = tool_keys(request)
            root = _absolute(Path(request.cache_root), "cache root")
            values = {
                tool: {
                    "key": keys[tool],
                    "directory": str(root / "tools" / tool / keys[tool]),
                    "lock": str(root / "locks" / f"{keys[tool]}.lock"),
                }
                for tool in selected
            }
            if args.format == "lines":
                for tool, value in values.items():
                    print("\t".join((tool, value["key"], value["directory"], value["lock"])))
            else:
                print(json.dumps(values, sort_keys=True))
        elif args.command == "verify":
            pins = locked_pins(request)
            installs = {tool: _verify_install(request, tool, pins) for tool in selected}
            print(
                json.dumps(
                    {
                        tool: {**dict(manifest.record), "key": manifest.key, "executable": str(manifest.binary)}
                        for tool, manifest in installs.items()
                    },
                    sort_keys=True,
                    separators=(",", ":"),
                )
            )
        elif args.command == "stage-evidence":
            if args.build_root is None:
                raise CacheError("stage-evidence requires --build-root")
            print(stage_evidence(request, args.tool, args.build_root))
        elif args.command == "publish":
            # Bootstrap writes the identity files first; derive the record
            # from them so the shell does not need to serialize JSON.
            if args.lock_fd is None:
                raise CacheError("ready publication must run under the per-install build lock (--lock-fd)")
            commit = locked_pins(request)[args.tool]["commit"]
            build = install_directory(request, args.tool) / "build" / args.tool
            identity_path = build / f".identity-{commit}.txt"
            digest_path = build / f".digest-{commit}.sha256"
            if not identity_path.is_file() or not digest_path.is_file():
                raise CacheError(f"missing bootstrap identity evidence for {args.tool}")
            # Bootstrap owns the kernel flock for this process tree.  Taking
            # another blocking flock here would deadlock; the inherited
            # descriptor is checked against /proc/self/fdinfo and a fresh
            # descriptor before publication.
            manifest = publish_install(
                request,
                args.tool,
                identity=identity_path.read_text(encoding="utf-8").strip(),
                external_lock_fd=args.lock_fd,
            )
            print(json.dumps({"key": manifest.key, "install": str(manifest.install)}, sort_keys=True))
        return 0
    except CacheError as exc:
        print(f"toolchain-cache: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":  # pragma: no cover - exercised by shell bootstrap
    raise SystemExit(_main(sys.argv[1:]))
