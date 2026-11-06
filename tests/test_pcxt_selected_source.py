import hashlib
import json
from pathlib import Path
import tempfile
import subprocess
import sys
import unittest
from unittest import mock

from scripts import pcxt_selected_source as selected
from scripts import pcxt_floppy_persistence_acceptance as floppy

PRODUCER='bb87b9b657bf9f1826dd1b9e001736e7bd8484ee'
CURRENT='02f2c6f298db409f0b96491155fc7fd89ab5bf13'
SOFTWARE='bb51f4454eb72048cefeea16aa16b28eb2aadaad'
PACKAGE='87fd8149c09a7e319092c56802b891039a118dfe0378d302a6543bcf841659fc'
ARCHIVE='19fd34ec5d193e2e8c6b7723ea86b02bb9e8f76bd4febf2a77c88a0759b224d8'
RBF='94b3c222bee23d24fd027a8c908d90aa9f53eae9b01db648e655f8b1d7ac0222'


class SelectionTests(unittest.TestCase):
    def setUp(self):
        self.side={'format':1,'original_repository':selected.REPOSITORY,'original_revision':PRODUCER,
                   'original_source_path':'sources/misteross','selected_repository':selected.REPOSITORY,
                   'selected_revision':CURRENT,'selected_source_path':'sources/misteross',
                   'original_record_sha256':'1'*64,'selected_record_sha256':'2'*64,
                   'functional_inputs_sha256':'3'*64,'package_id':PACKAGE,'core_rbf_sha256':RBF}
        self.doc={'format':2,'core_id':'fes.pcxt-dos','archive':{'sha256':ARCHIVE},'package_id':PACKAGE,
                  'selection':{'path':'select.toml','sha256':'4'*64,'payload_sha256':RBF,'manifest_sha256':'a'*64},
                  'source_selection':{'sha256':'b'*64},'sources':{k:CURRENT for k in ('misteross','FogCast','libmister-runtime','mister-packages')}}
        self.result={'source_selection':self.side.copy(),'selection_sha256':'4'*64,'package_id':PACKAGE,
            'payload_sha256':RBF,'manifest_sha256':'a'*64,'archive_sha256':ARCHIVE}
        self.kw=dict(doc=self.doc,sidecar=self.side,resolved_doc=self.doc.copy(),selection_bytes=b'selection',
            provenance_bytes=b'provenance',resolved_selection=b'selection',resolved_provenance=b'provenance',
            prepared_sha256='5'*64,archive_sha256=ARCHIVE,package_id=PACKAGE,rbf_sha256=RBF,
            producer=PRODUCER,selected=CURRENT)
    def test_worker_rejects_video_producers_before_prepare(self):
        driver = """
import sys,types
bundle=types.ModuleType('bundle');core_dev=types.ModuleType('core_dev');recipes=types.ModuleType('recipes')
recipes.recipe_for=lambda _core:types.SimpleNamespace(video_profiles=('extra',))
core_dev.prepare=lambda *a,**kw: (_ for _ in ()).throw(AssertionError('prepare must not run'))
sys.modules.update(bundle=bundle,core_dev=core_dev,recipes=recipes)
sys.argv=['worker','/tmp','/tmp']
exec(WORKER)
"""
        program='WORKER='+repr(selected.WORKER)+'\n'+driver
        result=subprocess.run([sys.executable,'-I','-c',program],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        self.assertNotEqual(result.returncode,0)
        self.assertIn(b'video profile producers are not permitted',result.stderr)
        self.assertNotIn(b'prepare must not run',result.stderr)

    def test_selection_binds_original_and_current(self):
        evidence=selected.compare_resolution(**self.kw)
        self.assertEqual(evidence['selected_commit'],CURRENT)
        self.assertEqual(evidence['producer_commit'],PRODUCER)
        self.assertEqual(evidence['archive_sha256'],ARCHIVE)
    def test_resolver_result_and_exact_bytes_must_match(self):
        for key in ('resolved_selection','resolved_provenance'):
            with self.subTest(key=key),self.assertRaises(ValueError):
                selected.compare_resolution(**dict(self.kw,**{key:b'altered'}))
        result=dict(self.doc);result['package_id']='0'*64
        with self.assertRaises(ValueError):
            selected.compare_resolution(**dict(self.kw,resolved_doc=result))
    def test_changed_selected_recipe_or_cache_winner_rejected(self):
        # The selected commit's normal preparation output must match all
        # pinned receipt and sidecar bytes, including canonical input records.
        for key in ('selected_record_sha256','original_record_sha256','functional_inputs_sha256'):
            changed=dict(self.side,**{key:'0'*64})
            with self.subTest(key=key),self.assertRaises(ValueError):
                selected.compare_resolution(**dict(self.kw,sidecar=changed,resolved_provenance=b'changed'))
    def test_other_package_or_selected_source_rejected(self):
        for key,value in (('package_id','0'*64),('archive_sha256','0'*64),('rbf_sha256','0'*64),
                          ('selected','0'*40),('producer','0'*40)):
            with self.subTest(key=key),self.assertRaises(ValueError):
                selected.compare_resolution(**dict(self.kw,**{key:value}))


class WrapperSelectionTests(unittest.TestCase):
    def test_observer_uses_owner_proof_but_rechecks_bytes_and_source(self):
        from argparse import Namespace
        from scripts import pcxt_dos_application_acceptance as app
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            selected_checkout = root / 'selected'; selected_checkout.mkdir()
            producer_checkout = root / 'producer'; producer_checkout.mkdir()
            files = {'core.fcore': b'package bytes', 'select.toml': b'selection',
                     'select.provenance.json': b'provenance'}
            for name, data in files.items(): (root / name).write_bytes(data)
            digest = lambda data: hashlib.sha256(data).hexdigest()
            doc = {key: {'path': name, 'sha256': digest(files[name])} for key, name in
                   (('archive', 'core.fcore'), ('selection', 'select.toml'),
                    ('source_selection', 'select.provenance.json'))}
            prepared = root / 'prepared.json'; prepared.write_text(json.dumps(doc))
            session_archive = root / 'session.fcore'; session_archive.write_bytes(files['core.fcore'])
            args = Namespace(selected_prepared=prepared, selected_prepared_sha256=digest(prepared.read_bytes()),
                selected_source_checkout=selected_checkout, producer_source_checkout=producer_checkout,
                source_repository='DeanoC/fes', source_historical_main=True, source_main=False,
                source_pr=None, source_commit=PRODUCER, source_observed_tip=CURRENT, source_minimum_tip=CURRENT,
                archive=session_archive, archive_sha256=digest(files['core.fcore']), package_id=PACKAGE, rbf_sha256=RBF)
            evidence = {'producer_commit': PRODUCER, 'selected_commit': CURRENT,
                'archive_sha256': args.archive_sha256, 'package_id': PACKAGE, 'rbf_sha256': RBF,
                'prepared_sha256': args.selected_prepared_sha256}
            calls = []
            heads = {selected_checkout: CURRENT, producer_checkout: PRODUCER}
            def command(argv, deadline, cwd=None, **kwargs):
                if argv[:2] == ['git', 'status']: return b''
                if argv[:3] == ['git', 'rev-parse', 'HEAD']: return (heads[cwd]+'\n').encode()
                calls.append(argv)
                return json.dumps(evidence).encode()
            def child_check():
                def observe(_deadline):
                    provenance = {}
                    floppy.selected_source_admission(provenance, args, root=root)
                    return {'proof': provenance['selected_package']}
                return app._hard_deadline_observation(observe, seconds=2)
            with mock.patch.object(floppy, 'bounded_source_command', side_effect=command):
                floppy.selected_source_admission({}, args, root=root)
                self.assertEqual(len(calls), 1)
                self.assertEqual(child_check()['proof'], evidence)
                for path in (prepared, *(root/name for name in files), session_archive):
                    original = path.read_bytes(); path.write_bytes(original+b'changed')
                    with self.subTest(path=path.name), self.assertRaises(app.pa.AcceptanceError): child_check()
                    path.write_bytes(original)
                heads[selected_checkout] = '0'*40
                with self.assertRaises(app.pa.AcceptanceError): child_check()
                heads[selected_checkout] = CURRENT
                args.package_id = '0'*64
                with self.assertRaises(app.pa.AcceptanceError): child_check()
                args.package_id = PACKAGE
                # The owner must rerun the full resolver for final evidence.
                floppy.selected_source_admission({}, args, root=root)
                self.assertEqual(len(calls), 2)
                evidence['package_id'] = '0'*64
                with self.assertRaises(app.pa.AcceptanceError):
                    floppy.selected_source_admission({}, args, root=root)

    def test_unpaired_options_and_pr_mode_rejected(self):
        from argparse import Namespace
        for a in (Namespace(selected_prepared=Path('/x')), Namespace(selected_prepared=Path('/x'),
            selected_prepared_sha256='0'*64, selected_source_checkout=Path('/x'),producer_source_checkout=Path('/y'),source_repository='DeanoC/fes',
            source_historical_main=False,source_main=True,source_pr=None,source_minimum_tip='x',source_observed_tip='x')):
            with self.assertRaises(floppy.pa.AcceptanceError):floppy.selected_source_admission({},a,root=Path('/tmp'))
    def test_selected_policy_rejects_other_github_repository(self):
        from argparse import Namespace
        a=Namespace(selected_prepared=Path('/x'),selected_prepared_sha256='0'*64,
            selected_source_checkout=Path('/x'),producer_source_checkout=Path('/y'),
            source_repository='Other/fes',source_historical_main=True,source_main=False,
            source_pr=None,source_minimum_tip=CURRENT,source_observed_tip=CURRENT)
        with self.assertRaises(floppy.pa.AcceptanceError):
            floppy.selected_source_admission({},a,root=Path('/tmp'))

    def test_selected_policy_stops_when_main_advances(self):
        from argparse import Namespace
        raw_diff=b'proof'; live={'sha':CURRENT,'parents':[{'sha':PRODUCER}]}
        a=Namespace(selected_prepared=Path('/prepared'),source_repository='Example/fes',source_base='main',
            source_pr=None,source_commit=PRODUCER,source_tool_commit='b'*40,source_main=False,
            source_historical_main=True,source_observed_tip=CURRENT,source_minimum_tip=CURRENT,
            source_diff_sha256=hashlib.sha256(raw_diff).hexdigest())
        root=Path(floppy.__file__).resolve().parent.parent
        def command(argv,deadline,cwd=None):
            if argv[:3]==['git','rev-parse','HEAD']:return b'b'*40+b'\n'
            if argv[:2]==['git','show']:return (root/argv[2].split(':',1)[1]).read_bytes()
            if argv[:2]==['git','diff']:return raw_diff
            if argv[-1]=='repos/Example/fes/commits/main':return json.dumps(live).encode()
            raise AssertionError(argv)
        with mock.patch.object(floppy,'bounded_source_command',side_effect=command):
            floppy.require_source(a)
            live['sha']='d'*40;live['parents']=[{'sha':CURRENT}]
            with self.assertRaises(floppy.pa.AcceptanceError):floppy.require_source(a)

    def test_historical_companion_without_selection_remains_rejected(self):
        from argparse import Namespace
        a=Namespace(compatible_software_source=SOFTWARE,source_main=False,source_historical_main=True,source_pr=None,source_commit=PRODUCER)
        with self.assertRaises(floppy.pa.AcceptanceError):
            floppy.check_source_admission({'host':{'source_commit':PRODUCER},'software':{'source_commit':SOFTWARE}},a)

if __name__=='__main__':unittest.main()
