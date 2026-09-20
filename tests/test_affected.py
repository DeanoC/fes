import subprocess
import os
import re
import tempfile
from pathlib import Path
import unittest

from scripts.affected import changed_paths, plan, LANES, CORES, MODULE_ROOTS


class AffectedTests(unittest.TestCase):
    def test_contracts_and_unknown_changes_close_over_every_lane(self):
        for path in ('sources/mister-packages', 'sources/mister-packages/packages/abi/new.yaml',
                     'image/Makefile', '.github/workflows/check.yml', 'unknown/new.bin'):
            with self.subTest(path=path):
                result = plan([path])
                self.assertTrue(all(result['lanes'].values()))
                self.assertEqual(set(result['cores']), set(CORES))

    def test_module_roots_and_gitlinks(self):
        for module in ('host', 'runtime'):
            for path in (MODULE_ROOTS[module], MODULE_ROOTS[module] + '/src/main.cpp'):
                result = plan([path])
                self.assertEqual({k for k,v in result['lanes'].items() if v},
                                 {'parent', module} | ({'host'} if module == 'runtime' else set()))

    def test_core_families_include_cross_core_consumers(self):
        cases = {
            'cores/fes-common/rtl/coleco_vdp.sv': {'coleco', 'sg1000', 'sms'},
            'cores/fes-common/rtl/tv80/tv80_core.v': {'coleco', 'sg1000', 'sms'},
            'cores/fes-coleco/rtl/coleco_machine.sv': {'coleco', 'sg1000', 'sms'},
            'cores/fes-coleco/generated/fes_simple_computer.vh': {'coleco', 'sg1000', 'sms'},
            'cores/fes-common/rtl/fes_application_gp.v': {'demo', 'coleco'},
            'cores/fes-common/rtl/fes_video_720p.v': {'demo', 'pong'},
            'cores/fes-pong/sim/board_models.v': {'demo', 'pong'},
            'cores/pong/rtl/pong_game.sv': {'pong'},
            'cores/fes-sms/rtl/sms_vdp.sv': {'sms'},
            'cores/fes-zx81/rtl/zx81_machine.sv': {'zx81'},
            'cores/fes-sg1000/diagnostic/generate.py': {'sg1000'},
            'cores/fes-sms/rtl/new_unit.sv': {'sms'},
            'scripts/sim_fes_demo.py': {'demo'},
        }
        for path, consumers in cases.items():
            with self.subTest(path=path):
                result = plan(['sources/misteross/' + path])
                self.assertEqual(set(result['cores']), consumers)
                self.assertEqual({lane for lane, selected in result['lanes'].items() if selected},
                                 {'parent', 'fpga'})

    def test_producer_software_does_not_recompile_unchanged_rtl(self):
        for path in ('scripts/build_fes_coleco_oss.py', 'scripts/build_fes_demo.py',
                     'scripts/fes_build_common.py', 'scripts/core_package.py',
                     'scripts/compiler_read_audit.py', 'tests/test_build_fes_sms.py',
                     'tests/test_export_core_package.py', 'tests/test_coleco_sim_shards.py'):
            with self.subTest(path=path):
                result = plan(['sources/misteross/' + path])
                self.assertTrue(result['lanes']['fpga'])
                self.assertTrue(result['lanes']['parent'])
                self.assertEqual(result['cores'], [])

    def test_unclassified_fpga_inputs_fail_broad(self):
        for path in ('', 'Makefile', 'AGENTS.md', 'toolchain.lock',
                     'toolchains/registered-memory.lock', 'scripts/new_helper.py',
                     'scripts/build_fes_new_core.py', 'tests/test_new_behavior.py',
                     'cores/fes-new/rtl/top.v', 'cores/fes-common/rtl/new_unit.sv',
                     'cores/fes-common/generated/fes_application.vh'):
            with self.subTest(path=path):
                result = plan(['sources/misteross' + ('/' + path if path else '')])
                self.assertEqual(set(result['cores']), set(CORES))
                self.assertTrue(result['lanes']['fpga'])

    def test_mixed_changes_union_consumers_and_keep_software_lane(self):
        result = plan(['sources/misteross/scripts/build_fes_sms.py',
                       'sources/misteross/cores/fes-sms/rtl/sms_vdp.sv',
                       'sources/misteross/cores/fes-zx81/sim/machine_tb.cpp'])
        self.assertEqual(set(result['cores']), {'sms', 'zx81'})
        self.assertTrue(result['lanes']['fpga'])

    def test_consumer_rules_cover_current_simulation_and_producer_sources(self):
        # Read Make's expanded recipes without running a simulator or building.
        # Checking source/include paths, not a hand-copied dependency inventory,
        # makes uncovered literal cross-family references fail even on a
        # software-only PR. This is not recursive compiler/import tracing.
        root = Path(__file__).resolve().parents[1] / 'sources/misteross'
        environment = dict(os.environ)
        for name in ('MAKEFLAGS', 'MFLAGS', 'FES_TOOLCHAIN_CACHE_ROOT', 'CACHE_ROOT'):
            environment.pop(name, None)
        for core in CORES:
            targets = ['sim-fes-' + core]
            if core in ('sg1000', 'sms'):
                targets.append('sim-fes-' + core + '-oss')
            recipe = subprocess.check_output(
                ['make', '--no-print-directory', '--dry-run', 'VERILATOR=true', *targets],
                cwd=root, env=environment, text=True)
            # Demo's recipe delegates to Python. Inspect that script as well as
            # each producer's literal source/include references; never import
            # producer modules (which could have execution side effects).
            sources = [root / 'scripts' / ('build_fes_' + core + '.py'),
                       root / 'scripts' / ('build_fes_' + core + '_oss.py')]
            if core == 'demo':
                sources.append(root / 'scripts/sim_fes_demo.py')
            recipe += ''.join(path.read_text() for path in sources if path.exists())
            references = set(re.findall(r'cores/[A-Za-z0-9_./-]+', recipe))
            self.assertTrue(references, core)
            for path in references:
                with self.subTest(core=core, path=path):
                    self.assertIn(core, plan(['sources/misteross/' + path])['cores'],
                                  'new consumer requires a planner dependency rule')

    def test_docs_only_reports_explicit_skips_and_metadata(self):
        result = plan(['README.md', 'docs/development.md', 'sources/FogCast/docs/ARCHITECTURE.md'])
        self.assertEqual(result['skipped'], list(LANES))
        self.assertTrue(result['always'])
        self.assertIn('hardware acceptance', result['not_run'])
        self.assertTrue(all(plan(['AGENTS.md'])['lanes'].values()))

    def test_real_git_merge_base_deletes_and_renames(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            def git(*args):
                return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()
            git('init', '-q'); git('config', 'user.name', 'Test'); git('config', 'user.email', 'test@example.invalid')
            path = root/'sources/FogCast/main.go'
            path.parent.mkdir(parents=True); path.write_text('old')
            git('add', '.'); git('commit', '-qm', 'base')
            base = git('rev-parse', 'HEAD')
            git('checkout', '-qb', 'feature')
            path.unlink()
            moved = root/'sources/libmister-runtime/new.cpp'
            moved.parent.mkdir(parents=True); moved.write_text('old')
            git('add', '-A'); git('commit', '-qm', 'move between modules')
            head = git('rev-parse', 'HEAD')
            git('checkout', '-qb', 'base-advanced', base)
            (root/'unrelated.txt').write_text('base only')
            git('add', '.'); git('commit', '-qm', 'base advances')
            paths = changed_paths(root, 'HEAD', head)
            self.assertEqual(paths, ['sources/FogCast/main.go', 'sources/libmister-runtime/new.cpp'])
            result = plan(paths)
            self.assertTrue(result['lanes']['host'] and result['lanes']['runtime'])
            self.assertFalse(result['lanes']['fpga'])


if __name__ == '__main__':
    unittest.main()
