import io
import tarfile
from pathlib import Path
import tempfile
import unittest

from scripts import de25_owner_image as owner


class CandidateArchiveTests(unittest.TestCase):
    def test_owner_permissions_rule_preserves_all_other_device_rules(self):
        base = b'console root:tty 600 @chmod 600 $MDEV\n$MODALIAS=.* root:root 660 @modprobe "$MODALIAS"\n'
        kernel = {'fes_revision': 'a' * 40, 'files': {'Image': {'sha256': 'b' * 64}},
                  'programming_contract': owner.PROGRAMMING_CONTRACT, 'kernel_notes_sha256': 'e' * 64}
        changes = owner.root_changes({'fes_revision': 'c' * 40}, kernel, 'd' * 40, base)
        import json
        record = json.loads(changes['usr/share/fes/kernel-modules.json'][0])
        self.assertEqual(record['programming_contract'], owner.PROGRAMMING_CONTRACT)
        self.assertEqual(record['kernel_notes_sha256'], 'e' * 64)
        self.assertEqual(changes['etc/mdev.conf'], (owner.OWNER_MDEV_RULE + base, 0o644))
        self.assertIn(b'@chmod 600 /dev/fes-de25-scanout', changes['etc/mdev.conf'][0])
        with self.assertRaisesRegex(ValueError, 'already contains'):
            owner.root_changes({'fes_revision': 'c' * 40}, kernel, 'd' * 40, owner.OWNER_MDEV_RULE + base)

    def add(self, archive, name, data=b'payload', **metadata):
        item = tarfile.TarInfo('./' + name)
        for key, value in metadata.items():
            setattr(item, key, value)
        if item.isreg():
            item.size = len(data)
        archive.addfile(item, io.BytesIO(data) if item.isreg() else None)

    def test_vendor_modules_removed_without_changing_library_links_or_data(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            original, output = root / 'source.tar', root / 'candidate.tar'
            with tarfile.open(original, 'w') as archive:
                self.add(archive, 'lib/libc.so.6', b'unchanged libc', mode=0o755, uid=0, gid=0)
                self.add(archive, 'lib/ld.so', type=tarfile.SYMTYPE, linkname='libc.so.6')
                self.add(archive, 'usr/bin/[', type=tarfile.SYMTYPE, linkname='../../bin/busybox')
                self.add(archive, 'usr/bin/[[', type=tarfile.SYMTYPE, linkname='../../bin/busybox')
                self.add(archive, 'lib/modules', type=tarfile.DIRTYPE)
                self.add(archive, 'lib/modules/vendor', type=tarfile.DIRTYPE)
                self.add(archive, 'lib/modules/vendor/driver.ko', b'old module')
                self.add(archive, 'etc/fes-kernel-release', b'old\n')
            additions = {'etc/fes-kernel-release': (b'new\n', 0o644)}
            owner.rewritten_tar(original, output, additions, 1751459412)
            with tarfile.open(output) as archive:
                items = owner.members(archive)
                self.assertFalse(any(n.startswith('lib/modules') for n in items))
                self.assertEqual(archive.extractfile(items['lib/libc.so.6']).read(), b'unchanged libc')
                self.assertEqual(items['lib/libc.so.6'].mode, 0o755)
                self.assertEqual(items['lib/ld.so'].linkname, 'libc.so.6')
                self.assertTrue(items['lib/ld.so'].issym())
                for name in ('usr/bin/[', 'usr/bin/[['):
                    self.assertEqual(items[name].linkname, '../../bin/busybox')
                self.assertEqual(archive.extractfile(items['etc/fes-kernel-release']).read(), b'new\n')
                self.assertEqual(items['etc/fes-kernel-release'].uid, 0)
                self.assertEqual(items['etc/fes-kernel-release'].mtime, 1751459412)
            with tarfile.open(original) as archive:
                self.assertEqual(archive.extractfile('./etc/fes-kernel-release').read(), b'old\n')

    def test_traversal_and_command_characters_are_refused(self):
        for name in ('/etc/passwd', '../outside', './lib/modules/../../etc/shadow', 'x;rm /y', 'name with space'):
            with self.subTest(name=name), self.assertRaises(ValueError):
                owner.canonical_name(name)

    def test_duplicate_archive_path_is_refused(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / 'duplicate.tar'
            with tarfile.open(path, 'w') as archive:
                self.add(archive, 'etc/marker')
                self.add(archive, 'etc/marker')
            with tarfile.open(path) as archive, self.assertRaises(ValueError):
                owner.members(archive)


class KernelIdentityTests(unittest.TestCase):
    def test_notes_bind_the_linked_range_and_refuse_ambiguous_or_invalid_inputs(self):
        import hashlib
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            image = bytearray(256)
            image[56:60] = b'ARMd'
            image[128:144] = bytes(range(16))
            (root / 'Image').write_bytes(image)
            symbols = '1000 T _text\n1080 R __start_notes\n1090 R __stop_notes\n'
            (root / 'System.map').write_text(symbols)
            self.assertEqual(owner.kernel_notes(root), hashlib.sha256(bytes(range(16))).hexdigest())
            for broken in (symbols + '1080 R __start_notes\n',
                           symbols.replace('1090', '1200'), symbols.replace('1080', '0ff0'),
                           symbols.replace('1090', '1080')):
                (root / 'System.map').write_text(broken)
                with self.assertRaises(ValueError):
                    owner.kernel_notes(root)
            (root / 'System.map').write_text(symbols)
            image[56] = 0
            (root / 'Image').write_bytes(image)
            with self.assertRaises(ValueError):
                owner.kernel_notes(root)
