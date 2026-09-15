#!/usr/bin/env python3
"""Exercise the exact FES package set on a real target.

This is an operator-facing hardware lane.  It proves host/target readiness,
the exact package identities selected by FES, persistent library launch,
remote input delivery, optional development-media delivery, and clean Stop.
When a capture directory is supplied it also records one HDMI frame and its
digest per core.  The digest is evidence of the captured artifact; visual
meaning remains an operator review rather than an automated equivalence claim.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


CORE_ORDER = ("fes.pong", "fes.zx81", "fes.coleco")
SELECTION_NAMES = {
    "fes.pong": "fes-pong.package-selection.toml",
    "fes.zx81": "fes-zx81.package-selection.toml",
    "fes.coleco": "fes-coleco.package-selection.toml",
}
PACKAGE_ID_RE = re.compile(r"^[0-9a-f]{64}$")


class AcceptanceError(RuntimeError):
    pass


class Api:
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
                raw = response.read()
        except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError) as exc:
            detail = getattr(exc, "reason", exc)
            raise AcceptanceError(f"{method} {path}: {detail}") from exc
        if not raw:
            return {}
        try:
            return json.loads(raw)
        except json.JSONDecodeError as exc:
            raise AcceptanceError(f"{method} {path}: response was not JSON") from exc

    def get(self, path: str) -> Any:
        return self.request("GET", path)

    def post_json(self, path: str, value: Any) -> Any:
        return self.request(
            "POST",
            path,
            json.dumps(value, separators=(",", ":")).encode("utf-8"),
            {"Content-Type": "application/json", "Accept": "application/json"},
        )

    def post_empty(self, path: str) -> Any:
        return self.request("POST", path, headers={"Accept": "application/json"})

    def post_bytes(self, path: str, body: bytes, headers: dict[str, str]) -> Any:
        request_headers = {"Content-Type": "application/octet-stream", **headers}
        return self.request("POST", path, body, request_headers)


@dataclass(frozen=True)
class CoreSpec:
    core_id: str
    events: tuple[dict[str, Any], ...]


def event(device: int, kind: int, action: int, code: int, value: int = 0) -> dict[str, Any]:
    return {
        "event": {
            "device": device,
            "kind": kind,
            "action": action,
            "code": code,
            "value": value,
        }
    }


def capture_command(video_device: str, capture_size: str, capture_frames: int, output: Path) -> list[str]:
    if capture_frames < 1:
        raise ValueError("capture_frames must be positive")
    # V4L2 devices can return a buffered frame from the previous core.  Read
    # several frames and keep the last one so the digest belongs to the
    # currently exercised session rather than the transition into it.
    selector = f"select=eq(n\\,{capture_frames - 1})"
    return [
        "ffmpeg",
        "-hide_banner",
        "-loglevel",
        "error",
        "-y",
        "-f",
        "v4l2",
        "-input_format",
        "mjpeg",
        "-video_size",
        capture_size,
        "-i",
        video_device,
        "-vf",
        selector,
        "-frames:v",
        "1",
        str(output),
    ]


CORE_SPECS = (
    CoreSpec(
        "fes.pong",
        (
            event(1, 1, 1, 104),
            event(1, 1, 0, 104),
            event(1, 1, 1, 100),
            event(1, 1, 0, 100),
        ),
    ),
    CoreSpec(
        "fes.zx81",
        (
            event(0, 0, 1, 275),
            event(0, 0, 0, 275),
            event(0, 0, 1, 257),
            event(0, 0, 0, 257),
        ),
    ),
    CoreSpec(
        "fes.coleco",
        (
            event(1, 1, 1, 104),
            event(1, 1, 0, 104),
            event(1, 1, 1, 100),
            event(1, 1, 0, 100),
        ),
    ),
)


def package_id_from_selection(path: Path) -> str:
    try:
        import tomllib

        with path.open("rb") as handle:
            value = tomllib.load(handle)
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise AcceptanceError(f"cannot read selection {path}: {exc}") from exc
    package_id = value.get("package_id")
    if not isinstance(package_id, str) or not PACKAGE_ID_RE.fullmatch(package_id):
        raise AcceptanceError(f"selection {path} has no valid package_id")
    return package_id


def parse_media(values: list[str]) -> dict[str, Path]:
    result: dict[str, Path] = {}
    for value in values:
        core, separator, filename = value.partition("=")
        if not separator or core not in CORE_ORDER or not filename:
            raise AcceptanceError(f"media must be CORE=PATH for a supported core: {value}")
        if core in result:
            raise AcceptanceError(f"media supplied more than once for {core}")
        path = Path(filename)
        if not path.is_file() or path.is_symlink():
            raise AcceptanceError(f"media is not a regular file: {path}")
        result[core] = path
    return result


class Runner:
    def __init__(self, args: argparse.Namespace):
        self.host = Api(args.host_api, args.timeout)
        self.target = Api(args.target_api, args.timeout)
        self.selection_dir = Path(args.selection_dir)
        self.poll_attempts = args.poll_attempts
        self.poll_interval = args.poll_interval
        self.media = parse_media(args.media)
        self.capture_dir = Path(args.capture_dir) if args.capture_dir else None
        self.video_device = args.video_device
        self.capture_size = args.capture_size
        self.capture_frames = args.capture_frames
        self.records: list[dict[str, Any]] = []
        self.active = False

    def selection_ids(self) -> dict[str, str]:
        return {
            core: package_id_from_selection(self.selection_dir / filename)
            for core, filename in SELECTION_NAMES.items()
        }

    def check_health(self) -> None:
        host = self.host.get("/api/v1/health")
        if host.get("ready") is not True:
            raise AcceptanceError("host is not ready")
        target = host.get("target") or {}
        if target.get("reachable") is not True or target.get("ready") is not True:
            raise AcceptanceError("target is not ready through the host")
        host_revision = (host.get("host") or {}).get("revision")
        agent_revision = (target.get("artifacts") or {}).get("agent_revision")
        if not host_revision or host_revision != agent_revision:
            raise AcceptanceError("host and target agent revisions do not match")
        direct = self.target.get("/v1/health")
        if direct.get("ready") is not True:
            raise AcceptanceError("direct target health is not ready")

    def inventory(self, expected: dict[str, str]) -> dict[str, str]:
        packages = self.host.get("/api/v1/core-packages").get("packages", [])
        entries = self.host.get("/api/v1/library/core-entries").get("entries", [])
        games: dict[str, str] = {}
        for core in CORE_ORDER:
            package_matches = [
                package
                for package in packages
                if package.get("package_id") == expected[core]
                and ((package.get("descriptor") or {}).get("core") or {}).get("id") == core
            ]
            if len(package_matches) != 1:
                raise AcceptanceError(f"{core}: expected exact installed package {expected[core]}")
            entry_matches = [
                entry
                for entry in entries
                if entry.get("core_id") == core
                and entry.get("package_id") == expected[core]
                and entry.get("game_id")
            ]
            if len(entry_matches) != 1:
                raise AcceptanceError(f"{core}: expected one selected library entry")
            games[core] = entry_matches[0]["game_id"]
        return games

    def wait_session(self, state: str, game_id: str | None = None, package_id: str | None = None) -> dict[str, Any]:
        last: dict[str, Any] = {}
        for attempt in range(self.poll_attempts):
            try:
                last = self.host.get("/api/v1/session")
            except AcceptanceError as exc:
                last = {"error": str(exc)}
            if last.get("state") == state:
                if game_id is not None and last.get("game_id") != game_id:
                    pass
                elif package_id is not None and (last.get("core_package") or {}).get("package_id") != package_id:
                    pass
                else:
                    return last
            if attempt + 1 < self.poll_attempts:
                time.sleep(self.poll_interval)
        raise AcceptanceError(f"session did not reach {state}: {json.dumps(last, sort_keys=True)}")

    def wait_input(self, attached: bool, minimum_frames: int = 0) -> dict[str, Any]:
        last: dict[str, Any] = {}
        for attempt in range(self.poll_attempts):
            last = self.host.get("/api/v1/session/input")
            metrics = last.get("metrics") or {}
            if (
                last.get("state") == ("attached" if attached else "detached")
                and (not attached or last.get("ready") is True)
                and int(metrics.get("frames_sent", 0)) >= minimum_frames
            ):
                return last
            if attempt + 1 < self.poll_attempts:
                time.sleep(self.poll_interval)
        raise AcceptanceError(f"input did not reach expected state: {json.dumps(last, sort_keys=True)}")

    def send_media(self, core: str, package_id: str) -> str:
        path = self.media[core]
        session = self.host.get("/api/v1/session")
        package = session.get("core_package") or {}
        required = (session.get("id"), session.get("target"), package.get("generation"))
        if not all(required):
            raise AcceptanceError(f"{core}: active session lacks media admission identity")
        headers = {
            "X-FogCast-Package-ID": package_id,
            "X-FogCast-Core-Generation": str(package["generation"]),
            "X-FogCast-Session-ID": str(session["id"]),
            "X-FogCast-Target": str(session["target"]),
        }
        if session.get("target_id"):
            headers["X-FogCast-Target-ID"] = str(session["target_id"])
        self.host.post_bytes("/api/v1/session/development-media", path.read_bytes(), headers)
        return hashlib.sha256(path.read_bytes()).hexdigest()

    def capture(self, core: str) -> dict[str, Any] | None:
        if self.capture_dir is None:
            return None
        self.capture_dir.mkdir(parents=True, exist_ok=True)
        output = self.capture_dir / f"{core.replace('.', '-')}.jpg"
        command = capture_command(self.video_device, self.capture_size, self.capture_frames, output)
        try:
            result = subprocess.run(command, check=True, capture_output=True, text=True, timeout=self.host.timeout)
        except (OSError, subprocess.SubprocessError) as exc:
            detail = getattr(exc, "stderr", "") or str(exc)
            raise AcceptanceError(f"{core}: HDMI capture failed: {detail.strip()}") from exc
        if not output.is_file() or output.stat().st_size == 0:
            raise AcceptanceError(f"{core}: HDMI capture produced no bytes")
        digest = hashlib.sha256(output.read_bytes()).hexdigest()
        return {"path": str(output), "sha256": digest, "bytes": output.stat().st_size}

    def run_core(self, spec: CoreSpec, package_id: str, game_id: str) -> None:
        baseline_frames = 0
        try:
            launch = self.host.post_json("/api/v1/session/launch", {"game_id": game_id})
            self.active = True
            if launch.get("state") != "active" or launch.get("game_id") != game_id:
                raise AcceptanceError(f"{spec.core_id}: launch did not become active")
            if (launch.get("core_package") or {}).get("package_id") != package_id:
                raise AcceptanceError(f"{spec.core_id}: launch selected the wrong package")
            session = self.wait_session("active", game_id, package_id)
            input_status = session.get("input") or {}
            baseline_frames = int((input_status.get("metrics") or {}).get("frames_sent", 0))
            self.wait_input(True, baseline_frames)
            media_sha = None
            if spec.core_id in self.media:
                media_sha = self.send_media(spec.core_id, package_id)
            for input_event in spec.events:
                self.host.post_json("/api/v1/session/input/event", input_event)
            input_status = self.wait_input(True, baseline_frames + len(spec.events))
            capture = self.capture(spec.core_id)
            record = {
                "core": spec.core_id,
                "game_id": game_id,
                "package_id": package_id,
                "generation": (session.get("core_package") or {}).get("generation"),
                "frames_sent": (input_status.get("metrics") or {}).get("frames_sent", 0),
                "input_events": len(spec.events),
            }
            if media_sha:
                record["media_sha256"] = media_sha
            if capture:
                record["capture"] = capture
            self.records.append(record)
            print(f"passed {spec.core_id}: package={package_id} game={game_id}")
        finally:
            if self.active:
                try:
                    self.host.post_empty("/api/v1/session/stop")
                    self.wait_session("idle")
                finally:
                    self.active = False

    def run(self) -> None:
        expected = self.selection_ids()
        self.check_health()
        games = self.inventory(expected)
        self.wait_session("idle")
        for spec in CORE_SPECS:
            self.run_core(spec, expected[spec.core_id], games[spec.core_id])
        if self.capture_dir is not None:
            manifest = {
                "format": 1,
                "host_api": self.host.origin,
                "target_api": self.target.origin,
                "created_at_utc": datetime.now(timezone.utc).isoformat(),
                "cores": self.records,
            }
            (self.capture_dir / "acceptance.json").write_text(
                json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8"
            )
        print("target acceptance passed: Pong, ZX81, Coleco")


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--host-api", default=os.environ.get("FOGCAST_HOST_API", "http://127.0.0.1:8787"))
    result.add_argument("--target-api", default=os.environ.get("FOGCAST_TARGET_API", "http://192.168.10.84:8182"))
    result.add_argument("--selection-dir", default=os.environ.get("FES_SELECTION_DIR", "out/native-integration-dev/development"))
    result.add_argument("--timeout", type=float, default=float(os.environ.get("FES_ACCEPTANCE_TIMEOUT", "30")))
    result.add_argument("--poll-attempts", type=int, default=int(os.environ.get("FES_ACCEPTANCE_POLL_ATTEMPTS", "60")))
    result.add_argument("--poll-interval", type=float, default=float(os.environ.get("FES_ACCEPTANCE_POLL_INTERVAL", "1")))
    result.add_argument("--media", action="append", default=[], metavar="CORE=PATH")
    result.add_argument("--capture-dir", default=os.environ.get("FES_ACCEPTANCE_CAPTURE_DIR"))
    result.add_argument("--video-device", default=os.environ.get("FES_HDMI_DEVICE", "/dev/video0"))
    result.add_argument("--capture-size", default=os.environ.get("FES_HDMI_SIZE", "1280x720"))
    result.add_argument("--capture-frames", type=int, default=int(os.environ.get("FES_HDMI_FRAMES", "5")))
    result.add_argument("--no-capture", action="store_true", help="explicitly disable capture output")
    return result


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    if args.no_capture:
        args.capture_dir = None
    if args.timeout <= 0 or args.poll_attempts <= 0 or args.poll_interval < 0 or args.capture_frames <= 0:
        print("target acceptance: timeout, poll attempts and interval must be valid", file=sys.stderr)
        return 2
    try:
        Runner(args).run()
    except AcceptanceError as exc:
        print(f"target acceptance: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
