"""pc386 discovery admits only complete atomically published evidence entries."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from scripts import bundle
from scripts.recipes import recipe_for


class Pc386DiscoveryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.store = self.root / 'build/packages'
        self.store.mkdir(parents=True)
        self.recipe = recipe_for('fes.pc386')
        self.record_id = 'a' * 64
        self.package_id = 'b' * 64
        self.record = json.dumps({'identities': {'record': self.record_id}}).encode()

    def match(self, *, cache=None):
        return bundle._matching_package_candidates(self.root, self.record, self.recipe, store_override=cache)

    def test_core_owned_loader_refuses_symlinked_producer_before_import(self):
        target = self.root / 'foreign.py'
        target.write_text('raise AssertionError("linked producer was executed")')
        tools = self.root / 'cores/fes-pc386/tools'
        tools.mkdir(parents=True)
        (tools / 'build_pc386_product.py').symlink_to(target)
        with mock.patch.object(bundle, 'authenticate_misteross_origin'):
            with self.assertRaises(ValueError):
                bundle.canonical_package_record(self.root, recipe=self.recipe, selection_only=True)

    def test_core_owned_sibling_symlinks_never_execute_on_either_loader(self):
        source = Path(__file__).resolve().parents[1] / 'sources/misteross/cores/fes-pc386/tools'
        tools = self.root / 'cores/fes-pc386/tools'
        tools.mkdir(parents=True)
        (tools / 'build_pc386_product.py').write_bytes((source / 'build_pc386_product.py').read_bytes())
        for name in ('build_pc386_with_lockstep.py', 'pc386_run_lockstep.py', 'pc386_execution.py', 'pc386_owned_job.py', 'pc386_seal_evidence.py'):
            (tools / name).write_text('# fixture helper\n')
        marker = self.root / 'executed'
        sentinel = self.root / 'outside.py'
        sentinel.write_text(f"from pathlib import Path; Path({str(marker)!r}).write_text('bad')\n")
        helper = tools / 'pc386_execution.py'
        helper.unlink()
        helper.symlink_to(sentinel)
        with mock.patch.object(bundle, 'authenticate_misteross_origin'):
            with self.assertRaises(ValueError):
                bundle.canonical_package_record(self.root, recipe=self.recipe, selection_only=True)
        direct = __import__('subprocess').run([__import__('sys').executable, str(tools / 'build_pc386_product.py'), '--help'],
                                              cwd=self.root, capture_output=True, text=True)
        self.assertNotEqual(direct.returncode, 0)
        self.assertFalse(marker.exists())
        helper.unlink()
        helper.write_text('# fixture helper\n')
        wrapper = tools / 'build_pc386_with_lockstep.py'
        wrapper.unlink()
        wrapper.symlink_to(sentinel)
        direct = __import__('subprocess').run([__import__('sys').executable, str(tools / 'build_pc386_product.py'),
                                                '--with-lockstep'], cwd=self.root, capture_output=True, text=True)
        self.assertNotEqual(direct.returncode, 0)
        self.assertFalse(marker.exists())
        wrapper.unlink()
        wrapper.write_text('# fixture helper\n')
        reader = tools / 'pc386_seal_evidence.py'
        reader.unlink()
        reader.symlink_to(sentinel)
        ev = self.root / 'ev'
        ev.mkdir()
        ev.chmod(0o555)
        with self.assertRaises(ValueError):
            bundle._inspect_pc386_evidence(self.root, self.root / 'pkg', self.root / 'record', ev)
        self.assertFalse(marker.exists())

    def test_authenticated_canonical_subprocess_is_bounded_and_preserves_output(self):
        import os
        import sys
        source = Path(__file__).resolve().parents[1] / 'sources/misteross'
        command = [sys.executable, '-c', 'print("fixture-record")']
        self.assertEqual(bundle._bounded_pc386_canonical(command, source, os.environ.copy(), timeout=5),
                         b'fixture-record\n')
        with self.assertRaises(TimeoutError):
            bundle._bounded_pc386_canonical([sys.executable, '-c', 'import time; time.sleep(60)'],
                                             source, os.environ.copy(), timeout=.2)

    def test_legacy_partial_export_and_uncommitted_staging_are_ignored(self):
        # The generic exporter publishes record and archive before its package.
        (self.store / (self.package_id + '.' + self.record_id + '.build-inputs.json')).write_bytes(self.record)
        (self.store / (self.package_id + '.fcore')).write_bytes(b'partial')
        staging = self.store / '.pc386-entries/.publish-incomplete'
        staging.mkdir(parents=True)
        (staging / 'build-inputs.json').write_bytes(self.record)
        with mock.patch.object(bundle, '_inspect_package_candidate') as inspect:
            self.assertEqual(self.match(), [])
            inspect.assert_not_called()

    def test_complete_local_entry_is_verified_before_selection(self):
        entry = self.store / '.pc386-entries' / self.record_id
        package = entry / self.package_id
        package.mkdir(parents=True)
        (entry / 'build-inputs.json').write_bytes(self.record)
        (entry / 'evidence').mkdir()
        with mock.patch.object(bundle, '_inspect_package_candidate', return_value={'package_id': self.package_id}), \
                mock.patch.object(bundle, '_inspect_pc386_evidence', return_value='c' * 64) as validate:
            result = self.match()
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0][2]['pc386_evidence']['receipt_sha256'], 'c' * 64)
        validate.assert_called_once_with(self.root, package, entry / 'build-inputs.json', entry / 'evidence')

    def test_incomplete_shared_cache_is_not_a_cache_hit(self):
        cache = self.root / 'shared'
        entry = cache / self.record_id
        package = entry / self.package_id
        package.mkdir(parents=True)
        (entry / 'build-inputs.json').write_bytes(self.record)
        with mock.patch.object(bundle, '_inspect_package_candidate', return_value={'package_id': self.package_id}):
            with self.assertRaises((ValueError, OSError)):
                self.match(cache=cache)

    def test_existing_bad_local_evidence_fails_closed(self):
        entry = self.store / '.pc386-entries' / self.record_id
        (entry / self.package_id).mkdir(parents=True)
        (entry / 'build-inputs.json').write_bytes(self.record)
        (entry / 'evidence').symlink_to(self.root, target_is_directory=True)
        with mock.patch.object(bundle, '_inspect_package_candidate', return_value={'package_id': self.package_id}):
            with self.assertRaises(ValueError):
                self.match()
