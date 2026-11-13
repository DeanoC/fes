"""Keep the experimental ATF replacement bounded to a locked off-board bundle."""
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import zlib

from scripts import de25_sdm_diagnostic as diagnostic
from tests.test_de25_boot_contract import fdt, node, word


def fixture():
    stock_atf, firmware, elf = b'original ATF' * 10, b'corrected ATF' * 5, b'ELF fixture'
    blobs = {'uboot': b'vendor U-Boot', 'atf': stock_atf, 'fdt-0': b'vendor board tree'}
    offsets = {'uboot': 4096, 'atf': 8192, 'fdt-0': 12288}
    images = []
    for name, blob in blobs.items():
        properties = [('type', {'uboot': b'standalone\0', 'atf': b'firmware\0',
                                'fdt-0': b'flat_dt\0'}[name]),
                      ('data-position', word(offsets[name])), ('data-size', word(len(blob)))]
        if name == 'atf':
            properties.extend([('load', word(0x80000000)), ('entry', word(0x80000000))])
        images.append(node(name, properties, [node('hash', [('algo', b'crc32\0'),
                             ('value', word(zlib.crc32(blob) & 0xffffffff))])]))
    configuration = node('configurations', [('default', b'board-0\0')], [node('board-0', [
        ('firmware', b'atf\0'), ('loadables', b'uboot\0'), ('fdt', b'fdt-0\0')], [
            node('signature', [('algo', b'crc32\0'), ('key-name-hint', b'dev\0')])])])
    tree = fdt(node(children=[node('images', children=images), configuration]))
    stock = bytearray(16384)
    stock[:len(tree)] = tree
    for name, data in blobs.items():
        stock[offsets[name]:offsets[name] + len(data)] = data
    stock = bytes(stock)
    size_word, crc_word = word(len(stock_atf)), word(zlib.crc32(stock_atf) & 0xffffffff)
    assert tree.count(size_word) == tree.count(crc_word) == 1
    lock = dict(format=1, board='de25-nano', classification='unqualified-sdm-error-diagnostic',
                production_admission=False, stock_fit_sha256=diagnostic.sha(stock),
                stock_fit_bytes=len(stock), stock_bl31_sha256=diagnostic.sha(stock_atf),
                bl31_sha256=diagnostic.sha(firmware), bl31_elf_sha256=diagnostic.sha(elf),
                atf_offset=offsets['atf'], atf_source_bytes=len(stock_atf),
                atf_size_field=tree.index(size_word), atf_crc32_field=tree.index(crc_word))
    return stock, firmware, elf, lock


class FitReplacementTests(unittest.TestCase):
    def test_only_selected_payload_size_and_crc_change(self):
        stock, firmware, _, lock = fixture()
        changed = diagnostic.patch_fit(stock, firmware, lock)
        before, images = diagnostic.contract.fit_images(stock)
        after, result = diagnostic.contract.fit_images(changed)
        self.assertEqual(result['atf']['data'], firmware)
        for name in ('uboot', 'fdt-0'):
            self.assertEqual(result[name], images[name])
        self.assertEqual(before['/configurations/board-0'], after['/configurations/board-0'])
        self.assertEqual(before['/configurations/board-0/signature'],
                         after['/configurations/board-0/signature'])
        allowed = set(range(lock['atf_offset'], lock['atf_offset'] + lock['atf_source_bytes']))
        for key in ('atf_size_field', 'atf_crc32_field'):
            allowed.update(range(lock[key], lock[key] + 4))
        self.assertTrue(all(a == b or index in allowed
                            for index, (a, b) in enumerate(zip(stock, changed))))
        end = lock['atf_offset'] + lock['atf_source_bytes']
        self.assertEqual(changed[lock['atf_offset'] + len(firmware):end],
                         bytes(lock['atf_source_bytes'] - len(firmware)))
        self.assertEqual(len(changed), len(stock))

    def test_wrong_stock_or_candidate_refuses(self):
        stock, firmware, _, lock = fixture()
        for a, b in [(stock[:-1], firmware), (stock[:-1] + b'X', firmware),
                     (stock, firmware + b'X')]:
            with self.subTest(stock=len(a), candidate=len(b)), self.assertRaises(ValueError):
                diagnostic.patch_fit(a, b, lock)

    def test_slot_or_metadata_mismatch_refuses(self):
        stock, firmware, _, original = fixture()
        for key in ('atf_offset', 'atf_source_bytes', 'atf_size_field', 'atf_crc32_field'):
            lock = dict(original)
            lock[key] += 4
            with self.subTest(key=key), self.assertRaises(ValueError):
                diagnostic.patch_fit(stock, firmware, lock)

    def test_oversized_candidate_cannot_overwrite_following_payload(self):
        stock, _, _, lock = fixture()
        firmware = b'X' * (lock['atf_source_bytes'] + 1)
        lock['bl31_sha256'] = diagnostic.sha(firmware)
        with self.assertRaisesRegex(ValueError, 'slot'):
            diagnostic.patch_fit(stock, firmware, lock)


