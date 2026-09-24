"""Shared authenticated FES producer operations; recipes stay in their producers."""
from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import tempfile
from dataclasses import dataclass
from pathlib import Path
from scripts.source_repository import canonical_repository
from typing import Mapping, Sequence
from scripts.lockfile import LockfileError, load_lock

TARGET = "5CSEBA6U23I7"
TOP = "top"
FES_GPU_BACKEND = "hip"
FES_GPU_ROUTER = "HIP"
FES_GPU_ARCHITECTURES = "gfx1100;gfx1201"
FES_TOOLCHAIN_CONFIGURATION = (
    f"gpu-router={FES_GPU_ROUTER}; hip-architectures={FES_GPU_ARCHITECTURES}"
)
HEX32_RE = re.compile(r"[0-9a-f]{32}\Z")
HEX40_RE = re.compile(r"[0-9a-f]{40}\Z")
HEX64_RE = re.compile(r"[0-9a-f]{64}\Z")
EXPECTED_TOOL_COMMITS = {
    "mistral": "b28e30a36b5139aaed5a5d361a30b542e6b7c758",
    "nextpnr": "49ab82f54f801c6e0f3bdb2c3f53a33880c0bc94",
    "yosys": "fb879d81e0352f558297bdcc61bc7a4a922fa7b0",
}


class BuildError(ValueError):
    """Raised when the build cannot produce authenticated passing evidence."""


def _require_gpu_backend(route_text: str) -> str:
    """Require nextpnr to have routed on a live HIP device backend."""

    lowered = route_text.lower()
    if "falling back to the cpu reference backend" in lowered or "backend cpu-reference" in lowered:
        raise BuildError(
            "route log proves that --router gpu has no live GPU device backend and "
            "fell back to the CPU reference backend"
        )
    match = re.search(r"\bbackend\s+hip:[^\n]*\bready\b", route_text, re.IGNORECASE)
    if match is None:
        raise BuildError("route log does not prove a live HIP device backend")
    return FES_GPU_BACKEND


@dataclass(frozen=True)
class AuthenticatedTool:
    path: Path
    identity: str


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


def _require_clean_source(root: Path, *, pinned_inputs: Sequence[str], identity_version: int = 2) -> tuple[str, str]:
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    from scripts.source_provenance import context
    try:
        root = context(root).module
    except ValueError as exc:
        raise BuildError(str(exc)) from exc
    revision = _git(root, "rev-parse", "HEAD")
    if HEX40_RE.fullmatch(revision) is None:
        raise BuildError("source HEAD is not a full lowercase Git commit")
    if _git(root, "status", "--porcelain", "--untracked-files=all", "--", "."):
        raise BuildError("source checkout must be clean before build and export")
    repositories = _git(root, "remote", "get-url", "--all", "origin").splitlines()
    if len(repositories) != 1:
        raise BuildError("source checkout must have exactly one origin URL")
    for relative in pinned_inputs:
        _regular_input(root, relative)
        try:
            _git(root, "ls-files", "--error-unmatch", "--", relative)
        except BuildError as exc:
            raise BuildError(f"pinned build input is not tracked: {relative}") from exc
    try:
        repository = canonical_repository(repositories[0])
    except ValueError as exc:
        raise BuildError(str(exc)) from exc
    return repository, revision


def _read_evidence(path: Path, expected: str) -> str:
    if path.is_symlink() or not path.is_file():
        raise BuildError(f"missing tool authentication evidence: {path}")
    try:
        value = path.read_text(encoding="utf-8").strip()
    except OSError as exc:
        raise BuildError(f"cannot read tool authentication evidence: {path}") from exc
    if expected == "digest" and HEX64_RE.fullmatch(value) is None:
        raise BuildError(f"invalid tool digest evidence: {path}")
    return value


