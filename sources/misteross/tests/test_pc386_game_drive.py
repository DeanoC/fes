# SPDX-License-Identifier: MIT
"""pc386 game drives (scripts/build_pc386_game_drive.py): the manifest, a
drive built from a stand-in zip and folder, and how runners resolve a drive.
No network access and no user-supplied programs: the zip and folder are made
here, and the FreeDOS system disk is replaced by an empty FAT16 volume."""
import hashlib
import io
import json
import os
import shutil
import subprocess
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest import mock

from scripts import build_pc386_freedos_image as freedos
from scripts import build_pc386_game_drive as drives
from scripts import sim_fes_pc386 as sim

MTOOLS = shutil.which('mformat') and shutil.which('mcopy') and shutil.which('mdir')
ENV = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', TZ='UTC')


class ManifestTest(unittest.TestCase):
    def test_every_title_is_complete_and_fits_its_drive(self):
        data = drives.manifest()
        ids = []
        for name, drive in data['drives'].items():
            self.assertLessEqual(drive['cylinders'] * freedos.HEADS * freedos.SPT * 512, 128 * 1024 * 1024, name)
            for title in drive['titles']:
                ids.append(title['id'])
                self.assertRegex(title['sha256'], r'^[0-9a-f]{64}$', title['id'])
                self.assertTrue(title['launch'].endswith('|'), title['id'])
                for copy in title['copy']:
                    for part in copy['to'].split('\\'):
                        self.assertEqual(drives.dos_name(part), part, title['id'])
                for seconds, keys in title.get('keys', []):
                    self.assertGreaterEqual(seconds, 0)
                    self.assertLess(seconds, title.get('limit', 20.0), title['id'])
        self.assertEqual(len(ids), len(set(ids)))
        self.assertGreaterEqual(len(ids), 30)

    def test_buckets_cover_the_census(self):
        data = drives.manifest()
        used = {title['bucket'] for drive in data['drives'].values() for title in drive['titles']}
        self.assertEqual(used, set(data['buckets']))

    def test_the_acceptance_titles_require_opl2(self):
        data = drives.manifest()
        opl2 = {title['id'] for drive in data['drives'].values() for title in drive['titles'] if title.get('opl2')}
        self.assertEqual(opl2, {'wolf3d', 'ultima6'})

    def test_a_title_on_two_drives_is_refused(self):
        data = drives.manifest()
        first = next(iter(data['drives'].values()))['titles'][0]
        text = drives.MANIFEST.read_text() + (
            f'\n[drives.extra]\ncylinders = 65\n[[drives.extra.titles]]\nid = "{first["id"]}"\ntitle = "x"\n'
            f'bucket = "real"\npath = "x.zip"\nsha256 = "{"0" * 64}"\nlaunch = "x|"\n'
            'copy = [{ from = "x", to = "X" }]\n')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'drives.toml'
            path.write_text(text)
            with self.assertRaisesRegex(drives.DriveError, 'is on drives'):
                drives.manifest(path)


class NameTest(unittest.TestCase):
    def test_dos_names(self):
        self.assertEqual(drives.dos_name('wolf3d.exe'), 'WOLF3D.EXE')
        self.assertEqual(drives.dos_name('SB16'), 'SB16')
        self.assertIsNone(drives.dos_name('Commander Keen.txt'))
        self.assertIsNone(drives.dos_name('manual.html'))
        self.assertIsNone(drives.dos_name('toolongname.exe'))


