"""DDR diagnostic producer contract, separate from pattern-only evidence."""
import json
from pathlib import Path
import tempfile
import unittest
from scripts import build_fes_menu as menu
from scripts.fes_build_common import BuildError
from tests import test_build_fes_menu as pattern
from tests import test_build_fes_splash as splash
ROOT=Path(__file__).resolve().parents[1]
class MenuDDRProducerTests(unittest.TestCase):
    def test_selects_shared_ddr_and_generated_window_with_qualified_tools(self):
        inputs=menu.inputs_for('ddr')
        for path in ('cores/fes-common/rtl/fes_hps_ddr.v','cores/fes-common/rtl/fes_hps_ddr_guard.v','cores/fes-common/generated/fes_application.vh','toolchains/ramtest.lock'):
            self.assertIn(path,inputs)
        synth,route=menu.build_commands({'yosys':Path('/auth/yosys'),'nextpnr-mistral':Path('/auth/nextpnr')},mode='ddr')
        self.assertIn('TEST_PATTERN 0',synth[-1])
        self.assertIn('cores/fes-common/generated',synth[-1])
        self.assertEqual(route[route.index('--rbf')+1],'build/oss/fes-menu/core.rbf')
        self.assertEqual(route[route.index('--gpu-device')+1],'0')
    def outputs(self, output):
        pattern.MenuPatternProducerTests().outputs(output)
        for filename in ('synth.json','routed.json'):
            p=output/filename;data=json.loads(p.read_text())
            data['modules']['top']['cells']['ddr']=splash.SplashProducerTests.layout_cell(cmd_data_0=['0']*60)
            p.write_text(json.dumps(data))
        p=output/'timing.json';data=json.loads(p.read_text())
        data['utilization']['cyclonev_hps_interface_fpga2sdram']={'available':1,'used':1}
        p.write_text(json.dumps(data))
    def test_matching_layout_required_in_both_graphs(self):
        for filename in ('synth.json','routed.json'):
            with self.subTest(filename=filename),tempfile.TemporaryDirectory() as d:
                output=Path(d);self.outputs(output)
                result=menu.validate_build_evidence(output,ROOT,mode='ddr')
                self.assertEqual(result['hps_ddr']['interface'],'fes.memory.hps-ddr')
                p=output/filename;data=json.loads(p.read_text())
                bits=data['modules']['top']['cells']['ddr']['connections']['cfg_port_width']
                bits[0]='0' if bits[0]=='1' else '1';p.write_text(json.dumps(data))
                with self.assertRaises(BuildError):menu.validate_build_evidence(output,ROOT,mode='ddr')
    def test_unknown_mode_rejected(self):
        with self.assertRaises(ValueError):menu.inputs_for('other')

    def test_writes_or_unused_ports_rejected(self):
        for port in ('wr_valid_0','cmd_valid_1'):
            with self.subTest(port=port),tempfile.TemporaryDirectory() as d:
                output=Path(d);self.outputs(output)
                p=output/'routed.json';data=json.loads(p.read_text())
                data['modules']['top']['cells']['ddr']['connections'][port]=['1']
                p.write_text(json.dumps(data))
                with self.assertRaisesRegex(BuildError,'tied low'):
                    menu.validate_build_evidence(output,ROOT,mode='ddr')
