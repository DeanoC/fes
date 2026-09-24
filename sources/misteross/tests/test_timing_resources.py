import unittest

from scripts.fes_build_common import BuildError, validate_timing_resources


class TimingResourceTests(unittest.TestCase):
    def test_unknown_zero_usage_is_recorded(self):
        resources = validate_timing_resources(
            {"MISTRAL_FF": {"used": 4, "available": 100},
             "new_toolchain_resource": {"used": 0, "available": 1}},
            {"MISTRAL_FF"},
        )
        self.assertEqual(resources["new_toolchain_resource"], {"used": 0, "available": 1})

    def test_unknown_nonzero_usage_is_rejected(self):
        with self.assertRaisesRegex(BuildError, "unknown resources in use: new_toolchain_resource"):
            validate_timing_resources(
                {"new_toolchain_resource": {"used": 1, "available": 1}}, set())

    def test_unknown_malformed_usage_is_rejected(self):
        for used in (False, 0.0, -1, None):
            with self.subTest(used=used), self.assertRaisesRegex(BuildError, "malformed resource counts"):
                validate_timing_resources(
                    {"new_toolchain_resource": {"used": used, "available": 1}}, set())


if __name__ == "__main__":
    unittest.main()
