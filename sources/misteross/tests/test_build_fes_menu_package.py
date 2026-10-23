import importlib
import json
from unittest.mock import patch
import tempfile
import tomllib
import unittest
from pathlib import Path

class MenuPackageProducerTest(unittest.TestCase):
    def setUp(self):
        self.producer = importlib.import_module('scripts.build_fes_menu_package')

    def test_commands_select_controlled_top_and_qualified_tools(self):
        p = self.producer
        commands = p.build_commands('0123456789abcdef0123456789abcdef', {'yosys':Path('/yosys'),'nextpnr-mistral':Path('/nextpnr')},seed=4)
        synth = commands[0][-1]
        self.assertIn('cores/fes-menu/rtl/menu_top.v',synth)
        self.assertNotIn('cores/fes-menu/rtl/top.v',synth)
        self.assertIn('fes_application_gp.v',synth)
        self.assertIn('fes_menu_control.v',synth)
        for blitter in ('fes_menu_blit_control.v','fes_menu_blitter.v','fes_menu_blit_read.v','fes_menu_blend.v','fes_menu_blit_alpha.v'):
            self.assertIn(blitter,synth)
        self.assertIn("BUILD_ID 128'h0123456789abcdef0123456789abcdef",synth)
        self.assertIn(p.MENU_TOOLCHAIN_LOCK, p.INPUTS)
        self.assertIn('--gpu-device',commands[1]); self.assertEqual(commands[1][commands[1].index('--gpu-device')+1],'0')
        default_route = p.build_commands('0'*32, {'yosys':Path('/yosys'),'nextpnr-mistral':Path('/nextpnr')})[1]
        self.assertEqual(default_route[default_route.index('--seed')+1], '5')
        leased_route = p.build_commands('0'*32, {'yosys':Path('/yosys'),'nextpnr-mistral':Path('/nextpnr')},
                                        gpu_device=1)[1]
        self.assertEqual(leased_route[leased_route.index('--gpu-device')+1], '1')
        self.assertEqual(p.PLACER_SEEDS, (5, 1, 2, 3, 4, 6, 7, 8))
        self.assertEqual(p.seed_order(4), (4, 5, 1, 2, 3, 6, 7, 8))
        self.assertEqual(p.PLACER_QOR_CLOCKS, ((None, 74.25),))
        self.assertIn('cores/fes-common/generated/fes_application.vh',p.INPUTS)
        for invalid in (0,9,True):
            with self.assertRaises(ValueError): p.build_commands('0'*32,{'yosys':Path('/yosys'),'nextpnr-mistral':Path('/nextpnr')},seed=invalid)

    def test_manifest_is_described_idle_not_playable(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root/self.producer.OUTPUT).mkdir(parents=True)
            (root/self.producer.OUTPUT/'core.rbf').write_bytes(b'synthetic-only')
            fields = tomllib.loads(self.producer.create_manifest(root,'https://github.com/DeanoC/fes','a'*40,b'{"recipe_sha256":"'+b'a'*64+b'","tools":{"yosys":"synthetic"}}').decode())
        self.assertEqual(fields['core']['id'],'fes.menu')
        self.assertNotIn('system',fields['core'])
        self.assertEqual(fields['abi']['id'],'fes.application')
        self.assertEqual(fields['target']['programming_profile'],'fes-gp-v1')
        self.assertEqual({i['id'] for i in fields['interfaces'] if i['required']},{'fes.video.fixed-720p60','fes.memory.hps-ddr','fes.video.menu-display','fes.video.menu-blitter'})
        self.assertTrue(all(i['required'] for i in fields['interfaces']))

    def test_package_routes_first_pass_ladder(self):
        p = self.producer
        invocation = type('Invocation', (), {'env': {}, 'gpu_device': 0})()
        with patch.object(p, 'route_after_synth') as route:
            p._route_placement(p.ROOT, p.ROOT/p.OUTPUT, Path('/nextpnr'), invocation, 5)
        options = route.call_args.kwargs
        self.assertEqual(options['seeds'], p.PLACER_SEEDS)
        self.assertEqual(options['mode'], 'first-pass')
        self.assertEqual(options['required'], ((None, 74.25),))
        self.assertEqual(options['budget'], 8)
        self.assertEqual(options['timeout'], 1800)
        self.assertEqual(options['core_id'], 'fes.menu')

    def test_package_requires_gp_without_weakening_diagnostics(self):
        from scripts import build_fes_menu as menu
        from scripts.fes_build_common import BuildError
        from tests import test_fes_menu as ddr
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            ddr.MenuDDRProducerTests().outputs(output)
            with self.assertRaises(BuildError):
                menu.validate_build_evidence(output,self.producer.ROOT,mode='ddr',menu_gp=True)
            for name in ('synth.json','routed.json'):
                path = output/name; data = json.loads(path.read_text())
                data['modules']['top']['cells']['gp'] = {'type':'cyclonev_hps_interface_mpu_general_purpose'}
                data['modules']['top']['cells']['ddr']['connections']['cmd_data_2'] = ['0']*60
                path.write_text(json.dumps(data))
            path = output/'timing.json'; data = json.loads(path.read_text())
            data['utilization']['cyclonev_hps_interface_mpu_general_purpose']['used'] = 1
            path.write_text(json.dumps(data))
            menu.validate_build_evidence(output,self.producer.ROOT,mode='ddr',menu_gp=True)
            with self.assertRaises(BuildError): menu.validate_build_evidence(output,self.producer.ROOT,mode='ddr')
            # The blitter may read and write port 1 and read port 2; nothing else.
            path = output/'routed.json'; data = json.loads(path.read_text())
            connections = data['modules']['top']['cells']['ddr']['connections']
            connections.update(cmd_valid_1=['1'], wr_valid_2=['1'], cmd_valid_2=['1'],
                               cmd_data_1=['1','1']+['0']*58, cmd_data_2=['1','0']+['0']*58)
            path.write_text(json.dumps(data))
            menu.validate_build_evidence(output,self.producer.ROOT,mode='ddr',menu_gp=True)
            for port, value in (('wr_valid_0',['1']),('wr_valid_1',['1']),('wr_valid_3',['1']),('cmd_valid_3',['1']),
                                ('cmd_data_2',['1','1']+['0']*58),('cmd_data_0',['1','1']+['0']*58)):
                with self.subTest(port=port):
                    changed = json.loads(json.dumps(data))
                    changed['modules']['top']['cells']['ddr']['connections'][port] = value
                    path.write_text(json.dumps(changed))
                    with self.assertRaisesRegex(BuildError,'tied low'):
                        menu.validate_build_evidence(output,self.producer.ROOT,mode='ddr',menu_gp=True)

    def test_build_rejects_excluded_markdown_read(self):
        from scripts.compiler_read_audit import ReadAuditError
        def read_markdown(*args, **kwargs):
            (self.producer.ROOT/'README.md').read_bytes()
        with patch.object(self.producer.board,'_require_clean_source',side_effect=read_markdown):
            with self.assertRaises(ReadAuditError): self.producer.build(self.producer.ROOT)
