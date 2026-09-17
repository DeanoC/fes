#!/usr/bin/env python3
"""Run the existing package acceptance flow in an isolated host container."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import secrets
import signal
import socket
import stat
import subprocess
import sys
import tomllib
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


PACKAGE_ACCEPTANCE = Path(__file__).with_name("package_acceptance.py")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
DEVELOPMENT_HOST_RE = re.compile(r"^(?:diag|dev)[-_][A-Za-z0-9_.-]+$")
IMAGE_RE = re.compile(r"^sha256:[0-9a-f]{64}$")
CORE_ID_RE = re.compile(r"^[a-z][a-z0-9_.-]{0,95}$")
PACKAGE_ID_RE = SHA256_RE
MAX_ARCHIVE_BYTES = 33 * 1024 * 1024
MAX_TIMEOUT = 300.0
MAX_CONTAINER_TIMEOUT = 600.0
MAX_SHUTDOWN_TIMEOUT = 120.0
MAX_POLL_ATTEMPTS = 600
MAX_POLL_INTERVAL = 30.0
MAX_STRING = 256
MAX_BINARY_BYTES = 256 * 1024 * 1024
MAX_PRIVATE_CONFIG_BYTES = 64 * 1024
STARTUP_POLL_INTERVAL = 0.05
MAX_HTTP_BODY = 1 << 20
LISTEN_RE = re.compile(r"(?m)^FogCast API listening on http://127\.0\.0\.1:(\d{1,5})[ \t]*$")


class AcceptanceError(RuntimeError):
    """An operator-facing validation or acceptance error."""


@dataclass(frozen=True)
class PrivateTarget:
    name: str
    address: str
    agent: str
    target_id: str


@dataclass
class PrivateHome:
    root: Path
    config: Path
    config_identity: tuple[int, int]
    target: PrivateTarget


@dataclass
class ContainerRecord:
    name: str
    cycle: str
    listen_port: int | None = None
    exit_code: int | None = None
    status: str | None = None
    stop_error: str | None = None
    remove_error: str | None = None
    forced_remove_error: str | None = None
    start_attempted: bool = False
    started: bool = False
    stop_attempted: bool = False
    remove_attempts: int = 0
    remove_attempted: bool = False
    removed: bool = False


def _interrupt(signum: int, _frame: Any) -> None:
    raise AcceptanceError(f"interrupted by {signal.Signals(signum).name}")


def _require_string(value: Any, name: str, maximum: int = MAX_STRING) -> str:
    if (
        not isinstance(value, str)
        or not value
        or len(value) > maximum
        or any(ord(char) < 32 for char in value)
    ):
        raise AcceptanceError(f"{name} must be a bounded non-empty string")
    return value


def _require_digest(value: Any, name: str) -> str:
    if not isinstance(value, str) or SHA256_RE.fullmatch(value) is None:
        raise AcceptanceError(f"{name} must be 64 lowercase hexadecimal characters")
    return value


def _require_package_id(value: Any, name: str) -> str:
    if not isinstance(value, str) or PACKAGE_ID_RE.fullmatch(value) is None:
        raise AcceptanceError(f"{name} must be 64 lowercase hexadecimal characters")
    return value


def _require_core_id(value: Any) -> str:
    if not isinstance(value, str) or CORE_ID_RE.fullmatch(value) is None:
        raise AcceptanceError("expected core ID is invalid")
    return value


def _require_file(path_value: Any, name: str) -> Path:
    path = Path(_require_string(path_value, name, 4096))
    try:
        info = path.lstat()
    except OSError as exc:
        raise AcceptanceError(f"cannot inspect {name}: {exc}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
        raise AcceptanceError(f"{name} must be a regular non-symlink file")
    return path


def _read_digest(path: Path, name: str, maximum: int | None = None) -> tuple[int, str]:
    try:
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode):
            raise AcceptanceError(f"{name} must be a regular non-symlink file")
        if maximum is not None and (info.st_size < 1 or info.st_size > maximum):
            raise AcceptanceError(f"{name} size is outside the bounded range")
        digest = hashlib.sha256()
        size = 0
        with path.open("rb") as handle:
            while True:
                chunk = handle.read(1024 * 1024)
                if not chunk:
                    break
                size += len(chunk)
                if maximum is not None and size > maximum:
                    raise AcceptanceError(f"{name} exceeds the bounded size")
                digest.update(chunk)
        if size != info.st_size:
            raise AcceptanceError(f"{name} changed while it was read")
        return size, digest.hexdigest()
    except AcceptanceError:
        raise
    except OSError as exc:
        raise AcceptanceError(f"cannot read {name}: {exc}") from exc


def _load_private_target(path: Path, expected_target_id: str) -> PrivateTarget:
    try:
        info = path.lstat()
    except OSError as exc:
        raise AcceptanceError(f"cannot inspect host config: {exc}") from exc
    if info.st_mode & 0o077:
        raise AcceptanceError("host config must be private (mode 0600 or stricter)")
    try:
        with path.open("rb") as handle:
            config = tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise AcceptanceError(f"cannot read host config: {exc}") from exc
    if not isinstance(config, dict):
        raise AcceptanceError("host config must be a TOML table")
    legacy_token = config.get("token", "")
    if not isinstance(legacy_token, str):
        raise AcceptanceError("host config token has an invalid type")
    selected_name = config.get("selected_target")
    if not isinstance(selected_name, str) or not selected_name:
        raise AcceptanceError("host config has no selected target")
    targets = config.get("targets")
    if not isinstance(targets, list):
        raise AcceptanceError("host config has no target list")
    matches = [
        target
        for target in targets
        if isinstance(target, dict) and target.get("target_id") == expected_target_id
    ]
    if len(matches) != 1:
        raise AcceptanceError("host config must contain exactly one expected target ID")
    target = matches[0]
    if target.get("enabled") is not True or target.get("name") != selected_name:
        raise AcceptanceError("expected target must be the enabled selected target")
    name = _require_string(target.get("name"), "target name")
    address = _require_string(target.get("address"), "target address", 2048)
    parsed = urllib.parse.urlparse(address)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise AcceptanceError("target address must be an HTTP URL")
    if parsed.username is not None or parsed.password is not None:
        raise AcceptanceError("target address must not contain URL userinfo")
    agent = _require_string(target.get("agent"), "target agent", 4096)
    return PrivateTarget(name, address, agent, expected_target_id)


def _toml_string(value: str) -> str:
    return json.dumps(value, ensure_ascii=True)


def _minimal_config(target: PrivateTarget) -> str:
    return "\n".join(
        [
            'token = ""',
            f"selected_target = {_toml_string(target.name)}",
            "libraries = []",
            "library_media = []",
            "request_timeout_seconds = 30",
            "upload_timeout_seconds = 60",
            "",
            "[[targets]]",
            f"name = {_toml_string(target.name)}",
            "enabled = true",
            f"address = {_toml_string(target.address)}",
            f"agent = {_toml_string(target.agent)}",
            f"target_id = {_toml_string(target.target_id)}",
            "",
            "[remote_input]",
            "enabled = false",
            "",
            "[media]",
            "enabled = false",
            "",
            "[metadata]",
            "enabled = false",
            "",
        ]
    )


def prepare_private_home(evidence_dir: Path, target: PrivateTarget) -> PrivateHome:
    root = evidence_dir / "container-home"
    config_dir = root / ".config" / "fogcast"
    config: Path | None = None
    config_identity: tuple[int, int] | None = None
    try:
        for directory in (
            root,
            root / ".config",
            config_dir,
            root / ".local",
            root / ".local" / "share",
            root / ".local" / "share" / "fogcast",
            root / ".cache",
            root / ".cache" / "fogcast",
        ):
            directory.mkdir(mode=0o700)
            os.chmod(directory, 0o700)
        config = config_dir / "config.toml"
        with config.open("x", encoding="utf-8") as handle:
            info = os.fstat(handle.fileno())
            config_identity = (info.st_dev, info.st_ino)
            handle.write(_minimal_config(target))
        os.chmod(config, 0o600)
        info = config.lstat()
        config_identity = (info.st_dev, info.st_ino)
        return PrivateHome(root, config, config_identity, target)
    except BaseException as exc:
        if config is not None and config_identity is not None:
            try:
                info = config.lstat()
                if (
                    stat.S_ISREG(info.st_mode)
                    and not stat.S_ISLNK(info.st_mode)
                    and (info.st_dev, info.st_ino) == config_identity
                ):
                    config.unlink()
            except BaseException as cleanup_error:
                if hasattr(exc, "add_note"):
                    exc.add_note(f"private credential cleanup failed: {_safe_text(cleanup_error)}")
        if isinstance(exc, AcceptanceError):
            raise
        if isinstance(exc, OSError):
            raise AcceptanceError(f"cannot create private host config: {exc}") from exc
        raise


def _read_private_config(path: Path, identity: tuple[int, int], expected: bytes) -> None:
    fd: int | None = None
    try:
        flags = os.O_RDONLY | os.O_CLOEXEC | getattr(os, "O_NOFOLLOW", 0)
        fd = os.open(path, flags)
        opened = os.fstat(fd)
        if (
            not stat.S_ISREG(opened.st_mode)
            or opened.st_mode & 0o077
            or (opened.st_dev, opened.st_ino) != identity
        ):
            raise AcceptanceError("private credential changed; refusing to remove an unfamiliar file")
        handle = os.fdopen(fd, "rb")
        fd = None
        with handle:
            contents = handle.read(MAX_PRIVATE_CONFIG_BYTES + 1)
        current = path.lstat()
        if (
            stat.S_ISLNK(current.st_mode)
            or not stat.S_ISREG(current.st_mode)
            or current.st_mode & 0o077
            or (current.st_dev, current.st_ino) != identity
        ):
            raise AcceptanceError("private credential changed; refusing to remove an unfamiliar file")
    except AcceptanceError:
        raise
    except OSError as exc:
        raise AcceptanceError(f"cannot inspect private credential: {exc}") from exc
    finally:
        if fd is not None:
            os.close(fd)
    if len(contents) > MAX_PRIVATE_CONFIG_BYTES or contents != expected:
        raise AcceptanceError("private credential changed; refusing to remove an unfamiliar file")


def remove_private_config(home: PrivateHome) -> None:
    try:
        info = home.config.lstat()
    except FileNotFoundError:
        return
    except OSError as exc:
        raise AcceptanceError(f"cannot inspect private credential: {exc}") from exc
    if (
        stat.S_ISLNK(info.st_mode)
        or not stat.S_ISREG(info.st_mode)
        or info.st_mode & 0o077
        or (info.st_dev, info.st_ino) != home.config_identity
    ):
        raise AcceptanceError("private credential changed; refusing to remove an unfamiliar file")
    _read_private_config(
        home.config,
        home.config_identity,
        _minimal_config(home.target).encode("utf-8"),
    )
    try:
        info = home.config.lstat()
        if (
            stat.S_ISLNK(info.st_mode)
            or not stat.S_ISREG(info.st_mode)
            or info.st_mode & 0o077
            or (info.st_dev, info.st_ino) != home.config_identity
        ):
            raise AcceptanceError("private credential changed; refusing to remove an unfamiliar file")
        home.config.unlink()
    except AcceptanceError:
        raise
    except OSError as exc:
        raise AcceptanceError(f"cannot remove private credential: {exc}") from exc


def remove_unreturned_private_config(evidence_dir: Path, target: PrivateTarget) -> None:
    config = evidence_dir / "container-home" / ".config" / "fogcast" / "config.toml"
    try:
        info = config.lstat()
    except FileNotFoundError:
        return
    except OSError as exc:
        raise AcceptanceError(f"cannot inspect private credential: {exc}") from exc
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISREG(info.st_mode) or info.st_mode & 0o077:
        raise AcceptanceError("private credential changed; refusing to remove an unfamiliar file")
    identity = (info.st_dev, info.st_ino)
    _read_private_config(
        config,
        identity,
        _minimal_config(target).encode("utf-8"),
    )
    try:
        info = config.lstat()
        if (
            stat.S_ISLNK(info.st_mode)
            or not stat.S_ISREG(info.st_mode)
            or info.st_mode & 0o077
            or (info.st_dev, info.st_ino) != identity
        ):
            raise AcceptanceError("private credential changed; refusing to remove an unfamiliar file")
        config.unlink()
    except AcceptanceError:
        raise
    except OSError as exc:
        raise AcceptanceError(f"cannot remove private credential: {exc}") from exc


def validate_image_metadata(image: str, metadata: Any) -> None:
    if os.getuid() == 0 or os.getgid() == 0:
        raise AcceptanceError("isolated package acceptance must run as a non-root user")
    if not isinstance(metadata, dict) or metadata.get("Id") != image:
        raise AcceptanceError("container image identity does not match the requested immutable ID")
    config = metadata.get("Config")
    if not isinstance(config, dict) or config.get("User") != "builder":
        raise AcceptanceError("container image must run as the supported builder user")
    labels = config.get("Labels")
    if (
        not isinstance(labels, dict)
        or labels.get("org.fes.media.uid") != str(os.getuid())
        or labels.get("org.fes.media.gid") != str(os.getgid())
    ):
        raise AcceptanceError("container image lacks the supported builder UID/GID labels")
    for value in config.get("Env") or []:
        if isinstance(value, str) and value.startswith("HOME=") and value != "HOME=/home/builder":
            raise AcceptanceError("container image has an unsupported HOME value")


def snapshot_binary(source: Path, evidence_dir: Path, expected_digest: str) -> Path:
    snapshot = evidence_dir / "host-binary"
    try:
        before = source.lstat()
        if stat.S_ISLNK(before.st_mode) or not stat.S_ISREG(before.st_mode):
            raise AcceptanceError("host binary must be a regular non-symlink file")
        digest = hashlib.sha256()
        size = 0
        with source.open("rb") as source_handle, snapshot.open("xb") as snapshot_handle:
            while True:
                chunk = source_handle.read(1024 * 1024)
                if not chunk:
                    break
                size += len(chunk)
                if size > MAX_BINARY_BYTES:
                    raise AcceptanceError("host binary exceeds the bounded size")
                digest.update(chunk)
                snapshot_handle.write(chunk)
        after = source.lstat()
        if (
            stat.S_ISLNK(after.st_mode)
            or not stat.S_ISREG(after.st_mode)
            or (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns)
            != (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns)
        ):
            raise AcceptanceError("host binary changed while it was snapshotted")
        observed = digest.hexdigest()
        if size != after.st_size or observed != expected_digest:
            raise AcceptanceError(
                f"host binary sha256 mismatch: expected {expected_digest}, observed {observed}"
            )
        os.chmod(snapshot, 0o500)
        return snapshot
    except AcceptanceError:
        raise
    except OSError as exc:
        raise AcceptanceError(f"cannot snapshot host binary: {exc}") from exc


def _safe_text(value: Any, secrets_to_redact: tuple[str, ...] = ()) -> str:
    text = value.decode("utf-8", "replace") if isinstance(value, bytes) else str(value or "")
    for secret in secrets_to_redact:
        if secret:
            text = text.replace(secret, "<redacted>")
    return text


def _write_private_text(path: Path, value: str) -> None:
    try:
        with path.open("x", encoding="utf-8") as handle:
            handle.write(value)
        os.chmod(path, 0o600)
    except OSError as exc:
        raise AcceptanceError(f"cannot write evidence log: {exc}") from exc


def _write_json_new(path: Path, value: dict[str, Any]) -> None:
    try:
        with path.open("x", encoding="utf-8") as handle:
            import json

            json.dump(value, handle, indent=2, sort_keys=True)
            handle.write("\n")
        os.chmod(path, 0o600)
    except FileExistsError as exc:
        raise AcceptanceError(f"refusing to overwrite evidence: {path}") from exc
    except OSError as exc:
        raise AcceptanceError(f"cannot write evidence: {exc}") from exc


def _container_state(value: Any) -> dict[str, Any]:
    state = value.get("State", value) if isinstance(value, dict) else None
    if not isinstance(state, dict):
        raise AcceptanceError("container inspection has no state")
    return state


def _listen_port(log: str, requested: int) -> int | None:
    matches = {int(value) for value in LISTEN_RE.findall(log)}
    if not matches:
        return None
    if len(matches) != 1:
        raise AcceptanceError("isolated host startup log contains multiple listener addresses")
    port = matches.pop()
    if not 1 <= port <= 65535:
        raise AcceptanceError("isolated host reported an invalid listener port")
    if requested and port != requested:
        raise AcceptanceError("isolated host listener did not bind the requested port")
    return port


def _require_port_available(port: int) -> None:
    if port == 0:
        return
    probe = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    try:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 0)
        probe.bind(("127.0.0.1", port))
    except OSError as exc:
        raise AcceptanceError(f"listen port {port} is occupied") from exc
    finally:
        probe.close()


def _get_json(origin: str, path: str, timeout: float) -> dict[str, Any]:
    request = urllib.request.Request(origin.rstrip("/") + path, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read(MAX_HTTP_BODY + 1)
    except (urllib.error.URLError, TimeoutError, OSError) as exc:
        raise AcceptanceError(f"isolated host readiness request failed: {type(exc).__name__}") from exc
    if len(raw) > MAX_HTTP_BODY:
        raise AcceptanceError("isolated host readiness response exceeded the bounded size")
    try:
        value = json.loads(raw)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise AcceptanceError("isolated host readiness response was not JSON") from exc
    if not isinstance(value, dict):
        raise AcceptanceError("isolated host readiness response was not an object")
    return value


def _probe_host(origin: str, args: argparse.Namespace, target: PrivateTarget) -> None:
    value = _get_json(origin, "/api/v1/health", args.timeout)
    if value.get("ready") is not True:
        raise AcceptanceError("isolated host is not ready")
    target_value = value.get("target")
    connection = target_value.get("connection") if isinstance(target_value, dict) else None
    artifacts = target_value.get("artifacts") if isinstance(target_value, dict) else None
    if (
        not isinstance(target_value, dict)
        or target_value.get("reachable") is not True
        or target_value.get("ready") is not True
        or not isinstance(connection, dict)
        or connection.get("target_id") != args.expected_target_id
        or not isinstance(artifacts, dict)
    ):
        raise AcceptanceError("isolated host target identity is not ready")
    observed = {
        "host": (value.get("host") or {}).get("revision"),
        "agent": artifacts.get("agent_revision"),
        "runtime": artifacts.get("runtime_commit"),
    }
    expected = {
        "host": args.expected_host_revision,
        "agent": args.expected_agent_revision,
        "runtime": args.expected_runtime_revision,
    }
    for name in ("host", "agent", "runtime"):
        if observed[name] != expected[name]:
            raise AcceptanceError(f"isolated host {name} identity does not match the expected revision")


def wait_for_host(
    runtime: DockerRuntime,
    container: ContainerRecord,
    args: argparse.Namespace,
    target: PrivateTarget,
) -> tuple[str, str]:
    deadline = time.monotonic() + args.container_timeout
    last_error = ""
    while True:
        inspection = runtime.inspect(container.name)
        state = _container_state(inspection)
        if state.get("Running") is not True:
            raise AcceptanceError(
                f"isolated host container exited before readiness (exit {state.get('ExitCode')!r})"
            )
        log = runtime.logs(container.name)
        try:
            port = _listen_port(log, args.listen_port)
        except AcceptanceError as exc:
            last_error = str(exc)
            port = None
        if port is not None:
            origin = f"http://127.0.0.1:{port}"
            try:
                _probe_host(origin, args, target)
            except AcceptanceError as exc:
                last_error = str(exc)
            else:
                container.listen_port = port
                return origin, log
        if time.monotonic() >= deadline:
            detail = f": {last_error}" if last_error else ""
            raise AcceptanceError(f"isolated host did not become ready{detail}")
        time.sleep(min(STARTUP_POLL_INTERVAL, max(0.0, deadline - time.monotonic())))


def runner_command(
    args: argparse.Namespace,
    origin: str,
    receipt: Path,
    game_id: str | None,
) -> list[str]:
    command = [
        sys.executable,
        str(PACKAGE_ACCEPTANCE),
        "--host-api",
        origin,
        "--archive",
        str(Path(args.archive).resolve()),
        "--expected-archive-sha256",
        args.expected_archive_sha256,
        "--expected-package-id",
        args.expected_package_id,
        "--expected-core-id",
        args.expected_core_id,
        "--expected-target-id",
        args.expected_target_id,
        "--expected-host-revision",
        args.expected_host_revision,
        "--expected-agent-revision",
        args.expected_agent_revision,
        "--expected-runtime-revision",
        args.expected_runtime_revision,
        "--receipt",
        str(receipt),
        "--poll-attempts",
        str(args.poll_attempts),
        "--poll-interval",
        str(args.poll_interval),
        "--timeout",
        str(args.timeout),
        "--execute",
    ]
    if game_id is None:
        command.extend(["--new-entry-title", args.new_entry_title])
    else:
        command.extend(["--game-id", game_id, "--expected-selected-package", args.expected_package_id])
    return command


def _mount(source: Path, destination: str, mode: str) -> str:
    source_value = str(source.resolve())
    if "," in source_value:
        raise AcceptanceError("isolated mount paths must not contain commas")
    suffix = f",{mode}" if mode else ""
    return f"type=bind,src={source_value},dst={destination}{suffix}"


def container_command(
    runtime: str,
    image: str,
    name: str,
    binary: Path,
    home: PrivateHome,
    listen_port: int,
) -> list[str]:
    return [
        runtime,
        "run",
        "--detach",
        "--name",
        name,
        "--pull=never",
        "--network=host",
        "--read-only",
        "--tmpfs",
        "/tmp:rw,nosuid,nodev,noexec",
        "--cap-drop=ALL",
        "--security-opt=no-new-privileges",
        "--mount",
        _mount(binary, "/opt/fes/fogcast-api", "ro"),
        "--mount",
        _mount(home.root, "/home/builder", ""),
        "--entrypoint",
        "/opt/fes/fogcast-api",
        image,
        "--config",
        "/home/builder/.config/fogcast/config.toml",
        "--listen",
        f"127.0.0.1:{listen_port}",
        "--headless",
    ]


class DockerRuntime:
    def __init__(self, executable: str, command_timeout: float):
        self.executable = executable
        self.command_timeout = command_timeout

    def run(self, arguments: list[str], timeout: float | None = None) -> subprocess.CompletedProcess[str]:
        try:
            return subprocess.run(
                [self.executable, *arguments],
                capture_output=True,
                text=True,
                timeout=timeout or self.command_timeout,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise AcceptanceError(f"container runtime command failed: {type(exc).__name__}") from exc

    def inspect_image(self, image: str) -> dict[str, Any]:
        result = self.run(["image", "inspect", "--format", "{{json .}}", image])
        if result.returncode != 0:
            raise AcceptanceError("container image is not available locally; refusing pull or build")
        try:
            value = json.loads(result.stdout)
        except json.JSONDecodeError as exc:
            raise AcceptanceError("container image inspection was not JSON") from exc
        if not isinstance(value, dict):
            raise AcceptanceError("container image inspection was not an object")
        return value

    def start(self, command: list[str], name: str, secrets_to_redact: tuple[str, ...] = ()) -> None:
        result = self.run(command[1:])
        if result.returncode != 0:
            detail = _safe_text(result.stderr, secrets_to_redact).strip()
            if len(detail) > 4096:
                detail = detail[:4096] + "..."
            suffix = f": {detail}" if detail else ""
            raise AcceptanceError(f"isolated host container failed to start{suffix}")

    def logs(self, name: str) -> str:
        result = self.run(["logs", name])
        if result.returncode != 0:
            raise AcceptanceError("cannot read the isolated host startup log")
        return result.stdout

    def inspect(self, name: str) -> dict[str, Any]:
        result = self.run(["inspect", name])
        if result.returncode != 0:
            raise AcceptanceError("cannot inspect the isolated host container")
        try:
            value = json.loads(result.stdout)
        except json.JSONDecodeError as exc:
            raise AcceptanceError("container inspection was not JSON") from exc
        if isinstance(value, list) and len(value) == 1:
            value = value[0]
        if not isinstance(value, dict):
            raise AcceptanceError("container inspection was not an object")
        return value

    def stop(self, name: str, timeout: float) -> dict[str, Any]:
        result = self.run(["stop", f"--time={math.ceil(timeout)}", name], timeout=timeout + 5)
        inspection = self.inspect(name)
        state = inspection.get("State", inspection)
        if not isinstance(state, dict):
            raise AcceptanceError("container inspection has no state")
        if result.returncode != 0:
            raise AcceptanceError("isolated host container shutdown failed")
        if state.get("Running") is True or state.get("ExitCode") != 0:
            raise AcceptanceError("isolated host container did not exit successfully")
        return {"exit_code": state.get("ExitCode"), "status": state.get("Status")}

    def remove(self, name: str, force: bool = False) -> None:
        arguments = ["rm"]
        if force:
            arguments.append("--force")
        arguments.append(name)
        result = self.run(arguments)
        if result.returncode != 0:
            raise AcceptanceError("isolated host container cleanup failed")


def _container_value(record: ContainerRecord) -> dict[str, Any]:
    return {
        "name": record.name,
        "cycle": record.cycle,
        "listen_port": record.listen_port,
        "exit_code": record.exit_code,
        "status": record.status,
        "stop_error": record.stop_error,
        "remove_error": record.remove_error,
        "forced_remove_error": record.forced_remove_error,
        "remove_attempts": record.remove_attempts,
        "removed": record.removed,
    }


def _identity_value(args: argparse.Namespace) -> dict[str, str]:
    return {
        "host": args.expected_host_revision,
        "agent": args.expected_agent_revision,
        "runtime": args.expected_runtime_revision,
    }


def _receipt_base(args: argparse.Namespace, success: bool) -> dict[str, Any]:
    return {
        "format": 1,
        "success": success,
        "mode": "isolated-lifecycle",
        "host_binary_sha256": args.expected_host_sha256,
        "container_image": args.container_image,
        "package_id": args.expected_package_id,
        "core_id": args.expected_core_id,
        "target_id": args.expected_target_id,
        "revisions": _identity_value(args),
        "host_revision_policy": "development-label" if args.allow_development_host else "strict",
    }


def _cycle_directory(evidence_dir: Path, cycle: str) -> Path:
    path = evidence_dir / cycle
    try:
        path.mkdir(mode=0o700)
        os.chmod(path, 0o700)
    except OSError as exc:
        raise AcceptanceError(f"cannot create evidence cycle directory: {exc}") from exc
    return path


def _write_runner_output(cycle_dir: Path, result: subprocess.CompletedProcess[str], target: PrivateTarget) -> None:
    secrets_to_redact = (target.agent,)
    _write_private_text(cycle_dir / "runner.stdout", _safe_text(result.stdout, secrets_to_redact))
    _write_private_text(cycle_dir / "runner.stderr", _safe_text(result.stderr, secrets_to_redact))


def _read_runner_receipt(path: Path, args: argparse.Namespace) -> str:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise AcceptanceError("package acceptance runner did not produce a valid receipt") from exc
    if not isinstance(value, dict) or value.get("success") is not True:
        raise AcceptanceError("package acceptance runner did not report success")
    if value.get("package_id") != args.expected_package_id or value.get("core_id") != args.expected_core_id:
        raise AcceptanceError("package acceptance runner receipt identity does not match the request")
    if value.get("target_id") != args.expected_target_id:
        raise AcceptanceError("package acceptance runner receipt target identity does not match the request")
    selection = value.get("selection")
    if not isinstance(selection, dict):
        raise AcceptanceError("package acceptance runner receipt has no selection")
    game_id = selection.get("game_id")
    return _require_string(game_id, "package acceptance runner game ID", 512)


class IsolatedAcceptance:
    def __init__(
        self,
        args: argparse.Namespace,
        target: PrivateTarget,
        runtime: DockerRuntime,
        evidence_dir: Path,
        home: PrivateHome,
        binary: Path,
    ):
        self.args = args
        self.target = target
        self.runtime = runtime
        self.evidence_dir = evidence_dir
        self.home = home
        self.binary = binary
        self.containers: list[ContainerRecord] = []

    def _start(self, cycle: str) -> tuple[ContainerRecord, str]:
        record = ContainerRecord(
            name=f"fes-package-acceptance-{secrets.token_hex(8)}",
            cycle=cycle,
        )
        self.containers.append(record)
        record.start_attempted = True
        command = container_command(
            self.runtime.executable,
            self.args.container_image,
            record.name,
            self.binary,
            self.home,
            self.args.listen_port,
        )
        self.runtime.start(command, record.name, (self.target.agent,))
        record.started = True
        origin, log = wait_for_host(self.runtime, record, self.args, self.target)
        cycle_dir = _cycle_directory(self.evidence_dir, cycle)
        _write_private_text(cycle_dir / "startup.log", _safe_text(log, (self.target.agent,)))
        return record, origin

    def _run_runner(self, cycle: str, origin: str, game_id: str | None) -> str:
        cycle_dir = self.evidence_dir / cycle
        receipt = cycle_dir / "receipt.json"
        command = runner_command(self.args, origin, receipt, game_id)
        try:
            result = subprocess.run(
                command,
                cwd=PACKAGE_ACCEPTANCE.parent.parent,
                capture_output=True,
                text=True,
                timeout=self.args.runner_timeout,
                check=False,
            )
        except subprocess.TimeoutExpired as exc:
            _write_private_text(
                cycle_dir / "runner.stdout",
                _safe_text(getattr(exc, "stdout", ""), (self.target.agent,)),
            )
            _write_private_text(
                cycle_dir / "runner.stderr",
                _safe_text(getattr(exc, "stderr", ""), (self.target.agent,)),
            )
            raise AcceptanceError("package acceptance runner timed out") from exc
        except OSError as exc:
            raise AcceptanceError(f"package acceptance runner failed to start: {type(exc).__name__}") from exc
        _write_runner_output(cycle_dir, result, self.target)
        if result.returncode != 0:
            raise AcceptanceError(f"package acceptance runner failed (exit {result.returncode})")
        return _read_runner_receipt(receipt, self.args)

    def _stop(self, record: ContainerRecord) -> None:
        if record.stop_attempted:
            raise AcceptanceError(f"container {record.name} shutdown was already attempted")
        record.stop_attempted = True
        try:
            value = self.runtime.stop(record.name, self.args.shutdown_timeout)
        except BaseException as exc:
            record.stop_error = _safe_text(exc)
            raise
        record.exit_code = value.get("exit_code")
        record.status = value.get("status")

    def _remove(self, record: ContainerRecord, force: bool = False) -> None:
        if record.remove_attempted:
            if record.remove_attempts >= 2:
                raise AcceptanceError(f"container {record.name} removal retry limit reached")
        record.remove_attempts += 1
        record.remove_attempted = True
        try:
            self.runtime.remove(record.name, force=force)
        except BaseException as exc:
            if force:
                record.forced_remove_error = _safe_text(exc)
            else:
                record.remove_error = _safe_text(exc)
            raise
        record.removed = True

    def _cycle(self, cycle: str, game_id: str | None) -> tuple[str, dict[str, Any]]:
        record, origin = self._start(cycle)
        selected_game_id = self._run_runner(cycle, origin, game_id)
        self._stop(record)
        self._remove(record)
        return selected_game_id, _container_value(record)

    def cleanup(self) -> list[str]:
        errors: list[str] = []
        for record in reversed(self.containers):
            if record.removed:
                continue
            if record.started and not record.stop_attempted:
                try:
                    self._stop(record)
                except BaseException as exc:
                    errors.append(f"{record.name} shutdown: {_safe_text(exc)}")
            if not record.remove_attempted:
                try:
                    self._remove(record, force=record.stop_error is not None or not record.started)
                except BaseException as exc:
                    errors.append(f"{record.name} removal: {_safe_text(exc)}")
            if not record.removed and record.remove_attempts < 2:
                try:
                    self._remove(record, force=True)
                except BaseException as exc:
                    errors.append(
                        f"{record.name} forced removal failed; residual container may remain: "
                        f"{_safe_text(exc)}"
                    )
        return errors


def _require_new_evidence_dir(path_value: Any) -> Path:
    path = Path(_require_string(path_value, "evidence directory", 4096))
    try:
        path.lstat()
    except FileNotFoundError:
        pass
    except OSError as exc:
        raise AcceptanceError(f"cannot inspect evidence directory: {exc}") from exc
    else:
        raise AcceptanceError("evidence directory must be a new path")
    if not path.parent.is_dir():
        raise AcceptanceError("evidence directory parent must exist")
    return path


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--host-binary", required=True)
    result.add_argument("--expected-host-sha256", required=True)
    result.add_argument("--container-image", required=True)
    result.add_argument("--host-config", required=True)
    result.add_argument("--evidence-dir", required=True)
    result.add_argument("--archive", required=True)
    result.add_argument("--expected-archive-sha256", required=True)
    result.add_argument("--expected-package-id", required=True)
    result.add_argument("--expected-core-id", required=True)
    result.add_argument("--expected-target-id", required=True)
    result.add_argument("--expected-host-revision", required=True)
    result.add_argument("--expected-agent-revision", required=True)
    result.add_argument(
        "--expected-runtime-revision",
        "--expected-runtime-commit",
        dest="expected_runtime_revision",
        required=True,
    )
    result.add_argument("--new-entry-title", required=True)
    result.add_argument("--container-runtime", default=os.environ.get("CONTAINER_RUNTIME", "docker"))
    result.add_argument("--listen-port", type=int, default=0)
    result.add_argument("--timeout", type=float, default=30.0)
    result.add_argument("--container-timeout", type=float, default=120.0)
    result.add_argument("--shutdown-timeout", type=float, default=20.0)
    result.add_argument("--runner-timeout", type=float, default=120.0)
    result.add_argument("--poll-attempts", type=int, default=60)
    result.add_argument("--poll-interval", type=float, default=1.0)
    result.add_argument("--allow-development-host", action="store_true")
    result.add_argument("--execute", action="store_true")
    return result


def validate_args(args: argparse.Namespace) -> PrivateTarget:
    if not args.execute:
        raise AcceptanceError("refusing isolated mutations without explicit --execute")
    if args.listen_port < 0 or args.listen_port > 65535:
        raise AcceptanceError("listen port must be between 0 and 65535")
    if not 0 < args.timeout <= MAX_TIMEOUT:
        raise AcceptanceError("timeout must be positive and bounded")
    if not 0 < args.container_timeout <= MAX_CONTAINER_TIMEOUT:
        raise AcceptanceError("container timeout must be positive and bounded")
    if not 0 < args.shutdown_timeout <= MAX_SHUTDOWN_TIMEOUT:
        raise AcceptanceError("shutdown timeout must be positive and bounded")
    if not 0 < args.runner_timeout <= MAX_CONTAINER_TIMEOUT:
        raise AcceptanceError("runner timeout must be positive and bounded")
    if not 0 < args.poll_attempts <= MAX_POLL_ATTEMPTS or not 0 <= args.poll_interval <= MAX_POLL_INTERVAL:
        raise AcceptanceError("poll attempts and interval must be bounded")
    _require_digest(args.expected_host_sha256, "expected host binary sha256")
    _require_digest(args.expected_archive_sha256, "expected archive sha256")
    _require_package_id(args.expected_package_id, "expected package ID")
    _require_core_id(args.expected_core_id)
    target_id = _require_string(args.expected_target_id, "expected target ID", 256)
    revisions = (
        ("expected host revision", args.expected_host_revision),
        ("expected agent revision", args.expected_agent_revision),
        ("expected runtime revision", args.expected_runtime_revision),
    )
    for name, value in revisions:
        _require_string(value, name)
    if COMMIT_RE.fullmatch(args.expected_agent_revision) is None:
        raise AcceptanceError("expected agent revision must be a full 40-character commit")
    if COMMIT_RE.fullmatch(args.expected_runtime_revision) is None:
        raise AcceptanceError("expected runtime revision must be a full 40-character commit")
    if not args.allow_development_host:
        if COMMIT_RE.fullmatch(args.expected_host_revision) is None:
            raise AcceptanceError("strict host acceptance requires a full 40-character host revision")
        if args.expected_host_revision != args.expected_agent_revision:
            raise AcceptanceError("strict host acceptance requires matching host and agent revisions")
    elif COMMIT_RE.fullmatch(args.expected_host_revision) is not None:
        if args.expected_host_revision != args.expected_agent_revision:
            raise AcceptanceError("development mode cannot compare unequal full host and agent commits")
    elif DEVELOPMENT_HOST_RE.fullmatch(args.expected_host_revision) is None:
        raise AcceptanceError("development host revision must be a diag-/dev- labelled value")
    _require_string(args.new_entry_title, "new entry title")
    _require_string(args.container_runtime, "container runtime", 4096)

    binary = _require_file(args.host_binary, "host binary")
    binary_mode = binary.stat().st_mode
    if binary_mode & 0o111 == 0 or binary_mode & 0o022:
        raise AcceptanceError("host binary must be executable and not group/world writable")
    _, binary_digest = _read_digest(binary, "host binary")
    if binary_digest != args.expected_host_sha256:
        raise AcceptanceError(
            f"host binary sha256 mismatch: expected {args.expected_host_sha256}, observed {binary_digest}"
        )
    archive = _require_file(args.archive, "archive")
    _, archive_digest = _read_digest(archive, "archive", MAX_ARCHIVE_BYTES)
    if archive_digest != args.expected_archive_sha256:
        raise AcceptanceError(
            f"archive sha256 mismatch: expected {args.expected_archive_sha256}, observed {archive_digest}"
        )
    image = _require_string(args.container_image, "container image", 128)
    if IMAGE_RE.fullmatch(image) is None:
        raise AcceptanceError("container image must be an immutable sha256 image ID")
    config = _require_file(args.host_config, "host config")
    target = _load_private_target(config, target_id)
    _require_new_evidence_dir(args.evidence_dir)
    if not PACKAGE_ACCEPTANCE.is_file():
        raise AcceptanceError("existing package acceptance runner is missing")
    return target


def _create_evidence_dir(path: Path) -> None:
    try:
        path.mkdir(mode=0o700)
        os.chmod(path, 0o700)
    except OSError as exc:
        raise AcceptanceError(f"cannot create evidence directory: {exc}") from exc


def _write_failure(
    evidence_dir: Path,
    args: argparse.Namespace,
    cycles: list[dict[str, Any]],
    containers: list[ContainerRecord],
    primary: BaseException,
    cleanup_errors: list[str],
) -> None:
    value = _receipt_base(args, False)
    value.update(
        {
            "error": _safe_text(primary),
            "cleanup_errors": cleanup_errors,
            "cycles": cycles,
            "containers": [_container_value(record) for record in containers],
            "created_at_utc": datetime.now(timezone.utc).isoformat(),
        }
    )
    _write_json_new(evidence_dir / "failure.json", value)


def execute_isolated(args: argparse.Namespace, target: PrivateTarget) -> None:
    runtime = DockerRuntime(args.container_runtime, args.container_timeout)
    validate_image_metadata(args.container_image, runtime.inspect_image(args.container_image))
    _require_port_available(args.listen_port)

    evidence_dir = Path(args.evidence_dir)
    _create_evidence_dir(evidence_dir)
    home: PrivateHome | None = None
    acceptance: IsolatedAcceptance | None = None
    cycles: list[dict[str, Any]] = []
    try:
        binary = snapshot_binary(Path(args.host_binary), evidence_dir, args.expected_host_sha256)
        home = prepare_private_home(evidence_dir, target)
        acceptance = IsolatedAcceptance(args, target, runtime, evidence_dir, home, binary)
        first_game_id, first_container = acceptance._cycle("cycle-1", None)
        cycles.append({"name": "initial", "game_id": first_game_id, "container": first_container})
        second_game_id, second_container = acceptance._cycle("cycle-2", first_game_id)
        if second_game_id != first_game_id:
            raise AcceptanceError("restart selected a different persisted library entry")
        cycles.append({"name": "restart", "game_id": second_game_id, "container": second_container})
        remove_private_config(home)
        receipt = _receipt_base(args, True)
        receipt.update(
            {
                "cycles": cycles,
                "created_at_utc": datetime.now(timezone.utc).isoformat(),
            }
        )
        _write_json_new(evidence_dir / "receipt.json", receipt)
    except BaseException as primary:
        cleanup_errors = acceptance.cleanup() if acceptance is not None else []
        if home is not None:
            try:
                remove_private_config(home)
            except BaseException as cleanup_error:
                cleanup_errors.append(f"private credential cleanup: {_safe_text(cleanup_error)}")
        else:
            try:
                remove_unreturned_private_config(evidence_dir, target)
            except BaseException as cleanup_error:
                cleanup_errors.append(f"private credential cleanup: {_safe_text(cleanup_error)}")
        try:
            _write_failure(
                evidence_dir,
                args,
                cycles,
                acceptance.containers if acceptance is not None else [],
                primary,
                cleanup_errors,
            )
        except BaseException as report_error:
            cleanup_errors.append(f"failure evidence: {_safe_text(report_error)}")
        raise
    print(
        f"isolated package acceptance passed: core={args.expected_core_id} "
        f"package={args.expected_package_id} game={cycles[0]['game_id']} "
        f"evidence={evidence_dir} (lifecycle-only; no media/input diagnostics)"
    )


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    previous_signals: dict[int, Any] = {}
    try:
        target = validate_args(args)
        if args.execute:
            for signum in (signal.SIGINT, signal.SIGTERM):
                previous_signals[signum] = signal.signal(signum, _interrupt)
            try:
                execute_isolated(args, target)
            finally:
                for signum, handler in previous_signals.items():
                    signal.signal(signum, handler)
    except AcceptanceError as exc:
        print(f"isolated package acceptance: {exc}", file=sys.stderr)
        return 2 if not args.execute else 1
    except BaseException as exc:
        print(f"isolated package acceptance: {_safe_text(exc)}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
