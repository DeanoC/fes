"""Execute the diagnostic's actual task runner with a local fake compiler."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('de25_bounded_compile', ROOT/'prototypes/de25_board_control/build.py')
build = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build)


class BoundedCompileTests(unittest.TestCase):
    def fixture(self, root):
        output=root/'build';project=output/'project';project.mkdir(parents=True)
        (output/'inputs.json').write_text('{"fes_revision":"host-test"}')
        (output/'quartus-version.txt').write_text('host-test compiler; no FPGA output')
        (project/'clocks.sdc').write_text('source constraint fixture\n')
        tool=root/'compiler';tool.write_text('''#!/usr/bin/env python3
import json,os,subprocess,sys,time
from pathlib import Path
task=sys.argv[-1]
with Path('calls.jsonl').open('a') as f:f.write(json.dumps({'task':task,'pid':os.getpid()})+'\\n')
print('executing '+task,flush=True)
if task==os.environ.get('FES_TEST_FORK'):
    worker=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'])
    Path('worker.pid').write_text(str(worker.pid))
    raise SystemExit(9)
if task==os.environ.get('FES_TEST_STALL'):time.sleep(30)
if task==os.environ.get('FES_TEST_FAIL'):raise SystemExit(9)
if task=='assembler':
    Path('output_files').mkdir()
    Path('output_files/golden_top.sof').write_bytes(b'host-only synthetic output')
    Path('output_files/golden_top.sta.summary').write_text('synthetic report')
''');tool.chmod(0o700)
        return output,project,tool

    def test_actual_runner_keeps_every_standard_task_and_reports_the_output(self):
        with tempfile.TemporaryDirectory() as temp:
            output,project,tool=self.fixture(Path(temp))
            artifact=build.compile_project(output,tool)
            calls=[json.loads(line) for line in (project/'calls.jsonl').read_text().splitlines()]
            expected=['dni_ipgenerate','dni_elaboration','dni_synthesis','fitter_plan','fitter_place',
                      'fitter_route','fitter_retime','fitter_finalize','sta_signoff','assembler']
            self.assertEqual([call['task'] for call in calls],expected)
            record=json.loads((output/'build.json').read_text())
            self.assertEqual(record['sha256'],build.digest(artifact.read_bytes()))
            self.assertEqual(record['phase_limit_seconds'],120)
            self.assertEqual(record['hardware_acceptance'],'not-run')
            self.assertIn('clocks.sdc',record['generated_handoff_and_sdc_sha256'])
            for task,row in zip(expected,record['commands']):
                self.assertEqual(row['argv'][-4:],['-start',task,'-end',task])
                self.assertEqual(row['returncode'],0)
                self.assertEqual(row['log_sha256'],build.digest((output/('compile-'+task+'.log')).read_bytes()))

    def test_failed_route_stops_without_later_tasks_artifact_or_success_receipt(self):
        with tempfile.TemporaryDirectory() as temp:
            output,project,tool=self.fixture(Path(temp))
            with patch.dict(os.environ,FES_TEST_FAIL='fitter_route'):
                with self.assertRaises(subprocess.CalledProcessError):build.compile_project(output,tool)
            calls=[json.loads(line)['task'] for line in (project/'calls.jsonl').read_text().splitlines()]
            self.assertEqual(calls[-1],'fitter_route')
            self.assertEqual(calls.count('fitter_route'),1)
            self.assertFalse((output/'build.json').exists())
            self.assertFalse((project/'output_files/golden_top.sof').exists())

    def test_expired_phase_is_reaped_without_replay_or_success_receipt(self):
        with tempfile.TemporaryDirectory() as temp:
            output,project,tool=self.fixture(Path(temp))
            with patch.dict(os.environ,FES_TEST_STALL='dni_ipgenerate'),patch.object(build,'PHASE_LIMIT_SECONDS',.5):
                with self.assertRaises(subprocess.TimeoutExpired):build.compile_project(output,tool)
            calls=[json.loads(line) for line in (project/'calls.jsonl').read_text().splitlines()]
            self.assertEqual([call['task'] for call in calls],['dni_ipgenerate'])
            with self.assertRaises(ProcessLookupError):os.kill(calls[0]['pid'],0)
            self.assertFalse((output/'build.json').exists())
            self.assertIn('executing dni_ipgenerate',(output/'compile-dni_ipgenerate.log').read_text())

    def test_failed_shell_cannot_leave_its_worker_running(self):
        with tempfile.TemporaryDirectory() as temp:
            output,project,tool=self.fixture(Path(temp))
            with patch.dict(os.environ,FES_TEST_FORK='dni_ipgenerate'):
                with self.assertRaises(subprocess.CalledProcessError):build.compile_project(output,tool)
            pid=int((project/'worker.pid').read_text());status=Path('/proc')/str(pid)/'stat'
            until=time.monotonic()+1
            while status.exists():
                try:state=status.read_text().split(') ',1)[1][0]
                except FileNotFoundError:break
                if state=='Z':break
                self.assertLess(time.monotonic(),until,'owned compiler worker survived failure')
                time.sleep(.01)
            self.assertFalse((output/'build.json').exists())


if __name__=='__main__':unittest.main()
