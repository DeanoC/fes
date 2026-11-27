#!/usr/bin/env python3
"""Verify the booted DE10 idle MENU and its HDMI output under the kit claim."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import re
import urllib.request

API = "http://127.0.0.1:8787"
out = Path(os.environ["C_OUT"])
pkg = Path(os.environ["C_PKG"])


def capture(name):
    frame = out / (name + ".ppm")
    subprocess.run(["ffmpeg", "-nostdin", "-loglevel", "error", "-f", "v4l2",
                    "-i", os.environ["C_VID"], "-vf", "select=gte(n\\,15),scale=160:90",
                    "-frames:v", "1", str(frame)], timeout=45, check=True)
    pixels = frame.read_bytes().split(b"\n", 3)[3]
    if len(pixels) != 160 * 90 * 3 or sum(x > 40 for x in pixels) < len(pixels) // 100:
        raise ValueError("no visible HDMI home frame")
    return hashlib.sha256(frame.read_bytes()).hexdigest()


def booted_menu():
    package_id = pkg.stem
    if not re.fullmatch(r"[a-f0-9]{64}", package_id):
        raise ValueError("invalid staged MENU identity")
    directory = "/usr/share/mister-runtime/core-packages/" + package_id
    probe = r"""
set -eu
set -- $(pidof mister-runtime)
test "$#" = 1
cat /proc/sys/kernel/random/boot_id
sha256sum DIRECTORY/manifest.toml DIRECTORY/core.rbf
tr '\000' '\n' < "/proc/$1/cmdline" | tail -n 3
""".replace("DIRECTORY", directory)
    result = subprocess.check_output([Path.home() / "lander-hilq/kits/kssh", os.environ["C_KIT"], probe],
                                     text=True, timeout=30).splitlines()
    with tarfile.open(pkg, "r:") as archive:
        expected = [hashlib.sha256(archive.extractfile(name).read()).hexdigest() + "  " + directory + "/" + name
                    for name in ("manifest.toml", "core.rbf")]
    if (len(result) != 6 or not re.fullmatch(r"[a-f0-9-]{36}", result[0])
            or result[1:] != [*expected, "--menu-package", directory, package_id]):
        raise ValueError("booted runtime and staged MENU differ")
    with urllib.request.urlopen(API + "/api/v1/session", timeout=10) as response:
        if json.load(response).get("state") != "idle":
            raise ValueError("kit is running a session")
    return result


before = booted_menu()
frame = capture("booted-menu")
if booted_menu() != before:
    raise ValueError("MENU boot identity changed during HDMI capture")
(out / "result.json").write_text(json.dumps(dict(verdict="PASS", kit=os.environ["C_KIT"],
    head=os.environ["C_HEAD"], package_id=pkg.stem, boot_id=before[0], installed=before[1:3],
    hdmi_sha256=frame)))
