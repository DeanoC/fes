import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('de25_serial_recovery', ROOT / 'scripts/de25_serial_recovery.py')
recovery = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(recovery)


class De25SerialRecoveryTests(unittest.TestCase):
    def test_detects_linux_boot_marker(self):
        result = recovery.classify_console(b'Linux version 6.12.11-fes-de25-owner')
        self.assertEqual(result['state'], 'linux-boot')
        self.assertTrue(result['linux_boot_seen'])
        self.assertFalse(result['login_prompt_seen'])

    def test_detects_login_prompt_separately(self):
        result = recovery.classify_console(b'FES login:')
        self.assertEqual(result['state'], 'login-prompt')
        self.assertFalse(result['linux_boot_seen'])
        self.assertTrue(result['login_prompt_seen'])

    def test_empty_observation_is_quiet(self):
        self.assertEqual(recovery.classify_console(b'')['state'], 'quiet')

    def test_only_j8g_console_is_selected(self):
        self.assertIn('TRWU8J8G', recovery.J8G_DEVICE)
        self.assertNotIn('TRWU8CBL', recovery.J8G_DEVICE)
        self.assertEqual(recovery.J8G_SERIAL, 'TRWU8J8G')

    def test_receipt_requires_a_detected_boot_and_distinct_network_boot_ids(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            observed = root / 'console.json'
            receipt = root / 'recovery.json'
            observed.write_text(json.dumps({
                'serial': 'TRWU8J8G', 'device': recovery.J8G_DEVICE,
                'resolved_device': '/dev/ttyUSB2', 'command': 'serial-sysrq-break-b',
                'input_sent': True, 'after': {'linux_boot_seen': True},
            }))
            self.assertEqual(recovery.main([
                '--receipt-from', str(observed), '--serial', 'TRWU8J8G',
                '--output', str(receipt), '--before-boot-id', 'before',
                '--after-boot-id', 'after', '--target-id', 'j8g-target',
            ]), 0)
            value = json.loads(receipt.read_text())
            self.assertEqual(value['before_boot_id'], 'before')
            self.assertEqual(value['after_boot_id'], 'after')
            self.assertTrue(value['linux_boot_seen'])

            self.assertEqual(recovery.main([
                '--receipt-from', str(observed), '--serial', 'TRWU8J8G',
                '--output', str(receipt), '--before-boot-id', 'same',
                '--after-boot-id', 'same', '--target-id', 'j8g-target',
            ]), 2)


if __name__ == '__main__':
    unittest.main()
