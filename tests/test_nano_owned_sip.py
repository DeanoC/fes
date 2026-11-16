"""The generated channel and standalone firmware codec require no board."""
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FIRMWARE = ROOT / "platform/embedded/firmware"


class NanoOwnedSiPTests(unittest.TestCase):
    def test_standalone_wire_and_owned_state(self):
        with tempfile.TemporaryDirectory(prefix="fes-nano-owned-sip-") as temp:
            binary = Path(temp) / "codec"
            command = ["cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I" + str(FIRMWARE),
                       str(FIRMWARE / "nano_bridges.c"), str(FIRMWARE / "nano_bridge_sip.c"), str(FIRMWARE / "tests/nano_bridge_sip_test.c"), "-o", str(binary)]
            subprocess.run(command, check=True, capture_output=True, text=True)
            result = subprocess.run([str(binary)], check=True, capture_output=True, text=True)
            self.assertIn("PASS owned bridge wire", result.stdout)

    def test_all_generated_symbols_match_frozen_wire_oracle(self):
        oracle = json.loads((ROOT / "sources/mister-packages/testdata/oracles/fes-embedded-bridges.json").read_text())
        go = (ROOT / "platform/embedded/tamago/embeddedcontrol/generated/fes_embedded_bridges.go").read_text()
        values = {name: int(value, 16) for name,value in re.findall(r"(FesEmbedded\w+)\s+uint32\s+=\s+0x([0-9a-f]+)", go)}
        self.assertEqual(values, oracle["constants"])
        self.assertEqual(values["FesEmbeddedBridgeCall"], 0xc2000301)
        self.assertNotEqual(values["FesEmbeddedBridgeCall"], 0xc2000032)


if __name__ == "__main__":
    unittest.main()
