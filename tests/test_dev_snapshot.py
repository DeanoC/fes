"""Real Git coverage for diagnostic snapshots and release exclusion."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import dev_snapshot
import inputs


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()


class DevelopmentSnapshotTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        git(self.root, 'init', '-q')
        git(self.root, 'config', 'user.name', 'Fixture')
        git(self.root, 'config', 'user.email', 'fixture@example.invalid')
        git(self.root, 'remote', 'add', 'origin', 'https://example.invalid/fes.git')
        (self.root / '.gitignore').write_text('/out/\n')
        for name in inputs.COMPONENTS:
            module = self.root / 'sources' / name
            module.mkdir(parents=True)
            (module / 'README').write_text(name)
        git(self.root, 'add', '.')
        git(self.root, 'commit', '-qm', 'modules')
        self.base = git(self.root, 'rev-parse', 'HEAD')

    def test_capture_final_bytes_without_changing_source_index_refs_or_files(self):
        module = self.root / 'sources/FogCast'
        (module / 'README').write_text('staged')
        (module / 'staged').write_text('staged-only addition')
        git(self.root, 'add', '.')
        (module / 'README').write_text('unstaged after staging')
        (module / 'new').write_bytes(b'new\x00bytes')
        (module / 'run').write_text('#!/bin/sh\nexit 0\n')
        (module / 'run').chmod(0o755)
        (self.root / 'sources/misteross/README').unlink()
        index = (self.root / '.git/index').read_bytes()
        refs = git(self.root, 'show-ref')
        status = git(self.root, 'status', '--porcelain=v1', '--untracked-files=all')
        # The destination does not inherit the source's local user identity.
        with patch.dict(os.environ, {'GIT_CONFIG_GLOBAL': '/dev/null', 'GIT_CONFIG_NOSYSTEM': '1'}):
            result = dev_snapshot.create(self.root)
        destination = Path(result['directory'])
        self.assertEqual((destination / 'sources/FogCast/README').read_text(), 'unstaged after staging')
        self.assertEqual((destination / 'sources/FogCast/staged').read_text(), 'staged-only addition')
        self.assertEqual((destination / 'sources/FogCast/new').read_bytes(), b'new\x00bytes')
        self.assertTrue((destination / 'sources/FogCast/run').stat().st_mode & 0o111)
        self.assertFalse((destination / 'sources/misteross/README').exists())
        self.assertEqual(git(destination, 'status', '--porcelain'), '')
        self.assertEqual(git(destination, 'rev-parse', 'HEAD^'), self.base)
        self.assertEqual(git(destination, 'remote', 'get-url', 'origin'), 'https://example.invalid/fes.git')
        self.assertEqual((self.root / '.git/index').read_bytes(), index)
        self.assertEqual(git(self.root, 'show-ref'), refs)
        self.assertEqual(git(self.root, 'status', '--porcelain=v1', '--untracked-files=all'), status)
        self.assertEqual((module / 'README').read_text(), 'unstaged after staging')
        self.assertEqual(inputs.development_snapshot(destination)['captured_tree'], result['captured_tree'])

    def test_diagnostics_allowed_release_rejected_even_if_working_marker_deleted(self):
        result = dev_snapshot.create(self.root)
        destination = Path(result['directory'])
        self.assertEqual(inputs.validate(destination, allow_development=True),
                         dict.fromkeys(inputs.COMPONENTS, result['revision']))
        (destination / dev_snapshot.MARKER).unlink()
        with self.assertRaisesRegex(ValueError, 'development snapshot'):
            inputs.validate(destination)
        with self.assertRaisesRegex(ValueError, 'another snapshot'):
            dev_snapshot.create(destination)

    def test_profile_cannot_select_diagnostic_commit_for_release(self):
        result = dev_snapshot.create(self.root)
        git(self.root, 'fetch', '-q', result['directory'], result['revision'])
        profile = {'sources': {'FogCast': result['revision']}}
        with self.assertRaisesRegex(ValueError, 'development snapshot'):
            inputs.validate(self.root, profile)
        self.assertEqual(inputs.validate(self.root, profile, allow_development=True)['FogCast'], result['revision'])

    def test_fingerprints_classify_profile_selected_snapshot_in_ordinary_root(self):
        import build
        result = dev_snapshot.create(self.root)
        git(self.root, 'fetch', '-q', result['directory'], result['revision'])
        revisions = dict.fromkeys(inputs.COMPONENTS, self.base)
        revisions['FogCast'] = result['revision']
        with patch.object(build, 'ROOT', self.root), \
                patch.object(build, 'recipe_fingerprint', return_value={}), \
                patch.object(build, 'image_recipe_files', return_value=[]):
            for fingerprint in (build.host_fingerprint, build.build_fingerprint):
                _, info = fingerprint(revisions, {}, 'go fixture')
                classification = info['development_snapshot']
                self.assertEqual(classification['classification'], 'development-only')
                self.assertIsNone(classification['root'])
                self.assertEqual(classification['selected_sources']['FogCast']['revision'], result['revision'])
                self.assertEqual(classification['selected_sources']['FogCast']['snapshot']['base_revision'], self.base)
                _, ordinary = fingerprint(dict.fromkeys(inputs.COMPONENTS, self.base), {}, 'go fixture')
                self.assertNotIn('development_snapshot', ordinary)

    def test_malformed_committed_marker_fails_closed(self):
        marker = self.root / dev_snapshot.MARKER
        marker.parent.mkdir()
        marker.write_text(json.dumps({'format': True, 'classification': 'development-only'}))
        git(self.root, 'add', '.')
        git(self.root, 'commit', '-qm', 'invalid classification')
        with self.assertRaisesRegex(ValueError, 'invalid development snapshot'):
            inputs.validate(self.root, allow_development=True)

    def test_inherited_alternate_index_is_not_modified(self):
        alternate = self.root / 'out' / 'caller-index'
        alternate.parent.mkdir()
        alternate.write_bytes((self.root / '.git/index').read_bytes())
        before = alternate.read_bytes()
        with patch.dict(os.environ, {'GIT_INDEX_FILE': str(alternate)}):
            result = dev_snapshot.create(self.root)
        self.assertEqual(alternate.read_bytes(), before)
        self.assertEqual(git(Path(result['directory']), 'status', '--porcelain'), '')

    def test_config_symlink_cannot_write_marker_outside_snapshot(self):
        external = self.root / 'out' / 'external-config'
        external.mkdir(parents=True)
        (self.root / 'config').symlink_to(external, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'config directory'):
            dev_snapshot.create(self.root)
        self.assertFalse((external / 'development-snapshot.json').exists())

    def test_unignored_output_is_rejected(self):
        (self.root / '.gitignore').write_text('')
        with self.assertRaises(subprocess.CalledProcessError):
            dev_snapshot.create(self.root)
        self.assertFalse((self.root / 'out').exists())

    def test_nested_repository_is_not_silently_captured_as_gitlink(self):
        nested = self.root / 'nested'
        nested.mkdir()
        git(nested, 'init', '-q')
        git(nested, 'config', 'user.name', 'Fixture')
        git(nested, 'config', 'user.email', 'fixture@example.invalid')
        git(nested, 'commit', '--allow-empty', '-qm', 'nested')
        with self.assertRaisesRegex(ValueError, 'embedded repository'):
            dev_snapshot.create(self.root)
        self.assertEqual(git(self.root, 'rev-parse', 'HEAD'), self.base)


if __name__ == '__main__':
    unittest.main()
