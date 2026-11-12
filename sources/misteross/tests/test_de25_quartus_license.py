"""Explicit local licensing must stay private, stable and bound to execution.

These tests use marker files, not valid licenses or compiler qualification.
The installed Quartus license check and full fits remain separate evidence.
"""
import hashlib
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest import mock

from boards.de25nano.producers import build_menu_package as menu, build_zx81_package as zx81
from scripts.functional_execution import execution_digest


class QuartusLicenseTests(unittest.TestCase):
    def test_both_producers_snapshot_explicit_bytes_without_ambient_settings(self):
        for producer in (menu, zx81):
            with self.subTest(producer=producer.RECIPE), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                source = root / 'selected.dat'
                source.write_bytes(b'private license marker\n')
                home = root / 'home'
                home.mkdir()
                with mock.patch.dict(os.environ, {'LM_LICENSE_FILE': 'ambient-server',
                                                  'ALTERAD_LICENSE_FILE': 'other-server'}):
                    env = producer.execution_environment(home, {}, license_file=source)
                selected = Path(env['LM_LICENSE_FILE'])
                self.assertEqual(selected, home / 'quartus-license.dat')
                self.assertEqual(selected.read_bytes(), source.read_bytes())
                self.assertEqual(stat.S_IMODE(selected.stat().st_mode), 0o400)
                self.assertNotIn('ALTERAD_LICENSE_FILE', env)
                before = menu.license_execution_inputs(env)
                source.write_bytes(b'changed source during independent repeat\n')
                self.assertEqual(menu.license_execution_inputs(env), before)

    def test_execution_binds_bytes_without_private_path_or_contents(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / 'selected.dat'
            source.write_bytes(b'DO_NOT_PUBLISH_LICENSE_CONTENTS\n')
            inputs = []
            for name in ('first', 'independent'):
                home = root / name
                home.mkdir()
                env = menu.execution_environment(home, {}, license_file=source)
                normalized, license_input = menu.license_execution_inputs(env)
                inputs.append({'environment': normalized, 'license_file': license_input})
            self.assertEqual(execution_digest(inputs[0]), execution_digest(inputs[1]))
            self.assertEqual(inputs[0]['license_file'], {
                'bytes': source.stat().st_size,
                'sha256': hashlib.sha256(source.read_bytes()).hexdigest()})
            self.assertNotIn(str(root), json.dumps(inputs))
            self.assertNotIn('DO_NOT_PUBLISH_LICENSE_CONTENTS', json.dumps(inputs))
            selected = root / 'independent/quartus-license.dat'
            selected.chmod(0o600)
            selected.write_bytes(b'changed selected compiler input\n')
            normalized, license_input = menu.license_execution_inputs(env)
            changed = {'environment': normalized, 'license_file': license_input}
            self.assertNotEqual(execution_digest(inputs[1]), execution_digest(changed))

    def test_no_explicit_file_keeps_controlled_default_and_ignores_ambient_license(self):
        for producer in (menu, zx81):
            with self.subTest(producer=producer.RECIPE), tempfile.TemporaryDirectory() as temp:
                with mock.patch.dict(os.environ, {'LM_LICENSE_FILE': 'ambient-server'}):
                    env = producer.execution_environment(Path(temp), {})
                self.assertNotIn('LM_LICENSE_FILE', env)
                normalized, license_input = menu.license_execution_inputs(env)
                self.assertIsNone(license_input)
                self.assertEqual(normalized['HOME'], '<private-build-home>')

    def test_nonregular_empty_linked_and_oversize_inputs_refuse_before_snapshot(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            empty = root / 'empty'
            empty.touch()
            large = root / 'large'
            large.write_bytes(b'x' * (1024 * 1024 + 1))
            linked = root / 'linked'
            linked.symlink_to(large)
            fifo = root / 'fifo'
            os.mkfifo(fifo)
            for index, source in enumerate((root, empty, large, linked, fifo)):
                with self.subTest(source=source.name):
                    home = root / ('home-' + str(index))
                    home.mkdir()
                    with self.assertRaises((menu.BuildError, OSError)):
                        menu.execution_environment(home, {}, license_file=source)
                    self.assertFalse((home / 'quartus-license.dat').exists())

    def test_only_the_private_selected_file_is_accepted_by_execution_capture(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / 'selected.dat'
            source.write_bytes(b'license marker\n')
            env = menu.execution_environment(root, {}, license_file=source)
            with self.assertRaisesRegex(menu.BuildError, 'private snapshot'):
                menu.license_execution_inputs(dict(env, LM_LICENSE_FILE=str(source)))
            selected = Path(env['LM_LICENSE_FILE'])
            selected.unlink()
            selected.symlink_to(source)
            with self.assertRaises(OSError):
                menu.license_execution_inputs(env)


if __name__ == '__main__':
    unittest.main()
