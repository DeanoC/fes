"""Exercise both real DE25 record producers over their captured source bytes.

Temporary Git repositories and explicit tool markers test identity boundaries;
they provide no compiler, timing, package or hardware acceptance.
"""
from pathlib import Path
import importlib.util
import subprocess
import tempfile
import unittest

from boards.de25nano.producers import build_menu_package as menu, build_zx81_package as zx81, build_zx81_static_package as static
from boards.de25nano.package.export_core_package import build_identity, verify_record_source_at_revision
from scripts.source_closure import imports

ROOT = Path(__file__).resolve().parents[1]


class ProductionIdentityTests(unittest.TestCase):
    def repository(self, producer, directory):
        root = directory / 'sources/misteross'
        captured = (menu.scanout(ROOT).capture() if producer is menu else
                    zx81.computer(ROOT).capture_standard_board())
        files = {Path(name).relative_to('sources/misteross').as_posix(): data
                 for name, data in captured.items()}
        # Keep actual producer imports and fitted qualification inputs, rather
        # than stubbing the source-closure function under test.
        helpers = {producer.RECIPE, 'boards/de25nano/build.py', 'boards/de25nano/quartus.lock',
                   'boards/de25nano/producers/build_menu_package.py',
                   'boards/de25nano/producers/build_zx81_package.py',
                   'boards/de25nano/package/core_package.py',
                   'boards/de25nano/package/export_core_package.py'}
        for module in ('build_fes_pcxt_quartus', 'fes_build_common', 'functional_execution',
                       'source_closure', 'compiler_read_audit'):
            helpers |= imports(module, ROOT)
        helpers.add('boards/de25nano/producers/review_audio_clocks.py')
        helpers |= {path.relative_to(ROOT).as_posix()
                    for directory in ('producers', 'package')
                    for path in (ROOT / 'boards/de25nano' / directory).glob('*.py')}
        helpers.update(static.REVIEW_INPUTS)
        helpers.update(menu.REVIEW_INPUTS)
        helpers |= {'boards/de25nano/scanout/' + name for name in (
            'async_inputs.py', 'async_inputs.tcl', 'vendor_reference.py',
            'audio_reference.py', 'audio_clocks.tcl')}
        files.update({name: (ROOT / name).read_bytes() for name in helpers})
        for name, data in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        self.git(directory, 'init', '-q')
        self.commit(directory)
        return root, captured

    def git(self, directory, *args):
        return subprocess.check_output(['git', '-C', str(directory), *args], text=True).strip()

    def commit(self, directory):
        self.git(directory, 'add', '.')
        self.git(directory, '-c', 'user.name=Test', '-c', 'user.email=test@example.invalid',
                 'commit', '-qm', 'identity inputs')

    def record(self, producer, root, captured):
        revision = self.git(root, 'rev-parse', 'HEAD')
        return producer.create_build_record(root, 'https://github.com/DeanoC/fes', revision,
            {name: 'explicit identity fixture ' + name for name in ('quartus', 'qsys', 'ipgenerate', 'pfg')},
            {'toolchain': 'quartus', 'identity_fixture': True}, captured)

    def test_diagnostics_simulations_and_documentation_do_not_mint_core_identities(self):
        for producer in (menu, zx81):
            with self.subTest(recipe=producer.RECIPE), tempfile.TemporaryDirectory() as temp:
                directory = Path(temp)
                root, captured = self.repository(producer, directory)
                original = self.record(producer, root, captured)
                for name in ('boards/de25nano/link-proof/identity_diagnostic.py',
                             'boards/de25nano/scanout/test_identity.py',
                             'boards/de25nano/scanout/identity_tb.cpp',
                             'boards/de25nano/scanout/identity_harness.v',
                             'boards/de25nano/scanout/sim.py',
                             'scripts/sim_identity_diagnostic.py',
                             'cores/fes-menu/sim/identity.cpp',
                             'cores/fes-zx81/rtl/identity.md'):
                    path = root / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text('non-deployed identity test input\n')
                self.commit(directory)
                changed = self.record(producer, root, captured)
                self.assertEqual(build_identity(original), build_identity(changed))
                fields = verify_record_source_at_revision(root, changed)
                self.assertTrue(any(path.startswith('boards/de25nano/')
                                    for path in fields['source_roots']))
                self.assertNotIn('scripts', fields['source_roots'])

    def test_captured_rtl_changes_identity_and_stale_capture_is_refused(self):
        for producer in (menu, zx81):
            with self.subTest(recipe=producer.RECIPE), tempfile.TemporaryDirectory() as temp:
                directory = Path(temp)
                root, captured = self.repository(producer, directory)
                original = self.record(producer, root, captured)
                name = 'boards/de25nano/scanout/fes_de25_scanout.v'
                path = root / name
                path.write_bytes(path.read_bytes() + b'\n// Changed production input.\n')
                self.commit(directory)
                with self.assertRaisesRegex(producer.BuildError, 'differs from canonical closure'):
                    self.record(producer, root, captured)
                selected = dict(captured, **{'sources/misteross/' + name: path.read_bytes()})
                changed = self.record(producer, root, selected)
                self.assertNotEqual(build_identity(original), build_identity(changed))
                verify_record_source_at_revision(root, changed)

    def test_transitive_imported_producer_helper_changes_identity(self):
        for producer in (menu, zx81):
            with self.subTest(recipe=producer.RECIPE), tempfile.TemporaryDirectory() as temp:
                directory = Path(temp)
                root, captured = self.repository(producer, directory)
                original = self.record(producer, root, captured)
                path = root / 'scripts/source_repository.py'
                path.write_bytes(path.read_bytes() + b'\n# Changed transitive producer helper.\n')
                self.commit(directory)
                changed = self.record(producer, root, captured)
                self.assertNotEqual(build_identity(original), build_identity(changed))
                verify_record_source_at_revision(root, changed)

    def test_relocated_common_helper_changes_zx81_identity(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            root, captured = self.repository(zx81, directory)
            original = self.record(zx81, root, captured)
            self.assertIn(menu.RECIPE, verify_record_source_at_revision(root, original)['source_inputs'])
            helper = root / menu.RECIPE
            helper.write_bytes(helper.read_bytes() + b'\n# Changed authentication/timing helper.\n')
            self.commit(directory)
            changed = self.record(zx81, root, captured)
            self.assertNotEqual(build_identity(original), build_identity(changed))

    def test_static_producer_helper_changes_static_identity(self):
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            root, captured = self.repository(zx81, directory)
            def record():
                return static.create_build_record(root, 'https://github.com/DeanoC/fes',
                    self.git(root, 'rev-parse', 'HEAD'),
                    {name: 'explicit identity fixture ' + name for name in ('quartus', 'qsys', 'ipgenerate', 'pfg')},
                    {'toolchain': 'quartus', 'identity_fixture': True}, captured,
                    control_build_id='a' * 32, placement='X2 Y7 X26 Y12', routing='X1 Y6 X27 Y13')
            original = record()
            name = 'boards/de25nano/producers/build_zx81_static_package.py'
            self.assertIn(name, verify_record_source_at_revision(root, original)['source_inputs'])
            helper = root / name
            helper.write_bytes(helper.read_bytes() + b'\n# Changed static producer helper.\n')
            self.commit(directory)
            self.assertNotEqual(build_identity(original), build_identity(record()))

    def test_every_fitted_qualification_input_is_bound(self):
        for producer in (menu, zx81):
            with self.subTest(recipe=producer.RECIPE), tempfile.TemporaryDirectory() as temp:
                directory = Path(temp)
                root, captured = self.repository(producer, directory)
                original = self.record(producer, root, captured)
                fields = verify_record_source_at_revision(root, original)
                for name in producer.REVIEW_INPUTS:
                    self.assertIn(name, fields['source_inputs'])
                    path = root / name
                    path.write_bytes(path.read_bytes() + b'\n# Changed qualification implementation.\n')
                self.commit(directory)
                changed = self.record(producer, root, captured)
                self.assertNotEqual(build_identity(original), build_identity(changed))
                verify_record_source_at_revision(root, changed)

    def test_hashed_board_inputs_cannot_skip_the_de25_hardware_gate(self):
        path = ROOT.parents[1] / 'scripts/hil_plan.py'
        spec = importlib.util.spec_from_file_location('de25_identity_hil_plan', path)
        planner = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(planner)
        for producer in (menu, zx81):
            with self.subTest(recipe=producer.RECIPE), tempfile.TemporaryDirectory() as temp:
                root, captured = self.repository(producer, Path(temp))
                fields = verify_record_source_at_revision(root, self.record(producer, root, captured))
                for name in fields['source_inputs']:
                    if name.startswith('boards/de25nano/'):
                        with self.subTest(input=name):
                            row = planner.classify_path('sources/misteross/' + name)
                            self.assertEqual(row['class'], 'overlay')
                            self.assertTrue(row['component'].startswith('core:de25-'))


if __name__ == '__main__':
    unittest.main()
