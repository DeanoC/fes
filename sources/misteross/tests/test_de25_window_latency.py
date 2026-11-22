"""A window machine's qsys_top adds no interconnect pipeline stages; nothing else changes."""
import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
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
        self.assertIn('    if spec.WINDOW:\n        require_ghrd_latency(project)\n        exports += WINDOW_TOP\n', source)
        # Every other machine on this builder has no window.
        for name in ('spectrum', 'atari-st', 'c64'):
            self.assertIn('\nWINDOW = False\n', (ROOT / f'boards/de25nano/{name}/build.py').read_text())
        self.assertIn('\nWINDOW = True\n', (ROOT / 'boards/de25nano/pc386/build.py').read_text())

    def test_the_ghrd_latency_is_checked_first(self):
        B.require_ghrd_latency(self.project(['4'] * 3, 0))
        with self.assertRaisesRegex(ValueError, 'GHRD interconnect latency changed'):
            B.require_ghrd_latency(self.project(['4', '2'], 0))
        with self.assertRaisesRegex(ValueError, 'GHRD interconnect latency changed'):
            B.require_ghrd_latency(self.project([], 0))

    def test_generated_latency_is_checked(self):
        self.assertEqual(B.verify_window_latency(self.project(['0'] * 4, 0))['pipeline_stages'], 0)
        with self.assertRaisesRegex(ValueError, 'latency differs'):
            B.verify_window_latency(self.project(['0', '4'], 0))
        with self.assertRaisesRegex(ValueError, 'latency differs'):
            B.verify_window_latency(self.project([], 0))
        with self.assertRaisesRegex(ValueError, '16 pipeline stage'):
            B.verify_window_latency(self.project(['0'] * 4, 16))


if __name__ == '__main__':
    unittest.main()
