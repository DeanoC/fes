#!/usr/bin/env python3
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('software', Path(__file__).parents[1] / 'kit-software.py')
software = importlib.util.module_from_spec(spec)
spec.loader.exec_module(software)


class SoftwareABI(unittest.TestCase):
    def test_target_symlinks(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'lib').mkdir()
            (root / 'lib/real').write_bytes(b'library')
            (root / 'lib/alias').symlink_to('/lib/real')
            self.assertEqual(software.resolve(root, '/lib/alias'), root / 'lib/real')
            (root / 'lib/escape').symlink_to('../../outside')
            with self.assertRaises(ValueError):
                software.resolve(root, '/lib/escape')
            (root / 'lib/loop').symlink_to('loop')
            with self.assertRaises(ValueError):
                software.resolve(root, '/lib/loop')

    def test_elf_architecture_search_paths_and_versioned_symbols(self):
        outputs = {'-h': 'Class: ELF32\nMachine: ARM\nData: little endian\nFlags: Version5 EABI',
                   '-d': ' (NEEDED) [libc.so.6]\n (SONAME) [libtest.so.1]',
                   '-l': '[Requesting program interpreter: /lib/ld-linux-armhf.so.3]',
                   '--dyn-syms': ' 1: 00000000 0 FUNC GLOBAL DEFAULT UND memcpy@GLIBC_2.4 (2)\n'
                                 ' 2: 00000001 0 FUNC GLOBAL DEFAULT 12 supplied@@ABI_1\n'
                                 ' 3: 00000000 0 NOTYPE WEAK DEFAULT UND optional'}
        with patch.object(software, 'run', side_effect=lambda _, flag, *args: outputs[flag]):
            value = software.elf(Path('binary'))
            self.assertEqual(value['required'], ['memcpy@GLIBC_2.4'])
            self.assertEqual(value['defined'], ['supplied', 'supplied@ABI_1'])
            self.assertEqual(value['needed'], ['libc.so.6'])
            outputs['-d'] += '\n (RUNPATH) []\n (RPATH) []'
            self.assertEqual(software.elf(Path('binary')), value)
            outputs['-d'] += '\n (RUNPATH) [/build/libraries]'
            with self.assertRaises(ValueError):
                software.elf(Path('binary'))
            outputs['-d'] = ''
            outputs['-h'] = 'Class: ELF64\nMachine: X86-64'
            with self.assertRaises(ValueError):
                software.elf(Path('binary'))

    def test_update_rejects_changed_base_before_pack(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'etc/fes').mkdir(parents=True)
            (root / 'etc/fes/software-base.json').write_text('{"base_contract":"old"}')
            payload = root / 'payload'
            (payload / 'bin').mkdir(parents=True)
            for name in software.BINARIES:
                (payload / 'bin' / name).write_bytes(b'new executable')
            output = root / 'update.tar'
            output.write_bytes(b'previous archive')
            with patch.object(software, 'run', return_value='{}') as run, \
                 patch.object(software, 'base_contract', return_value={'base_contract': 'changed'}):
                with self.assertRaisesRegex(ValueError, 'differs from recorded'):
                    software.pack_update(root, payload, output, 'new', root, 'a' * 40)
                self.assertEqual(output.read_bytes(), b'previous archive')
                self.assertEqual(run.call_count, 1)  # verify only; no pack

    def test_dependency_closure_and_contract_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'lib').mkdir()
            lib = root / 'lib/libc.so.6'
            lib.write_bytes(b'base code one')
            binary = root / 'program'
            binary.write_bytes(b'program')
            table = {
                lib: {'needed': [], 'soname': ['libc.so.6'], 'interpreter': [],
                      'defined': ['memcpy@GLIBC_2.4'], 'required': []},
                binary: {'needed': ['libc.so.6'], 'soname': [], 'interpreter': [],
                         'defined': [], 'required': ['memcpy@GLIBC_2.4']},
            }
            with patch.object(software, 'elf', side_effect=lambda p: table[p]):
                first = software.base_contract(root, [binary])
                lib.write_bytes(b'new implementation, same ABI')
                binary.write_bytes(b'another application')
                self.assertEqual(first, software.base_contract(root, [binary]))
                table[lib]['defined'].append('new_symbol@ABI_2')
                self.assertNotEqual(first['base_contract'], software.base_contract(root, [binary])['base_contract'])
                table[binary]['required'].append('missing@ABI_3')
                with self.assertRaises(ValueError):
                    software.base_contract(root, [binary])
                table[binary]['needed'] = ['missing.so.1']
                with self.assertRaises(ValueError):
                    software.base_contract(root, [binary])


if __name__ == '__main__':
    unittest.main()