@unittest.skipUnless(MTOOLS, 'mtools is not installed')
class BuildTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        root = Path(self.directory.name)
        self.store, self.sources = root / 'store', root / 'sources'
        (self.sources / 'tree/MARKS/TOOL').mkdir(parents=True)
        (self.sources / 'tree/MARKS/TOOL/TOOL.EXE').write_bytes(b'MZtool')
        zip_path = self.sources / 'zips/Game (1993).zip'
        zip_path.parent.mkdir()
        with zipfile.ZipFile(zip_path, 'w') as archive:
            for name, data in (('Game/GAME.EXE', b'MZgame'), ('Game/game.cfg', b'adlib'),
                               ('Game/SB16/GAME.CFG', b'sb 220 7 1'), ('Game/MT32/GAME.CFG', b'mt32'),
                               ('Game/Read me first.txt', b'eXoDOS note')):
                info = zipfile.ZipInfo(name, (1993, 5, 1, 12, 0, 0))
                archive.writestr(info, data)
        self.data = {
            'sources': {'exodos': {'kind': 'zip', 'env': 'TEST_ZIPS', 'default': str(zip_path.parent)},
                        'pack': {'kind': 'tree', 'env': 'TEST_TREE', 'default': str(self.sources / 'tree')}},
            'buckets': {'real': '', 'program': ''},
            'drives': {'test': {'cylinders': 20, 'titles': [
                {'id': 'game', 'title': 'Game (1993)', 'bucket': 'real', 'path': 'Game (1993).zip',
                 'sha256': drives.sha256(zip_path), 'launch': 'cd \\game|game|', 'keys': [[2.0, '|']],
                 'copy': [{'from': 'Game', 'to': 'GAME', 'skip': ['SB16', 'MT32']},
                          {'from': 'Game/SB16', 'to': 'GAME'}]},
                {'id': 'tool', 'title': 'Tool', 'bucket': 'program', 'source': 'pack', 'path': 'MARKS/TOOL',
                 'sha256': drives.tree_sha256(self.sources / 'tree/MARKS/TOOL'), 'launch': 'tool|',
                 'copy': [{'from': '', 'to': 'MARKS\\TOOL'}]},
            ]}},
        }

    def tearDown(self):
        self.directory.cleanup()

    def fake_system_disk(self, profile, cylinders, work, download=False, jobs=4):
        """An empty FAT16 volume of the drive's geometry in place of FreeDOS."""
        shutil.rmtree(work, ignore_errors=True)
        work.mkdir(parents=True)
        disk = work / 'disk.img'
        total = cylinders * freedos.HEADS * freedos.SPT
        disk.write_bytes(bytes(total * 512))
        subprocess.run(['mformat', '-i', f'{disk}@@{freedos.PARTITION_START * 512}', '-h', str(freedos.HEADS),
                        '-s', str(freedos.SPT), '-H', str(freedos.PARTITION_START),
                        '-T', str(total - freedos.PARTITION_START), '-N', '13860026', '-v', 'FREEDOS', '::'],
                       check=True, capture_output=True, env=dict(ENV, SOURCE_DATE_EPOCH=str(freedos.FILE_TIME)))
        return disk, {'kernel': {'commit': 'test'}}

    def build(self):
        with mock.patch.object(drives, 'STORE', self.store), \
                mock.patch.object(drives.freedos, 'system_disk', self.fake_system_disk), \
                mock.patch.object(drives.freedos, 'inputs_key', lambda profile: 'inputs'), \
                mock.patch('sys.stdout', new_callable=io.StringIO):
            return drives.build('test', self.data)

    def listing(self, image, path):
        return subprocess.run(['mdir', '-i', f'{image}@@{freedos.PARTITION_START * 512}', path],
                              check=True, capture_output=True, text=True, env=ENV).stdout

    def test_a_drive_is_named_by_its_bytes_and_rebuilds_the_same(self):
        image = self.build()
        digest = hashlib.sha256(image.read_bytes()).hexdigest()
        self.assertEqual(image.name, f'{digest}.img')
        self.assertEqual(self.build(), image)
        self.assertEqual((self.store / 'by-name/test.img').resolve(), image)
        self.assertEqual(self.store.stat().st_mode & 0o777, 0o700)
        record = json.loads(image.with_suffix('.json').read_text())
        self.assertEqual(record['sha256'], digest)
        self.assertEqual(record['geometry'], {'cylinders': 20, 'heads': 16, 'sectors_per_track': 63})
        self.assertEqual([title['id'] for title in record['titles']], ['game', 'tool'])
        self.assertEqual(record['titles'][0]['files'], 2)  # GAME.EXE, and GAME.CFG twice
        self.assertEqual(record['titles'][0]['skipped_long_names'], 1)
        self.assertNotIn('copy', record['titles'][0])

    def test_a_later_copy_replaces_files_and_skipped_folders_stay_out(self):
        image = self.build()
        game = self.listing(image, '::GAME')
        self.assertIn('GAME     CFG        10', game)
        self.assertNotIn('MT32', game)
        self.assertNotIn('SB16', game)
        self.assertIn('TOOL     EXE', self.listing(image, '::MARKS/TOOL'))

    def test_a_changed_source_is_refused(self):
        self.data['drives']['test']['titles'][0]['sha256'] = '0' * 64
        with self.assertRaisesRegex(drives.DriveError, 'the manifest pins'):
            self.build()

    def test_runners_resolve_a_drive_by_path_hash_prefix_and_name(self):
        image = self.build()
        digest = image.stem
        for reference in (str(image), digest, digest[:12], 'test'):
            with self.subTest(reference=reference):
                found, record = drives.resolve(reference, self.store)
                self.assertEqual(found, image)
                self.assertEqual(record['drive'], 'test')
        self.assertEqual(drives.geometry(record), '20/16/63')
        self.assertEqual(drives.title_of(record, 'tool')['launch'], 'tool|')
        with self.assertRaises(drives.DriveError):
            drives.title_of(record, 'missing')
        with self.assertRaises(drives.DriveError):
            drives.resolve('nothing', self.store)

    def test_a_drive_whose_bytes_changed_is_refused(self):
        image = self.build()
        with open(image, 'r+b') as stream:
            stream.seek(1 << 20)
            stream.write(b'changed')
        with self.assertRaisesRegex(drives.DriveError, 'its record says'):
            drives.resolve(str(image), self.store)

    def test_the_game_suite_finds_the_drive_that_holds_a_title(self):
        image = self.build()
        args = mock.Mock(drive=None, game='tool')
        with mock.patch.object(drives, 'STORE', self.store):
            found, record, title = sim.game_drive(args)
        self.assertEqual(found, image)
        self.assertEqual(title['id'], 'tool')
        args.game = 'missing'
        with mock.patch.object(drives, 'STORE', self.store), self.assertRaises(sim.SimError):
            sim.game_drive(args)


class SystemDiskTest(unittest.TestCase):
    def test_a_drive_larger_than_the_ata_window_is_refused(self):
        self.assertEqual(freedos.MAX_CYLINDERS, 260)
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(freedos.ImageError):
                freedos.system_disk('games', 261, Path(directory) / 'work')


if __name__ == '__main__':
    unittest.main()
