import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
NANO = ROOT / 'platform/embedded/nano'
FW = ROOT / 'platform/embedded/firmware'
sys.path.insert(0, str(NANO))
import usb_readiness


class NanoUSBBoardTests(unittest.TestCase):
    def project(self, temp):
        project = Path(temp) / 'project'
        handoff = project / 'board/hps/project/output_files/hps_bootloader_handoff.bin'
        handoff.parent.mkdir(parents=True)
        data = (ROOT / 'tests/fixtures/nano-usb-board/hps_bootloader_handoff.bin').read_bytes()
        handoff.write_bytes(data)
        receipt = project / 'board/hps/build.json'
        receipt.write_text(json.dumps({'generated_handoff_and_sdc_sha256': {
            'output_files/hps_bootloader_handoff.bin': hashlib.sha256(data).hexdigest()}}))
        return project, handoff

    def test_actual_readiness_and_two_phase_usb_engine(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-usb-board-') as temp:
            project, _ = self.project(temp)
            policy = usb_readiness.derive(project, {'hps_io_hash': 'd' * 64})
            self.assertEqual(len(policy['hps_io']['registers']), 164)
            (Path(temp) / 'usb_board_policy.h').write_text(
                usb_readiness.header(project, {'hps_io_hash': 'd' * 64}))
            binary = Path(temp) / 'usb-board'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                       '-fsanitize=undefined', '-fno-sanitize-recover=all',
                       '-I' + str(FW), '-I' + temp]
            command += [str(FW / n) for n in (
                'nano_cold.c', 'nano_hps_io.c', 'nano_usb_platform.c', 'nano_usb_board.c',
                'tests/nano_usb_board_test.c')]
            subprocess.run([*command, '-o', str(binary)], check=True, timeout=30)
            result = subprocess.run([str(binary)], capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('PASS actual USB board clocks/pins', result.stdout)

    def test_policy_requires_actual_compiled_handoff(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-usb-policy-') as temp:
            project, handoff = self.project(temp)
            context = {'hps_io_hash': 'd' * 64}
            original = usb_readiness.header(project, context)
            original_data = handoff.read_bytes()
            data = bytearray(original_data)
            data[40] ^= 1
            handoff.write_bytes(data)
            with self.assertRaises(ValueError):
                usb_readiness.header(project, context)
            self.assertIn('FES_NANO_USB_BOARD_PINS', original)
            handoff.write_bytes(original_data)
            with self.assertRaises(ValueError):
                usb_readiness.derive(project, {'hps_io_hash': '0' * 64})


if __name__ == '__main__':
    unittest.main()
