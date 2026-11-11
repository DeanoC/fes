"""Exercise locked module identity, ELF interpretation and dependency closure."""
import hashlib
import importlib.util
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile

LANE = Path(__file__).resolve().parent
sys.path.insert(0, str(LANE))
SPEC = importlib.util.spec_from_file_location('de25_kernel_modules', LANE / 'kernel_modules.py')
MODULES = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULES)


def elf(name, dependencies='', kernel='6.12.11-test'):
    strings = b'\0.shstrtab\0.modinfo\0'
    info = ('name=' + name + '\0depends=' + dependencies + '\0vermagic=' + kernel + ' SMP\0').encode()
    data = bytearray(64 + 3 * 64)
    data[:6] = b'\x7fELF\x02\x01'
    struct.pack_into('<HH', data, 16, 1, 183)
    struct.pack_into('<Q', data, 40, 64)
    struct.pack_into('<HHH', data, 58, 64, 3, 1)
    struct.pack_into('<IIQQQQIIQQ', data, 128, 1, 3, 0, 0, len(data), len(strings), 0, 0, 1, 0)
    data += strings
    struct.pack_into('<IIQQQQIIQQ', data, 192, 11, 1, 0, 0, len(data), len(info), 0, 0, 1, 0)
    return bytes(data) + info


class KernelModuleTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def module(self, filename, data):
        path = self.root / ('kernel/drivers/fpga/' + filename + '.ko')
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return {'path': path.relative_to(self.root).as_posix(), 'size': len(data),
                'sha256': hashlib.sha256(data).hexdigest()}

    def test_real_lock_matches_retained_hardware_module_identities(self):
        record = MODULES.load_lock()
        evidence = json_load(LANE.parents[1] / 'docs/validation/2026-10-30-de25-linux-fpga/artifacts.json')
        self.assertEqual(record['modules'], evidence['modules'])
        self.assertEqual(record['kernel'], evidence['kernel'])

    def test_dependency_graph_uses_actual_elf_names(self):
        bridge = self.module('fpga-bridge', elf('fpga_bridge'))
        region = self.module('fpga-region', elf('fpga_region', 'fpga_bridge'))
        record = {'kernel': '6.12.11-test', 'modules': [bridge, region]}
        result = MODULES.verify_modules(self.root, record)
        self.assertEqual(result, bridge['path'] + ':\n' + region['path'] + ': ' + bridge['path'] + '\n')

    def test_wrong_kernel_and_missing_dependency_rejected(self):
        for data, message in [(elf('fpga_region', kernel='wrong'), 'vermagic'),
                              (elf('fpga_region', 'absent'), 'outside selected closure')]:
            with self.subTest(message=message):
                row = self.module('fpga-region', data)
                with self.assertRaisesRegex(ValueError, message):
                    MODULES.verify_modules(self.root, {'kernel': '6.12.11-test', 'modules': [row]})

    def test_modified_or_symlinked_module_rejected(self):
        row = self.module('fpga-bridge', elf('fpga_bridge'))
        record = {'kernel': '6.12.11-test', 'modules': [row]}
        path = self.root / row['path']
        original = path.read_bytes()
        path.write_bytes(original[:-1] + b'x')
        with self.assertRaisesRegex(ValueError, 'locked payload'):
            MODULES.verify_modules(self.root, record)
        path.unlink()
        (self.root / 'external').write_bytes(original)
        path.symlink_to(self.root / 'external')
        with self.assertRaisesRegex(ValueError, 'non-symlink'):
            MODULES.verify_modules(self.root, record)

    def test_wrong_architecture_or_invalid_sections_rejected(self):
        valid = elf('fpga_bridge')
        variants = [b'not ELF', valid[:180]]
        other_arch = bytearray(valid)
        struct.pack_into('<H', other_arch, 18, 62)
        variants.append(other_arch)
        bad_section = bytearray(valid)
        struct.pack_into('<Q', bad_section, 192 + 24, len(valid) + 1)
        variants.append(bad_section)
        for data in variants:
            with self.subTest(size=len(data)), self.assertRaises(ValueError):
                MODULES.module_info_bytes(data)

    @unittest.skipUnless(shutil.which('debugfs') and shutil.which('mke2fs'),
                         'regular-file ext3 extraction runs in the image builder')
    def test_actual_ext3_and_zip_extraction_and_source_rejection(self):
        rows = [self.module('fpga-bridge', elf('fpga_bridge')),
                self.module('fpga-region', elf('fpga_region', 'fpga_bridge'))]
        record = {'kernel': '6.12.11-test', 'modules': rows}
        contents = self.root / 'filesystem/lib/modules' / record['kernel']
        shutil.copytree(self.root / 'kernel', contents / 'kernel')
        partition = self.root / 'partition.img'
        subprocess.run(['mke2fs', '-q', '-t', 'ext3', '-d', str(self.root / 'filesystem'),
                        str(partition), '8192'], check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        image = bytes(4096) + partition.read_bytes()
        archive = self.root / 'test-only-bsp.zip'
        with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as zipped:
            zipped.writestr('test-only.img', image)
        vendor = {'bsp': {'size': archive.stat().st_size,
                          'sha256': hashlib.sha256(archive.read_bytes()).hexdigest()},
                  'image_member': 'test-only.img', 'image_size': len(image),
                  'image_sha256': hashlib.sha256(image).hexdigest(),
                  'partitions': [{}, {'start_sector': 8, 'sectors': len(image[4096:]) // 512}]}
        output = self.root / 'overlay'
        with patch.object(MODULES.boot, 'load_lock', return_value=vendor):
            MODULES.prepare(archive, output, record)
            root = output / 'lib/modules' / record['kernel']
            self.assertEqual((root / 'modules.dep').read_text(), MODULES.verify_modules(root, record))
            self.assertEqual((output / 'etc/fes-kernel-release').read_text(), record['kernel'] + '\n')
            vendor['image_sha256'] = '0' * 64
            with self.assertRaisesRegex(ValueError, 'image hash differs'):
                MODULES.prepare(archive, self.root / 'rejected', record)
            self.assertFalse((self.root / 'rejected').exists())

    @unittest.skipUnless(os.geteuid() == 0 and shutil.which('chroot') and
                         Path('/lib/x86_64-linux-gnu/libc.so.6').exists(),
                         'boot service isolation runs as root in the amd64 image builder')
    def test_real_boot_service_checks_kernel_and_module_failures(self):
        root = self.root / 'isolated'
        for name in ['bin', 'etc', 'lib/x86_64-linux-gnu', 'lib64',
                     'sys/class/fpga_manager/fpga0', 'sys/class/fpga_region/region0']:
            (root / name).mkdir(parents=True, exist_ok=True)
        shutil.copyfile('/bin/dash', root / 'bin/sh')
        shutil.copyfile('/lib/x86_64-linux-gnu/libc.so.6', root / 'lib/x86_64-linux-gnu/libc.so.6')
        shutil.copyfile('/lib64/ld-linux-x86-64.so.2', root / 'lib64/ld-linux-x86-64.so.2')
        (root / 'bin/sh').chmod(0o755)
        (root / 'lib64/ld-linux-x86-64.so.2').chmod(0o755)
        scripts = {'cat': 'IFS= read -r line < "$1"; printf "%s\\n" "$line"',
                   'uname': 'IFS= read -r line < /actual-kernel; printf "%s\\n" "$line"',
                   'modprobe': 'printf "%s\\n" "$1" >> /probes; [ ! -e /fail-module ]'}
        for name, body in scripts.items():
            path = root / 'bin' / name
            path.write_text('#!/bin/sh\n' + body + '\n')
            path.chmod(0o755)
        (root / 'etc/fes-kernel-release').write_text('6.12.11-test\n')
        (root / 'actual-kernel').write_text('wrong\n')
        (root / 'sys/class/fpga_manager/fpga0/name').write_text('fixture manager\n')
        shutil.copyfile(LANE / 'rootfs/overlay/etc/init.d/S15fpga', root / 'service')
        (root / 'service').chmod(0o755)
        def start():
            return subprocess.run([shutil.which('chroot'), str(root), '/service', 'start'],
                                  env={'PATH': '/bin'}, capture_output=True, text=True)
        wrong = start()
        self.assertNotEqual(wrong.returncode, 0)
        self.assertIn('kernel version differs', wrong.stderr)
        self.assertFalse((root / 'probes').exists())
        (root / 'actual-kernel').write_text('6.12.11-test\n')
        good = start()
        self.assertEqual(good.returncode, 0, good.stderr)
        self.assertIn('FPGA drivers: ready', good.stdout)
        self.assertEqual((root / 'probes').read_text(), 'stratix10-soc\nof-fpga-region\n')
        (root / 'fail-module').touch()
        failed = start()
        self.assertNotEqual(failed.returncode, 0)
        self.assertNotIn('drivers: ready', failed.stdout)


def json_load(path):
    import json
    return json.loads(path.read_text())


if __name__ == '__main__':
    unittest.main()
