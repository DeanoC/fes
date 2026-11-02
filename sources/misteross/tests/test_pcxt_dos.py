"""Regress DOS simulation evidence cleanup, dispatch and snapshot checks."""
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import sim_fes_pcxt_dos as dos


class DosEvidenceTest(unittest.TestCase):
    def test_low_disk_preflight_clears_old_result(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            build = root / 'build/sim/fes-pcxt/dos'
            build.mkdir(parents=True)
            (build / 'result.json').write_text('old success')
            args = SimpleNamespace(scenario='dos', writable_test=False)
            with patch.object(dos, 'ROOT', root), \
                 patch.object(dos, 'check_space', side_effect=ValueError('low disk')):
                with self.assertRaisesRegex(ValueError, 'low disk'):
                    dos.run(args)
            self.assertFalse((build / 'result.json').exists())

    def test_failed_fixture_preflight_removes_prior_result(self):
        for scenario, writable in (('dos', False), ('dos', True), ('com', False), ('game', False)):
            with self.subTest(scenario=scenario, writable=writable), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                suffix = '' if scenario == 'dos' else '-' + scenario
                build = root / ('build/sim/fes-pcxt/dos' + suffix + ('-write' if writable else ''))
                build.mkdir(parents=True)
                names = ('result.json', 'result-pending.json', 'dos.ppm', 'screen.bin', 'screen.txt',
                         'inputs.json', 'bus.log', 'saved-floppy.img', 'candidate.json',
                         'title-before-start.ppm', 'start-control.ppm', 'start-key.ppm',
                         'right-control.ppm', 'right-key.ppm')
                for name in names:
                    (build / name).write_text('old evidence')
                args = SimpleNamespace(scenario=scenario, bios=None, font=None, download=False,
                                       writable_test=writable)
                with patch.object(dos, 'ROOT', root), \
                     patch.object(dos, 'check_space'), \
                     patch.object(dos.bios, 'fixture_inputs', side_effect=ValueError('bad BIOS')):
                    with self.assertRaisesRegex(ValueError, 'bad BIOS'):
                        dos.run(args)
                self.assertFalse(any((build / name).exists() for name in names))

    def test_writable_application_scenarios_fail_before_input_preparation(self):
        for scenario in ('com', 'game'):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as directory:
                args = SimpleNamespace(scenario=scenario, writable_test=True)
                with patch.object(dos, 'ROOT', Path(directory)), \
                     patch.object(dos.bios, 'fixture_inputs') as fixture:
                    with self.assertRaisesRegex(ValueError, 'requires the dos scenario'):
                        dos.run(args)
                    fixture.assert_not_called()

    def test_snapshot_checks_both_complete_files(self):
        for original, saved, valid in ((b'file contents', b'file contents', True),
                                       (b'file contents', b'file content', False), (b'', b'', False)):
            with self.subTest(original=original, saved=saved), \
                 patch.object(dos.subprocess, 'run', side_effect=[SimpleNamespace(stdout=original),
                                                                  SimpleNamespace(stdout=saved)]) as run:
                if valid:
                    dos.verify_snapshot(Path('/tmp/build'))
                else:
                    with self.assertRaisesRegex(ValueError, 'DOS copy not present or differs'):
                        dos.verify_snapshot(Path('/tmp/build'))
                self.assertEqual([call.args[0][-1] for call in run.call_args_list],
                                 ['::FESXT.TXT', '::SAVE.TXT'])
                self.assertTrue(all(call.kwargs['check'] for call in run.call_args_list))

    def test_scenario_dispatch_and_prepare_only(self):
        for scenario, writable, prepare in (('dos', True, False), ('game', False, False),
                                             ('dos', True, True), ('com', False, True)):
            with self.subTest(scenario=scenario, writable=writable, prepare=prepare), \
                 tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                disk = root / 'selected.img'
                disk.write_bytes(b'selected image')
                tb = root / 'cores/fes-pcxt/sim/dos_tb.cpp'
                tb.parent.mkdir(parents=True)
                tb.write_text('testbench')
                args = SimpleNamespace(scenario=scenario, writable_test=writable, prepare_only=prepare,
                                       disk=disk, digger_exe=None, bios=None, font=None,
                                       download=False, verilator='verilator')
                identity = {'image_sha256': 'image', 'files': {}, 'program_sha256': 'program',
                            'base_sha256': 'base'}
                def fixture(build, *_):
                    (build / 'inputs.json').write_text('{}')
                def git(*_, **kwargs):
                    return 'commit\n' if kwargs.get('text') else b''
                with patch.object(dos, 'ROOT', root), patch.object(dos, 'check_space'), \
                     patch.object(dos.bios, 'fixture_inputs', side_effect=fixture), \
                     patch.object(dos.freedos, 'validate_selected_image', return_value=identity) as standard, \
                     patch.object(dos.acceptance, 'validate', return_value=identity) as application, \
                     patch.object(dos.sim, 'verify_vendor'), patch.object(dos.sim, 'cpu_roms'), \
                     patch.object(dos.subprocess, 'check_output', side_effect=git), \
                     patch.object(dos.subprocess, 'run') as compile_run, \
                     patch.object(dos, 'run_simulation') as simulation, \
                     patch.object(dos, 'verify_snapshot') as snapshot, \
                     patch.object(dos, 'verify_game') as game:
                    dos.run(args)
                self.assertEqual(standard.call_count, int(scenario == 'dos'))
                self.assertEqual(application.call_count, int(scenario != 'dos'))
                if scenario != 'dos':
                    application.assert_called_once_with(b'selected image', scenario)
                command = compile_run.call_args.args[0]
                self.assertEqual("-GWITH_WRITABLE=1'b1" in command, writable)
                self.assertEqual('-DPCXT_WRITABLE_TEST=1' in command[command.index('-CFLAGS') + 1], writable)
                self.assertEqual(simulation.call_count, int(not prepare))
                if not prepare:
                    self.assertEqual(simulation.call_args.args[0][-1], scenario)
                self.assertEqual(snapshot.call_count, int(writable and not prepare))
                self.assertEqual(game.call_count, int(scenario == 'game' and not prepare))


if __name__ == '__main__':
    unittest.main()
