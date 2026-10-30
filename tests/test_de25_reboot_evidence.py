"""Keep the DE25 reboot evidence within its retained-log boundaries."""
import gzip
import hashlib
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT / "docs/validation/2026-10-30-de25-reboot"


class De25RebootEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.readme = (RECORD / "README.md").read_text()
        self.normalized_readme = " ".join(self.readme.split())
        self.proof = json.loads((RECORD / "proof.json").read_text())

    def test_identity_is_limited_to_programmer_log_fields(self):
        self.assertEqual(
            self.proof["board_identity"],
            {
                "quartus_cable_name": "DE25-Nano [3-11.3-iface0]",
                "fpga_part": "A5EB013BB23BCS",
                "usb_serial": None,
                "board_uid": None,
                "jtag_idcode": None,
                "physical_board_binding": "not-established",
            },
        )
        log = gzip.decompress((RECORD / "stock-program.log.gz").read_bytes()).decode()
        self.assertIn('Using programming cable "DE25-Nano [3-11.3-iface0]"', log)
        self.assertIn("device A5EB013BB23BCS@1", log)
        self.assertIn(
            "do not bind the session to a USB serial, board UID, or JTAG IDCODE",
            self.normalized_readme,
        )
        for unsupported_identity in ("TRWU8J8G", "79B9468337C3A50B", "4362c0dd"):
            with self.subTest(unsupported_identity=unsupported_identity):
                self.assertNotIn(unsupported_identity, self.readme)
        self.assertNotIn("board_usb_serial", self.proof)
        self.assertNotIn("board_uid", self.proof)
        self.assertNotIn("jtag_idcode", self.proof)

    def test_unrecorded_operator_states_are_not_asserted(self):
        self.assertEqual(
            self.proof["operator_state"],
            {
                "board_claim": "not-recorded",
                "uart_ssh_jtag_sessions": "not-recorded",
                "top_board_state": "not-recorded",
                "factory_baseline_restore": "not-recorded",
            },
        )
        self.assertIn("Those states are not asserted here.", self.normalized_readme)
        unsupported_claims = (
            r"same owner retains the board claim",
            r"all reboot-control UART, SSH and JTAG master sessions are closed",
            r"the top board is untouched",
        )
        for claim in unsupported_claims:
            with self.subTest(claim=claim):
                self.assertIsNone(re.search(claim, self.normalized_readme, re.IGNORECASE))

    def test_manifest_hashes_match_retained_record_files(self):
        manifest = json.loads((RECORD / "manifest.json").read_text())
        for filename, expected in manifest["file_sha256"].items():
            with self.subTest(filename=filename):
                actual = hashlib.sha256((RECORD / filename).read_bytes()).hexdigest()
                self.assertEqual(actual, expected)


if __name__ == "__main__":
    unittest.main()
