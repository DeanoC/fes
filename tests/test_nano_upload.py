"""Recovery uploads never overwrite boot files or accept uncertain transfers."""
import importlib.util
import json
from pathlib import Path
import shutil
import socket
import struct
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import upload
from upload_transport import Transfer


class Model:
    def __init__(self,kit,payload,fault=''):
        self.kit,self.payload,self.fault=kit,payload,fault
        self.writes=0;self.commands=[];self.memory={};self.boot_reads=0
        self.mac = {'mac-present':'02:fe:25:08:cb:01','mac-uppercase':'02:FE:25:08:CB:01',
                    'wrong-mac':'02:fe:25:08:cb:02'}.get(fault,'')
        self.phy_reads=0;self.marker="";self.allocations={};self.files={};self.extra_regions=[];self.map_reads=0
    def command(self,c,timeout):
        self.commands.append(c)
        if c=='version':
            return ('U-Boot 2025.01'+('-fes-nano-recovery-ksz9031-gd0f8813fd6bf' if self.fault=='old-loader' else upload.recovery.LOCALVERSION)+
                    ' (Jun 27 2025 - 01:40:35 +0000)socfpga_agilex5\r\n')
        if c=='echo FES_UPLOAD_SESSION=${fes_upload_session}':return 'FES_UPLOAD_SESSION='+self.marker+'\r\n'
        if c.startswith('setenv fes_upload_session '):self.marker=c.split()[2];return ''
        if c=='bdinfo':
            self.map_reads+=1
            rows=[[0,0xffffff,'no-map'],[0xbeaebff0,0xbfffffff,'no-overwrite']]
            scratch=[]
            for address,length in sorted(self.allocations.items()):
                end=address+length-1
                if scratch and scratch[-1][1]+1>=address:scratch[-1][1]=max(scratch[-1][1],end)
                else:scratch.append([address,end,'none'])
            rows+=scratch+self.extra_regions
            if self.fault=='reserved':rows.append([0x83f00000,0x83ffffff,'none'])
            rows.sort()
            lines=['-> start = 0x80000000','-> size = 0x40000000','relocaddr = 0xbfef6000',
                   'memory.count = 0x1','memory[0] [0x80000000-0xbfffffff], 0x40000000 bytes, flags: none',
                   'reserved.count = '+hex(len(rows))]
            lines.extend(f'reserved[{i}] [{a:#x}-{z:#x}], {z-a+1:#x} bytes, flags: {flags}' for i,(a,z,flags) in enumerate(rows))
            return '\n'.join(lines)
        if c=='mmcinfo':return 'SD version 2.0\nCapacity: 3.7 GiB'
        if c=='mmc part':return '1 32768 262144\n'
        if c.startswith('mmc read'):
            self.memory[0x83000000]=None;return '32768 blocks read: OK'
        if c.startswith('fatload'):
            addr=int(c.split()[3],16);name=c.split()[4]
            if name=='u-boot.itb':
                self.boot_reads+=1
                data=b'changed' if self.fault=='boot-change' and self.writes else self.kit['fit']
            elif name=='FES/RECOVERY.JSON':data=b'wrong-card' if self.fault=='wrong-card' or self.fault=='manifest-change' and self.writes else self.kit['manifest']
            else:data=b'corrupt' if self.fault=='readback' else self.files.get(name,self.payload)
            self.memory[addr]=data
            self.allocations[addr]=max(self.allocations.get(addr,0),len(data))
            if (self.fault=='oversize-readback' and name in upload.DESTINATIONS.values()) or (self.fault=='oversize-boot' and name=='u-boot.itb'):
                return f'1{len(data)} bytes read'
            return f'{len(data)} bytes read'
        if c.startswith('hash sha256'):
            addr=int(c.split()[2],16);data=self.memory[addr]
            result=self.kit['prefix_sha256'] if data is None else upload.sha(data)
            if (self.fault=='download-hash' and addr==0x82000000 and self.memory[addr]==self.payload) or self.fault=='prefix':result='0'*64
            return result
        if c=='dhcp':
            if self.fault=='renegotiation-timeout':raise ValueError('loader PHY startup timeout after pre-start link')
            return 'DHCP client bound to address 192.168.10.211 (3 ms)'
        if c=='echo FES_UPLOAD_ETHADDR=${ethaddr}':return 'FES_UPLOAD_ETHADDR='+self.mac+'\r\n'
        if c.startswith('setenv ethaddr '):
            if self.mac:raise ValueError('loader MAC is write-once')
            self.mac=c.split()[2];return ''
        if c=='net list':return 'absent' if self.fault=='ethernet-device' else 'eth0 : ethernet@10810000'
        if c=='fesphy':
            line='FES_RECOVERY_PHY PREPARED address=1 id=00221622 configured=1 reset=0'
            if self.fault=='prepare-timeout':raise TimeoutError('uncertain recovery PHY preparation')
            if self.fault=='prepare-reject':raise ValueError('recovery PHY preparation rejected')
            if self.fault=='prepare-missing':return ''
            if self.fault=='prepare-duplicate':return line+'\n'+line
            if self.fault=='prepare-wrong-id':return line.replace('00221622','00221621')
            if self.fault=='prepare-reset':return line.replace('reset=0','reset=1')
            return line
        if c=='mii read 1 2':return 'FFFF\r\n' if self.fault=='phy-id-missing' else '0022\r\n'
        if c=='mii read 1 3':return '1621\r\n' if self.fault=='wrong-phy' else '1622\r\n'
        if c=='mii read 1 1':
            self.phy_reads+=1
            if self.fault=='phy-unknown':return 'FFFF\r\n'
            if self.fault=='phy-malformed':return 'missing PHY\r\n'
            if self.fault=='link-timeout' or (self.fault=='link-transient' and self.phy_reads==2):return '7949\r\n'
            return '796D\r\n'
        if c.startswith('tftpboot'):
            if self.fault=='interrupted':
                self.allocations[0x82000000]=max(self.allocations.get(0x82000000,0),1)
                raise TimeoutError('download interrupted')
            self.memory[0x82000000]=self.payload
            self.allocations[0x82000000]=max(self.allocations.get(0x82000000,0),len(self.payload))
            return f'Bytes transferred = {len(self.payload)} (hex)'
        if c.startswith('fatwrite'):
            self.writes+=1
            self.files[c.split()[4]]=self.memory[0x82000000]
            if self.fault=='write-timeout':raise TimeoutError('SD write uncertain')
            return f'{len(self.payload)-(self.fault=="short-write")} bytes written'
        if c.startswith('cmp.b'):return f'Total of {len(self.payload)} byte(s) were the same'
        return ''


