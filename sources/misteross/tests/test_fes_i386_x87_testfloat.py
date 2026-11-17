# SPDX-License-Identifier: MIT
"""Focused checks for the extF80 TestFloat gate's matrix and manifest."""
from pathlib import Path
import tempfile
import unittest

from scripts import test_fes_i386_x87_testfloat as testfloat


class ConfigurationTest(unittest.TestCase):
    def test_all_precision_rounding_and_operation_combinations_are_present(self):
        configs = testfloat.configurations()
        self.assertEqual(len(configs), 84)
        self.assertEqual({item[0] for item in configs}, set(testfloat.OPERATIONS))
        self.assertEqual({item[1] for item in configs}, set(testfloat.PRECISIONS))
        self.assertEqual({item[2] for item in configs}, set(range(4)))
        self.assertEqual(len({item[:3] for item in configs}), 84)

    def test_smoke_limit_is_a_prefix_and_must_be_positive(self):
        configs = testfloat.configurations()
        self.assertEqual(testfloat.configurations(3), configs[:3])
        with self.assertRaises(testfloat.TestFloatError):
            testfloat.configurations(0)

    def test_generation_records_config_counts_and_quoted_paths(self):
        original_output = testfloat.OUTPUT
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / 'testfloat output'
            root.mkdir()
            generator = root / 'generator'
            generator.write_text('#!/bin/sh\nprintf "3FFF8000000000000000 3FFF8000000000000000 '
                                 '40008000000000000000 00\\n"\n')
            generator.chmod(0o755)
            testfloat.OUTPUT = root / 'artifacts'
            try:
                manifest = testfloat.OUTPUT / 'manifest.txt'
                result = testfloat.generate(generator, manifest, 2)
                manifest_text = manifest.read_text()
            finally:
                testfloat.OUTPUT = original_output
        self.assertEqual(result['vectors'], 2)
        self.assertEqual([(item['operation'], item['precision'], item['rc']) for item in result['configs']],
                         [('add', 32, 0), ('sub', 32, 0)])
        self.assertIn('"', manifest_text)


if __name__ == '__main__':
    unittest.main()
