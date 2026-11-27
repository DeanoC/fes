import hashlib
import io
import json
import os
from pathlib import Path
import runpy
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch


SMOKE = Path(__file__).resolve().parents[1] / "scripts/powerboat/nightly-menu-smoke.py"


class BootedMenuTest(unittest.TestCase):
    def smoke(self, change=None):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp)
            package_id = "a" * 64
            pkg = out / (package_id + ".fcore")
            directory = "/usr/share/mister-runtime/core-packages/" + package_id
            boot = "01234567-1234-1234-1234-0123456789ab"
            probe = [boot]
            with tarfile.open(pkg, "w") as archive:
                for name in ("manifest.toml", "core.rbf"):
                    data = name.encode()
                    info = tarfile.TarInfo(name)
                    info.size = len(data)
                    archive.addfile(info, io.BytesIO(data))
                    probe.append(f"{hashlib.sha256(data).hexdigest()}  {directory}/{name}")
            probe.extend(["--menu-package", directory, package_id])
            after = probe.copy()
            if change == "installed":
                probe[2] = "0" * 64 + "  " + directory + "/core.rbf"
            if change == "selection":
                probe[-1] = "b" * 64
            if change == "boot":
                after[0] = boot.replace("7", "8")
            responses = ["\n".join(x) for x in (probe, after)]
            def capture(argv, **kwargs):
                self.assertEqual(argv[0], "ffmpeg")
                pixels = bytes([0 if change == "dark" else 255]) * (160 * 90 * 3)
                Path(argv[-1]).write_bytes(b"P6\n160 90\n255\n" + pixels)
                return subprocess.CompletedProcess(argv, 0)
            env = dict(C_OUT=tmp, C_PKG=str(pkg), C_KIT="A", C_VID="fixture", C_HEAD="c" * 40)
            with (patch.dict(os.environ, env), patch("subprocess.check_output", side_effect=responses),
                  patch("subprocess.run", side_effect=capture),
                  patch("urllib.request.urlopen", side_effect=lambda *a, **kw: io.BytesIO(b'{"state":"idle"}'))):
                runpy.run_path(str(SMOKE))
            return json.loads((out / "result.json").read_bytes())

    def test_exact_booted_menu(self):
        self.assertEqual(self.smoke()["verdict"], "PASS")

    def test_rejects_wrong_bytes_selection_changed_boot_and_dark_output(self):
        for change in ("installed", "selection", "boot", "dark"):
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.smoke(change)
