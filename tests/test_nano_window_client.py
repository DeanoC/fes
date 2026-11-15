"""Exercise the actual bounded client and its no-retry/ownership gates."""
import copy
import json
import re
import unittest

import test_nano_window_service_trace as fixture
from test_nano_window_present import BUILD,SEED
from window_client import WindowClient,exercise


class WindowClientTests(unittest.TestCase):
    def client(self,fault=None):
        f=fixture.WindowServiceTraceTests();f.setUp();initial=f.packet(f.initial,f.cold+f.open)
        state=dict(reply=f.initial,exchanges=0,owns=True,clock=0.,rows=[])
        def exchange(raw,bound):
            state['exchanges']+=1;command=json.loads(raw);previous=state['reply'];op=command['op'];prefix=''
            if fault=='transport':raise OSError('disconnected')
            if fault=='late':state['clock']+=bound
            if fault=='backwards':state['clock']-=1
            if fault=='claim':state['owns']=False
            invalid=op!='status' and (command['generation']!=previous['generation'] or previous['state']!=('ready' if op=='launch' else 'running'))
            if invalid:
                r=dict(previous,op=op,ok=False,error='stale request')
            elif op=='status':r=dict(previous,op=op)
            elif op=='present':
                r=copy.deepcopy(previous);r.update(op=op,frames=previous['frames']+1)
                frame=r['last_frame'];frame.update(reader_generation=r['frames']+1,drain_generation=r['frames']+1,drain_cookie=r['frames']+1,
                    published_crc32=0x12340000+r['frames'],read_crc32=0x12340000+r['frames'],frame_sha256=f'{r["frames"]:064x}')
            elif op in ('return','park'):
                source=previous['last_frame']['source_generation'];r=f.reply(op,previous['generation']+1,previous['frames'],source,closed=source,state='ready' if op=='return' else 'parked');prefix=f.close_prefix(source)
            elif op=='launch':
                old=previous['last_return']['source_generation'];source=old+6;r=f.reply(op,previous['generation']+1,1,source,closed=old)
                prefix=f.warm_prefix(source,old)
                prefix=re.sub(r'counter=([0-9a-f]+)',lambda m:'counter='+format(int(m[1],16)+source*(1<<32),'x'),prefix)
            else:raise AssertionError(command)
            if not invalid:state['reply']=r
            if fault=='bad-reply':r=dict(r,generation=True)
            return f.packet(r,prefix)
        c=WindowClient(initial,build_id=BUILD,owner_seed=SEED,identity=f.identity,configuration=f.configuration,memory=f.memory,contract=f.contract,cpu_mapping=f.mapping,
            exchange=exchange,owns=lambda:state['owns'],now=lambda:state['clock'],record=lambda row:state['rows'].append(row))
        return c,state

    def test_three_sessions_and_expected_rejections_finish_parked(self):
        c,state=self.client();result=exercise(c)
        self.assertEqual(len(result['cycles']),3);self.assertEqual(result['commands'],19)
        self.assertEqual([r['returned']['closed_generation'] for r in result['cycles']],[7,13,19])
        self.assertEqual(result['final']['storage'],dict(generation=16,active=False,prepared=False))
        self.assertFalse(c.retired);self.assertEqual(state['exchanges'],19)
        self.assertFalse(result['ordinary_admission']);self.assertFalse(result['memory_released'])
        with self.assertRaises(ValueError):c.request('status')
        self.assertEqual(state['exchanges'],19)

    def test_uncertain_exchange_or_time_or_claim_never_retries(self):
        for fault in ('transport','late','backwards','claim','bad-reply'):
            with self.subTest(fault=fault):
                c,state=self.client(fault)
                with self.assertRaises((ValueError,OSError)):c.request('status')
                self.assertTrue(c.retired);self.assertEqual(state['exchanges'],1)
                self.assertEqual(c.records[-1]['status'],'failed')
                with self.assertRaises(ValueError):c.request('status')
                self.assertEqual(state['exchanges'],1)

    def test_invalid_local_request_lost_claim_and_busy_have_no_exchange(self):
        c,state=self.client()
        with self.assertRaises(ValueError):c.request('launch')
        with self.assertRaises(ValueError):c.request('present',generation=0)
        c.active.acquire()
        try:
            with self.assertRaises(ValueError):c.request('status')
        finally:c.active.release()
        self.assertEqual(state['exchanges'],0)
        state['owns']=False
        with self.assertRaises(ValueError):c.request('status')
        self.assertTrue(c.retired);self.assertEqual(state['exchanges'],0)

    def test_public_views_and_returned_replies_do_not_modify_client_state(self):
        c,state=self.client();r=c.request('status');r['last_frame']['source_generation']=999
        view=c.view;view['generation']=999
        self.assertEqual(c.view['generation'],1);self.assertEqual(c.view['last_frame']['source_generation'],4)

if __name__=='__main__':unittest.main()
