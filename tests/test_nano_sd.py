"""Exercise the actual SD lease engine and freeze generated C/Go wire symbols."""
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / "platform/embedded/firmware"


class NanoSDTests(unittest.TestCase):
    def test_engine_lifecycle_and_failure_retention(self):
        with tempfile.TemporaryDirectory(prefix="fes-nano-sd-") as temp:
            binary = Path(temp) / "sd"
            flags = ["-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I" + str(FW)]
            subprocess.run(["cc", *flags, str(FW / "nano_bridges.c"), str(FW / "nano_sd.c"), str(FW / "tests/nano_sd_test.c"), "-o", str(binary)], check=True, capture_output=True, text=True, timeout=30)
            result = subprocess.run([str(binary)], check=True, capture_output=True, text=True, timeout=10)
            self.assertIn("PASS SD firmware lease", result.stdout)

    def test_generated_symbols_match_frozen_wire(self):
        oracle = json.loads((ROOT / "sources/mister-packages/testdata/oracles/fes-embedded-sd.json").read_text())
        go = (ROOT / "platform/embedded/tamago/embeddedcontrol/generated/fes_embedded_sd.go").read_text()
        values = {name: int(value, 16) for name, value in re.findall(r"(FesEmbedded\w+)\s+uint32\s+=\s+0x([0-9a-f]+)", go)}
        self.assertEqual(values, oracle["constants"])
        self.assertEqual(values["FesEmbeddedSdCall"], 0xc2000302)


if __name__ == "__main__":
    unittest.main()
