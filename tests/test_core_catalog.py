import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import core_catalog

class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.metadata = self.root / 'metadata.toml'
        self.metadata.write_text('version = 1\nsource_id = "fes-first-party"\n[[cores]]\ncore_id = "fes.pong"\nlabel = "Pong"\nsystem = "pong"\nstanding = "supported"\n')

    def candidate(self):
        folder = self.root / 'candidate'; folder.mkdir()
        fixture = ROOT / 'sources/FogCast/corepackage/testdata/core-bundle-v2'
        manifest = (fixture / 'manifests/valid-basic.toml').read_bytes().replace(b'fes.fixture', b'fes.pong')
        payload = (fixture / 'payloads/fes-fixture.rbf').read_bytes()
        (folder / 'manifest.toml').write_bytes(manifest)
        (folder / 'core.rbf').write_bytes(payload)
        program = '''import sys,json,hashlib
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from scripts.export_core_package import _archive_bytes
from scripts.core_package import read_package
p=Path(sys.argv[2]);p.joinpath('core.fcore').write_bytes(_archive_bytes(p.joinpath('manifest.toml').read_bytes(),p.joinpath('core.rbf').read_bytes(),None))
x=read_package(p/'core.fcore');print(json.dumps({'package_id':x.package_id,'core_id':x.fields['core']['id']}))'''
        identity = json.loads(subprocess.check_output([sys.executable,'-I','-c',program,str(ROOT/'sources/misteross'),str(folder)],text=True))
        sha = lambda b: hashlib.sha256(b).hexdigest()
        selection = ('format = 2\nkind = "core-package"\ncore_id = "fes.pong"\npackage_id = "'+identity['package_id']+'"\nmisteross_revision = "'+'1'*40+'"\nmister_packages_revision = "'+'1'*40+'"\npayload_sha256 = "'+sha(payload)+'"\n').encode()
        (folder/'selection.toml').write_bytes(selection)
        archive = (folder/'core.fcore').read_bytes()
        receipt = {'format':1,'core_id':'fes.pong','package_id':identity['package_id'],'archive':{'path':'core.fcore','sha256':sha(archive),'size':len(archive)},'sources':{n:'1'*40 for n in ['FogCast','libmister-runtime','misteross','mister-packages']},'selection':{'path':'selection.toml','sha256':sha(selection),'manifest_sha256':sha(manifest),'payload_sha256':sha(payload)}}
        path = folder/'prepared.json'; path.write_text(json.dumps(receipt));return path

    def test_unprepared_core_has_no_install_action(self):
        value = core_catalog.publish(ROOT,self.metadata,{},self.root/'out')
        self.assertEqual(value['source_id'],'fes-first-party')
        self.assertNotIn('package_id',value['entries'][0])

    def test_publish_binds_prepared_identity(self):
        candidate = self.candidate()
        value = core_catalog.publish(ROOT,self.metadata,{'fes.pong':candidate},self.root/'out')
        row=value['entries'][0]
        self.assertEqual(row['package_id'],json.loads(candidate.read_text())['package_id'])
        self.assertEqual(hashlib.sha256((self.root/'out'/row['archive_path']).read_bytes()).hexdigest(),row['archive_sha256'])

    def test_repeat_publication_same_bytes(self):
        candidate=self.candidate()
        a=core_catalog.publish(ROOT,self.metadata,{'fes.pong':candidate},self.root/'a')
        b=core_catalog.publish(ROOT,self.metadata,{'fes.pong':candidate},self.root/'b')
        self.assertEqual(a,b)

    def test_tampered_or_wrong_core_candidate_rejected(self):
        candidate=self.candidate();candidate.with_name('core.fcore').write_bytes(b'changed')
        with self.assertRaises(ValueError):core_catalog.publish(ROOT,self.metadata,{'fes.pong':candidate},self.root/'out')
        self.assertFalse((self.root/'out').exists())

    def test_wrong_core_candidate_rejected(self):
        candidate = self.candidate()
        self.metadata.write_text(self.metadata.read_text().replace('fes.pong', 'fes.sms'))
        with self.assertRaises(ValueError):
            core_catalog.publish(ROOT, self.metadata, {'fes.sms': candidate}, self.root / 'out')
        self.assertFalse((self.root / 'out').exists())

    def test_curated_metadata_covers_registered_cores(self):
        value = core_catalog.publish(ROOT, ROOT / 'config/core-library.toml', {}, self.root / 'all')
        rows = {row['core_id']: row for row in value['entries']}
        self.assertEqual(set(rows), set(core_catalog.recipes.load_recipes()) - {'fes.menu'})
        self.assertEqual(rows['fes.apple2']['standing'], 'experimental')
        self.assertEqual(rows['fes.c64']['standing'], 'experimental')
        self.assertEqual(rows['fes.c64']['system'], 'c64')
        self.assertEqual(rows['fes.spectrum']['standing'], 'experimental')
        self.assertEqual(rows['fes.spectrum']['system'], 'spectrum')
        self.assertEqual(rows['fes.catch']['standing'], 'demo')
        self.assertEqual(rows['fes.ramtest']['standing'], 'supported')
        self.assertEqual(rows['fes.ramtest']['system'], 'ramtest')
        self.assertIn('fes.sms', rows)
        self.assertIn('fes.sg1000', rows)
        self.assertTrue(all('package_id' not in row for row in rows.values()))

    def test_idle_menu_cannot_be_published_as_a_playable_core(self):
        self.metadata.write_text(self.metadata.read_text().replace('fes.pong', 'fes.menu'))
        with self.assertRaisesRegex(ValueError, 'not a playable catalog core'):
            core_catalog.publish(ROOT, self.metadata, {}, self.root / 'out')

    def test_output_collision_preserves_existing_catalog(self):
        output=self.root/'out';output.mkdir();(output/'catalog.json').write_text('retained')
        with self.assertRaises(FileExistsError):core_catalog.publish(ROOT,self.metadata,{},output)
        self.assertEqual((output/'catalog.json').read_text(),'retained')

if __name__ == '__main__':unittest.main()
