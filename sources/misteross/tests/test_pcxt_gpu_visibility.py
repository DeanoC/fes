"""Preserve shared GPU lease restrictions with the pinned HIP runtime."""
import importlib.util
import os
from pathlib import Path
import sys
import unittest
from unittest import mock

SCRIPTS = Path(__file__).resolve().parents[1] / 'scripts'
original_path = sys.path[:]
sys.path.insert(0, str(SCRIPTS))
try:
    spec = importlib.util.spec_from_file_location('pcxt_builder', SCRIPTS / 'build_fes_pcxt.py')
    builder = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(builder)
finally:
    sys.path[:] = original_path


class GpuVisibilityTest(unittest.TestCase):
    def test_second_physical_gpu_keeps_its_lease_without_double_filtering(self):
        before = {'FES_GPU_IDS': '1', 'ROCR_VISIBLE_DEVICES': '1',
                  'HIP_VISIBLE_DEVICES': '1', 'PATH': '/tools'}
        with mock.patch.dict(os.environ, before, clear=True):
            after = builder.route_environment()
            self.assertEqual(after, {'FES_GPU_IDS': '1', 'ROCR_VISIBLE_DEVICES': '1', 'PATH': '/tools'})
            self.assertEqual(dict(os.environ), before)

    def test_independent_or_single_visibility_restrictions_survive(self):
        for env in ({'HIP_VISIBLE_DEVICES': '1'},
                    {'ROCR_VISIBLE_DEVICES': '1', 'HIP_VISIBLE_DEVICES': '1'},
                    {'FES_GPU_IDS': '1', 'HIP_VISIBLE_DEVICES': '1'},
                    {'FES_GPU_IDS': '1', 'ROCR_VISIBLE_DEVICES': '1'},
                    {'FES_GPU_IDS': '1', 'ROCR_VISIBLE_DEVICES': '1', 'HIP_VISIBLE_DEVICES': '0'}):
            with self.subTest(env=env), mock.patch.dict(os.environ, env, clear=True):
                self.assertEqual(builder.route_environment(), env)
