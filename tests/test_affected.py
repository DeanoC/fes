import subprocess
import os
import re
import tempfile
from pathlib import Path
import unittest

from scripts.affected import (changed_paths, plan, LANES, CORES, MODULE_ROOTS,
                               FPGA_SOFTWARE_TESTS, PARENT_ROOTS, CI_BROAD_INPUTS)


class AffectedTests(unittest.TestCase):
    def test_fpga_software_test_patterns_match_current_files(self):
        root = Path(__file__).resolve().parents[1] / 'sources/misteross/tests'
        for pattern in FPGA_SOFTWARE_TESTS:
            with self.subTest(pattern=pattern):
                self.assertTrue(list(root.glob(pattern)), pattern)

    def test_contracts_and_unknown_changes_close_over_every_lane(self):
        for path in ('sources/mister-packages', 'sources/mister-packages/packages/abi/new.yaml',
                     'unknown/new.bin'):
            with self.subTest(path=path):
                result = plan([path])
                self.assertTrue(all(result['lanes'].values()))
                self.assertEqual(set(result['cores']), set(CORES))

    def test_parent_owned_roots_select_only_the_parent_lane(self):
        self.assertEqual(PARENT_ROOTS,
                         ('scripts', 'tests', 'image', 'platform', 'profiles', 'containers'))
        for path in ('scripts/media.py', 'scripts/recipes.py', 'scripts/nested/new.py',
                     'tests/test_media.py', 'image/Makefile', 'image/buildroot/path',
                     'platform/internal/applianceboot/boot.go',
                     'profiles/native-integration-dev.toml', 'containers/build/Dockerfile',
                     'scripts', 'tests', 'image', 'platform', 'profiles', 'containers'):
            with self.subTest(path=path):
                result = plan([path])
                self.assertEqual({lane for lane, enabled in result['lanes'].items() if enabled},
                                 {'parent'})
                self.assertEqual(result['cores'], [])

    def test_parent_root_boundaries_do_not_swallow_sibling_names(self):
        for path in ('scripts-other/new.py', 'test-other/new.py', 'images/x',
                     'config/source-imports.toml', 'boot-media.lock.toml'):
            with self.subTest(path=path):
                result = plan([path])
                self.assertTrue(all(result['lanes'].values()))
                self.assertEqual(set(result['cores']), set(CORES))

    def test_simulation_infrastructure_exercises_simulations_without_host(self):
        for path in CI_BROAD_INPUTS:
            with self.subTest(path=path):
                self.assertEqual(plan([path])['test_modes'],
                                 {'video': False, 'media': False, 'full_race': False})
        for path in CI_BROAD_INPUTS:
            with self.subTest(path=path):
                result = plan([path])
                self.assertEqual({k for k, v in result['lanes'].items() if v},
                                 {'parent', 'fpga'})
                self.assertEqual(set(result['cores']), set(CORES))

    def test_ci_and_generator_regressions_do_not_run_unchanged_rtl(self):
        for path in ('scripts/affected.py', 'scripts/ci_gate.py',
                     'scripts/generate.py', 'scripts/consistency.py',
                     'tests/test_affected.py', 'tests/test_test_changed.py',
                     'tests/test_ci_gate.py', 'tests/test_generate.py',
                     'tests/test_consistency.py', 'tests/test_test_policy.py'):
            with self.subTest(path=path):
                result = plan([path])
                self.assertEqual({k for k, v in result['lanes'].items() if v}, {'parent'})
                self.assertEqual(result['cores'], [])
        for path in ('Makefile', '.github/workflows/check.yml', 'scripts/test_changed.py',
                     'scripts/test_policy.py',
                     'sources/mister-packages/internal/emitcpp/emitcpp.go'):
            with self.subTest(path=path):
                result = plan([path])
                self.assertTrue(all(result['lanes'].values()))
                self.assertEqual(result['cores'], [])
        # Real generated outputs still select their hardware consumers, even
        # when mixed with a generator or planner edit.
        result = plan(['scripts/generate.py',
                       'sources/misteross/cores/fes-common/generated/fes_pcxt_isa.vh'])
        self.assertEqual(result['cores'], ['pcxt'])

    def test_recipe_and_compiler_provisioning_changes_do_not_simulate_rtl(self):
        for path in ('config/core-recipes.toml', 'sources/misteross/scripts/bootstrap.sh'):
            with self.subTest(path=path):
                result = plan([path])
                self.assertEqual({k for k, v in result['lanes'].items() if v}, {'parent', 'fpga'})
                self.assertEqual(result['cores'], [])

    def test_parent_owned_changes_union_with_component_lanes(self):
        result = plan(['image/Makefile', 'sources/FogCast/catalog/query.go'])
        self.assertEqual({lane for lane, enabled in result['lanes'].items() if enabled},
                         {'parent', 'host'})
        self.assertEqual(result['cores'], [])
        result = plan(['scripts/media.py',
                       'sources/misteross/cores/fes-atari-st/rtl/st_machine.sv'])
        self.assertEqual({lane for lane, enabled in result['lanes'].items() if enabled},
                         {'parent', 'fpga'})
        self.assertEqual(result['cores'], ['atari-st'])

    def test_runtime_test_sources_do_not_select_host_consumers(self):
        root = MODULE_ROOTS['runtime']
        for path in ('tests/unit/native_hardware_test.cpp',
                     'tests/integration/daemon_server_test.cpp',
                     'tests/support/fake_input.cpp', 'tests/support/fake_input.hpp',
                     'tests/support/new_helper.h'):
            with self.subTest(path=path):
                result = plan([root + '/' + path, 'docs/development.md'])
                self.assertEqual({k for k, v in result['lanes'].items() if v},
                                 {'parent', 'runtime'})
                self.assertEqual(result['cores'], [])
                self.assertFalse(any(result['test_modes'].values()))

    def test_runtime_test_exception_preserves_other_consumer_inputs(self):
        test = MODULE_ROOTS['runtime'] + '/tests/unit/input_test.cpp'
        for path in ('src/native/input.cpp', 'src/daemon/protocol.hpp',
                     'include/libmister-runtime/runtime.h', 'Makefile',
                     'tests/incremental_build_test.sh', 'tests/fixtures/input.bin',
                     'tests/unit-other/helper.cpp', 'tests/unreviewed/helper.cpp',
                     'tests/unit/../../src/native/input.cpp', 'tests/unit/new.py'):
            with self.subTest(path=path):
                self.assertTrue(plan([test, MODULE_ROOTS['runtime'] + '/' + path])['lanes']['host'])
        for other in ('sources/FogCast/internal/misterruntime/client.go',
                      '.github/workflows/check.yml', 'unknown/new.bin', '<new-branch>'):
            with self.subTest(other=other):
                self.assertTrue(plan([test, other])['lanes']['host'])

    def test_module_roots_and_gitlinks(self):
        for module in ('host', 'runtime'):
            for path in (MODULE_ROOTS[module], MODULE_ROOTS[module] + '/src/main.cpp'):
                result = plan([path])
                self.assertEqual({k for k,v in result['lanes'].items() if v},
                                 {'parent', module} | ({'host'} if module == 'runtime' else set()))

    def test_core_families_include_cross_core_consumers(self):
        cases = {
            'cores/fes-common/rtl/coleco_native_video.v': {'coleco'},
            'cores/fes-common/rtl/fes_native_cdc.v': {'coleco'},
            'cores/fes-common/rtl/fes_native_video.v': {'coleco'},
            'cores/fes-common/rtl/fes_native_video_cart.v': {'coleco'},
            'cores/fes-common/sim/native_socket_top.v': {'coleco'},
            'cores/fes-common/sim/native_socket_tb.cpp': {'coleco'},
            'cores/fes-common/sim/native_video_top.v': {'coleco'},
            'cores/fes-common/sim/native_video_tb.cpp': {'coleco'},
            'cores/fes-common/generated/fes_native_video.vh': {'coleco'},
            'scripts/sim_fes_native_video.py': {'coleco'},
            'scripts/sim_fes_coleco_native.py': {'coleco'},
            'scripts/sim_fes_coleco_session.py': {'coleco'},
            'scripts/sim_fes_native_socket.py': {'coleco'},
            'cores/fes-common/rtl/coleco_vdp.sv': {'coleco', 'sg1000', 'sms'},
            'cores/fes-common/rtl/tv80/tv80_core.v': {'coleco', 'sms'},
            'cores/fes-coleco/rtl/coleco_machine.sv': {'coleco', 'sg1000', 'sms'},
            'cores/fes-coleco/generated/fes_simple_computer.vh': {'coleco', 'sg1000', 'sms'},
            'cores/fes-common/rtl/fes_application_gp.v': {'demo', 'coleco', 'menu', 'ramtest', 'riscv'},
            'cores/fes-common/rtl/fes_video_720p.v': {'demo', 'pong', 'ramtest', 'riscv'},
            'cores/fes-common/rtl/fes_audio_i2s.v': {'demo', 'zx81', 'coleco', 'sg1000', 'sms', 'apple2', 'c64', 'spectrum', 'atari-st'},
            'cores/fes-common/rtl/fes_audio_pll.v': {'demo'},
            'cores/fes-common/rtl/fes_audio_output.v': {'zx81', 'coleco', 'sg1000', 'sms', 'apple2', 'c64', 'spectrum', 'atari-st'},
            'cores/fes-common/rtl/fes_sn76489.sv': {'coleco', 'sg1000'},
            'cores/fes-common/rtl/fes_z80_ce.sv': {'coleco', 'sg1000', 'sms'},
            'cores/fes-pong/sim/board_models.v': {'demo', 'pong', 'riscv'},
            'cores/pong/rtl/pong_game.sv': {'pong'},
            'cores/fes-sms/rtl/sms_vdp.sv': {'sms'},
            'cores/fes-zx81/rtl/zx81_machine.sv': {'zx81'},
            'cores/fes-sg1000/diagnostic/generate.py': {'sg1000'},
            'cores/fes-sms/rtl/new_unit.sv': {'sms'},
            'scripts/sim_fes_demo.py': {'demo'},
            'cores/fes-menu/rtl/fes_menu_reader.v': {'menu', 'zx81', 'spectrum', 'c64', 'coleco', 'pc386'},
            'cores/fes-menu/rtl/fes_menu_control.v': {'menu', 'zx81', 'spectrum', 'c64', 'coleco', 'pc386'},
            'cores/fes-menu/rtl/fes_menu_video.v': {'menu', 'zx81', 'spectrum', 'c64', 'coleco', 'pc386'},
            'cores/fes-menu/rtl/fes_menu_ddr.v': {'menu'},
            'cores/fes-menu/sim/ddr_model.v': {'menu', 'zx81', 'spectrum', 'c64', 'coleco'},
            'scripts/sim_fes_menu.py': {'menu'},
            'scripts/sim_fes_zx81_session.py': {'zx81'},
            'scripts/sim_fes_display_plane.py': {'zx81'},
            'cores/fes-common/rtl/fes_display_plane.v': {'zx81', 'spectrum', 'c64', 'coleco', 'pc386'},
            'cores/fes-common/rtl/fes_display_cdc.v': {'zx81', 'spectrum', 'c64', 'coleco', 'pc386'},
            'cores/fes-common/sim/display_plane_tb.cpp': {'zx81'},
            'cores/fes-common/generated/fes_display_plane.vh': {'menu', 'zx81', 'spectrum', 'c64', 'coleco', 'pc386'},
            'cores/fes-apple2/rtl/apple2_machine.sv': {'apple2'},
            'cores/fes-c64/rtl/c64_machine.sv': {'c64'},
            'cores/fes-common/rtl/cpu6502/cpu6502.v': {'apple2', 'c64'},
            'cores/fes-common/rtl/fes_computer_mailbox.v': {'apple2', 'c64', 'spectrum', 'atari-st', 'zx81'},
            'cores/fes-common/rtl/t80pa.v': {'coleco', 'sms'},
            'scripts/apple2_slots.py': {'apple2'},
            'scripts/build_apple2_slot_card.py': {'apple2'},
            'scripts/c64_slots.py': {'c64'},
            'scripts/build_c64_slot_card.py': {'c64'},
            'cores/fes-spectrum/rtl/spectrum_machine.sv': {'spectrum'},
            'scripts/spectrum_slots.py': {'spectrum'},
            'cores/fes-common/rtl/z80/fes_z80_alu.sv': {'z80', 'sg1000', 'spectrum', 'zx81'},
            'cores/fes-common/rtl/z80/fes_z80_engine.sv': {'z80', 'sg1000', 'spectrum', 'zx81'},
            'cores/fes-common/rtl/z80/new_unit.sv': {'z80', 'sg1000', 'spectrum', 'zx81'},
            'cores/fes-common/sim/z80/cpu_tb.cpp': {'z80'},
            'cores/fes-common/sim/z80/new_fixture.bin': {'z80'},
            'scripts/sim_fes_z80.py': {'z80'},
            'scripts/benchmark_fes_z80.py': {'z80'},
            'scripts/test_fes_z80_vectors.py': {'z80'},
            'cores/fes-atari-st/rtl/st_machine.sv': {'atari-st'},
            'cores/fes-atari-st/sim/machine_tb.cpp': {'atari-st'},
            'cores/fes-c64/rtl/c64_system_pll.v': {'c64', 'atari-st'},
            'cores/fes-zx81/expansions/zonx_ay.v': {'zx81', 'atari-st', 'spectrum'},
            'cores/fes-ramtest/rtl/sdram_addon_port.v': {'ramtest', 'atari-st', 'pcxt'},
            'cores/fes-pong/rtl/pixel_pll.v': {'demo', 'pong', 'ramtest', 'riscv'},
            'cores/fes-common/rtl/riscv/fes_rv32_cpu.sv': {'riscv'},
            'cores/fes-pcxt/rtl/pcxt_isa_bridge.sv': {'pcxt'},
            'cores/fes-common/generated/fes_pcxt_isa.vh': {'pcxt'},
            'scripts/sim_fes_pcxt.py': {'pcxt'},
            'cores/fes-common/vendor/kf8259/KF8259.sv': {'pcxt', 'pc386'},
            'cores/fes-common/vendor/kf8253/read_back.patch': {'pcxt', 'pc386'},
            'cores/fes-common/rtl/fes_dma8237.sv': {'pcxt', 'pc386'},
            'cores/fes-pc386/rtl/pc386_at_core.sv': {'pc386'},
            'cores/fes-pc386/firmware/rombios/rombios.c': {'pc386'},
            'scripts/sim_fes_pc386.py': {'pc386'},
            'scripts/build_pc386_firmware.py': {'pc386'},
            'scripts/build_pc386_freedos_image.py': {'pc386'},
            'cores/fes-common/sim/riscv/rv32_model.h': {'riscv'},
            'cores/fes-riscv/firmware/firmware.S': {'riscv'},
            'scripts/sim_fes_riscv.py': {'riscv'},
            'scripts/atari_st_slot.py': {'atari-st'},
            'scripts/build_atari_st_slot_card.py': {'atari-st'},
            'scripts/fetch_atari_st_emutos.py': {'atari-st'},
            'cores/fes-common/rtl/fx68k/fx68k.sv': {'atari-st'},
            'cores/fes-common/rtl/fx68k/microrom.mem': {'atari-st'},
            'cores/fes-common/rtl/fes_video_part_direct.v': {'coleco', 'atari-st', 'pcxt'},
            'cores/fes-common/rtl/fes_video_part_scanlines.v': {'coleco', 'atari-st', 'pcxt'},
            'scripts/test_fes_z80_pin_trace.py': {'z80'},
            'scripts/sim_fes_spectrum.py': {'spectrum'},
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
                     'tests/test_export_core_package.py', 'tests/test_coleco_sim_shards.py',
                     'scripts/rom_map.py', 'scripts/rom_map_oracle.py', 'tests/test_rom_map.py',
                     'tests/test_core_package_v3.py', 'scripts/build_fes_atari_st_oss.py',
                     'tests/test_atari_st_producer.py', 'tests/test_atari_st_slot_card.py'):
            with self.subTest(path=path):
                result = plan(['sources/misteross/' + path])
                self.assertTrue(result['lanes']['fpga'])
                self.assertTrue(result['lanes']['parent'])
                self.assertEqual(result['cores'], [])

    def test_i386_runs_software_tests_without_simulations(self):
        for path in ('cores/fes-common/sim/i386/bochs_oracle.cpp', 'cores/fes-common/sim/i386/divergences.toml',
                     'cores/fes-common/sim/i386/bochs/instrument.h', 'cores/fes-common/sim/i386/frontend_tb.cpp',
                     'cores/fes-common/sim/i386/spike/spike_ag.sv', 'cores/fes-common/rtl/i386/fes_i386_qalign.sv',
                     'cores/fes-common/sim/i386/lockstep_tb.cpp', 'cores/fes-common/rtl/i386/fes_i386_cpu.sv',
                     'cores/fes-common/rtl/i386/toolchain.lock',
                     'scripts/fes_i386_oracle.py', 'scripts/test_fes_i386_vectors.py',
                     'scripts/test_fes_i386_test386.py', 'scripts/test_fes_i386_frontend.py',
                     'scripts/test_fes_i386_lockstep.py', 'scripts/quartus_fes_i386.py',
                     'scripts/benchmark_fes_i386.py'):
            with self.subTest(path=path):
                result = plan(['sources/misteross/' + path])
                self.assertEqual(result['cores'], [])
                self.assertTrue(result['lanes']['fpga'])
        # The rule is the i386 directory only: a new shared RTL unit still fails broad.
        self.assertEqual(set(plan(['sources/misteross/cores/fes-common/rtl/fes_i386.sv'])['cores']), set(CORES))

    def test_unclassified_fpga_inputs_fail_broad(self):
        for path in ('', 'Makefile', 'toolchain.lock',
                     'toolchains/zx81-expansion.lock', 'scripts/new_helper.py',
                     'scripts/build_fes_new_core.py', 'scripts/build_fes_z80.py',
                     'cores/fes-new/rtl/top.v', 'cores/fes-common/rtl/new_unit.sv',
                     'cores/fes-common/generated/fes_application.vh'):
            with self.subTest(path=path):
                result = plan(['sources/misteross' + ('/' + path if path else '')])
                self.assertEqual(set(result['cores']), set(CORES))
                self.assertTrue(result['lanes']['fpga'])

    def test_pcxt_cdc_audit_runs_host_checks_without_rtl(self):
        for name in ('audit_fes_pcxt_cdc.py', 'quartus_fes_pcxt.py', 'pcxt_quartus_mailbox.py'):
            with self.subTest(script=name):
                selected = plan(['sources/misteross/scripts/' + name])
                self.assertTrue(selected['lanes']['fpga'])
                self.assertEqual(selected['cores'], [])

    def test_misteross_tests_run_software_without_simulations(self):
        for path in ('tests/test_new_behavior.py', 'tests/producer_fixture.py',
                     'tests/fixtures/new/case.json'):
            with self.subTest(path=path):
                result = plan(['sources/misteross/' + path])
                self.assertEqual(result['cores'], [])
                self.assertTrue(result['lanes']['fpga'])
                self.assertFalse(result['test_modes']['video'])

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
            producer_name = core.replace('-', '_')
            sources = [root / 'scripts' / ('build_fes_' + producer_name + '.py'),
                       root / 'scripts' / ('build_fes_' + producer_name + '_oss.py')]
            if core in ('demo', 'menu', 'z80', 'riscv', 'pcxt', 'pc386'):
                sources.append(root / ('scripts/sim_fes_' + core + '.py'))
            if core == 'zx81':
                sources.append(root / 'scripts/sim_fes_zx81_session.py')
                sources.append(root / 'scripts/sim_fes_display_plane.py')
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
        self.assertEqual(result['test_modes'],
                         {'video': False, 'media': False, 'full_race': False})
        self.assertTrue(result['always'])
        self.assertIn('hardware acceptance', result['not_run'])
        self.assertFalse(any(plan(['AGENTS.md'])['lanes'].values()))
        self.assertEqual(plan(['AGENTS.md'])['test_modes'],
                         {'video': False, 'media': False, 'full_race': False})

    def test_go_expansion_module_selects_software_consumers_only(self):
        for path in ('rbf.go', 'asset_test.go', 'go.mod', 'go.sum', 'testdata/linked.rbf'):
            with self.subTest(path=path):
                result = plan(['sources/misteross/expansion/' + path])
                self.assertEqual({lane for lane, enabled in result['lanes'].items() if enabled}, {'parent', 'host'})
                self.assertEqual(result['cores'], [])
        # The software module exception must not swallow adjacent FPGA trees.
        self.assertEqual(set(plan(['sources/misteross/expansion-other/new.v'])['cores']), set(CORES))

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
