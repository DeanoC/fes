"""Actual shared boot/board/master callbacks, including late observation faults."""
from pathlib import Path
import subprocess
import tempfile
import unittest
import test_nano_usb_board as board
import usb_readiness


class NanoCoherentSnapshotTests(unittest.TestCase):
    def test_live_snapshot_cost_and_late_faults(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-coherent-') as tmp:
            project, _ = board.NanoUSBBoardTests().project(tmp)
            (Path(tmp) / 'usb_board_policy.h').write_text(
                usb_readiness.header(project, {'hps_io_hash': 'd' * 64}))
            binary = Path(tmp) / 'snapshot'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                       '-fsanitize=undefined', '-fno-sanitize-recover=all',
                       '-I' + str(board.FW), '-I' + str(board.FW / 'tests'), '-I' + tmp]
            command += [str(board.FW / name) for name in (
                'nano_cold.c', 'nano_hps_io.c', 'nano_bridges.c', 'nano_boot.c',
                'nano_sd.c', 'nano_sd_platform.c', 'nano_usb_platform.c',
                'nano_usb_board.c', 'nano_usb_service.c', 'nano_usb_composition.c',
                'tests/nano_coherent_snapshot_test.c')]
            subprocess.run([*command, '-o', str(binary)], check=True, timeout=30)
            for fault in range(43):
                with self.subTest(fault=fault):
                    result = subprocess.run([str(binary), str(fault)],
                                            capture_output=True, text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn('PASS coherent snapshot', result.stdout)


if __name__ == '__main__':
    unittest.main()
