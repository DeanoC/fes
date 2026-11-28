import io
from pathlib import Path
import tarfile
import tempfile
import unittest

from scripts import de25_conformance  # Adds the sibling scripts directory to sys.path.
from scripts import de25_pc386_smoke as smoke


class Pc386SmokeTests(unittest.TestCase):
    def test_timedemo_requires_complete_consistent_result(self):
        self.assertEqual(smoke.timedemo_score("969 frames 122.6 seconds   7.9 fps"),
                         {"frames": 969, "seconds": 122.6, "fps": 7.9})
        for text in ("6 frames 1.0 seconds 6.0 fps", "969 frames 123.6 seconds 7.9 fps",
                     "969 frames 0.0 seconds 7.9 fps", "969 frames 122.6 seconds"):
            with self.subTest(text=text):
                self.assertIsNone(smoke.timedemo_score(text))

    def test_model_is_bound_to_package_manifest(self):
        for model, description in (("486sx", "Standard DE25 pc386: CPU as a 486SX (I486 = 1)"),
                                   ("486dx", "Standard DE25 pc386: the i486DX (387 on)")):
            with self.subTest(model=model), tempfile.TemporaryDirectory() as tmp:
                package = Path(tmp) / "test.fcore"
                content = (f'[core]\nid = "fes.pc386"\ndescription = "{description}"\n'
                           '[target]\nplatform = "de25_nano"\n[build]\nrevision = "head"\n').encode()
                with tarfile.open(package, "w") as archive:
                    member = tarfile.TarInfo("manifest.toml")
                    member.size = len(content)
                    archive.addfile(member, io.BytesIO(content))
                self.assertEqual(smoke.package_model(package, "head", f"fes.pc386-{model}"), model)
                with self.assertRaisesRegex(RuntimeError, "disagrees"):
                    smoke.package_model(package, "head", "fes.pc386-other")
                with self.assertRaisesRegex(RuntimeError, "revision"):
                    smoke.package_model(package, "base", f"fes.pc386-{model}")
