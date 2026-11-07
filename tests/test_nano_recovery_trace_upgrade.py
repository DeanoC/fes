"""Execute the trace-upgrade controller on the existing stateful card/LMB model."""
import copy
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import recovery_trace_upgrade as controller
from test_nano_upload import Model

OLD=dict(fit=b'old-fit'*100,prefix_sha256='a'*64)
NEW=dict(fit=b'new-fit'*120,prefix_sha256='b'*64)
OLD['manifest']=json.dumps(dict(fit_sha256=controller.sha(OLD['fit']),native_handoff=False,automatic_writes=False)).encode()
NEW['manifest']=json.dumps(dict(fit_sha256=controller.sha(NEW['fit']),spl_sha256=controller.BASE_SPL,
    bl31_sha256=controller.BASE_BL31,return_observations=dict(scope=controller.MODE,phases=34,
        profile_sha256=controller.sha(Path(controller.recovery_return_trace.__file__).read_bytes())),
    native_handoff=False,automatic_writes=False)).encode()
PRIME=bytes(4*(1<<20)+4096);PACKAGE=b'retained package remains exact'


class Card(Model):
    def __init__(self,fault):
        super().__init__(OLD,b'');self.fault=fault;self.parked=True;self.cookie='';self.raw_prefix=OLD['prefix_sha256']
        self.files={controller.BOOT:OLD['fit'],controller.META:OLD['manifest'],controller.PRIME:PRIME,controller.PACKAGE:PACKAGE}
    def command(self,c,timeout):
        if c=='bdinfo':
            if self.fault=='map' and self.map_reads>=3:self.extra_regions=[[0x83f00000,0x83ffffff,'none']]
            return super().command(c,timeout)
        self.commands.append(c)
        if c=='version':
            text=controller.OLD_VERSION+' (Nov 03 2026 - 00:00:00)socfpga_agilex5\n'
            if self.fault=='version':return 'wrong'
            if self.fault=='version-duplicate':return text+text
            if self.fault=='version-no-ident':return text.replace('socfpga_agilex5','')
            if self.fault=='version-foreign-ident':return text.replace('socfpga_agilex5','socfpga_agilex5_other')
            return text
        if c.startswith('echo FES_UPGRADE_'):
            key=c.split('=')[1].strip('${}');value=self.cookie if key=='fes_upgrade_session' else 'old' if self.fault=='cookie' else ''
            return c.split()[1].split('=')[0]+'='+value+'\r\n'
        if c.startswith('setenv fes_upgrade_session '):self.cookie=c.split()[2];return ''
        if c=='mmc dev 0':return ''
        if c=='mmcinfo':return 'SD version 3.0\nCapacity: '+('7.4 GiB' if self.fault=='card' else '3.7 GiB')
        if c=='mmc part':return '1 32768 '+('1000' if self.fault=='partition' else '262144')+'\n'
        if c=='fatls mmc 0:1 FES':return 'r1107a.itb' if self.fault=='backup-exists' else 'r1103.itb\nr1103.jsn\nrecovery.json\npkgtest.bin'
        if c.startswith('mmc read'):self.memory[0x83000000]=None;return '32768 blocks read: OK'
        if c.startswith('hash sha256'):
            _,_,address,count=c.split();data=self.memory[int(address,16)]
            value=self.raw_prefix if data is None else controller.sha(data[:int(count,16)])
            if self.fault=='prefix' and not self.writes:value='0'*64
            if self.fault=='new-prefix' and self.writes==4 and data is None:value='0'*64
            return value
        if c.startswith('fatload'):
            _,_,_,address,name=c.split();address=int(address,16);data=self.files[name]
            if self.fault=='prime' and name==controller.PRIME:data=b'bad'
            if self.fault=='package' and name==controller.PACKAGE:data=b'bad'
            if self.fault=='readback' and name==controller.BOOT and self.writes==3:data=b'x'*len(data)
            self.memory[address]=data;self.allocations[address]=max(self.allocations.get(address,0),len(data))
            return str(len(data))+' bytes read'
        if c.startswith('fatwrite'):
            _,_,_,address,name,count=c.split();self.files[name]=self.memory[int(address,16)][:int(count,16)];self.writes+=1
            if (self.fault,self.writes) in (('backup-timeout',1),('fit-timeout',3),('manifest-timeout',5)):
                raise TimeoutError('unknown FAT write outcome')
            return str(int(count,16))+' bytes written'
        if c.startswith('mmc write'):
            _,_,address,sector,count=c.split();assert sector=='200'
            padded=(max(len(OLD['fit']),len(NEW['fit']))+511)&~511
            assert self.memory[int(address,16)]==NEW['fit']+bytes(padded-len(NEW['fit']))
            self.writes+=1;self.raw_prefix=NEW['prefix_sha256']
            return '0 blocks written: OK' if self.fault=='raw-short' else str(int(count,16))+' blocks written: OK'
        raise AssertionError(c)


