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
            if drive.get('kind') == 'image':
                self.assertRegex(drive['sha256'], r'^[0-9a-f]{64}$', name)
            for title in drive['titles']:
                ids.append(title['id'])
                self.assertTrue(title['launch'].endswith('|'), title['id'])
                if drive.get('kind') == 'image':
                    self.assertNotIn('copy', title, title['id'])
                    continue
                self.assertRegex(title['sha256'], r'^[0-9a-f]{64}$', title['id'])
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


    def test_the_windows_drive_is_an_image_drive(self):
        drive = drives.manifest()['drives']['wfw-msdos']
        self.assertEqual(drive['kind'], 'image')
        self.assertEqual([title['id'] for title in drive['titles']], [sim.WFW_TITLE])
        self.assertEqual(sim.WFW_DRIVE, 'wfw-msdos')

    def test_an_image_drive_needs_its_hash(self):
        text = drives.MANIFEST.read_text().replace(
            'sha256 = "a2afd3c7689526eba919cfbfe77810f4751525dea1827d0c196dd81e0c8d0c6c"', 'sha256 = "x"')
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'drives.toml'
            path.write_text(text)
            with self.assertRaisesRegex(drives.DriveError, 'pins its image'):
                drives.manifest(path)


class ImageDriveTest(unittest.TestCase):
    """A stand-in image drive of one cylinder: registered, refused, skipped."""

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        root = Path(self.directory.name)
        self.store = root / 'store'
        self.image = root / 'supplied.img'
        self.image.write_bytes(bytes(range(256)) * (freedos.HEADS * freedos.SPT * 2))
        self.data = {'buckets': {'program': ''}, 'sources': {}, 'drives': {'win': {
            'kind': 'image', 'cylinders': 1, 'sha256': drives.sha256(self.image),
            'titles': [{'id': 'win', 'title': 'Windows', 'bucket': 'program', 'launch': 'win|', 'limit': 12.0}]}}}

    def tearDown(self):
        self.directory.cleanup()

    def register(self, source=None):
        with mock.patch.object(drives, 'STORE', self.store):
            return drives.register_image('win', self.data, source)

    def test_an_imported_image_is_stored_by_its_hash_with_a_record(self):
        image = self.register(self.image)
        digest = self.data['drives']['win']['sha256']
        self.assertEqual(image, self.store.resolve() / f'{digest}.img')
        self.assertEqual(image.stat().st_mode & 0o777, 0o600)
        found, record = drives.resolve('win', self.store)
        self.assertEqual(found, image)
        self.assertEqual(record['base']['kind'], 'image')
        self.assertEqual(drives.title_of(record, 'win')['sha256'], digest)
        self.assertEqual(drives.geometry(record), '1/16/63')
        self.assertEqual(self.register(), image)  # already stored: no source needed

    def test_a_missing_image_is_reported_as_missing(self):
        with self.assertRaises(drives.DriveMissing):
            self.register()

    def test_an_image_with_other_bytes_is_refused(self):
        self.data['drives']['win']['sha256'] = '0' * 64
        with self.assertRaisesRegex(drives.DriveError, 'the manifest pins'):
            self.register(self.image)

    def test_an_image_of_another_size_is_refused(self):
        self.data['drives']['win']['cylinders'] = 2
        with self.assertRaisesRegex(drives.DriveError, 'cylinders are'):
            self.register(self.image)

    def test_a_full_build_skips_a_missing_image_drive(self):
        manifest = Path(self.directory.name) / 'drives.toml'
        manifest.write_text('[buckets]\nprogram = ""\n[drives.win]\nkind = "image"\ncylinders = 1\n'
                            f'sha256 = "{"0" * 64}"\n[[drives.win.titles]]\nid = "win"\ntitle = "W"\n'
                            'bucket = "program"\nlaunch = "win|"\n')
        with mock.patch.object(drives, 'STORE', self.store), \
                mock.patch.dict(os.environ, {'FES_PC386_DRIVES': str(self.store)}), \
                mock.patch('sys.stdout', new_callable=io.StringIO) as out:
            self.assertEqual(drives.main(['--manifest', str(manifest)]), 0)
            self.assertIn('win: skipped', out.getvalue())
        with mock.patch.object(drives, 'STORE', self.store), \
                mock.patch.dict(os.environ, {'FES_PC386_DRIVES': str(self.store)}), \
                mock.patch('sys.stderr', new_callable=io.StringIO):
            self.assertEqual(drives.main(['--manifest', str(manifest), 'win']), 1)


