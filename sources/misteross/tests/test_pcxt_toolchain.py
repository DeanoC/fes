"""The PC/XT producer selects its own lock and the requested GPU backend."""
import importlib.util
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


class PcxtToolchainTest(unittest.TestCase):
    def test_selected_gpu_backend_authenticates_the_pcxt_lock(self):
        cache = Path('/cache')
        for router in ('HIP', 'CUDA'):
            with self.subTest(router=router), mock.patch.object(builder, '_authenticate_tools') as authenticate:
                builder.authenticate_tools(builder.ROOT, cache, router)
                args = authenticate.call_args.kwargs
                self.assertEqual(args['lock_path'], builder.ROOT / 'cores/fes-pcxt/toolchain.lock')
                self.assertEqual(args['toolchain_root'], builder.ROOT / 'build/toolchain/fes-pcxt')
                self.assertEqual(args['cache_root'], cache)
                self.assertEqual(args['gpu_router'], router)
                self.assertIn('gpu-router=' + router, args['expected_configuration']['nextpnr'])
