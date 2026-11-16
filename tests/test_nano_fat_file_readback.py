"""Independent recovery-file observations; no physical kit is opened."""
import copy
import sys
from pathlib import Path
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
from fat_file_readback import FATFileReadback
from fat_file_trace import payload
from sd_scratch_readback import TAIL_RAM
import test_nano_sd_scratch_readback as scratch_models
from test_nano_sd_scratch_readback import ReadModel, sha
from test_nano_upload import Model


class FileReadModel(ReadModel):
    def __init__(self,kit,plan,fault=''):
        super().__init__(kit,plan,fault)
        self.file_read=False
        self.files[plan['path']]=payload(plan['path'],updated=True)

    def command(self,c,timeout):
        fault=self.read_fault
        if c=='fatsize mmc 0:1 '+self.plan['path']:
            self.commands.append(c)
            if fault=='missing-file':raise ValueError('File not found')
            self.filesize=1024+(fault=='target-size')
            return ''
        if c.startswith('fatload') and c.split()[4]==self.plan['path']:
            self.commands.append(c)
            if c!=f'fatload mmc 0:1 {TAIL_RAM:x} {self.plan["path"]} 400':
                raise AssertionError('unexpected diagnostic file load')
            self.file_read=self.tail_read=True
            if fault=='target-timeout':raise TimeoutError('file load interrupted')
            if fault=='created-not-updated':self.files[self.plan['path']]=payload(self.plan['path'],updated=False)[:1024]
            if fault=='target-hash':self.files[self.plan['path']]=b'wrong'.ljust(1024,b' ')
            # The original model supplies the actual file-load/LMB bookkeeping.
            self.commands.pop()
            raw=Model.command(self,c,timeout)
            if fault=='target-short':raw='1023 bytes read\r\n'
            if fault=='target-duplicate':raw+=raw
            return raw
        return super().command(c,timeout)


class FATFileRecoveryReadbackTests(unittest.TestCase):
    def setUp(self):
        fixture=scratch_models.ScratchRecoveryReadbackTests();fixture.setUp()
        self.kit,self.plan=fixture.kit,fixture.plan
        del self.plan['original_sha256']
        self.plan.update(scope='nano-sd-fat-file-v1',path='FES/SDP-0123456789abcdef.JSN',
            recovery_bytes=len(self.kit['manifest']),recovery_sha256=sha(self.kit['manifest']),
            fit_bytes=len(self.kit['fit']),fit_sha256=sha(self.kit['fit']))

    def session(self,fault='',owns=lambda:True):
        model=FileReadModel(self.kit,self.plan,fault)
        return model,FATFileReadback(model,self.kit,self.plan,owns=owns),{'commands':[]}

    def test_one_bounded_file_load_and_real_lmb_observation_with_no_media_writes(self):
        model,session,r=self.session();session.run(r)
        self.assertEqual(r['status'],'verified')
        self.assertTrue(r['independent_file_readback'])
        self.assertEqual((r['path'],r['bytes'],r['sha256']),
            (self.plan['path'],1024,sha(payload(self.plan['path'],updated=True))))
        self.assertFalse(r['sd_write_attempted']);self.assertFalse(r['full_cid_reobserved'])
        self.assertFalse(r['ordinary_admission']);self.assertFalse(r['power_loss_qualification'])
        for key in ('boot_files_unchanged','recovery_manifest_unchanged','mbr_unchanged','protected_prefix_unchanged'):
            self.assertTrue(r[key])
        self.assertEqual([x['stage'] for x in r['memory_observations']],['admit','before-file','after-file','complete'])
        self.assertEqual([c for c in model.commands if c.startswith('mmc read')],
            ['mmc read 83000000 0 8000','mmc read 82800000 0 1',
             'mmc read 83000000 0 8000','mmc read 82800000 0 1'])
        self.assertEqual(sum(c.startswith('fatload') and self.plan['path'] in c for c in model.commands),1)
        self.assertIn([TAIL_RAM,TAIL_RAM+1023,'none'],session.memory.owned)
        self.assertEqual(len(session.memory.owned),3)
        before=copy.deepcopy(model.commands)
        with self.assertRaisesRegex(ValueError,'retired'):session.run({'commands':[]})
        self.assertEqual(model.commands,before)

    def test_plan_and_protected_kit_mismatch_reject_before_any_command(self):
        for change in ({'path':'FES/RECOVERY.JSON'},{'recovery_sha256':'0'*64},
                       {'fit_bytes':1024},{'extra':1}):
            model=FileReadModel(self.kit,self.plan)
            with self.subTest(change=change),self.assertRaises(ValueError):
                FATFileReadback(model,self.kit,dict(self.plan,**change),owns=lambda:True)
            self.assertEqual(model.commands,[])

    def test_protected_media_and_card_failures_stop_before_target_load(self):
        for fault in ('wrong-manufacturer','wrong-oem','wrong-product','wrong-capacity',
                'duplicate-card','extra-partition','wrong-partition','mbr','prefix',
                'file-size','duplicate-size','short-file','wrong-hash-range','duplicate-hash'):
            with self.subTest(fault=fault):
                model,session,r=self.session(fault)
                with self.assertRaises(ValueError):session.run(r)
                self.assertEqual(r['status'],'failed');self.assertFalse(r['independent_file_readback'])
                self.assertFalse(model.file_read)

    def test_target_and_postload_failures_never_claim_persistence(self):
        for fault in ('missing-file','target-size','target-timeout','created-not-updated',
                'target-hash','target-short','target-duplicate','late-lmb','late-file-size','late-file-hash'):
            with self.subTest(fault=fault):
                model,session,r=self.session(fault)
                with self.assertRaises((ValueError,TimeoutError)):session.run(r)
                self.assertEqual(r['status'],'failed');self.assertFalse(r['independent_file_readback'])
                self.assertFalse(r['boot_files_unchanged'])
                before=copy.deepcopy(model.commands)
                with self.assertRaisesRegex(ValueError,'retired'):session.run({'commands':[]})
                self.assertEqual(model.commands,before)
                self.assertTrue(session.retired)

    def test_fresh_marker_ownership_and_initial_memory_are_required(self):
        for fault in ('foreign-marker','reserved'):
            model,session,r=self.session()
            if fault=='foreign-marker':model.marker='foreign'
            else:model.extra_regions=[[TAIL_RAM,TAIL_RAM+1023,'none']]
            with self.subTest(fault=fault),self.assertRaises(ValueError):session.run(r)
            self.assertFalse(model.file_read)
        for allowed in (0,1,35):
            calls=0
            def owns():
                nonlocal calls
                calls+=1
                return calls<=allowed
            model,session,r=self.session(owns=owns)
            with self.subTest(allowed=allowed),self.assertRaisesRegex(ValueError,'claim changed'):session.run(r)
            self.assertEqual(len(model.commands),(allowed+1)//2)
            self.assertFalse(r['independent_file_readback'])

    def test_prior_success_receipt_cannot_be_adopted(self):
        model,session,r=self.session()
        with self.assertRaisesRegex(ValueError,'fresh read-only'):
            session.run(dict(r,status='verified',independent_file_readback=True))
        self.assertEqual(model.commands,[])


if __name__=='__main__':unittest.main()
