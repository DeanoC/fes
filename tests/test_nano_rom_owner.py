"""Real ROM ownership predicate joined to the real shared boot/master/USB code."""
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest
import test_nano_usb_board as board
import usb_readiness


class NanoROMOwnerTests(unittest.TestCase):
    def test_trusted_shared_binding_and_every_fresh_ownership_bracket(self):
        sys.path.insert(0, str(board.FW))
        try:
            from verify import function
        finally:
            sys.path.remove(str(board.FW))
        platform = (board.FW / 'nano_cold_platform.c').read_text()
        platform, count = re.subn(r'(?m)^static int cold_master_snapshot\([^;{]*\);\n', '', platform)
        self.assertEqual(count, 1)
        names = ('cold_master_cpu_scope', 'master_failure', 'cold_producer_sample', 'cold_master_snapshot')
        bodies = ''.join(function(platform, name) for name in names)
        for before, after in zip(names, ('observed_cpu_scope', 'observed_failure', 'observed_producer_sample', 'observed_master_snapshot')):
            bodies = bodies.replace(before, after)
        rom = (board.FW / 'nano_rom_platform.inc').read_text()
        predicate = ''.join(function(rom, name) for name in ('computer_rom_scope_failure', 'computer_rom_owned'))
        with tempfile.TemporaryDirectory(prefix='fes-nano-rom-owner-') as tmp:
            project, _ = board.NanoUSBBoardTests().project(tmp)
            (Path(tmp) / 'usb_board_policy.h').write_text(usb_readiness.header(project, {'hps_io_hash': 'd' * 64}))
            (Path(tmp) / 'owner_callback.h').write_text(bodies)
            (Path(tmp) / 'rom_owner_callback.h').write_text(predicate)
            binary = Path(tmp) / 'rom-owner'
            argv = ['cc', '-std=c11', '-Os', '-Wall', '-Wextra', '-Werror', '-fsanitize=undefined',
                    '-fno-sanitize-recover=all', '-I' + str(board.FW), '-I' + str(board.FW / 'tests'), '-I' + tmp]
            argv += [str(board.FW / name) for name in (
                'nano_cold.c', 'nano_hps_io.c', 'nano_bridges.c', 'nano_boot.c', 'nano_sd.c', 'nano_sd_platform.c',
                'nano_usb_platform.c', 'nano_usb_board.c', 'nano_usb_service.c', 'nano_usb_composition.c',
                'tests/nano_rom_owner_test.c')]
            subprocess.run([*argv, '-o', str(binary)], check=True, timeout=30)
            for fault in range(47):
                with self.subTest(fault=fault):
                    result = subprocess.run([str(binary), str(fault)], capture_output=True, text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn('PASS ROM shared ownership', result.stdout)


if __name__ == '__main__':
    unittest.main()
