"""Production entrypoints cannot select retired identity or raw bundle lanes."""
import contextlib
import importlib
import io
import json
from pathlib import Path
import subprocess
import unittest
from tests.producer_fixture import clean_module, EXECUTION
from scripts.export_core_package import build_identity

ROOT = Path(__file__).resolve().parents[1]

class PackageOnlyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.fixture, cls.root = clean_module(ROOT)
    @classmethod
    def tearDownClass(cls):
        cls.fixture.cleanup()
    def test_normal_producers_default_to_two_and_reject_one(self):
        for name in ('pong', 'zx81_oss', 'coleco_oss', 'sms_oss', 'sg1000_oss', 'catch', 'demo'):
            module = importlib.import_module('scripts.build_fes_' + name)
            with self.subTest(producer=name):
                args = (self.root, 'https://example.invalid/fes', 'a' * 40, {'yosys': 'fixture'})
                record = module.create_build_record(*args, execution=EXECUTION)
                self.assertEqual(json.loads(record)['format'], 2)
                with self.assertRaisesRegex(ValueError, 'unsupported'):
                    module.create_build_record(*args, identity_version=1, execution=EXECUTION)
                with self.assertRaisesRegex(ValueError, 'controlled execution'):
                    module.create_build_record(*args)
    def test_demo_variants_and_catch_keep_distinct_functional_identity(self):
        from scripts import build_fes_demo as demo, build_fes_catch as catch
        args = (self.root, 'https://example.invalid/fes', 'a' * 40, {'yosys': 'fixture'})
        records = [demo.create_build_record(*args, execution=EXECUTION, **variant)
                   for variant in ({}, {'media': True}, {'audio': True})]
        records.append(catch.create_build_record(*args, execution=EXECUTION))
        self.assertEqual(len({build_identity(r) for r in records}), 4)
    def test_cli_rejects_legacy_zx81_and_main_programming(self):
        from scripts import build_fes_zx81_oss as zx81, program
        for action, flags in ((zx81.main, ['--legacy']), (program.parse_args, ['--transport', 'mister'])):
            with self.subTest(flags=flags), contextlib.redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as raised:
                    action(flags)
                self.assertEqual(raised.exception.code, 2)
    def test_live_source_rejects_standalone_and_arbitrary_module(self):
        from scripts import fes_build_common as common
        repo = self.root.parent.parent
        for path in (repo, repo / 'sources/other'):
            path.mkdir(exist_ok=True)
            with self.subTest(path=path), self.assertRaisesRegex(ValueError, 'exact sources/misteross'):
                common._require_clean_source(path, pinned_inputs=())
