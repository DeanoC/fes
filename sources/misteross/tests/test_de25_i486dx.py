"""The DE25's i486DX derivation and forks (boards/de25nano/pc386/i486dx), and the DE25 system bench's DX."""
from pathlib import Path
from types import SimpleNamespace
from unittest import mock
import importlib.util
import inspect
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ElementTree

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
CONST = re.compile(r"\d+'s?([bodh])([0-9a-fA-F_]+)")


def load_bench():
    """boards/de25nano/pc386/sim.py. It puts its own directory on sys.path and imports derive and i486dx from
    it by those bare names; both are undone once it is loaded, so no later test sees them."""
    path, modules = list(sys.path), set(sys.modules)
    try:
        return load('boards/de25nano/pc386/sim.py', 'test_de25_pc386_sim')
    finally:
        sys.path[:] = path
        for name in set(sys.modules) - modules:
            if name.split('.')[0] in ('derive', 'i486dx'):
                del sys.modules[name]


def elaborated(xml):
    """Verilator's --xml-only output: for each module, by its original name, the numeric parameter values of
    each of its elaborated copies."""
    modules = {}
    for module in ElementTree.parse(xml).getroot().iter('module'):
        values = {}
        for var in module.findall("var[@param='true']"):
            const = var.find('const')
            match = CONST.fullmatch(const.get('name', '')) if const is not None else None
            if match:
                values[var.get('name')] = int(match[2].replace('_', ''), {'b': 2, 'o': 8, 'd': 10, 'h': 16}[match[1]])
        modules.setdefault(module.get('origName'), []).append(values)
    return modules


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


@unittest.skipUnless(shutil.which('verilator'), 'Verilator is required to elaborate the DE25 system bench')
class De25SystemBenchTest(unittest.TestCase):
    """boards/de25nano/pc386/sim.py, the DE25 system bench, elaborates the machine the DE25 producer
    (pc386/build.py) builds: the bench's own sim.verilate arguments, elaborated only, with no firmware build
    and no C++ build."""

    def bench_call(self, work):
        """The bench module and its sim.verilate arguments by name, its derived sources written under work."""
        bench = load_bench()
        calls = []
        with mock.patch.object(bench.sim, 'OUTPUT', work), \
                mock.patch.object(bench.sim.firmware, 'images', return_value=None), \
                mock.patch.object(bench.sim.firmware, 'write_rom_hex', return_value=work / 'pc386-rom.hex'), \
                mock.patch.object(bench.sim, 'verilate', autospec=True,
                                  side_effect=lambda *args, **kwargs: calls.append((args, kwargs))):
            bench.system_testbench(SimpleNamespace(download=False, jobs=1, verilator='verilator'))
        self.assertEqual(len(calls), 1)
        call = inspect.signature(bench.sim.verilate).bind(*calls[0][0], **calls[0][1])
        call.apply_defaults()
        return bench, call.arguments

    def test_the_bench_elaborates_the_producers_identity_and_cpu(self):
        producer = load('boards/de25nano/pc386/build.py', 'test_de25_pc386_build')
        # pc386_system's parameters the producer sets beyond its guest rates: the CPU identity and the 387.
        identity = {name: value for name, value in producer.machine_parameters().items()
                    if name not in producer.guest_rates()}
        self.assertLessEqual({'I486', 'X87_PRESENT'}, set(identity))
        with tempfile.TemporaryDirectory() as work:
            work = Path(work)
            bench, call = self.bench_call(work)
            xml = work / 'bench.xml'
            # The bench's top, flags, sources, -G parameters and defines, elaborated only: -Wno-fatal leaves
            # lint to the bench's build, and a -G parameter the top does not declare is still an error.
            command = ['verilator', '--xml-only', '--xml-output', str(xml), '-Wno-fatal', '--top-module', call['top'],
                       *call['flags'],
                       *(f'-I{bench.sim.COMMON / path}' for path in ('vendor/kf8259', 'vendor/kf8253', 'generated')),
                       *(f'-G{name}={value}' for name, value in call['parameters'].items()),
                       *(f'-D{name}' for name in call['defines']), *map(str, call['rtl'])]
            result = subprocess.run(command, cwd=bench.ROOT, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout[-2000:] + result.stderr[-2000:])
            modules = elaborated(xml)
        self.assertEqual(len(modules['pc386_system']), 1)
        for name, value in identity.items():
            with self.subTest(name):
                self.assertEqual(modules['pc386_system'][0].get(name), value)
        # The DX's CPU and its two-way L1s, and none of the integer core's.
        forks = {Path(fork).stem for fork, _ in dx.FORKS.values()}
        self.assertLessEqual({dx.TOP, *forks}, set(modules))
        self.assertFalse({dx.UPSTREAM_TOP, *(Path(upstream).stem for upstream in dx.FORKS)} & set(modules))


if __name__ == '__main__':
    unittest.main()
