"""Scoped off-board derivation reuse retains the complete source guard."""
import concurrent.futures
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from test_nano_native_admission import Model

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('cold_derivation_test',ROOT/'platform/embedded/nano/cold_profile.py')
cold=importlib.util.module_from_spec(spec);spec.loader.exec_module(cold)
hps=cold.hps_context


class DerivationTests(unittest.TestCase):
    def model(self, root):
        m=Model(root)
        tree=root/'platform/embedded/tamago';tree.mkdir(parents=True)
        (tree/'main.go').write_text('fixture native source')
        return m

    def call(self, m, body, worker):
        with patch.object(cold,'ROOT',m.root),patch.object(cold,'_make',body),patch.object(hps.subprocess,'run',worker):
            return cold.make(m.entry,firmware_ram=True)

    def worker(self, calls):
        def run(argv, **kwargs):
            self.assertEqual(kwargs['timeout'],120)
            self.assertEqual(argv[1],str(Path(hps.__file__).resolve()))
            calls.append(argv)
            return SimpleNamespace(returncode=0,stderr=b'',stdout=json.dumps({'args':argv[2:],'nested':{'values':[1,2]}}).encode())
        return run

    def test_unique_queries_copied_results_and_independent_derivations(self):
        with tempfile.TemporaryDirectory() as temp:
            m=self.model(Path(temp));calls=[]
            def body(path, **kwargs):
                self.assertEqual((path,kwargs),(m.entry,{'firmware_ram':True}))
                first=hps.control(m.projects[0]);first['nested']['values'][0]=99
                self.assertEqual(hps.control(m.projects[0])['nested']['values'],[1,2])
                for _ in range(2):
                    hps.derive(m.projects[0]);hps.control(m.projects[1])
                    hps._worker(m.projects[0],'selected-program','a'*64)
                    hps._worker(m.projects[0],'selected-program','b'*64)
                return {'complete':True}
            self.assertEqual(self.call(m,body,self.worker(calls)),{'complete':True})
            self.assertEqual(len(calls),5);self.assertIsNone(hps._derivation.get())
            self.call(m,body,self.worker(calls));self.assertEqual(len(calls),10)
            with patch.object(hps.subprocess,'run',self.worker(calls)):
                hps.control(m.projects[0]);hps.control(m.projects[0])
            self.assertEqual(len(calls),12)

    def test_source_bytes_membership_receipt_and_selected_files_cannot_change(self):
        changes=(
            lambda m:(m.projects[0]/'board/output.sof').write_bytes(b'changed'),
            lambda m:(m.root/'sources/misteross/core/core.sv').unlink(),
            lambda m:(m.root/'platform/embedded/tamago/extra.go').write_text('new source'),
            lambda m:(m.root/'platform/embedded/nano/new-directory').mkdir(),
            lambda m:(m.entry/'payload.elf').write_bytes(b'changed payload'),
            lambda m:(m.entry/'result.json').write_text('{}'),
            lambda m:(m.root/'producer.py').write_text('changed producer'),
        )
        for change in changes:
            with self.subTest(change=change),tempfile.TemporaryDirectory() as temp:
                m=self.model(Path(temp));calls=[]
                def body(*args,**kwargs):
                    hps.control(m.projects[0]);change(m);hps.control(m.projects[0])
                    return {'would_publish':True}
                with self.assertRaises((ValueError,OSError)):
                    self.call(m,body,self.worker(calls))
                self.assertIsNone(hps._derivation.get());self.assertEqual(len(calls),1)

    def test_outside_project_nested_scope_and_symlinks_reject(self):
        with tempfile.TemporaryDirectory() as temp:
            m=self.model(Path(temp));calls=[]
            with self.assertRaisesRegex(ValueError,'outside'):
                self.call(m,lambda *a,**k:hps.control(m.root/'out/foreign'),self.worker(calls))
            self.assertEqual(calls,[]);self.assertIsNone(hps._derivation.get())
            def nested(*args,**kwargs):
                with hps.worker_scope(m.projects,lambda:{}):self.fail('nested derivation entered')
            with self.assertRaisesRegex(ValueError,'nested'):self.call(m,nested,self.worker(calls))
            self.assertIsNone(hps._derivation.get())
            (m.projects[0]/'escape').symlink_to(m.root/'producer.py')
            with self.assertRaisesRegex(ValueError,'symlink'):self.call(m,lambda *a,**k:None,self.worker(calls))
            self.assertEqual(calls,[])

    def test_failed_workers_are_not_cached_and_scope_does_not_leak(self):
        with tempfile.TemporaryDirectory() as temp:
            m=self.model(Path(temp));calls=[];run=self.worker(calls)
            def flaky(argv,**kwargs):
                if not calls:
                    calls.append(argv);return SimpleNamespace(returncode=1,stderr=b'failed verification',stdout=b'')
                return run(argv,**kwargs)
            def body(*args,**kwargs):
                with self.assertRaisesRegex(ValueError,'failed verification'):hps.control(m.projects[0])
                hps.control(m.projects[0]);hps.control(m.projects[0])
            self.call(m,body,flaky);self.assertEqual(len(calls),2);self.assertIsNone(hps._derivation.get())
            with self.assertRaisesRegex(RuntimeError,'abort'):
                self.call(m,lambda *a,**k:(_ for _ in ()).throw(RuntimeError('abort')),run)
            self.assertIsNone(hps._derivation.get())

    def test_context_is_not_shared_between_threads(self):
        calls=[];barrier=threading.Barrier(2)
        with patch.object(hps.subprocess,'run',self.worker(calls)):
            def run(_):
                with hps.worker_scope([ROOT],lambda:{'fixed':True}):
                    hps.control(ROOT);barrier.wait(timeout=5);hps.control(ROOT)
                self.assertIsNone(hps._derivation.get())
            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:list(pool.map(run,range(2)))
        self.assertEqual(len(calls),2)

    def test_worker_environment_is_part_of_the_query(self):
        calls=[]
        with patch.object(hps.subprocess,'run',self.worker(calls)),hps.worker_scope([ROOT],lambda:{}):
            with patch.dict(os.environ,{'FES_TEST_DERIVATION_ENV':'first'}):
                hps.control(ROOT);hps.control(ROOT)
            with patch.dict(os.environ,{'FES_TEST_DERIVATION_ENV':'second'}):
                hps.control(ROOT)
            with patch.dict(os.environ,{'FES_TEST_DERIVATION_ENV':'first'}):
                hps.control(ROOT)
        self.assertEqual(len(calls),2)

    def test_invalid_firmware_selection_rejects_before_files(self):
        with self.assertRaisesRegex(ValueError,'explicit firmware RAM'):cold.make(Path('/no/such/entry'),firmware_ram=1)


if __name__=='__main__':unittest.main()
