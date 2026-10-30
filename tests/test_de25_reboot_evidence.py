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

    def test_programmed_sof_identity_uses_retained_quartus_checksums(self):
        self.assertNotIn("ram_sof_sha256", self.proof)
        self.assertNotIn("stock_sof_sha256", self.proof)
        self.assertEqual(
            self.proof["programmed_sofs"],
            {
                "ram": {
                    "quartus_checksum": "0x067AE616",
                    "programmer_log": "ram-reload.log.gz",
                },
                "stock": {
                    "quartus_checksum": "0x062BB4EF",
                    "programmer_log": "stock-program.log.gz",
                },
            },
        )
        for variant, digest in (
            ("ram", "2267ecc2e9e8dd08cd24e8d413ee7e704cc022863d5b761b6d6e6aa33a6bb64c"),
            ("stock", "e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09"),
        ):
            with self.subTest(variant=variant):
                log_name = self.proof["programmed_sofs"][variant]["programmer_log"]
                log = gzip.decompress((RECORD / log_name).read_bytes()).decode()
                checksum = self.proof["programmed_sofs"][variant]["quartus_checksum"]
                self.assertIn(checksum, log)
                self.assertNotIn(digest, log)
                self.assertNotIn(digest, self.readme)
        self.assertIn("Neither log retains a SHA-256 digest", self.normalized_readme)

    def test_physical_power_cycle_state_is_not_asserted_without_a_record(self):
        self.assertEqual(
            self.proof["physical_power_cycle_during_investigation"],
            "not-recorded",
        )
        self.assertIn("does not establish whether a physical power cycle occurred", self.normalized_readme)
        self.assertNotIn("without a power cycle", self.normalized_readme)

        linux_guide = " ".join((ROOT / "docs/de25-linux.md").read_text().split())
        self.assertIn("does not establish whether a physical power cycle occurred", linux_guide)
        self.assertNotIn("recovers without a power cycle", linux_guide)

    def test_manifest_hashes_match_retained_record_files(self):
        manifest = json.loads((RECORD / "manifest.json").read_text())
        for filename, expected in manifest["file_sha256"].items():
            with self.subTest(filename=filename):
                actual = hashlib.sha256((RECORD / filename).read_bytes()).hexdigest()
                self.assertEqual(actual, expected)


if __name__ == "__main__":
    unittest.main()
