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
        (output/'inputs.json').write_text('{"fes_revision":"host-test","compile_processors":2}')
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
            self.assertEqual(record['parallel_processors'],2)
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

    def test_explicit_processors_change_only_selection_and_bind_the_inputs(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);hps=root/'hps';(hps/'project').mkdir(parents=True)
            qsf=hps/'project/golden_top.qsf'
            original='set_global_assignment -name SEED 1\nset_global_assignment -name NUM_PARALLEL_PROCESSORS 2\nset_global_assignment -name SDC_FILE clocks.sdc\n'
            qsf.write_text(original);(hps/'inputs.json').write_text('{}')
            build.configure_processors(root,16)
            self.assertEqual(qsf.read_text(),original.replace('PROCESSORS 2','PROCESSORS 16'))
            self.assertEqual(json.loads((hps/'inputs.json').read_text())['compile_processors'],16)
            with self.assertRaises(ValueError):build.configure_processors(root,16)
            for bad in (0,17,True,2.0):
                with self.subTest(bad=bad),self.assertRaises(ValueError):build.configure_processors(root,bad)
            qsf.write_text(original+original)
            with self.assertRaises(ValueError):build.configure_processors(root,16)

    def test_ddr_parameter_review_requires_exact_port_change_and_tool_defaults(self):
        import copy
        import xml.etree.ElementTree as ET
        spec=importlib.util.spec_from_file_location('ddr_review_hps',ROOT/'boards/de25nano/hps/build.py')
        hps=importlib.util.module_from_spec(spec);spec.loader.exec_module(hps)
        ip='http://www.accellera.org/XMLSchema/IPXACT/1685-2014'
        altera='http://www.altera.com/XMLSchema/IPXact2014/extensions'
        name='hps_subsys/ip/hps_subsys/agilex_hps.ip'
        adapter='hps_subsys/ip/hps_subsys/f2sdram_adapter.ip'
        emif='hps_subsys/ip/qsys_top/emif_io96b_hps.ip'
        before={name:{'f2s_data_width':'256','clock':'100'},adapter:{'width':'256'},emif:{'memory_width':'32'}}
        actual=copy.deepcopy(before);actual[name]['f2s_data_width']='0'
        actual[name].update({key:'true' for key in ('ED_en_sim_gen','FPGA_EMAC0_tx_clk_en','FPGA_EMAC1_tx_clk_en','FPGA_EMAC2_tx_clk_en','FPGA_EMAC_PTP_clk_en')})
        for fault in (None,'missing-change','wrong-change','hps-clock','ddr-width','emif-width','missing-original','extra-default','missing-default','wrong-default','missing-inventory'):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as temp:
                output=Path(temp);original=copy.deepcopy(before);selected=copy.deepcopy(actual)
                if fault=='missing-change':selected[name]['f2s_data_width']='256'
                elif fault=='wrong-change':selected[name]['f2s_data_width']='128'
                elif fault=='hps-clock':selected[name]['clock']='200'
                elif fault=='ddr-width':selected[adapter]['width']='128'
                elif fault=='emif-width':selected[emif]['memory_width']='16'
                elif fault=='missing-original':del selected[emif]['memory_width']
                elif fault=='extra-default':selected[name]['unknown']='true'
                elif fault=='missing-default':del selected[name]['ED_en_sim_gen']
                elif fault=='wrong-default':selected[name]['FPGA_EMAC_PTP_clk_en']='false'
                elif fault=='missing-inventory':del original[emif]
                inputs=json.dumps({'vendor_hps_emif_parameters':original})
                (output/'inputs.json').write_text(inputs)
                for relative,parameters in selected.items():
                    root=ET.Element('{'+ip+'}component')
                    extension=ET.SubElement(root,'{'+altera+'}altera_module_parameters')
                    container=ET.SubElement(extension,'{'+ip+'}parameters')
                    for key,value in parameters.items():
                        p=ET.SubElement(container,'{'+ip+'}parameter',parameterId=key)
                        ET.SubElement(p,'{'+ip+'}value').text=value
                    path=output/'project'/relative;path.parent.mkdir(parents=True,exist_ok=True)
                    ET.ElementTree(root).write(path)
                if fault:
                    with self.assertRaisesRegex(ValueError,'HPS/EMIF parameter'):
                        build.review_ddr_parameters(output,hps)
                else:
                    result=build.review_ddr_parameters(output,hps)
                    self.assertEqual(result['changed_original_module_parameters'],{name:{'f2s_data_width':{'before':'256','after':'0'}}})
                    self.assertEqual(result['boot_firmware_compatibility'],'not-qualified')
                    self.assertEqual(result,json.loads((output/'hps-emif-parameter-review.json').read_text()))
                self.assertEqual((output/'inputs.json').read_text(),inputs)

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
