"""A window machine's qsys_top adds no interconnect pipeline stages; nothing else changes."""
import importlib.util
import json
from pathlib import Path
import re
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
RECORD = ROOT.parents[1] / 'docs/validation/2026-11-22-de25-window-latency'
spec = importlib.util.spec_from_file_location('de25_window_latency_board', ROOT / 'boards/de25nano/machine/board.py')
B = importlib.util.module_from_spec(spec)
spec.loader.exec_module(B)

CONNECTION = ('        <altera:connection_parameter altera:parameter_name="qsys_mm.maxAdditionalLatency" '
              'altera:parameter_value="{}"></altera:connection_parameter>\n')


class WindowLatencyTests(unittest.TestCase):
    def project(self, values, stages):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        project = Path(temp.name)
        (project / 'qsys_top.qsys').write_text(''.join(CONNECTION.format(value) for value in values))
        synth = project / 'qsys_top/altera_mm_interconnect_1920/synth'
        synth.mkdir(parents=True)
        (synth / 'lw.v').write_text('qsys_top_altera_avalon_st_pipeline_stage_1930_x lw_pipeline (\n')
        (synth / 'f2sdram.v').write_text('subsys_periph_fes_win_mem\n' +
                                          'qsys_top_altera_avalon_st_pipeline_stage_1930_x limiter_pipeline (\n' * stages)
        return project

    def test_only_the_window_integration_sets_the_latency(self):
        # The window's qsys_top additions are the only integration text that names it,
        # and prepare appends them only for a spec with WINDOW.
        self.assertIn('set_domain_assignment {$system} qsys_mm.maxAdditionalLatency 0\n', B.WINDOW_TOP)
        source = (ROOT / 'boards/de25nano/machine/board.py').read_text()
        self.assertEqual(source.count('exports += WINDOW_TOP'), 1)
        self.assertIn('    if has_window(spec):\n        exports += WINDOW_TOP\n', source)
        self.assertIn('def has_window(spec):\n    return getattr(spec, "WINDOW", False)\n', source)
        # Every other machine on this builder has no window: none sets WINDOW, so it defaults to False.
        for name in ('spectrum', 'atari-st', 'c64', 'pcxt'):
            self.assertNotRegex((ROOT / f'boards/de25nano/{name}/build.py').read_text(), r'(?m)^WINDOW\b')
        self.assertIn('\nWINDOW = True\n', (ROOT / 'boards/de25nano/pc386/build.py').read_text())

    def test_generated_latency_is_checked(self):
        self.assertEqual(B.verify_window_latency(self.project(['0'] * 4, 0))['pipeline_stages'], 0)
        with self.assertRaisesRegex(ValueError, 'latency differs'):
            B.verify_window_latency(self.project(['0', '4'], 0))
        with self.assertRaisesRegex(ValueError, 'latency differs'):
            B.verify_window_latency(self.project([], 0))
        with self.assertRaisesRegex(ValueError, '16 pipeline stage'):
            B.verify_window_latency(self.project(['0'] * 4, 16))

    def test_d12_harness_runs_every_package_that_holds_the_board(self):
        # machine/board.py is in the closure of every other machine package, so this change moves
        # each one's BUILD_ID and each takes D12; review found the C64 and PC/XT left out. The
        # closure record finds the packages from the producers, and the harness needs a row for each.
        closure = json.loads((RECORD / 'closure-ids-20383ab3b.json').read_text())
        packages = {name: entry for name, entry in closure.items() if isinstance(entry, dict)}
        held = {name: (entry['producer'], entry['model'])
                for name, entry in packages.items() if entry['board_py_in_closure']}
        self.assertLessEqual({'c64pal', 'c64nativepal', 'pcxt'}, set(held))
        self.assertEqual(set(packages) - set(held), {'menu', 'zx81'})
        jobs = re.search(r'(?m)^JOBS="([^"]*)"$', (RECORD / 'd12-slat.sh').read_text()).group(1)
        rows = {name: (module.rsplit('.', 1)[1], model)
                for name, module, model in (line.split() for line in jobs.splitlines())}
        self.assertEqual(rows, held)
        # board.py is the only file of their closures that differs from the base; MENU and ZX81,
        # which do not hold it, have none.
        for name, entry in packages.items():
            self.assertEqual(entry['closure_files_changed_since_base'],
                             ['boards/de25nano/machine/board.py'] if name in held else [], name)


if __name__ == '__main__':
    unittest.main()
