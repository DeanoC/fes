"""Keep DE25 ZX81 claims within the retained programmer and boot logs."""
import gzip
import hashlib
import json
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT / "docs/validation/2026-10-30-de25-zx81-hardware"


def walk_dicts(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from walk_dicts(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk_dicts(child)


class De25Zx81HardwareEvidenceTests(unittest.TestCase):
    def test_board_and_programmed_sof_identity_match_retained_programmer_log(self):
        proof = json.loads((RECORD / "proof.json").read_text())
        identity = proof["board_identity"]
        self.assertEqual(identity["quartus_cable_name"], "DE25-Nano [3-11.3-iface0]")
        self.assertEqual(identity["fpga_part"], "A5EB013BB23BCS")
        self.assertIsNone(identity["usb_serial"])
        self.assertIsNone(identity["board_uid"])
        self.assertIsNone(identity["jtag_idcode"])
        self.assertEqual(identity["physical_board_binding"], "not-established")
        self.assertNotIn("serial", proof)
        self.assertNotIn("boot_id", proof)
        self.assertNotIn("programmed_sof_sha256", proof)
        self.assertEqual(proof["programmed_sof_quartus_checksum"], "0x0649BFAE")
        self.assertEqual(proof["programmer_log"], "program.log.gz")

        programmer = gzip.decompress((RECORD / "program.log.gz").read_bytes()).decode()
        self.assertIn('Using programming cable "DE25-Nano [3-11.3-iface0]"', programmer)
        self.assertIn("with checksum 0x0649BFAE for device A5EB013BB23BCS@1", programmer)

        docs = " ".join((RECORD / "README.md").read_text().split())
        self.assertIn("not bound to a particular DE25 board", docs)
        self.assertIn("does not retain a SHA-256 digest", docs)
        self.assertNotIn("TRWU8J8G", docs)
        self.assertNotIn("79B9468337C3A50B", docs)
        self.assertNotIn("4362c0dd", docs)
        self.assertNotIn("74e2b65c-13f3-4881-9d54-397ac63d18e0", docs)
        self.assertNotIn("cce19b02476f67d300e317180138e0e6801a9d86a69613605ad94a280d7a3784", docs)

        visual_proof = json.loads((RECORD / "visual/proof.json").read_text())
        for entry in walk_dicts(visual_proof):
            self.assertNotIn("serial", entry)
            self.assertNotIn("boot_id", entry)
            self.assertNotIn("programmed_sof_sha256", entry)
            if "programmed_sof_quartus_checksum" in entry:
                self.assertEqual(entry["programmed_sof_quartus_checksum"], "0x0649BFAE")

        visual_docs = " ".join((RECORD / "visual/README.md").read_text().split())
        self.assertIn("do not bind the session to a USB serial, board UID or JTAG IDCODE", visual_docs)
        for value in ("TRWU8J8G", "79B9468337C3A50B", "4362c0dd", "cce19b02476f67d300e317180138e0e6801a9d86a69613605ad94a280d7a3784"):
            self.assertNotIn(value, visual_docs)

        for script in ("run.py", "visual/visual-step.py", "visual/visual-step-v2.py"):
            with self.subTest(script=script):
                source = (RECORD / script).read_text()
                self.assertNotIn("board.serial", source)
                self.assertNotIn("board.row['boot_id']", source)
                self.assertNotIn("programmed_sof_sha256", source)
                self.assertIn("programmed_sof_quartus_checksum", source)

    def test_unclean_journal_does_not_coexist_with_a_graceful_halt_claim(self):
        boot = gzip.decompress((RECORD / "boot.log.gz").read_bytes()).decode()
        self.assertIn("corrupted or uncleanly shut down", boot)

        docs = " ".join((RECORD / "README.md").read_text().split())
        self.assertIn("corrupted or uncleanly shut down", docs)
        self.assertNotIn("synced and gracefully halted before each volatile load", docs)
        self.assertNotIn("Linux is synced and gracefully halted", docs)
        self.assertNotIn("74e2b65c-13f3-4881-9d54-397ac63d18e0", docs)

    def test_manifest_hashes_match_retained_record_files(self):
        manifest = json.loads((RECORD / "manifest.json").read_text())
        for name, expected in manifest["file_sha256"].items():
            with self.subTest(name=name):
                actual = hashlib.sha256((RECORD / name).read_bytes()).hexdigest()
                self.assertEqual(actual, expected)


if __name__ == "__main__":
    unittest.main()
