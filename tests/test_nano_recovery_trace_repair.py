"""Execute the exact repair policy over the stateful card/memory model."""
import copy,json,sys,unittest
from pathlib import Path
from unittest.mock import patch
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import recovery_trace_upgrade as controller
import test_nano_recovery_trace_upgrade as fixture

ROLLBACK=copy.deepcopy(fixture.OLD)
OLD=dict(fit=b'consumed-wrong-UART-fit'*40,prefix_sha256='c'*64)
before=json.loads(fixture.NEW['manifest']);before['fit_sha256']=controller.sha(OLD['fit'])
before['return_observations']['profile_sha256']='0'*64
OLD['manifest']=json.dumps(before).encode()
NEW=copy.deepcopy(fixture.NEW)

class RepairCard(fixture.Card):
    def __init__(self,fault):
        super().__init__(fault)
        self.files.update({controller.BACKUPS[controller.BOOT]:ROLLBACK['fit'],
                           controller.BACKUPS[controller.META]:ROLLBACK['manifest']})
        if fault=='saved-rollback-before':self.files[controller.BACKUPS[controller.BOOT]]=b'x'*len(ROLLBACK['fit'])
        if fault=='missing-rollback':del self.files[controller.BACKUPS[controller.META]]
        if fault=='old-file':self.files[controller.BOOT]=b'x'*len(OLD['fit'])
    def command(self,c,timeout):
        if c=='fatls mmc 0:1 FES':
            self.commands.append(c)
            return 'r1107b.itb' if self.fault=='backup-exists' else 'r1107a.itb\nr1107a.jsn\nr1103.itb\nr1103.jsn'
        if c.startswith('fatload') and c.split()[-1] not in self.files:
            self.commands.append(c);return '0 bytes read'
        out=super().command(c,timeout)
        if self.fault=='saved-rollback-after' and c.startswith('fatwrite') and self.writes==5:
            self.files[controller.BACKUPS[controller.BOOT]]=b'x'*len(ROLLBACK['fit'])
        return out

