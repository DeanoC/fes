# SPDX-License-Identifier: MIT
"""The pc386 post-build seal checker accepts only exact, passing evidence."""

import json
from pathlib import Path
import tempfile
import unittest

from scripts import verify_pc386_quartus_seal as seal


HEAD = "2e9b086e20a2009c7049526a5ed701671b37f56a"
BIOS = "d11a805ed1cbbb00e8d764035f467c60c4fd22a941fa09ece51cf4095ba8541a"


class VerifySealTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.summary = self.root / "build-summary.json"
        self.inputs = self.root / "build-inputs.json"
        self.firmware_lock = self.root / "firmware.lock"
        self.write_json(self.summary, {
            "status": "pass",
            "timing": {"status": "pass"},
            "firmware": {"bios": BIOS},
            "build_id": "0123456789abcdef0123456789abcdef",
            "seed": 1,
            "rbf": {"sha256": "0" * 64},
        })
        self.write_json(self.inputs, {"revision": HEAD})
        self.write_json(self.firmware_lock, {"images": {"bios": {"sha256": BIOS}}})

    def tearDown(self):
        self.temporary.cleanup()

    @staticmethod
    def write_json(path, value):
        path.write_text(json.dumps(value), encoding="utf-8")

    def verify(self):
        return seal.verify(self.summary, self.inputs, HEAD, self.firmware_lock)

    def test_exact_passing_seal_is_accepted(self):
        self.assertEqual(self.verify()["status"], "pass")

    def test_failed_timing_is_rejected(self):
        data = json.loads(self.summary.read_text(encoding="utf-8"))
        data["timing"]["status"] = "fail"
        self.write_json(self.summary, data)
        with self.assertRaisesRegex(seal.VerificationError, "timing=fail"):
            self.verify()

    def test_wrong_bios_digest_is_rejected(self):
        data = json.loads(self.summary.read_text(encoding="utf-8"))
        data["firmware"]["bios"] = "f" * 64
        self.write_json(self.summary, data)
        with self.assertRaisesRegex(seal.VerificationError, "firmware.bios"):
            self.verify()

    def test_wrong_source_revision_is_rejected(self):
        self.write_json(self.inputs, {"revision": "0" * 40})
        with self.assertRaisesRegex(seal.VerificationError, "exact head"):
            self.verify()

    def test_missing_inputs_are_not_treated_as_unbound(self):
        self.inputs.unlink()
        with self.assertRaisesRegex(seal.VerificationError, "cannot read build inputs"):
            self.verify()

    def test_head_must_be_a_full_lowercase_sha(self):
        with self.assertRaisesRegex(seal.VerificationError, "full lowercase Git SHA"):
            seal.verify(self.summary, self.inputs, "2e9b086", self.firmware_lock)


if __name__ == "__main__":
    unittest.main()
