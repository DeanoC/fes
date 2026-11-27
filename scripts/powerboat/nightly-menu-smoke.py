#!/usr/bin/env python3
"""DE10 MENU's volatile load and HDMI home restore under hil-core.sh's kit lease."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.request

API = "http://127.0.0.1:8787"
out = Path(os.environ["C_OUT"])
pkg = Path(os.environ["C_PKG"])


def api(path, payload=None):
    data = json.dumps(payload).encode() if payload is not None else None
    request = urllib.request.Request(API + path, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def capture(name):
    frame = out / (name + ".ppm")
    subprocess.run(["ffmpeg", "-nostdin", "-loglevel", "error", "-f", "v4l2",
                    "-i", os.environ["C_VID"], "-vf", "select=gte(n\\,15),scale=160:90",
                    "-frames:v", "1", str(frame)], timeout=45, check=True)
    pixels = frame.read_bytes().split(b"\n", 3)[3]
    if len(pixels) != 160 * 90 * 3 or sum(x > 40 for x in pixels) < len(pixels) // 100:
        raise ValueError("no visible HDMI home frame")
    return hashlib.sha256(frame.read_bytes()).hexdigest()


home = capture("home-before")
try:
    loaded = subprocess.check_output([os.environ["C_FOGCAST"], "--api", API, "--json",
                                      "core-load", pkg], timeout=240)
    session = api("/api/v1/session")
    if (json.loads(loaded)["core_package"]["package_id"] != pkg.stem or
            session.get("state") != "active" or session.get("execution") != "fpga_development"
            or session["core_package"]["package_id"] != pkg.stem):
        raise ValueError("exact MENU firmware did not become active")
finally:
    api("/api/v1/session/stop", {"target": os.environ["C_TARGET"]})
time.sleep(5)
if api("/api/v1/session").get("state") != "idle":
    raise ValueError("MENU stop did not return to idle")
after = capture("home-after")
(out / "result.json").write_text(json.dumps(dict(verdict="PASS", kit=os.environ["C_KIT"],
    head=os.environ["C_HEAD"], package_id=pkg.stem, session=session, before=home, after=after)))
