# SPDX-License-Identifier: MIT
"""pc386 machine M1-M5: firmware/audio imports, boot reports, directed
device, audio, OPL2, presenter and port 2 benches, and the M5 system bench's
report and producer inputs. No network access; the oracle boot and VGA
replay are separate suites."""
import hashlib
import io
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import build_pc386_firmware as firmware
from scripts import build_pc386_freedos_image as freedos
from scripts import build_fes_pc386_quartus as producer
from scripts import sim_fes_pc386 as sim
from scripts import synth_fes_pc386 as synthesis

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
        self.assertEqual(set(lock['packages']), {'kernel', 'freecom', 'fdisk', 'himemx', 'ctmouse', 'mem'})
        self.assertEqual(set(lock['packages']), {name for spec in freedos.PROFILES.values() for name in spec['packages']})
        for spec in lock['packages'].values():
            self.assertRegex(spec['commit'], r'^[0-9a-f]{40}$')
            self.assertIn('GPL', spec['license'])
            self.assertIn('LICENSE', spec['files'])

    def test_profile_binary_files_are_pinned_in_the_lock(self):
        lock = freedos.lock()['packages']
        for profile, spec in freedos.PROFILES.items():
            for filename, source in spec['bin'].items():
                with self.subTest(profile=profile, filename=filename):
                    if source is None:
                        self.assertEqual(filename, 'FESENV.COM')
                        self.assertTrue(freedos.FESENV.is_file())
                        continue
                    package_name, package_path = source
                    self.assertIn(package_name, spec['packages'])
                    self.assertIn(package_path, lock[package_name]['files'])

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
            with mock.patch.dict(freedos.PROFILES['boot'], {'image': image}), \
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

    def test_games_image_identity_tracks_its_local_com_program(self):
        with tempfile.TemporaryDirectory(dir=freedos.ROOT) as directory:
            root = Path(directory)
            source = root / 'fesenv.asm'
            source.write_bytes(b'first version')
            image = root / 'freedos-games.img'
            image.write_bytes(b'disk')
            with mock.patch.object(freedos, 'FESENV', source), \
                    mock.patch.dict(freedos.PROFILES['games'], {'image': image}):
                boot_inputs = freedos.inputs_key('boot')
                record = {'sha256': hashlib.sha256(b'disk').hexdigest(),
                          'inputs': freedos.inputs_key('games')}
                image.with_suffix('.json').write_text(json.dumps(record))
                self.assertTrue(freedos.image_is_current('games'))
                source.write_bytes(b'changed version')
                self.assertNotEqual(record['inputs'], freedos.inputs_key('games'))
                self.assertFalse(freedos.image_is_current('games'))
                self.assertEqual(boot_inputs, freedos.inputs_key('boot'))

    def test_profiles_use_separate_work_directories(self):
        self.assertEqual(freedos.work_directory('boot'), freedos.BUILD / 'work-boot')
        self.assertEqual(freedos.work_directory('games'), freedos.BUILD / 'work-games')
        self.assertNotEqual(freedos.work_directory('boot'), freedos.work_directory('games'))

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
        for name, license_name in (('opl2_fpga', 'LGPL-3.0-or-later'),
                                   ('nuked-opl3', 'LGPL-2.1-or-later')):
            tree = sim.CORE / 'vendor' / name
            manifest = json.loads((tree / 'source.json').read_text())
            self.assertEqual(manifest['license'], license_name)
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
        square = [8000 if (n // 24) % 2 else -8000 for n in range(4800)]
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
IO vga-out 2 7 2697
IO vga-in 1 6 120
IO vga-in 1 31 4
INTA 146 52
SNAP text 720 400 frames/text.ppm
PRESENT text frames/text-720p.ppm 0
VGA scanout_underflows 0
SCREEN 24 |C:\\>dir                                                                         |
MARK 1200 speaker
OPL2 samples=144000 exact=144000 nonzero=5000 writes=120
AUDIO samples=139200
SB resets=1 commands=12 dma=6000 irqs=6
END prompt steps=69772120 cycles=290000013 seconds=2.900
"""

RTL_BOOT_OUTPUT = """PROMPT 100
RTL retired=42 oracle_steps=0 protected_steps=0 parked_clocks=0 tf_retired=0 vga_loads=0 replays=0 port0_reads=0 port0_writes=0 smc_stores=0 smc_addresses=0 smc_executed=0
END prompt steps=42 cycles=100 seconds=0.001
"""


class A20TestTest(unittest.TestCase):
    def test_suite_checks_are_the_programs_names(self):
        source = (sim.CORE / 'sim/a20_test.asm').read_text()
        names = {line.split("'")[1].rstrip('$') for line in source.splitlines() if line.startswith('name_')}
        suite = {name for checks in sim.A20_CHECKS.values() for name in checks}
        # Mode X, for MS-DOS on a kit, adds its own.
        self.assertEqual(names - suite, {'dos-start', 'dos-locks-0', 'dos-release', 'xms-global', 'xms-lock'})
        self.assertLessEqual(suite, names)

    def test_himemx_method_is_read_from_its_banner(self):
        self.assertEqual(sim.HIMEMX_METHOD.search('Fast A20 method used').group(1), 'Fast')
        self.assertEqual(sim.HIMEMX_METHOD.search('Always on A20 method used').group(1), 'Always on')
        self.assertIsNone(sim.HIMEMX_METHOD.search('Interface : XMS 3.00 80386 4G'))


class BootReportTest(unittest.TestCase):
    def test_boot_suite_output_names_cover_image_cpu_and_preset(self):
        cases = (
            ('boot', 'oracle', 0, 0, 0, 'boot'),
            ('games', 'oracle', 0, 0, 0, 'boot-games'),
            ('boot', 'rtl', 0, 0, 0, 'boot-rtl'),
            ('games', 'rtl', 0, 0, 0, 'boot-games-rtl'),
            ('boot', 'rtl', 1, 0, 0, 'boot-rtl-preset1'),
            ('games', 'rtl', 3, 0, 0, 'boot-games-rtl-preset3'),
            ('boot', 'rtl', 1, 60, 0, 'boot-rtl-preset1-frequency60'),
            ('boot', 'rtl', 1, 0, 1, 'boot-rtl-preset1-turbo'),
            ('boot', 'rtl', 1, 60, 1, 'boot-rtl-preset1-frequency60-turbo'),
        )
        for image, cpu, preset, frequency, turbo, expected in cases:
            with self.subTest(image=image, cpu=cpu, preset=preset, frequency=frequency, turbo=turbo):
                args = mock.Mock(image=image, cpu=cpu, preset=preset, frequency=frequency, turbo=turbo)
                self.assertEqual(sim.boot_suite(args), expected)

    def test_governor_report_suffix_distinguishes_frequency_and_turbo(self):
        cases = (
            (1, 0, 0, '-preset1'),
            (1, 60, 0, '-preset1-frequency60'),
            (1, 0, 1, '-preset1-turbo'),
            (1, 60, 1, '-preset1-frequency60-turbo'),
        )
        for preset, frequency, turbo, expected in cases:
            with self.subTest(preset=preset, frequency=frequency, turbo=turbo):
                args = mock.Mock(preset=preset, frequency=frequency, turbo=turbo)
                self.assertEqual(sim.suffix(args), expected)

    def test_non_boot_suites_reject_a_games_image(self):
        for suite in ('sound', 'game', 'vga', 'system', 'all'):
            with self.subTest(suite=suite), \
                    mock.patch.object(sim.sys, 'argv', ['sim_fes_pc386.py', '--suite', suite, '--image', 'games']), \
                    mock.patch('sys.stderr', new_callable=io.StringIO) as stderr:
                with self.assertRaises(SystemExit) as error:
                    sim.main()
                self.assertEqual(error.exception.code, 2)
                self.assertIn('--image is supported by the boot suite only', stderr.getvalue())

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
        self.assertEqual(report['snaps']['text'], {'width': 720, 'height': 400, 'file': 'frames/text.ppm'})
        self.assertEqual(report['present']['text'], {'file': 'frames/text-720p.ppm', 'mismatches': 0})
        self.assertEqual(report['vga'], {'scanout_underflows': 0})
        self.assertEqual(report['marks'], {'speaker': 1200})
        self.assertEqual(report['opl2'], {'samples': 144000, 'exact': 144000, 'nonzero': 5000, 'writes': 120})
        self.assertEqual(report['audio_samples'], 139200)
        self.assertEqual(report['sb'], {'resets': 1, 'commands': 12, 'dma': 6000, 'irqs': 6})

    def test_timing_classes(self):
        self.assertEqual(sim.timing_failures(sim.parse_boot(BOOT_OUTPUT)), [])
        slow = sim.parse_boot(BOOT_OUTPUT.replace('IO isa8 1 74 71477', 'IO isa8 1 74 71476\nIO isa8 1 75 1'))
        self.assertEqual(len(sim.timing_failures(slow)), 1)
        inta = sim.parse_boot(BOOT_OUTPUT.replace('INTA 146 52', 'INTA 140 52'))
        self.assertEqual(len(sim.timing_failures(inta)), 1)
        unknown = sim.parse_boot(BOOT_OUTPUT + 'IO isa16 1 99 1\n')
        self.assertEqual(len(sim.timing_failures(unknown)), 1)
        # A VGA write posts at a fixed time; a read takes at least its minimum
        # and at most the bound.
        posted = sim.parse_boot(BOOT_OUTPUT.replace('IO vga-out 2 7 2697', 'IO vga-out 2 8 2697'))
        self.assertEqual(len(sim.timing_failures(posted)), 1)
        early = sim.parse_boot(BOOT_OUTPUT.replace('IO vga-in 1 6 120', 'IO vga-in 1 5 120'))
        self.assertEqual(len(sim.timing_failures(early)), 1)
        late = sim.parse_boot(BOOT_OUTPUT + f'IO vga-in 1 {sim.VGA_READ_BOUND + 1} 1\n')
        self.assertEqual(len(sim.timing_failures(late)), 1)

    def test_rtl_boot_requires_the_rtl_to_reach_the_prompt_without_stops(self):
        passed = sim.parse_boot(RTL_BOOT_OUTPUT)
        self.assertEqual(sim.rtl_boot_failures(passed, 'rtl'), [])
        # The oracle boot keeps its existing acceptance rules.
        self.assertEqual(sim.rtl_boot_failures(passed, 'oracle'), [])

        cases = (
            (RTL_BOOT_OUTPUT.replace('retired=42', 'retired=0'), 'no instructions', None),
            (RTL_BOOT_OUTPUT.replace('oracle_steps=0', 'oracle_steps=1'), 'oracle_steps=1', None),
            (RTL_BOOT_OUTPUT.replace('protected_steps=0', 'protected_steps=1'), 'protected_steps=1', None),
            ('STOP stall 50 f000:00000000 00 00\n'
             'GAP stall 00 1 1 0 f000:00000000 50\n' + RTL_BOOT_OUTPUT, 'stall 00', 'unimplemented'),
            ('STOP unimplemented 50 f000:00000000 27 27\n'
             'GAP unimplemented 27 1 1 0 f000:00000000 50\n' + RTL_BOOT_OUTPUT,
             'unimplemented 27', 'stall 00'),
            (RTL_BOOT_OUTPUT.replace('PROMPT 100\n', '').replace('END prompt', 'END limit'), 'prompt', None),
            ('PROMPT 100\nEND prompt steps=42 cycles=100 seconds=0.001\n', 'counters', None),
        )
        for output, expected, wrong_classification in cases:
            with self.subTest(expected=expected):
                failures = sim.rtl_boot_failures(sim.parse_boot(output), 'rtl')
                self.assertTrue(any(expected in failure for failure in failures), failures)
                if wrong_classification:
                    self.assertFalse(any(wrong_classification in failure for failure in failures), failures)


@unittest.skipUnless(shutil.which(os.environ.get('CXX', 'c++')), 'requires a C++ compiler')
class SmcStaleTrackerTest(unittest.TestCase):
    def test_fetch_timing_and_multiple_writer_attribution(self):
        with tempfile.TemporaryDirectory(prefix='pc386-smc-stale-') as directory:
            binary = Path(directory) / 'smc-stale-test'
            source = sim.COMMON / 'sim/i386/smc_stale_tb.cpp'
            compiler = shutil.which(os.environ.get('CXX', 'c++'))
            result = subprocess.run([compiler, '-std=c++17', '-Wall', '-Wextra', '-Werror', str(source), '-o', str(binary)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            result = subprocess.run([str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


@unittest.skipUnless(shutil.which(os.environ.get('CXX', 'c++')), 'requires a C++ compiler')
class Port0ModelTest(unittest.TestCase):
    def test_waitrequest_holds_all_sixteen_byte_enable_lanes(self):
        with tempfile.TemporaryDirectory(prefix='pc386-port0-model-') as directory:
            binary = Path(directory) / 'pc386-port0-model-test'
            source = sim.CORE / 'sim/pc386_ddr_model_tb.cpp'
            compiler = shutil.which(os.environ.get('CXX', 'c++'))
            result = subprocess.run([compiler, '-std=c++17', '-Wall', '-Wextra', '-Werror', str(source), '-o', str(binary)],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            result = subprocess.run([str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


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


@unittest.skipUnless(VERILATOR, 'requires Verilator')
class PresenterBenchTest(unittest.TestCase):
    """The presenter and the plane on port 2 against their models (minutes)."""
    class Args:
        verilator = VERILATOR
        jobs = min(os.cpu_count() or 2, 16)

    def bench(self, name, top, rtl, testbench):
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(sim, 'OUTPUT', Path(directory)):
            report = sim.run_bench(self.Args(), name, top, rtl, testbench)
        self.assertEqual(report['failures'], [], '\n'.join(report['checks']))
        return report

    def test_presenter_matches_its_model(self):
        report = self.bench('presenter', 'pc386_presenter', sim.PRESENTER_RTL, sim.CORE / 'sim/presenter_tb.cpp')
        self.assertEqual(len(report['checks']), 10)

    def test_plane_reads_through_port_2(self):
        report = self.bench('plane', 'plane_top', sim.PLANE_RTL, sim.CORE / 'sim/plane_tb.cpp')
        self.assertEqual(len(report['checks']), 14)


class Port2SynthesisHarnessTest(unittest.TestCase):
    def test_pair_forwards_the_auxiliary_burst_count(self):
        self.assertIn('input wire [7:0] aux_burstcount', synthesis.PAIR)
        self.assertIn('.aux_burstcount(aux_burstcount)', synthesis.PAIR)


SYSTEM_OUTPUT = """WRITE-TEST 444436
IDENTITY 17734 12627 1 0 4 1 1 12297 26437 291 52617 1158 61715 2469 13863 3702 15
RESTORE 9000
CLOCK 9400
CRC-REJECTED 600000 flags=8
DISK 1200000 sectors=65520 verify_cycles=590000
RELEASE 1200100
POST 00 1265600
DEBUG BIOS BUILD DATE: 10/26/26
COUNTER cpu_resets=1 200000000
PROMPT 300000000
SCREEN 0 |C:\\>                                                                             |
STATUS running=1 halted=1 stopped=0 a20m=0 intr=0 post=00 cpu_resets=1 retired=50000000 cs=0070 vector=00 eip=00001234 frames_shown=600 scanout_underflows=0 capture_drops=0 present_underflows=0 plane_underflows=0 samples=480000 dma_cycles=0 opl2_samples=0
SAVE dirty=1 chunks=0 epoch=3 changed_bytes=12 changed_chunks=0
CHECK PASS Saved clears the dirty map and flag
RECORD 0 0 0 0 576 5 1 0
CHECK PASS the record keeps the restored settings
CHECK PASS CMOS 00h-0Dh retain seconds, minutes, hours, weekday, day and the 2026-10 clock mode
ROUNDTRIP 0 0 0 0 576 5 1 0
CHECK PASS the record's settings and CMOS RAM make the round trip through the RTC
CHECK PASS the running clock keeps valid seconds, minutes, hours, weekday and date after restore
END prompt cycles=303000000 seconds=4.932
"""


class SystemTest(unittest.TestCase):
    def test_parse(self):
        report = sim.parse_system(SYSTEM_OUTPUT)
        self.assertEqual(len(report['identity']), 17)
        self.assertEqual(report['identity'][16], 15)
        self.assertEqual(report['restore_cycle'], 9000)
        self.assertEqual(report['clock_cycle'], 9400)
        self.assertEqual(report['crc_rejected'], {'cycle': 600000, 'flags': 8})
        self.assertEqual(report['disk'], {'cycle': 1200000, 'sectors': 65520, 'verify_cycles': 590000})
        self.assertEqual(report['release_cycle'], 1200100)
        self.assertEqual(report['post'], [{'code': '00', 'cycle': 1265600}])
        self.assertEqual(report['debug'], ['BIOS BUILD DATE: 10/26/26'])
        self.assertEqual(report['counters'], [{'name': 'cpu_resets', 'value': 1, 'cycle': 200000000}])
        self.assertEqual(report['prompt_cycle'], 300000000)
        self.assertTrue(report['screen'][0].startswith('C:\\>'))
        self.assertEqual(report['status']['cs'], 0x70)
        self.assertEqual(report['status']['eip'], 0x1234)
        self.assertEqual(report['status']['retired'], 50000000)
        self.assertEqual(report['save'], {'dirty': 1, 'epoch': 3, 'changed_bytes': 12, 'chunks': [0],
                                          'changed_chunks': [0]})
        self.assertEqual(report['write_test'], 444436)
        self.assertEqual(report['record'][:8], [0, 0, 0, 0, 576, 5, 1, 0])
        self.assertEqual(len(report['record_checks']), 5)
        self.assertEqual(report['end'], {'reason': 'prompt', 'cycles': 303000000, 'seconds': 4.932})
        self.assertEqual(sim.system_failures(report), [])

    def test_failures(self):
        cases = (
            (SYSTEM_OUTPUT.replace('END prompt', 'END stalled'), 'did not reach the prompt'),
            (SYSTEM_OUTPUT.replace('stopped=0', 'stopped=1').replace('vector=00', 'vector=08'), 'vector 08'),
            (SYSTEM_OUTPUT.replace('present_underflows=0', 'present_underflows=3'), 'present_underflows=3'),
            (SYSTEM_OUTPUT.replace('DEBUG BIOS', 'DEBUG PANIC: BIOS'), 'panic'),
            (SYSTEM_OUTPUT.replace('retired=50000000', 'retired=0'), 'retired nothing'),
            (SYSTEM_OUTPUT.replace('CRC-REJECTED 600000 flags=8\n', ''), 'wrong CRC'),
            (SYSTEM_OUTPUT.replace('WRITE-TEST 444436', 'WRITE-TEST absent'), 'disk write'),
            (SYSTEM_OUTPUT.replace('CHECK PASS the record keeps', 'CHECK FAIL the record keeps'),
             'record check failed: the record keeps'),
            (SYSTEM_OUTPUT.replace('ROUNDTRIP 0 0 0 0 576 5 1 0\n', ''), 'did not save'),
            (SYSTEM_OUTPUT.replace('END prompt', 'MAILBOX no acknowledgement for opcode 17\nEND mailbox'),
             'mailbox: no acknowledgement'),
            ('END limit cycles=1 seconds=0.000\n', 'no status words'),
        )
        for output, expected in cases:
            with self.subTest(expected=expected):
                failures = sim.system_failures(sim.parse_system(output))
                self.assertTrue(any(expected in failure for failure in failures), failures)

    def test_producer_builds_what_the_bench_simulates(self):
        self.assertEqual({sim.ROOT / path for path in producer.MACHINE_SOURCES}, set(sim.SYSTEM_RTL))
        for path in producer.PINNED_INPUTS:
            self.assertTrue((sim.ROOT / path).is_file(), path)


if __name__ == '__main__':
    unittest.main()
