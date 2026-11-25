"""The DE25 pc386 derivation makes DMA controller 2 its register file; the DE10 source keeps the full controller."""
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('de25_pc386_derive', ROOT / 'boards/de25nano/pc386/derive.py')
D = importlib.util.module_from_spec(spec)
spec.loader.exec_module(D)
AT_CORE = ROOT / 'cores/fes-pc386/rtl/pc386_at_core.sv'


class Dma2DeriveTests(unittest.TestCase):
    def test_the_de10_source_keeps_the_full_controller(self):
        source = AT_CORE.read_bytes()
        self.assertEqual(source.count(D.AT_DMA2_BEFORE), 1)
        self.assertNotIn(b'pc386_dma2_registers', source)

    def test_the_de25_at_core_has_the_register_file(self):
        source = AT_CORE.read_bytes()
        result, record = D.retarget_at_core(source)
        self.assertNotIn(D.AT_DMA2_BEFORE, result)
        self.assertEqual(result.count(b'pc386_dma2_registers dma2 ('), 1)
        self.assertEqual(result.count(b'fes_dma8237 #('), 1)  # controller 1 stays
        self.assertEqual(record['source_sha256'], D.sha(source))
        self.assertEqual(record['derived_sha256'], D.sha(result))

    def test_a_moved_instance_is_refused(self):
        source = AT_CORE.read_bytes().replace(b'.cycle_rdata(8\'hff), .cycle_fault(2\'b00)', b'.cycle_rdata(8\'h00), .cycle_fault(2\'b00)')
        with self.assertRaisesRegex(ValueError, 'controller 2 instance changed'):
            D.retarget_at_core(source)

    def test_the_build_and_sim_carry_the_module(self):
        board = (ROOT / 'boards/de25nano/pc386/build.py').read_text()
        self.assertIn('"pc386_dma2_registers.sv")', board)
        self.assertIn('derive.retarget_at_core', board)
        self.assertIn("('pc386_at_core.sv', derive.retarget_at_core)", (ROOT / 'boards/de25nano/pc386/sim.py').read_text())
        module = (ROOT / 'boards/de25nano/pc386/pc386_dma2_registers.sv').read_text()
        self.assertIn('module pc386_dma2_registers', module)
        self.assertIn('channels 4-7', module)


if __name__ == '__main__':
    unittest.main()
