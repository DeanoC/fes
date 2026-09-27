from pathlib import Path
import json
import tempfile
import unittest
from scripts import build_fes_menu as menu
from scripts.fes_build_common import BuildError
from tests import test_build_fes_pong as pong

ROOT = Path(__file__).resolve().parents[1]

class MenuPatternProducerTests(unittest.TestCase):
    def test_commands_keep_real_fifo_and_gpu_zero(self):
        synth, route = menu.build_commands({'yosys': Path('/auth/yosys'), 'nextpnr-mistral': Path('/auth/nextpnr')})
        self.assertNotIn('-nobram', synth[-1])
        self.assertIn('fes_menu_reader.v', synth[-1])
        self.assertIn('fes_menu_pattern_memory.v', synth[-1])
        self.assertEqual(route[route.index('--gpu-device') + 1], '0')
        self.assertEqual(route[route.index('--rbf') + 1], 'build/oss/fes-menu-pattern/core.rbf')

    def outputs(self, output):
        pong.BuildFesPongTests()._write_passing_outputs(output)
        for name in ('synth.json', 'routed.json'):
            p = output / name; data = json.loads(p.read_text())
            cells = data['modules']['top']['cells']
            for key in list(cells):
                if cells[key]['type'] == 'cyclonev_hps_interface_mpu_general_purpose': del cells[key]
            p.write_text(json.dumps(data))
        p = output / 'timing.json'; data = json.loads(p.read_text())
        data['utilization']['cyclonev_hps_interface_mpu_general_purpose']['used'] = 0
        p.write_text(json.dumps(data))

    def test_evidence_rejects_actual_ddr_or_gp(self):
        for kind in ('cyclonev_hps_interface_fpga2sdram', 'cyclonev_hps_interface_mpu_general_purpose'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as directory:
                output = Path(directory); self.outputs(output)
                menu.validate_build_evidence(output, ROOT)
                p = output / 'synth.json'; data = json.loads(p.read_text())
                data['modules']['top']['cells']['unexpected'] = {'type': kind}
                p.write_text(json.dumps(data))
                with self.assertRaises(BuildError): menu.validate_build_evidence(output, ROOT)

    def test_evidence_rejects_asynchronous_fifo(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory); self.outputs(output)
            p = output / 'synth.json'; data = json.loads(p.read_text())
            data['modules']['top']['cells']['fifo'] = {
                'type': 'MISTRAL_M10K', 'parameters': {'CFG_ASYNC_READ': '1'}}
            p.write_text(json.dumps(data))
            with self.assertRaisesRegex(BuildError, 'synchronous M10K'):
                menu.validate_build_evidence(output, ROOT)

if __name__ == '__main__': unittest.main()
