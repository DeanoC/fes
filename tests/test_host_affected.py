"""Regression coverage for narrowing host tests without dropping consumers."""
import json
import contextlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from scripts import host_affected, host_tests, test_policy, affected


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.root = Path('/repo')
        def package(name, imports=(), tests=(), external=(), module=host_affected.MODULES[0]):
            return {'ImportPath': 'github.com/example/' + name,
                    'Dir': str(self.root / module / name),
                    'Module': {'Dir': str(self.root / module)},
                    'Imports': list(imports), 'TestImports': list(tests),
                    'XTestImports': list(external)}
        self.records = [package('lib'), package('api', ['github.com/example/lib']),
                        package('consumer', tests=['github.com/example/api']),
                        package('external', external=['github.com/example/lib']),
                        package('unrelated')]

    def test_selector_changes_run_host_lane_without_core_simulation(self):
        for path in ('scripts/host_affected.py', 'tests/test_host_affected.py'):
            plan = affected.plan([path])
            self.assertTrue(plan['lanes']['host'])
            self.assertTrue(plan['lanes']['parent'])
            self.assertEqual(plan['cores'], [])

    def test_reverse_transitive_and_external_test_imports(self):
        result = host_affected.select(['sources/FogCast/lib/lib.go'], self.records, self.root)
        self.assertEqual(result, {'github.com/example/' + n
                                 for n in ('lib', 'api', 'consumer', 'external')})

    def test_test_only_edit_keeps_its_package_and_consumers(self):
        self.assertEqual(host_affected.select(['sources/FogCast/api/api_test.go'],
                                             self.records, self.root),
                         {'github.com/example/api', 'github.com/example/consumer'})

    def test_unknown_inputs_and_deleted_whole_packages_keep_all_tests(self):
        for paths in ([], ['<new-branch>'], ['sources/FogCast/go.mod'],
                      ['sources/FogCast/lib/file.go', 'sources/libmister-runtime/src/api.cpp'],
                      ['sources/FogCast/lib/data.json'],
                      ['sources/FogCast/gone/deleted.go']):
            with self.subTest(paths=paths):
                self.assertIsNone(host_affected.select(paths, self.records, self.root))

    def test_guides_keep_changed_go_packages_and_reverse_consumers(self):
        go = 'sources/FogCast/lib/lib.go'
        expected = host_affected.select([go], self.records, self.root)
        for guide in ('README.md', 'AGENTS.md', 'docs/development.md',
                      'sources/FogCast/README.md', 'sources/FogCast/docs/ARCHITECTURE.md',
                      'sources/FogCast/appliance/docs/update.md',
                      'sources/misteross/expansion/docs/format.md'):
            with self.subTest(guide=guide):
                self.assertEqual(host_affected.select([go, guide], self.records, self.root), expected)
        self.assertIsNone(host_affected.select(['docs/development.md'], self.records, self.root))

    def test_markdown_assets_testdata_and_unreviewed_paths_keep_all(self):
        for other in ('sources/FogCast/lib/README.md',
                      'sources/FogCast/lib/testdata/docs/example.md',
                      'sources/FogCast/docs/testdata/example.md',
                      'sources/FogCast/docs/data.json', 'notes/guide.md',
                      'sources/FogCast/docs/../go.mod', '/docs/guide.md',
                      './docs/guide.md', 'docs-other/guide.md', 'sources/FogCast/go.sum'):
            with self.subTest(other=other):
                self.assertIsNone(host_affected.select(
                    ['sources/FogCast/lib/lib.go', other], self.records, self.root))

    def test_embed_inputs_keep_full_coverage_even_for_deleted_guides(self):
        for field in host_affected.EMBED_FIELDS:
            record = {'ImportPath': 'github.com/example/root',
                      'Dir': str(self.root / host_affected.MODULES[0]), field: ['docs']}
            with self.subTest(field=field):
                self.assertIsNone(host_affected.select(
                    ['sources/FogCast/lib/lib.go', 'sources/FogCast/docs/deleted.md'],
                    [*self.records, record], self.root))

    def test_guide_symlink_cannot_hide_a_runtime_fixture(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'docs').mkdir()
            (root / 'fixture.md').write_text('data')
            (root / 'docs/guide.md').symlink_to('../fixture.md')
            self.assertIsNone(host_affected.select(
                ['sources/FogCast/lib/lib.go', 'docs/guide.md'], self.records, root))

    def test_host_runner_narrows_go_plus_guides_before_sharding(self):
        records = list(self.records)
        for module in host_affected.MODULES[1:]:
            records.append({'ImportPath': 'github.com/example/' + module.rsplit('/', 1)[-1],
                            'Dir': str(self.root / module), 'Module': {'Dir': str(self.root / module)}})
        packages = '\n'.join(record['ImportPath'] for record in self.records)
        paths = ['sources/FogCast/lib/lib.go', 'docs/development.md']
        with patch.object(host_tests, '__file__', str(self.root / 'scripts/host_tests.py')), \
                patch.object(host_affected, 'listing', return_value=records), \
                patch.object(host_tests.subprocess, 'check_output', return_value=packages), \
                contextlib.redirect_stdout(io.StringIO()) as output:
            self.assertEqual(host_tests.main(['--plan-only', '--paths-json', json.dumps(paths)]), 0)
        report = json.loads(output.getvalue())
        self.assertEqual(report['selection']['scope'], 'affected')
        selected = host_affected.select(paths, records, self.root)
        self.assertEqual(set(report['selection']['packages']), selected)
        self.assertFalse(any('github.com/example/unrelated' in command['argv']
                             for command in report['commands']))

    def test_removed_file_and_rename_keep_both_existing_package_owners(self):
        selected = host_affected.select(['sources/FogCast/api/deleted.go',
                                        'sources/FogCast/unrelated/new.go',
                                        'docs/deleted-guide.md'], self.records, self.root)
        self.assertEqual(selected, {'github.com/example/api', 'github.com/example/consumer',
                                    'github.com/example/unrelated'})

    def test_filter_never_leaves_bare_go_test_and_expands_module_patterns(self):
        command = {'cwd': host_affected.MODULES[0], 'label': 'test',
                   'argv': ['go', 'test', '-race', '-short', './...']}
        selected = {'github.com/example/api'}
        self.assertEqual(host_affected.filter_commands([command], selected, self.records, self.root),
                         [{**command, 'argv': ['go', 'test', '-race', '-short', *selected]}])
        command['argv'][-1] = './unrelated'
        self.assertEqual(host_affected.filter_commands([command], selected, self.records, self.root), [])

    def test_shards_preserve_every_selected_package_once(self):
        command = {'cwd': host_affected.MODULES[0],
                   'label': test_policy.ORDINARY_RACE_LABEL,
                   'argv': ['go', 'test', '-race', '-short',
                            *[record['ImportPath'] for record in self.records]]}
        selected = host_affected.select(['sources/FogCast/lib/lib.go', 'docs/development.md'], self.records, self.root)
        commands = host_affected.filter_commands([command], selected, self.records, self.root)
        packages = []
        for index in range(3):
            for shard in test_policy.shard_host_commands(commands, index, 3):
                packages.extend(arg for arg in shard['argv'] if arg.startswith('github.com/'))
        self.assertCountEqual(packages, selected)

    def test_full_mode_ignores_narrowing(self):
        with patch.object(host_affected, 'listing', side_effect=AssertionError('must not narrow')), \
                patch('sys.stdout') as out:
            self.assertEqual(host_tests.main(['--full', '--plan-only', '--paths-json',
                                             '["sources/FogCast/lib/lib.go", "docs/development.md"]']), 0)
        report = json.loads(''.join(call.args[0] for call in out.write.call_args_list))
        self.assertEqual(report['selection']['scope'], 'all')
        self.assertEqual(report['commands'], test_policy.host_test_commands(full=True))

    def test_invalid_paths_fail_instead_of_silently_omitting_tests(self):
        for value in ('{}', 'null', '[1]', 'invalid'):
            with self.subTest(value=value), patch('sys.stderr'), self.assertRaises(SystemExit):
                host_tests.main(['--plan-only', '--paths-json', value])

    @unittest.skipUnless(shutil.which('go'), 'Go required for real import graph fixture')
    def test_real_go_graph_crosses_nested_modules_and_test_imports(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            modules = host_affected.MODULES
            for module, name in zip(modules, ('host', 'appliance', 'expansion')):
                target = root / module
                target.mkdir(parents=True, exist_ok=True)
                (target / 'go.mod').write_text('module example.com/' + name + '\n\ngo 1.20\n')
                (target / (name + '.go')).write_text('package ' + name + '\n')
            host = root / modules[0]
            with (host / 'go.mod').open('a') as out:
                out.write('\nrequire example.com/appliance v0.0.0\n'
                          'replace example.com/appliance => ./appliance\n'
                          'require example.com/expansion v0.0.0\n'
                          'replace example.com/expansion => ../misteross/expansion\n')
            (host / 'host_test.go').write_text('package host_test\nimport _ "example.com/appliance"\n')
            (host / 'host_race.go').write_text(
                '//go:build race\n\npackage host\nimport _ "example.com/expansion"\n')
            env = dict(os.environ, GOWORK='off', GOTOOLCHAIN='local')
            for name in ('GOOS', 'GOARCH', 'GOARM', 'GOFLAGS'):
                env.pop(name, None)
            records = host_affected.listing(root, env)
            selected = host_affected.select([modules[1] + '/appliance.go'], records, root)
            self.assertEqual(selected, {'example.com/appliance', 'example.com/host'})
            selected = host_affected.select([modules[2] + '/expansion.go'], records, root)
            self.assertEqual(selected, {'example.com/expansion', 'example.com/host'})
            (host / 'docs').mkdir()
            (host / 'docs/guide.md').write_text('guide')
            mixed = [modules[1] + '/appliance.go', modules[0] + '/docs/guide.md']
            self.assertEqual(host_affected.select(mixed, records, root),
                             {'example.com/appliance', 'example.com/host'})
            (host / 'embed_test.go').write_text(
                'package host\nimport _ "embed"\n//go:embed docs\nvar testGuide string\n')
            records = host_affected.listing(root, env)
            self.assertIsNone(host_affected.select(mixed, records, root))
            # The directory embed still matters after the changed guide was
            # deleted, even though EmbedFiles only lists the remaining file.
            (host / 'docs/other.md').write_text('remaining')
            (host / 'docs/guide.md').unlink()
            records = host_affected.listing(root, env)
            self.assertIsNone(host_affected.select(mixed, records, root))
            (host / 'embed_test.go').unlink()
            (host / 'embed_race_test.go').write_text(
                '//go:build race\n\npackage host\nimport _ "embed"\n'
                '//go:embed docs\nvar raceGuide string\n')
            records = host_affected.listing(root, env)
            self.assertTrue(any(record.get('TestEmbedPatterns') for record in records))
            self.assertTrue(any(not record.get('TestEmbedPatterns') and
                                record['ImportPath'] == 'example.com/host' for record in records))
            self.assertIsNone(host_affected.select(mixed, records, root))


if __name__ == '__main__':
    unittest.main()