class NanoUploadTests(unittest.TestCase):
    def setUp(self):
        self.payload=b'experimental bytes'*127
        self.kit={'manifest':b'manifest','fit':b'bootfit','fit_sha256':upload.sha(b'bootfit'),'prefix_sha256':upload.sha(b'prefix')}
    def run_upload(self,fault='',purpose='boot'):
        model=Model(self.kit,self.payload,fault);receipt={'commands':[],'sd_write_attempted':False}
        self.last_model=model
        class Server:
            complete=fault!='lost-final-ack'
            thread=type('Thread',(),{'join':lambda self,timeout:None})()
            def __init__(self,*args):self.closed=False
            def start(self):pass
            def close(self):self.closed=True
        with patch('builtins.print'),patch.object(upload.time,'sleep'):
            session=upload.UploadSession(model,self.kit,'192.168.10.202','02:fe:25:08:cb:01',Server)
            self.last_session=session
            session.upload(self.payload,receipt,purpose=purpose)
        return model,receipt
    def test_success_changes_only_fixed_experimental_file(self):
        model,r=self.run_upload()
        self.assertEqual(r['status'],'verified');self.assertTrue(r['boot_files_unchanged'])
        self.assertEqual(model.writes,1);self.assertEqual(model.boot_reads,2)
        self.assertEqual(r['phy_id'],0x00221622)
        writes=[c for c in model.commands if c.startswith('fatwrite')]
        self.assertEqual(writes,[f'fatwrite mmc 0:1 82000000 FES/UPLOAD.BIN {len(self.payload):x}'])
        self.assertTrue(r['recovery_manifest_unchanged'])

    def test_captured_vendor_version_suffix_is_required_before_any_card_access(self):
        captured=(ROOT/'tests/fixtures/nano-recovery-version.txt').read_text()
        current=captured.replace('-fes-nano-recovery-ksz9031-gd0f8813fd6bf', upload.recovery.LOCALVERSION)
        responses=(current, captured, current.replace('socfpga_agilex5',''),
                   current.replace('socfpga_agilex5','socfpga_agilex5_other'),
                   current.replace('gd0f8813fd6bf','gd0f8813fd6bf-dirty'),
                   current+current, current.replace('U-Boot ', 'unexpected U-Boot '))
        for index,response in enumerate(responses):
            with self.subTest(response=response), patch('builtins.print'), patch.object(upload.time,'sleep'):
                model,session=self.session();original=model.command
                def command(value,timeout):
                    if value=='version':model.commands.append(value);return response
                    return original(value,timeout)
                model.command=command
                if index==0:
                    receipt={'commands':[]};session.upload(model.payload,receipt)
                    self.assertEqual(receipt['status'],'verified');self.assertEqual(model.writes,1)
                else:
                    with self.assertRaisesRegex(ValueError,'expected recovery loader'):
                        session.upload(model.payload,{'commands':[]})
                    self.assertEqual(model.commands,['version']);self.assertEqual(model.writes,0)
                    self.assertTrue(session.retired)
    def test_package_upload_uses_only_the_separate_fixed_file_and_preserves_boot(self):
        model,r=self.run_upload(purpose='package')
        self.assertEqual(r['destination'],'FES/PKGTEST.BIN')
        self.assertEqual(r['purpose'],'package')
        self.assertTrue(r['boot_files_unchanged'] and r['recovery_manifest_unchanged'])
        writes=[c for c in model.commands if c.startswith('fatwrite')]
        self.assertEqual(writes,[f'fatwrite mmc 0:1 82000000 FES/PKGTEST.BIN {len(self.payload):x}'])
        for fault in ('short-write','readback','boot-change','write-timeout','oversize-readback','manifest-change'):
            with self.subTest(fault=fault),self.assertRaises((ValueError,TimeoutError)):
                self.run_upload(fault,purpose='package')
            self.assertEqual(self.last_model.writes,1)
    def test_arbitrary_upload_destinations_reject_before_console_access(self):
        for purpose in ('FES/RECOVERY.JSON','../u-boot.itb','package; boot','',None):
            model=Model(self.kit,self.payload)
            with self.subTest(purpose=purpose),self.assertRaises(ValueError):
                upload.UploadSession(model,self.kit,'192.168.10.202','02:fe:25:08:cb:01').upload(self.payload,{},purpose=purpose)
            self.assertEqual(model.commands,[])
    def test_identity_and_transfer_failures_never_write_sd(self):
        for fault in ('wrong-card','prefix','download-hash','interrupted','lost-final-ack','reserved','oversize-boot',
                      'wrong-mac','ethernet-device','phy-unknown','phy-malformed','wrong-phy','phy-id-missing','old-loader'):
            with self.subTest(fault=fault):
                model=Model(self.kit,self.payload,fault);r={'commands':[],'sd_write_attempted':False}
                class Server:
                    complete=fault!='lost-final-ack'
                    thread=type('Thread',(),{'join':lambda self,timeout:None})()
                    def __init__(self,*args):pass
                    def start(self):pass
                    def close(self):pass
                with patch('builtins.print'),self.assertRaises((ValueError,TimeoutError)):
                    upload.UploadSession(model,self.kit,'192.168.10.202','02:fe:25:08:cb:01',Server).upload(self.payload,r)
                self.assertEqual(model.writes,0);self.assertFalse(r['sd_write_attempted'])
    def test_positive_prestart_status_cannot_bypass_failed_phy_startup(self):
        with self.assertRaises(ValueError):self.run_upload('renegotiation-timeout')
        model=self.last_model
        self.assertGreaterEqual(model.phy_reads,2)
        self.assertEqual(model.commands.count('dhcp'),1)
        self.assertFalse(any(c.startswith(('tftpboot','fatwrite')) for c in model.commands))

    def session(self, model=None):
        class Server:
            complete=True
            thread=type('Thread',(),{'join':lambda self,timeout:None})()
            def __init__(self,*args):pass
            def start(self):pass
            def close(self):pass
        model=model or Model(self.kit,self.payload)
        return model,upload.UploadSession(model,self.kit,'192.168.10.202','02:fe:25:08:cb:01',Server)

    def test_actual_hardware_map_matches_completed_upload_and_allows_owned_package_reuse(self):
        from upload_memory import memory_map
        fixture=json.loads((ROOT/'tests/fixtures/nano_upload_lmb.json').read_text())
        self.kit=dict(self.kit,manifest=b'm'*fixture['manifest_bytes'],fit=b'f'*fixture['protected_fit_bytes'])
        self.kit['fit_sha256']=upload.sha(self.kit['fit'])
        self.payload=b'x'*fixture['firmware_bytes']
        model,session=self.session()
        self.assertEqual(memory_map(model.command('bdinfo',15)),memory_map(fixture['initial']))
        with patch('builtins.print'):
            first={'commands':[]};session.upload(self.payload,first)
            actual=memory_map(model.command('bdinfo',15))
            self.assertEqual(actual,memory_map(fixture['after_first_upload']))
            package=b'p'*1649664;model.payload=package
            second={'commands':[]};session.upload(package,second,purpose='package')
        self.assertEqual(model.writes,2)
        self.assertEqual(model.files['FES/UPLOAD.BIN'],self.payload)
        self.assertEqual(model.files['FES/PKGTEST.BIN'],package)
        self.assertEqual(first['upload_session'],second['upload_session'])
        self.assertEqual([r['stage'] for r in second['memory_observations']],['admit','before-sd-write','complete'])
        self.assertEqual(memory_map(model.command('bdinfo',15)),actual)
        self.assertFalse(session.retired)
        self.assertEqual(sum(c.startswith('setenv fes_upload_session ') for c in model.commands),1)

    def test_growing_and_shrinking_payloads_preserve_owned_maximum_and_fixed_destinations(self):
        model,session=self.session()
        with patch('builtins.print'):
            for size,purpose in ((1,'boot'),(upload.LIMIT,'package'),(17,'boot')):
                model.payload=b'x'*size
                receipt={'commands':[]};session.upload(model.payload,receipt,purpose=purpose)
                self.assertEqual(receipt['status'],'verified')
                self.assertTrue(receipt['sd_readback_verified'] and receipt['boot_files_unchanged'])
        self.assertEqual(model.writes,3)
        self.assertEqual(session.memory.owned,[[0x82000000,0x83000007,'none']])

    def test_marker_loss_or_reset_retires_session_before_next_transfer(self):
        for reset in (False,True):
            with self.subTest(reset=reset),patch('builtins.print'):
                model,session=self.session()
                session.upload(model.payload,{'commands':[]})
                marker=model.marker;model.marker=''
                if reset:model.allocations.clear()
                begin=len(model.commands)
                with self.assertRaisesRegex(ValueError,'marker'):
                    session.upload(model.payload,{'commands':[]},purpose='package')
                self.assertFalse(any(c.startswith(('dhcp','tftpboot','fatwrite')) for c in model.commands[begin:]))
                model.marker=marker;count=len(model.commands)
                with self.assertRaisesRegex(ValueError,'retired'):session.upload(model.payload,{'commands':[]})
                self.assertEqual(len(model.commands),count);self.assertEqual(model.writes,1)

    def test_new_session_never_adopts_old_flags_none_reservations(self):
        with patch('builtins.print'):
            model,session=self.session();session.upload(model.payload,{'commands':[]});session.close()
            for clear_marker in (False,True):
                with self.subTest(clear_marker=clear_marker):
                    if clear_marker:model.marker=''
                    _,new=self.session(model);begin=len(model.commands)
                    with self.assertRaises(ValueError):new.upload(model.payload,{'commands':[]},purpose='package')
                    self.assertTrue(new.retired)
                    self.assertFalse(any(c.startswith(('fatload','mmc read','dhcp','tftpboot','fatwrite')) for c in model.commands[begin:]))
            self.assertEqual(model.writes,1)

    def test_changed_inventory_or_receipt_mutation_never_grants_foreign_ownership(self):
        for fault in ('foreign','protected','lost-allocation','caller-mutation'):
            with self.subTest(fault=fault),patch('builtins.print'):
                model,session=self.session();receipt={'commands':[]};session.upload(model.payload,receipt)
                if fault=='lost-allocation':model.allocations.pop(0x82800000)
                else:
                    flags='no-overwrite' if fault=='protected' else 'none'
                    model.extra_regions.append([0x84000000,0x8400000f,flags])
                    if fault=='caller-mutation':
                        receipt['memory_observations'][0]['map']['reserved'].append([0x84000000,0x8400000f,'none'])
                begin=len(model.commands)
                with self.assertRaisesRegex(ValueError,'LMB'):session.upload(model.payload,{'commands':[]},purpose='package')
                self.assertEqual(model.writes,1);self.assertTrue(session.retired)
                self.assertFalse(any(c.startswith(('fatload','dhcp','tftpboot','fatwrite')) for c in model.commands[begin:]))

    def test_unexpected_reservation_after_download_stops_before_sd_write(self):
        for flags in ('none','no-overwrite'):
            with self.subTest(flags=flags),patch('builtins.print'):
                model,session=self.session();original=model.command
                def changed(command,timeout):
                    result=original(command,timeout)
                    if command.startswith('tftpboot'):model.extra_regions.append([0x83f00000,0x83f0000f,flags])
                    return result
                model.command=changed;receipt={'commands':[]}
                with self.assertRaisesRegex(ValueError,'LMB'):session.upload(model.payload,receipt)
                self.assertTrue(receipt['download_verified'])
                self.assertFalse(receipt['sd_write_attempted']);self.assertEqual(model.writes,0)
                self.assertEqual(receipt['status'],'failed');self.assertTrue(session.retired)

    def test_write_failure_and_close_prevent_replay_through_same_session(self):
        for fault in ('write-timeout','interrupted',''):
            with self.subTest(fault=fault),patch('builtins.print'):
                model,session=self.session();model.fault=fault
                if fault:
                    with self.assertRaises(TimeoutError):session.upload(model.payload,{'commands':[]})
                else:
                    session.upload(model.payload,{'commands':[]});session.close()
                count=len(model.commands);writes=model.writes
                model.fault=''
                with self.assertRaisesRegex(ValueError,'retired'):session.upload(model.payload,{'commands':[]})
                self.assertEqual(len(model.commands),count);self.assertEqual(model.writes,writes)

    def test_verified_receipt_cannot_be_reused_for_another_upload(self):
        with patch('builtins.print'):
            model,session=self.session();receipt={'commands':[]};session.upload(model.payload,receipt)
            count=len(model.commands)
            with self.assertRaisesRegex(ValueError,'fresh'):session.upload(model.payload,receipt)
            self.assertEqual(len(model.commands),count);self.assertEqual(model.writes,1)

    def test_read_only_firmware_verification_keeps_owned_ledger_for_later_uploads(self):
        model,session=self.session()
        boot=b'a'*4096;package=b'b'*2048
        with patch('builtins.print'):
            model.payload=boot;session.upload(boot,{'commands':[]})
            model.payload=package;session.upload(package,{'commands':[]},purpose='package')
            fresh={'commands':[]};session.verify(boot,fresh)
            self.assertEqual(fresh['status'],'verified');self.assertTrue(fresh['read_only'] and fresh['fresh_readback'])
            self.assertFalse(fresh['sd_write_attempted']);self.assertEqual(model.writes,2)
            self.assertEqual(sum(c['command'].startswith('fatload') for c in fresh['commands']),1)
            self.assertFalse(any(c['command'].startswith(('dhcp','tftpboot','fatwrite','mmc write')) for c in fresh['commands']))
            again={'commands':[]};session.upload(package,again,purpose='package')
        self.assertEqual(model.writes,3);self.assertEqual(again['status'],'verified')
        self.assertEqual(fresh['upload_session'],again['upload_session'])

    def test_fresh_read_rejects_unknown_or_stale_file_identity_without_io(self):
        for wrong in ('not-uploaded','wrong-size','wrong-hash','overwritten'):
            with self.subTest(wrong=wrong),patch('builtins.print'):
                model,session=self.session();expected=model.payload
                if wrong!='not-uploaded':session.upload(expected,{'commands':[]})
                if wrong=='wrong-size':expected+=b'x'
                if wrong=='wrong-hash':expected=b'z'*len(expected)
                if wrong=='overwritten':
                    model.payload=b'q'*len(expected);session.upload(model.payload,{'commands':[]})
                count=len(model.commands)
                with self.assertRaisesRegex(ValueError,'not verified'):session.verify(expected,{'commands':[]})
                self.assertTrue(session.retired);self.assertEqual(len(model.commands),count)

    def test_corrupt_fresh_read_stops_and_retires_without_writing(self):
        for fault in ('length','hash','marker','memory'):
            with self.subTest(fault=fault),patch('builtins.print'):
                model,session=self.session();session.upload(model.payload,{'commands':[]})
                original=model.command
                def corrupt(command,timeout):
                    output=original(command,timeout)
                    if command.startswith('fatload') and command.endswith('FES/UPLOAD.BIN'):
                        if fault=='length':return '1 bytes read'
                        if fault=='hash':model.memory[0x83000000]=b'wrong content'
                        if fault=='marker':model.marker=''
                        if fault=='memory':model.extra_regions.append([0x84000000,0x8400000f,'none'])
                    return output
                model.command=corrupt;receipt={'commands':[]}
                with self.assertRaises(ValueError):session.verify(model.payload,receipt)
                self.assertFalse(receipt['fresh_readback']);self.assertFalse(receipt['sd_write_attempted'])
                self.assertEqual(receipt['status'],'failed');self.assertTrue(session.retired);self.assertEqual(model.writes,1)

    def test_concurrent_operation_and_close_reject_without_interleaving_commands(self):
        model,session=self.session()
        entered=threading.Event();release=threading.Event();errors=[]
        original=model.command
        def paused(command,timeout):
            if command=='version':
                entered.set()
                if not release.wait(3):raise TimeoutError('test did not release upload')
            return original(command,timeout)
        model.command=paused
        def worker():
            try:session.upload(model.payload,{'commands':[]})
            except BaseException as error:errors.append(error)
        with patch('builtins.print'):
            thread=threading.Thread(target=worker,daemon=True);thread.start()
            try:
                self.assertTrue(entered.wait(2))
                count=len(model.commands)
                with self.assertRaisesRegex(ValueError,'active'):session.upload(model.payload,{'commands':[]})
                with self.assertRaisesRegex(ValueError,'active'):session.close()
                self.assertEqual(len(model.commands),count)
            finally:
                release.set();thread.join(5)
        self.assertFalse(thread.is_alive());self.assertEqual(errors,[]);self.assertEqual(model.writes,1)

    @unittest.skipUnless(all(shutil.which(n) for n in ('mformat','mmd','mcopy')),'mtools required')
    def test_recovery_kit_binds_the_actual_phy_profile_and_rejects_old_metadata(self):
        import recovery_package
        with tempfile.TemporaryDirectory() as temp:
            base=Path(temp);source=base/'input.itb';source.write_bytes(b'synthetic recovery FIT')
            root=base/'kit';root.mkdir();(root/'u-boot.itb').write_bytes(source.read_bytes())
            manifest={'format':1,'fit_sha256':upload.sha(source.read_bytes()),'spl_sha256':'a'*64,
                      'bl31_sha256':'b'*64,'sof_sha256':'c'*64,'fat32_start_sector':32768,'fat32_sectors':262144,
                      'native_handoff':False,'automatic_writes':False,'spl_boot_mode':'fat','spl_payload_path':'u-boot.itb',
                      'spl_fat_partition':1,'recovery_phy':dict(upload.recovery.PHY),'phy_driver_linked':True}
            path=root/'RECOVERY.JSON';path.write_text(json.dumps(manifest))
            tools={n:Path(shutil.which(n)).absolute() for n in ('mformat','mmd','mcopy')}
            def run(label,argv):
                import subprocess
                subprocess.run([str(a) for a in argv],check=True,capture_output=True,timeout=20)
            disk=recovery_package.make_card(root,source,tools,run)
            with disk.open('rb') as f:digest=__import__('hashlib').file_digest(f,'sha256').hexdigest()
            record=dict(manifest,status='prepared',sd_image_size=disk.stat().st_size,sd_image_sha256=digest)
            receipt=root/'result.json';receipt.write_text(json.dumps(record))
            self.assertEqual(upload.load_kit(root)['fit'],source.read_bytes())
            for field,value in (('recovery_phy',None),('recovery_phy',dict(upload.recovery.PHY,id=0x001cc916)),
                                ('phy_driver_linked',False)):
                receipt.write_text(json.dumps(dict(record,**{field:value})))
                with self.subTest(field=field,value=value),self.assertRaises(ValueError):upload.load_kit(root)
            receipt.write_text(json.dumps(record));wrong=dict(manifest,recovery_phy=dict(upload.recovery.PHY,address=0))
            path.write_text(json.dumps(wrong))
            with self.assertRaises(ValueError):upload.load_kit(root)
    def test_write_readback_and_boot_change_failures_are_not_replayed(self):
        for fault in ('short-write','readback','boot-change','write-timeout','oversize-readback','manifest-change'):
            with self.subTest(fault=fault):
                with self.assertRaises((ValueError,TimeoutError)):
                    self.run_upload(fault)
                self.assertEqual(self.last_model.writes,1)
    def test_matching_write_once_mac_is_reused_and_other_mac_never_overwritten(self):
        for fault in ('mac-present','mac-uppercase'):
            model,r=self.run_upload(fault)
            self.assertEqual(r['status'],'verified')
            self.assertFalse(any(c.startswith('setenv ethaddr ') for c in model.commands))
        model,r=self.run_upload()
        self.assertEqual(sum(c.startswith('setenv ethaddr ') for c in model.commands),1)
    def test_preparation_and_two_consecutive_link_reads_precede_dhcp(self):
        for fault,reads in (('',2),('link-transient',4)):
            model,r=self.run_upload(fault)
            self.assertTrue(r['phy_link_verified']);self.assertEqual(model.phy_reads,reads)
            self.assertTrue(r['phy_prepared_before_link'])
            self.assertLess(model.commands.index('net list'),model.commands.index('fesphy'))
            self.assertLess(model.commands.index('fesphy'),model.commands.index('mii read 1 1'))
            self.assertLess(max(i for i,c in enumerate(model.commands) if c=='mii read 1 1'),model.commands.index('dhcp'))
    def test_uncertain_or_changed_preparation_never_samples_link_downloads_or_writes(self):
        for fault in ('prepare-timeout','prepare-reject','prepare-missing','prepare-duplicate','prepare-wrong-id','prepare-reset'):
            with self.subTest(fault=fault),self.assertRaises((ValueError,TimeoutError)):self.run_upload(fault)
            model=self.last_model
            self.assertNotIn('dhcp',model.commands);self.assertNotIn('mii read 1 1',model.commands)
            self.assertFalse(any(c.startswith('tftpboot') for c in model.commands));self.assertEqual(model.writes,0)
    def test_phy_deadline_never_downloads_or_writes(self):
        tick=iter(range(0,100,5))
        with patch.object(upload.time,'monotonic',side_effect=lambda:next(tick)),self.assertRaisesRegex(TimeoutError,'PHY'):
            self.run_upload('link-timeout')
        self.assertNotIn('dhcp',self.last_model.commands)
        self.assertFalse(any(c.startswith('tftpboot') for c in self.last_model.commands))
        self.assertEqual(self.last_model.writes,0)
    def test_size_and_mac_rejected_before_console_commands(self):
        model=Model(self.kit,self.payload)
        for data,mac in ((b'','02:fe:25:08:cb:01'),(b'x'*(upload.LIMIT+1),'02:fe:25:08:cb:01'),(b'x','ff:ff:ff:ff:ff:ff')):
            with self.assertRaises(ValueError):upload.UploadSession(model,self.kit,'192.168.10.202',mac).upload(data,{'commands':[]})
        self.assertEqual(model.commands,[])
    def test_claim_refuses_other_owner_and_preserves_uncertain_claim(self):
        with tempfile.TemporaryDirectory() as temp:
            p=Path(temp)/'de25-TRWU8CBL-STATUS.md';p.write_text('## Active claim\n- Someone else\n')
            claim=upload.Claim(p)
            with self.assertRaises(ValueError):claim.start()
            self.assertEqual(p.read_text(),'## Active claim\n- Someone else\n')
            p.write_text('## Current status — released\n')
            claim.start();claim.finish(False,'failed')
            with self.assertRaises(ValueError):upload.Claim(p).start()
            self.assertIn('Claim held; recovery needed',p.read_text())
    def test_lost_claim_after_download_never_starts_sd_write(self):
        model=Model(self.kit,self.payload);receipt={'commands':[],'sd_write_attempted':False}
        class Server:
            complete=True
            thread=type('Thread',(),{'join':lambda self,timeout:None})()
            def __init__(self,*args):pass
            def start(self):pass
            def close(self):pass
        owns=lambda:not any(c.startswith('tftpboot') for c in model.commands)
        with patch('builtins.print'),self.assertRaisesRegex(ValueError,'claim ownership changed'):
            upload.UploadSession(model,self.kit,'192.168.10.202','02:fe:25:08:cb:01',Server,owns).upload(self.payload,receipt)
        self.assertFalse(receipt['sd_write_attempted']);self.assertEqual(model.writes,0)
    def test_default_cli_plan_never_claims_or_opens_a_console(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);(root/'out').mkdir();payload=root/'payload';payload.write_bytes(self.payload)
            args=['upload','--payload',str(payload),'--recovery-kit',str(root),'--host-ip','192.168.10.202','--output',str(root/'out/plan')]
            with patch.object(upload,'ROOT',root),patch.object(upload,'load_kit',return_value={'image_sha256':'a'*64}),\
                    patch.object(upload,'Console',side_effect=AssertionError('opened serial')),\
                    patch.object(upload,'Claim',side_effect=AssertionError('claimed kit')),\
                    patch.object(sys,'argv',args),patch('builtins.print'):
                upload.main()
            r=json.loads((root/'out/plan/result.json').read_text())
            self.assertEqual(r['status'],'planned');self.assertEqual(r['commands'],[])
            self.assertFalse(r['activation']);self.assertFalse(r['sd_write_attempted'])
    def test_actual_tftp_ignores_writes_and_wrong_names_and_sends_final_empty_block(self):
        payload=bytes(range(256))*4
        server=Transfer('127.0.0.1','127.0.0.1','EXPECTED.BIN',payload,port=0,timeout=5);server.start()
        try:
            with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as c:
                c.bind(('127.0.0.1',0));c.settimeout(2)
                c.sendto(b'\0\2EXPECTED.BIN\0octet\0',('127.0.0.1',server.port))
                c.sendto(b'\0\1WRONG.BIN\0octet\0',('127.0.0.1',server.port))
                c.sendto(b'\0\1EXPECTED.BIN\0octet\0',('127.0.0.1',server.port))
                data=bytearray()
                for block in (1,2,3):
                    packet,peer=c.recvfrom(2048);self.assertEqual(packet[:4],struct.pack('!HH',3,block))
                    data.extend(packet[4:]);c.sendto(struct.pack('!HH',4,block),peer)
                self.assertEqual(len(packet),4);self.assertEqual(bytes(data),payload)
            server.thread.join(2);self.assertTrue(server.complete)
        finally:server.close()
    def test_tftp_negotiation_and_cancelled_transfer_does_not_report_complete(self):
        for finish in (False,True):
            payload=b'x'*1800
            server=Transfer('127.0.0.1','127.0.0.1','EXPECTED.BIN',payload,port=0,timeout=5);server.start()
            try:
                with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as c:
                    c.bind(('127.0.0.1',0));c.settimeout(2)
                    c.sendto(b'\0\1EXPECTED.BIN\0octet\0blksize\0001468\0',('127.0.0.1',server.port))
                    packet,peer=c.recvfrom(2048);self.assertEqual(packet,b'\0\6blksize\0001468\0')
                    c.sendto(struct.pack('!HH',4,0),peer)
                    packet,_=c.recvfrom(2048);self.assertEqual(packet[:4],struct.pack('!HH',3,1))
                    if finish:
                        c.sendto(struct.pack('!HH',4,1),peer)
                        packet,_=c.recvfrom(2048);self.assertEqual(packet[4:],payload[1468:])
                        c.sendto(struct.pack('!HH',4,2),peer)
                        server.thread.join(2);self.assertTrue(server.complete)
            finally:server.close()
            self.assertEqual(server.complete,finish)


