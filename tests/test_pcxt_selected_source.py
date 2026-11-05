import hashlib
import json
from pathlib import Path
import tempfile
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
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name)
        self.orig={'format':2,'repository':selected.bundle.FES_REPOSITORY,'revision':PRODUCER,
                   'source_path':'sources/misteross','source_inputs':{'example':'a'*64}}
        self.curr={**self.orig,'revision':CURRENT}
        self.enc=lambda obj:json.dumps(obj,sort_keys=True,separators=(',',':')).encode()+b'\n'
        self.orig_bytes=self.enc(self.orig);self.curr_bytes=self.enc(self.curr)
        self.side={'format':1,'original_repository':selected.bundle.FES_REPOSITORY,'original_revision':PRODUCER,
                   'original_source_path':'sources/misteross','selected_repository':selected.bundle.FES_REPOSITORY,
                   'selected_revision':CURRENT,'selected_source_path':'sources/misteross',
                   'original_record_sha256':hashlib.sha256(self.orig_bytes).hexdigest(),
                   'selected_record_sha256':hashlib.sha256(self.curr_bytes).hexdigest(),
                   'functional_inputs_sha256':selected.artifact_cache.functional_key(self.orig_bytes),
                   'package_id':PACKAGE,'core_rbf_sha256':RBF}
        self.doc={'format':2,'core_id':'fes.pcxt-dos','archive':{'sha256':ARCHIVE},'package_id':PACKAGE,
                  'selection':{'path':'select.toml','payload_sha256':RBF,'manifest_sha256':'a'*64},
                  'source_selection':{'path':'provenance.json','sha256':'b'*64},'sources':{k:CURRENT for k in ('misteross','FogCast','libmister-runtime','mister-packages')}}
        (self.root/'select.toml').write_text('format = 2\n')
        (self.root/'provenance.json').write_text(json.dumps(self.side))
        self.prepared=self.root/'prepared.json';self.prepared.write_text(json.dumps(self.doc))
        self.kw={'prepared':self.prepared,'prepared_sha256':hashlib.sha256(self.prepared.read_bytes()).hexdigest(),
                 'source_checkout':'/selected','producer_checkout':'/original','archive_sha256':ARCHIVE,
                 'package_id':PACKAGE,'rbf_sha256':RBF,'producer':PRODUCER,'selected':CURRENT}
        self.patches=[mock.patch.object(selected.core_dev_accept,'candidate_arguments'),
          mock.patch.object(selected.core_dev_accept,'source_selection',return_value=self.side),
          mock.patch.object(selected.bundle,'canonical_package_record',side_effect=lambda p,**kw:self.curr_bytes if str(p).startswith('/selected/') else self.orig_bytes),
          mock.patch.object(selected.bundle,'_inspect_package_candidate',return_value={'package_id':PACKAGE,'core_rbf_sha256':RBF,'manifest_sha256':'a'*64})]
        for p in self.patches:p.start();self.addCleanup(p.stop)
        import tarfile,io
        with tarfile.open(self.root/'core.fcore','w') as t:
            for name in ('manifest.toml','core.rbf'):
                info=tarfile.TarInfo(name);info.size=1;t.addfile(info,io.BytesIO(b'x'))
    def test_selection_binds_original_and_current(self):
        evidence=selected.verify(**self.kw)
        self.assertEqual(evidence['selected_commit'],CURRENT)
        self.assertEqual(evidence['producer_commit'],PRODUCER)
        self.assertEqual(evidence['archive_sha256'],ARCHIVE)
    def test_selected_or_original_record_tamper_rejected(self):
        for key in ('selected_record_sha256','original_record_sha256','functional_inputs_sha256'):
            with self.subTest(key=key):
                original=self.side[key];self.side[key]='0'*64
                with self.assertRaises(ValueError):selected.verify(**self.kw)
                self.side[key]=original
    def test_other_package_or_unreviewed_preparation_rejected(self):
        for key in ('package_id','archive_sha256','rbf_sha256','prepared_sha256'):
            with self.subTest(key=key),self.assertRaises(ValueError):
                selected.verify(**dict(self.kw,**{key:'0'*64}))
    def test_different_selected_or_original_commit_rejected(self):
        for key in ('selected','producer'):
            with self.subTest(key=key),self.assertRaises(ValueError):
                selected.verify(**dict(self.kw,**{key:'0'*40}))


class WrapperSelectionTests(unittest.TestCase):
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
