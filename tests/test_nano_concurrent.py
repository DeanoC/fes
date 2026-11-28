"""Exercise shared concurrent SD + active FPGA + confirmed Ethernet C firmware."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / "platform/embedded/firmware"


class NanoConcurrentTests(unittest.TestCase):
    def test_concurrent_sd_fpga_and_ethernet_policy(self):
        with tempfile.TemporaryDirectory(prefix="fes-nano-concurrent-") as temp:
            binary = Path(temp) / "concurrent-test"
            flags = [
                "-std=c11", "-Wall", "-Wextra", "-Werror",
                "-fsanitize=undefined", "-fno-sanitize-recover=all",
                "-I" + str(FW),
            ]
            sources = [
                FW / "nano_bridges.c",
                FW / "nano_bridge_sip.c",
                FW / "nano_sd.c",
                FW / "nano_mac.c",
                FW / "nano_usb_service.c",
                FW / "nano_identity.c",
                FW / "nano_configuration.c",
                FW / "tests/nano_concurrent_test.c",
            ]
            subprocess.run(["cc", *flags, *map(str, sources), "-o", str(binary)],
                           check=True, capture_output=True, text=True, timeout=30)
            result = subprocess.run([str(binary)], check=True, capture_output=True, text=True, timeout=10)
            self.assertIn("PASS concurrent SD + active FPGA + confirmed Ethernet", result.stdout)

    def test_sdm_input_vendor_service_matches_owned_sip_coexistence_gates(self):
        owned_sip = (FW / "nano_owned_sip.inc").read_text()
        vendor_service = (FW / "sdm_input_vendor_service.c").read_text()
        for snippet in (
            "unsigned is_config_call=",
            "int sd_gate=fes_nano_sd_gate(&fes_nano_owned_sd,0);",
            "fes_nano_mac_coexist_gate(&fes_nano_owned_mac,is_config_call);",
            "if (!(fid==FES_EMBEDDED_SD_CALL && x3==FES_EMBEDDED_SD_COOKIE)) {",
            "int sd_gate=fes_nano_sd_gate(&fes_nano_owned_sd,is_config_call);",
        ):
            self.assertIn(snippet, owned_sip)
            self.assertIn(snippet, vendor_service)
        self.assertNotIn("fes_nano_mac_gate(", owned_sip)
        self.assertNotIn("fes_nano_mac_gate(", vendor_service)


if __name__ == "__main__":
    unittest.main()