class NanoUploadMemoryTests(unittest.TestCase):
    def test_actual_map_parser_rejects_incomplete_ambiguous_and_changed_metadata(self):
        from upload_memory import memory_map
        fixture=json.loads((ROOT/'tests/fixtures/nano_upload_lmb.json').read_text())
        initial=fixture['initial'];after=fixture['after_first_upload']
        self.assertEqual(len(memory_map(initial)['reserved']),2)
        self.assertEqual(len(memory_map(after)['reserved']),5)
        changes=[
            initial.replace('reserved.count = 0x2','reserved.count = 0x3'),
            initial.replace('reserved[1]','reserved[0]'),
            initial.replace('0x1000000 bytes','0x1000001 bytes'),
            initial.replace('flags: no-map','flags: invalid 0x1'),
            initial.replace('flags: no-map','flags: none, no-map'),
            initial.replace('reserved[1]','reserved[01]')+'\nreserved[2] malformed\n',
            initial.replace('-> size     = 0x0000000040000000','-> size     = 0x0000000080000000'),
            initial.replace('memory.count = 0x1','memory.count = 0x0'),
            initial.replace('no-overwrite','no-overwrite, no-overwrite'),
            after.replace('0x8264cf43','0x819fffff'),
            initial+'\nreserved.count = 0x2\n',
            initial+'\n-> start = 0x90000000\n-> size = malformed\n',
        ]
        for value in changes:
            with self.subTest(value=value[-120:]),self.assertRaises(ValueError):memory_map(value)

    def test_flags_none_alone_never_confers_initial_ownership(self):
        from upload_memory import ScratchMemory
        fixture=json.loads((ROOT/'tests/fixtures/nano_upload_lmb.json').read_text())
        with self.assertRaisesRegex(ValueError,'unowned'):ScratchMemory().observe(fixture['after_first_upload'])


if __name__=='__main__':unittest.main()