class GamePassTest(unittest.TestCase):
    def test_game_report_identifies_zip_and_folder_sources(self):
        # The record retains the title's source name, omitting the default
        # eXoDOS source. Its sha256 is a zip hash or a tree hash accordingly.
        titles = [
            {'id': 'game', 'title': 'Game', 'bucket': 'real', 'sha256': 'a' * 64, 'launch': 'game|'},
            {'id': 'tool', 'title': 'Tool', 'bucket': 'program', 'source': 'dosbench',
             'sha256': 'b' * 64, 'launch': 'tool|'},
        ]
        record = {'drive': 'example', 'sha256': 'c' * 64, 'titles': titles}
        with tempfile.TemporaryDirectory() as directory, \
                mock.patch.object(sim, 'OUTPUT', Path(directory)), \
                mock.patch.object(drives, 'resolve', return_value=(Path(directory) / 'disk.img', record)), \
                mock.patch.object(sim.firmware, 'images', return_value={}), \
                mock.patch.object(sim, 'machine_testbench', return_value=Path('machine_tb')), \
                mock.patch.object(sim, 'run_machine', side_effect=lambda *args: {
                    'failures': [], 'end': {'reason': 'limit'},
                    'game_launch': {'seconds': 1.25, 'evidence': 'frame'}, 'opl2': None}):
            for title, source in zip(titles, ('exodos', 'dosbench')):
                with self.subTest(source=source):
                    args = mock.Mock(drive='example', game=title['id'], limit_seconds_given=None,
                                     game_keys=[], frame_every=0, rtl_at_prompt=False,
                                     instruction_census=False, oracle_profile=False, boot_entry=1,
                                     preset=0, frequency=0, turbo=False, cpu='oracle')
                    game = sim.run_game(args, [])['game']
                    self.assertEqual(game['source'], source)
                    self.assertEqual(game['source_sha256'], title['sha256'])
                    self.assertNotIn('zip_sha256', game)

    def test_the_game_report_parses_positive_launch_evidence(self):
        report = sim.parse_boot('GAME-LAUNCH seconds=1.250 evidence=screen\n')
        self.assertEqual(report['game_launch'], {'seconds': 1.25, 'evidence': 'screen'})

    def test_a_time_limit_without_launch_evidence_fails(self):
        report = {'failures': ['machine_tb exited 1: limit'], 'end': {'reason': 'limit'},
                  'game_launch': None, 'opl2': None}
        failures = sim.game_failures(report, 'oracle', opl2_expected=False)
        self.assertIn('machine_tb exited 1: limit', failures)
        self.assertIn('the game produced no screen or VGA progress after its launch command', failures)

    def test_a_time_limit_is_accepted_only_after_launch_progress(self):
        report = {'failures': ['machine_tb exited 1: limit'], 'end': {'reason': 'limit'},
                  'game_launch': {'seconds': 1.25, 'evidence': 'frame'}, 'opl2': None}
        self.assertEqual(sim.game_failures(report, 'oracle', opl2_expected=False), [])

    def test_returning_to_the_prompt_without_launch_evidence_fails(self):
        report = {'failures': [], 'end': {'reason': 'prompt'}, 'game_launch': None, 'opl2': None}
        self.assertIn('the game produced no screen or VGA progress after its launch command',
                      sim.game_failures(report, 'oracle', opl2_expected=False))

    def test_returning_to_the_prompt_is_accepted_after_launch_progress(self):
        report = {'failures': [], 'end': {'reason': 'prompt'},
                  'game_launch': {'seconds': 1.25, 'evidence': 'screen'}, 'opl2': None}
        self.assertEqual(sim.game_failures(report, 'oracle', opl2_expected=False), [])


class StorePathTest(unittest.TestCase):
    def test_fes_pc386_drives_override_cannot_resolve_inside_repository(self):
        override = drives.REPOSITORY_ROOT / 'out' / 'test-pc386-game-drive-store'
        with mock.patch.dict(os.environ, {'FES_PC386_DRIVES': str(override)}), \
                mock.patch('sys.stderr', new_callable=io.StringIO) as stderr:
            self.assertEqual(drives.main(['--list']), 1)
        self.assertIn('inside the repository', stderr.getvalue())
        self.assertFalse(override.exists())


class NameTest(unittest.TestCase):
    def test_dos_names(self):
        self.assertEqual(drives.dos_name('wolf3d.exe'), 'WOLF3D.EXE')
        self.assertEqual(drives.dos_name('SB16'), 'SB16')
        self.assertIsNone(drives.dos_name('Commander Keen.txt'))
        self.assertIsNone(drives.dos_name('manual.html'))
        self.assertIsNone(drives.dos_name('toolongname.exe'))


class ExtractionPathTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.zip_path = self.root / 'game.zip'

    def tearDown(self):
        self.directory.cleanup()

    def write_archive(self, entries):
        with zipfile.ZipFile(self.zip_path, 'w') as archive:
            for name, data in entries:
                archive.writestr(name, data)
        return {
            'sources': {'exodos': {'kind': 'zip', 'env': 'TEST_PC386_ZIP', 'default': str(self.root)}},
        }, {
            'id': 'game', 'path': self.zip_path.name, 'sha256': drives.sha256(self.zip_path),
            'copy': [{'from': 'Game', 'to': 'GAME'}],
        }

    def test_zip_dot_components_are_rejected_before_copying(self):
        data, title = self.write_archive([
            ('Game/GAME.EXE', b'MZgame'),
            ('Game/../../../TARGET', b'escape'),
        ])

        with self.assertRaisesRegex(drives.DriveError, 'unsafe archive path'):
            drives.extract(data, title, self.root / 'staging')

        self.assertFalse((self.root.parent / 'TARGET').exists())

    def test_copy_target_dot_components_are_rejected(self):
        data, title = self.write_archive([('Game/GAME.EXE', b'MZgame')])
        title['copy'][0]['to'] = r'GAME\..\outside'

        with self.assertRaisesRegex(drives.DriveError, 'unsafe copy target'):
            drives.extract(data, title, self.root / 'staging')

    def test_copy_target_symlinks_cannot_escape_the_staging_directory(self):
        data, title = self.write_archive([('Game/GAME.EXE', b'MZgame')])
        staging = self.root / 'staging'
        staging.mkdir()
        outside = self.root / 'outside'
        outside.mkdir()
        (staging / 'GAME').symlink_to(outside, target_is_directory=True)

        with self.assertRaisesRegex(drives.DriveError, 'path escapes staging directory'):
            drives.extract(data, title, staging)

        self.assertFalse((outside / 'GAME.EXE').exists())


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
