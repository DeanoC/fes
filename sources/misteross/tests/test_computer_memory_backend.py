from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(shutil.which('verilator'), 'Verilator is required for the real RTL identity test')
class ComputerMemoryBackendTests(unittest.TestCase):
    def test_reserved_window_is_independent_of_session_display(self):
        sources = (
            'cores/fes-common/sim/computer_memory_top.v',
            'cores/fes-common/rtl/fes_computer_mailbox.v',
            'cores/fes-zx81/rtl/fes_zx81_core.v',
            'cores/fes-zx81/rtl/zx81_keyboard.v',
            'cores/fes-zx81/rtl/zx81_tape_store.v',
            'cores/fes-zx81/rtl/zx81_video_720p.v',
            'cores/fes-zx81/rtl/zx81_dpram.v',
            'cores/fes-zx81/rtl/zx81_machine_clock.v',
            'cores/fes-zx81/rtl/zx81_machine.sv',
            'cores/fes-common/rtl/z80/fes_z80_alu.sv',
            'cores/fes-common/rtl/z80/fes_z80_engine.sv',
            'cores/fes-common/rtl/z80/fes_z80_bus.sv',
            'cores/fes-common/rtl/z80/fes_z80_nmos.sv',
        )
        warnings = ('UNUSEDSIGNAL', 'UNUSEDPARAM', 'PINCONNECTEMPTY', 'DECLFILENAME',
                    'WIDTHEXPAND', 'WIDTHTRUNC', 'SYNCASYNCNET', 'UNOPTFLAT',
                    'CASEINCOMPLETE', 'CASEX', 'VARHIDDEN')
        with tempfile.TemporaryDirectory(prefix='fes-computer-memory-') as directory:
            command = ['verilator', '--cc', '--exe', '--build', '--top-module', 'computer_memory_top',
                       '-Wall', *['-Wno-' + name for name in warnings],
                       '-I' + str(ROOT / 'cores/fes-common/generated'),
                       '-I' + str(ROOT / 'cores/fes-zx81/generated'),
                       '-I' + str(ROOT / 'cores/fes-zx81/rtl'),
                       '-I' + str(ROOT / 'cores/fes-common/rtl/z80'),
                       '--Mdir', directory, *[str(ROOT / name) for name in sources],
                       str(ROOT / 'cores/fes-common/sim/computer_memory_tb.cpp')]
            compiled = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, timeout=120)
            self.assertEqual(compiled.returncode, 0, compiled.stdout + compiled.stderr)
            result = subprocess.run([str(Path(directory) / 'Vcomputer_memory_top')],
                                    cwd=ROOT, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
