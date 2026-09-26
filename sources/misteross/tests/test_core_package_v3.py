"""Cross-consumer format-3 fixtures and restricted archive/member admission."""
import json
from pathlib import Path
import tempfile
import unittest

from scripts.core_package import PackageError, read_package
from scripts.export_core_package import _archive_bytes

FIXTURES = Path(__file__).resolve().parent / 'fixtures' / 'core-bundle-v3'
V4_FIXTURES = FIXTURES.parent / 'core-bundle-v4'


class CorePackageV3Tests(unittest.TestCase):
    def test_format_four_two_sources_and_exact_members(self):
        cases = json.loads((V4_FIXTURES / 'cases.json').read_text())
        for case in cases:
            with self.subTest(case=case['name']), tempfile.TemporaryDirectory() as tmp:
                manifest, payload, mapping = ((V4_FIXTURES / case[key]).read_bytes()
                                              for key in ('manifest', 'payload', 'rom_map'))
                directory = Path(tmp)
                for name, data in [('manifest.toml', manifest), ('core.rbf', payload),
                                   ('rom-map.json', mapping)]:
                    (directory / name).write_bytes(data)
                if 'archive_members' in case:
                    (directory / 'extra.bin').write_bytes(b'extra')
                if case['valid']:
                    got = read_package(directory)
                    self.assertEqual(got.package_id, case['package_id'])
                    self.assertEqual([r['role'] for r in got.fields['roms']],
                                     ['firmware', 'cartridge'])
                else:
                    with self.assertRaises(PackageError):
                        read_package(directory)

    def test_shared_cases_as_directories_and_archives(self):
        for case in json.loads((FIXTURES / 'cases.json').read_text()):
            with self.subTest(case=case['name']), tempfile.TemporaryDirectory() as tmp:
                path = Path(tmp)
                manifest, payload, mapping = ((FIXTURES / case[key]).read_bytes() for key in ('manifest','payload','rom_map'))
                directory = path / 'package'
                directory.mkdir()
                for name, data in [('manifest.toml',manifest),('core.rbf',payload),('rom-map.json',mapping)]:
                    (directory / name).write_bytes(data)
                archive = path / 'package.fcore'
                archive.write_bytes(_archive_bytes(manifest,payload,mapping))
                for target in (directory, archive):
                    if case['valid']:
                        actual = read_package(target)
                        self.assertEqual(case['package_id'], actual.package_id)
                        self.assertEqual(mapping, actual.rom_map_bytes)
                    else:
                        with self.assertRaises(PackageError):
                            read_package(target)

    def test_missing_extra_and_linked_map_members(self):
        case = json.loads((FIXTURES / 'cases.json').read_text())[0]
        manifest,payload,mapping = ((FIXTURES / case[key]).read_bytes() for key in ('manifest','payload','rom_map'))
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            archive=root/'package.fcore'
            archive.write_bytes(_archive_bytes(manifest,payload))
            with self.assertRaises(PackageError):
                read_package(archive)
            archive.write_bytes(_archive_bytes(manifest,payload,mapping)+b'\0'*512)
            with self.assertRaises(PackageError):
                read_package(archive)
            directory=root/'package';directory.mkdir()
            (directory/'manifest.toml').write_bytes(manifest)
            (directory/'core.rbf').write_bytes(payload)
            outside=root/'map.json';outside.write_bytes(mapping)
            (directory/'rom-map.json').symlink_to(outside)
            with self.assertRaises(PackageError):
                read_package(directory)
