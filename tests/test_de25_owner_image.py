import io
import tarfile
from pathlib import Path
import tempfile
import unittest

from scripts import de25_owner_image as owner


class CandidateArchiveTests(unittest.TestCase):
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
