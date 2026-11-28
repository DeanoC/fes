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

    def test_only_the_dx_system_instantiates_the_dx(self):
        sx, _ = board.retarget_system((ROOT / 'cores/fes-pc386/rtl/pc386_system.sv').read_bytes())
        self.assertIn(b'    fes_i386_cpu #(.GOV_N(GOV_N), .GOV_D(GOV_D), .I486(I486)) cpu (\n', sx)
        data, _ = board.retarget_system_cpu(sx)
        self.assertIn(b'    fes_i486dx_cpu #(.GOV_N(GOV_N), .GOV_D(GOV_D), .I486(I486)) cpu (\n', data)
        self.assertNotIn(b'fes_i386_cpu #(', data)
        self.assertEqual(data.replace(b'fes_i486dx_cpu #(', b'fes_i386_cpu #('), sx)

    def test_loaded_rearm_matches_the_qualified_presenter(self):
        source = (ROOT / board.PRESENTER_UPSTREAM).read_bytes()
        data, record = board.retarget_presenter_loaded(source)
        self.assertEqual(record["source_sha256"], board.sha(source))
        self.assertEqual(record["derived_sha256"], board.sha(data))
        self.assertEqual(record["derived_sha256"], "7437e4b680112d6d8e5fb7ecf5a0a65f92ba937148b9bfbdfe1550703bdf8167")
        self.assertEqual(record["loaded_rearm_cycles"], 8)
        with self.assertRaisesRegex(ValueError, "presenter upstream changed"):
            board.retarget_presenter_loaded(source + b"\n")


class DE25ModelsTest(unittest.TestCase):
    """Each DE25 pc386 model builds its own elaboration, and --model 486dx builds the DX or refuses."""

    @classmethod
    def setUpClass(cls):
        cls.build = load('boards/de25nano/pc386/build.py', 'test_pc386_build')
        cls.sources = cls.build.board.capture(cls.build, model='486dx')

    def model(self, model):
        files, cpu, _ = self.build.elaboration(self.sources, model)
        return files, cpu, self.build.machine_parameters(model)

    def test_the_models(self):
        self.assertEqual(tuple(self.build.MODELS), ('486sx', '486dx'))
        clock = self.build.model_clock('486dx')
        self.assertEqual((clock['c0'], clock['mhz']), (28, '111.904762'))
        self.assertNotEqual(clock['pll_ip'], self.build.model_clock('486sx')['pll_ip'])

    def test_the_486sx_is_fes_i386_cpu_at_i486_1_without_the_387(self):
        files, cpu, parameters = self.model('486sx')
        self.assertIn(b'    fes_i386_cpu #(.GOV_N(GOV_N), .GOV_D(GOV_D), .I486(I486)) cpu (\n', files['pc386_system.sv'])
        self.assertNotIn(b'fes_i486dx_cpu', files['pc386_system.sv'])
        self.assertIsNone(cpu)
        self.assertEqual(parameters['I486'], 1)
        self.assertNotIn('X87_PRESENT', parameters)

    def test_the_486dx_is_fes_i486dx_cpu_with_the_387(self):
        files, cpu, parameters = self.model('486dx')
        self.assertIn(b'    fes_i486dx_cpu #(.GOV_N(GOV_N), .GOV_D(GOV_D), .I486(I486)) cpu (\n', files['pc386_system.sv'])
        self.assertEqual(re.findall(rb'^module (\w+)', cpu, re.M), [b'fes_i486dx_cpu'])
        self.assertEqual((parameters['I486'], parameters['X87_PRESENT']), (1, 1))

    def test_the_models_differ_in_the_cpu_and_clock(self):
        sx, _, psx = self.model('486sx')
        dxf, _, pdx = self.model('486dx')
        self.assertEqual(set(dxf), set(sx))
        self.assertIn('pc386_presenter.sv', sx)
        for name in sx:
            if name != 'pc386_system.sv':
                self.assertEqual(sx[name], dxf[name], name)
        self.assertEqual(set(pdx) - set(psx), {'X87_PRESENT'})

    def test_each_model_records_its_identity(self):
        sx, ddx = self.build.record_parameters(ROOT, '486sx'), self.build.record_parameters(ROOT, '486dx')
        self.assertEqual((sx['x87_present'], 'cpu' in sx), (False, False))
        self.assertEqual((ddx['x87_present'], ddx['cpu']), (True, 'fes_i486dx_cpu'))
        self.assertEqual(sx['loaded_rearm_cycles'], board.LOADED_REARM_CYCLES)
        self.assertEqual(ddx['loaded_rearm_cycles'], board.LOADED_REARM_CYCLES)

    def calls(self, model):
        """What the board's own path (machine/board.py component_files) hands the elaboration for a model."""
        seen = []
        real = self.build.machine_netlist
        self.build.machine_netlist = lambda sources, files, model=None, cpu=None: (
            seen.append((model, files['pc386_system.sv'], files.get('pc386_presenter.sv'), cpu)) or (b'', {}))
        try:
            files, derived, _ = self.build.board.component_files(self.build, self.sources, model=model)
        finally:
            self.build.machine_netlist = real
        guard = 'fes_hps_ddr_guard.v'
        self.assertIn(b"reads_owed <= 12'd0; finishing <= 1'b0;", files[guard])
        self.assertEqual(derived[guard]['derived_sha256'], self.build.board.sha(files[guard]))
        return seen, derived

    def test_the_board_builds_the_dx_for_486dx(self):
        [(model, system, presenter, cpu)], derived = self.calls('486dx')
        self.assertEqual(model, '486dx')
        self.assertIn(b'fes_i486dx_cpu #(', system)
        self.assertIsNotNone(cpu)
        expected, record = board.retarget_presenter_loaded((ROOT / board.PRESENTER_UPSTREAM).read_bytes())
        self.assertEqual(presenter, expected)
        self.assertEqual(derived['pc386_presenter.sv'], record)
        [(model, system, presenter, cpu)], derived = self.calls('486sx')
        self.assertEqual(model, '486sx')
        self.assertIn(b'fes_i386_cpu #(', system)
        self.assertIsNone(cpu)
        self.assertEqual(presenter, expected)
        self.assertEqual(derived['pc386_presenter.sv'], record)

    def test_a_dx_is_never_elaborated_as_the_sx_nor_an_unknown_model_as_either(self):
        with self.assertRaisesRegex(ValueError, 'only it'):
            self.build.machine_netlist(self.sources, {}, '486dx', None)
        with self.assertRaisesRegex(ValueError, 'only it'):
            self.build.machine_netlist(self.sources, {}, '486sx', b'module x; endmodule')
        for call in (self.build.machine_parameters, self.build.model_clock, lambda m: self.build.elaboration(self.sources, m)):
            with self.assertRaises(ValueError):
                call('486dx2')


