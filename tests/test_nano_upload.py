"""Recovery uploads never overwrite boot files or accept uncertain transfers."""
import importlib.util
import json
from pathlib import Path
import socket
import struct
import sys
import tempfile
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
        self.phy_reads=0
    def command(self,c,timeout):
        self.commands.append(c)
        if c=='version':return 'U-Boot 2025.01-fes-nano-recovery-gd0f8813fd6bf'
        if c=='bdinfo':
            reserved='0x82000000-0x82100000' if self.fault=='reserved' else '0xbfa00000-0xbfffffff'
            return '-> start = 0x80000000\n-> size = 0x40000000\nrelocaddr = 0xbff00000\nreserved.count = 0x1\nreserved[0] ['+reserved+']'
        if c=='mmcinfo':return 'SD version 2.0\nCapacity: 3.7 GiB'
        if c=='mmc part':return '1 32768 262144\n'
        if c.startswith('mmc read'):
            self.memory[0x83000000]=None;return '32768 blocks read: OK'
        if c.startswith('fatload'):
            addr=int(c.split()[3],16);name=c.split()[4]
            if name=='u-boot.itb':
                self.boot_reads+=1
                data=b'changed' if self.fault=='boot-change' and self.writes else self.kit['fit']
            elif name=='FES/RECOVERY.JSON':data=b'wrong-card' if self.fault=='wrong-card' else self.kit['manifest']
            else:data=b'corrupt' if self.fault=='readback' else self.payload
            self.memory[addr]=data
            if (self.fault=='oversize-readback' and name=='FES/UPLOAD.BIN') or (self.fault=='oversize-boot' and name=='u-boot.itb'):
                return f'1{len(data)} bytes read'
            return f'{len(data)} bytes read'
        if c.startswith('hash sha256'):
            addr=int(c.split()[2],16);data=self.memory[addr]
            result=self.kit['prefix_sha256'] if data is None else upload.sha(data)
            if (self.fault=='download-hash' and addr==0x82000000 and self.memory[addr]==self.payload) or self.fault=='prefix':result='0'*64
            return result
        if c=='dhcp':return 'DHCP client bound to address 192.168.10.211 (3 ms)'
        if c=='echo FES_UPLOAD_ETHADDR=${ethaddr}':return 'FES_UPLOAD_ETHADDR='+self.mac+'\r\n'
        if c.startswith('setenv ethaddr '):
            if self.mac:raise ValueError('loader MAC is write-once')
            self.mac=c.split()[2];return ''
        if c=='net list':return 'absent' if self.fault=='ethernet-device' else 'eth0 : ethernet@10810000'
        if c=='mii read 1 1':
            self.phy_reads+=1
            if self.fault=='phy-unknown':return 'FFFF\r\n'
            if self.fault=='phy-malformed':return 'missing PHY\r\n'
            if self.fault=='link-timeout' or (self.fault=='link-transient' and self.phy_reads==2):return '7949\r\n'
            return '796D\r\n'
        if c.startswith('tftpboot'):
            if self.fault=='interrupted':raise TimeoutError('download interrupted')
            self.memory[0x82000000]=self.payload
            return f'Bytes transferred = {len(self.payload)} (hex)'
        if c.startswith('fatwrite'):
            self.writes+=1
            if self.fault=='write-timeout':raise TimeoutError('SD write uncertain')
            return f'{len(self.payload)-(self.fault=="short-write")} bytes written'
        if c.startswith('cmp.b'):return f'Total of {len(self.payload)} byte(s) were the same'
        return ''


class NanoUploadTests(unittest.TestCase):
    def setUp(self):
        self.payload=b'experimental bytes'*127
        self.kit={'manifest':b'manifest','fit':b'bootfit','fit_sha256':upload.sha(b'bootfit'),'prefix_sha256':upload.sha(b'prefix')}
    def run_upload(self,fault=''):
        model=Model(self.kit,self.payload,fault);receipt={'commands':[],'sd_write_attempted':False}
        self.last_model=model
        class Server:
            complete=fault!='lost-final-ack'
            thread=type('Thread',(),{'join':lambda self,timeout:None})()
            def __init__(self,*args):self.closed=False
            def start(self):pass
            def close(self):self.closed=True
        with patch('builtins.print'),patch.object(upload.time,'sleep'):
            upload.perform(model,self.kit,self.payload,'192.168.10.202','02:fe:25:08:cb:01',receipt,Server)
        return model,receipt
    def test_success_changes_only_fixed_experimental_file(self):
        model,r=self.run_upload()
        self.assertEqual(r['status'],'verified');self.assertTrue(r['boot_files_unchanged'])
        self.assertEqual(model.writes,1);self.assertEqual(model.boot_reads,2)
        writes=[c for c in model.commands if c.startswith('fatwrite')]
        self.assertEqual(writes,[f'fatwrite mmc 0:1 82000000 FES/UPLOAD.BIN {len(self.payload):x}'])
    def test_identity_and_transfer_failures_never_write_sd(self):
        for fault in ('wrong-card','prefix','download-hash','interrupted','lost-final-ack','reserved','oversize-boot',
                      'wrong-mac','ethernet-device','phy-unknown','phy-malformed'):
            with self.subTest(fault=fault):
                model=Model(self.kit,self.payload,fault);r={'commands':[],'sd_write_attempted':False}
                class Server:
                    complete=fault!='lost-final-ack'
                    thread=type('Thread',(),{'join':lambda self,timeout:None})()
                    def __init__(self,*args):pass
                    def start(self):pass
                    def close(self):pass
                with patch('builtins.print'),self.assertRaises((ValueError,TimeoutError)):
                    upload.perform(model,self.kit,self.payload,'192.168.10.202','02:fe:25:08:cb:01',r,Server)
                self.assertEqual(model.writes,0);self.assertFalse(r['sd_write_attempted'])
    def test_write_readback_and_boot_change_failures_are_not_replayed(self):
        for fault in ('short-write','readback','boot-change','write-timeout','oversize-readback'):
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
    def test_ethernet_probe_and_two_consecutive_link_reads_precede_dhcp(self):
        for fault,reads in (('',2),('link-transient',4)):
            model,r=self.run_upload(fault)
            self.assertTrue(r['phy_link_verified']);self.assertEqual(model.phy_reads,reads)
            self.assertLess(model.commands.index('net list'),model.commands.index('mii read 1 1'))
            self.assertLess(max(i for i,c in enumerate(model.commands) if c=='mii read 1 1'),model.commands.index('dhcp'))
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
            with self.assertRaises(ValueError):upload.perform(model,self.kit,data,'192.168.10.202',mac,{'commands':[]})
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
            upload.perform(model,self.kit,self.payload,'192.168.10.202','02:fe:25:08:cb:01',receipt,Server,owns)
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


if __name__=='__main__':unittest.main()
