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
import core_dev_accept
import factory_video_parts

class CatalogTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.metadata = self.root / 'metadata.toml'
        self.metadata.write_text('version = 1\nsource_id = "fes-first-party"\n[[cores]]\ncore_id = "fes.pong"\nlabel = "Pong"\nsystem = "pong"\nstanding = "supported"\n')

    def candidate(self, *, video_interface=None, source_bound=False, parts=False):
        folder = self.root / 'candidate'; folder.mkdir()
        fixture = ROOT / 'sources/FogCast/corepackage/testdata/core-bundle-v2'
        manifest = (fixture / 'manifests/valid-basic.toml').read_bytes().replace(b'fes.fixture', b'fes.pong')
        core = 'fes.coleco' if video_interface else 'fes.pong'
        if video_interface:
            manifest = manifest.replace(b'id = "fes.pong"', b'id = "fes.coleco"').replace(b'id = "fes.simple-game"', b'id = "fes.application"')
            manifest += f'\n[[interfaces]]\nid = "{video_interface}"\nmajor = 1\nminor = 0\nrequired = false\n'.encode()
            self.metadata.write_text(self.metadata.read_text().replace('fes.pong', core))
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
        selection = ('format = 2\nkind = "core-package"\ncore_id = "'+core+'"\npackage_id = "'+identity['package_id']+'"\nmisteross_revision = "'+'1'*40+'"\nmister_packages_revision = "'+'1'*40+'"\npayload_sha256 = "'+sha(payload)+'"\n').encode()
        (folder/'selection.toml').write_bytes(selection)
        archive = (folder/'core.fcore').read_bytes()
        receipt = {'format':1,'core_id':core,'package_id':identity['package_id'],'archive':{'path':'core.fcore','sha256':sha(archive),'size':len(archive)},'sources':{n:'1'*40 for n in ['FogCast','libmister-runtime','misteross','mister-packages']},'selection':{'path':'selection.toml','sha256':sha(selection),'manifest_sha256':sha(manifest),'payload_sha256':sha(payload)}}
        if source_bound:
            sidecar = dict(format=1, selected_repository='https://example.com/fes.git', selected_revision='1'*40,
                           selected_source_path='sources/misteross', original_repository='https://example.com/fes.git',
                           original_revision='1'*40, original_source_path='sources/misteross',
                           functional_inputs_sha256='2'*64, original_record_sha256='3'*64, selected_record_sha256='3'*64,
                           package_id=identity['package_id'], core_rbf_sha256=sha(payload))
            encoded = json.dumps(sidecar).encode()
            (folder/'selection.provenance.json').write_bytes(encoded)
            receipt.update(format=2, source_selection=dict(path='selection.provenance.json', sha256=sha(encoded)))
        if parts:
            records, files = [], {}
            for profile in factory_video_parts.PROFILES:
                archive = (profile + ' synthetic publication fixture').encode()
                part_id = sha(archive)
                relative = identity['package_id'] + '/' + part_id + '.tar'
                files[relative] = archive
                records.append(dict(profile=profile, part_id=part_id, archive_path=relative,
                                    archive_sha256=sha(archive), archive_size=len(archive)))
            index = factory_video_parts.canonical(dict(version=1, packages=[dict(package_id=identity['package_id'], parts=records)]))
            factory_video_parts._publish(folder/'core-video-parts', files | {'index.json': index})
            self.addCleanup(lambda: factory_video_parts._remove(folder/'core-video-parts'))
            (folder/'fes-core-video-parts.json').write_bytes(index)
            receipt['video_parts'] = dict(path='fes-core-video-parts.json', sha256=sha(index), size=len(index))
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

    def test_native_shell_requires_companions_in_preparation_and_catalog(self):
        candidate = self.candidate(video_interface='fes.fabric.video.native-pixels', source_bound=True)
        with self.assertRaisesRegex(ValueError, 'requires its selected parts inventory'):
            core_dev_accept.candidate_arguments(candidate)
        with self.assertRaisesRegex(ValueError, 'requires its selected parts inventory'):
            core_catalog.publish(ROOT, self.metadata, {'fes.coleco': candidate}, self.root/'out')
        self.assertFalse((self.root/'out').exists())
        # A historical v1 receipt must not bypass the publication guard either.
        receipt = json.loads(candidate.read_bytes())
        receipt['format'] = 1
        receipt.pop('source_selection')
        candidate.write_text(json.dumps(receipt))
        with self.assertRaisesRegex(ValueError, 'publication requires its selected parts inventory'):
            core_catalog.publish(ROOT, self.metadata, {'fes.coleco': candidate}, self.root/'out')

    def test_native_catalog_retains_exact_shell_companion_archives(self):
        candidate = self.candidate(video_interface='fes.fabric.video.native-pixels', source_bound=True, parts=True)
        value = core_catalog.publish(ROOT, self.metadata, {'fes.coleco': candidate}, self.root/'out')
        entry = value['entries'][0]
        self.assertEqual([part['profile'] for part in entry['video_parts']], ['direct', 'scanlines'])
        for part in entry['video_parts']:
            published = self.root/'out'/part['archive_path']
            self.assertEqual(part['archive_path'].split('/')[1], entry['package_id'])
            self.assertEqual(hashlib.sha256(published.read_bytes()).hexdigest(), part['archive_sha256'])

    def test_existing_raster_catalog_keeps_exact_shell_companions(self):
        candidate = self.candidate(video_interface='fes.fabric.video.raster-rgb888', source_bound=True, parts=True)
        value = core_catalog.publish(ROOT, self.metadata, {'fes.coleco': candidate}, self.root/'out')
        self.assertEqual([part['profile'] for part in value['entries'][0]['video_parts']], ['direct', 'scanlines'])

    def test_native_catalog_rejects_changed_companion_before_publication(self):
        candidate = self.candidate(video_interface='fes.fabric.video.native-pixels', source_bound=True, parts=True)
        archive = next((candidate.parent/'core-video-parts').glob('*/*.tar'))
        archive.chmod(0o644)
        archive.write_bytes(b'changed')
        archive.chmod(0o444)
        with self.assertRaisesRegex(ValueError, 'digest or size differs'):
            core_catalog.publish(ROOT, self.metadata, {'fes.coleco': candidate}, self.root/'out')
        self.assertFalse((self.root/'out').exists())

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
        self.assertEqual(rows['fes.spectrum']['label'], 'ZX Spectrum 48K')
        self.assertEqual(rows['fes.zx81']['label'], 'Sinclair ZX81')
        self.assertEqual(rows['fes.sms']['label'], 'Sega Master System')
        self.assertEqual(rows['fes.sms']['system'], 'sms')
        self.assertEqual(rows['fes.sg1000']['label'], 'Sega SG-1000')
        self.assertEqual(rows['fes.coleco']['label'], 'ColecoVision')
        self.assertEqual(rows['fes.coleco']['system'], 'coleco')
        self.assertEqual(rows['fes.catch']['label'], 'Catch')
        self.assertEqual(rows['fes.catch']['standing'], 'demo')
        self.assertEqual(rows['fes.ramtest']['standing'], 'supported')
        self.assertEqual(rows['fes.ramtest']['system'], 'ramtest')
        self.assertIn('fes.sms', rows)
        self.assertIn('fes.sg1000', rows)
        self.assertTrue(all('package_id' not in row for row in rows.values()))

    def test_colecovision_alias_is_emitted_as_coleco(self):
        self.metadata.write_text(
            'version = 1\nsource_id = "fes-first-party"\n'
            '[[cores]]\ncore_id = "fes.coleco"\nlabel = "ColecoVision"\n'
            'system = "colecovision"\nstanding = "supported"\n')
        value = core_catalog.publish(ROOT, self.metadata, {}, self.root / 'alias')
        self.assertEqual(value['entries'][0]['system'], 'coleco')
        self.assertNotIn('colecovision', json.dumps(value))

    def test_idle_menu_cannot_be_published_as_a_playable_core(self):
        self.metadata.write_text(self.metadata.read_text().replace('fes.pong', 'fes.menu'))
        with self.assertRaisesRegex(ValueError, 'not a playable catalog core'):
            core_catalog.publish(ROOT, self.metadata, {}, self.root / 'out')

    def test_output_collision_preserves_existing_catalog(self):
        output=self.root/'out';output.mkdir();(output/'catalog.json').write_text('retained')
        with self.assertRaises(FileExistsError):core_catalog.publish(ROOT,self.metadata,{},output)
        self.assertEqual((output/'catalog.json').read_text(),'retained')

if __name__ == '__main__':unittest.main()
