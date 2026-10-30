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

    def test_board50_route_reaches_the_selected_tool_authentication(self):
        class Stop(Exception):
            pass
        with mock.patch.object(sys, 'argv', ['build_fes_pcxt.py', '--half-rate-board']), \
                mock.patch.object(builder, 'authenticate_tools', side_effect=Stop) as authenticate:
            with self.assertRaises(Stop):
                builder.main()
            self.assertEqual(authenticate.call_args.args, (builder.ROOT, None, 'HIP'))

    def test_board50_routes_with_its_pad_constraints_and_explicit_timing_modes(self):
        command = builder.nextpnr_command(Path('/tools/nextpnr-mistral'), 2, True)
        value = lambda name: command[command.index(name) + 1]
        self.assertEqual(value('--qsf'), str(builder.ROOT / builder.BOARD50_QSF))
        self.assertEqual(value('--seed'), '2')
        self.assertEqual(value('--freq'), '100')
        self.assertIn('--phase-related-harmonic-clocks', command)
        self.assertIn('--mistral-ff-reset-reference', command)
        self.assertEqual(value('--critical-cohort-budget'), '0')
        self.assertEqual(value('--timing-report-paths'), '1024')

    def test_common_clock_recipe_keeps_the_normal_pad_and_timing_selection(self):
        command = builder.nextpnr_command(Path('/tools/nextpnr-mistral'), 3)
        self.assertEqual(command[command.index('--qsf') + 1], str(builder.ROOT / builder.QSF))
        self.assertEqual(command[command.index('--timing-report-paths') + 1], '64')
        for option in ('--phase-related-harmonic-clocks', '--mistral-ff-reset-reference',
                       '--critical-cohort-budget'):
            self.assertNotIn(option, command)

    def test_neither_route_recipe_suppresses_timing_failures(self):
        for half_rate in (False, True):
            with self.subTest(half_rate=half_rate):
                command = builder.nextpnr_command(Path('/tools/nextpnr-mistral'), 1, half_rate)
                self.assertNotIn('--timing-allow-fail', command)
                self.assertNotIn('--ignore-rel-clk', command)

    def test_board50_synthesis_is_explicit_and_default_stays_common_clock(self):
        default = builder.yosys_program(Path('/tmp/pcxt-fixture'), False, False, 'synth')
        self.assertNotIn('-set HALF_RATE_BOARD', default)
        trial = builder.yosys_program(Path('/tmp/pcxt-fixture'), False, False, 'synth', True)
        self.assertEqual(trial, default.replace(' top; synth;', ' -set HALF_RATE_BOARD 1 top; synth;'))

    def test_make_passes_cache_root_only_for_the_shared_lane(self):
        environment = {key: value for key, value in os.environ.items()
                       if key not in ('CACHE_ROOT', 'FES_TOOLCHAIN_CACHE_ROOT', 'MAKEFLAGS', 'MFLAGS', 'MAKELEVEL')}
        def dry_run(target, *variables, **extra):
            result = subprocess.run(['make', '-n', target, *variables], cwd=builder.ROOT,
                                    env={**environment, **extra}, text=True, capture_output=True, check=False)
            self.assertEqual(result.returncode, 0, result.stderr)
            return result.stdout
        # OSS provisioning remains available for advisory diagnostics.
        self.assertIn(f'FES_TOOLCHAIN_ROOT="{builder.ROOT / "build/toolchain/fes-pcxt"}"',
                      dry_run('toolchain-fes-pcxt'))
        self.assertEqual(dry_run('build-fes-pcxt').split(),
                         ['python3', 'scripts/build_fes_pcxt_quartus.py'])
        for variables, extra in ((('CACHE_ROOT=/cache',), {}), ((), {'FES_TOOLCHAIN_CACHE_ROOT': '/cache'})):
            with self.subTest(variables=variables, extra=extra):
                self.assertIn('--cache-root "/cache"', dry_run('build-fes-pcxt', *variables, **extra))