def _probe_authenticated_tool(
    root: Path,
    path: Path,
    lock_name: str,
    executable: str,
    arguments: tuple[str, ...],
) -> None:
    """Run the same short identity probe for local and shared tools."""

    try:
        result = subprocess.run(
            [str(path), *arguments],
            cwd=root,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=30.0,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise BuildError(f"cannot execute authenticated tool identity check: {executable}") from exc
    output = "\n".join((result.stdout, result.stderr)).strip()
    if result.returncode != 0 or not output:
        raise BuildError(f"authenticated tool identity check failed: {executable}")
    if lock_name == "mistral" and TARGET not in output:
        raise BuildError(f"authenticated Mistral database does not list {TARGET}")


def _authenticate_shared_tools(
    root: Path,
    *,
    lock_path: Path,
    expected_commits: Mapping[str, str],
    expected_configuration: Mapping[str, str],
    gpu_router: str | None,
    hip_architectures: str | None,
    cache_root: Path,
) -> dict[str, AuthenticatedTool]:
    """Resolve immutable shared tools through the verified cache manifest."""

    try:
        from scripts import toolchain_cache

        request = toolchain_cache.request_from_environment(
            root,
            lock_path,
            cache_root=cache_root,
            gpu_router=gpu_router,
            hip_architectures=hip_architectures,
        )
        manifest = toolchain_cache.resolve_ready(request)
    except toolchain_cache.CacheError as exc:
        raise BuildError(f"shared toolchain verification failed: {exc}") from exc

    definitions = (
        ("yosys", "yosys", "yosys", ("--version",)),
        ("mistral", "mistral", "mistral-cv", ("models",)),
        ("nextpnr-mistral", "nextpnr", "nextpnr-mistral", ("--version",)),
    )
    authenticated: dict[str, AuthenticatedTool] = {}
    for record_name, lock_name, executable, arguments in definitions:
        record = manifest.tools.get(lock_name)
        if not isinstance(record, Mapping):
            raise BuildError(f"shared toolchain manifest has no authenticated {lock_name} record")
        commit = record.get("commit")
        digest = record.get("sha256")
        binary_name = record.get("binary")
        manifest_path = record.get("path")
        if commit != expected_commits[lock_name]:
            raise BuildError(
                f"shared authenticated {lock_name} commit {expected_commits[lock_name]}, got {commit}"
            )
        expected_binary = f"bin/{executable}"
        if binary_name != expected_binary or manifest_path != f"install/{expected_binary}":
            raise BuildError(f"shared authenticated tool path is invalid: {executable}")
        path = manifest.slot / Path(manifest_path)
        if path.is_symlink() or not path.is_file() or not os.access(path, os.X_OK):
            raise BuildError(f"shared authenticated tool is not a regular executable: {path}")
        if not isinstance(digest, str) or HEX64_RE.fullmatch(digest) is None:
            raise BuildError(f"shared authenticated tool digest is invalid: {executable}")
        configuration = expected_configuration.get(lock_name)
        if configuration is not None:
            try:
                manifest_configuration = (
                    f"gpu-router={manifest.configuration['gpu-router']}; "
                    f"hip-architectures={manifest.configuration['hip-architectures']}"
                )
            except (KeyError, TypeError) as exc:
                raise BuildError(
                    f"shared toolchain manifest configuration is invalid: {executable}"
                ) from exc
            if manifest_configuration != configuration:
                raise BuildError(
                    f"shared tool configuration does not match the requested build lane: {executable}"
                )
            configuration_path = manifest.evidence / lock_name / f".config-{commit}.txt"
            actual_configuration = _read_evidence(configuration_path, "configuration")
            if actual_configuration != configuration:
                raise BuildError(
                    f"shared tool configuration evidence does not match the requested build lane: {executable}"
                )
        _probe_authenticated_tool(root, path, lock_name, executable, arguments)
        identity = f"commit={commit}; sha256={digest}"
        if configuration is not None:
            identity += f"; {configuration}"
        authenticated[record_name] = AuthenticatedTool(path=path, identity=identity)
    try:
        toolchain_cache.verify_ready(request)
    except toolchain_cache.CacheError as exc:
        raise BuildError(f"shared toolchain verification failed after identity probes: {exc}") from exc
    return authenticated


def _fes_hip_local_provision_hint(root: Path, lock_path: Path, configuration: str) -> str:
    if configuration != FES_TOOLCHAIN_CONFIGURATION:
        return ""
    try:
        relative = lock_path.resolve().relative_to(root.resolve()).as_posix()
    except (OSError, ValueError):
        return ""
    target = {
        "toolchain.lock": "toolchain-fes",
        "toolchains/zx81-expansion.lock": "toolchain-fes-zx81",
    }.get(relative)
    return f"; run `make {target}` to provision the FES HIP local toolchain" if target else ""


def _authenticate_tools(
    root: Path,
    *,
    lock_path: Path | None = None,
    toolchain_root: Path | None = None,
    expected_commits: Mapping[str, str] | None = None,
    expected_configuration: Mapping[str, str] | None = None,
    gpu_router: str | None = None,
    hip_architectures: str | None = None,
    cache_root: Path | None = None,
) -> dict[str, AuthenticatedTool]:
    root = Path(root).resolve()
    lock_path = root / "toolchain.lock" if lock_path is None else Path(lock_path)
    if not lock_path.is_absolute():
        lock_path = root / lock_path
    toolchain_root = root / "build/toolchain" if toolchain_root is None else Path(toolchain_root)
    if not toolchain_root.is_absolute():
        toolchain_root = root / toolchain_root
    expected_commits = EXPECTED_TOOL_COMMITS if expected_commits is None else expected_commits
    gpu_router = FES_GPU_ROUTER if gpu_router is None else gpu_router
    hip_architectures = FES_GPU_ARCHITECTURES if hip_architectures is None else hip_architectures
    expected_configuration = (
        {"nextpnr": FES_TOOLCHAIN_CONFIGURATION}
        if expected_configuration is None
        else expected_configuration
    )
    definitions = (
        ("yosys", "yosys", "yosys", ("--version",)),
        ("mistral", "mistral", "mistral-cv", ("models",)),
        ("nextpnr-mistral", "nextpnr", "nextpnr-mistral", ("--version",)),
    )
    if cache_root is not None:
        return _authenticate_shared_tools(
            root,
            lock_path=lock_path,
            expected_commits=expected_commits,
            expected_configuration=expected_configuration,
            gpu_router=gpu_router,
            hip_architectures=hip_architectures,
            cache_root=Path(cache_root),
        )
    try:
        pins = load_lock(lock_path)
    except (OSError, LockfileError, ValueError) as exc:
        raise BuildError(f"cannot load pinned toolchain: {exc}") from exc
    install = toolchain_root / "install/bin"
    build_root = toolchain_root / "build"
    authenticated: dict[str, AuthenticatedTool] = {}
    for record_name, lock_name, executable, arguments in definitions:
        pin = pins[lock_name]
        if pin.commit != expected_commits[lock_name]:
            raise BuildError(
                f"authenticated {lock_name} commit {expected_commits[lock_name]}, "
                f"got {pin.commit}"
            )
        path = install / executable
        if path.is_symlink() or not path.is_file() or not os.access(path, os.X_OK):
            raise BuildError(f"pinned tool is not a regular executable: {path}")
        stamp = build_root / lock_name / f".built-{pin.commit}"
        digest_path = build_root / lock_name / f".digest-{pin.commit}.sha256"
        if _read_evidence(stamp, "stamp") != f"commit={pin.commit}":
            raise BuildError(f"tool build stamp does not match locked commit: {executable}")
        expected_digest = _read_evidence(digest_path, "digest")
        actual_digest = _sha256(path)
        if actual_digest != expected_digest:
            raise BuildError(f"tool executable digest does not match lock evidence: {executable}")
        configuration = expected_configuration.get(lock_name)
        if configuration is not None:
            configuration_path = build_root / lock_name / f".config-{pin.commit}.txt"
            hint = _fes_hip_local_provision_hint(root, lock_path, configuration)
            try:
                actual_configuration = _read_evidence(configuration_path, "configuration")
            except BuildError as exc:
                raise BuildError(f"{exc}{hint}") from exc
            if actual_configuration != configuration:
                raise BuildError(
                    f"tool configuration does not match the requested build lane: {executable}{hint}"
                )
        _probe_authenticated_tool(root, path, lock_name, executable, arguments)
        identity = f"commit={pin.commit}; sha256={actual_digest}"
        if configuration is not None:
            identity += f"; {configuration}"
        authenticated[record_name] = AuthenticatedTool(
            path=path,
            identity=identity,
        )
    return authenticated


def _write_atomic(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink() or (path.exists() and not path.is_file()):
        raise BuildError(f"build output is not a regular file: {path}")
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def _prepare_output(root: Path, *, relative: Path, build_outputs: Sequence[str]) -> Path:
    output = root / relative
    build_root = root / "build"
    if build_root.is_symlink() or (build_root.exists() and not build_root.is_dir()):
        raise BuildError(f"build root must be a non-symlink directory: {build_root}")
    if output.is_symlink() or (output.exists() and not output.is_dir()):
        raise BuildError(f"FES output must be a non-symlink directory: {output}")
    output.mkdir(parents=True, exist_ok=True)
    for name in build_outputs:
        path = output / name
        if path.is_symlink() or (path.exists() and not path.is_file()):
            raise BuildError(f"build output must be a regular file: {path}")
        if path.exists():
            path.unlink()
    return output


def _run_tool(command: tuple[str, ...], cwd: Path, log: Path,
              *, output_relative: Path, env=None, audit_source_root=None) -> None:
    try:
        from scripts.compiler_read_audit import audited_run
        runner = subprocess.run if audit_source_root is None else audited_run
        result = runner(
            list(command),
            **({"source_root": audit_source_root} if audit_source_root is not None else {}),
            cwd=cwd,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
        )
    except OSError as exc:
        raise BuildError(f"cannot run authenticated tool: {command[0]}") from exc
    _write_atomic(log, result.stdout)
    if result.returncode != 0:
        # nextpnr-mistral can emit an intermediate timing ERROR, retry, then
        # finish a passing route and still exit 1. Accept a finished route
        # with a payload; validate_build_evidence still requires final PASS.
        routed = cwd / output_relative / "core.rbf"
        if (
            Path(command[0]).name == "nextpnr-mistral"
            and b"Info: Program finished normally." in result.stdout
            and routed.is_file()
            and not routed.is_symlink()
            and routed.stat().st_size > 0
        ):
            return
        raise BuildError(f"tool failed with exit {result.returncode}: {command[0]}; see {log}")


def _read_json(path: Path, label: str) -> dict:
    if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
        raise BuildError(f"missing nonempty {label}: {path}")
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BuildError(f"invalid {label}: {path}") from exc
    if not isinstance(value, dict):
        raise BuildError(f"{label} must be a JSON object")
    return value


def validate_timing_resources(utilization: object, known: set[str] | frozenset[str]) -> dict[str, dict[str, int]]:
    """Keep valid unused toolchain rows, but reject any unrecognized resource in use."""
    if not isinstance(utilization, dict):
        raise BuildError("timing report has no structured utilization data")
    if any(not isinstance(name, str) or not name for name in utilization):
        raise BuildError("timing report has an invalid resource name")
    resources: dict[str, dict[str, int]] = {}
    for name, fields in sorted(utilization.items()):
        if not isinstance(fields, dict):
            raise BuildError(f"malformed resource evidence: {name}")
        used, available = fields.get("used"), fields.get("available")
        if type(used) is not int or used < 0 or type(available) is not int or available < 0:
            raise BuildError(f"malformed resource counts: {name}")
        resources[name] = {"available": available, "used": used}
    unknown_used = sorted(name for name in resources if name not in known and resources[name]["used"] != 0)
    if unknown_used:
        raise BuildError("timing report contains unknown resources in use: " + ", ".join(unknown_used))
    return resources


def _cell_counts(synthesis: dict) -> dict[str, int]:
    modules = synthesis.get("modules")
    if not isinstance(modules, dict):
        raise BuildError("synthesis evidence has no modules")
    counts: dict[str, int] = {}
    for module in modules.values():
        if not isinstance(module, dict) or not isinstance(module.get("cells"), dict):
            continue
        for cell in module["cells"].values():
            if isinstance(cell, dict) and isinstance(cell.get("type"), str):
                name = cell["type"]
                counts[name] = counts.get(name, 0) + 1
    return counts


def _i2c_evidence(design: dict, label: str) -> None:
    module = design.get("modules", {}).get(TOP, {})
    cells = module.get("cells", {})
    bridges = [cell for cell in cells.values()
               if cell.get("type") == "cyclonev_hps_interface_peripheral_i2c"]
    if len(bridges) != 1:
        raise BuildError(f"{label} HDMI I2C requires exactly one HPS bridge")
    bridge = bridges[0]
    site = "cyclonev_hps_interface_peripheral_i2c.52.60.0"
    placement = "NEXTPNR_BEL" if label == "routed" else "BEL"
    if bridge.get("attributes", {}).get(placement) != site:
        raise BuildError(f"{label} HDMI I2C must use HPS site X52 Y60")
    connections = bridge.get("connections", {})
    if (set(connections) != {"out_clk", "out_data", "scl", "sda"}
            or any(not isinstance(bits, list) or len(bits) != 1
                   or type(bits[0]) is not int for bits in connections.values())
            or len({bits[0] for bits in connections.values()}) != 4):
        raise BuildError(f"{label} HDMI I2C requires four distinct signal nets")
    grounds = [["0"]]
    if label == "routed":
        grounds += [cell.get("connections", {}).get("Q") for cell in cells.values()
                    if cell.get("type") == "MISTRAL_CONST"
                    and re.fullmatch("0+", str(cell.get("parameters", {}).get("LUT", "")))
                    and isinstance(cell.get("connections", {}).get("Q"), list)
                    and len(cell["connections"]["Q"]) == 1]
    for name, enable, feedback, pin, bel in (
        ("hdmi_scl_pad", "out_clk", "scl", "PIN_U10", "MISTRAL_IO.6.0.0"),
        ("hdmi_sda_pad", "out_data", "sda", "PIN_AA4", "MISTRAL_IO.4.0.2"),
    ):
        pad = cells.get(name, {})
        ports = pad.get("connections", {})
        if (pad.get("type") != "MISTRAL_IO" or ports.get("I") not in grounds
                or ports.get("OE") != connections[enable]
                or ports.get("O") != connections[feedback]):
            raise BuildError(f"{label} HDMI I2C {name} must drive low or release with pad feedback")
        port_name = "HDMI_I2C_SCL" if enable == "out_clk" else "HDMI_I2C_SDA"
        port = module.get("ports", {}).get(port_name, {})
        if (port.get("direction") != "inout" or not isinstance(port.get("bits"), list)
                or len(port["bits"]) != 1 or ports.get("PAD") != port["bits"]):
            raise BuildError(f"{label} HDMI I2C {name} must connect its bidirectional pad")
        if label == "routed" and (
                pad.get("attributes", {}).get("LOC") != pin
                or pad.get("attributes", {}).get("NEXTPNR_BEL") != bel):
            raise BuildError(f"routed HDMI I2C {name} must use {pin}")


def _invalidate_failed_artifact(output: Path) -> None:
    for name in ("core.rbf", "manifest.toml", "build-summary.json"):
        path = output / name
        if path.is_symlink() or path.is_file():
            path.unlink()
        elif path.exists():
            raise BuildError(f"cannot invalidate non-file failed build output: {path}")
