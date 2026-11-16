"""Strict cached replies plus independent firmware/configuration observations."""
import copy
import json
import re
import sys
from pathlib import Path
import unittest

import test_nano_window_reload_trace as fixture
from test_nano_window_present import BUILD,SEED,OWNER

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
import window_service_trace as trace


class WindowServiceTraceTests(unittest.TestCase):
    def setUp(self):
        f=fixture.WindowReloadTraceTests();f.setUp();self.fixture=f;b=f.first.frames.base
        self.identity=b.identity;self.configuration=b.configuration;self.memory=b.memory;self.contract=b.contract;self.mapping=b.mapping
        lines=f.first.raw.splitlines(keepends=True)
        split=next(i for i,line in enumerate(lines) if 'FES_NANO_WINDOW_SCOPE phase=control' in line)
        self.cold=self.instrument(''.join(lines[:split]))
        self.open=f.first.frames.prefix[len(b.prefix):]
        self.warm=self.instrument(''.join(line for line in f.program.splitlines(keepends=True) if not line.startswith(('FES_NATIVE_HPS_CONTEXT','FES_NATIVE_FPGA_PROGRAMMED'))))
        self.initial=self.reply('status',1,1,4)

    def instrument(self,raw):
        rows=[];index=0;seen=set()
        for line in raw.splitlines(keepends=True):
            if 'FES_NANO_CORE_TRANSFER ' not in line:rows.append(line);continue
            line=re.sub(r' counter=[0-9a-f]+ hash_ticks=[0-9a-f]+','',line).rstrip('\r\n')+f' counter={0x1000000+index*400000:x} hash_ticks={index*100:x}\n';index+=1;rows.append(line)
            generation=int(re.search(r'generation=(\d+)',line)[1]);phase=re.search(r'phase=(\w+)',line)[1]
            if phase=='start':rows.append(f'NOTICE:  FES_NANO_CORE_ENGINE credits=4 block_bytes=1048560 next_job=0 owner={OWNER:x} generation={generation}\n')
            mailbox={'start':1,'write':2,'poll':3}.get(phase)
            if mailbox and mailbox not in seen:
                seen.add(mailbox);completed=int(re.search(r'completed=(\d+)',line)[1]);pending=int(re.search(r'pending=(\d+)',line)[1])
                rows.append(f'NOTICE:  FES_NANO_CORE_MAILBOX phase={mailbox} owner={OWNER:x} generation={generation} completed={completed} pending={pending} job=1 cin=4 cout=4 rin=0 rout=0 bell=0 status=0 state=0 next=0 slot=-1 cmd_used=0 resp_used=0\n')
        return ''.join(rows)

    def reply(self,op,generation,frames,source,closed=None,state='running'):
        r=dict(scope=trace.SCOPE,op=op,ok=True,state=state,owner=f'{OWNER:016x}',generation=generation,frames=frames,package_id=self.identity['package_id'],
            last_frame=dict(source_generation=source,reader_generation=frames+1,cookie=1,bytes=3686400,published_crc32=0x12340000+frames,
                read_crc32=0x12340000+frames,frame_sha256=f'{frames:064x}',drain_generation=frames+1,drain_cookie=frames+1),
            storage=dict(generation=source if state!='ready' else source+5,active=state=='ready',prepared=state=='ready'))
        if closed is not None:r['last_return']=dict(source_generation=closed,closed_generation=closed+3,frames=2,reset=15,masters_contained=True)
        return r

    def packet(self,r,prefix=''):return prefix.encode()+trace.PREFIX+json.dumps(r).encode()+b'\n'
    def startup(self,data):return trace.startup(data,BUILD,SEED,identity=self.identity,configuration=self.configuration,memory=self.memory,contract=self.contract,cpu_mapping=self.mapping)
    def transition(self,data,previous,op):return trace.transition(data,previous,op,identity=self.identity,configuration=self.configuration,memory=self.memory)

    def close_prefix(self,source):
        return ''.join(line for line in self.fixture.window.splitlines(keepends=True) if any(marker in line for marker in
            ('FES_NANO_WINDOW_SCOPE phase=close','FES_NANO_MASTER_STOP','FES_NANO_WINDOW_READER_DRAIN','FES_NANO_WINDOW_CLOSURE'))).replace('generation=12 ',f'generation={source+2} ').replace('generation=13 ',f'generation={source+3} ')

    def warm_prefix(self,source,previous):
        raw=self.warm+self.open.replace('generation=5 ',f'generation={source+1} ').replace('generation=6 ',f'generation={source+2} ')
        return re.sub(r'\bgeneration=10\b',f'generation={source}',raw).replace('previous=4 rearm=1',f'previous={previous} rearm=1')

    def test_three_cycles_require_boot_fresh_configuration_and_each_owned_return(self):
        result=self.startup(self.packet(self.initial,self.cold+self.open));self.assertFalse(result['runtime_admission'])
        previous=result['reply']
        self.assertEqual(self.transition(self.packet(previous),previous,'status')['reply'],previous)
        for cycle in range(3):
            source=4+6*cycle;generation=1+2*cycle
            presented=self.reply('present',generation,2,source,closed=source-6 if cycle else None)
            previous=self.transition(self.packet(presented),previous,'present')['reply']
            returned=self.reply('park' if cycle==2 else 'return',generation+1,2,source,closed=source,state='parked' if cycle==2 else 'ready')
            previous=self.transition(self.packet(returned,self.close_prefix(source)),previous,returned['op'])['reply']
            if cycle==2:break
            launched=self.reply('launch',generation+2,1,source+6,closed=source)
            previous=self.transition(self.packet(launched,self.warm_prefix(source+6,source)),previous,'launch')['reply']
        self.assertEqual(previous['state'],'parked');self.assertEqual(previous['last_return']['closed_generation'],19)

    def test_reply_types_duplicates_stale_scopes_and_incomplete_packets_reject(self):
        raw=self.packet(self.initial)
        invalid=[raw[:-1],raw+raw,raw+b'extra\n',raw.replace(b'"ok": true',b'"ok": true, "ok": true'),raw.replace(b'"cookie": 1',b'"cookie": 1, "cookie": 1'),raw.replace(b'"frames": 1',b'"frames": NaN')]
        for key,value in [('generation',True),('frames',0),('scope','wrong'),('owner','0'*16),('package_id','0'*64)]:
            invalid.append(self.packet(dict(self.initial,**{key:value})))
        for key,value in [('bytes',32),('published_crc32',True),('read_crc32',0),('drain_generation',1),('frame_sha256','0'*64)]:
            changed=copy.deepcopy(self.initial);changed['last_frame'][key]=value;invalid.append(self.packet(changed))
        for key,value in [('generation',5),('active',True),('prepared',True)]:
            changed=copy.deepcopy(self.initial);changed['storage'][key]=value;invalid.append(self.packet(changed))
        for data in invalid:
            with self.subTest(data=data[:90]),self.assertRaises((ValueError,UnicodeError)):trace.response(data)
        failed={k:v for k,v in self.initial.items() if k not in ('last_frame','storage')};failed.update(ok=False,state='reboot_required',error='uncertain frame')
        self.assertFalse(trace.response(self.packet(failed))[0]['ok'])
        with self.assertRaises(ValueError):trace.response(self.packet(dict(failed,last_frame=self.initial['last_frame'])))

    def test_missing_and_changed_startup_opening_and_configuration_fail(self):
        valid=self.packet(self.initial,self.cold+self.open);self.startup(valid)
        for marker in ('FES_NATIVE_FPGA_WINDOW_MEMORY','FES_NANO_FABRIC_ACTIVATION','FES_NANO_CORE_ENGINE','FES_NANO_CORE_MAILBOX',
                       'FES_NATIVE_FPGA_PROGRAMMED','FES_NANO_WINDOW_GATE','FES_NANO_WINDOW_SCOPE','FES_NANO_DDR_REGION','FES_NANO_SMMU'):
            data=b''.join(line for line in valid.splitlines(keepends=True) if marker.encode() not in line)
            with self.subTest(marker=marker),self.assertRaises(ValueError):self.startup(data)
        for end in range(1,len(valid),701):
            with self.assertRaises(ValueError):self.startup(valid[:end])

    def test_omitted_or_replayed_drain_and_warm_configuration_fail(self):
        previous=self.reply('present',1,2,4)
        returned=self.reply('return',2,2,4,closed=4,state='ready')
        raw=self.packet(returned,self.close_prefix(4))
        for marker in ('FES_NANO_WINDOW_SCOPE','FES_NANO_MASTER_STOP','FES_NANO_WINDOW_READER_DRAIN','FES_NANO_WINDOW_CLOSURE'):
            for duplicate in (False,True):
                data=b''.join(line*(2 if duplicate and marker.encode() in line else 1) for line in raw.splitlines(keepends=True) if duplicate or marker.encode() not in line)
                with self.subTest(marker=marker,duplicate=duplicate),self.assertRaises(ValueError):self.transition(data,previous,'return')
        launched=self.reply('launch',3,1,10,closed=4);warm=self.packet(launched,self.warm_prefix(10,4))
        self.transition(warm,returned,'launch')
        for marker in ('FES_NANO_FABRIC_ACTIVATION','FES_NANO_CORE_ENGINE','FES_NANO_CORE_MAILBOX','FES_NANO_CORE_TRANSFER','FES_NANO_WINDOW_GATE','FES_NANO_CONFIG_STATUS'):
            data=b''.join(line for line in warm.splitlines(keepends=True) if marker.encode() not in line)
            with self.subTest(marker=marker),self.assertRaises(ValueError):self.transition(data,returned,'launch')
        for before,after in [(b'previous=4 rearm=1',b'previous=1 rearm=1'),(b'generation=10 ',b'generation=4 '),(b'credits=4 ',b'credits=5 ')]:
            with self.subTest(before=before),self.assertRaises(ValueError):self.transition(warm.replace(before,after),returned,'launch')

if __name__=='__main__':unittest.main()