class TraceRepairTests(unittest.TestCase):
    def exercise(self,fault,*,mutate_snapshot=False):
        pins=dict(BASE_FIT=controller.sha(ROLLBACK['fit']),ROLLBACK_MANIFEST=controller.sha(ROLLBACK['manifest']),
                  REPAIR_FIT=controller.sha(OLD['fit']),REPAIR_MANIFEST=controller.sha(OLD['manifest']),REPAIR_PREFIX=OLD['prefix_sha256'])
        with patch.multiple(controller,**pins),patch.multiple(fixture,OLD=OLD,NEW=NEW):
            card=RepairCard(fault);owner=[fault!='claim-before'];saved=[];transfers=[]
            def command(c,timeout):
                out=RepairCard.command(card,c,timeout)
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
            old,new,rollback=copy.deepcopy(OLD),copy.deepcopy(NEW),{k:ROLLBACK[k] for k in ('fit','manifest')}
            if fault=='baseline':old=copy.deepcopy(ROLLBACK)
            if fault=='old-manifest':old['manifest']+=b' '
            if fault=='old-prefix':old['prefix_sha256']='d'*64
            if fault=='host-rollback':rollback['fit']+=b'x'
            if fault=='host-rollback-manifest':rollback['manifest']+=b' '
            if fault=='new-profile':
                data=json.loads(new['manifest']);data['return_observations']['profile_sha256']='f'*64;new['manifest']=json.dumps(data).encode()
            if fault=='same-fit':new['fit']=old['fit']
            if mutate_snapshot:
                old['fit']=bytearray(old['fit']);new['fit']=bytearray(new['fit']);rollback['fit']=bytearray(rollback['fit'])
            u=controller.Repair(card,old,new,fixture.PRIME,fixture.PACKAGE,wire,lambda:owner[0],save,rollback)
            if mutate_snapshot:
                old['fit'][0]^=1;new['fit'][0]^=1;rollback['fit'][0]^=1
                old['prefix_sha256']='e'*64;rollback['manifest']=b'caller changed';new['manifest']=b'caller changed'
            accepted=False
            try:accepted=u.run()['pass']
            except (ValueError,TimeoutError,OSError):
                if fault=='pass':raise
            self.assertEqual(accepted,fault=='pass',fault)
            self.assertFalse(u.active);self.assertTrue(u.r['session_retired'])
            stop=(len(card.commands),card.writes,len(transfers))
            for call in (u.run,lambda:u.command('version'),lambda:u.receive('fit',NEW['fit']),
                         lambda:u.write_fat(controller.BOOT,0x82000000,NEW['fit'])):
                with self.assertRaises(ValueError):call()
                self.assertEqual(stop,(len(card.commands),card.writes,len(transfers)),fault)
            forbidden=set(controller.BACKUPS.values())
            self.assertFalse(any(c.startswith('fatwrite') and c.split()[4] in forbidden for c in card.commands))
            self.assertFalse(any(c.startswith(('tftp','dhcp','fesboot','bootm','reset','sf ')) for c in card.commands))
            self.assertEqual(card.files[controller.PRIME],fixture.PRIME);self.assertEqual(card.files[controller.PACKAGE],fixture.PACKAGE)
            if accepted:
                self.assertEqual(card.writes,5);self.assertEqual(transfers,['probe','fit','manifest'])
                self.assertEqual(u.r['preserved_original_rollback'],['before-write','after-write'])
                self.assertTrue(all(row['readback_verified'] for row in u.r['writes']))
                self.assertEqual(card.files[u.backups[controller.BOOT]],OLD['fit'])
                self.assertEqual(card.files[u.backups[controller.META]],OLD['manifest'])
                self.assertEqual(card.files[controller.BACKUPS[controller.BOOT]],ROLLBACK['fit'])
                self.assertEqual(card.files[controller.BACKUPS[controller.META]],ROLLBACK['manifest'])
            if fault in ('baseline','old-manifest','old-prefix','host-rollback','host-rollback-manifest','new-profile','same-fit',
                         'old-file','missing-rollback','saved-rollback-before','claim-before','claim-after-command','claim-uart','journal-first','journal-write'):
                self.assertEqual(card.writes,0,fault)
            return u
    def test_repair_success_and_failure_boundaries(self):
        for fault in ('pass','version','version-duplicate','version-no-ident','version-foreign-ident','cookie','backup-exists',
                      'prefix','prime','map','uart','probe','backup-timeout','fit-timeout','raw-short','manifest-timeout',
                      'readback','new-prefix','card','partition','package','uart-receipt','old-file','missing-rollback',
                      'saved-rollback-before','saved-rollback-after'):
            with self.subTest(fault=fault):self.exercise(fault)
    def test_exact_transition_ownership_and_journal_failures(self):
        for fault in ('baseline','old-manifest','old-prefix','host-rollback','host-rollback-manifest','new-profile','same-fit',
                      'claim-before','claim-after-command','claim-after-write','claim-uart','journal-first','journal-write'):
            with self.subTest(fault=fault):self.exercise(fault)
    def test_caller_owned_repair_and_rollback_inputs_are_snapshotted(self):
        self.exercise('pass',mutate_snapshot=True)
    def test_original_updater_still_rejects_trace_and_repair_requires_rollback_pair(self):
        card=RepairCard('pass')
        args=(card,OLD,NEW,fixture.PRIME,fixture.PACKAGE,lambda *a:None,lambda:True,lambda r:None)
        u=controller.Upgrade(*args)
        with self.assertRaises(ValueError):u.run()
        self.assertEqual(card.commands,[]);self.assertTrue(u.r['session_retired'])
        for rollback in ({},{'fit':b'only-one'},{'fit':b'x','manifest':b'y','extra':b'z'}):
            with self.assertRaises(ValueError):controller.Repair(*args,rollback)
        self.assertEqual(card.commands,[])

if __name__=='__main__':unittest.main()
