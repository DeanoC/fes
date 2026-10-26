# SPDX-License-Identifier: MIT
"""pc386 machine M1 and M3: the firmware and FreeDOS locks, the image layout
helpers, the vendored audio imports, the boot report parser and its I/O timing
and audio checks, and (with Verilator) the directed device benches and a short
OPL2 run against Nuked-OPL3. No network access; the oracle boot and the sound
suite run from scripts/sim_fes_pc386.py --suite boot and --suite sound."""
import hashlib
import json
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import build_pc386_firmware as firmware
from scripts import build_pc386_freedos_image as freedos
from scripts import sim_fes_pc386 as sim

VERILATOR = shutil.which(os.environ.get('VERILATOR', 'verilator'))


class FirmwareTest(unittest.TestCase):
    def test_vendored_sources_match_their_manifests(self):
        for name in ('rombios', 'vgabios'):
            firmware.verify_sources(name)

    def test_changed_files_record_upstream_hashes_and_dated_changes(self):
        for name in ('rombios', 'vgabios'):
            manifest = json.loads((firmware.FIRMWARE / name / 'source.json').read_text())
            self.assertEqual(manifest['license'], 'LGPL-2.1-or-later')
            changed = [item for item in manifest['files'] if 'upstream_sha256' in item]
            self.assertEqual(bool(changed), bool(manifest['local_changes']), name)
            for change in manifest['local_changes']:
                self.assertRegex(change, r'^\d{4}-\d{2}-\d{2} ')
            for item in changed:
                text = (firmware.FIRMWARE / name / item['path']).read_text(errors='replace')
                self.assertIn('FES 20', text, f'{name}/{item["path"]} marks no local change')

    def test_a_changed_source_is_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            tree = Path(directory) / 'vgabios'
            shutil.copytree(firmware.FIRMWARE / 'vgabios', tree)
            with (tree / 'vgabios.h').open('a') as stream:
                stream.write('\n')
            with mock.patch.object(firmware, 'FIRMWARE', Path(directory)):
                with self.assertRaisesRegex(firmware.FirmwareError, 'does not match'):
                    firmware.verify_sources('vgabios')

    def test_lock_pins_dev86_and_both_images(self):
        lock = firmware.read_lock()
        self.assertEqual(lock['dev86']['revision'], firmware.DEV86_REVISION)
        for name, spec in firmware.IMAGES.items():
            self.assertEqual(lock['images'][name]['bytes'], spec['bytes'])
            self.assertRegex(lock['images'][name]['sha256'], r'^[0-9a-f]{64}$')


