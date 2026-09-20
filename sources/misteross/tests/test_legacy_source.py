"""Real Git/format-1 export checks; no Quartus, synthesis or kit execution."""
import hashlib
import importlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import tomllib
import unittest
from unittest.mock import patch

from scripts import legacy_source, export_core_package as exporter
from scripts.core_package import encode_manifest, read_package

ROOT = Path(__file__).resolve().parents[1]
PRODUCERS = [importlib.import_module('scripts.build_fes_' + name)
             for name in ('demo', 'coleco', 'sms', 'sg1000', 'zx81')]


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()


class LegacySourceTests(unittest.TestCase):
    def fixture(self, producer, nested):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        repo = Path(temporary.name)
        module = repo / 'sources/misteross' if nested else repo
        module.mkdir(parents=True, exist_ok=True)
        git(repo, 'init', '-q')
        git(repo, 'config', 'user.name', 'Fixture')
        git(repo, 'config', 'user.email', 'fixture@example.invalid')
        git(repo, 'remote', 'add', 'origin', 'https://example.invalid/fes.git')
        (repo / '.gitignore').write_text('build/\n')
        for name in producer.PINNED_INPUTS:
            target = module / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes((ROOT / name).read_bytes())
        git(repo, 'add', '.')
        git(repo, 'commit', '-qm', 'source')
        return repo, module

    def require(self, producer, module):
        if producer.__name__.endswith('_demo'):
            return producer.require_clean_source(module, producer.PINNED_INPUTS)
        return producer.require_clean_source(module)

    def export_inputs(self, producer, repo, module):
        origin, revision = self.require(producer, module)
        record = producer.create_build_record(module, origin, revision, {'compiler': 'fixture'})
        fields = json.loads(record)
        output = module / 'build/fixture'
        output.mkdir(parents=True)
        payload = b'synthetic non-hardware payload'
        rbf = output / 'core.rbf'
        rbf.write_bytes(payload)
        (output / 'build-inputs.json').write_bytes(record)
        manifest = tomllib.loads((ROOT / 'tests/fixtures/core-bundle-v2/manifests/valid-basic.toml').read_text())
        manifest['payload'].update(size=len(payload), sha256=hashlib.sha256(payload).hexdigest())
        manifest['build'].update(repository=origin, revision=revision,
                                 recipe_sha256=fields['recipe_sha256'], id=exporter.build_identity(record))
        return fields, encode_manifest(manifest), rbf

    def test_all_producers_export_v1_in_both_layouts(self):
        for producer in PRODUCERS:
            for nested in (False, True):
                with self.subTest(producer=producer.__name__, nested=nested):
                    repo, module = self.fixture(producer, nested)
                    fields, manifest, rbf = self.export_inputs(producer, repo, module)
                    prefix = 'sources/misteross/' if nested else ''
                    self.assertEqual(fields['format'], 1)
                    self.assertNotIn('source_path', fields)
                    self.assertEqual(fields['recipe'], prefix + producer.RECIPE)
                    self.assertEqual(fields['abi_definition'], prefix + producer.ABI_DEFINITION)
                    self.assertEqual(fields['revision'], git(repo, 'rev-parse', 'HEAD'))
                    self.assertEqual(fields['repository'], git(repo, 'remote', 'get-url', 'origin'))
                    self.assertEqual(fields['dependencies'], {})
                    self.assertEqual(exporter.encode_build_record(fields), (rbf.parent / 'build-inputs.json').read_bytes())
                    package = exporter.export_package(manifest, rbf, module / 'build/packages')
                    self.assertEqual(read_package(package).fields['build']['revision'], fields['revision'])

    def test_whole_repository_dirty_rejected_by_each_producer_and_exporter(self):
        for producer in PRODUCERS:
            with self.subTest(producer=producer.__name__):
                repo, module = self.fixture(producer, True)
                _, manifest, rbf = self.export_inputs(producer, repo, module)
                (repo / 'unrelated-parent-edit').write_text('dirty')
                with self.assertRaisesRegex(ValueError, 'clean'):
                    self.require(producer, module)
                with self.assertRaisesRegex(ValueError, 'clean'):
                    exporter.export_package(manifest, rbf, module / 'build/packages')

    def test_final_export_rejects_changed_head_and_removes_publication(self):
        repo, module = self.fixture(PRODUCERS[0], True)
        _, manifest, rbf = self.export_inputs(PRODUCERS[0], repo, module)
        original = exporter._write_sealed
        changed = False
        def write(path, data):
            nonlocal changed
            original(path, data)
            if not changed:
                changed = True
                git(repo, 'commit', '--allow-empty', '-qm', 'concurrent source advance')
        with patch.object(exporter, '_write_sealed', side_effect=write):
            with self.assertRaisesRegex(ValueError, 'pinned revision'):
                exporter.export_package(manifest, rbf, module / 'build/packages')
        self.assertEqual(list((module / 'build/packages').iterdir()), [])

    def test_wrong_nested_path_symlink_and_embedded_checkout_rejected(self):
        repo, module = self.fixture(PRODUCERS[0], True)
        wrong = repo / 'sources/other'
        wrong.mkdir()
        with self.assertRaisesRegex(ValueError, 'exact sources/misteross'):
            legacy_source.context(wrong)
        link = repo / 'link'
        link.symlink_to(module, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'symlink'):
            legacy_source.context(link)
        git(module, 'init', '-q')
        with self.assertRaisesRegex(ValueError, 'embedded checkout'):
            legacy_source.context(module)

    def test_gitlink_not_accepted_as_module_tree(self):
        repo, module = self.fixture(PRODUCERS[0], True)
        revision = git(repo, 'rev-parse', 'HEAD')
        git(repo, 'rm', '-r', '--cached', 'sources/misteross')
        git(repo, 'update-index', '--add', '--cacheinfo', '160000,' + revision + ',sources/misteross')
        git(repo, 'commit', '-qm', 'gitlink')
        with self.assertRaisesRegex(ValueError, 'tracked module tree'):
            legacy_source.context(module)

    def test_quartus_print_commands_work_in_clean_monorepo(self):
        for producer in PRODUCERS[1:]:
            with self.subTest(producer=producer.__name__):
                repo, module = self.fixture(producer, True)
                with patch.object(producer, 'authenticate_quartus',
                                  return_value=(Path('/fixture'), Path('/fixture/quartus_sh'), '17.0.2', 'fixture')), \
                     patch.object(producer, '_run_quartus', side_effect=AssertionError('compiler forbidden')), \
                     patch('sys.stdout', new=io.StringIO()) as stdout:
                    producer.build(module, print_commands=True)
                self.assertIn('command:', stdout.getvalue())
                self.assertEqual(git(repo, 'status', '--porcelain'), '')

    def test_dirty_oracles_stay_unsealed_and_module_relative(self):
        for producer in PRODUCERS[2:4]:
            with self.subTest(producer=producer.__name__):
                repo, module = self.fixture(producer, True)
                (repo / 'dirty').write_text('uncommitted')
                with patch.object(producer, 'authenticate_quartus',
                                  return_value=(Path('/fixture'), Path('/fixture/quartus_sh'), '17.0.2', 'fixture')), \
                     patch.object(producer, '_run_quartus'), \
                     patch.object(producer, 'validate_quartus_evidence', return_value={}):
                    result = producer.compile_oracle(module, require_clean=False)
                self.assertFalse(result['sealed'])
                self.assertEqual(result['repository'], 'uncommitted')
                self.assertEqual(result['revision'], '0' * 40)
                self.assertEqual(set(result['inputs']), set(producer.PINNED_INPUTS))
                self.assertFalse((module / producer.OUTPUT_RELATIVE / 'build-inputs.json').exists())

    def test_clean_oracle_rechecks_entire_repository_after_compilation(self):
        for producer in PRODUCERS[2:4]:
            repo, module = self.fixture(producer, True)
            def change(*args):
                (repo / 'concurrent-edit').write_text('dirty')
            with patch.object(producer, 'authenticate_quartus',
                              return_value=(Path('/fixture'), Path('/fixture/quartus_sh'), '17.0.2', 'fixture')), \
                 patch.object(producer, '_run_quartus', side_effect=change), \
                 patch.object(producer, 'validate_quartus_evidence', return_value={}):
                with self.assertRaisesRegex(ValueError, 'clean'):
                    producer.compile_oracle(module)
