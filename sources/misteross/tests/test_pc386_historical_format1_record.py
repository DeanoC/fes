# SPDX-License-Identifier: MIT
"""A format-1 pc386 seal stays readable and valid after its writer is gone.

#1984 S5 deleted the manual sealer, scripts/build_fes_pc386_quartus.py, which
wrote format-1 records. The seal of PR #1780 (docs/validation/
2026-11-13-pr1780-pc386-seal) is one: its record must still decode, validate
and give the BUILD_ID it was sealed with, and both seal verifiers must still
accept it. The sealed revision is not on main, so the verifiers read the
recorded source digests in place of Git. The record's summary.md gives the
command that checks them against the revision itself; pass it the
firmware.lock of that revision, since main's BIOS pin has moved on.
"""
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from scripts import verify_pc386_quartus_seal as quartus_seal
from scripts.export_core_package import _decode_build_record, build_identity

ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT.parents[1] / "docs/validation/2026-11-13-pr1780-pc386-seal"
HEAD = "9ba4f85d8092e27d40e66a27ff1a22c98768055b"
BUILD_ID = "3e9d57407dad8e3e94f314a927ba9ba2"
# The BIOS firmware.lock pinned at HEAD; the record's README cites it.
SEALED_BIOS = "d11a805ed1cbbb00e8d764035f467c60c4fd22a941fa09ece51cf4095ba8541a"


def _product_seal():
    import importlib.util
    spec = importlib.util.spec_from_file_location(
        "verify_pc386_product_seal", ROOT / "cores/fes-pc386/tools/verify_pc386_product_seal.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class HistoricalFormat1RecordTest(unittest.TestCase):
    def setUp(self):
        self.record = (RECORD / "build-inputs.json").read_bytes()
        self.summary = json.loads((RECORD / "seal-summary.json").read_text())

    def test_the_writer_is_gone(self):
        self.assertFalse((ROOT / "scripts/build_fes_pc386_quartus.py").exists())

    def test_the_record_decodes_and_keeps_its_build_id(self):
        fields = _decode_build_record(self.record)
        self.assertEqual(fields["format"], 1)
        self.assertEqual(fields["revision"], HEAD)
        self.assertEqual(fields["recipe"], "sources/misteross/scripts/build_fes_pc386_quartus.py")
        self.assertEqual(build_identity(self.record), BUILD_ID)
        self.assertEqual(self.summary["build_id"], BUILD_ID)

    def test_both_verifiers_accept_the_seal(self):
        recorded = {f"sources/misteross/{path}": digest for path, digest in self.summary["inputs"].items()}
        with tempfile.TemporaryDirectory() as temporary:
            lock = Path(temporary) / "firmware.lock"
            lock.write_text(json.dumps({"images": {"bios": {"sha256": SEALED_BIOS}}}))
            for module in (quartus_seal, _product_seal()):
                with self.subTest(verifier=module.__name__), \
                        mock.patch.object(module, "_blob_sha256",
                                          side_effect=lambda _repo, revision, paths: (
                                              self.assertEqual(revision, HEAD)
                                              or {path: recorded[path] for path in paths})):
                    summary = module.verify(RECORD / "seal-summary.json", RECORD / "build-inputs.json", HEAD,
                                            lock)
                    self.assertEqual(summary["build_id"], BUILD_ID)
                    self.assertEqual(summary["_bound_inputs"], len(recorded))

    def test_a_changed_source_is_still_refused(self):
        recorded = {f"sources/misteross/{path}": digest for path, digest in self.summary["inputs"].items()}
        changed = dict(recorded, **{"sources/misteross/cores/fes-pc386/rtl/top.sv": "0" * 64})
        with tempfile.TemporaryDirectory() as temporary:
            lock = Path(temporary) / "firmware.lock"
            lock.write_text(json.dumps({"images": {"bios": {"sha256": SEALED_BIOS}}}))
            with mock.patch.object(quartus_seal, "_blob_sha256",
                                   side_effect=lambda _repo, _revision, paths: {path: changed[path] for path in paths}), \
                    self.assertRaisesRegex(quartus_seal.VerificationError, "differ at"):
                quartus_seal.verify(RECORD / "seal-summary.json", RECORD / "build-inputs.json", HEAD, lock)


if __name__ == "__main__":
    unittest.main()