@unittest.skipUnless(shutil.which('verilator'), 'Verilator is required to elaborate the DE25 system bench')
class De25SystemBenchTest(unittest.TestCase):
    """boards/de25nano/pc386/sim.py, the DE25 system bench, elaborates the machine the DE25 producer
    (pc386/build.py) builds: the bench's own sim.verilate arguments, elaborated only, with no firmware build
    and no C++ build."""

    def bench_call(self, work, model):
        """The bench module and its sim.verilate arguments by name for a model, its derived sources written under
        work."""
        bench = load_bench()
        calls = []
        with mock.patch.object(bench.sim, 'OUTPUT', work), \
                mock.patch.object(bench.sim.firmware, 'images', return_value=None), \
                mock.patch.object(bench.sim.firmware, 'write_rom_hex', return_value=work / 'pc386-rom.hex'), \
                mock.patch.object(bench.sim, 'verilate', autospec=True,
                                  side_effect=lambda *args, **kwargs: calls.append((args, kwargs))):
            bench.system_testbench(SimpleNamespace(download=False, jobs=1, verilator='verilator', model=model))
        self.assertEqual(len(calls), 1)
        call = inspect.signature(bench.sim.verilate).bind(*calls[0][0], **calls[0][1])
        call.apply_defaults()
        return bench, call.arguments

    def elaborate(self, model):
        """pc386_system's numeric parameters and the module names the bench's own call elaborates for a model."""
        with tempfile.TemporaryDirectory() as work:
            work = Path(work)
            bench, call = self.bench_call(work, model)
            [presenter] = [path for path in call['rtl'] if path.name == 'pc386_presenter.sv']
            original = ROOT / board.PRESENTER_UPSTREAM
            expected, _ = board.retarget_presenter_loaded(original.read_bytes())
            self.assertEqual(presenter.read_bytes(), expected)
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
        return modules['pc386_system'][0], set(modules)

    def test_the_bench_elaborates_each_models_identity_and_cpu(self):
        producer = load('boards/de25nano/pc386/build.py', 'test_de25_pc386_build')
        forks = {Path(fork).stem for fork, _ in dx.FORKS.values()}
        integer = {dx.UPSTREAM_TOP, *(Path(upstream).stem for upstream in dx.FORKS)}
        for model in producer.MODELS:
            with self.subTest(model):
                # pc386_system's parameters the producer sets beyond its guest rates: the CPU identity and, for
                # the 486DX, the 387; the 486SX's X87_PRESENT is pc386_system's own 0.
                identity = {name: value for name, value in producer.machine_parameters(model).items()
                            if name not in producer.guest_rates(model)}
                identity.setdefault('X87_PRESENT', 0)
                system, modules = self.elaborate(model)
                for name, value in identity.items():
                    self.assertEqual(system.get(name), value, name)
                # Its guest rates are the model's own, as the producer synthesizes them, not the DE10's.
                for name, value in producer.guest_rates(model).items():
                    self.assertEqual(system.get(name), value, name)
                if model == producer.DX_MODEL:
                    self.assertEqual(identity['X87_PRESENT'], 1)
                    # The DX's CPU and its two-way L1s, and none of the integer core's.
                    self.assertLessEqual({dx.TOP, *forks}, modules)
                    self.assertFalse(integer & modules)
                else:
                    self.assertLessEqual(integer, modules)
                    self.assertFalse({dx.TOP, *forks} & modules)

class De25SystemBenchClockTest(unittest.TestCase):
    """The DE25 system bench runs system_tb at the model's own machine clock, not the DE10's 61.44 MHz."""

    def test_system_tb_gets_each_models_clock(self):
        bench = load_bench()
        producer = load('boards/de25nano/pc386/build.py', 'test_de25_pc386_build_clock')
        from scripts import pc386_quartus
        shared = pc386_quartus.SYSTEM_CLOCK_HZ
        seen = {}
        for model in producer.MODELS:
            with self.subTest(model):
                args = SimpleNamespace(model=model)
                with mock.patch.object(bench.sim, 'run_system',
                                       side_effect=lambda args, **kwargs: seen.setdefault(
                                           model, pc386_quartus.SYSTEM_CLOCK_HZ)) as run_system:
                    bench.run_at_model_clock(args)
                run_system.assert_called_once_with(args, expect_floppy=False)
                self.assertEqual(seen[model], producer.model_clock(model)['hz'])
                self.assertEqual(pc386_quartus.SYSTEM_CLOCK_HZ, shared)  # restored


if __name__ == '__main__':
    unittest.main()
