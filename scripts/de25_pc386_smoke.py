#!/usr/bin/env python3
"""Disk-bearing pc386 acceptance inside the managed Kit D25 claim."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import tarfile
import time
import tomllib

from de25_conformance import Host, TEXT_KEYS, audio_windows, frame_delta, grab, judge_audio, rgb, tesseract
from pcxt_game_audio_capture import AudioRecorder

DISKS = {
    "pmode": ("b4be6e29297168abb31ca536829cbd8a42e9fb05093d16793932ea51d72814b3", 260),
    "bench": ("e69a4cb6462a79c47f55adef11d41c652481017dd541ae980cf27beed5e4823c", 80),
}
QUAKE_COMMAND = "quake -condebug -nosound -nocdaudio +exec mode0.cfg +timedemo demo1"
DOS_KEYS = {**TEXT_KEYS, ">": "shift+period", "+": "shift+equal", "_": "shift+minus", "\\": "backslash"}
SCORE = re.compile(r"\b969\s+frames\s+(\d+\.\d)\s+seconds\s+(\d+\.\d)\s+fps\b", re.I)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def package_model(package, head, slug):
    with tarfile.open(package) as archive:
        descriptor = tomllib.loads(archive.extractfile("manifest.toml").read().decode())
    require(descriptor["build"]["revision"] == head, "pc386 package revision differs from the selected head")
    require(descriptor["core"]["id"] == "fes.pc386" and descriptor["target"]["platform"] == "de25_nano",
            "expected a DE25 pc386 package")
    models = set(re.findall(r"\bi?(486sx|486dx)\b", descriptor["core"]["description"].lower()))
    require(len(models) == 1, "pc386 manifest must identify one supported CPU model")
    model = models.pop()
    require(slug in {"fes.pc386", f"fes.pc386-{model}"}, f"pc386 slug {slug} disagrees with manifest model {model}")
    return model


def preflight(drives, model):
    disks = ("pmode", "bench") if model == "486dx" else ("pmode",)
    for name in disks:
        digest, cylinders = DISKS[name]
        path = drives / f"{name}.img"
        with path.open("rb") as stream:
            require(path.stat().st_size == cylinders * 16 * 63 * 512 and
                    hashlib.file_digest(stream, "sha256").hexdigest() == digest,
                    f"fixture identity mismatch: {path}")
    for tool in ("ffmpeg", "tesseract"):
        require(shutil.which(tool), f"required capture tool missing: {tool}")


def timedemo_result(text):
    match = SCORE.search(" ".join(text.split()))
    mode = re.search(r"(?im)^\s*320\s*x\s*200\s*$", text)
    if match and mode:
        seconds, fps = map(float, match.groups())
        if seconds > 0 and fps > 0 and abs(969 / seconds - fps) <= 0.06:
            return {"frames": 969, "seconds": seconds, "fps": fps, "mode": "320x200"}
    return None


def doom_hud(frame):
    cropped = frame.with_suffix(".hud.png")
    subprocess.run(["ffmpeg", "-v", "error", "-y", "-i", str(frame), "-vf",
                    "crop=iw*0.55:ih*0.047:iw*0.13:ih*0.946,format=gray",
                    "-frames:v", "1", str(cropped)], check=True, timeout=15)
    return tesseract(cropped, 7)


class Smoke:
    def __init__(self, args, receipt):
        self.args, self.receipt = args, receipt
        self.host = Host(args.api, args.fogcast, args.out)
        self.package = args.package.stem
        self.session = ""
        self.launched = False
        self.run_id = secrets.token_hex(6)
        self.generation = 0

    def record(self, check, evidence):
        self.receipt["checks"][check] = evidence
        print(check, evidence, flush=True)

    def cli(self, *args):
        rc, value, output = self.host.cli(*args)
        require(rc == 0, f"{args[0]} failed: {output[:400]}")
        return value

    def entry(self, name):
        digest, cylinders = DISKS[name]
        disk = self.cli("core-hard-disk-install", str((self.args.drives / f"{name}.img").resolve(strict=True)))
        require(disk.get("media_id") == digest, f"imported {name} differs from its pinned SHA-256")
        entry = self.cli("core-entry", f"D25 pc386 HIL {name} {self.run_id}", self.package, "hard-disk", digest)
        game = entry["game_id"]
        geometry = f"{cylinders}/16/63"
        self.cli("core-disk-geometry", game, self.package, digest, geometry)
        self.record(f"{name}_disk", {"game_id": game, "sha256": digest, "geometry": geometry})
        return game

    def keys(self, *tokens):
        for token in tokens:
            reply = self.host.tap(self.session, token)
            require(reply.get("ok") is True, f"HID {token} was not acknowledged: {reply}")
            time.sleep(0.15)

    def command(self, command):
        self.keys(*(DOS_KEYS.get(char, char) for char in command), "enter")
        time.sleep(0.5)

    def frame(self, name):
        path = self.args.out / f"{name}.png"
        require(grab(self.args.video, path), f"HDMI capture failed: {name}")
        return path

    def wait_for(self, name, read, seconds):
        deadline = time.monotonic() + seconds
        for attempt in range(1, 100):
            frame = self.frame(f"{name}-{attempt:02d}")
            result = read(frame)
            if result:
                return {**result, "frame": frame.name}
            if time.monotonic() >= deadline:
                break
            time.sleep(3)
        raise RuntimeError(f"HDMI evidence timed out: {name}")

    def boot(self, game, name):
        self.launched = True
        response = self.host.req("/api/v1/session/launch", {"game_id": game, "target": "de25"}, timeout=180)
        require(response.get("state") == "active", f"launch failed: {response}")
        live = self.host.req("/api/v1/session")
        core = live.get("core_package") or {}
        generation = core.get("generation", 0)
        require(live.get("state") == "active" and live.get("game_id") == game and
                live.get("target_id") == self.args.target_id and core.get("package_id") == self.package
                and type(generation) is int and generation > self.generation,
                f"expected a new generation of the requested pc386 entry: {live}")
        self.generation = generation
        self.session = self.host.input_session()
        require(self.session, "pc386 HID session unavailable")

        def dos(frame):
            text = tesseract(frame, 11)
            return {"session": live["id"], "generation": generation} if re.search(
                r"free\s*com", text, re.I
            ) and re.search(
                r"(?im)^\s*c[1i:][\\/n].{0,3}[>_d]", text
            ) else None

        self.record(name, self.wait_for(name, dos, 90))

    def stop(self, name):
        reply = self.host.req("/api/v1/session/stop", {"target": "de25"}, timeout=90)
        live = self.host.req("/api/v1/session")
        require(not reply.get("error") and live.get("state") == "idle", f"pc386 failed to stop: {reply}, {live}")
        self.launched = False
        self.record(name, {"state": live["state"]})

    def doom(self):
        self.command("cd doom")
        self.command("doom")
        time.sleep(20)
        self.frame("doom-title")
        for _ in range(4):
            self.keys("enter")
            time.sleep(1)
        time.sleep(5)
        before = self.frame("doom-e1m1-before")
        hud = doom_hud(before)
        (self.args.out / "doom-e1m1-ocr.txt").write_text(hud)
        require(re.search(r"\bhealth\b", hud, re.I), "Doom gameplay HUD not detected")

        # Start capture after gameplay is visible. Fire in the level so
        # title music and menu sound effects cannot satisfy this check.
        recorder = AudioRecorder(self.args.out, "D25")
        recorder.start()
        try:
            self.keys("up", "up", "up", "left", "left", "leftctrl", "leftctrl")
            after = self.frame("doom-e1m1-after")
        finally:
            recorder.stop()
        delta = frame_delta(rgb(before), rgb(after))
        require(delta is not None and delta >= 0.008, f"Doom movement not visible: {delta}")
        audio_ok, audio_note = judge_audio(audio_windows(recorder.path), {"min_rms": 450})
        require(audio_ok, f"Doom HDMI audio not detected: {audio_note}")
        self.record("doom_e1m1", {"before": before.name, "after": after.name, "frame_delta": delta, "audio": audio_note})

    def quake(self):
        game = self.entry("bench")
        self.boot(game, "quake_freedos")
        self.command("cd marks")
        self.command("cd quakes")
        self.command(QUAKE_COMMAND)
        time.sleep(95)

        def complete(frame):
            screen = tesseract(frame, 6)
            return {"timedemo_complete": True} if re.search(
                r"seconds\s+\d+\.\d\s+fps", screen, re.I
            ) else None

        self.record("quake_console", self.wait_for("quake-console", complete, 210))
        self.command("vid_describecurrentmode")
        self.command("quit")

        def dos_prompt(frame):
            text = tesseract(frame, 6)
            if re.search(r"(?im)^\s*c:\\marks\\quakes\s*[>_]", text):
                return {"prompt": "C:\\MARKS\\QUAKES>"}
            return None

        self.record("quake_return_to_dos", self.wait_for("quake-return-to-dos", dos_prompt, 35))
        self.command(r"type id1\qconsole.log")

        def read(frame):
            text = tesseract(frame, 6)
            (self.args.out / "quake-qconsole-ocr.txt").write_text(text)
            return timedemo_result(text)

        score = self.wait_for("quake-demo1", read, 35)
        self.record("quake_demo1", {**score, "command": QUAKE_COMMAND})
        self.stop("quake_stop")

    def run(self, model):
        installed = self.cli("core-install", str(self.args.package))
        require(installed.get("package_id") == self.package, "installed pc386 package ID mismatch")
        game = self.entry("pmode")
        self.boot(game, "freedos")
        marker = str(secrets.randbelow(89999999) + 10000000)
        self.command(f"echo {marker} > feshil.txt")
        self.doom()
        self.stop("doom_stop")
        self.boot(game, "freedos_relaunch")
        self.command("type feshil.txt")

        def read_marker(frame):
            content = tesseract(frame, 11)
            return {"value": marker} if marker in {line.strip() for line in content.splitlines()} else None

        self.record("save_relaunch", self.wait_for("save-relaunch", read_marker, 30))
        self.stop("freedos_stop")
        if model == "486dx":
            self.quake()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preflight", action="store_true")
    parser.add_argument("--package", type=Path, required=True)
    parser.add_argument("--head", required=True)
    parser.add_argument("--slug", required=True)
    parser.add_argument("--drives", type=Path, required=True)
    parser.add_argument("--api")
    parser.add_argument("--fogcast")
    parser.add_argument("--target-id")
    parser.add_argument("--out", type=Path)
    parser.add_argument("--video", default="/dev/v4l/by-id/fes-ms2109-hil")
    args = parser.parse_args()
    model = package_model(args.package, args.head, args.slug)
    if args.preflight:
        preflight(args.drives, model)
        print(f"pinned DE25 {model} fixtures verified")
        return 0
    if not (args.api and args.fogcast and args.out and args.target_id):
        parser.error("execution requires --api, --fogcast, --target-id and --out")
    receipt = {"kit": "D25", "head": args.head, "package_id": args.package.stem,
               "model": model, "checks": {}, "pass": False}
    smoke = Smoke(args, receipt)
    try:
        smoke.run(model)
        receipt["pass"] = True
    except Exception as exc:
        receipt["error"] = str(exc)
        print(f"pc386 HIL failed: {exc}", flush=True)
    finally:
        if smoke.launched:
            try:
                smoke.stop("cleanup_stop")
            except Exception as exc:
                receipt["cleanup_error"] = str(exc)
                print(f"pc386 HIL cleanup failed: {exc}", flush=True)
        (args.out / "result.json").write_text(json.dumps(receipt, indent=2) + "\n")
    return 0 if receipt["pass"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