class BundleTests(unittest.TestCase):
    def setup_fixture(self, root):
        stock, firmware, elf, lock = fixture()
        profile = root / 'profile'
        profile.mkdir()
        patch = b'source-bound patch fixture\n'
        lock['patch_sha256'] = diagnostic.sha(patch)
        (profile / 'completion-errors.patch').write_bytes(patch)
        (profile / 'firmware.lock.json').write_text(json.dumps(lock) + '\n')
        paths = []
        for name, data in [('vendor.itb', stock), ('bl31.bin', firmware), ('bl31.elf', elf)]:
            path = root / name
            path.write_bytes(data)
            paths.append(path)
        return profile, paths

    def test_prepare_verify_rollback_and_two_pass_reconstruction(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            profile, paths = self.setup_fixture(root)
            with mock.patch.object(diagnostic, 'PROFILE', profile):
                first = diagnostic.prepare(*paths, root / 'first')
                self.assertEqual(diagnostic.verify(root / 'first'), first)
                second = diagnostic.prepare(*paths, root / 'second')
                self.assertEqual(first, second)
                for name in diagnostic.FILES:
                    self.assertEqual((root / 'first' / name).read_bytes(),
                                     (root / 'second' / name).read_bytes())
                self.assertFalse(first['production_admission'])
                self.assertFalse(first['hardware_acceptance'])
                self.assertEqual((root / 'first/stock-u-boot.itb').read_bytes(), paths[0].read_bytes())

    def test_invalid_source_refuses_before_output_creation(self):
        for change in ('stock', 'firmware', 'elf', 'symlink'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                profile, paths = self.setup_fixture(root)
                index = {'stock': 0, 'firmware': 1, 'elf': 2, 'symlink': 1}[change]
                if change == 'symlink':
                    alias = root / 'alias'
                    alias.symlink_to(paths[index])
                    paths[index] = alias
                else:
                    paths[index].write_bytes(paths[index].read_bytes() + b'changed')
                with mock.patch.object(diagnostic, 'PROFILE', profile), self.assertRaises(ValueError):
                    diagnostic.prepare(*paths, root / 'output')
                self.assertFalse((root / 'output').exists())

    def test_existing_output_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            profile, paths = self.setup_fixture(root)
            output = root / 'output'
            output.mkdir()
            (output / 'sentinel').write_text('keep')
            with mock.patch.object(diagnostic, 'PROFILE', profile), self.assertRaises(ValueError):
                diagnostic.prepare(*paths, output)
            self.assertEqual([p.name for p in output.iterdir()], ['sentinel'])

    def test_changed_bundle_refuses_even_if_file_inventory_hash_is_updated(self):
        for name in ('u-boot.itb', 'stock-u-boot.itb', 'bl31.bin', 'bl31.elf',
                     'completion-errors.patch', 'firmware.lock.json'):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                profile, paths = self.setup_fixture(root)
                with mock.patch.object(diagnostic, 'PROFILE', profile):
                    output = root / 'output'
                    diagnostic.prepare(*paths, output)
                    changed = (output / name).read_bytes() + b'changed'
                    (output / name).write_bytes(changed)
                    record = json.loads((output / 'diagnostic.json').read_bytes())
                    record['files'][name] = {'size': len(changed), 'sha256': diagnostic.sha(changed)}
                    (output / 'diagnostic.json').write_text(json.dumps(record))
                    with self.assertRaises(ValueError):
                        diagnostic.verify(output)

    def test_extra_file_symlink_or_admission_claim_refuses(self):
        for change in ('extra', 'symlink', 'admission'):
            with self.subTest(change=change), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                profile, paths = self.setup_fixture(root)
                with mock.patch.object(diagnostic, 'PROFILE', profile):
                    output = root / 'output'
                    diagnostic.prepare(*paths, output)
                    if change == 'extra':
                        (output / 'unexpected').touch()
                    elif change == 'symlink':
                        (output / 'bl31.bin').unlink()
                        (output / 'bl31.bin').symlink_to(paths[1])
                    else:
                        record = json.loads((output / 'diagnostic.json').read_bytes())
                        record['production_admission'] = True
                        (output / 'diagnostic.json').write_text(json.dumps(record))
                    with self.assertRaises(ValueError):
                        diagnostic.verify(output)


if __name__ == '__main__':
    unittest.main()
