"""The PC/XT producer selects its own lock, toolchain lane and GPU backend."""
import importlib.util
import io
import os
from pathlib import Path
import subprocess
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

    def test_omitted_cache_root_authenticates_the_local_toolchain(self):
        class Stop(Exception):
            pass
        for argv, cache in ((['build_fes_pcxt.py'], None),
                            (['build_fes_pcxt.py', '--cache-root', '/cache'], Path('/cache')),
                            (['build_fes_pcxt.py', '--seed', '2'], None)):
            with self.subTest(argv=argv), mock.patch.object(sys, 'argv', argv), \
                    mock.patch.object(builder, 'authenticate_tools', side_effect=Stop) as authenticate:
                with self.assertRaises(Stop):
                    builder.main()
                self.assertEqual(authenticate.call_args.args, (builder.ROOT, cache, 'HIP'))
        # None selects the repository-local install, never a shared cache at '.'.
        with mock.patch.object(builder, '_authenticate_tools') as authenticate:
            builder.authenticate_tools(builder.ROOT, None)
            self.assertIsNone(authenticate.call_args.kwargs['cache_root'])

    def test_invalid_seed_rejected_before_tools(self):
        for seed in ('-1', str(1 << 64), 'invalid'):
            with self.subTest(seed=seed), \
                    mock.patch.object(sys, 'argv', ['build_fes_pcxt.py', '--seed', seed]), \
                    mock.patch.object(sys, 'stderr', io.StringIO()), \
                    mock.patch.object(builder, 'authenticate_tools') as authenticate:
                with self.assertRaises(SystemExit) as stopped:
                    builder.main()
                self.assertEqual(stopped.exception.code, 2)
                authenticate.assert_not_called()

    def test_make_passes_cache_root_only_for_the_shared_lane(self):
        environment = {key: value for key, value in os.environ.items()
                       if key not in ('CACHE_ROOT', 'FES_TOOLCHAIN_CACHE_ROOT', 'MAKEFLAGS', 'MFLAGS', 'MAKELEVEL')}
        def dry_run(target, *variables, **extra):
            result = subprocess.run(['make', '-n', target, *variables], cwd=builder.ROOT,
                                    env={**environment, **extra}, text=True, capture_output=True, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            return result.stdout
        # Without a cache root both recipes use the same repository-local lane.
        self.assertIn(f'FES_TOOLCHAIN_ROOT="{builder.ROOT / "build/toolchain/fes-pcxt"}"',
                      dry_run('toolchain-fes-pcxt'))
        self.assertEqual(dry_run('build-fes-pcxt').split(),
                         ['python3', 'scripts/build_fes_pcxt.py', '--gpu-router', '"HIP"'])
        for variables, extra in ((('CACHE_ROOT=/cache',), {}), ((), {'FES_TOOLCHAIN_CACHE_ROOT': '/cache'})):
            with self.subTest(variables=variables, extra=extra):
                self.assertIn('--cache-root "/cache"', dry_run('build-fes-pcxt', *variables, **extra))
