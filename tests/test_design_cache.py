"""Real resolver/exporter/cache integration with an explicitly synthetic package."""
import dataclasses
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from scripts import bundle

ROOT = Path(__file__).resolve().parents[1]
MODULE = ROOT / 'sources/misteross'
PRODUCER = '''
import hashlib
import subprocess
from scripts.export_core_package import encode_build_record, source_input_closure
from scripts.input_identity import with_identities

def _require_clean_source(root, **options):
    return 'https://github.com/DeanoC/fes.git', subprocess.check_output(
        ['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()

def _authenticate_tools(root, **options):
    raise RuntimeError('compiler deliberately unavailable in cache-only fixture')

def create_build_record(root, repository, revision, tools, *, execution, identity_version=2):
    inputs = source_input_closure(root, ['cores/fixture', 'scripts'])
    fields = dict(format=2, repository=repository, revision=revision,
        source_path='sources/misteross', source_roots=['cores/fixture', 'scripts'],
        source_inputs=inputs, recipe='scripts/fixture_producer.py',
        recipe_sha256=inputs['scripts/fixture_producer.py'], abi_definition='cores/fixture/core.json',
        abi_definition_sha256=inputs['cores/fixture/core.json'], dependencies={}, tools=tools,
        parameters={'seed': 1, 'execution_sha256': hashlib.sha256(repr(execution).encode()).hexdigest()})
    if execution.get('selection_only'):
        fields['parameters']['selection_only'] = True
    return encode_build_record(with_identities(fields, root=root))
'''


class DesignCacheTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.cleanup)
        self.directory = Path(self.temp.name)
        self.repo = self.directory / 'repo'
        self.source = self.repo / 'sources/misteross'
        scripts = self.source / 'scripts'
        scripts.mkdir(parents=True)
        for name in ('__init__.py', 'export_core_package.py', 'input_identity.py',
                     'core_package.py', 'compiler_read_audit.py', 'source_repository.py', 'rfc3986_validator.py'):
            if (MODULE / 'scripts' / name).exists():
                shutil.copyfile(MODULE / 'scripts' / name, scripts / name)
        (scripts / 'fixture_producer.py').write_text(PRODUCER)
        (scripts / 'build_fes_pong.py').write_text(
            'from scripts.fixture_producer import _require_clean_source, _authenticate_tools, create_build_record\n')
        core = self.source / 'cores/fixture'
        core.mkdir(parents=True)
        (core / 'top.v').write_text('module top; endmodule\n')
        (core / 'core.json').write_text('{}\n')
        self.lock = core / 'toolchain.lock'
        self.lock.write_text('[tool.yosys]\nrepo="https://example.invalid/yosys"\ncommit="' + 'a' * 40 + '"\n')
        (self.repo / '.gitignore').write_text('__pycache__/\nbuild/\n')
        self.git('init', '-q')
        self.git('config', 'user.name', 'Fixture')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.git('remote', 'add', 'origin', 'https://github.com/DeanoC/fes.git')
        self.commit()
        program = '''
import hashlib,json,tomllib,sys
from pathlib import Path
from scripts import fixture_producer as producer
from scripts.core_package import encode_manifest
from scripts.export_core_package import export_package,build_identity
from scripts.input_identity import manifest_toolchain
root=Path.cwd(); output=root/'build/fixture'; output.mkdir(parents=True)
record=producer.create_build_record(root,*producer._require_clean_source(root),
    {'fixture':'synthetic; no FPGA compiler'},execution={'gpu_device':'lease'})
(output/'build-inputs.json').write_bytes(record)
payload=Path(sys.argv[1]).read_bytes(); (output/'core.rbf').write_bytes(payload)
fields=tomllib.loads(Path(sys.argv[2]).read_text()); inputs=json.loads(record)
fields['core']['id']='fes.pong'
fields['payload'].update(size=len(payload),sha256=hashlib.sha256(payload).hexdigest())
fields['build'].update(id=build_identity(record),repository=inputs['repository'],revision=inputs['revision'],
    recipe_sha256=inputs['recipe_sha256'],toolchain=manifest_toolchain(record))
package=export_package(encode_manifest(fields),output/'core.rbf',root/'build/fixture-store')
print(package)
'''
        fixture = MODULE / 'tests/fixtures/core-bundle-v2'
        package = Path(subprocess.check_output([sys.executable, '-c', program,
            str(fixture / 'payloads/fes-fixture.rbf'), str(fixture / 'manifests/valid-basic.toml')],
            cwd=self.source, text=True).strip())
        self.cache = self.directory / 'cache'
        self.record = package.with_suffix('.build-inputs.json').read_bytes()
        self.cached = bundle.artifact_cache.publish(self.cache, package.with_suffix('.build-inputs.json'), package)
        self.original_bytes = (self.cached / 'core.rbf').read_bytes()
        self.recipe = dataclasses.replace(bundle.recipe_for('fes.pong'), producer_module='scripts.fixture_producer')

    def cleanup(self):
        if hasattr(self, 'directory'):
            for path in self.directory.rglob('*'):
                if not path.is_symlink():
                    path.chmod(0o755 if path.is_dir() else 0o644)
        self.temp.cleanup()

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.repo), *args], text=True).strip()

    def commit(self):
        self.git('add', '-A')
        self.git('commit', '-qm', 'fixture inputs')

    def resolve(self, force=False):
        with patch.object(bundle, 'ARTIFACT_CACHE_ROOT', self.cache):
            return bundle.resolve_core_package(self.source, 'd' * 40,
                self.directory / 'selection.toml', recipe=self.recipe, force=force)

    def test_packaging_change_reuses_sealed_package_without_a_compiler(self):
        path = self.source / 'scripts/export_core_package.py'
        path.write_text(path.read_text() + '\n# Packaging-only fixture edit.\n')
        self.commit()
        selected = self.resolve()
        self.assertEqual(selected['directory'], self.cached)
        self.assertEqual((self.cached / 'core.rbf').read_bytes(), self.original_bytes)
        self.assertEqual((self.cached.parent / 'build-inputs.json').read_bytes(), self.record)
        receipt = selected['inputs']['source_selection']
        self.assertNotEqual(receipt['original_revision'], receipt['selected_revision'])
        self.assertFalse(receipt['toolchain_update_available'])

    def test_new_pin_is_advisory_and_original_pin_remains_visible(self):
        self.lock.write_text(self.lock.read_text().replace('a' * 40, 'b' * 40))
        self.commit()
        receipt = self.resolve()['inputs']['source_selection']
        self.assertTrue(receipt['toolchain_update_available'])
        self.assertEqual(receipt['built_pins']['cores/fixture/toolchain.lock']['yosys']['commit'], 'a' * 40)
        self.assertEqual(receipt['selected_pins']['cores/fixture/toolchain.lock']['yosys']['commit'], 'b' * 40)
        self.assertEqual((self.cached.parent / 'build-inputs.json').read_bytes(), self.record)

    def test_live_source_report_compares_pins_without_authenticating_a_compiler(self):
        self.lock.write_text(self.lock.read_text().replace('a' * 40, 'b' * 40))
        self.commit()
        output = subprocess.check_output([sys.executable, str(MODULE / 'tools/identity.py'),
            str(self.cached.parent / 'build-inputs.json'), '--source', str(self.source),
            '--core', 'fes.pong'], text=True)
        report = json.loads(output)
        self.assertFalse(report['design_inputs_changed'])
        self.assertTrue(report['toolchain_change_only'])
        self.assertFalse(report['execution_comparison_available'])
        self.assertTrue(report['candidate']['selection_only'])

    def test_design_change_requires_authentication_and_cannot_reuse_old_package(self):
        path = self.source / 'cores/fixture/top.v'
        path.write_text(path.read_text() + '// changed RTL\n')
        self.commit()
        with self.assertRaisesRegex(ValueError, 'cannot derive authenticated'):
            self.resolve()

    def test_forced_update_cannot_fall_back_to_the_existing_package(self):
        with self.assertRaisesRegex(ValueError, 'cannot derive authenticated'):
            self.resolve(force=True)

    def test_forced_build_requires_its_new_record_even_when_old_design_matches(self):
        local = self.source / 'build/packages'
        local.mkdir()
        shutil.copytree(self.cached, local / self.cached.name)
        record_path = local / (self.cached.name + '.build-inputs.json')
        record_path.write_bytes(self.record)
        record_path.chmod(0o444)
        fields = json.loads(self.record)
        fields['tools'] = {'fixture': 'different synthetic execution'}
        fields = bundle.artifact_cache.identity.with_identities(fields)
        authenticated = bundle.artifact_cache.identity.canonical(fields) + b'\n'
        with patch.object(bundle, 'canonical_package_record', return_value=authenticated), \
             patch.object(bundle, '_build_package'):
            with self.assertRaisesRegex(ValueError, 'forced build did not produce'):
                self.resolve(force=True)

    def test_corrupt_cached_payload_fails_instead_of_rebuilding_or_relabelling(self):
        payload = self.cached / 'core.rbf'
        payload.chmod(0o644)
        payload.write_bytes(b'corrupted synthetic payload')
        payload.chmod(0o444)
        with patch.object(bundle, '_build_package') as build:
            with self.assertRaisesRegex(ValueError, 'producer validation'):
                self.resolve()
            build.assert_not_called()

    def test_selection_record_cannot_publish_as_build_evidence(self):
        record = bundle.canonical_package_record(self.source, recipe=self.recipe, selection_only=True)
        path = self.directory / 'selection-inputs.json'
        path.write_bytes(record)
        with self.assertRaisesRegex(ValueError, 'selection-only'):
            bundle.artifact_cache.publish(self.cache, path, self.cached)

    def test_namespace_link_is_rejected_and_legacy_cache_is_not_relabelled(self):
        namespace = self.cache / 'design-v1'
        self.assertEqual(self.cached.parent.parent.parent, namespace)
        namespace.rename(self.cache / 'saved')
        namespace.symlink_to(self.cache / 'saved', target_is_directory=True)
        with self.assertRaises(ValueError):
            self.resolve()

    def test_recording_changes_keep_package_bytes_and_independent_evidence(self):
        program = '''
import json,sys
from pathlib import Path
from scripts.export_core_package import encode_build_record,export_package
from scripts.input_identity import with_identities
root=Path.cwd(); output=root/'build/fixture'
original=(output/'build-inputs.json').read_bytes(); fields=json.loads(original)
# Redundant enumeration describes the same source set through different roots.
fields['source_roots']=sorted(fields['source_roots']+['scripts/core_package.py'])
record=encode_build_record(with_identities(fields))
(output/'build-inputs.json').write_bytes(record)
package=export_package(Path(sys.argv[1]).read_bytes(),output/'core.rbf',root/'build/fixture-store')
print(json.dumps({'package':str(package),'record':json.loads(record)['identities']['record']}))
'''
        result = json.loads(subprocess.check_output([sys.executable, '-c', program,
            str(self.cached / 'manifest.toml')], cwd=self.source, text=True))
        package = Path(result['package'])
        new_evidence = package.with_name(package.name + '.' + result['record'] + '.build-inputs.json')
        self.assertEqual(package.name, self.cached.name)
        self.assertEqual((package / 'manifest.toml').read_bytes(), (self.cached / 'manifest.toml').read_bytes())
        self.assertEqual(package.with_suffix('.build-inputs.json').read_bytes(), self.record)
        self.assertNotEqual(new_evidence.read_bytes(), self.record)
        original_key = bundle.artifact_cache.functional_key(self.record)
        self.assertEqual(bundle.artifact_cache.functional_key(new_evidence.read_bytes()), original_key)
        second = bundle.artifact_cache.publish(self.cache, new_evidence, package)
        self.assertEqual(second.name, self.cached.name)
        self.assertNotEqual(second.parent, self.cached.parent)
        self.assertEqual((self.cached.parent / 'build-inputs.json').read_bytes(), self.record)
