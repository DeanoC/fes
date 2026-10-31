"""A 360 KiB FAT12 fixture must preserve the selected boot code and files."""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import build_pcxt_freedos_image as disk


class FixtureSourceTest(unittest.TestCase):
    def test_no_download_without_explicit_opt_in(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(disk, 'BUILD', Path(directory)), \
                 patch.object(disk.urllib.request, 'urlopen') as network, \
                 patch.object(disk, 'run') as command:
                with self.assertRaisesRegex(disk.ImageError, 'use --download'):
                    disk.package('kernel', {'commit': 'not-cached'}, False)
                with self.assertRaisesRegex(disk.ImageError, 'use --download'):
                    disk.nasm({'version': '2.16.03'}, False, 1)
                network.assert_not_called()
                command.assert_not_called()

    def test_pinned_nasm_version_allows_upstream_build_suffix(self):
        for version in ('NASM version 2.16.03', 'NASM version 2.16.03 compiled on Oct 30 2026'):
            with patch.object(disk, 'run', return_value=version):
                disk.check_nasm_version('nasm', '2.16.03')
        for version in ('NASM version 2.16.030', 'NASM version 2.16.02'):
            with patch.object(disk, 'run', return_value=version):
                with self.assertRaisesRegex(disk.ImageError, 'version differs'):
                    disk.check_nasm_version('nasm', '2.16.03')

    def test_cached_source_is_verified_before_use(self):
        with tempfile.TemporaryDirectory() as directory:
            build = Path(directory)
            source = build / 'repo'
            source.mkdir()
            subprocess.run(['git', 'init', '-q'], cwd=source, check=True)
            (source / 'payload').write_bytes(b'checked')
            subprocess.run(['git', '-c', 'user.name=test', '-c', 'user.email=test@example.invalid',
                            'add', 'payload'], cwd=source, check=True)
            subprocess.run(['git', '-c', 'user.name=test', '-c', 'user.email=test@example.invalid',
                            'commit', '-qm', 'fixture'], cwd=source, check=True)
            revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=source, text=True).strip()
            source.rename(build / ('kernel-' + revision))
            spec = {'commit': revision, 'files': {'payload': disk.digest(b'checked')}}
            with patch.object(disk, 'BUILD', build):
                self.assertEqual(disk.package('kernel', spec, False), build / ('kernel-' + revision))
                (build / ('kernel-' + revision) / 'payload').write_bytes(b'tampered')
                with self.assertRaisesRegex(disk.ImageError, 'differs'):
                    disk.package('kernel', spec, False)


