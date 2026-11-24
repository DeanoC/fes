"""The DE25's i486DX derivation and forks (boards/de25nano/pc386/i486dx)."""
from pathlib import Path
import importlib.util
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from scripts import yosys_netlist  # noqa: E402


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, ROOT / path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


dx = load('boards/de25nano/pc386/i486dx/derive.py', 'test_i486dx_derive')
board = load('boards/de25nano/pc386/derive.py', 'test_pc386_derive')


class I486dxDeriveTest(unittest.TestCase):
    def test_the_cpu_derives_with_every_anchor(self):
        data, record = dx.derive_cpu((ROOT / dx.CPU_UPSTREAM).read_bytes())
        text = data.decode()
        self.assertEqual(re.findall(r'^module (\w+)', text, re.M), ['fes_i486dx_cpu'])
        self.assertIn('    fes_i486dx_l1i l1i (\n', text)
        self.assertIn('    fes_i486dx_l1d l1d (\n', text)
        self.assertNotIn('fes_i386_l1i l1i', text)
        self.assertIn("d_out.ldfwd = 1'b0;", text)
        self.assertEqual(len(record['changes']), len(dx.CPU_EDITS))

    def test_the_benches_form_keeps_the_upstream_name_and_every_other_edit(self):
        source = (ROOT / dx.CPU_UPSTREAM).read_bytes()
        bench, _ = dx.derive_cpu(source, rename=False)
        synth, _ = dx.derive_cpu(source)
        self.assertEqual(re.findall(r'^module (\w+)', bench.decode(), re.M), ['fes_i386_cpu'])
        self.assertEqual(bench.replace(b'module fes_i386_cpu #(', b'module fes_i486dx_cpu #('), synth)

    def test_a_moved_anchor_fails(self):
        source = (ROOT / dx.CPU_UPSTREAM).read_bytes()
        with self.assertRaisesRegex(ValueError, 'anchor'):
            dx.derive_cpu(source.replace(dx.LDFWD_BEFORE, dx.LDFWD_BEFORE.replace(b'SIZE_32', b'SIZE_16')))

    def test_the_forks_are_synced_with_their_upstream(self):
        dx.check_forks(ROOT)
        for upstream, (fork, _) in dx.FORKS.items():
            name = Path(upstream).stem.replace('fes_i386_', 'fes_i486dx_')
            text = (ROOT / fork).read_text()
            self.assertEqual(re.findall(r'^module (\w+)', text, re.M), [name])
            self.assertIn(upstream, text)  # the header names its upstream

    def test_the_source_list_replaces_only_the_cpu_and_the_l1s(self):
        swapped = dx.cpu_sources(yosys_netlist.CPU_SOURCES, '/derived/fes_i486dx_cpu.sv')
        changed = {old: new for old, new in zip(yosys_netlist.CPU_SOURCES, swapped) if old != new}
        self.assertEqual(set(changed), {dx.CPU_UPSTREAM, *dx.FORKS})
        self.assertEqual(changed[dx.CPU_UPSTREAM], '/derived/fes_i486dx_cpu.sv')

    def test_the_de10_cpu_closure_names_no_dx_file(self):
        self.assertFalse([path for path in yosys_netlist.CPU_SOURCES if 'i486dx' in path])

    def test_the_de25_system_instantiates_the_dx(self):
        data, _ = board.retarget_system((ROOT / 'cores/fes-pc386/rtl/pc386_system.sv').read_bytes())
        self.assertIn(b'    fes_i486dx_cpu #(.GOV_N(GOV_N), .GOV_D(GOV_D), .I486(I486)) cpu (\n', data)
        self.assertNotIn(b'fes_i386_cpu #(', data)


if __name__ == '__main__':
    unittest.main()
