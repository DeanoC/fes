import gzip
import hashlib
import json
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts.check_doc_links import broken_links


class CheckDocLinksTests(unittest.TestCase):
    def repo(self, files):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        for name, text in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(text)
        subprocess.run(['git', 'init', '-q', str(root)], check=True)
        subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
        return root

    def test_reports_missing_relative_targets_with_line(self):
        root = self.repo({'docs/a.md': 'intro\n[gone](missing.md)\n[ok](b.md#part)\n',
                          'docs/b.md': '# Part\n'})
        self.assertEqual(broken_links(root), ['docs/a.md:2: missing.md'])

    def test_ignores_urls_anchors_and_fenced_code(self):
        root = self.repo({'a.md': '[web](https://example.com/x.md) [here](#top)\n'
                                  '```\n[sample](not-there.md)\n```\n[root](/a.md)\n'})
        self.assertEqual(broken_links(root), [])

    def test_destinations_may_contain_balanced_parentheses(self):
        root = self.repo({'a.md': '[v2](manual(v2).md) [gone](old(v1).md)\n',
                          'manual(v2).md': 'ok\n'})
        self.assertEqual(broken_links(root), ['a.md:1: old(v1).md'])

    def test_indented_and_nested_fences_hide_examples(self):
        root = self.repo({'a.md': '- item\n\n  ```md\n  [example](missing.md)\n  ```\n'
                                  '````md\n```\n[inner](missing.md)\n```\n````\n'
                                  '[after](gone.md)\n'})
        self.assertEqual(broken_links(root), ['a.md:11: gone.md'])

    def test_reference_definitions_are_checked(self):
        root = self.repo({'a.md': '[guide][setup] [ok][b]\n\n[setup]: missing.md\n'
                                  '[b]: b.md\n[web]: https://example.com\n',
                          'b.md': 'ok\n'})
        self.assertEqual(broken_links(root), ['a.md:3: missing.md'])

    def test_untracked_markdown_is_not_checked(self):
        root = self.repo({'a.md': 'ok\n'})
        (root / 'scratch.md').write_text('[gone](missing.md)\n')
        self.assertEqual(broken_links(root), [])


