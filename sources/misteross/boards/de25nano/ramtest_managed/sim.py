#!/usr/bin/env python3
"""Exercise the managed SDRAM control, status raster and real three-clock shell."""
from pathlib import Path
import importlib.util
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
LOCAL = HERE.parent / 'ramtest/managed'
ROOT = HERE.parents[2]


def load():
    spec = importlib.util.spec_from_file_location('ramtest_board_spec', HERE / 'build.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def run():
    parent = ROOT / 'build/sim/de25-ramtest-managed'
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix='run-', dir=parent))
    board = load()
    files, _, _ = board.board.component_files(board, board.board.capture(board))
    staged = output / 'component'
    for name, data in files.items():
        path = staged / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    waivers = ('UNUSEDSIGNAL', 'UNUSEDPARAM', 'UNOPTFLAT', 'CASEINCOMPLETE', 'CASEOVERLAP', 'WIDTHTRUNC',
               'WIDTHEXPAND', 'SYNCASYNCNET', 'PINCONNECTEMPTY', 'DECLFILENAME', 'IMPLICITSTATIC',
               'VARHIDDEN', 'CASEX', 'BLKSEQ', 'MULTITOP')
    # The scan and mailbox intentionally retain power-up state across reset to
    # fail closed on a lost transport epoch. Newer Verilator calls that
    # PROCASSINIT; only pass its targeted waiver when the installed tool knows it.
    procassinit = subprocess.run(['verilator', '-Wno-PROCASSINIT', '--version'],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
    base = ['verilator', '--cc', '--exe', '--build', '-j', '2', '-Wall',
            *['-Wno-' + warning for warning in waivers],
            *(['-Wno-PROCASSINIT'] if procassinit else []),
            '-I' + str(staged), '-I' + str(staged / 'generated')]
    cases = (
        ('scan', 'fes_de25_managed_ram_scan', ['-GTEST_BYTES=32', '-GMAX_BURST=1', '-GTIMEOUT=16', '-GLATENCY_SAMPLES=1'],
         ['fes_de25_managed_ram_scan.v', 'fes_de25_ram_scan.v'], 'scan_control_tb.cpp'),
        ('high-address', 'fes_de25_ram_scan', ['-GDATA_W=16', '-GTIMEOUT=64', '-GLATENCY_SAMPLES=16',
         '-GCHECK_ADDRESS_HIGH=1', '-CFLAGS', '-O2 -DDATA_W=16 -DCHECK_ADDR_HIGH=1'],
         ['fes_de25_ram_scan.v'], '../scan_tb.cpp'),
        ('hold', 'fes_de25_ramtest_hold_guard', [], ['fes_de25_ramtest_hold_guard.v'], 'hold_guard_tb.cpp'),
        ('hold-scan', 'fes_de25_ramtest_hold_scan_harness', [],
         ['fes_de25_ramtest_hold_guard.v', 'fes_de25_managed_ram_scan.v', 'fes_de25_ram_scan.v',
          'fes_computer_mailbox.v'], 'hold_scan_tb.cpp'),
        ('video', 'fes_de25_ramtest_video', [], ['fes_de25_ramtest_video.v', 'fes_video_720p.v', 'ram_font.v'],
         'status_video_tb.cpp'),
        ('standard', 'fes_de25_ramtest_standard', ['--public-flat-rw', '-GSDRAM_POWERUP_CYCLES=16', '-GSDRAM_TEST_BYTES=4096'],
         sorted(p.name for p in staged.glob('*.v')), 'standard_tb.cpp'),
    )
    for name, top, flags, inputs, bench in cases:
        mdir = output / name
        print('DE25 managed RAM test simulation:', name, flush=True)
        subprocess.run(base + ['--top-module', top, '--Mdir', str(mdir), *flags,
                              *([str(LOCAL / 'hold_scan_harness.v')] if name == 'hold-scan' else []),
                              *[str(staged / source) for source in inputs], str(LOCAL / bench)], check=True)
        subprocess.run([str(mdir / ('V' + top))], check=True)
    print('Simulation outputs:', output, flush=True)


if __name__ == '__main__':
    run()