@unittest.skipUnless(shutil.which('mformat') and shutil.which('mcopy'), 'requires mtools')
class FloppyImageTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.work = Path(temporary.name)
        self.kernel = self.work / 'kernel'
        self.freecom = self.work / 'freecom'
        (self.kernel / 'BIN').mkdir(parents=True)
        (self.kernel / 'SOURCE/KERNEL/boot').mkdir(parents=True)
        (self.freecom / 'BIN').mkdir(parents=True)
        self.files = {'KERNEL.SYS': bytes((i * 7) & 255 for i in range(2501)),
                      'COMMAND.COM': bytes((i * 11) & 255 for i in range(1701)),
                      'FDCONFIG.SYS': disk.FDCONFIG, 'AUTOEXEC.BAT': disk.AUTOEXEC,
                      'FESXT.TXT': disk.MARKER}
        (self.kernel / 'BIN/KERNL86N.SYS').write_bytes(self.files['KERNEL.SYS'])
        (self.kernel / 'SOURCE/KERNEL/boot/boot.asm').write_text('synthetic fixture input\n')
        (self.freecom / 'BIN/COMMAND.COM').write_bytes(self.files['COMMAND.COM'])
        self.boot = bytearray((i * 17) & 255 for i in range(512))
        self.boot[-2:] = b'\x55\xaa'
        self.bootfile = self.work / 'boot-template.bin'
        self.bootfile.write_bytes(self.boot)
        self.nasm = self.work / 'nasm'
        self.nasm.write_text('#!/usr/bin/env python3\n'
            'import shutil,sys\n'
            'if sys.argv[1:] == ["-v"]: print("NASM version 2.16.03")\n'
            f'else: shutil.copyfile({str(self.bootfile)!r},sys.argv[sys.argv.index("-o")+1])\n')
        self.nasm.chmod(0o755)
        self.build = self.work / 'build'
        self.output = self.build / 'freedos-360.img'
        def package(name, spec, download):
            return {'kernel': self.kernel, 'freecom': self.freecom}[name]
        selected = disk.lock()
        selected['nasm']['boot_code_sha256'] = disk.digest(self.boot[:11] + self.boot[0x3e:])
        selected['packages']['kernel']['files']['BIN/KERNL86N.SYS'] = disk.digest(self.files['KERNEL.SYS'])
        selected['packages']['freecom']['files']['BIN/COMMAND.COM'] = disk.digest(self.files['COMMAND.COM'])
        self.patches = [patch.object(disk, 'lock', return_value=selected),
                        patch.object(disk, 'BUILD', self.build),
                        patch.object(disk, 'IMAGE', self.output),
                        patch.object(disk, 'package', side_effect=package)]
        for item in self.patches:
            item.start()
            self.addCleanup(item.stop)
        self.output = disk.image(False, nasm_path=self.nasm)
        self.data = self.output.read_bytes()

    def test_reproducible_real_fat12_volume_and_file_contents(self):
        first_hash = disk.digest(self.data)
        self.assertEqual(self.output.stat().st_size, 368640)
        self.assertEqual(self.data[-2:], bytes(2))  # last sector is unallocated
        detail = disk.validate_image(self.data, self.boot, self.files)
        self.assertEqual(detail['files']['FESXT.TXT']['sha256'], disk.digest(disk.MARKER))
        self.assertEqual(disk.image(False, nasm_path=self.nasm).read_bytes(), self.data)
        self.assertEqual(json.loads(self.output.with_suffix('.json').read_text())['sha256'], first_hash)

    def test_concurrent_builds_publish_matching_image_and_receipt(self):
        with ThreadPoolExecutor(max_workers=3) as executor:
            paths = list(executor.map(lambda _: disk.image(False, nasm_path=self.nasm), range(3)))
        self.assertTrue(all(path == self.output for path in paths))
        self.assertEqual(self.output.read_bytes(), self.data)
        self.assertEqual(json.loads(self.output.with_suffix('.json').read_text())['sha256'],
                         disk.digest(self.data))

    def test_corrupt_bpb_boot_code_fat_or_kernel_are_rejected(self):
        mutations = {
            'bpb': 11,
            'volume serial': 0x27,
            'boot code': 0x50,
            'signature': 510,
            'second FAT': 3 * 512 + 10,
            'kernel name': 5 * 512 + 32,
            'kernel data': 12 * 512,
        }
        for label, offset in mutations.items():
            with self.subTest(label=label):
                data = bytearray(self.data)
                data[offset] ^= 1
                with self.assertRaises(disk.ImageError):
                    disk.validate_image(data, self.boot, self.files)

    def test_content_validation_accepts_compatible_unallocated_bytes_but_binds_files(self):
        detail = disk.validate_selected_image(self.data)
        self.assertEqual(detail['image_sha256'], disk.digest(self.data))
        alternative = bytearray(self.data)
        alternative[-1] ^= 1  # unallocated sector: file contents and geometry unchanged
        self.assertNotEqual(disk.digest(alternative), disk.digest(self.data))
        disk.validate_selected_image(alternative)
        alternative[12 * 512] ^= 1
        with self.assertRaisesRegex(disk.ImageError, 'pinned fixture'):
            disk.validate_selected_image(alternative)
        bad_boot = bytearray(self.data)
        bad_boot[0x50] ^= 1
        with self.assertRaisesRegex(disk.ImageError, 'boot code'):
            disk.validate_selected_image(bad_boot)

    def test_cyclic_fat_chain_is_rejected_even_with_matching_fat_copies(self):
        data = bytearray(self.data)
        first = disk.read_root(data)[0][1]
        offset = first + first // 2
        for start in (512, 3 * 512):
            pair = int.from_bytes(data[start+offset:start+offset+2], 'little')
            if first & 1:
                pair = (pair & 0x000f) | (first << 4)
            else:
                pair = (pair & 0xf000) | first
            data[start+offset:start+offset+2] = pair.to_bytes(2, 'little')
        with self.assertRaisesRegex(disk.ImageError, 'cluster chain'):
            disk.validate_image(data, self.boot, self.files)


if __name__ == '__main__':
    unittest.main()
