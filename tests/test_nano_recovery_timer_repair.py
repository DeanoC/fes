"""Exercise exact-image timer repair against a stateful card and UART model."""
import copy,json,sys,unittest
from pathlib import Path
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import recovery_trace_upgrade as controller
import test_nano_recovery_trace_upgrade as fixture
import test_nano_recovery_trace_repair as prior_fixture

ORIGINAL=copy.deepcopy(prior_fixture.ROLLBACK)
PRIOR=copy.deepcopy(prior_fixture.OLD)
ORIGINAL_PAIR={k:ORIGINAL[k] for k in ('fit','manifest')}
PRIOR_PAIR={k:PRIOR[k] for k in ('fit','manifest')}
OLD=dict(fit=b'installed-UART1-before-timer-correction'*32,prefix_sha256='d'*64)
before=json.loads(fixture.NEW['manifest']);before['fit_sha256']=controller.sha(OLD['fit'])
before['return_observations']=dict(scope='recovery-return-observations-v1',phases=34,profile_sha256='1'*64)
OLD['manifest']=json.dumps(before).encode()
NEW=copy.deepcopy(fixture.NEW)

class TimerCard(fixture.Card):
    def __init__(self,fault):
        super().__init__(fault)
        for pair,names in ((ORIGINAL,controller.BACKUPS),(PRIOR,controller.Repair.backups)):
            self.files.update({names[controller.BOOT]:pair['fit'],names[controller.META]:pair['manifest']})
        for label,names in (('original',controller.BACKUPS),('prior',controller.Repair.backups)):
            for part,key in (('fit',controller.BOOT),('manifest',controller.META)):
                if fault==label+'-'+part+'-missing':del self.files[names[key]]
                if fault==label+'-'+part+'-corrupt':self.files[names[key]]=b'x'*len(self.files[names[key]])
        if fault=='old-file':self.files[controller.BOOT]=b'x'*len(OLD['fit'])
        if fault=='old-meta-file':self.files[controller.META]=b'x'*len(OLD['manifest'])
    def command(self,c,timeout):
        if c=='fatls mmc 0:1 FES':
            self.commands.append(c)
            existing='r1107a.itb\nr1107a.jsn\nr1107b.itb\nr1107b.jsn'
            if self.fault=='backup-fit-exists':existing+='\nr1107c.itb'
            if self.fault=='backup-manifest-exists':existing+='\nr1107c.jsn'
            return existing
        if c.startswith('fatload') and c.split()[-1] not in self.files:
            self.commands.append(c);return '0 bytes read'
        out=super().command(c,timeout)
        if c.startswith('fatwrite') and self.writes==5:
            for label,names in (('original',controller.BACKUPS),('prior',controller.Repair.backups)):
                for part,key in (('fit',controller.BOOT),('manifest',controller.META)):
                    if self.fault==label+'-'+part+'-after':self.files[names[key]]=b'x'*len(self.files[names[key]])
        return out

