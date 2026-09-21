"""Clone transport normalization preserves real Git provenance and configuration."""
import importlib
import unittest

from scripts.source_repository import canonical_repository
from scripts import export_core_package as exporter
from scripts.core_package import encode_manifest, read_package
from tests import test_source_provenance as fixtures


class RepositoryTests(unittest.TestCase):
    fixture = fixtures.SourceProvenanceTests.fixture
    require = fixtures.SourceProvenanceTests.require
    export_inputs = fixtures.SourceProvenanceTests.export_inputs

    def test_only_supported_ssh_spellings_normalize_and_https_bytes_survive(self):
        for origin in ('git@github.com:DeanoC/fes.git', 'ssh://git@github.com/DeanoC/fes.git'):
            self.assertEqual(canonical_repository(origin), 'https://github.com/DeanoC/fes.git')
        for origin in ('https://github.com/DeanoC/fes.git', 'https://example.invalid/Fork/Repo',
                       'https://github.com/CaseOwner/CaseRepo'):
            self.assertEqual(canonical_repository(origin), origin)
        self.assertEqual(canonical_repository('git@github.com:OtherOwner/ActualFork.git'),
                         'https://github.com/OtherOwner/ActualFork.git')
        for origin in ('github.com:DeanoC/fes.git', 'git@example.invalid:DeanoC/fes.git', 'ssh://git@github.com:2222/DeanoC/fes.git',
                       'ssh://other@github.com/DeanoC/fes.git', 'git@github.com:../fes.git',
                       'git@github.com:DeanoC/fes.git/other', 'git@github.com:DeanoC/..'):
            with self.subTest(origin=origin), self.assertRaisesRegex(ValueError, 'unsupported SSH'):
                canonical_repository(origin)

    def test_real_git_ssh_v1_v2_export_module_preserves_remote(self):
        producer = fixtures.PRODUCERS[0]
        for nested in (True,):
            for version in (1, 2):
                for origin in ('git@github.com:DeanoC/fes.git', 'ssh://git@github.com/DeanoC/fes.git'):
                    with self.subTest(nested=nested, version=version, origin=origin):
                        repo, module = self.fixture(producer, nested)
                        fixtures.git(repo, 'remote', 'set-url', 'origin', origin)
                        fields, manifest, rbf = self.export_inputs(producer, repo, module)
                        if version == 2:
                            fields.update(format=2, recipe=producer.RECIPE, abi_definition=producer.ABI_DEFINITION,
                                          source_path='sources/misteross' if nested else '.',
                                          source_roots=['cores', 'scripts'])
                            fields['source_inputs'] = exporter.source_input_closure(module, fields['source_roots'])
                            import tomllib
                            manifest_fields = tomllib.loads(manifest.decode())
                            record = exporter.encode_build_record(fields)
                            (rbf.parent / 'build-inputs.json').write_bytes(record)
                            manifest_fields['build']['id'] = exporter.build_identity(record)
                            manifest = encode_manifest(manifest_fields)
                        for _ in range(2):
                            sealed = exporter.export_package(manifest, rbf, module / 'build/packages')
                            self.assertEqual(read_package(sealed).fields['build']['repository'],
                                             'https://github.com/DeanoC/fes.git')
                            self.assertEqual(fixtures.git(repo, 'remote', 'get-url', 'origin'), origin)
                        self.assertEqual(fixtures.git(repo, 'status', '--porcelain'), '')

    def test_all_oss_and_oracle_guards_return_equivalent_origin_without_mutation(self):
        oss = [importlib.import_module('scripts.build_fes_' + name)
               for name in ('pong', 'zx81_oss', 'coleco_oss', 'sms_oss', 'sg1000_oss')]
        for producer in [*fixtures.PRODUCERS, *oss]:
            with self.subTest(producer=producer.__name__):
                repo, module = self.fixture(producer, True)
                origin = 'git@github.com:DeanoC/fes.git'
                fixtures.git(repo, 'remote', 'set-url', 'origin', origin)
                if producer in oss:
                    observed, revision = producer._require_clean_source(module, identity_version=2)
                else:
                    observed, revision = self.require(producer, module)
                self.assertEqual(observed, 'https://github.com/DeanoC/fes.git')
                self.assertEqual(revision, fixtures.git(repo, 'rev-parse', 'HEAD'))
                self.assertEqual(fixtures.git(repo, 'remote', 'get-url', 'origin'), origin)

    def test_export_rejects_different_owner_and_unsupported_ssh(self):
        repo, module = self.fixture(fixtures.PRODUCERS[0], True)
        fixtures.git(repo, 'remote', 'set-url', 'origin', 'git@github.com:DeanoC/fes.git')
        _, manifest, rbf = self.export_inputs(fixtures.PRODUCERS[0], repo, module)
        for origin in ('git@github.com:AnotherOwner/fes.git', 'git@other.example:DeanoC/fes.git'):
            fixtures.git(repo, 'remote', 'set-url', 'origin', origin)
            with self.assertRaises(ValueError):
                exporter.export_package(manifest, rbf, module / 'build/packages')
            self.assertEqual(fixtures.git(repo, 'remote', 'get-url', 'origin'), origin)
