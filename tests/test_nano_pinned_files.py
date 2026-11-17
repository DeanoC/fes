import hashlib
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'platform/embedded/nano'))
from pinned_files import PinnedFiles


class PinnedFilesTests(unittest.TestCase):
    def test_live_checks_do_not_hash_and_boundary_audits_do(self):
        with tempfile.TemporaryDirectory() as name:
            p=Path(name)/'tool';p.write_bytes(b'expected')
            pins=PinnedFiles({str(p):hashlib.sha256(p.read_bytes()).hexdigest()})
            self.addCleanup(pins.close)
            pins.audit()
            with patch('hashlib.file_digest',side_effect=AssertionError('per-command hash')):
                for _ in range(1000):self.assertTrue(pins.check())
            pins.audit();self.assertEqual((pins.audits,pins.checks),(2,1000))

    def test_file_or_symlink_replacement_and_edit_restore_retire(self):
        for fault in ('replace','edit-restore','symlink','parent'):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as name:
                root=Path(name);directory=root/'inputs';directory.mkdir()
                p=directory/'file';p.write_bytes(b'expected')
                pins=PinnedFiles({str(p):hashlib.sha256(p.read_bytes()).hexdigest()});pins.audit()
                self.addCleanup(pins.close)
                if fault=='replace':
                    other=directory/'other';other.write_bytes(b'expected');other.replace(p)
                elif fault=='edit-restore':p.write_bytes(b'changed!');p.write_bytes(b'expected')
                elif fault=='symlink':
                    other=directory/'other';other.write_bytes(b'expected');p.unlink();p.symlink_to(other)
                else:
                    directory.rename(root/'old');directory.mkdir();p.write_bytes(b'expected')
                with self.assertRaises(ValueError):pins.check()
                with self.assertRaises(ValueError):pins.audit()

    def test_wrong_digest_and_mutation_during_audit_retire(self):
        with tempfile.TemporaryDirectory() as name:
            p=Path(name)/'file';p.write_bytes(b'expected')
            pins=PinnedFiles({str(p):'0'*64})
            self.addCleanup(pins.close)
            with self.assertRaises(ValueError):pins.audit()
            with self.assertRaises(ValueError):pins.check()

    def test_edit_and_restore_during_hash_is_rejected(self):
        with tempfile.TemporaryDirectory() as name:
            p=Path(name)/'file';p.write_bytes(b'expected')
            pins=PinnedFiles({str(p):hashlib.sha256(p.read_bytes()).hexdigest()})
            self.addCleanup(pins.close)
            digest=hashlib.file_digest
            def changing(stream,algorithm):
                result=digest(stream,algorithm);p.write_bytes(b'expected');return result
            with patch('hashlib.file_digest',side_effect=changing),self.assertRaises(ValueError):pins.audit()
            self.assertTrue(pins.retired)
