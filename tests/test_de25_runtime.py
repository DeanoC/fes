"""Failure gates for the off-board AArch64 software diagnostic."""
from contextlib import nullcontext, redirect_stderr
import importlib.util
import io
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location(
    "de25_runtime_probe", Path(__file__).resolve().parents[1] / "scripts/de25_runtime.py")
RUNTIME = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RUNTIME)


class OffboardRuntimeTests(unittest.TestCase):
    def test_clears_cross_build_and_make_overrides_before_nested_builds(self):
        with mock.patch.dict(os.environ, {"PYTHON": "/an/override", "MAKEFLAGS": "bad",
                                         "GOARCH": "arm", "GOARM": "7", "GOARM64": "v9.0",
                                         "PATH": "/keep/path"}):
            environment = RUNTIME.environment()
        self.assertNotIn("PYTHON", environment)
        self.assertNotIn("MAKEFLAGS", environment)
        self.assertNotIn("GOARCH", environment)
        self.assertNotIn("GOARM", environment)
        self.assertNotIn("GOARM64", environment)
        self.assertEqual(environment["PATH"], "/keep/path")

    def test_rejects_image_output_overlap_without_mutating_it(self):
        with tempfile.TemporaryDirectory() as temporary:
            image = Path(temporary) / "image"
            image.mkdir()
            sentinel = image / "runtime.json"
            sentinel.write_text("retained image content")
            for output in (image, image / "nested", image.parent):
                with self.subTest(output=output), mock.patch.object(RUNTIME, "builder_module") as builder, \
                        redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as failure:
                    RUNTIME.main(["--image-output", str(image), "--output", str(output)])
                self.assertEqual(failure.exception.code, 2)
                builder.return_value.snapshot.assert_not_called()
            self.assertEqual(sentinel.read_text(), "retained image content")

    def test_failed_input_verification_removes_old_success_before_building(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output, image = root / "runtime", root / "image"
            output.mkdir()
            image.mkdir()
            for name in ("runtime.json", "checks.json"):
                (output / name).write_text("previous success")
            helper = SimpleNamespace(snapshot=mock.Mock(return_value=("a" * 40, root)),
                                     output_lock=lambda path: nullcontext(),
                                     validate_output=lambda path: None,
                                     verify_receipt=mock.Mock(side_effect=ValueError("changed SDK")),
                                     ensure_builder=mock.Mock())
            with mock.patch.object(RUNTIME, "builder_module", return_value=helper), \
                    redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as failure:
                RUNTIME.main(["--image-output", str(image), "--output", str(output)])
            self.assertEqual(failure.exception.code, 1)
            self.assertFalse((output / "runtime.json").exists())
            self.assertFalse((output / "checks.json").exists())
            helper.ensure_builder.assert_not_called()

    def test_zero_or_partial_agent_test_execution_cannot_publish_success(self):
        with tempfile.TemporaryDirectory() as temporary:
            log = Path(temporary) / "go.log"
            for text in ("PASS\n", "--- PASS: " + RUNTIME.GO_TESTS[0] + " (0.00s)\n"):
                log.write_text(text)
                with self.assertRaisesRegex(ValueError, "did not run"):
                    RUNTIME.check_agent_results(log)
            log.write_text("".join("--- PASS: " + name + " (0.00s)\n" for name in RUNTIME.GO_TESTS))
            RUNTIME.check_agent_results(log)


if __name__ == "__main__":
    unittest.main()