class FreedosImageTest(unittest.TestCase):
    def test_lock_pins_each_package_at_the_release_tag(self):
        lock = freedos.lock()
        self.assertEqual(lock['tag'], 'FreeDOS-v1.4')
        self.assertEqual(set(lock['packages']), {'kernel', 'freecom', 'fdisk'})
        for spec in lock['packages'].values():
            self.assertRegex(spec['commit'], r'^[0-9a-f]{40}$')
            self.assertIn('GPL', spec['license'])
            self.assertIn('LICENSE', spec['files'])

    def test_geometry_and_partition_addresses(self):
        self.assertEqual(freedos.TOTAL_SECTORS, 65 * 16 * 63)
        self.assertEqual(freedos.chs(freedos.PARTITION_START), bytes((1, 1, 0)))
        # The last sector: cylinder 64, head 15, sector 63.
        self.assertEqual(freedos.chs(freedos.TOTAL_SECTORS - 1), bytes((15, 63, 64)))

    def test_cached_image_is_used_only_when_its_inputs_match(self):
        with tempfile.TemporaryDirectory() as directory:
            image = Path(directory) / 'freedos.img'
            image.write_bytes(b'disk')
            record = {'sha256': freedos.hashlib.sha256(b'disk').hexdigest(), 'inputs': freedos.inputs_key()}
            image.with_suffix('.json').write_text(json.dumps(record))
            with mock.patch.object(freedos, 'IMAGE', image), \
                    mock.patch.object(freedos, 'build', return_value='rebuilt') as build:
                self.assertEqual(freedos.image(), image)
                image.with_suffix('.json').write_text(json.dumps({**record, 'inputs': 'old'}))
                self.assertEqual(freedos.image(), 'rebuilt')
                image.with_suffix('.json').write_text(json.dumps(record))
                image.write_bytes(b'changed')
                self.assertEqual(freedos.image(), 'rebuilt')
                image.with_suffix('.json').write_text('{')
                self.assertEqual(freedos.image(), 'rebuilt')
                self.assertEqual(build.call_count, 3)

    def test_mbr_code_is_read_from_the_fdisk_array(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / 'bootnorm.c'
            values = [0xfa] + [0] * 509 + [85, 170]
            source.write_text('const unsigned char bootnormal_code[512] = {\n' +
                              ',\n'.join(str(v) for v in values) + '\n};\n')
            self.assertEqual(freedos.mbr_code(source), bytes([0xfa] + [0] * 445))
            source.write_text('const unsigned char bootnormal_code[512] = {1, 2};')
            with self.assertRaises(freedos.ImageError):
                freedos.mbr_code(source)

class AudioImportTest(unittest.TestCase):
    def test_vendored_files_match_their_manifests(self):
        for name, license in (('opl2_fpga', 'LGPL-3.0-or-later'), ('nuked-opl3', 'LGPL-2.1-or-later')):
            tree = sim.CORE / 'vendor' / name
            manifest = json.loads((tree / 'source.json').read_text())
            self.assertEqual(manifest['license'], license)
            self.assertRegex(manifest['commit'], r'^[0-9a-f]{40}$')
            listed = {item['path'] for item in manifest['files']}
            self.assertEqual(listed, {path.name for path in tree.iterdir()} - {'source.json', 'README.md', '.gitignore'},
                             name)
            for item in manifest['files']:
                digest = hashlib.sha256((tree / item['path']).read_bytes()).hexdigest()
                self.assertEqual(digest, item['sha256'], f'{name}/{item["path"]}')

    def test_every_changed_import_file_says_why(self):
        manifest = json.loads((sim.OPL2_VENDOR / 'source.json').read_text())
        changed = {item['path'] for item in manifest['files'] if 'upstream_sha256' in item}
        described = {change.split(':')[0] for change in manifest['local_changes']}
        self.assertEqual(changed, described)

    def test_every_import_file_is_built(self):
        built = {path.name for path in sim.OPL2_RTL if path.parent == sim.OPL2_VENDOR}
        self.assertEqual(built, {path.name for path in sim.OPL2_VENDOR.glob('*.sv')})

    def test_tone(self):
        square = [8000 if (n // 24) % 2 else -8000 for n in range(4800)]   # 1 kHz at 48 kHz
        section = sim.tone(square)
        self.assertAlmostEqual(section['hz'], 1000.0, delta=1)
        self.assertEqual(section['peak'], 8000)
        self.assertEqual(sim.tone([0] * 100)['hz'], 0.0)


BOOT_OUTPUT = """POST 00 11397
DEBUG 402 BIOS BUILD DATE: 10/26/26
PROMPT 240000013
IO isa8 1 74 71477
IO isa8 2 146 249
IO isa16 2 38 144384
IO vga 2 5 2697
INTA 146 52
SCREEN 24 |C:\\>dir                                                                         |
MARK 1200 speaker
OPL2 samples=144000 exact=144000 nonzero=5000 writes=120
AUDIO samples=139200
END prompt steps=69772120 cycles=290000013 seconds=2.900
"""


class BootReportTest(unittest.TestCase):
    def test_parse(self):
        report = sim.parse_boot(BOOT_OUTPUT)
        self.assertEqual(report['post'], [{'code': '00', 'cycle': 11397}])
        self.assertEqual(report['debug'], [{'port': '402', 'text': 'BIOS BUILD DATE: 10/26/26'}])
        self.assertEqual(report['io']['isa8 1'], {'74': 71477})
        self.assertEqual(report['inta'], {'146': 52})
        self.assertEqual(report['prompt_cycle'], 240000013)
        self.assertTrue(report['screen'][0].startswith('C:\\>dir'))
        self.assertEqual(report['end']['reason'], 'prompt')
        self.assertEqual(report['end']['seconds'], 2.9)
        self.assertEqual(report['marks'], {'speaker': 1200})
        self.assertEqual(report['opl2'], {'samples': 144000, 'exact': 144000, 'nonzero': 5000, 'writes': 120})
        self.assertEqual(report['audio_samples'], 139200)

    def test_timing_classes(self):
        self.assertEqual(sim.timing_failures(sim.parse_boot(BOOT_OUTPUT)), [])
        slow = sim.parse_boot(BOOT_OUTPUT.replace('IO isa8 1 74 71477', 'IO isa8 1 74 71476\nIO isa8 1 75 1'))
        self.assertEqual(len(sim.timing_failures(slow)), 1)
        inta = sim.parse_boot(BOOT_OUTPUT.replace('INTA 146 52', 'INTA 140 52'))
        self.assertEqual(len(sim.timing_failures(inta)), 1)
        unknown = sim.parse_boot(BOOT_OUTPUT + 'IO isa16 1 99 1\n')
        self.assertEqual(len(sim.timing_failures(unknown)), 1)


@unittest.skipUnless(VERILATOR, 'requires Verilator')
class DeviceBenchTest(unittest.TestCase):
    def test_directed_device_checks(self):
        class Args:
            verilator = VERILATOR
            jobs = min(os.cpu_count() or 2, 16)
        # A private build directory: make sim-fes-pc386 may build the same
        # bench in build/ at the same time.
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(sim, 'OUTPUT', Path(directory)):
            report = sim.run_devices(Args())
        self.assertEqual(report['failures'], [], '\n'.join(report['checks']))
        self.assertGreater(len(report['checks']), 50)

    def test_opl2_against_nuked_opl3(self):
        class Args:
            verilator = VERILATOR
            jobs = min(os.cpu_count() or 2, 16)
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(sim, 'OUTPUT', Path(directory)), \
                mock.patch.object(sim, 'OPL2_SEEDS', (7,)), mock.patch.object(sim, 'OPL2_SAMPLES', 4000):
            report = sim.run_opl2(Args())
        self.assertEqual(report['failures'], [], '\n'.join(report['checks']))
        random = report['runs'][0]
        self.assertEqual(random['exact'], random['samples'])
        self.assertGreater(random['nonzero'], 3000)


if __name__ == '__main__':
    unittest.main()