class De25PhysicalMemoryRecordTests(unittest.TestCase):
    root = Path(__file__).resolve().parents[1]
    record = root / 'docs/validation/2026-10-30-de25-physical-memory'

    def test_sdram_chip_select_claim_matches_scan_extent_and_controller(self):
        scans = json.loads((self.record / 'scans.json').read_text())['results']
        sdram = {row['test']: row for row in scans if row['bank'] == 'sdram'}
        boundary = 1 << 26
        self.assertEqual(sdram['smoke']['length_bytes'], 4096)
        self.assertLess(sdram['smoke']['length_bytes'], boundary)
        self.assertEqual(sdram['full']['length_bytes'], 128 * 1024 * 1024)
        self.assertGreater(sdram['full']['length_bytes'], boundary)

        smoke_script = (self.record / 'smoke-ram.tcl').read_text()
        self.assertIn('de25ramtest::run $service 6ee668470911276a9e553d0025c7ef0b $bank 60000 10 4096', smoke_script)
        jtag_script = (self.root / 'sources/misteross/boards/de25nano/ramtest/jtag.tcl').read_text()
        self.assertRegex(jtag_script, r'BASE_OFFSET 0 BASE_HIGH_OFFSET 0')
        controller = (self.root / 'sources/misteross/boards/de25nano/ramtest/sdram.v').read_text()
        self.assertIn('word_addr<=write&&write_left!=0?write_next:address[26:1]', controller)
        self.assertRegex(controller, r'word_addr\[25\].*2\'b01:2\'b10')

        report = ' '.join((self.record / 'README.md').read_text().split())
        ramtest_guide = ' '.join((self.root / 'sources/misteross/boards/de25nano/ramtest/README.md').read_text().split())
        self.assertIn('4,096-byte SDRAM smoke starts at address 0 and stays below byte-address bit 26', report)
        self.assertIn('Only the full 128-MiB SDRAM scan crosses byte-address bit 26', ramtest_guide)
        self.assertNotIn('The SDRAM range includes both physical chip selects', report)

    def test_factory_clean_halt_claim_matches_retained_boot_log(self):
        with gzip.open(self.record / 'temporary-sof-boot.log.gz', 'rt') as stream:
            boot_log = stream.read()
        report = ' '.join((self.record / 'README.md').read_text().split())
        self.assertIn('corrupted or uncleanly shut down', boot_log)
        self.assertIn('does not establish that factory Linux completed a clean halt', report)
        self.assertNotRegex(report, r'Factory Linux is synced and gracefully halted')

    def test_device_mask_claim_matches_retained_device_names(self):
        hardware = json.loads((self.record / 'hardware.json').read_text())
        board = hardware['board']
        with gzip.open(self.record / 'factory-linked-services.log.gz', 'rt') as stream:
            linked_services = stream.read()
        with gzip.open(self.record / 'ramtest-program-with-spl.log.gz', 'rt') as stream:
            programmer = stream.read()

        self.assertEqual(board['system_console_device_path_pattern'], 'A5E(A013BB23B|B013BB23BCS)')
        self.assertNotIn('R1', board['system_console_device_path_pattern'])
        self.assertIn(f'/devices/{board["system_console_device_path_pattern"]}', linked_services)
        self.assertIn(f'for device {board["programmer_device_name"]}@1', programmer)
        self.assertEqual(board['silicon_suffix'], 'unconfirmed; retained device path has no R1 arm')

        report = ' '.join((self.record / 'README.md').read_text().split())
        self.assertIn('with no R1 arm', report)
        self.assertNotIn('matches this part to both the CS and R1 masks', report)

    def test_factory_restore_boot_identity_is_bound_to_ssh_not_uart(self):
        restore = json.loads((self.record / 'restore.json').read_text())
        with gzip.open(self.record / 'restore-reboot-uart.log.gz', 'rt') as stream:
            uart = stream.read()
        with gzip.open(self.record / 'restored-factory-linux.log.gz', 'rt') as stream:
            ssh = stream.read()

        self.assertEqual(restore['uart_factory_boot'], 'NOT_RETAINED')
        self.assertEqual(restore['pinned_ssh_factory_boot'], 'PASS')
        self.assertIn('reboot: Restarting system', uart)
        self.assertNotIn(restore['factory_boot_id'], uart)
        for value in (restore['factory_boot_id'], restore['factory_kernel'], restore['root']):
            self.assertIn(value, ssh)

        report = ' '.join((self.record / 'README.md').read_text().split())
        self.assertIn('no subsequent factory boot transcript', report)
        self.assertIn('The pinned-key SSH transcript records factory boot UUID', report)
        self.assertNotIn('UART/pinned SSH checks', report)

    def test_programmed_sof_identity_uses_logged_quartus_checksum(self):
        hardware = json.loads((self.record / 'hardware.json').read_text())
        programmed = hardware['programmed_sof']
        self.assertEqual(programmed['programmer_log'], 'ramtest-program-with-spl.log.gz')
        self.assertEqual(programmed['quartus_checksum'], '0x067AE616')
        self.assertNotIn('sha256', programmed)
        self.assertNotIn('bytes', programmed)

        with gzip.open(self.record / programmed['programmer_log'], 'rt') as stream:
            programmer = stream.read()
        self.assertRegex(programmer, r'Using programming file .* with checksum 0x067AE616 for device A5EB013BB23BCS@1')
        report = ' '.join((self.record / 'README.md').read_text().split())
        self.assertIn('No SHA-256 or byte count for the generated SOF was retained', report)
        self.assertNotIn('2267ecc2e9e8dd08cd24e8d413ee7e704cc022863d5b761b6d6e6aa33a6bb64c', report)

    def test_board_identity_and_release_claims_are_limited_to_retained_evidence(self):
        hardware = json.loads((self.record / 'hardware.json').read_text())
        board = hardware['board']
        self.assertEqual(board['quartus_cable'], 'DE25-Nano [3-11.3-iface0]')
        self.assertNotIn('jtag_idcode', board)
        self.assertNotIn('usb_serial', board)
        self.assertNotIn('uid', board)

        report = ' '.join((self.record / 'README.md').read_text().split())
        self.assertIn('do not bind the session to a USB serial or board', report)
        self.assertNotIn('TRWU8J8G', report)
        self.assertNotIn('79B9468337C3A50B', report)
        self.assertNotIn('TRWU8CBL', report)
        self.assertNotIn('Both serial-specific status files record release', report)

        guide = (self.root / 'docs/de25-linux.md').read_text()
        self.assertNotIn('identified board', guide)
        self.assertIn('do not bind this session to a USB serial or board UID', ' '.join(guide.split()))
        self.assertNotIn('4362c0dd', (self.record / 'README.md').read_text())

        checks_note = ' '.join((self.record / 'README.md').read_text().split())
        self.assertIn('does not attest checks on this PR head', checks_note)

        with gzip.open(self.record / 'device-info-command-index2.log.gz', 'rt') as stream:
            failed_attempt = stream.read()
        self.assertIn('Error (213019)', failed_attempt)
        self.assertIn('Error (213002)', failed_attempt)
        self.assertNotIn('Info: Command:', failed_attempt)
        self.assertIn('device index 2 information attempt failed', report)
        restore = json.loads((self.record / 'restore.json').read_text())
        self.assertIn('claim_released_utc', restore)
        self.assertIn('one claim-release timestamp', report)

    def test_collector_cli_replays_retained_logs_to_exact_scans_json(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            logs = Path(temp_dir) / 'logs'
            logs.mkdir()
            output = Path(temp_dir) / 'scans.json'
            for name in ('smoke-ram.log', 'full-ram.log'):
                with gzip.open(self.record / f'{name}.gz', 'rb') as source:
                    (logs / name).write_bytes(source.read())

            subprocess.run([
                sys.executable,
                str(self.record / 'collect.py'),
                '--logs', str(logs),
                '--output', str(output),
            ], check=True, capture_output=True, text=True)
            self.assertEqual(output.read_bytes(), (self.record / 'scans.json').read_bytes())

    def test_manifest_hashes_match_the_retained_evidence(self):
        manifest = json.loads((self.record / 'manifest.json').read_text())
        for name, expected in manifest['file_sha256'].items():
            with self.subTest(name=name):
                actual = hashlib.sha256((self.record / name).read_bytes()).hexdigest()
                self.assertEqual(actual, expected)


if __name__ == '__main__':
    unittest.main()
