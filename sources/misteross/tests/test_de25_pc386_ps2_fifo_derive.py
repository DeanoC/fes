"""The DE25 pc386 derivation gives pc386_kbc the one-write-port PS/2 FIFO; the DE10 source keeps its own."""
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('de25_pc386_derive', ROOT / 'boards/de25nano/pc386/derive.py')
D = importlib.util.module_from_spec(spec)
spec.loader.exec_module(D)
KBC = ROOT / 'cores/fes-pc386/rtl/pc386_kbc.sv'
MLAB = ROOT / 'boards/de25nano/pc386/pc386_ps2_fifo_mlab.sv'


class Ps2FifoDeriveTests(unittest.TestCase):
    def test_the_de10_source_keeps_its_fifo(self):
        source = KBC.read_bytes()
        self.assertEqual(source.count(D.KBC_FIFO_BEFORE), D.KBC_FIFOS)
        self.assertNotIn(b'pc386_ps2_fifo_mlab', source)
        self.assertNotIn(b'ramstyle', source)

    def test_the_de25_kbc_uses_the_mlab_fifo(self):
        source = KBC.read_bytes()
        result, record = D.retarget_kbc(source)
        self.assertNotIn(D.KBC_FIFO_BEFORE, result)
        self.assertEqual(result.count(D.KBC_FIFO_AFTER), 2)
        self.assertEqual(record['source_sha256'], D.sha(source))
        self.assertEqual(record['derived_sha256'], D.sha(result))

    def test_a_changed_de10_fifo_is_refused(self):
        source = KBC.read_bytes().replace(b'count <= {4\'b0, push};', b'count <= 5\'d0;', 1)
        with self.assertRaisesRegex(ValueError, 'FIFO module changed'):
            D.retarget_kbc(source)

    def test_a_moved_instance_is_refused(self):
        source = KBC.read_bytes().replace(D.KBC_FIFO_BEFORE, b'    pc386_ps2_fifo  fifo (', 1)
        with self.assertRaisesRegex(ValueError, 'FIFO instances changed'):
            D.retarget_kbc(source)

    def test_the_module_has_one_write_port(self):
        module = MLAB.read_text()
        self.assertIn('module pc386_ps2_fifo_mlab (', module)
        self.assertEqual(module.count('(* ramstyle = "MLAB" *)'), 5)
        self.assertEqual(module.count('[store_at] <='), 5)

    def test_the_build_and_sim_carry_the_module(self):
        board = (ROOT / 'boards/de25nano/pc386/build.py').read_text()
        self.assertIn('"pc386_ps2_fifo_mlab.sv")', board)
        self.assertIn('derive.retarget_kbc', board)
        sim = (ROOT / 'boards/de25nano/pc386/sim.py').read_text()
        self.assertIn("('pc386_kbc.sv', derive.retarget_kbc)", sim)
        self.assertIn("HERE / 'pc386_ps2_fifo_mlab.sv'", sim)


if __name__ == '__main__':
    unittest.main()
