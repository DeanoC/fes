import gzip
import hashlib
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
EVIDENCE = ROOT / 'docs/validation/2026-10-30-de25-physical-memory'
BOOT_UUID = '971c5c52-685c-4c62-97e8-e908937258ac'
KERNEL = '6.12.11-gd7d192a9ddd9'
ROOT_DEVICE = '/dev/mmcblk0p2 ext3'


class De25PhysicalMemoryEvidenceTests(unittest.TestCase):
    def test_boot_and_ssh_claims_match_their_retained_logs(self):
        boot_log = gzip.decompress(
            (EVIDENCE / 'temporary-sof-boot.log.gz').read_bytes()
        ).decode(errors='replace')
        ssh_log = gzip.decompress(
            (EVIDENCE / 'temporary-sof-linux.log.gz').read_bytes()
        ).decode(errors='replace')

        self.assertIn('Ubuntu 22.04.3', boot_log)
        self.assertIn(KERNEL, boot_log)
        self.assertIn('root=/dev/mmcblk0p2', boot_log)
        self.assertIn('EXT4-fs (mmcblk0p2): mounting ext3 file system', boot_log)
        self.assertIn('Mounted root (ext3 filesystem) on device 179:2', boot_log)
        self.assertNotIn(BOOT_UUID, boot_log)
        self.assertNotIn('Ubuntu 22.04.3', ssh_log)
        self.assertIn(BOOT_UUID, ssh_log)
        self.assertIn(KERNEL, ssh_log)
        self.assertIn(ROOT_DEVICE, ssh_log)

    def test_full_scan_script_covers_both_full_local_bank_capacities(self):
        script = (EVIDENCE / 'full-ram.tcl').read_text()
        matches = re.findall(r'foreach\s+\{bank length\}\s+\{([^}]+)\}', script)
        self.assertEqual(len(matches), 1)
        fields = matches[0].split()
        self.assertEqual(list(zip(fields[::2], fields[1::2])), [
            ('fpga', '1073741824'),
            ('sdram', '134217728'),
        ])

    def test_evidence_manifest_tracks_the_updated_record(self):
        manifest = json.loads((EVIDENCE / 'manifest.json').read_text())
        digest = hashlib.sha256((EVIDENCE / 'README.md').read_bytes()).hexdigest()
        self.assertEqual(manifest['file_sha256']['README.md'], digest)

    def test_boot_uuid_is_attributed_to_ssh(self):
        hardware = json.loads((EVIDENCE / 'hardware.json').read_text())
        self.assertEqual(hardware['boot']['temporary_sof_boot_id'], BOOT_UUID)
        self.assertEqual(hardware['boot']['temporary_sof_boot_id_source'],
                         'temporary-sof-linux.log.gz (pinned-key SSH)')
        normalized = re.sub(r'\s+', ' ', (EVIDENCE / 'README.md').read_text())
        self.assertIn('The UART transcript records Ubuntu 22.04.3', normalized)
        self.assertIn('The pinned-key SSH transcript records that boot UUID', normalized)

    def test_reboot_claim_is_limited_to_the_retained_uart_bytes(self):
        uart = gzip.decompress(
            (EVIDENCE / 'restore-reboot-uart.log.gz').read_bytes()
        ).decode(errors='replace')
        self.assertIn('reboot: Restarting system', uart)
        restore = json.loads((EVIDENCE / 'restore.json').read_text())
        self.assertIn('no subsequent boot', restore['software_reboot'])
        self.assertNotIn('60-second', restore['software_reboot'])
        normalized = re.sub(r'\s+', ' ', (EVIDENCE / 'README.md').read_text())
        self.assertIn('do not retain the Linux commands that initiated shutdown', normalized)
        self.assertIn('document the capture duration, or include a subsequent boot', normalized)
        self.assertNotIn('operator procedure', normalized)
        self.assertNotIn('Linux `sync` and authenticated `sudo /sbin/reboot`', normalized)

    def test_failed_device_info_log_does_not_name_a_device_index(self):
        failed = gzip.decompress(
            (EVIDENCE / 'device-info-command-failed.log.gz').read_bytes()
        ).decode(errors='replace')
        self.assertIn('Error (213019)', failed)
        self.assertIn('Error (213002)', failed)
        self.assertNotRegex(failed, r'device index [0-9]+|@[0-9]+|--device\s*=?\s*[0-9]+')
        normalized = re.sub(r'\s+', ' ', (EVIDENCE / 'README.md').read_text())
        self.assertIn('does not establish which index was queried', normalized)
        self.assertIn('[config-status-device1.log.gz](config-status-device1.log.gz)', normalized)
        self.assertNotIn('device-info-command-index2', normalized)


if __name__ == '__main__':
    unittest.main()
