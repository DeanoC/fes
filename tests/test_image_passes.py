"""Cached images must match the requested pass count in both directions."""
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import build
from image_passes import validate_image_passes


class ImagePassCacheTest(unittest.TestCase):
    def test_default_one_pass_in_local_and_ci_and_explicit_reproducibility(self):
        for env in ({}, {"CI": "true"}, {"GITHUB_ACTIONS": "true"}):
            self.assertEqual(validate_image_passes(environment=env), 1)
            self.assertEqual(validate_image_passes(2, environment=env), 2)
        for value in (0, 3, True, "garbage"):
            with self.assertRaises(ValueError):
                validate_image_passes(value)

    def test_pass_count_must_match_in_both_directions(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            output.joinpath("reproducibility.txt").write_text("image_passes=2\nshared_cache=0\n")
            self.assertEqual(build.image_pass_cache_reason(output, "2"), "")
            self.assertIn("2-pass output cannot satisfy a 1-pass request",
                          build.image_pass_cache_reason(output, "1"))
            output.joinpath("reproducibility.txt").write_text("image_passes=1\n")
            self.assertIn("1-pass output cannot satisfy a 2-pass request",
                          build.image_pass_cache_reason(output, "2"))
            self.assertEqual(build.image_pass_cache_reason(output, "1"), "")
            output.joinpath("reproducibility.txt").unlink()
            self.assertEqual(build.image_pass_cache_reason(output, "1"), "image pass evidence is missing")
            output.joinpath("reproducibility.txt").write_text("shared_cache=0\n")
            self.assertEqual(build.image_pass_cache_reason(output, "2"), "")
            self.assertIn("cannot satisfy a 1-pass request", build.image_pass_cache_reason(output, "1"))


if __name__ == "__main__":
    unittest.main()
