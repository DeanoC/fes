import copy
import gzip
import unittest

from scripts import de25_kernel_test as kernel


class KernelEvidenceTests(unittest.TestCase):
    def test_passing_kunit_does_not_hide_kernel_warnings(self):
        kernel.check_kernel_log("Booting Linux\n# fes-de25-dma: pass:8 fail:0\n")
        for message in ("WARNING: CPU: 0", "BUG: bad access", "Oops: abort",
                        "Kernel panic - not syncing", "Internal error: Oops"):
            with self.subTest(message=message), self.assertRaises(ValueError):
                kernel.check_kernel_log("[  0.123] " + message)

    def test_ikconfig_requires_one_complete_bounded_payload(self):
        config = b"CONFIG_KUNIT=y\n"
        image = b"prefixIKCFG_ST" + gzip.compress(config) + b"IKCFG_EDsuffix"
        self.assertEqual(kernel.config_from_image(image), config)
        for damaged in (image + image, image.replace(b"IKCFG_ED", b"broken"),
                        image.replace(gzip.compress(config), b"invalid")):
            with self.subTest(damaged=damaged[:20]), self.assertRaises((ValueError, OSError)):
                kernel.config_from_image(damaged)
        oversized = b"IKCFG_ST" + gzip.compress(b"X" * ((1 << 20) + 1)) + b"IKCFG_ED"
        with self.assertRaises(ValueError):
            kernel.config_from_image(oversized)

    def test_all_lifetime_and_revocation_cases_must_run_and_pass(self):
        cases = [{"name": name, "status": "PASS"} for name in sorted(kernel.REQUIRED_CASES)]
        good = {"sub_groups": [{"name": "fes-de25-dma", "test_cases": cases}]}
        self.assertEqual(set(kernel.passing_cases(good)), kernel.REQUIRED_CASES)
        for status in ("FAIL", "SKIP", "ERROR"):
            changed = copy.deepcopy(good)
            changed["sub_groups"][0]["test_cases"][0]["status"] = status
            with self.subTest(status=status), self.assertRaises(ValueError):
                kernel.passing_cases(changed)
        for changed in ({}, {"sub_groups": good["sub_groups"] * 2},
                        {"sub_groups": [{"name": "fes-de25-dma", "test_cases": cases[:-1]}]},
                        {"sub_groups": [{"name": "fes-de25-dma", "test_cases": cases + cases[:1]}]}):
            with self.assertRaises(ValueError):
                kernel.passing_cases(changed)
