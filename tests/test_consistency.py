import importlib.util
from pathlib import Path
import tempfile
import unittest
import os
import shutil
import subprocess
import sys
from unittest.mock import patch

SCRIPT = Path(__file__).resolve().parents[1] / 'scripts/consistency.py'
sys.path.insert(0, str(SCRIPT.parent))
REPORT = b'core_source megadrive_mister\nrepository https://example.org/core\ncommit abc\nrbf_path releases/core.rbf\nrbf_sha256 def\nrbf_size 123\nproject MegaDrive.qpf\n'


class ConsistencyTest(unittest.TestCase):
    def setUp(self):
        self.assertTrue(SCRIPT.exists(), 'consistency checker is not implemented')
        spec = importlib.util.spec_from_file_location('consistency', SCRIPT)
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.sources = {name: self.root / name for name in ('FogCast', 'libmister-runtime', 'misteross', 'mister-packages')}
        for path in self.sources.values():
            path.mkdir()
        for _, _, component, destination in self.module.GENERATED:
            path = self.sources[component] / destination
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b'generated\n')
        for source, component, destination in self.module.COPIED_TREES:
            canonical = self.sources['mister-packages'] / source
            if not canonical.exists():
                canonical.mkdir(parents=True)
                (canonical / 'fixture').write_bytes(f'{source}\n'.encode())
        for source, component, destination in self.module.COPIED_FILES:
            canonical = self.sources['mister-packages'] / source
            if not canonical.exists():
                canonical.parent.mkdir(parents=True, exist_ok=True)
                canonical.write_bytes(f'{source}\n'.encode())
            copied = self.sources[component] / destination
            copied.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(canonical, copied)
        # Populate canonical single-file fixtures before copying a tree that
        # also contains those files (persistence is shared both ways).
        for source, component, destination in self.module.COPIED_TREES:
            shutil.copytree(self.sources['mister-packages'] / source,
                            self.sources[component] / destination,
                            dirs_exist_ok=True)
        for owner, source, component, destination in self.module.COMPONENT_FIXTURES:
            canonical = self.sources[owner] / source
            canonical.parent.mkdir(parents=True, exist_ok=True)
            canonical.write_bytes(f'{source}\n'.encode())
            copied = self.sources[component] / destination
            copied.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(canonical, copied)
        self.core = self.sources['misteross'] / 'cores.lock'
        self.core.write_text('[core.megadrive]\nrepo="https://example.org/core"\ncommit="abc"\nrbf_path="releases/core.rbf"\nrbf_sha256="def"\nrbf_size=123\nproject="MegaDrive.qpf"\n')
        self.core.write_text(self.core.read_text() + self.core.read_text().replace('[core.megadrive]', '[core.snes]').replace('MegaDrive.qpf', 'SNES.qpf') + self.core.read_text().replace('[core.megadrive]', '[core.nes]').replace('MegaDrive.qpf', 'NES.qpf'))
        self.fog = self.root / 'image/build/native-inputs.toml'
        self.fog.parent.mkdir(parents=True)
        self.fog.write_text('[megadrive_rbf]\nrepository="https://example.org/core"\ncommit="abc"\npath="releases/core.rbf"\nsha256="def"\nsize=123\n')
        self.calls = []
        def run(packages, command, source):
            self.calls.append((command, source))
            if command == 'report':
                for core, project in (('nes', 'NES.qpf'), ('snes', 'SNES.qpf')):
                    if f'/{core}_mister.yaml' in source:
                        return REPORT.replace(b'megadrive_mister', f'{core}_mister'.encode()).replace(b'MegaDrive.qpf', project.encode())
                return REPORT
            return b'generated\n'
        self.mock = patch.object(self.module, '_run', side_effect=run)
        self.mock.start()
        self.addCleanup(self.mock.stop)

    def test_fogcast_core_bundle_fixture_uses_public_package_path(self):
        self.assertIn(
            ('testdata/core-bundle-v2', 'FogCast', 'corepackage/testdata/core-bundle-v2'),
            self.module.COPIED_TREES)
        self.assertNotIn(
            ('testdata/core-bundle-v2', 'FogCast', 'internal/corepackage/testdata/core-bundle-v2'),
            self.module.COPIED_TREES)

    def test_selected_sources_and_validation_coverage(self):
        self.assertEqual(self.module.check(self.root, self.sources), {
            'generated_files': 20, 'source_pin_copies': 4, 'fixture_copies': 20})
        self.assertEqual({source for command, source in self.calls if command == 'validate'}, {
            'packages/platform/de10_nano.yaml', 'packages/system/megadrive.yaml',
            'packages/system/pong.yaml', 'packages/system/snes.yaml', 'packages/system/nes.yaml',
            'packages/abi/fes_simple_game.yaml', 'packages/abi/fes_simple_computer.yaml',
            'packages/abi/fes_application.yaml',
            'packages/programming/de10_nano.yaml',
            'packages/source/megadrive_mister.yaml', 'packages/source/snes_mister.yaml', 'packages/source/nes_mister.yaml'})

    def test_shared_fixture_copy_drift(self):
        fixture_copies = [(component, destination, None)
                          for _, component, destination in self.module.COPIED_TREES]
        fixture_copies += [(component, destination, destination)
                           for _, component, destination in self.module.COPIED_FILES]
        fixture_copies += [(component, destination, destination)
                           for _, _, component, destination in self.module.COMPONENT_FIXTURES]
        for component, destination, file_destination in fixture_copies:
            with self.subTest(destination=destination):
                copied = (self.sources[component] / file_destination if file_destination else
                          next(path for path in (self.sources[component] / destination).rglob('*')
                               if path.is_file()))
                original = copied.read_bytes()
                copied.write_bytes(original + b'drift')
                with self.assertRaisesRegex(ValueError, 'fixture.*differs'):
                    self.module.check(self.root, self.sources)
                copied.write_bytes(original)

    def test_missing_shared_fixture_tree_roots_are_rejected(self):
        source, component, destination = self.module.COPIED_TREES[0]
        for fixture_root in (
            self.sources['mister-packages'] / source,
            self.sources[component] / destination,
        ):
            with self.subTest(fixture_root=fixture_root):
                shutil.rmtree(fixture_root)
                with self.assertRaisesRegex(ValueError, 'fixture.*must be a directory'):
                    self.module.check(self.root, self.sources)
                fixture_root.mkdir(parents=True)
                (fixture_root / 'fixture').write_bytes(f'{source}\n'.encode())

    def test_nondirectory_shared_fixture_tree_roots_are_rejected(self):
        source, component, destination = self.module.COPIED_TREES[0]
        for fixture_root in (
            self.sources['mister-packages'] / source,
            self.sources[component] / destination,
        ):
            with self.subTest(fixture_root=fixture_root):
                shutil.rmtree(fixture_root)
                fixture_root.write_bytes(b'not a fixture tree\n')
                with self.assertRaisesRegex(ValueError, 'fixture.*must be a directory'):
                    self.module.check(self.root, self.sources)
                fixture_root.unlink()
                fixture_root.mkdir(parents=True)
                (fixture_root / 'fixture').write_bytes(f'{source}\n'.encode())

    def test_generated_consumer_drift(self):
        for _, _, component, destination in self.module.GENERATED:
            with self.subTest(destination=destination):
                path = self.sources[component] / destination
                path.write_bytes(b'drift\n')
                with self.assertRaisesRegex(ValueError, 'generated.*differs'):
                    self.module.check(self.root, self.sources)
                path.write_bytes(b'generated\n')

    def test_copied_pin_drift(self):
        for path in (self.core, self.fog):
            with self.subTest(path=path):
                original = path.read_text()
                path.write_text(original.replace('commit="abc"', 'commit="changed"'))
                with self.assertRaisesRegex(ValueError, 'commit.*differs'):
                    self.module.check(self.root, self.sources)
                path.write_text(original)

    def test_snes_copied_pin_drift(self):
        original = self.core.read_text()
        for field, value in (('commit', 'abc'), ('rbf_sha256', 'def'), ('project', 'SNES.qpf')):
            with self.subTest(field=field):
                md, snes = original.split('[core.snes]')
                self.core.write_text(md + '[core.snes]' + snes.replace(f'{field}="{value}"', f'{field}="changed"'))
                with self.assertRaisesRegex(ValueError, rf'core.snes.{field} differs'):
                    self.module.check(self.root, self.sources)
        self.core.write_text(original)

    def test_yaml_validation_failure_propagates(self):
        import subprocess
        with patch.object(self.module, '_run', side_effect=subprocess.CalledProcessError(1, 'validate')):
            with self.assertRaises(subprocess.CalledProcessError):
                self.module.check(self.root, self.sources)

    def test_emitter_ignores_ambient_go_overrides(self):
        with patch.dict(os.environ, {'GOOS': 'windows', 'GOARCH': 'arm',
                                     'GOFLAGS': '-invalid', 'GOWORK': '/wrong'}):
            with patch.object(self.module.subprocess, 'check_output', return_value=b'ok') as call:
                # Call the real subprocess boundary, not setUp's emitter fixture.
                self.mock.stop()
                self.module._run(self.sources['mister-packages'], 'validate', 'fixture')
                env = call.call_args.kwargs.get('env', {})
                self.assertEqual(env.get('GOFLAGS'), '')
                self.assertEqual(env.get('GOWORK'), 'off')
                self.assertNotIn('GOOS', env)
                self.assertNotIn('GOARCH', env)

    def test_bootable_media_docs_and_agent_entrypoints_are_linked(self):
        required = {
            'README.md': 'docs/bootable-media.md',
            'AGENTS.md': 'make verify-media',
            'docs/README.md': 'bootable-media.md',
            'docs/getting-started.md': 'make media',
            'docs/development.md': 'media/current/fes.img',
        }
        repository = Path(__file__).resolve().parents[1]
        for name, needle in required.items():
            self.assertIn(needle, (repository / name).read_text(), name)

    def test_bootable_media_operator_guide_covers_rollback_config_and_installed_hashes(self):
        guide = (Path(__file__).resolve().parents[1] / 'docs/bootable-media.md').read_text()
        required = (
            'make rollback-media GENERATION=', 'make verify-media', 'exclusive media lease',
            'generations/<image-sha256>/<evidence-sha256>', 'two independent',
            'rootfs.sha256', 'kernel.sha256', 'splash.sha256', 'idle.sha256',
            '/media/fat/fogcast/agent.toml',
            'installed rootfs, agent, runtime, kernel, splash, idle artifact',
            'core-packages', 'fes.pong', 'fes.zx81', 'fes.coleco',
        )
        for needle in required:
            self.assertIn(needle, guide, needle)
        for obsolete in ('rootfs_sha256', 'kernel_sha256', 'idle_sha256', 'mv -Tf', 'previous_target='):
            self.assertNotIn(obsolete, guide)

    def test_bootable_media_rollback_uses_leased_cli_entrypoint(self):
        repository = Path(__file__).resolve().parents[1]
        result = subprocess.run(['make', '-n', 'rollback-media', 'GENERATION=' + 'a' * 64 + '/' + 'b' * 64],
                                cwd=repository, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('scripts/media.py rollback', result.stdout)
        self.assertIn('--generation "' + 'a' * 64 + '/' + 'b' * 64 + '"', result.stdout)
        self.assertIn('make rollback-media', (repository / 'AGENTS.md').read_text())
        result = subprocess.run(['make', '-n', 'rollback-media', 'AGENT_CONFIG=/tmp/config'],
                                cwd=repository, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
