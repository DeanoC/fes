import importlib.util
import hashlib
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('de25_restore', ROOT / 'scripts/de25_restore.py')
restore = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(restore)


class De25RestoreTests(unittest.TestCase):
    def setUp(self):
        self.baseline = {
            'schema': 1, 'kit_id': 'de25-j8g', 'serial': 'TRWU8J8G',
            'target_id': 'target-id', 'image_sha256': 'image-id', 'kit_sw': 'bundle-id',
            'boot_id': 'boot-id', 'baseline_state_sha256': 'native-hash',
        }
        self.state = {
            'kit': 'de25-j8g', 'serial': 'TRWU8J8G', 'target_id': 'target-id', 'boot_id': 'boot-id',
            'agent_health': {'ready': True}, 'agent_status': {'state': 'idle'}, 'lease': {'state': 'free'},
            'host_health': {'target': {
                'artifacts': {'image_sha256': 'image-id'},
                'software': {'active_bundle': 'bundle-id'},
            }},
            'native_readonly': 'stable', 'native_sha256': 'native-hash',
        }

    def test_matching_baseline_and_idle_free_restore_pass(self):
        result = restore.verify(self.baseline, self.state, self.state.copy())
        self.assertTrue(result['passed'], result['checks'])

    def test_wrong_serial_image_bundle_or_busy_runtime_fails(self):
        changed = self.state.copy()
        changed['serial'] = 'TRWU8CBL'
        changed['agent_status'] = {'state': 'running'}
        changed['host_health'] = {'target': {
            'artifacts': {'image_sha256': 'wrong-image'},
            'software': {'active_bundle': 'wrong-bundle'},
        }}
        result = restore.verify(self.baseline, self.state, changed)
        self.assertFalse(result['passed'])
        self.assertFalse(result['checks']['after_serial'])
        self.assertFalse(result['checks']['after_image'])
        self.assertFalse(result['checks']['after_bundle'])
        self.assertFalse(result['checks']['after_runtime_idle'])

    def test_native_state_must_match_prestate_and_baseline(self):
        before = self.state.copy()
        before['native_sha256'] = 'unexpected'
        result = restore.verify(self.baseline, before, self.state)
        self.assertFalse(result['passed'])
        self.assertFalse(result['checks']['native_state_matches_baseline'])
        self.assertFalse(result['checks']['native_state_hash_unchanged'])

    def test_missing_baseline_identity_is_rejected(self):
        with self.assertRaises(ValueError):
            restore.verify({'schema': 1}, self.state, self.state)

    def test_recorded_serial_reboot_allows_only_new_boot_identity(self):
        before, after, receipt = self.reboot_states('boot-id')
        result = restore.verify(self.baseline, before, after, receipt)
        self.assertTrue(result['passed'], result['checks'])
        self.assertTrue(result['checks']['serial_recovery_receipt'])
        self.assertTrue(result['checks']['native_state_unchanged'])

    @staticmethod
    def set_native(state, text):
        state['native_readonly'] = text
        state['native_sha256'] = hashlib.sha256(text.encode()).hexdigest()

    def reboot_states(self, before_id):
        before = self.state.copy()
        before['boot_id'] = before_id
        self.set_native(before, f'boot_id={before_id}\n\nkernel=6.1\nroot=ro\n')
        after = self.state.copy()
        after['boot_id'] = 'new-boot-id'
        self.set_native(after, 'boot_id=new-boot-id\n\nkernel=6.1\nroot=ro\n')
        baseline_native = 'boot_id=boot-id\n\nkernel=6.1\nroot=ro\n'
        self.baseline['baseline_state_sha256'] = hashlib.sha256(baseline_native.encode()).hexdigest()
        receipt = {
            'schema': 1, 'serial': 'TRWU8J8G',
            'device': '/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8J8G-if02-port0',
            'command': 'serial-sysrq-break-b', 'linux_boot_seen': True,
            'before_boot_id': before_id, 'after_boot_id': 'new-boot-id',
            'target_id': 'target-id',
        }
        return before, after, receipt

    def test_serial_reboot_after_stale_baseline_boot_is_verified(self):
        before, after, receipt = self.reboot_states('already-rebooted')
        result = restore.verify(self.baseline, before, after, receipt)
        self.assertTrue(result['passed'], result['checks'])
        self.assertFalse(restore.verify(self.baseline, before, before)['passed'])

    def test_recovery_does_not_hide_existing_or_new_native_drift(self):
        before, after, receipt = self.reboot_states('already-rebooted')
        self.set_native(before, before['native_readonly'].replace('root=ro', 'root=rw'))
        self.set_native(after, after['native_readonly'].replace('root=ro', 'root=rw'))
        result = restore.verify(self.baseline, before, after, receipt)
        self.assertFalse(result['passed'])
        self.assertFalse(result['checks']['native_state_matches_baseline'])
        self.assertTrue(result['checks']['native_state_unchanged'])
        before, after, receipt = self.reboot_states('already-rebooted')
        self.set_native(after, after['native_readonly'].replace('root=ro', 'root=rw'))
        self.assertFalse(restore.verify(self.baseline, before, after, receipt)['passed'])

    def test_recovery_receipt_must_bind_before_after_and_board(self):
        before, after, receipt = self.reboot_states('already-rebooted')
        for field, wrong in (
            ('schema', 2), ('serial', 'other'), ('device', '/dev/ttyUSB0'),
            ('command', 'other'), ('linux_boot_seen', False),
            ('before_boot_id', 'boot-id'), ('after_boot_id', 'wrong'),
            ('target_id', 'other'),
        ):
            with self.subTest(field=field):
                invalid = {**receipt, field: wrong}
                result = restore.verify(self.baseline, before, after, invalid)
                self.assertFalse(result['passed'])
                self.assertFalse(result['checks']['serial_recovery_receipt'])
        self.assertFalse(restore.verify(self.baseline, before, after, {})['passed'])

    def test_recovery_requires_one_matching_boot_field_and_correct_snapshot_hash(self):
        for bad in (
            'root=ro\n',
            'boot_id=wrong\nroot=ro\n',
            'boot_id=already-rebooted\nboot_id=already-rebooted\nroot=ro\n',
            'boot_id=already-rebooted extra\nroot=ro\n',
        ):
            with self.subTest(native=bad):
                before, after, receipt = self.reboot_states('already-rebooted')
                self.set_native(before, bad)
                self.assertFalse(restore.verify(self.baseline, before, after, receipt)['passed'])
        before, after, receipt = self.reboot_states('already-rebooted')
        before['native_sha256'] = 'forged'
        self.assertFalse(restore.verify(self.baseline, before, after, receipt)['passed'])
        before, after, receipt = self.reboot_states('already-rebooted')
        after['native_sha256'] = 'forged'
        self.assertFalse(restore.verify(self.baseline, before, after, receipt)['passed'])

    def test_serial_reboot_without_boot_observation_fails(self):
        receipt = {
            'schema': 1, 'serial': 'TRWU8J8G',
            'device': '/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8J8G-if02-port0',
            'command': 'serial-sysrq-break-b', 'linux_boot_seen': False,
            'before_boot_id': 'boot-id', 'after_boot_id': 'new-boot-id',
            'target_id': 'target-id',
        }
        after = self.state.copy()
        after['boot_id'] = 'new-boot-id'
        result = restore.verify(self.baseline, self.state, after, receipt)
        self.assertFalse(result['passed'])
        self.assertFalse(result['checks']['serial_recovery_receipt'])


if __name__ == '__main__':
    unittest.main()
