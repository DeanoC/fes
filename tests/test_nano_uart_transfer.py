"""Replay the captured successful-sender/queued-CRLF race in the actual relay."""
import hashlib
import io
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import uart_transfer

class Stream(io.BytesIO):
    def __init__(self,fd,data=b''):
        super().__init__(data);self.fd=fd
    def fileno(self):return self.fd

class RecordedSender:
    """Captured exit point and real bytes; simulate a short UART write."""
    def __init__(self,body,prefix,status=0,already_exited=False,stalled=False):
        self.body,self.prefix,self.status=body,prefix,status
        self.already_exited,self.stalled=already_exited,stalled
        self.stdin,self.stdout=Stream(11),Stream(12,body)
        self.written=bytearray();self.chunks=[];self.ticks=0
    def poll(self):
        if self.stalled:return None
        return self.status if self.already_exited or len(self.written)>=self.prefix else None
    def select(self,read,write,errors,timeout):
        self.ticks+=1
        return ([12] if 12 in read and not self.stalled else [],
                [10] if 10 in write and not self.stalled else [],[])
    def read(self,fd,size):
        if fd!=12:raise AssertionError('unexpected target read')
        return self.stdout.read(size)
    def write(self,fd,data):
        if fd!=10:raise AssertionError('must not write to exited sender')
        count=min(len(data),self.prefix) if not self.written else len(data)
        self.written.extend(data[:count]);self.chunks.append(bytes(data[:count]));return count

class NanoUARTTransferTests(unittest.TestCase):
    def setUp(self):
        fixture=ROOT/'tests/fixtures/nano-kermit-exit'
        self.body=(fixture/'sender-stdout.bin').read_bytes()
        self.observation=json.loads((fixture/'observation.json').read_text())
        self.assertEqual(hashlib.sha256(self.body).hexdigest(),self.observation['sender_stdout_sha256'])
        self.assertEqual(self.body[-2:],bytes.fromhex(self.observation['queued_hex']))
        self.assertEqual(len(self.body),self.observation['generated_bytes'])
        packets=(fixture/'target-packets.bin').read_bytes()
        self.assertEqual(hashlib.sha256(packets).hexdigest(),self.observation['target_packets_sha256'])
        self.assertEqual(packets[-6:],b'\x01#$YB\r')
    def exercise(self,sender,*,owned=lambda:True,limit=None):
        self.state={};self.outgoing=io.BytesIO();self.incoming=io.BytesIO();self.generated=io.BytesIO()
        def check():
            if not owned():raise ValueError('claim lost')
            if sender.ticks>=5:raise TimeoutError('unchanged relay deadline')
        with patch.object(uart_transfer.select,'select',side_effect=sender.select),\
             patch.object(uart_transfer.os,'read',side_effect=sender.read),\
             patch.object(uart_transfer.os,'write',side_effect=sender.write):
            return uart_transfer._relay(sender,10,limit or len(self.body),check,
                                        self.outgoing,self.incoming,self.generated,self.state)
    def test_successful_sender_exit_drains_captured_queued_crlf(self):
        sender=RecordedSender(self.body,self.observation['transmitted_bytes'])
        self.exercise(sender)
        self.assertEqual(sender.chunks,[self.body[:-2],b'\r\n'])
        self.assertEqual(bytes(sender.written),self.body)
        self.assertEqual(self.outgoing.getvalue(),self.body)
        self.assertEqual(self.generated.getvalue(),self.body)
        self.assertEqual(self.state['sender_exit_observation']['queued_host_bytes'],2)
        self.assertEqual(self.state['sender_returncode'],0)
        self.assertEqual(self.state['queued_host_bytes'],0)
        self.assertFalse(self.state['stdout_open'])
    def test_successful_exit_still_drains_unread_stdout(self):
        sender=RecordedSender(self.body,len(self.body)-2,already_exited=True)
        self.exercise(sender)
        self.assertEqual(bytes(sender.written),self.body)
        self.assertTrue(self.state['sender_exit_observation']['stdout_open'])
        self.assertEqual(self.state['host_wire_bytes'],len(self.body))
    def test_failed_sender_leaves_unsent_bytes_unsent(self):
        sender=RecordedSender(self.body,len(self.body)-2,status=1)
        with self.assertRaisesRegex(ValueError,'exited with status 1'):self.exercise(sender)
        self.assertEqual(bytes(sender.written),self.body[:-2])
        self.assertEqual(self.state['queued_host_bytes'],2)
        self.assertEqual(self.state['sender_returncode'],1)
    def test_lost_claim_cannot_flush_queued_bytes(self):
        sender=RecordedSender(self.body,len(self.body)-2)
        with self.assertRaisesRegex(ValueError,'claim lost'):
            self.exercise(sender,owned=lambda:len(sender.written)<len(self.body)-2)
        self.assertEqual(bytes(sender.written),self.body[:-2])
        self.assertEqual(self.state['queued_host_bytes'],2)
    def test_sender_exit_does_not_extend_the_wire_budget(self):
        sender=RecordedSender(self.body,len(self.body)-2,already_exited=True)
        with self.assertRaisesRegex(ValueError,'owned RAM wire bound'):
            self.exercise(sender,limit=len(self.body)-1)
        self.assertEqual(sender.written,b'')
    def test_stalled_sender_keeps_the_existing_deadline(self):
        sender=RecordedSender(self.body,len(self.body)-2,stalled=True)
        with self.assertRaisesRegex(TimeoutError,'unchanged relay deadline'):self.exercise(sender)
        self.assertEqual(sender.ticks,5);self.assertEqual(sender.written,b'')
    def test_invalid_or_unowned_source_rejects_before_uart_access(self):
        with tempfile.TemporaryDirectory() as temporary:
            folder=Path(temporary);payload=folder/'payload.bin';payload.write_bytes(b'pattern')
            console=types.SimpleNamespace(parked=True)
            expected=hashlib.sha256(payload.read_bytes()).hexdigest()
            for kind in ('hash','window','type','owner','prompt','symlink','size'):
                with self.subTest(kind=kind):
                    selected=payload;digest=expected;window=1<<20;console.parked=True
                    if kind=='hash':digest='0'*64
                    if kind=='window':window=1
                    if kind=='type':window=True
                    if kind=='prompt':console.parked=False
                    if kind=='symlink':
                        selected=folder/'link';selected.symlink_to(payload)
                    if kind=='size':
                        selected=folder/'large';selected.write_bytes(bytes((2<<20)+1))
                    with patch.object(uart_transfer.termios,'tcgetattr') as attrs,self.assertRaises(ValueError):
                        uart_transfer.send(console,folder/'sender',selected,expected_sha256=digest,window_bytes=window,
                                           owns=lambda:kind!='owner',stderr=folder/'sender.log')
                    attrs.assert_not_called()
            self.assertFalse((folder/'sender.log.wire').exists())

if __name__=='__main__':unittest.main()
