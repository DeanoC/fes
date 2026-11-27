import contextlib
import importlib.util
import io
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import call, patch


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('de25_serial_recovery', ROOT / 'scripts/de25_serial_recovery.py')
recovery = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(recovery)


class De25SerialRecoveryTests(unittest.TestCase):
    def _make_sysfs(self, sys_root, tty_name, serial, interface):
        bus = '3-11.1' if serial == 'TRWU8CBL' else '3-11.3'
        usb_device = (sys_root / 'devices/pci0000:80/0000:80:14.0/usb3' / bus)
        usb_device.mkdir(parents=True)
        for name, value in (
                ('serial', serial), ('idVendor', '09fb'), ('idProduct', '6026'),
                ('product', 'DE25-Nano')):
            (usb_device / name).write_text(value + '\n')
        interface_path = usb_device / f'{bus}:1.{int(interface)}'
        interface_path.mkdir()
        (interface_path / 'bInterfaceNumber').write_text(interface + '\n')
        tty_path = interface_path / tty_name
        tty_path.mkdir()
        class_device = sys_root / 'class/tty' / tty_name / 'device'
        class_device.parent.mkdir(parents=True)
        class_device.symlink_to(tty_path)

    def _run_probe(self, *, port=1, serial='TRWU8J8G', interface='02',
                   target_name=None, link_exists=True, opened_rdev=0):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            dev_root = root / 'dev'
            sys_root = root / 'sys'
            dev_root.mkdir()
            sys_root.mkdir()
            target_name = target_name or f'ttyUSB{port}'
            target = dev_root / target_name
            target.write_bytes(b'fixture device node')

            if target_name.startswith('ttyUSB'):
                self._make_sysfs(sys_root, target_name, serial, interface)
            by_id = dev_root / Path(recovery.J8G_DEVICE).relative_to('/dev')
            if link_exists:
                by_id.parent.mkdir(parents=True)
                by_id.symlink_to(Path('../..') / target_name)

            output = root / 'observation.json'
            stdout = io.StringIO()
            stderr = io.StringIO()
            with (patch.object(recovery, 'DEV_ROOT', dev_root),
                  patch.object(recovery, 'SYS_ROOT', sys_root),
                  patch.object(recovery.os, 'open', return_value=41) as mocked_open,
                  patch.object(recovery.os, 'fstat', return_value=SimpleNamespace(st_rdev=opened_rdev)),
                  patch.object(recovery.os, 'close') as mocked_close,
                  patch.object(recovery.termios, 'tcgetattr', return_value=[0] * 7),
                  patch.object(recovery.termios, 'tcsetattr'),
                  patch.object(recovery, 'capture', return_value=b'Linux version fixture')):
                with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                    status = recovery.main([
                        '--probe', '--serial', 'TRWU8J8G', '--output', str(output),
                        '--observe-seconds', '0.01',
                    ])
                observation = json.loads(output.read_text()) if output.exists() else None
                open_calls = mocked_open.call_args_list
                close_calls = mocked_close.call_args_list
            return status, observation, open_calls, close_calls, stderr.getvalue()

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

    @patch('fcntl.ioctl')
    @patch.object(recovery.os, 'write', return_value=1)
    @patch.object(recovery.termios, 'tcflush')
    @patch.object(recovery.time, 'sleep')
    def test_linux_break_ioctl_fallback_is_used_when_python_omits_constants(
            self, _sleep, _flush, _write, ioctl):
        self.assertFalse(hasattr(recovery.termios, 'TIOCSBRK'))
        self.assertFalse(hasattr(recovery.termios, 'TIOCCBRK'))
        recovery.sysrq_reboot(7)
        self.assertEqual(ioctl.call_args_list, [
            call(7, recovery.LINUX_TIOCSBRK),
            call(7, recovery.LINUX_TIOCCBRK),
        ])

    def test_j8g_interface_02_is_accepted_on_ttyusb1_and_ttyusb2(self):
        for port in (1, 2):
            with self.subTest(port=port):
                status, observation, open_calls, _close_calls, error = self._run_probe(port=port)
                self.assertEqual(status, 0, error)
                self.assertEqual(len(open_calls), 1)
                self.assertEqual(observation['resolved_device'], f'/dev/ttyUSB{port}')
                self.assertEqual(observation['usb_interface'], '02')
                self.assertEqual(observation['usb_serial'], 'TRWU8J8G')
                self.assertIn('/sys/devices/', observation['sysfs_device'])

    def test_cbl_board_is_refused_explicitly(self):
        status, observation, open_calls, _close_calls, error = self._run_probe(
            port=0, serial='TRWU8CBL')
        self.assertEqual(status, 2)
        self.assertIsNone(observation)
        self.assertEqual(open_calls, [])
        self.assertIn('TRWU8CBL', error)

    def test_j8g_interface_03_is_refused(self):
        status, observation, open_calls, _close_calls, error = self._run_probe(
            port=2, interface='03')
        self.assertEqual(status, 2)
        self.assertIsNone(observation)
        self.assertEqual(open_calls, [])
        self.assertIn('expected 02', error)

    def test_missing_by_id_link_is_refused(self):
        status, observation, open_calls, _close_calls, error = self._run_probe(link_exists=False)
        self.assertEqual(status, 2)
        self.assertIsNone(observation)
        self.assertEqual(open_calls, [])
        self.assertIn('link is missing', error)

    def test_non_ttyusb_resolution_is_refused(self):
        status, observation, open_calls, _close_calls, error = self._run_probe(target_name='ttyS0')
        self.assertEqual(status, 2)
        self.assertIsNone(observation)
        self.assertEqual(open_calls, [])
        self.assertIn('/dev/ttyUSB<number>', error)

    def test_opened_fd_device_number_must_match_verified_path(self):
        status, observation, open_calls, close_calls, error = self._run_probe(opened_rdev=1)
        self.assertEqual(status, 2)
        self.assertIsNone(observation)
        self.assertEqual(len(open_calls), 1)
        self.assertEqual(close_calls, [call(41)])
        self.assertIn('does not match verified device', error)

    def test_receipt_from_legacy_observation_is_refused(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            observed = root / 'console.json'
            receipt = root / 'recovery.json'
            observed.write_text(json.dumps({
                'serial': recovery.J8G_SERIAL, 'device': recovery.J8G_DEVICE,
                'resolved_device': '/dev/ttyUSB2', 'command': 'serial-sysrq-break-b',
                'input_sent': True, 'after': {'linux_boot_seen': True},
            }))
            with contextlib.redirect_stderr(io.StringIO()):
                status = recovery.main([
                    '--receipt-from', str(observed), '--serial', 'TRWU8J8G',
                    '--output', str(receipt), '--before-boot-id', 'before',
                    '--after-boot-id', 'after', '--target-id', 'j8g-target',
                ])
            self.assertEqual(status, 2)
            self.assertFalse(receipt.exists())

    def test_receipt_accepts_ttyusb1_with_verified_interface_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            observed = root / 'console.json'
            receipt = root / 'recovery.json'
            observed.write_text(json.dumps({
                'serial': recovery.J8G_SERIAL, 'device': recovery.J8G_DEVICE,
                'resolved_device': '/dev/ttyUSB1', 'usb_serial': 'TRWU8J8G',
                'usb_interface': '02', 'sysfs_device': '/sys/devices/fixture/ttyUSB1',
                'command': 'serial-sysrq-break-b', 'input_sent': True,
                'after': {'linux_boot_seen': True},
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

            with contextlib.redirect_stderr(io.StringIO()):
                status = recovery.main([
                    '--receipt-from', str(observed), '--serial', 'TRWU8J8G',
                    '--output', str(receipt), '--before-boot-id', 'same',
                    '--after-boot-id', 'same', '--target-id', 'j8g-target',
                ])
            self.assertEqual(status, 2)

    def test_receipt_rejects_mismatched_tty_or_usb_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            observed = root / 'console.json'
            receipt = root / 'recovery.json'
            valid = {
                'serial': recovery.J8G_SERIAL, 'device': recovery.J8G_DEVICE,
                'resolved_device': '/dev/ttyUSB1', 'usb_serial': 'TRWU8J8G',
                'usb_interface': '02', 'command': 'serial-sysrq-break-b',
                'input_sent': True, 'after': {'linux_boot_seen': True},
            }
            for field, bad_value in (
                    ('resolved_device', '/dev/ttyS0'), ('usb_serial', 'TRWU8CBL'),
                    ('usb_interface', '03')):
                with self.subTest(field=field):
                    observed.write_text(json.dumps({**valid, field: bad_value}))
                    with contextlib.redirect_stderr(io.StringIO()):
                        status = recovery.main([
                            '--receipt-from', str(observed), '--serial', 'TRWU8J8G',
                            '--output', str(receipt), '--before-boot-id', 'before',
                            '--after-boot-id', 'after', '--target-id', 'j8g-target',
                        ])
                    self.assertEqual(status, 2)


if __name__ == '__main__':
    unittest.main()
