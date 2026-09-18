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
