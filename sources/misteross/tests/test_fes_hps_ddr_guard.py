# SPDX-License-Identifier: GPL-2.0-or-later
"""Exercise the shared HPS DDR guard at both deployed interface widths."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
GUARD = ROOT / "cores/fes-common/rtl/fes_hps_ddr_guard.v"
BENCH = ROOT / "cores/fes-common/sim/hps_ddr_guard_tb.sv"
VERILATOR = shutil.which(os.environ.get("VERILATOR", "verilator"))
MISSING_TOOLS = [name for name, found in (("Verilator", VERILATOR),
                 ("make", shutil.which("make")), ("c++", shutil.which("c++"))) if not found]
REQUIRED = os.environ.get("FES_HPS_DDR_GUARD_REQUIRED") == "1"


@unittest.skipUnless(not MISSING_TOOLS or REQUIRED, "Verilator and C++ build tools are required")
class HpsDdrGuardTest(unittest.TestCase):
    def test_both_port_widths(self):
        self.assertFalse(MISSING_TOOLS, "missing guard simulation tools: " + ", ".join(MISSING_TOOLS))
        with tempfile.TemporaryDirectory(prefix="fes-hps-ddr-guard-") as tmp:
            for data_width, address_width in ((64, 29), (128, 28)):
                with self.subTest(data_width=data_width):
                    obj = Path(tmp) / str(data_width)
                    build = subprocess.run([
                        VERILATOR, "--binary", "--timing", "-Wno-fatal",
                        "--top-module", "hps_ddr_guard_tb", "--Mdir", str(obj),
                        f"-GDATA_W={data_width}", f"-GADDR_W={address_width}",
                        str(BENCH), str(GUARD),
                    ], stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                       text=True, timeout=120)
                    self.assertEqual(build.returncode, 0, build.stdout)
                    result = subprocess.run([str(obj / "Vhps_ddr_guard_tb")],
                                            capture_output=True, text=True, timeout=10)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn(f"PASS guard width {data_width}", result.stdout)


if __name__ == "__main__":
    unittest.main()
