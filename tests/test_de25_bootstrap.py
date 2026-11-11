"""Bootstrap rejection boundaries; no kit or programmer access."""
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import de25_bootstrap as boot


class BootstrapTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / 'source'
        self.source.mkdir()
        self.vendor = self.root / 'vendor'
        self.vendor.mkdir()
        self.output = self.root / 'output'
        self.receipt = {'format': 1, 'hps_initialization': 'HPS FIRST',
                        'production_admission': False, 'fes_revision': 'a' * 40,
                        'computer_build_id': 'b' * 32, 'artifacts': {}}
        for name in boot.ARTIFACTS:
            (self.source / name).write_bytes(name.encode())
            self.receipt['artifacts'][name] = boot.identity(self.source / name)
        self.write_receipt()
        for name in boot.VENDOR:
            (self.vendor / name).write_bytes(name.encode())
        self.lock = {name: boot.identity(self.vendor / name) for name in boot.VENDOR}
        self.addCleanup(patch.stopall)
        patch.object(boot, 'vendor_lock', return_value=self.lock).start()

    def write_receipt(self):
        (self.source / 'linux-programming.json').write_text(json.dumps(self.receipt))

    def converter(self, argv, stdout, **kwargs):
        self.assertEqual(argv[0], str(self.root / 'quartus_pfg'))
        if argv[1] == '-c':
            stdout.write(b'Conversion successful\n')
            dest = Path(argv[3]).parent
            (dest / 'recovery.hps.rbf').write_bytes(b'HPS only')
            (dest / 'recovery.core.rbf').write_bytes((self.source / 'linux-boot.core.rbf').read_bytes())
        else:
            text = ('JIC Header\nFlash device: MT25QU128\n'
                    'HPS/FPGA configuration order: HPS_FIRST\n'
                    'HPS IO hash: ABC\nHPS Section\nHPIO Section\n')
            stdout.write(text.encode() + b'Temperature threshold: 120\xb0C\n')

    def prepare(self):
        return boot.prepare(self.source, self.vendor, self.output, self.root / 'quartus_pfg')

    def test_modified_input_refused_before_conversion(self):
        (self.source / 'linux-complete.sof').write_bytes(b'different')
        with patch.object(boot.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'producer artifact differs'):
                self.prepare()
            run.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_fpga_first_and_wrong_rollback_refused(self):
        with patch.object(boot.subprocess, 'run') as run:
            self.receipt['hps_initialization'] = 'AFTER INIT_DONE'
            self.write_receipt()
            with self.assertRaisesRegex(ValueError, 'HPS-first'):
                self.prepare()
            self.receipt['hps_initialization'] = 'HPS FIRST'
            self.write_receipt()
            (self.vendor / 'golden_top_hps.jic').write_bytes(b'another vendor build')
            with self.assertRaisesRegex(ValueError, 'locked vendor rollback differs'):
                self.prepare()
            run.assert_not_called()

    def test_closed_package_detects_tampering_and_symlinks(self):
        with patch.object(boot.subprocess, 'run', side_effect=self.converter):
            record = self.prepare()
        self.assertEqual(boot.verify(self.output), record)
        recovery = self.output / 'recovery.hps.rbf'
        recovery.unlink()
        recovery.symlink_to(self.source / 'linux-complete.sof')
        with self.assertRaisesRegex(ValueError, 'regular file'):
            boot.verify(self.output)
        recovery.unlink()
        recovery.write_bytes(b'HPS only')
        (self.output / 'untracked.jic').write_bytes(b'wrong candidate')
        with self.assertRaisesRegex(ValueError, 'file set differs'):
            boot.verify(self.output)

    def test_mismatched_split_never_published(self):
        def bad_split(argv, **kwargs):
            self.converter(argv, **kwargs)
            if argv[1] == '-c':
                (Path(argv[3]).parent / 'recovery.core.rbf').write_bytes(b'wrong core')
        with patch.object(boot.subprocess, 'run', side_effect=bad_split):
            with self.assertRaisesRegex(ValueError, 'changed selected core'):
                self.prepare()
        self.assertFalse(self.output.exists())
        self.assertEqual(sorted(p.name for p in self.root.iterdir()), ['source', 'vendor'])

    def test_core_in_bootstrap_or_flash_mismatch_refused(self):
        for name in ('candidate-info.log', 'rollback-info.log', 'recovery-info.log'):
            (self.root / name).write_text('JIC Header\nFlash device: MT25QU128\n'
                'HPS/FPGA configuration order: HPS_FIRST\nHPS IO hash: ABC\nHPS Section\nHPIO Section\n')
        (self.root / 'candidate-info.log').write_text((self.root / 'candidate-info.log').read_text() + 'CORE Section\n')
        with self.assertRaisesRegex(ValueError, 'must not include FPGA core'):
            boot.check_info(self.root)
        (self.root / 'candidate-info.log').write_text('JIC Header\nFlash device: QSPI512\n')
        with self.assertRaisesRegex(ValueError, 'MT25QU128'):
            boot.check_info(self.root)


if __name__ == '__main__':
    unittest.main()
