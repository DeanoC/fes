import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import core_dev_accept as accept


class PreparedAcceptanceTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.package = "a" * 64
        self.revision = "b" * 40
        self.selection = ('format = 2\nkind = "core-package"\ncore_id = "fes.pong"\n'
                          f'package_id = "{self.package}"\npayload_sha256 = "{"c" * 64}"\n'
                          f'misteross_revision = "{self.revision}"\n'
                          f'mister_packages_revision = "{self.revision}"\n')
        self.data = {"format": 1, "core_id": "fes.pong", "package_id": self.package,
                     "sources": {k: self.revision for k in
                                 ("FogCast", "libmister-runtime", "misteross", "mister-packages")},
                     "archive": self.file("core.fcore", b"fixture archive"),
                     "selection": {**self.file("fes-pong.package-selection.toml", self.selection.encode()),
                                   "manifest_sha256": "d" * 64, "payload_sha256": "c" * 64}}
        del self.data["selection"]["size"]
        self.receipt = self.root / "prepared.json"
        self.write()

    def file(self, name, content):
        (self.root / name).write_bytes(content)
        return {"path": name, "sha256": hashlib.sha256(content).hexdigest(), "size": len(content)}

    def write(self):
        self.receipt.write_text(json.dumps(self.data))

    def make_v2(self):
        self.data["format"] = 2
        self.data["sources"]["misteross"] = "e" * 40
        sidecar = {"format": 1, "selected_repository": "https://example.com/fes.git",
                   "selected_revision": "e" * 40, "selected_source_path": "sources/misteross",
                   "original_repository": "https://example.com/misteross.git",
                   "original_revision": self.revision, "original_source_path": ".",
                   "functional_inputs_sha256": "f" * 64, "original_record_sha256": "1" * 64,
                   "selected_record_sha256": "2" * 64, "package_id": self.package,
                   "core_rbf_sha256": "c" * 64}
        self.write_v2_sidecar(sidecar)
        return sidecar

    def write_v2_sidecar(self, sidecar):
        record = self.file("fes-pong.package-selection.provenance.json", json.dumps(sidecar).encode())
        record.pop("size")
        self.data["source_selection"] = record
        self.write()

    def test_v2_cached_original_revision_is_distinct_from_current_selection(self):
        self.make_v2()
        provenance = {}
        args = accept.candidate_arguments(self.receipt, provenance)
        self.assertEqual(args[args.index("--expected-package-id") + 1], self.package)
        self.assertEqual(provenance["source_selection_sha256"], self.data["source_selection"]["sha256"])
        self.data["format"] = 1
        self.data.pop("source_selection")
        self.write()
        with self.assertRaisesRegex(ValueError, "selection differs"):
            accept.candidate_arguments(self.receipt)

    def test_v2_source_identity_fields_are_strict_and_consistent(self):
        original = self.make_v2()
        mutations = {"format": True, "selected_revision": "a" * 40,
                     "original_revision": "e" * 40, "package_id": "e" * 64,
                     "core_rbf_sha256": "e" * 64, "functional_inputs_sha256": "bad",
                     "original_record_sha256": "x" * 64, "selected_record_sha256": None,
                     "selected_repository": "https://user:secret@example.com/repo",
                     "original_repository": "http://example.com/repo",
                     "selected_source_path": "sources/../misteross", "original_source_path": "/absolute",
                     "unexpected": "field"}
        for key, value in mutations.items():
            with self.subTest(key=key):
                self.write_v2_sidecar({**original, key: value})
                with self.assertRaises(ValueError):
                    accept.candidate_arguments(self.receipt)
        for path in ("sources//misteross", "./sources", "sources/", "sources\\misteross", ""):
            self.write_v2_sidecar({**original, "selected_source_path": path})
            with self.assertRaises(ValueError):
                accept.candidate_arguments(self.receipt)

    def test_v2_changed_sidecar_fails_before_runner(self):
        self.make_v2()
        path = self.root / self.data["source_selection"]["path"]
        path.write_text("{}")
        with patch.object(accept.isolated, "main") as runner:
            self.assertEqual(accept.main(["--prepared", str(self.receipt)]), 1)
        runner.assert_not_called()

    def test_v2_sidecar_version_presence_and_boundaries(self):
        self.make_v2()
        saved = dict(self.data["source_selection"])
        for mutation in (lambda: self.data.pop("source_selection"),
                         lambda: self.data.update(format=1),
                         lambda: self.data["source_selection"].update(path="other.json"),
                         lambda: self.data["source_selection"].update(size=1)):
            self.data["format"] = 2
            self.data["source_selection"] = dict(saved)
            mutation()
            self.write()
            with self.assertRaises(ValueError):
                accept.candidate_arguments(self.receipt)
        self.data["format"] = 2
        self.data["source_selection"] = saved
        self.write()
        path = self.root / saved["path"]
        path.write_bytes(b"x" * 65537)
        with self.assertRaisesRegex(ValueError, "exceeds"):
            accept.candidate_arguments(self.receipt)
        path.unlink()
        path.symlink_to(self.root / "core.fcore")
        with self.assertRaises(OSError):
            accept.candidate_arguments(self.receipt)

    def test_forwards_exact_candidate_and_media(self):
        self.data["library_media"] = self.file("media.bin", b"rom")
        self.write()
        args = accept.candidate_arguments(self.receipt)
        self.assertEqual(args[args.index("--expected-package-id") + 1], self.package)
        self.assertEqual(args[args.index("--archive") + 1], str(self.root / "core.fcore"))
        self.assertEqual(args[args.index("--library-media") + 1], str(self.root / "media.bin"))
        self.assertNotIn("--execute", args)
        self.assertNotIn("--expected-target-id", args)

    def test_changed_bytes_fail_before_runner(self):
        (self.root / "core.fcore").write_bytes(b"changed")
        with patch.object(accept.isolated, "main") as runner:
            self.assertEqual(accept.main(["--prepared", str(self.receipt)]), 1)
        runner.assert_not_called()

    def test_symlinked_archive_rejected(self):
        (self.root / "other").write_bytes(b"fixture archive")
        (self.root / "core.fcore").unlink()
        (self.root / "core.fcore").symlink_to(self.root / "other")
        with self.assertRaises(OSError):
            accept.candidate_arguments(self.receipt)

    def test_changed_media_fails_before_runner(self):
        self.data["library_media"] = self.file("media.bin", b"rom")
        self.write()
        (self.root / "media.bin").write_bytes(b"new")
        with patch.object(accept.isolated, "main") as runner:
            self.assertEqual(accept.main(["--prepared", str(self.receipt)]), 1)
        runner.assert_not_called()

    def test_bad_receipt_rejected(self):
        for mutation in (
            lambda: self.data.update(format=True),
            lambda: self.data["archive"].update(path="../other"),
            lambda: self.data["archive"].update(size=True),
            lambda: self.data.update(package_id="e" * 64),
            lambda: self.data.update(unexpected=True),
        ):
            original = json.loads(json.dumps(self.data))
            mutation()
            self.write()
            with self.assertRaises(ValueError):
                accept.candidate_arguments(self.receipt)
            self.data = original

    def test_identity_override_rejected_even_equal_value(self):
        for flag in accept.OWNED:
            with self.subTest(flag=flag), patch.object(accept.isolated, "main") as runner:
                self.assertEqual(accept.main(["--prepared", str(self.receipt), flag + "=value"]), 1)
                runner.assert_not_called()

    def platform_options(self):
        return ["--host-binary", "/host", "--expected-host-sha256", "e" * 64,
                "--container-image", "sha256:" + "f" * 64, "--host-config", "/private",
                "--evidence-dir", "/new", "--expected-target-id", "authorized",
                "--expected-host-revision", self.revision,
                "--expected-agent-revision", self.revision,
                "--expected-runtime-revision", self.revision,
                "--new-entry-title", "Pong diagnostic"]

    def test_abbreviated_override_rejected(self):
        with patch.object(accept.isolated, "main") as runner, self.assertRaises(SystemExit):
            accept.main(["--prepared", str(self.receipt), *self.platform_options(), "--arch", "/other"])
        runner.assert_not_called()

    def test_delegates_without_reimplementing_lifecycle(self):
        with patch.object(accept.isolated, "main", return_value=7) as runner:
            self.assertEqual(accept.main(["--prepared", str(self.receipt),
                                         *self.platform_options(), "--execute"]), 7)
            self.assertIn("--execute", runner.call_args.args[0])
            self.assertIn("authorized", runner.call_args.args[0])

    def test_missing_execute_rejected_before_container_operations(self):
        with patch.object(accept.isolated, "execute_isolated") as execute:
            self.assertEqual(accept.main(["--prepared", str(self.receipt), *self.platform_options()]), 2)
        execute.assert_not_called()

    def test_explicit_input_options_forward_without_inference(self):
        diagnostic = self.root / "events.json"
        diagnostic.write_text('{"format":1}')
        expected = hashlib.sha256(diagnostic.read_bytes()).hexdigest()
        with patch.object(accept.isolated, "main", return_value=7) as runner:
            self.assertEqual(accept.main([
                "--prepared", str(self.receipt), *self.platform_options(),
                "--input-events", str(diagnostic), "--expected-input-sha256", expected,
                "--input-timeout", "3", "--execute"]), 7)
        forwarded = runner.call_args.args[0]
        self.assertEqual(forwarded[forwarded.index("--input-events") + 1], str(diagnostic))
        self.assertEqual(forwarded[forwarded.index("--expected-input-sha256") + 1], expected)

    def test_success_binds_exact_preparation_receipt(self):
        evidence = self.root / "evidence"
        evidence.mkdir()
        options = self.platform_options()
        options[options.index("--evidence-dir") + 1] = str(evidence)
        expected = hashlib.sha256(self.receipt.read_bytes()).hexdigest()
        with patch.object(accept.isolated, "main", return_value=0):
            self.assertEqual(accept.main(["--prepared", str(self.receipt), *options, "--execute"]), 0)
        recorded = json.loads((evidence / "preparation.json").read_text())
        self.assertEqual(recorded["prepared_sha256"], expected)
        self.assertEqual(recorded["package_id"], self.package)


if __name__ == "__main__":
    unittest.main()
