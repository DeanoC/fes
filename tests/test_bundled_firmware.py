import hashlib
import importlib.util
from io import BytesIO
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from zipfile import ZipFile

ROOT = Path(__file__).resolve().parents[1]


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


fw = load(ROOT / 'scripts/bundled_firmware.py', 'bundled_firmware')


def sha(data):
    return hashlib.sha256(data).hexdigest()


def tar_gz(members):
    out = BytesIO()
    with tarfile.open(fileobj=out, mode='w:gz') as archive:
        for name, data in members.items():
            info = tarfile.TarInfo(name)
            info.size = len(data)
            archive.addfile(info, BytesIO(data))
    return out.getvalue()


def zip_bytes(members):
    out = BytesIO()
    with ZipFile(out, 'w') as archive:
        for name, data in members.items():
            archive.writestr(name, data)
    return out.getvalue()


class BundledFirmwareTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        rom0, rom1 = b'\x00' * 16, b'\x01' * 16
        self.tar = tar_gz({'roms/a.rom': rom0, 'roms/b.rom': rom1})
        self.zip = zip_bytes({'os/os.img': b'\x02' * 32, 'os/license.txt': b'GPL text'})
        self.archives = {
            'roms': {'url': 'https://example.invalid/roms.orig.tar.gz', 'sha256': sha(self.tar)},
            'os': {'url': 'https://example.invalid/os.zip', 'sha256': sha(self.zip)},
        }
        self.firmware = (
            dict(id='pair', name='Pair', version='1', core_id='fes.pair', rom_id='pair-firmware',
                 file='pair.rom', archive='roms', members=('roms/a.rom', 'roms/b.rom'), size=32,
                 sha256=sha(rom0 + rom1), licence='LicenseRef-Test', licence_name='Test permission',
                 notice='Test notice.'),
            dict(id='os', name='OS', version='2', core_id='fes.os', rom_id='os-firmware', file='os.img',
                 archive='os', members=('os/os.img',), size=32, sha256=sha(b'\x02' * 32),
                 licence='GPL-2.0-only', licence_name='GNU General Public License v2',
                 licence_member='os/license.txt', notice='Free.',
                 source={'url': 'https://example.invalid/os-src.tar.gz', 'sha256': 'f' * 64}),
        )
        self.blobs = {self.archives['roms']['url']: self.tar, self.archives['os']['url']: self.zip}

    def fetcher(self, url, digest, cache):
        data = self.blobs[url]
        if sha(data) != digest:
            raise ValueError('digest')
        return data

    def test_stage_concatenates_members_and_writes_manifests(self):
        out = self.root / 'firmware'
        staged = fw.stage(out, self.root / 'cache', self.firmware, self.archives, self.fetcher)
        self.assertEqual([m['id'] for m in staged], ['pair', 'os'])
        self.assertEqual((out / 'pair/pair.rom').read_bytes(), b'\x00' * 16 + b'\x01' * 16)
        manifest = json.loads((out / 'os/manifest.json').read_text())
        self.assertEqual(manifest['format'], 1)
        self.assertEqual(manifest['licence_file'], 'LICENSE.txt')
        self.assertEqual(manifest['origin'], self.archives['os'])
        self.assertEqual(manifest['source']['sha256'], 'f' * 64)
        self.assertEqual((out / 'os/LICENSE.txt').read_bytes(), b'GPL text')
        self.assertNotIn('licence_file', json.loads((out / 'pair/manifest.json').read_text()))
        for path in out.rglob('*'):
            if path.is_file():
                self.assertEqual(path.stat().st_mode & 0o777, 0o444)

    def test_image_digest_mismatch_stages_nothing_for_it(self):
        bad = (dict(self.firmware[0], sha256='0' * 64),)
        with self.assertRaisesRegex(ValueError, 'pair image does not match'):
            fw.stage(self.root / 'firmware', self.root / 'cache', bad, self.archives, self.fetcher)
        self.assertFalse((self.root / 'firmware/pair').exists())

    def test_refuses_non_empty_output(self):
        out = self.root / 'firmware'
        out.mkdir()
        (out / 'old').write_text('x')
        with self.assertRaisesRegex(ValueError, 'not empty'):
            fw.stage(out, self.root / 'cache', self.firmware, self.archives, self.fetcher)

    def test_fetch_uses_cache_only_when_digest_matches(self):
        cache = self.root / 'cache'
        cache.mkdir()
        (cache / 'roms.orig.tar.gz').write_bytes(self.tar)
        self.assertEqual(fw.fetch(self.archives['roms']['url'], self.archives['roms']['sha256'], cache), self.tar)
        (cache / 'roms.orig.tar.gz').write_bytes(b'tampered')
        with self.assertRaises(OSError):
            # A bad cache entry is refetched; the invalid host fails instead of
            # returning the tampered bytes.
            fw.fetch(self.archives['roms']['url'], self.archives['roms']['sha256'], cache)

    def test_fetch_sources_copies_only_free_software_sources(self):
        self.blobs['https://example.invalid/os-src.tar.gz'] = b'source'
        firmware = (self.firmware[0], dict(self.firmware[1], source={'url': 'https://example.invalid/os-src.tar.gz', 'sha256': sha(b'source')}))
        written = fw.fetch_sources(self.root / 'sources', self.root / 'cache', firmware, self.fetcher)
        self.assertEqual([Path(p).name for p in written], ['os-src.tar.gz'])

    def test_v1_firmware_set_is_the_agreed_one(self):
        # Spectrum 48K, 128K/+2 and +2A/+3, and EmuTOS for the ST (Deano,
        # 1 November 2026). ZX81 family, Pentagon and C64 stay user-supplied.
        self.assertEqual(sorted(e['core_id'] for e in fw.FIRMWARE),
                         ['fes.atari-st', 'fes.spectrum', 'fes.spectrum-plus3', 'fes.spectrum128'])
        sizes = {e['core_id']: e['size'] for e in fw.FIRMWARE}
        self.assertEqual(sizes, {'fes.spectrum': 16384, 'fes.spectrum128': 32768,
                                 'fes.spectrum-plus3': 65536, 'fes.atari-st': 196608})
        for entry in fw.FIRMWARE:
            self.assertTrue(entry['notice'])
            self.assertIn(entry['archive'], fw.ARCHIVES)
            if entry['core_id'] != 'fes.atari-st':
                self.assertIn(fw.AMSTRAD_PERMISSION, entry['notice'])
        emutos = next(e for e in fw.FIRMWARE if e['core_id'] == 'fes.atari-st')
        self.assertEqual(emutos['licence'], 'GPL-2.0-only')
        self.assertTrue(emutos['source']['url'].endswith('emutos-src-1.4.tar.gz'))

    def test_emutos_pins_match_the_simulation_fetch_script(self):
        sim = load(ROOT / 'sources/misteross/scripts/fetch_atari_st_emutos.py', 'fetch_atari_st_emutos')
        emutos = next(e for e in fw.FIRMWARE if e['core_id'] == 'fes.atari-st')
        self.assertEqual(fw.ARCHIVES['emutos-192k']['url'], sim.URL)
        self.assertEqual(fw.ARCHIVES['emutos-192k']['sha256'], sim.ARCHIVE_SHA256)
        self.assertEqual(emutos['sha256'], sim.ROM_SHA256)
        self.assertIn(sim.SOURCE_COMMIT, emutos['notice'])


if __name__ == '__main__':
    unittest.main()
