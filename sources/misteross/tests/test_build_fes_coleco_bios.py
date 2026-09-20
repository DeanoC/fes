"""Private BIOS checks use synthetic bytes, never a console ROM."""
import hashlib
from contextlib import ExitStack
from types import SimpleNamespace
import json
import io
import os
from contextlib import redirect_stderr, redirect_stdout
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from scripts import build_fes_coleco_oss as producer
from scripts.export_core_package import build_identity
ROOT = Path(__file__).resolve().parents[1]

class PrivateBiosTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temporary.name) / 'source'
        cls.root.mkdir()
        for name in ('scripts', 'cores', 'boards', 'toolchains'):
            shutil.copytree(ROOT / name, cls.root / name, ignore=shutil.ignore_patterns('__pycache__'))
        (cls.root / '.gitignore').write_text('build/\n__pycache__/\n')
        for args in [('init', '-q'), ('remote', 'add', 'origin', 'https://example.invalid/source.git'), ('add', '.'), ('-c', 'user.name=Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'fixture')]:
            subprocess.run(['git', '-C', str(cls.root), *args], check=True)
        cls.revision = subprocess.check_output(['git', '-C', str(cls.root), 'rev-parse', 'HEAD'], text=True).strip()
    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()
    def setUp(self):
        self.output = producer._prepare_output(self.root)
        self.source = Path(self.temporary.name) / 'private-input.bin'
        self.data = bytes(range(256)) * 32
        self.source.write_bytes(self.data)
    def record(self, snapshot=None, version=2):
        return producer.create_build_record(self.root, 'https://example.invalid/source.git', self.revision,
            {'yosys': 'fixture'}, identity_version=version, execution={'gpu_device': 0}, bios_snapshot=snapshot)
    def test_snapshot_conversion_identity_and_private_manifest(self):
        snapshot = producer._snapshot_bios(self.source, self.output)
        parameters = snapshot.verify(self.output)
        self.assertEqual(parameters['bios_sha256'], hashlib.sha256(self.data).hexdigest())
        self.assertEqual(parameters['bios_size'], 8192)
        self.assertEqual((self.output / 'private-bios.hex').read_text(), ''.join(f'{b:02x}\n' for b in self.data))
        for name in ('private-bios.bin', 'private-bios.hex'):
            self.assertEqual((self.output / name).stat().st_mode & 0o777, 0o400)
        private, default = self.record(snapshot), self.record()
        self.assertNotEqual(build_identity(private), build_identity(default))
        self.assertNotIn('bios_mode', json.loads(default)['parameters'])
        self.assertNotIn('bios_mode', json.loads(self.record(version=1))['parameters'])
        self.assertEqual(json.loads(private)['parameters']['bios_mode'], 'private-8192')
        self.assertNotIn(str(self.source).encode(), private)
        self.source.write_bytes(bytes([1]) + self.data[1:])
        changed = producer._snapshot_bios(self.source, self.output)
        self.assertNotEqual(build_identity(private), build_identity(self.record(changed)))
        manifest = producer._manifest(private, {'build_id': build_identity(private),
            'rbf': {'size': 1, 'sha256': 'a' * 64}}, 'https://example.invalid/source.git', self.revision, {'yosys': 'fixture'})
        self.assertIn(b'id = "fes.coleco.private-bios"', manifest)
    def test_invalid_size_and_symlink_rejected(self):
        for size in (0, 8191, 8193):
            self.source.write_bytes(b'x' * size)
            with self.assertRaisesRegex(producer.BuildError, '8192'):
                producer._snapshot_bios(self.source, self.output)
        fifo = Path(self.temporary.name) / 'fifo.bin'
        os.mkfifo(fifo)
        try:
            with self.assertRaisesRegex(producer.BuildError, 'regular'):
                producer._snapshot_bios(fifo, self.output)
        finally:
            fifo.unlink()
        link = Path(self.temporary.name) / 'link.bin'
        link.symlink_to(self.source)
        try:
            with self.assertRaisesRegex(producer.BuildError, 'symlink'):
                producer._snapshot_bios(link, self.output)
        finally:
            link.unlink()
    def test_mutated_or_replaced_snapshot_rejected(self):
        for name in ('private-bios.bin', 'private-bios.hex'):
            snapshot = producer._snapshot_bios(self.source, self.output)
            path = self.output / name
            path.chmod(0o600)
            path.write_bytes(b'changed')
            with self.assertRaisesRegex(producer.BuildError, 'snapshot'):
                self.record(snapshot)
            path.unlink()
            path.symlink_to(self.source)
            with self.assertRaisesRegex(producer.BuildError, 'symlink'):
                snapshot.verify(self.output)
            path.unlink()
    def test_private_commands_are_explicit_and_legacy_rejected(self):
        tools = {'yosys': Path('/tool/yosys'), 'nextpnr-mistral': Path('/tool/nextpnr')}
        default, _ = producer.build_commands(self.root, self.output, 'a' * 32, tools)
        private, _ = producer.build_commands(self.root, self.output, 'a' * 32, tools, private_bios=True)
        self.assertNotIn('FES_COLECO_PRIVATE_BIOS', default[2])
        self.assertIn('-DFES_COLECO_PRIVATE_BIOS=1', private[2])
        with self.assertRaisesRegex(producer.BuildError, 'identity.*2'):
            producer.build(self.root, bios=self.source, identity_version=1)
        with patch.object(producer, 'build') as build, redirect_stderr(io.StringIO()):
            self.assertEqual(producer.main(['--bios', str(self.source), '--print-commands']), 1)
            build.assert_not_called()
        with patch.object(producer, 'build', return_value=Path('/private/package')) as build, redirect_stdout(io.StringIO()):
            self.assertEqual(producer.main(['--bios', str(self.source)]), 0)
            self.assertEqual(build.call_args.kwargs['bios'], self.source)
            self.assertEqual(build.call_args.kwargs['identity_version'], 2)
    def test_private_store_separate_and_restrictive(self):
        store = producer._package_store(self.root, None, private_bios=True)
        self.assertEqual(store, self.root / 'build/private-packages')
        self.assertEqual(store.stat().st_mode & 0o777, 0o700)
        with self.assertRaisesRegex(producer.BuildError, 'package store'):
            producer._package_store(self.root, self.root / 'build/packages', private_bios=True)

    def test_controlled_build_exports_private_identity_and_checks_both_mutation_boundaries(self):
        payload = b'synthetic-rbf-for-unit-test'
        tools = {name: SimpleNamespace(path=Path('/tool') / name, identity='fixture')
                 for name in ('yosys', 'nextpnr-mistral')}
        for mutation in (None, 'synthesis', 'route'):
            with self.subTest(mutation=mutation), ExitStack() as stack:
                def mutate():
                    path = self.output / 'private-bios.hex'
                    path.chmod(0o600)
                    path.write_bytes(b'x' * (8192 * 3))
                    path.chmod(0o400)
                def synth(command, root, log, **kwargs):
                    self.assertIn('-DFES_COLECO_PRIVATE_BIOS=1', command[2])
                    self.assertEqual(kwargs['audit_source_root'], self.root)
                    self.assertEqual(kwargs['env']['LANG'], 'C')
                    (self.output / 'synth.json').write_text('{}')
                    if mutation == 'synthesis': mutate()
                def route(**kwargs):
                    self.assertEqual(kwargs['audit_source_root'], self.root)
                    (self.output / 'core.rbf').write_bytes(payload)
                    if mutation == 'route': mutate()
                    return SimpleNamespace(seed=4, weight=300)
                stack.enter_context(patch.object(producer, '_authenticate_coleco_tools', return_value=tools))
                stack.enter_context(patch.object(producer, 'execution_inputs', return_value={'gpu_device': 0}))
                stack.enter_context(patch.object(producer, '_run_tool', side_effect=synth))
                route_mock = stack.enter_context(patch.object(producer, 'route_after_synth', side_effect=route))
                stack.enter_context(patch.object(producer, 'validate_build_evidence', side_effect=lambda *a: {
                    'route': {}, 'rbf': {'size': len(payload), 'sha256': hashlib.sha256(payload).hexdigest()}}))
                export = stack.enter_context(patch.object(producer, 'export_package', wraps=producer.export_package))
                if mutation:
                    with self.assertRaisesRegex(producer.BuildError, 'snapshot'):
                        producer.build(self.root, bios=self.source, identity_version=2)
                    export.assert_not_called()
                    self.assertFalse((self.output / 'core.rbf').exists())
                    if mutation == 'synthesis': route_mock.assert_not_called()
                else:
                    package = producer.build(self.root, bios=self.source, identity_version=2)
                    self.assertEqual(package.parent, self.root / 'build/private-packages')
                    self.assertIn(b'id = "fes.coleco.private-bios"', (package / 'manifest.toml').read_bytes())
                    self.assertEqual((package / 'core.rbf').read_bytes(), payload)
                    record = json.loads(package.with_suffix('.build-inputs.json').read_bytes())
                    self.assertEqual(record['parameters']['bios_sha256'], hashlib.sha256(self.data).hexdigest())
                    self.assertEqual(export.call_count, 1)
