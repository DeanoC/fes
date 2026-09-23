"""Format-3 members survive parent caching, publication, and rollback."""
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import artifact_cache
import build
import bundle
from tests.test_core_build import make_package_generation, package_file_snapshot


def add_map(package, data=b'{"format":1}\n'):
    path = package['directory'] / 'rom-map.json'
    path.write_bytes(data)
    package['inputs']['rom_map_sha256'] = hashlib.sha256(data).hexdigest()


class RomPackagePipelineTest(unittest.TestCase):
    def test_publish_verify_and_rollback_preserve_sealed_map(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = root / 'output'
            output.mkdir()
            packages, selections = make_package_generation(root, 'old', ['a' * 64, 'b' * 64])
            add_map(packages[1])
            names = build.publish_package_outputs(packages, selections, output)
            map_name = 'core-packages/' + 'b' * 64 + '/rom-map.json'
            self.assertIn(map_name, names)
            self.assertIn(map_name, build.package_output_names(packages[1]))
            self.assertEqual((output / map_name).stat().st_mode & 0o777, 0o444)
            self.assertEqual(build.verify_package_outputs(output, packages), names)
            original = package_file_snapshot(output)
            newer, newer_selections = make_package_generation(root, 'new', ['c' * 64, 'd' * 64])
            add_map(newer[1], b'new map')
            verify = build.verify_package_outputs
            def fail_after_publish(where, selected):
                if Path(where) == output and selected == newer:
                    raise ValueError('injected post-publication failure')
                return verify(where, selected)
            with patch.object(build, 'verify_package_outputs', side_effect=fail_after_publish):
                with self.assertRaisesRegex(ValueError, 'injected'):
                    build.publish_package_outputs(newer, newer_selections, output)
            self.assertEqual(package_file_snapshot(output), original)
            build.verify_package_outputs(output, packages)
            (output / map_name).chmod(0o644)
            (output / map_name).write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'changed'):
                build.verify_package_outputs(output, packages)

    def test_missing_extra_and_linked_map_fail_closed(self):
        for damage in ('missing', 'extra', 'symlink'):
            with self.subTest(damage=damage), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                output = root / 'output'
                output.mkdir()
                packages, selections = make_package_generation(root, 'old', ['a' * 64, 'b' * 64])
                add_map(packages[1])
                source = packages[1]['directory']
                if damage == 'missing':
                    (source / 'rom-map.json').unlink()
                elif damage == 'extra':
                    (source / 'unexpected').write_bytes(b'extra')
                else:
                    data = (source / 'rom-map.json').read_bytes()
                    (source / 'rom-map.json').unlink()
                    (root / 'external-map').write_bytes(data)
                    (source / 'rom-map.json').symlink_to(root / 'external-map')
                with self.assertRaisesRegex(ValueError, 'differs'):
                    build.publish_package_outputs(packages, selections, output)

    def test_matching_payloads_with_different_maps_are_ambiguous(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            candidates = [(root, root / 'record', {'core_rbf_sha256': 'a' * 64, 'rom_map_sha256': char * 64})
                          for char in ('b', 'c')]
            with patch.object(bundle, 'canonical_package_record', return_value=b'{}'), patch.object(bundle, '_matching_package_candidates', return_value=candidates):
                with self.assertRaisesRegex(ValueError, 'multiple package IDs'):
                    bundle.resolve_core_package(root, 'd' * 40, root / 'selection.toml')

    def test_candidate_inspection_requires_a_sealed_map(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            package = root / ('a' * 64)
            package.mkdir()
            for name in ('manifest.toml', 'core.rbf', 'rom-map.json'):
                (package / name).write_bytes(b'member')
                (package / name).chmod(0o444)
            record = root / 'record.json'
            record.write_bytes(b'{}')
            record.chmod(0o444)
            package.chmod(0o555)
            (package / 'rom-map.json').chmod(0o644)
            with patch.object(bundle.subprocess, 'run') as invoke:
                with self.assertRaisesRegex(ValueError, 'not sealed'):
                    bundle._inspect_package_candidate(root, package, record)
                invoke.assert_not_called()


    def test_cache_and_resolution_preserve_map_digest(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / 'source'
            source.mkdir()
            package = root / ('a' * 64)
            package.mkdir()
            for name, data in [('manifest.toml', b'format = 3\n'), ('core.rbf', b'payload'), ('rom-map.json', b'ROM map')]:
                (package / name).write_bytes(data)
            record = {'format': 2, 'repository': 'https://example.invalid/fes',
                      'revision': '1' * 40, 'source_path': 'sources/misteross',
                      'source_inputs': {'rtl/top.v': 'b' * 64}}
            encoded = json.dumps(record).encode()
            record_path = root / 'record.json'
            record_path.write_bytes(encoded)
            cached = artifact_cache.publish(root / 'cache', record_path, package)
            self.assertEqual((cached / 'rom-map.json').read_bytes(), b'ROM map')
            self.assertEqual(artifact_cache.publish(root / 'cache', record_path, package), cached)
            inspected = {'package_id': package.name, 'manifest': {
                'format': 3, 'core': {'id': 'fes.zx81'},
                'payload': {'sha256': bundle.digest(package / 'core.rbf')},
                'build': {'revision': record['revision']}}}
            for name, field in [('manifest.toml', 'manifest_sha256'), ('core.rbf', 'core_rbf_sha256'), ('rom-map.json', 'rom_map_sha256')]:
                inspected[field] = bundle.digest(package / name)
            with patch.object(bundle, 'ARTIFACT_CACHE_ROOT', root / 'cache'), patch.object(bundle, 'canonical_package_record', return_value=encoded), patch.object(bundle, '_inspect_package_candidate', return_value=inspected):
                result = bundle.resolve_core_package(source, '2' * 40, root / 'selection.toml', recipe=bundle.recipe_for('fes.zx81'))
            self.assertEqual(result['inputs']['rom_map_sha256'], inspected['rom_map_sha256'])
            self.assertNotIn('rom_map_sha256', result['inputs']['source_selection'])
            self.assertEqual(result['inputs']['selection']['format'], 2)
            (package / 'rom-map.json').write_bytes(b'changed')
            with self.assertRaisesRegex(ValueError, 'differs'):
                artifact_cache.publish(root / 'cache', record_path, package)


if __name__ == '__main__':
    unittest.main()
