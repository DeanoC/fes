import importlib.util
from pathlib import Path
import tempfile
import unittest
import os
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
        self.core = self.sources['misteross'] / 'cores.lock'
        self.core.write_text('[core.megadrive]\nrepo="https://example.org/core"\ncommit="abc"\nrbf_path="releases/core.rbf"\nrbf_sha256="def"\nrbf_size=123\nproject="MegaDrive.qpf"\n')
        self.core.write_text(self.core.read_text() + self.core.read_text().replace('[core.megadrive]', '[core.snes]').replace('MegaDrive.qpf', 'SNES.qpf'))
        self.fog = self.sources['FogCast'] / 'build/native-runtime.inputs.lock.toml'
        self.fog.parent.mkdir()
        self.fog.write_text('[megadrive_rbf]\nrepository="https://example.org/core"\ncommit="abc"\npath="releases/core.rbf"\nsha256="def"\nsize=123\n')
        self.calls = []
        def run(packages, command, source):
            self.calls.append((command, source))
            if command == 'report':
                return REPORT.replace(b'megadrive_mister', b'snes_mister').replace(b'MegaDrive.qpf', b'SNES.qpf') if 'snes' in source else REPORT
            return b'generated\n'
        self.mock = patch.object(self.module, '_run', side_effect=run)
        self.mock.start()
        self.addCleanup(self.mock.stop)

    def test_selected_sources_and_validation_coverage(self):
        self.assertEqual(self.module.check(self.root, self.sources), {'generated_files': 7, 'source_pin_copies': 3})
        self.assertEqual({source for command, source in self.calls if command == 'validate'}, {
            'packages/platform/de10_nano.yaml', 'packages/system/megadrive.yaml',
            'packages/system/pong.yaml', 'packages/system/snes.yaml',
            'packages/source/megadrive_mister.yaml', 'packages/source/snes_mister.yaml'})

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
