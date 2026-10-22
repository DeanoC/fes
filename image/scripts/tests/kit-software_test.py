#!/usr/bin/env python3
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
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

    def test_menu_requirements_come_from_the_sealed_menu_package(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            menu(root, [('fes.video.fixed-720p60', 1, 0, True), ('fes.video.menu-display', 1, 2, False)])
            self.assertEqual(software.menu_requirements(root),
                             {'menu.fes.video.fixed-720p60': '1.0', 'menu.fes.video.menu-display': '1.2'})
            for bad in ([], [('fes.video.menu-display', 1, 0, True), ('fes.video.menu-display', 1, 0, True)],
                        [('', 1, 0, True)], [('fes.video.menu-display', -1, 0, True)],
                        [('fes.video.menu display', 1, 0, True)]):
                with self.subTest(bad=bad):
                    menu(root, bad)
                    with self.assertRaises(ValueError):
                        software.menu_requirements(root)
            (root / software.MENU_SELECTION).write_text("package_id = '../escape'\n")
            with self.assertRaisesRegex(ValueError, 'invalid menu package selection'):
                software.menu_requirements(root)

    def test_runtime_menu_interfaces_read_from_string_dump(self):
        dump = ("\nString dump of section '.fes_menu_interfaces':\n"
                '  [     0]  fes.video.fixed-720p60=1.0\n  [    1b]  fes.video.menu-display=1.3\n\n')
        with patch.object(software, 'run', return_value=dump) as run:
            self.assertEqual(software.runtime_menu_interfaces(Path('runtime')),
                             {'menu.fes.video.fixed-720p60': '1.0', 'menu.fes.video.menu-display': '1.3'})
            run.assert_called_once_with('readelf', '--string-dump=.fes_menu_interfaces', 'runtime')
        for bad in ('', '  [     0]  fes.video.menu-display\n', '  [     0]  fes.video.menu-display=1\n',
                    '  [     0]  =1.0\n', '  [     0]  a=1.0\n  [     4]  a=1.0\n'):
            with self.subTest(dump=bad), patch.object(software, 'run', return_value=bad):
                with self.assertRaises(ValueError):
                    software.runtime_menu_interfaces(Path('runtime'))

    @unittest.skipUnless(shutil.which('cc') and shutil.which('readelf'), 'needs a host C compiler and readelf')
    def test_runtime_menu_interfaces_from_a_stripped_elf_section(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'runtime.c').write_text(
                'const char records[] __attribute__((section(".fes_menu_interfaces"), used)) =\n'
                '    "fes.video.fixed-720p60=1.0\\0fes.video.menu-display=1.0\\0";\n'
                'int main(void) { return 0; }\n')
            subprocess.run(['cc', '-O2', str(root / 'runtime.c'), '-o', str(root / 'runtime')], check=True)
            if shutil.which('strip'):
                subprocess.run(['strip', '--strip-unneeded', str(root / 'runtime')], check=True)
            self.assertEqual(software.runtime_menu_interfaces(root / 'runtime'),
                             {'menu.fes.video.fixed-720p60': '1.0', 'menu.fes.video.menu-display': '1.0'})
            (root / 'plain.c').write_text('int main(void) { return 0; }\n')
            subprocess.run(['cc', str(root / 'plain.c'), '-o', str(root / 'plain')], check=True)
            with self.assertRaisesRegex(ValueError, 'declares no menu interfaces'):
                software.runtime_menu_interfaces(root / 'plain')

    def test_unmet_requires_same_major_and_no_older_minor(self):
        provides = {'menu.a': '1.2', 'menu.b': '2.0', 'menu.c': '1.10'}
        self.assertEqual(software.unmet(provides, {'menu.a': '1.0', 'menu.b': '2.0', 'menu.c': '1.9'}), [])
        self.assertEqual(software.unmet(provides, {'menu.a': '1.3', 'menu.b': '1.0', 'menu.d': '1.0'}),
                         ['menu.a 1.3', 'menu.b 1.0', 'menu.d 1.0'])

    def test_image_build_refuses_a_factory_runtime_that_rejects_its_menu(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'usr/sbin').mkdir(parents=True)
            for name in software.BINARIES:
                (root / 'usr/sbin' / name).write_bytes(b'executable')
            menu(root, [('fes.video.menu-display', 1, 0, True), ('fes.video.menu-blitter', 1, 0, True)])
            contract = {'board': 'de10-nano', 'architecture': 'armv7', 'base_contract': 'base',
                        'interfaces': dict(software.INTERFACES)}
            dump = '  [     0]  fes.video.menu-display=1.0\n'
            with patch.object(software, 'base_contract', return_value=contract), \
                 patch.object(software, 'run', return_value=dump):
                with self.assertRaisesRegex(ValueError, 'menu.fes.video.menu-blitter 1.0'):
                    software.assemble(root, root, 'a' * 40, root)
            self.assertFalse((root / 'etc/fes/software-base.json').exists())
            self.assertFalse((root / 'usr/share/fes/software').exists())

    def test_update_lists_runtime_menu_interfaces_and_covers_base_menu(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'etc/fes').mkdir(parents=True)
            contract = {'board': 'de10-nano', 'architecture': 'armv7', 'base_contract': 'base',
                        'interfaces': dict(software.INTERFACES)}
            (root / 'etc/fes/software-base.json').write_text(
                json.dumps({**contract, 'requires': {'menu.fes.video.menu-display': '1.0'}}))
            payload = root / 'payload'
            (payload / 'bin').mkdir(parents=True)
            for name in software.BINARIES:
                (payload / 'bin' / name).write_bytes(b'new executable')
            (root / 'bin').mkdir()
            (root / 'bin/kit-software.revision').write_text('b' * 40 + '\n')
            packed = []

            def run(*args, dump='  [     0]  fes.video.menu-display=1.1\n  [    1b]  fes.video.menu-blitter=1.0\n'):
                if args[0] == 'readelf':
                    return dump
                if args[1] == 'pack':
                    packed.append(json.loads(Path(args[args.index('--manifest') + 1]).read_text()))
                return '{}'
            with patch.object(software, 'run', side_effect=run), \
                 patch.object(software, 'base_contract', return_value=contract):
                software.pack_update(root, payload, root / 'update.tar', 'new', root, 'a' * 40)
            self.assertEqual(packed[0]['interfaces'], {**software.INTERFACES, 'menu.fes.video.menu-display': '1.1',
                                                       'menu.fes.video.menu-blitter': '1.0'})
            self.assertNotIn('requires', packed[0])
            with patch.object(software, 'run', side_effect=lambda *args: run(*args, dump='  [     0]  fes.video.fixed-720p60=1.0\n')), \
                 patch.object(software, 'base_contract', return_value=contract):
                with self.assertRaisesRegex(ValueError, 'menu.fes.video.menu-display 1.0'):
                    software.pack_update(root, payload, root / 'refused.tar', 'new', root, 'a' * 40)
            self.assertEqual(len(packed), 1)

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


def menu(root, interfaces):
    """Write a sealed menu selection and its package manifest under root."""
    package = 'd' * 64
    selection = root / software.MENU_SELECTION
    selection.parent.mkdir(parents=True, exist_ok=True)
    selection.write_text(f"format = 2\nkind = 'core-package'\ncore_id = 'fes.menu'\npackage_id = '{package}'\n")
    manifest = root / 'usr/share/mister-runtime/core-packages' / package / 'manifest.toml'
    manifest.parent.mkdir(parents=True, exist_ok=True)
    lines = ['format = 2'] + ([] if interfaces else ['interfaces = []'])
    for name, major, minor, required in interfaces:
        lines += ['', '[[interfaces]]', f'id = "{name}"', f'major = {major}', f'minor = {minor}',
                  f'required = {str(required).lower()}']
    manifest.write_text('\n'.join(lines) + '\n')


if __name__ == '__main__':
    unittest.main()