class TimerRepairTests(unittest.TestCase):
    def exercise(self,fault,*,mutate_snapshot=False):
        pins=dict(BASE_FIT=controller.sha(ORIGINAL['fit']),ROLLBACK_MANIFEST=controller.sha(ORIGINAL['manifest']),
                  REPAIR_FIT=controller.sha(PRIOR['fit']),REPAIR_MANIFEST=controller.sha(PRIOR['manifest']),
                  TIMER_REPAIR_FIT=controller.sha(OLD['fit']),TIMER_REPAIR_MANIFEST=controller.sha(OLD['manifest']),
                  TIMER_REPAIR_PREFIX=OLD['prefix_sha256'])
        with patch.multiple(controller,**pins),patch.multiple(fixture,OLD=OLD,NEW=NEW):
            card=TimerCard(fault);owner=[fault!='claim-before'];transfers=[];saved=[]
            def command(c,timeout):
                out=TimerCard.command(card,c,timeout)
                if fault=='claim-after-command' or fault=='claim-after-write' and card.writes:owner[0]=False
                return out
            card.command=command
            def wire(purpose,data,reserved):
                transfers.append(purpose)
                if fault=='uart':raise TimeoutError('unknown UART transfer outcome')
                card.memory[0x82000000]=b'wrong' if fault=='probe' else data
                if fault=='claim-uart':owner[0]=False
                r=dict(source_bytes=len(data),source_sha256=controller.sha(data),address=0x82000000,
                       reserved_bytes=reserved,prompt_parked=True);r['pass']=True
                if fault=='uart-receipt':r['source_sha256']='0'*64
                return r
            def save(r):
                saved.append(copy.deepcopy(r))
                if fault=='journal-first' and len(saved)==1:raise OSError('journal unavailable')
                if fault=='journal-write' and r['writes'] and not r['writes'][-1]['acknowledged']:raise OSError('write journal unavailable')
            old,new,original,prior=(copy.deepcopy(p) for p in (OLD,NEW,ORIGINAL_PAIR,PRIOR_PAIR))
            if fault=='baseline':old=copy.deepcopy(ORIGINAL)
            if fault=='prior-source':old=copy.deepcopy(PRIOR)
            if fault=='old-fit':old['fit']+=b'x'
            if fault=='old-manifest':old['manifest']+=b' '
            if fault=='old-prefix':old['prefix_sha256']='e'*64
            for label,pair in (('original',original),('prior',prior)):
                if fault=='host-'+label+'-fit':pair['fit']+=b'x'
                if fault=='host-'+label+'-manifest':pair['manifest']+=b' '
            if fault=='new-profile':
                data=json.loads(new['manifest']);data['return_observations']['profile_sha256']='f'*64;new['manifest']=json.dumps(data).encode()
            if fault=='same-fit':new['fit']=old['fit']
            if mutate_snapshot:
                for pair in (old,new,original,prior):
                    for key in ('fit','manifest'):pair[key]=bytearray(pair[key])
            args=(card,old,new,fixture.PRIME,fixture.PACKAGE,wire,lambda:owner[0],save,original,prior)
            u=controller.TimerRepair(*args)
            if mutate_snapshot:
                for pair in (old,new,original,prior):
                    for key in ('fit','manifest'):pair[key][0]^=1;pair[key]=b'caller replacement'
                old['prefix_sha256']='e'*64
            accepted=False
            try:accepted=u.run()['pass']
            except (ValueError,TimeoutError,OSError):
                if fault=='pass':raise
            self.assertEqual(accepted,fault=='pass',fault)
            self.assertFalse(u.active);self.assertTrue(u.r['session_retired'])
            stopped=(len(card.commands),card.writes,len(transfers))
            for call in (u.run,lambda:u.command('version'),lambda:u.receive('fit',NEW['fit']),
                         lambda:u.write_fat(controller.BOOT,0x82000000,NEW['fit']),
                         lambda:u.preserve_rollback('after-retirement')):
                with self.assertRaises(ValueError):call()
                self.assertEqual(stopped,(len(card.commands),card.writes,len(transfers)),fault)
            forbidden=set(controller.BACKUPS.values())|set(controller.Repair.backups.values())
            self.assertFalse(any(c.startswith('fatwrite') and c.split()[4] in forbidden for c in card.commands))
            self.assertFalse(any(c.startswith(('tftp','dhcp','fesboot','bootm','reset','sf ')) for c in card.commands))
            self.assertEqual(card.files[controller.PRIME],fixture.PRIME);self.assertEqual(card.files[controller.PACKAGE],fixture.PACKAGE)
            if accepted:
                self.assertEqual(card.writes,5);self.assertEqual(transfers,['probe','fit','manifest'])
                self.assertEqual(u.r['preserved_original_rollback'],['before-write','after-write'])
                self.assertEqual(u.r['preserved_prior_rollback'],['before-write','after-write'])
                self.assertTrue(all(row['readback_verified'] for row in u.r['writes']))
                self.assertEqual(card.files[controller.BOOT],NEW['fit']);self.assertEqual(card.files[controller.META],NEW['manifest'])
                for pair,names in ((OLD,u.backups),(ORIGINAL,controller.BACKUPS),(PRIOR,controller.Repair.backups)):
                    self.assertEqual(card.files[names[controller.BOOT]],pair['fit'])
                    self.assertEqual(card.files[names[controller.META]],pair['manifest'])
            late=('backup-timeout','fit-timeout','raw-short','manifest-timeout','readback','new-prefix','claim-after-write')
            if fault!='pass' and fault not in late and not fault.endswith('-after'):
                self.assertEqual(card.writes,0,fault)
            return u
    def test_fixed_writes_and_existing_controller_failure_paths(self):
        for fault in ('pass','version','version-duplicate','version-no-ident','version-foreign-ident','cookie',
                      'backup-fit-exists','backup-manifest-exists','prefix','prime','map','uart','probe','backup-timeout',
                      'fit-timeout','raw-short','manifest-timeout','readback','new-prefix','card','partition','package','uart-receipt'):
            with self.subTest(fault=fault):self.exercise(fault)
    def test_exact_source_and_host_rollback_identities(self):
        for fault in ('baseline','prior-source','old-fit','old-manifest','old-prefix','host-original-fit','host-original-manifest',
                      'host-prior-fit','host-prior-manifest','new-profile','same-fit','old-file','old-meta-file'):
            with self.subTest(fault=fault):self.exercise(fault)
    def test_both_live_rollback_pairs_before_and_after_writes(self):
        for label in ('original','prior'):
            for part in ('fit','manifest'):
                for failure in ('missing','corrupt','after'):
                    fault=label+'-'+part+'-'+failure
                    with self.subTest(fault=fault):self.exercise(fault)
    def test_ownership_journal_and_permanent_retirement(self):
        for fault in ('claim-before','claim-after-command','claim-after-write','claim-uart','journal-first','journal-write'):
            with self.subTest(fault=fault):self.exercise(fault)
    def test_all_caller_inputs_are_snapshotted(self):
        self.exercise('pass',mutate_snapshot=True)
    def test_earlier_transitions_reject_installed_UART1_and_pairs_are_required(self):
        card=TimerCard('pass')
        args=(card,OLD,NEW,fixture.PRIME,fixture.PACKAGE,lambda *a:None,lambda:True,lambda r:None)
        for u in (controller.Upgrade(*args),controller.Repair(*args,ORIGINAL_PAIR)):
            with self.assertRaises(ValueError):u.run()
            self.assertEqual(card.commands,[])
        for invalid in ({},{'fit':b'only'},{'fit':b'x','manifest':b'y','extra':b'z'}):
            with self.assertRaises(ValueError):controller.TimerRepair(*args,ORIGINAL_PAIR,invalid)
            with self.assertRaises(ValueError):controller.TimerRepair(*args,invalid,PRIOR_PAIR)
        self.assertEqual(card.commands,[])

if __name__=='__main__':unittest.main()
