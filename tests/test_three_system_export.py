import tempfile
import unittest
from pathlib import Path
from scripts.rebuild_core import validate_timing, RebuildError, pin_fitter_seed
from scripts.export_core_bundle import BundleManifest, encode_manifest


class ThreeSystemTests(unittest.TestCase):
    def test_accepts_all_timing_categories_and_rejects_negative_or_missing(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'timing'
            valid = ''.join(f'Type : {kind}\nSlack : 0.2\nTNS : 0.000\n\n' for kind in ('Setup x', 'Hold x', 'Recovery x', 'Removal x', 'Minimum Pulse Width x'))
            path.write_text(valid)
            self.assertEqual(len(validate_timing(path)), 5)
            for bad in (valid.replace('0.2', '-0.1', 1), '', valid.replace('0.2', 'nan', 1), valid.replace('TNS : 0.000', 'TNS : -1.0', 1), valid.replace('Hold x', 'Unknown x')):
                path.write_text(bad)
                with self.assertRaises(RebuildError):
                    validate_timing(path)

    def test_seed_requires_one_existing_assignment(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'SNES.qsf'
            path.write_text('set_global_assignment -name SEED 1\n')
            pin_fitter_seed(path, 3)
            self.assertEqual(path.read_text(), 'set_global_assignment -name SEED 3\n')
            path.write_text('')
            with self.assertRaises(RebuildError):
                pin_fitter_seed(path, 3)

    def test_new_system_manifests_keep_eleven_fields(self):
        for system, recipe in [('snes', 'scripts/rebuild_core.py'), ('nes', 'scripts/rebuild_core.py'), ('pong', 'scripts/build_pong.py')]:
            value = BundleManifest(1, 'mister', system, system+'.rbf', 'a'*64, 123, 'https://example.org/source', 'b'*40, recipe, 'c'*64, 'Quartus')
            self.assertEqual(encode_manifest(value).count(b'\n'), 11)

class SnesExportTests(unittest.TestCase):
    def test_snes_export_rejects_recipe_and_timing_drift(self):
        from test_export_core_bundle import ExportCoreBundleTests
        from scripts.core_lock import load_lock
        from scripts.export_core_bundle import export_bundle, BundleExportError
        import json
        import hashlib
        fixture = ExportCoreBundleTests()
        fixture.setUp()
        self.addCleanup(fixture.tearDown)
        pin = load_lock(Path(__file__).resolve().parents[1] / 'cores.lock')['snes']
        work = fixture.root / 'build/rebuild/snes'
        work.mkdir()
        artifact = work / 'snes.rbf'
        artifact.write_bytes(fixture.payload)
        timing = work / 'project/output_files/SNES.sta.summary'
        timing.parent.mkdir(parents=True)
        timing.write_text(''.join(f'Type : {kind}\nSlack : 0.2\nTNS : 0\n' for kind in ('Setup x', 'Hold x', 'Recovery x', 'Removal x', 'Minimum Pulse Width x')))
        report = fixture._compare()
        report.update(core='snes', commit=pin.commit, project=pin.project, locked_sha256=pin.rbf_sha256, locked_size=pin.rbf_size, source=str(fixture.root / 'build/cores/snes'), built_rbf=str(artifact), build_date='260823', fitter_seed=3, timing=validate_timing(timing), timing_sha256=hashlib.sha256(timing.read_bytes()).hexdigest(), recipe_sha256=hashlib.sha256(fixture.recipe.read_bytes()).hexdigest())
        (work / 'compare.json').write_text(json.dumps(report))
        bundle = export_bundle(pin, fixture.root)
        self.assertTrue((bundle / 'snes-rbf.toml').is_file())
        original_recipe = fixture.recipe.read_bytes()
        fixture.recipe.write_text('changed')
        with self.assertRaises(BundleExportError):
            export_bundle(pin, fixture.root)
        fixture.recipe.write_bytes(original_recipe)
        timing.write_text(timing.read_text().replace('0.2', '0.3', 1))
        with self.assertRaises(BundleExportError):
            export_bundle(pin, fixture.root)

class PongExportTests(unittest.TestCase):
    def test_pong_commit_identity_and_source_drift(self):
        import hashlib
        import json
        import subprocess
        import tomllib
        from scripts.build_pong import LOCAL_SOURCES
        from scripts.export_core_bundle import export_pong, BundleExportError, TOOLCHAIN
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            original = Path(__file__).resolve().parents[1]
            for name in LOCAL_SOURCES:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((original / name).read_bytes())
            (root / '.gitignore').write_text('build/\n')
            def git(*args):
                return subprocess.check_output(['git', '-C', str(root), *args], stderr=subprocess.DEVNULL).decode().strip()
            git('init', '-q')
            git('add', '.')
            git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'source')
            work = root / 'build/rebuild/pong'
            work.mkdir(parents=True)
            sha = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
            pin = tomllib.loads((root / 'cores/pong/framework.toml').read_text())
            inputs = {'format': 1, 'system': 'pong', 'abi': 'mister', 'framework': pin, 'sources': {name: sha(root / name) for name in LOCAL_SOURCES}, 'staged_sources': {}}
            (work / 'inputs.json').write_text(json.dumps(inputs))
            (work / 'pong.rbf').write_bytes(b'pong')
            timing = work / 'project/output_files/Pong.sta.summary'
            timing.parent.mkdir(parents=True)
            timing.write_text(''.join(f'Type : {kind}\nSlack : 0.2\nTNS : 0\n' for kind in ('Setup x', 'Hold x', 'Recovery x', 'Removal x', 'Minimum Pulse Width x')))
            receipt = dict(framework=pin, inputs_sha256=sha(work/'inputs.json'), artifact='pong.rbf', sha256=sha(work/'pong.rbf'), size=4, quartus_version=TOOLCHAIN, timing_sha256=sha(timing), timing=validate_timing(timing))
            (work / 'build.json').write_text(json.dumps(receipt))
            bundle = export_pong(root)
            manifest = tomllib.loads((bundle / 'pong-rbf.toml').read_text())
            self.assertEqual(manifest['revision'], git('rev-parse', 'HEAD'))
            self.assertEqual(manifest['repository'], 'https://github.com/DeanoC/misteross')
            (root / 'cores/pong/Pong.sv').write_text('changed')
            with self.assertRaises(BundleExportError):
                export_pong(root)
            git('add', '.')
            git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'changed')
            with self.assertRaises(BundleExportError):
                export_pong(root)
