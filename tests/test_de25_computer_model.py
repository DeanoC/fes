"""Nano model selection uses captured firmware and rejects changed build inputs."""
import copy
import importlib.util
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
HERE = ROOT / 'sources/misteross/prototypes/de25_board_control'
spec = importlib.util.spec_from_file_location('de25_computer_model_test_subject', HERE/'computer_model.py')
model = importlib.util.module_from_spec(spec)
spec.loader.exec_module(model)
CAPTURED = '79d616e85e19d65f0d645b67a85ffe7e142dcdfc'


class ComputerModelTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.sources = {model.ROM_PATH + name: subprocess.check_output(
            ['git', 'show', CAPTURED + ':' + model.ROM_PATH + name], cwd=ROOT)
            for name in model.COMBINED_SHA}

    def test_zx80_installs_its_captured_rom_in_the_real_decoder_window(self):
        original = model.rom_images('zx81', self.sources)
        selected = model.rom_images('zx80', self.sources)
        self.assertEqual(original['zx8x.hex'], self.sources[model.ROM_PATH+'zx8x.hex'])
        combined = bytes.fromhex(original['zx8x.hex'].decode())
        image = bytes.fromhex(selected['zx8x.hex'].decode())
        self.assertEqual(image[:4096], combined[8192:12288])
        self.assertEqual(image[4096:], b'\xff' * 12288)
        self.assertNotEqual(image[:4096], combined[:4096])
        words = re.findall(rb'(?m)^([0-9A-F]{4}): ([0-9A-F]{2});$', selected['zx8x.mif'])
        self.assertEqual([int(address, 16) for address, _ in words], list(range(16384)))
        self.assertEqual(bytes(int(value, 16) for _, value in words), image)

    def test_unreviewed_firmware_and_model_cannot_select_a_zx80_rom(self):
        for name in model.COMBINED_SHA:
            changed = dict(self.sources)
            changed[model.ROM_PATH+name] += b'\n'
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'reviewed captured'):
                model.rom_images('zx80', changed)
        for bad in (None, True, [], 'ZX80', 'ts1000', 'zx80=0'):
            with self.subTest(model=bad), self.assertRaises(ValueError):
                model.rom_images(bad, self.sources)

    def test_synthesis_simulation_and_macros_must_match_selection(self):
        for selected in model.MODELS:
            with self.subTest(model=selected), tempfile.TemporaryDirectory() as directory:
                output = Path(directory)
                project = output/'hps/project'
                (project/'fes_mailbox').mkdir(parents=True)
                qsf = project/'golden_top.qsf'
                qsf.write_text('set_global_assignment -name SEED 1\n')
                inputs = {'computer': {'source_sha256': {}}}
                model.install(output, inputs, self.sources, selected)
                self.assertEqual(model.verify(project, inputs, self.sources), selected)
                baseline = {p: p.read_bytes() for p in project.rglob('*') if p.is_file()}
                for path, original in baseline.items():
                    path.write_bytes(original + (b'\nset_global_assignment -name VERILOG_MACRO ZX81_MODEL_TS1500=1\n'
                                                 if path == qsf else b'00\n'))
                    with self.subTest(changed=str(path)), self.assertRaises(ValueError):
                        model.verify(project, inputs, self.sources)
                    path.write_bytes(original)
                changed = copy.deepcopy(inputs)
                changed['computer_model'] = 'zx80' if selected == 'zx81' else 'zx81'
                with self.assertRaises(ValueError):
                    model.verify(project, changed, self.sources)
                changed = copy.deepcopy(inputs)
                changed['computer']['source_sha256']['zx8x.hex'] = '0' * 64
                with self.assertRaises(ValueError):
                    model.verify(project, changed, self.sources)
                if selected == 'zx80':
                    with self.assertRaisesRegex(ValueError, 'already selects'):
                        model.install(output, inputs, self.sources, selected)

    def test_captured_z80_machine_decoder_nmi_and_rom_window(self):
        # Exercise the actual frozen CPU/machine, rather than a Python model of
        # its selection. A probe ROM discriminates the ROM window and NMI path.
        core = 'sources/misteross/cores/fes-zx81/'
        z80 = 'sources/misteross/cores/fes-common/rtl/z80/'
        files = [core+'rtl/'+name for name in ('zx81_machine.sv', 'zx81_machine_clock.v',
                 'zx81_keyboard.v', 'zx81_dpram.v', 'zx81_model.vh')]
        files += [z80+name+'.sv' for name in ('fes_z80_alu', 'fes_z80_engine', 'fes_z80_bus', 'fes_z80_nmos')]
        with tempfile.TemporaryDirectory(prefix='fes-de25-models-') as directory:
            source = Path(directory)
            for name in files:
                (source/Path(name).name).write_bytes(subprocess.check_output(
                    ['git', 'show', CAPTURED+':'+name], cwd=ROOT))
            for selected in model.MODELS:
                objects = source/selected
                command = ['verilator', '--cc', '--exe', '--build', '-j', '2',
                           '--top-module', 'zx81_machine', '-Wno-fatal', '--Mdir', str(objects),
                           '-I'+str(source), '-GEXTERNAL_RAM=1', '-GCLOCK_NUMERATOR=13',
                           '-GCLOCK_DENOMINATOR=200',
                           '-GFIRMWARE_INIT="'+str(ROOT/core/'sim/variant_probe.hex')+'"',
                           *('-D'+macro for macro in model.macros(selected)),
                           *(str(source/Path(name).name) for name in files if not name.endswith('.vh')),
                           str(ROOT/core/'sim/variant_tb.cpp')]
                built = subprocess.run(command, capture_output=True, text=True, timeout=90)
                self.assertEqual(built.returncode, 0, built.stdout+'\n'+built.stderr)
                ran = subprocess.run([str(objects/'Vzx81_machine'), selected],
                                     capture_output=True, text=True, timeout=15)
                self.assertEqual(ran.returncode, 0, ran.stdout+'\n'+ran.stderr)
                self.assertIn('FES ZX81 variant '+selected, ran.stdout)
                print(ran.stdout.strip(), flush=True)


if __name__ == '__main__':
    unittest.main()
