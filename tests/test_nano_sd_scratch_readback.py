"""Independent recovery reads bind the native scratch result to protected media."""
import hashlib
from pathlib import Path
import sys
import unittest

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
from sd_scratch_readback import ScratchReadback, SCRATCH_PATTERN_SHA256, TAIL_RAM
from test_nano_upload import Model


def sha(data):
    return hashlib.sha256(data).hexdigest()


PATTERN=bytes((i*37+(i//512)*17+0x5a)&255 for i in range(4096))


class ReadModel(Model):
    def __init__(self,kit,plan,fault=''):
        super().__init__(kit,b'')
        self.plan,self.read_fault=plan,fault
        self.filesize=0;self.tail_read=False;self.mbr=b'mbr'.ljust(512,b'\0')

    def command(self,c,timeout):
        fault=self.read_fault
        if c.startswith(('fatwrite','mmc write','mmc erase','saveenv','tftp','dhcp')):
            raise AssertionError('readback must never write media or use the network')
        if c=='echo FES_SCRATCH_SESSION=${fes_sd_scratch_session}':
            self.commands.append(c)
            return 'FES_SCRATCH_SESSION='+self.marker+'\r\n'
        if c.startswith('setenv fes_sd_scratch_session '):
            self.commands.append(c);self.marker=c.split()[2];return ''
        if c=='mmcinfo':
            self.commands.append(c)
            info=('Device: mmc0@10808000\r\nManufacturer ID: 27\r\nOEM: 5048\r\nName: SD04G \r\n'
                  'Bus Speed: 50000000\r\nRd Block Len: 512\r\nSD version 3.0\r\n'
                  'High Capacity: Yes\r\nCapacity: 3.7 GiB\r\nBus Width: 4-bit\r\n')
            return {'wrong-manufacturer':info.replace('ID: 27','ID: 28'),
                    'wrong-oem':info.replace('OEM: 5048','OEM: 5049'),
                    'wrong-product':info.replace('SD04G','SD08G'),
                    'wrong-capacity':info.replace('3.7 GiB','7.4 GiB'),
                    'duplicate-card':info+info}.get(fault,info)
        if c=='mmc part' and fault in ('extra-partition','wrong-partition'):
            self.commands.append(c)
            return '1 32768 262144\n2 400000 100000\n' if fault=='extra-partition' else '1 32768 7774200\n'
        if c.startswith('fatsize '):
            self.commands.append(c)
            name=c.split()[3]
            data=self.kit['fit'] if name=='u-boot.itb' else self.kit['manifest']
            self.filesize=len(data)+(fault=='file-size' or fault=='late-file-size' and self.tail_read)
            return ''
        if c=='echo FES_SCRATCH_FILESIZE=${filesize}':
            self.commands.append(c)
            line=f'FES_SCRATCH_FILESIZE={self.filesize:x}\r\n'
            return line+line if fault=='duplicate-size' else line
        if c.startswith('fatload'):
            # The actual command must bound even a corrupt FAT directory entry.
            self.assert_bounded_load(c)
            result=super().command(c,timeout)
            address=int(c.split()[3],16)
            if fault=='late-file-hash' and self.tail_read:self.memory[address]=b'changed'
            if fault=='short-file':result='0 bytes read'
            return result
        if c.startswith('mmc read'):
            self.commands.append(c)
            _,_,address,lba,count=c.split()
            address,lba,count=int(address,16),int(lba,16),int(count,16)
            if lba==0 and count==32768:
                self.memory[address]=None
            elif lba==0 and count==1:
                self.memory[address]=b'changed' if fault=='mbr' else self.mbr
            else:
                if (address,lba,count)!=(TAIL_RAM,self.plan['sectors']-8,8):
                    raise AssertionError('unexpected raw sector range')
                self.tail_read=True
                if fault=='read-timeout':raise TimeoutError('raw read interrupted')
                self.memory[address]=b'corrupt' if fault=='tail' else PATTERN
                if fault=='short-tail':return '7 blocks read: OK'
            return f'MMC read: dev # 0, block # {lba}, count {count} ... {count} blocks read: OK\r\n'
        if c.startswith('hash sha256'):
            self.commands.append(c)
            address,size=(int(n,16) for n in c.split()[2:])
            data=self.memory[address]
            digest=self.kit['prefix_sha256'] if data is None else sha(data)
            if fault=='prefix' and data is None:digest='0'*64
            last=address+size-1+(fault=='wrong-hash-range')
            line=f'sha256 for {address:08x} ... {last:08x} ==> {digest}\r\n'
            return line+line if fault=='duplicate-hash' else line
        if c=='bdinfo' and fault=='late-lmb' and self.tail_read:
            self.extra_regions=[[0x83f00000,0x83ffffff,'none']]
        return super().command(c,timeout)

    def assert_bounded_load(self,c):
        parts=c.split()
        data=self.kit['fit'] if parts[4]=='u-boot.itb' else self.kit['manifest']
        if len(parts)!=6 or int(parts[5],16)!=len(data):
            raise AssertionError('file read must have an explicit bound')


class ScratchRecoveryReadbackTests(unittest.TestCase):
    def setUp(self):
        self.kit={'fit':b'qualified fit','manifest':b'qualified manifest',
                  'fit_sha256':sha(b'qualified fit'),'prefix_sha256':sha(b'prefix')}
        self.plan={'format':1,'scope':'nano-sd-scratch-write-v1',
                   'cid':'275048534430344730da25e394012b','sectors':7774208,
                   'mbr_sha256':sha(b'mbr'.ljust(512,b'\0')),'original_sha256':sha(b'original')}

    def session(self,fault='',owns=lambda:True):
        model=ReadModel(self.kit,self.plan,fault)
        return model,ScratchReadback(model,self.kit,self.plan,owns=owns),{'commands':[]}

    def test_exact_eight_sectors_are_read_without_nonvolatile_writes(self):
        model,session,receipt=self.session();session.run(receipt)
        self.assertEqual(sha(PATTERN),SCRATCH_PATTERN_SHA256)
        self.assertEqual(receipt['status'],'verified')
        self.assertTrue(receipt['independent_tail_readback'])
        self.assertFalse(receipt['sd_write_attempted'])
        self.assertFalse(receipt['full_cid_reobserved'])
        for key in ('boot_files_unchanged','recovery_manifest_unchanged','mbr_unchanged','protected_prefix_unchanged'):
            self.assertTrue(receipt[key])
        self.assertEqual([o['stage'] for o in receipt['memory_observations']],
                         ['admit','before-tail','after-tail','complete'])
        self.assertEqual([c for c in model.commands if c.startswith('mmc read')],
                         ['mmc read 83000000 0 8000','mmc read 82800000 0 1',
                          'mmc read 82800000 769ff8 8',
                          'mmc read 83000000 0 8000','mmc read 82800000 0 1'])
        self.assertEqual(len(session.memory.owned),2)
        self.assertFalse(any(start==TAIL_RAM for start,_,_ in session.memory.owned))
        self.assertTrue(all(c['status']=='returned' for c in receipt['commands']))
        with self.assertRaisesRegex(ValueError,'retired'):session.run({'commands':[]})

    def test_protected_identity_failures_stop_before_tail_read(self):
        for fault in ('wrong-manufacturer','wrong-oem','wrong-product','wrong-capacity','duplicate-card',
                      'extra-partition','wrong-partition','mbr','prefix','file-size','duplicate-size',
                      'short-file','wrong-hash-range','duplicate-hash'):
            with self.subTest(fault=fault):
                model,session,receipt=self.session(fault)
                with self.assertRaises(ValueError):session.run(receipt)
                self.assertFalse(model.tail_read)
                self.assertEqual(receipt['status'],'failed')
                self.assertFalse(receipt['independent_tail_readback'])
                self.assertTrue(session.retired)

    def test_tail_and_post_read_failures_never_claim_complete_or_issue_recovery_commands(self):
        for fault in ('tail','short-tail','read-timeout','late-lmb','late-file-size','late-file-hash'):
            with self.subTest(fault=fault):
                model,session,receipt=self.session(fault)
                with self.assertRaises((ValueError,TimeoutError)):session.run(receipt)
                self.assertTrue(model.tail_read)
                self.assertEqual(receipt['status'],'failed')
                self.assertFalse(receipt['independent_tail_readback'])
                self.assertFalse(receipt['boot_files_unchanged'])
                before=list(model.commands)
                with self.assertRaisesRegex(ValueError,'retired'):session.run({'commands':[]})
                self.assertEqual(model.commands,before)
                if fault=='read-timeout':
                    self.assertEqual(receipt['commands'][-1]['status'],'started')

    def test_existing_or_lost_boot_marker_is_not_adopted(self):
        model,session,receipt=self.session();model.marker='foreign'
        with self.assertRaisesRegex(ValueError,'boot marker differs'):session.run(receipt)
        self.assertFalse(any(c.startswith('mmc') for c in model.commands))
        model,session,receipt=self.session();session.run(receipt)
        other=ScratchReadback(model,self.kit,self.plan,owns=lambda:True)
        with self.assertRaisesRegex(ValueError,'boot marker differs'):other.run({'commands':[]})

    def test_lost_claim_before_or_after_command_stops_without_another_command(self):
        for allowed in (0,1,13):
            count=0
            def owns():
                nonlocal count
                count+=1
                return count<=allowed
            with self.subTest(allowed=allowed):
                model,session,receipt=self.session(owns=owns)
                with self.assertRaisesRegex(ValueError,'claim changed'):session.run(receipt)
                self.assertEqual(len(model.commands),(allowed+1)//2)
                self.assertEqual(receipt['status'],'failed')
                self.assertFalse(receipt['independent_tail_readback'])

    def test_unowned_or_changed_memory_map_refuses_raw_read(self):
        for fault in ('reserved','relocation'):
            model,session,receipt=self.session()
            if fault=='reserved':model.extra_regions=[[TAIL_RAM,TAIL_RAM+4095,'none']]
            else:
                original=model.command
                def command(value,timeout):
                    return original(value,timeout).replace('0xbfef6000','0x83000000')
                model.command=command
            with self.subTest(fault=fault),self.assertRaises(ValueError):session.run(receipt)
            self.assertFalse(any(c.startswith('mmc read') for c in model.commands))

    def test_receipt_cannot_reuse_a_prior_success(self):
        for prior in ({'commands':[],'status':'verified','independent_tail_readback':True},
                      {'commands':[],'sd_write_attempted':True},{'commands':[{'old':True}]}):
            model,session,_=self.session()
            with self.assertRaisesRegex(ValueError,'fresh read-only'):session.run(prior)
            self.assertEqual(model.commands,[])
            self.assertTrue(session.retired)

    def test_captured_vendor_summary_line_binds_device_lba_request_and_completed_count(self):
        captured=(ROOT/'tests/fixtures/nano/recovery-mmc-read-prefix.txt').read_text()
        for change in (None,'wrong-device','wrong-lba','wrong-request','short-completion','duplicate-summary','bare-count'):
            with self.subTest(change=change):
                model,session,receipt=self.session();original=model.command
                def command(value,timeout):
                    result=original(value,timeout)
                    if value=='mmc read 83000000 0 8000':
                        actual=captured
                        if change=='wrong-device':actual=actual.replace('dev # 0','dev # 1')
                        if change=='wrong-lba':actual=actual.replace('block # 0','block # 1')
                        if change=='wrong-request':actual=actual.replace('count 32768','count 32767')
                        if change=='short-completion':actual=actual.replace('32768 blocks read','32767 blocks read')
                        if change=='duplicate-summary':actual=actual+actual
                        if change=='bare-count':actual='32768 blocks read: OK\r\n'
                        return actual
                    return result
                model.command=command
                if change is None:
                    session.run(receipt)
                    self.assertEqual(receipt['status'],'verified')
                else:
                    with self.assertRaisesRegex(ValueError,'raw SD read'):session.run(receipt)
                    self.assertFalse(model.tail_read)
                    self.assertFalse(receipt['independent_tail_readback'])


if __name__=='__main__':unittest.main()
