"""Strict two-load trace auditing, with current configuration and fresh drains."""
import importlib.util,json,re,sys,unittest
from pathlib import Path
import test_nano_window_storage_trace as fixture
from test_nano_window_present import BUILD,SEED,OWNER
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'platform/embedded/nano'))
try:
 spec=importlib.util.spec_from_file_location('nano_window_reload_trace',ROOT/'platform/embedded/nano/window_reload_trace.py')
 trace=importlib.util.module_from_spec(spec);spec.loader.exec_module(trace)
finally:sys.path.pop(0)

class WindowReloadTraceTests(unittest.TestCase):
 def setUp(self):
  f=fixture.WindowStorageTraceTests();f.setUp();self.first=f
  base=f.frames.base
  prefix=base.prefix[base.prefix.index('FES_NATIVE_HPS_CONTEXT PASS '):]
  prefix=prefix.replace(' generation=4 ',' generation=10 ')
  prefix=prefix.replace('NOTICE:  FES_NANO_CORE_TRANSFER phase=complete',f'NOTICE:  FES_NANO_FABRIC_ACTIVATION owner={OWNER:x} generation=10 previous=4 rearm=1 complete=1 data_held=1\nNOTICE:  FES_NANO_CORE_TRANSFER phase=complete')
  counter=0;timed=[]
  for row in prefix.splitlines(keepends=True):
   if 'FES_NANO_CORE_TRANSFER ' in row:
    row=row.rstrip('\r\n')+f' counter={0x1000+counter*400000:x} hash_ticks={counter*100:x}\n';counter+=1
   timed.append(row)
  self.program=''.join(timed)
  body=f.frames.prefix[len(base.prefix):]+''.join(f.frames.blocks[:2])+f.frames.tail
  body=body.replace('source_generation=4 ','source_generation=10 ').replace('closed_generation=7 ','closed_generation=13 ')
  body=re.sub(r'(?<!_)generation=([567])\b',lambda m:'generation='+str(int(m[1])+6),body)
  body=body.replace('index=13 phase=return_','index=3 phase=return_').replace('frames=12 ','frames=2 ')
  for name in ['reader_generation','request_generation','ack_generation','cookie','ack_cookie']:
   body=body.replace(name+'=13 ',name+'=3 ')
  body=body.replace('crc32=1234000c ', 'crc32=12340002 ').replace('frame_sha256='+f'{12:064x}','frame_sha256='+f'{2:064x}')
  self.window=body
  self.begin=f'FES_NATIVE_FPGA_WINDOW_RELOAD BEGIN package_id={base.identity["package_id"]} owner={OWNER:x} returned_generation=7 source_generation=10 cycles=2 memory_retained=1\n'
  self.receipt=f'FES_NATIVE_FPGA_WINDOW_RELOAD PASS package_id={base.identity["package_id"]} owner={OWNER:x} source_generation=10 closed_generation=13 cycles=2 frames=2 data_closed=1 sd_contained=1 memory_retained=1 runtime_admission=unavailable\n'
  self.raw=f.raw+self.begin+self.program+self.window+self.receipt
 def decode(self,raw,**changes):
  b=self.first.frames.base
  options=dict(window_reload=True,fpga_identity=b.identity,configuration=b.configuration,from_storage=True,
   reader_memory=b.memory,window_read=b.contract,cpu_mapping=b.mapping,window_present=True)
  options.update(changes)
  return trace.decode(raw,BUILD,SEED,**options)
 def test_two_complete_cycles_and_fresh_SD_are_required(self):
  r=self.decode(self.raw);self.assertTrue(r['complete'],r['errors'])
  self.assertEqual(r['last_checkpoint'],'window_reload_closed')
  self.assertEqual(len(r['window_reload']['window']['presentations']),2)
  self.assertEqual(r['window_reload']['window']['reader_drain']['ack_cookie'],3)
  self.assertFalse(r['core_load_acceptance']);self.assertFalse(r['window_reload']['memory_released'])
  json.dumps(r)
  for value in [False,1,None,'yes']:
   with self.subTest(value=value),self.assertRaises(ValueError):self.decode(self.raw,window_reload=value)
  for raw in [self.first.raw,self.first.raw+self.begin,self.first.raw+self.begin+self.program,self.raw[:-1]]:
   self.assertFalse(self.decode(raw)['complete'])
 def test_source_transfer_readiness_and_closure_failures_reject(self):
  for text in ['FES_NATIVE_HPS_CONTEXT PASS ','FES_NANO_CORE_TRANSFER phase=status ','FES_NANO_FABRIC_ACTIVATION ',
   'FES_NATIVE_FPGA_PROGRAMMED PASS ','FES_NANO_CONFIG_QUERY ','FES_NANO_CONFIG_STATUS ',
   'FES_NANO_MASTER_STOP ','FES_NANO_WINDOW_READER_DRAIN ','FES_NANO_WINDOW_CLOSURE ','FES_NATIVE_FPGA_WINDOW_PRESENT PASS ']:
   suffix=''.join(line for line in (self.program+self.window).splitlines(keepends=True) if text not in line)
   with self.subTest(text=text):self.assertFalse(self.decode(self.first.raw+self.begin+suffix+self.receipt)['complete'])
  for before,after in [('generation=10 ','generation=9 '),('source_generation=10 ','source_generation=4 '),
   ('previous=4 rearm=1','previous=1 rearm=1'),('previous=4 rearm=1','previous=4 rearm=0'),
   ('closed_generation=13 ','closed_generation=7 '),('ack_cookie=3 ','ack_cookie=13 '),
   ('hardware_drained=8 ','hardware_drained=0 '),('published_crc32=12340002','published_crc32=12340001'),
   ('frames=2 ','frames=12 '),('last_reader_generation=3 ','last_reader_generation=13 '),('ready=1','ready=0')]:
   suffix=(self.program+self.window+self.receipt).replace(before,after)
   with self.subTest(before=before):self.assertFalse(self.decode(self.first.raw+self.begin+suffix)['complete'])
 def test_missing_reordered_replayed_or_extra_events_reject(self):
  for suffix in [self.receipt,self.program+self.window+self.receipt+self.receipt,
   self.receipt+self.program+self.window,self.program+self.window+self.receipt+'unexpected\n']:
   self.assertFalse(self.decode(self.first.raw+self.begin+suffix)['complete'])
  for end in range(0,len(self.raw),503):self.assertFalse(self.decode(self.raw[:end])['complete'])
  self.assertFalse(self.decode(self.raw+'x'*(1<<20))['complete'])
  self.assertFalse(self.decode(self.first.frames.raw+self.begin+self.program+self.window+self.receipt)['complete'])
  untimed=re.sub(r' counter=[0-9a-f]+ hash_ticks=[0-9a-f]+','',self.program)
  self.assertFalse(self.decode(self.first.raw+self.begin+untimed+self.window+self.receipt)['complete'])

if __name__=='__main__':unittest.main()
