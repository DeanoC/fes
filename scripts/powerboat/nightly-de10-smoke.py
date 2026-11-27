#!/usr/bin/env python3
"""Record the software running under the kit claim alongside the HILQ smoke."""
import os
from pathlib import Path
import subprocess
import sys

queue = Path.home() / "lander-hilq"
out = Path(os.environ["C_OUT"])
probe = r"""
set -e
cd /usr/sbin
for name in fes-kit-data fes-kit-software fogcast-kit fogcast-tenfoot mister-agent mister-runtime; do
    sha256sum "$name"
done
for name in fogcast-tenfoot mister-runtime; do
    set -- $(pidof "$name")
    [ "$#" -eq 1 ]
    sha256sum "/proc/$1/exe" | sed "s| .*| active-$name|"
done
# The daemon receives the active firmware directory and ID on its command line.
tr '\000' '\n' < "/proc/$(pidof mister-runtime)/cmdline" | tail -n 3
"""
result = subprocess.run([queue / "kits/kssh", os.environ["C_KIT"], probe], capture_output=True)
if result.returncode == 0:
    (out / "release-software.txt").write_bytes(result.stdout)
worker = (Path(__file__).resolve().with_name("nightly-menu-smoke.py")
          if os.environ["FES_NIGHTLY_CORE"] == "fes.menu" else queue / "hilcore.py")
checker = os.environ.get("FES_NIGHTLY_CHECKER")
if not checker:
    os.execv(sys.executable, [sys.executable, str(worker)])
subprocess.run([sys.executable, worker], check=True)
frames = sorted((out / "fr").glob("05-boot-*.png"))
frames.extend(out / "fr" / name for name in ("10-core.png", "11-core.png"))
subprocess.run([sys.executable, checker, "--output", out / "hil-check.json", *frames], check=True)
