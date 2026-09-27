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

    def test_build_python_reads_are_guarded_in_both_modes(self):
        from unittest.mock import patch
        from scripts import compiler_read_audit as audit
        for mode in ('test-pattern','ddr'):
            with self.subTest(mode=mode):
                def inspect_guard(*args,**kwargs):
                    active=audit._ACTIVE.get()
                    self.assertIsNotNone(active,'Python producer source audit is inactive')
                    self.assertEqual(active[0],ROOT.resolve())
                    self.assertIn('scripts',active[1])
                    raise RuntimeError('audited entry confirmed')
                with patch.object(menu.board,'_require_clean_source',side_effect=inspect_guard):
                    with self.assertRaisesRegex(RuntimeError,'audited entry confirmed'):
                        menu.build(ROOT,mode=mode)

    def test_build_rejects_excluded_source_markdown_reads(self):
        from unittest.mock import patch
        from scripts.compiler_read_audit import ReadAuditError
        for mode in ('test-pattern','ddr'):
            with self.subTest(mode=mode):
                with patch.object(menu.board,'_require_clean_source',
                        side_effect=lambda *a,**kw:(ROOT/'cores/fes-menu/README.md').read_bytes()):
                    with self.assertRaises(ReadAuditError):
                        menu.build(ROOT,mode=mode)

    def test_explicit_seed_is_bounded_and_used_by_router(self):
        tools={'yosys':Path('/auth/yosys'),'nextpnr-mistral':Path('/auth/nextpnr')}
        _,route=menu.build_commands(tools,mode='ddr',seed=4)
        self.assertEqual(route[route.index('--seed')+1],'4')
        for seed in (0,9,True):
            with self.assertRaises(ValueError):menu.build_commands(tools,mode='ddr',seed=seed)
