"""Execute staged loader functions with actual admission policy and injected I/O."""
import ast,hashlib,importlib.util,json,subprocess,sys,tempfile,types,unittest
from pathlib import Path
from unittest.mock import patch
s=Path(__file__).resolve().parent;root=s.parents[1];sys.path.insert(0,str(root/'platform/embedded/nano'))
import jtag_programming as policy
RAW=(root/'tests/fixtures/nano-jtag/vendor-live-scan.txt').read_bytes()
CABLE=next(x.removeprefix('FES_JTAG_SCAN CABLE ') for x in RAW.decode().splitlines() if x.startswith('FES_JTAG_SCAN CABLE '))
sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
def function(path,name):return next(x for x in ast.parse(path.read_text()).body if isinstance(x,ast.FunctionDef) and x.name==name)

class Model:
 def __init__(self,base,kind,mode=''):
  self.base=base;self.kind=kind;self.mode=mode;self.owned=True;self.calls=[];self.now=0
  self.probe=base/'probe';self.probe.write_bytes(b'model probe, never executed')
  self.image=base/'image.sof';self.image.write_bytes(b'model image, never programmed')
  (base/'private-client.conf').write_text('model private server')
  (base/'transport.json').write_text(json.dumps(dict(status='ready',source_commit='1'*40,serial='TRWU8CBL',owned_pid=1234,process_start_ticks='5678')))
  (base/'preflight.json').write_text(json.dumps(dict(host_programming_policy_sha256=sha(policy.__file__),vendor_scan_probe=dict(path=str(self.probe),sha256=sha(self.probe),client_sha256={p:'4'*64 for p in policy.LIBRARIES}))))
  self.common=types.ModuleType('common');self.common.BASE=base;self.common.ROOT=root;self.common.SOURCE='1'*40
  self.common.claim=lambda:types.SimpleNamespace(owns=lambda:self.owned,token='2'*16)
  self.common.adapter_cable=lambda:CABLE
  self.common.sha=lambda p:'4'*64 if str(p) in policy.LIBRARIES else sha(p)
  self.common.parse_jtag_clock=lambda text:text.strip()
  with patch.dict(sys.modules,common=self.common):
   spec=importlib.util.spec_from_file_location('operator_gate_'+kind,s/'trial/program_gate.py');self.gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.gate)
  self.subprocess=types.SimpleNamespace(Popen=self.popen,run=self.run,STDOUT=subprocess.STDOUT)
  self.gate.subprocess=self.subprocess
  self.gate.time=types.SimpleNamespace(monotonic=self.clock)
  self.env=dict(BASE=base,sof=self.image,claim=self.common.claim,jtag_cable=lambda:CABLE,sha=sha,os=types.SimpleNamespace(chown=lambda *args:None),
                subprocess=self.subprocess,time=types.SimpleNamespace(monotonic=self.clock),
                identity=lambda:None,save=lambda:None,pump=lambda seconds:None,
                receipt=dict(programming_attempted=False),native=kind=='native',child=None)
  tree=ast.Module(body=[function(s/'trial'/(kind+'.py'),name) for name in ('program_process','program')],type_ignores=[])
  exec(compile(ast.fix_missing_locations(tree),str(s/'trial'/(kind+'.py')),'exec'),self.env)
 def clock(self):self.now+=100 if self.mode=='program-timeout' else .001;return self.now
 def run(self,argv,**kwargs):
  self.calls.append(('clock',argv));kwargs['stdout'].write(b'6M\n');return types.SimpleNamespace(returncode=0)
 def popen(self,argv,**kwargs):
  scan=argv[0]==str(self.probe);kind='scan' if scan else 'query' if argv[-1]=='-a' else 'program'
  self.calls.append((kind,argv))
  kwargs['stdout'].write((b'bad scan\n' if self.mode=='bad-scan' else RAW) if scan else b'fixture programmer output\n')
  if scan and self.mode=='claim-loss':self.owned=False
  child=types.SimpleNamespace(returncode=1 if (scan and self.mode=='scan-fail') or (kind=='program' and self.mode=='program-fail') else 0)
  child.poll=lambda:None if self.mode=='program-timeout' and kind=='program' and child.returncode==0 else child.returncode
  def kill():self.calls.append(('kill',kind));child.returncode=-9
  child.kill=kill;child.wait=lambda timeout:child.returncode
  return child
 def program(self):
  with patch.dict(sys.modules,program_gate=self.gate):
   return self.env['program'](['-m','jtag','-o','p;'+str(self.image)+'@1'],self.kind+'-program.log',90)

class OperatorProgrammingTests(unittest.TestCase):
 def test_both_actual_loaders_consume_fresh_scan_before_traced_program(self):
  for kind in ('recovery','native'):
   with self.subTest(kind=kind),tempfile.TemporaryDirectory() as temp:
    m=Model(Path(temp),kind);self.assertEqual(m.program(),'fixture programmer output\n')
    self.assertEqual([k for k,_ in m.calls],['clock','scan','clock','clock','program'])
    argv=m.calls[-1][1];self.assertEqual(argv[0],'/usr/bin/strace');self.assertIn('--kill-on-exit',argv)
    self.assertEqual(argv[-4:],['-m','jtag','-o','p;'+str(m.image)+'@1'])
    self.assertTrue(m.env['receipt']['programming_attempted'])
    self.assertEqual(m.env['receipt']['live_programming_grant']['phase'],kind)
    with self.assertRaises(FileExistsError):m.program()
    self.assertEqual(len([x for x in m.calls if x[0]=='program']),1)
 def test_first_failure_stops_both_loaders_without_replay(self):
  for kind in ('recovery','native'):
   for mode in ('scan-fail','bad-scan','claim-loss','program-fail','program-timeout'):
    with self.subTest(kind=kind,mode=mode),tempfile.TemporaryDirectory() as temp:
     m=Model(Path(temp),kind,mode)
     with self.assertRaises((AssertionError,ValueError,TimeoutError)):m.program()
     attempted=mode.startswith('program-')
     self.assertEqual(len([x for x in m.calls if x[0]=='program']),int(attempted))
     self.assertEqual(m.env['receipt']['programming_attempted'],attempted)
     m.owned=True
     with self.assertRaises(FileExistsError):m.program()
     self.assertEqual(len([x for x in m.calls if x[0]=='program']),int(attempted))
 def test_read_only_query_does_not_mint_or_consume_programming_admission(self):
  for kind in ('recovery','native'):
   with self.subTest(kind=kind),tempfile.TemporaryDirectory() as temp:
    m=Model(Path(temp),kind)
    self.assertEqual(m.env['program'](['-a'],kind+'-query.log',30),'fixture programmer output\n')
    self.assertEqual([k for k,_ in m.calls],['query'])
    self.assertFalse(m.env['receipt']['programming_attempted'])
    self.assertFalse((Path(temp)/(kind+'-program-consumed.json')).exists())
 def test_original_receive_cleanup_and_phase_fuses_preserved(self):
  old=root/'out/nano-resume-board-20261108/trial'
  for name in ('native.py','recovery.py'):
   for method in ('pump','check_clock','start_attempt')+ (('drain_failure_tail',) if name=='native.py' else ()):
    self.assertEqual(ast.dump(function(old/name,method)),ast.dump(function(s/'trial'/name,method)),(name,method))
  for name in ('private_jtag.py','transport_guards.py','clock_policy.py','bounded_log.py'):
   self.assertEqual((old/name).read_bytes(),(s/'trial'/name).read_bytes(),name)

if __name__=='__main__':unittest.main(verbosity=2)
