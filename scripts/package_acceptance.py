#!/usr/bin/env python3
"""Run an explicitly opted-in, package-only FES acceptance lane.

This lane exercises host package import, target compatibility observation,
explicit catalog selection, one library launch, and cleanup of that launch.
It never reads image selection files and does not infer media or input tests
from a core name.  Stop is an unconditional host operation, so the operator
must provide exclusive ownership of the host session for the whole run.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import stat
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


PACKAGE_ID_RE = re.compile(r"^[0-9a-f]{64}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
CORE_ID_RE = re.compile(r"^[a-z][a-z0-9_.-]{0,95}$")
GAME_ID_RE = re.compile(r"^[a-z0-9][a-z0-9_.-]{0,127}$")
REVISION_MAX = 256
MAX_ARCHIVE_BYTES = 33 * 1024 * 1024
EXPECTED_LAUNCH_EXECUTION = "fpga_development"
HTTP_RESPONSE_BODY_LIMIT = 1 << 20
HTTP_ERROR_BODY_LIMIT = 16 * 1024
CLI_NOTE_LIMIT = 1024


class AcceptanceError(RuntimeError):
    """A bounded, operator-facing acceptance failure."""


def _safe_error_field(value: Any, limit: int = HTTP_ERROR_BODY_LIMIT) -> str:
    if not isinstance(value, str):
        return ""
    value = " ".join(value.split())
    if len(value) > limit:
        return value[:limit] + "..."
    return value


def _http_error_detail(error: urllib.error.HTTPError) -> str:
    try:
        body = error.read(HTTP_ERROR_BODY_LIMIT + 1)
    except Exception:
        body = b""
    finally:
        error.close()

    try:
        value = json.loads(body)
    except (UnicodeDecodeError, json.JSONDecodeError):
        value = None
    details = value.get("error") if isinstance(value, dict) else None
    if isinstance(details, dict):
        code = _safe_error_field(details.get("code"))
        phase = _safe_error_field(details.get("phase"))
        message = _safe_error_field(details.get("message"))
        result = f"HTTP {error.code}"
        if code:
            result += f" {code}"
        if phase:
            result += f" phase={phase}"
        if message:
            result += f": {message}"
        return result
    reason = _safe_error_field(error.reason)
    return f"HTTP {error.code}" + (f": {reason}" if reason else "")


class Api:
    """Small JSON/bytes client for the existing public host API."""

    def __init__(self, origin: str, timeout: float):
        self.origin = origin.rstrip("/")
        self.timeout = timeout

    def request(
        self,
        method: str,
        path: str,
        body: bytes | None = None,
        headers: dict[str, str] | None = None,
    ) -> Any:
        request = urllib.request.Request(
            self.origin + path,
            data=body,
            headers=headers or {},
            method=method,
        )
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                raw = response.read(HTTP_RESPONSE_BODY_LIMIT + 1)
        except urllib.error.HTTPError as exc:
            raise AcceptanceError(f"{method} {path}: {_http_error_detail(exc)}") from exc
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            detail = getattr(exc, "reason", exc)
            raise AcceptanceError(f"{method} {path}: {_safe_error_field(detail)}") from exc
        if len(raw) > HTTP_RESPONSE_BODY_LIMIT:
            raise AcceptanceError(f"{method} {path}: response exceeded the bounded size")
        if not raw:
            return {}
        try:
            return json.loads(raw)
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise AcceptanceError(f"{method} {path}: response was not JSON") from exc

    def get(self, path: str) -> Any:
        return self.request("GET", path)

    def post_empty(self, path: str) -> Any:
        return self.request("POST", path, headers={"Accept": "application/json"})

    def post_json(self, path: str, value: Any) -> Any:
        return self.request(
            "POST",
            path,
            json.dumps(value, separators=(",", ":")).encode("utf-8"),
            {"Content-Type": "application/json", "Accept": "application/json"},
        )

    def put_json(self, path: str, value: Any) -> Any:
        return self.request(
            "PUT",
            path,
            json.dumps(value, separators=(",", ":")).encode("utf-8"),
            {"Content-Type": "application/json", "Accept": "application/json"},
        )

    def post_bytes(self, path: str, body: bytes) -> Any:
        return self.request(
            "POST",
            path,
            body,
            {"Content-Type": "application/octet-stream", "Accept": "application/json"},
        )


@dataclass(frozen=True)
class SessionIdentity:
    session_id: str
    target_id: str
    flight_id: str
    game_id: str
    package_id: str
    generation: int


def _require_string(value: Any, name: str, maximum: int = REVISION_MAX) -> str:
    if not isinstance(value, str) or not value or len(value) > maximum or any(ord(char) < 32 for char in value):
        raise AcceptanceError(f"{name} must be a bounded non-empty string")
    return value


def _require_package_id(value: Any, name: str) -> str:
    if not isinstance(value, str) or PACKAGE_ID_RE.fullmatch(value) is None:
        raise AcceptanceError(f"{name} must be 64 lowercase hexadecimal characters")
    return value


def _require_sha256(value: Any, name: str) -> str:
    if not isinstance(value, str) or SHA256_RE.fullmatch(value) is None:
        raise AcceptanceError(f"{name} must be 64 lowercase hexadecimal characters")
    return value


def _require_core_id(value: Any) -> str:
    if not isinstance(value, str) or CORE_ID_RE.fullmatch(value) is None:
        raise AcceptanceError("expected core ID is invalid")
    return value


def _require_game_id(value: Any, name: str = "game ID") -> str:
    if not isinstance(value, str) or GAME_ID_RE.fullmatch(value) is None:
        raise AcceptanceError(f"{name} is invalid")
    return value


def _descriptor_core_id(value: Any, context: str) -> str:
    if not isinstance(value, dict):
        raise AcceptanceError(f"{context}: response descriptor is missing")
    descriptor = value.get("descriptor")
    core = descriptor.get("core") if isinstance(descriptor, dict) else None
    core_id = core.get("id") if isinstance(core, dict) else None
    if not isinstance(core_id, str):
        raise AcceptanceError(f"{context}: response descriptor core ID is missing")
    return core_id


def _present(value: Any) -> bool:
    return value not in (None, "", {}, [])


def _require_clean_idle(value: Any, context: str) -> dict[str, Any]:
    if not isinstance(value, dict) or value.get("state") != "idle":
        raise AcceptanceError(f"{context}: session is not idle")
    for field in ("game_id", "system", "error", "recovery", "core_package"):
        if _present(value.get(field)):
            raise AcceptanceError(f"{context}: idle session retains {field}")
    return value


def _require_session_target(
    value: Any, expected_target_id: str, context: str
) -> dict[str, Any]:
    observed_target_id = value.get("target_id") if isinstance(value, dict) else None
    if observed_target_id != expected_target_id:
        raise AcceptanceError(
            f"{context}: target_id does not match expected target "
            f"{expected_target_id!r}; observed {observed_target_id!r}"
        )
    return value


def _session_identity(value: Any, context: str) -> SessionIdentity:
    if not isinstance(value, dict) or value.get("state") != "active":
        raise AcceptanceError(f"{context}: session is not an identifiable active session")
    if value.get("execution") != EXPECTED_LAUNCH_EXECUTION:
        raise AcceptanceError(f"{context}: session execution is not fpga_development")
    package = value.get("core_package")
    if not isinstance(package, dict):
        raise AcceptanceError(f"{context}: active session has no package identity")
    generation = package.get("generation")
    if isinstance(generation, bool) or not isinstance(generation, int) or generation <= 0:
        raise AcceptanceError(f"{context}: active session has no valid package generation")
    return SessionIdentity(
        session_id=_require_string(value.get("id"), f"{context} session ID"),
        target_id=_require_string(value.get("target_id"), f"{context} target_id"),
        flight_id=_require_string(value.get("flight_id"), f"{context} flight ID"),
        game_id=_require_game_id(value.get("game_id"), f"{context} game ID"),
        package_id=_require_package_id(package.get("package_id"), f"{context} package ID"),
        generation=generation,
    )


def _same_identity(left: SessionIdentity, right: SessionIdentity) -> bool:
    return left == right


def _read_archive(path: Path) -> tuple[bytes, str]:
    try:
        info = path.lstat()
    except OSError as exc:
        raise AcceptanceError(f"cannot read archive {path}: {exc}") from exc
    if not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode):
        raise AcceptanceError(f"archive is not a regular non-symlink file: {path}")
    if info.st_size < 1 or info.st_size > MAX_ARCHIVE_BYTES:
        raise AcceptanceError(f"archive size is outside the bounded range: {path}")
    try:
        with path.open("rb") as handle:
            data = handle.read(MAX_ARCHIVE_BYTES + 1)
    except OSError as exc:
        raise AcceptanceError(f"cannot read archive {path}: {exc}") from exc
    if len(data) != info.st_size or len(data) > MAX_ARCHIVE_BYTES:
        raise AcceptanceError(f"archive changed or exceeded the bounded size: {path}")
    return data, hashlib.sha256(data).hexdigest()


class Runner:
    def __init__(self, args: argparse.Namespace):
        self.args = args
        self.api = Api(args.host_api, args.timeout)
        self.archive_path = Path(args.archive)
        self.receipt_path = Path(args.receipt)
        self.core_id = args.expected_core_id
        self.target_id = args.expected_target_id
        self.package_id = args.expected_package_id
        self.expected_selected_package = args.expected_selected_package
        self.archive: bytes = b""
        self.archive_sha256 = ""
        self.last_revisions: dict[str, str] = {}
        self.owned: SessionIdentity | None = None
        self.stop_attempted = False

    def _require_receipt_absent(self) -> None:
        try:
            info = self.receipt_path.lstat()
        except FileNotFoundError:
            info = None
        except OSError as exc:
            raise AcceptanceError(f"cannot inspect receipt path {self.receipt_path}: {exc}") from exc
        if info is not None:
            raise AcceptanceError(f"receipt already exists; refusing to overwrite: {self.receipt_path}")
        parent = self.receipt_path.parent
        if not parent.is_dir():
            raise AcceptanceError(f"receipt parent directory does not exist: {parent}")

    def check_health(self) -> dict[str, Any]:
        value = self.api.get("/api/v1/health")
        if not isinstance(value, dict) or value.get("ready") is not True:
            raise AcceptanceError("host is not ready")
        target = value.get("target")
        if not isinstance(target, dict) or target.get("reachable") is not True or target.get("ready") is not True:
            raise AcceptanceError("target is not ready through the host")
        connection = target.get("connection")
        observed_target_id = connection.get("target_id") if isinstance(connection, dict) else None
        if observed_target_id != self.target_id:
            raise AcceptanceError(
                f"target_id does not match expected target {self.target_id!r}; "
                f"observed {observed_target_id!r}"
            )
        artifacts = target.get("artifacts")
        if not isinstance(artifacts, dict):
            raise AcceptanceError("target health has no sealed artifact identity")
        actual = {
            "host": (value.get("host") or {}).get("revision"),
            "agent": artifacts.get("agent_revision"),
            "runtime": artifacts.get("runtime_commit"),
        }
        expected = {
            "host": self.args.expected_host_revision,
            "agent": self.args.expected_agent_revision,
            "runtime": self.args.expected_runtime_revision,
        }
        for name in ("host", "agent", "runtime"):
            if actual[name] != expected[name]:
                raise AcceptanceError(
                    f"{name} revision changed: expected {expected[name]!r}, observed {actual[name]!r}"
                )
        self.last_revisions = actual
        return value

    def session(self) -> dict[str, Any]:
        value = self.api.get("/api/v1/session")
        if not isinstance(value, dict):
            raise AcceptanceError("session response is not an object")
        return value

    def session_identity(self, value: Any, context: str) -> SessionIdentity:
        identity = _session_identity(value, context)
        if identity.target_id != self.target_id:
            raise AcceptanceError(
                f"{context}: target_id does not match expected target "
                f"{self.target_id!r}; observed {identity.target_id!r}"
            )
        return identity

    def require_idle(self, context: str) -> dict[str, Any]:
        return _require_session_target(
            _require_clean_idle(self.session(), context), self.target_id, context
        )

    def guard_mutation(self, context: str) -> None:
        self.check_health()
        self.require_idle(f"before {context}")

    def entry_path(self, game_id: str) -> str:
        return "/api/v1/library/core-entries/" + urllib.parse.quote(game_id, safe="")

    def read_entry(self, game_id: str) -> dict[str, Any]:
        value = self.api.get(self.entry_path(game_id))
        if not isinstance(value, dict):
            raise AcceptanceError("library entry response is not an object")
        return value

    def validate_entry(self, value: Any, game_id: str, context: str) -> dict[str, Any]:
        if not isinstance(value, dict):
            raise AcceptanceError(f"{context}: library entry response is not an object")
        if value.get("game_id") != game_id or value.get("core_id") != self.core_id or value.get("package_id") != self.package_id:
            raise AcceptanceError(f"{context}: library entry identity does not match the requested package")
        return value

    def validate_import(self, value: Any) -> dict[str, Any]:
        if not isinstance(value, dict) or value.get("package_id") != self.package_id:
            raise AcceptanceError(
                f"package identity changed during import: expected {self.package_id}, observed {value.get('package_id') if isinstance(value, dict) else None!r}"
            )
        if _descriptor_core_id(value, "package import") != self.core_id:
            raise AcceptanceError("package import descriptor core ID does not match the requested core")
        return value

    def validate_compatibility(self, value: Any) -> dict[str, Any]:
        if not isinstance(value, dict) or value.get("package_id") != self.package_id:
            raise AcceptanceError("compatibility response package identity does not match the requested package")
        if value.get("target_id") != self.target_id:
            raise AcceptanceError(
                f"compatibility target_id does not match expected target {self.target_id!r}; "
                f"observed {value.get('target_id')!r}"
            )
        if _descriptor_core_id(value, "compatibility") != self.core_id:
            raise AcceptanceError("compatibility descriptor core ID does not match the requested core")
        if value.get("compatible") is not True or value.get("state") != "compatible":
            raise AcceptanceError("package is not compatible with the current target")
        if value.get("compatibility_error") is not None:
            raise AcceptanceError("compatible response contains a compatibility error")
        return value

    def wait_for_owned_active(self, expected: SessionIdentity) -> dict[str, Any]:
        last_error = ""
        for attempt in range(self.args.poll_attempts):
            try:
                value = self.session()
                observed = self.session_identity(value, "launch confirmation")
                if not _same_identity(observed, expected):
                    raise AcceptanceError("launch session identity changed before cleanup")
                return value
            except AcceptanceError as exc:
                last_error = str(exc)
            if attempt + 1 < self.args.poll_attempts:
                time.sleep(self.args.poll_interval)
        raise AcceptanceError(f"owned launch was not confirmed: {last_error}")

    def wait_for_clean_idle(self, context: str) -> dict[str, Any]:
        last_error = ""
        for attempt in range(self.args.poll_attempts):
            try:
                return _require_session_target(
                    _require_clean_idle(self.session(), context), self.target_id, context
                )
            except AcceptanceError as exc:
                last_error = str(exc)
            if attempt + 1 < self.args.poll_attempts:
                time.sleep(self.args.poll_interval)
        raise AcceptanceError(f"{context} was not confirmed: {last_error}")

    def stop_owned(self, expected: SessionIdentity) -> dict[str, Any]:
        if self.stop_attempted:
            raise AcceptanceError("Stop was already attempted")
        self.stop_attempted = True
        current = self.session_identity(self.session(), "pre-Stop")
        if not _same_identity(current, expected):
            raise AcceptanceError(
                "owned session identity changed before Stop; refusing the unconditional Stop operation"
            )
        response = self.api.post_empty("/api/v1/session/stop")
        _require_clean_idle(response, "Stop response")
        _require_session_target(response, expected.target_id, "Stop response")
        response_id = response.get("id") if isinstance(response, dict) else None
        if response_id != expected.session_id:
            raise AcceptanceError("Stop response session ID does not match the owned host session")
        confirmed = self.wait_for_clean_idle("Stop")
        if confirmed.get("id") != expected.session_id:
            raise AcceptanceError("Stop confirmation session ID does not match the owned host session")
        return {"response": response, "confirmed": confirmed}

    def observe_ambiguous_launch(self, operation_error: AcceptanceError) -> None:
        try:
            observed = self.session()
            operation_error.add_note(
                f"post-launch session observation: state={observed.get('state')!r}"
            )
        except AcceptanceError as observation_error:
            operation_error.add_note(f"post-launch session observation failed: {observation_error}")

    def write_receipt(self, value: dict[str, Any]) -> None:
        try:
            with self.receipt_path.open("x", encoding="utf-8") as handle:
                json.dump(value, handle, indent=2, sort_keys=True)
                handle.write("\n")
        except FileExistsError as exc:
            raise AcceptanceError(f"receipt appeared during the run; refusing to overwrite: {self.receipt_path}") from exc
        except OSError as exc:
            raise AcceptanceError(f"cannot write receipt {self.receipt_path}: {exc}") from exc

    def run(self) -> None:
        self._require_receipt_absent()
        self.archive, self.archive_sha256 = _read_archive(self.archive_path)
        if self.archive_sha256 != self.args.expected_archive_sha256:
            raise AcceptanceError(
                f"archive sha256 changed: expected {self.args.expected_archive_sha256}, observed {self.archive_sha256}"
            )

        self.check_health()
        self.require_idle("preflight")
        game_id: str
        if self.args.game_id:
            game_id = self.args.game_id
            current = self.read_entry(game_id)
            if current.get("core_id") != self.core_id or current.get("package_id") != self.expected_selected_package:
                raise AcceptanceError("preflight selection does not match the expected current package")

        self.guard_mutation("package import")
        self.validate_import(self.api.post_bytes("/api/v1/core-packages", self.archive))

        self.guard_mutation("compatibility check")
        compatibility = self.validate_compatibility(
            self.api.post_empty(f"/api/v1/core-packages/{self.package_id}/compatibility")
        )

        if self.args.game_id:
            self.guard_mutation("selection")
            current = self.read_entry(self.args.game_id)
            if current.get("core_id") != self.core_id or current.get("package_id") != self.expected_selected_package:
                raise AcceptanceError("selection changed before compare-and-swap")
            self.validate_entry(self.api.put_json(
                self.entry_path(self.args.game_id),
                {"package_id": self.package_id, "expected_package_id": self.expected_selected_package},
            ), self.args.game_id, "selection")
            game_id = self.args.game_id
        else:
            self.guard_mutation("new library entry")
            selected = self.api.post_json(
                "/api/v1/library/core-entries",
                {"title": self.args.new_entry_title, "package_id": self.package_id},
            )
            if not isinstance(selected, dict):
                raise AcceptanceError("new library entry response is not an object")
            game_id = _require_game_id(selected.get("game_id"), "new library entry game ID")
            selected = self.validate_entry(selected, game_id, "new library entry")

        self.guard_mutation("launch")
        selected_now = self.validate_entry(self.read_entry(game_id), game_id, "pre-launch selection")
        try:
            launch = self.api.post_json("/api/v1/session/launch", {"game_id": game_id})
            expected = self.session_identity(launch, "launch response")
            if expected.package_id != self.package_id:
                raise AcceptanceError("launch response selected a different package")
            if expected.game_id != game_id:
                raise AcceptanceError("launch response selected a different game")
        except AcceptanceError as launch_error:
            self.observe_ambiguous_launch(launch_error)
            raise
        self.owned = expected
        try:
            self.wait_for_owned_active(expected)
            stop = self.stop_owned(expected)
        except BaseException as operation_error:
            if self.owned is not None and not self.stop_attempted:
                try:
                    self.stop_owned(self.owned)
                except BaseException as cleanup_error:
                    operation_error.add_note(f"owned-session cleanup failed: {cleanup_error}")
                else:
                    operation_error.add_note("owned-session cleanup completed after launch confirmation failure")
            raise
        self.check_health()

        receipt = {
            "format": 1,
            "success": True,
            "mode": "lifecycle-only",
            "diagnostics": [],
            "archive_path": str(self.archive_path),
            "archive_bytes": len(self.archive),
            "archive_sha256": self.archive_sha256,
            "package_id": self.package_id,
            "core_id": self.core_id,
            "target_id": self.target_id,
            "revisions": dict(self.last_revisions),
            "selection": {
                "game_id": game_id,
                "package_id": selected_now.get("package_id", self.package_id),
                "mode": "existing" if self.args.game_id else "new",
            },
            "compatibility": {
                "package_id": compatibility.get("package_id"),
                "state": compatibility.get("state"),
                "compatible": compatibility.get("compatible"),
            },
            "session": {
                "id": expected.session_id,
                "target_id": expected.target_id,
                "flight_id": expected.flight_id,
                "game_id": expected.game_id,
                "package_id": expected.package_id,
                "generation": expected.generation,
            },
            "stop": stop["confirmed"],
            "created_at_utc": datetime.now(timezone.utc).isoformat(),
        }
        self.write_receipt(receipt)
        print(
            f"package acceptance passed: core={self.core_id} package={self.package_id} "
            f"game={game_id} receipt={self.receipt_path} (lifecycle-only; no media/input diagnostics)"
        )


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--host-api", default=os.environ.get("FOGCAST_HOST_API", "http://127.0.0.1:8787"))
    result.add_argument("--archive", required=True, help="sealed .fcore archive bytes to import")
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
    result.add_argument("--receipt", required=True, help="new JSON receipt path; existing files are refused")
    selection = result.add_mutually_exclusive_group(required=True)
    selection.add_argument("--game-id", help="existing library entry to update with compare-and-swap")
    selection.add_argument("--new-entry-title", help="create a new explicit library entry after admission")
    result.add_argument("--expected-selected-package", help="required with --game-id")
    result.add_argument("--execute", action="store_true", help="required opt-in for session mutations")
    result.add_argument("--timeout", type=float, default=float(os.environ.get("FES_ACCEPTANCE_TIMEOUT", "30")))
    result.add_argument("--poll-attempts", type=int, default=int(os.environ.get("FES_ACCEPTANCE_POLL_ATTEMPTS", "60")))
    result.add_argument("--poll-interval", type=float, default=float(os.environ.get("FES_ACCEPTANCE_POLL_INTERVAL", "1")))
    return result


def validate_args(args: argparse.Namespace) -> None:
    if not args.execute:
        raise AcceptanceError("refusing session mutations without explicit --execute")
    if args.timeout <= 0 or args.poll_attempts <= 0 or args.poll_interval < 0:
        raise AcceptanceError("timeout, poll attempts, and poll interval must be valid")
    _require_sha256(args.expected_archive_sha256, "expected archive sha256")
    _require_package_id(args.expected_package_id, "expected package ID")
    _require_core_id(args.expected_core_id)
    for name, value in (
        ("expected target ID", args.expected_target_id),
        ("expected host revision", args.expected_host_revision),
        ("expected agent revision", args.expected_agent_revision),
        ("expected runtime revision", args.expected_runtime_revision),
    ):
        _require_string(value, name)
    if args.game_id:
        _require_game_id(args.game_id)
        _require_package_id(args.expected_selected_package, "expected selected package")
    elif args.expected_selected_package is not None:
        raise AcceptanceError("--expected-selected-package requires --game-id")
    if args.new_entry_title is not None and not args.new_entry_title.strip():
        raise AcceptanceError("new entry title must not be empty")


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    try:
        validate_args(args)
        Runner(args).run()
    except AcceptanceError as exc:
        print(f"package acceptance: {exc}", file=sys.stderr)
        for note in getattr(exc, "__notes__", ()):
            detail = _safe_error_field(note, CLI_NOTE_LIMIT)
            if detail:
                print(f"package acceptance: {detail}", file=sys.stderr)
        return 2 if not args.execute else 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
