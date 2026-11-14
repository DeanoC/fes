# SPDX-License-Identifier: MIT
"""The pc386 post-build seal checker accepts only exact, passing evidence."""

import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from scripts import verify_pc386_quartus_seal as seal


TOOLS = {"quartus_sh": "version=17.0.2; sha256=" + "1" * 64, "yosys": "commit=e240a46; sha256=" + "2" * 64}
SOURCES = {
    "scripts/build_fes_pc386_quartus.py": b"# the recipe\n",
    "cores/fes-common/generated/fes_computer.vh": b"// the ABI\n",
    "cores/fes-pc386/rtl/pc386_rtc.sv": b"module pc386_rtc; endmodule\n",
}
BIOS = "d11a805ed1cbbb00e8d764035f467c60c4fd22a941fa09ece51cf4095ba8541a"


class VerifySealTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.summary = self.root / "build-summary.json"
        self.inputs = self.root / "build-inputs.json"
        self.firmware_lock = self.root / "firmware.lock"
        # A repository whose one commit holds the sealed sources.
        self.repository = self.root / "repo"
        for path, data in SOURCES.items():
            target = self.repository / "sources/misteross" / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        git = ["git", "-C", str(self.repository), "-c", "user.name=t", "-c", "user.email=t@example.invalid"]
        subprocess.run(git[:3] + ["init", "-q"], check=True)
        subprocess.run(git + ["add", "-A"], check=True)
        subprocess.run(git + ["commit", "-q", "-m", "sealed"], check=True)
        self.head = subprocess.run(git[:3] + ["rev-parse", "HEAD"], check=True, capture_output=True,
                                   text=True).stdout.strip()
        self.write_json(self.summary, {
            "status": "pass",
            "timing": {"status": "pass"},
            "firmware": {"bios": BIOS},
            "build_id": "0123456789abcdef0123456789abcdef",
            "seed": 1,
            "rbf": {"sha256": "0" * 64},
            "tools": TOOLS,
            "inputs": {path: hashlib.sha256(data).hexdigest() for path, data in SOURCES.items()},
        })
        self.write_json(self.inputs, {
            "revision": self.head, "tools": TOOLS,
            "recipe": "sources/misteross/scripts/build_fes_pc386_quartus.py",
            "recipe_sha256": hashlib.sha256(SOURCES["scripts/build_fes_pc386_quartus.py"]).hexdigest(),
            "abi_definition": "sources/misteross/cores/fes-common/generated/fes_computer.vh",
            "abi_definition_sha256": hashlib.sha256(SOURCES["cores/fes-common/generated/fes_computer.vh"]).hexdigest(),
        })
        self.write_json(self.firmware_lock, {"images": {"bios": {"sha256": BIOS}}})

    def tearDown(self):
        self.temporary.cleanup()

    @staticmethod
    def write_json(path, value):
        path.write_text(json.dumps(value), encoding="utf-8")

    def verify(self):
        return seal.verify(self.summary, self.inputs, self.head, self.firmware_lock, self.repository,
                           "sources/misteross")

    def edit(self, path, change):
        data = json.loads(path.read_text(encoding="utf-8"))
        change(data)
        self.write_json(path, data)

    def test_exact_passing_seal_is_accepted(self):
        summary = self.verify()
        self.assertEqual(summary["status"], "pass")
        self.assertEqual(summary["_bound_inputs"], len(SOURCES))

    def test_summary_without_a_source_record_is_unbound(self):
        # Lander r2 P2: a passing summary with no source binding, paired with a
        # receipt that names the requested revision.
        self.edit(self.summary, lambda data: data.pop("inputs"))
        with self.assertRaisesRegex(seal.VerificationError, "no source-input record"):
            self.verify()

    def test_source_that_differs_at_the_revision_is_rejected(self):
        self.edit(self.summary, lambda data: data["inputs"].update({"cores/fes-pc386/rtl/pc386_rtc.sv": "a" * 64}))
        with self.assertRaisesRegex(seal.VerificationError, "differ at"):
            self.verify()

    def test_source_absent_at_the_revision_is_rejected(self):
        self.edit(self.summary, lambda data: data["inputs"].update({"cores/missing.sv": "b" * 64}))
        with self.assertRaisesRegex(seal.VerificationError, "differ at"):
            self.verify()

    def test_receipt_from_another_build_is_rejected(self):
        self.edit(self.inputs, lambda data: data["tools"].update({"yosys": "commit=other"}))
        with self.assertRaisesRegex(seal.VerificationError, "tools differ"):
            self.verify()

    def test_receipt_recipe_must_be_the_recorded_source(self):
        self.edit(self.inputs, lambda data: data.update({"recipe_sha256": "c" * 64}))
        with self.assertRaisesRegex(seal.VerificationError, "inputs.recipe"):
            self.verify()

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
        self.edit(self.inputs, lambda data: data.update({"revision": "0" * 40}))
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