class TraceUpgradeTests(unittest.TestCase):
    def exercise(self,fault,*,mutate_snapshot=False):
        card=Card(fault);owner=[fault!='claim-before'];transfers=[];saved=[]
        def transfer(purpose,data,reserved):
            transfers.append(purpose)
            if fault=='uart':raise TimeoutError('unknown UART transfer')
            card.memory[0x82000000]=b'wrong' if fault=='probe' else data
            if fault=='claim-uart':owner[0]=False
            return dict(pass_=True,source_bytes=len(data),source_sha256=controller.sha(data),address=0x82000000,
                reserved_bytes=reserved,prompt_parked=True)
        def wire(purpose,data,reserved):
            r=transfer(purpose,data,reserved);r['pass']=r.pop('pass_')
            if fault=='uart-receipt':r['source_sha256']='0'*64
            return r
        def save(r):
            saved.append(copy.deepcopy(r))
            if fault=='journal-first' and len(saved)==1:raise OSError('journal failed')
            if fault=='journal-write' and r['writes'] and not r['writes'][-1]['acknowledged']:raise OSError('journal failed')
        original=card.command
        def command(c,timeout):
            r=original(c,timeout)
            if fault=='claim-after-command':owner[0]=False
            return r
        card.command=command
        old,new=copy.deepcopy(OLD),copy.deepcopy(NEW)
        if fault=='new-profile':new['manifest']=json.dumps(dict(fit_sha256=controller.sha(new['fit']))).encode()
        if fault=='trace-identity':
            bad=json.loads(new['manifest']);bad['return_observations']['profile_sha256']='0'*64;new['manifest']=json.dumps(bad).encode()
        if fault=='baseline':old['fit']=b'foreign'
        if fault=='prefix-identity':old['prefix_sha256']='bad'
        if mutate_snapshot:new['fit']=bytearray(new['fit'])
        with patch.object(controller,'BASE_FIT',controller.sha(OLD['fit'])):
            u=controller.Upgrade(card,old,new,PRIME,PACKAGE,wire,lambda:owner[0],save)
            if mutate_snapshot:new['fit'][0]^=1;old['manifest']=b'caller changed its map'
            accepted=False
            try:accepted=u.run()['pass']
            except (ValueError,TimeoutError,OSError):pass
        self.assertEqual(accepted,fault=='pass',fault)
        self.assertFalse(u.active);self.assertTrue(u.r['session_retired'])
        before=(len(card.commands),card.writes,len(transfers))
        for call in (u.run,lambda:u.command('version'),lambda:u.write_fat(controller.BOOT,0x82000000,NEW['fit']),lambda:u.receive('fit',NEW['fit'])):
            with self.assertRaises(ValueError):call()
            self.assertEqual(before,(len(card.commands),card.writes,len(transfers)),fault)
        self.assertFalse(any(c.startswith(('dhcp','tftp','fesboot','bootm','reset','sf ')) for c in card.commands))
        self.assertEqual(card.files[controller.PRIME],PRIME)
        self.assertEqual(card.files[controller.PACKAGE],PACKAGE)
        if accepted:
            self.assertEqual(card.writes,5);self.assertEqual(transfers,['probe','fit','manifest'])
            self.assertEqual(card.files[controller.BOOT],NEW['fit']);self.assertEqual(card.files[controller.META],NEW['manifest'])
            self.assertEqual(card.files[controller.BACKUPS[controller.BOOT]],OLD['fit'])
            self.assertTrue(all(r['readback_verified'] for r in u.r['writes']))
        if fault in ('new-profile','trace-identity','baseline','prefix-identity','package','claim-before','claim-after-command','claim-uart','journal-first','journal-write'):
            self.assertEqual(card.writes,0,fault)
        return u

    def test_success_and_original_failure_boundaries(self):
        for fault in ('pass','version','version-duplicate','version-no-ident','version-foreign-ident','cookie',
            'backup-exists','prefix','prime','map','uart','probe','backup-timeout','fit-timeout','raw-short',
            'manifest-timeout','readback','new-prefix','card','partition','package','uart-receipt'):
            with self.subTest(fault=fault):self.exercise(fault)

    def test_ownership_input_and_journal_failure_retire_every_wire_entrypoint(self):
        for fault in ('new-profile','trace-identity','baseline','prefix-identity','claim-before','claim-after-command','claim-uart','journal-first','journal-write'):
            with self.subTest(fault=fault):self.exercise(fault)

    def test_caller_owned_bytes_and_maps_are_snapshotted(self):
        self.exercise('pass',mutate_snapshot=True)


if __name__=='__main__':unittest.main()
