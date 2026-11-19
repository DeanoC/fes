"""The experimental capture pipeline must retain the selected word until response."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VERILATOR = shutil.which('verilator')


@unittest.skipUnless(VERILATOR and shutil.which('c++'), 'Verilator and C++ required')
class CapturePipelineTests(unittest.TestCase):
    def test_first_word_and_frontend_deadline_require_retention(self):
        controller = ROOT / 'cores/fes-ramtest/rtl/sdram_addon_port.v'
        memory = ROOT / 'cores/fes-pcxt/rtl/pcxt_memory.sv'
        capture = ROOT / 'cores/fes-ramtest/rtl/sdram_cas3_capture.v'
        sim = ROOT / 'cores/fes-pcxt/sim'
        with tempfile.TemporaryDirectory(prefix='pcxt-capture-pipeline-') as directory:
            work = Path(directory)
            mutant = work / 'without-retention.sv'
            text = capture.read_text()
            guard = 'if (read_window[3]) read_word <= captured;'
            self.assertEqual(text.count(guard), 1)
            mutant.write_text(text.replace(guard, 'read_word <= captured;'))
            late = work / 'late-handoff.sv'
            late.write_text(text.replace(guard, 'if (read_window[2]) read_word <= captured;'))
            for name, selected, expected in [('positive', capture, 0), ('without-retention', mutant, 1),
                                              ('wrong-cycle', late, 1)]:
                with self.subTest(case=name):
                    obj = work / name
                    command = [VERILATOR, '--cc', '--exe', '--build', '--top-module', 'pipeline_top',
                               '-Wno-fatal', '-j', '2', '--Mdir', str(obj), str(controller), str(memory), str(selected),
                               str(sim/'memory_top.sv'), str(sim/'capture_pipeline_top.sv'),
                               str(sim/'capture_pipeline_tb.cpp')]
                    build = subprocess.run(command, capture_output=True, text=True, timeout=120)
                    self.assertEqual(build.returncode, 0, build.stdout + build.stderr)
                    result = subprocess.run([str(obj/'Vpipeline_top')], capture_output=True,
                                            text=True, timeout=30)
                    self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
                    if expected:
                        self.assertIn('handoff selected wrong pin word' if name == 'wrong-cycle'
                                      else 'frontend lane mismatch', result.stderr)
                    else:
                        self.assertIn('"status":"PASS"', result.stdout)
                        self.assertIn('"frontend_deadline_ps":83750', result.stdout)
