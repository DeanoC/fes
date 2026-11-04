"""Pinned generated-source and post-compile guards for shared FIFO resets.

The positive fixture's output digests come from the independently fitted
prototype. These are source/receipt tests, not physical reset qualification.
"""
import gzip
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("shared_fifo_reset_test", ROOT / "boards/de25nano/scanout/shared_resets.py")
S = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(S)


class SharedResetTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="de25-shared-reset-test-")
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.project = self.directory / "project"
        fixture = json.loads(gzip.decompress((ROOT / "tests/fixtures/de25_shared_fifo_resets.json.gz").read_bytes()))
        self.originals = {name: data.encode() for name, data in fixture["sources"].items()}
        self.expected = fixture["derived_sha256"]
        for name, data in self.originals.items():
            path = self.project / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        self.inputs = {"scanout": {"shared_fifo_resets": S.POLICY,
                                   "build_identity_scheme": "canonical-functional-record-v2",
                                   "hps_initialization": "HPS FIRST", "build_id": "1" * 32}}
        (self.directory / "inputs.json").write_text(json.dumps(self.inputs))

    def test_derivation_matches_the_actual_compiled_prototype(self):
        derived = S.derive(self.originals)
        self.assertEqual({name: hashlib.sha256(data).hexdigest() for name, data in derived.items()}, self.expected)

    def test_apply_retains_exact_inputs_and_leaves_constraints_unchanged(self):
        constraints = self.project / "untouched.sdc"
        constraints.write_text("create_clock -period 10 [get_ports clock]\n")
        before = constraints.read_bytes()
        result = S.apply(self.directory)
        self.assertEqual(result, S.verify(self.directory))
        self.assertEqual(result["release_edges"], 5)
        for name, data in self.originals.items():
            self.assertEqual((self.directory / "shared-reset-originals" / name).read_bytes(), data)
        self.assertEqual(constraints.read_bytes(), before)

    def test_unexpected_vendor_bytes_fail_before_any_replacement(self):
        name = sorted(self.originals)[-1]
        (self.project / name).write_bytes(self.originals[name] + b"\n")
        before = {name: (self.project / name).read_bytes() for name in self.originals}
        with self.assertRaisesRegex(ValueError, "unreviewed generated input"):
            S.apply(self.directory)
        self.assertEqual(before, {name: (self.project / name).read_bytes() for name in self.originals})
        self.assertFalse((self.directory / "shared-reset-originals").exists())
        self.assertFalse((self.directory / "shared-resets.json").exists())

    def test_standard_board_keeps_the_exact_compiled_fifo_derivation(self):
        parent = gzip.decompress((ROOT / "tests/fixtures/de25_standard_parent.v.gz").read_bytes())
        self.assertEqual(hashlib.sha256(parent).hexdigest(), S.STANDARD_PARENT_SHA256)
        (self.project / S.PARENT).write_bytes(parent)
        with self.assertRaisesRegex(ValueError, "unreviewed generated input"):
            S.apply(self.directory)  # Standard parent cannot replace a MENU parent.
        self.inputs["scanout"]["build_identity_scheme"] = "standard-board-integration-source-profile"
        self.inputs["standard_zx81"] = {"scope": "standard-core-board-integration", "runtime_admission": "not-qualified"}
        path = self.directory / "inputs.json"
        path.write_text(json.dumps(self.inputs))
        result = S.apply(self.directory)
        self.assertEqual(result, S.verify(self.directory))
        self.assertEqual({name: hashlib.sha256((self.project / name).read_bytes()).hexdigest()
                          for name in self.originals if name != S.PARENT},
                         {name: value for name, value in self.expected.items() if name != S.PARENT})
        self.inputs["standard_zx81"]["runtime_admission"] = "qualified"
        path.write_text(json.dumps(self.inputs))
        with self.assertRaisesRegex(ValueError, "canonical policy selection differs"):
            S.verify(self.directory)

    def test_post_compile_wrong_receiving_clock_is_rejected(self):
        S.apply(self.directory)
        parent = self.project / S.PARENT
        parent.write_text(parent.read_text().replace(".clk(fes_pixel_clk_out_clk_clk)", ".clk(periph_clk_out_clk_clk)", 1))
        with self.assertRaisesRegex(ValueError, "derived source changed"):
            S.verify(self.directory)

    def test_retained_original_mutation_is_rejected(self):
        S.apply(self.directory)
        original = self.directory / "shared-reset-originals" / sorted(self.originals)[0]
        original.write_bytes(original.read_bytes() + b"\n")
        with self.assertRaisesRegex(ValueError, "unreviewed generated input"):
            S.verify(self.directory)

    def test_receipt_cannot_shorten_the_release_pipeline(self):
        S.apply(self.directory)
        path = self.directory / "shared-resets.json"
        result = json.loads(path.read_text())
        result["release_edges"] = 4
        path.write_text(json.dumps(result))
        with self.assertRaisesRegex(ValueError, "derivation receipt differs"):
            S.verify(self.directory)

    def test_foreign_policy_cannot_select_the_derivation(self):
        self.inputs["scanout"]["shared_fifo_resets"] = "foreign"
        (self.directory / "inputs.json").write_text(json.dumps(self.inputs))
        with self.assertRaisesRegex(ValueError, "canonical policy selection differs"):
            S.apply(self.directory)
        self.assertFalse((self.directory / "shared-reset-originals").exists())

    def test_symlink_cannot_modify_source_outside_the_private_project(self):
        name = sorted(self.originals)[0]
        outside = self.directory / "outside.v"
        outside.write_bytes(self.originals[name])
        path = self.project / name
        path.unlink()
        path.symlink_to(outside)
        with self.assertRaisesRegex(ValueError, "unsafe generated source path"):
            S.apply(self.directory)
        self.assertEqual(outside.read_bytes(), self.originals[name])
        self.assertFalse((self.directory / "shared-reset-originals").exists())


if __name__ == "__main__":
    unittest.main()
