"""Actual shared boot/board/master callbacks, including late observation faults."""
from pathlib import Path
import subprocess
import re
import sys
import tempfile
import unittest
import test_nano_usb_board as board
import usb_readiness


class NanoCoherentSnapshotTests(unittest.TestCase):
    def test_cold_callback_compiles_with_uboot_barrier_macro(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-cold-macro-') as tmp:
            header = Path(tmp) / 'barrier.h'
            header.write_text('#define barrier() __asm__ volatile ("" ::: "memory")\n')
            subprocess.run(['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                            '-include', str(header), '-I' + str(board.FW), '-c',
                            str(board.FW / 'nano_cold.c'), '-o', str(Path(tmp) / 'cold.o')],
                           check=True, timeout=30)

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
                'nano_usb_board.c', 'nano_mac.c', 'nano_usb_service.c', 'nano_usb_composition.c',
                'tests/nano_coherent_snapshot_test.c')]
            subprocess.run([*command, '-o', str(binary)], check=True, timeout=30)
            for fault in range(69):
                with self.subTest(fault=fault):
                    result = subprocess.run([str(binary), str(fault)],
                                            capture_output=True, text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn('PASS coherent snapshot', result.stdout)


    def test_strong_callback_cpu_guards_and_staged_publication(self):
        sys.path.insert(0, str(board.FW))
        try:
            from verify import function
        finally:
            sys.path.remove(str(board.FW))
        platform = (board.FW / 'nano_cold_platform.c').read_text()
        platform, declarations = re.subn(r'(?m)^static int cold_master_snapshot\([^;{]*\);\n', '', platform)
        self.assertEqual(declarations, 1)
        names = ('cold_master_cpu_scope', 'master_failure', 'cold_producer_sample', 'cold_master_snapshot')
        bodies = '#include \"nano_cpu_mode.h\"\n' + ''.join(function(platform, name) for name in names)
        for before, after in zip(names, ('observed_cpu_scope', 'observed_failure', 'observed_producer_sample', 'observed_master_snapshot')):
            bodies = bodies.replace(before, after)
        with tempfile.TemporaryDirectory(prefix='fes-nano-owner-cpu-') as tmp:
            project, _ = board.NanoUSBBoardTests().project(tmp)
            (Path(tmp) / 'usb_board_policy.h').write_text(
                usb_readiness.header(project, {'hps_io_hash': 'd' * 64}))
            (Path(tmp) / 'owner_callback.h').write_text(bodies)
            binary = Path(tmp) / 'owner'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                       '-fsanitize=undefined', '-fno-sanitize-recover=all',
                       '-I' + str(board.FW), '-I' + str(board.FW / 'tests'), '-I' + tmp]
            command += [str(board.FW / name) for name in (
                'nano_cold.c', 'nano_hps_io.c', 'nano_bridges.c', 'nano_boot.c',
                'nano_sd.c', 'nano_sd_platform.c', 'nano_usb_platform.c',
                'nano_usb_board.c', 'nano_mac.c', 'nano_usb_service.c', 'nano_usb_composition.c',
                'tests/nano_owner_snapshot_test.c')]
            subprocess.run([*command, '-o', str(binary)], check=True, timeout=30)
            for fault in range(21):
                with self.subTest(fault=fault):
                    result = subprocess.run([str(binary), str(fault)], capture_output=True,
                                            text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn('PASS strong coherent master', result.stdout)


    def test_actual_shared_dispatcher_observes_once_and_preserves_gates(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-owner-dispatch-') as tmp:
            binary = Path(tmp) / 'dispatch'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                       '-fsanitize=undefined', '-fno-sanitize-recover=all',
                       '-I' + str(board.FW), '-I' + str(board.FW / 'tests')]
            command += [str(board.FW / name) for name in (
                'nano_cold.c', 'nano_boot.c', 'nano_bridges.c', 'nano_bridge_sip.c',
                'nano_sd.c', 'nano_sd_platform.c', 'nano_usb_platform.c',
                'nano_mac.c', 'nano_usb_service.c', 'nano_usb_composition.c', 'nano_identity.c',
                'nano_configuration.c', 'tests/nano_owner_dispatch_test.c')]
            subprocess.run([*command, '-o', str(binary)], check=True, timeout=30)
            for mode in range(21):
                with self.subTest(mode=mode):
                    result = subprocess.run([str(binary), str(mode)], capture_output=True,
                                            text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn('PASS shared snapshot dispatcher', result.stdout)


if __name__ == '__main__':
    unittest.main()
