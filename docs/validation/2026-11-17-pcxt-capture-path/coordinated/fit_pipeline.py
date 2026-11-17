#!/usr/bin/env python3
"""Fresh, source-frozen elaboration and fixed-seed diagnostic; never seal or export."""
import argparse, hashlib, importlib.util, json, shutil, subprocess, threading, time
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
for n in ('module-root','yosys','quartus-root','work','collector','proof'):
 p.add_argument('--'+n,type=Path,required=True)
a=p.parse_args();root=a.module_root.resolve();work=a.work.resolve()
if not json.loads(a.proof.read_text())['trace']['status']=='PASS':raise ValueError('read deadline proof required')
if work.exists():raise ValueError('refuse existing work')
work.parent.mkdir(parents=True,exist_ok=True)
if shutil.disk_usage(work.parent).free<30*1024**3:raise ValueError('under 30 GiB free')
work.mkdir();spec=importlib.util.spec_from_file_location('q',root/'scripts/quartus_fes_pcxt.py');q=importlib.util.module_from_spec(spec);spec.loader.exec_module(q)
stop=threading.Event();start=time.monotonic()
def heartbeat():
 while not stop.is_set():
  (work/'heartbeat.json').write_text(json.dumps({'elapsed_s':round(time.monotonic()-start)})+'\n')
  if stop.wait(15):return
threading.Thread(target=heartbeat,daemon=True).start()
record={'classification':'experimental source-frozen 100 MHz fit; not production design-v1 identity or physical acceptance','commands':[],'proof_sha256':q.sha(a.proof),'driver_sha256':q.sha(Path(__file__))}
def run(label,cmd,cwd):
 item={'label':label,'argv':list(map(str,cmd)),'cwd':str(cwd)};record['commands'].append(item)
 with (work/(label+'.log')).open('w') as log:
  item['exit_code']=subprocess.run(list(map(str,cmd)),cwd=cwd,stdout=log,stderr=subprocess.STDOUT).returncode
 if item['exit_code']:raise RuntimeError(label+' failed: '+str(item['exit_code']))
try:
 source,record['source_sha256']=q.capture_sources(root,work)
 proof=json.loads(a.proof.read_text())
 if any(q.sha(source/n)!=h for n,h in proof['sources_sha256'].items()):raise ValueError('proof does not cover captured source bytes')
 config={'capture_pipeline':1,'seed':1,'system_mhz':100,'capture_phase_ps':8125,'forward_phase_ps':1250,'input_registers':16,'forward_clock_routing':'REGIONAL CLOCK','forward_clock_net':'clocks[2]'}
 record['config']=config
 record['experimental_id']=hashlib.sha256(json.dumps({'sources':record['source_sha256'],'config':config},sort_keys=True).encode()).hexdigest()[:32]
 elab=work/'elaboration';elab.mkdir();q.prepare_elaboration_inputs(source,elab,dos_profile=True)
 library=a.yosys.resolve().parent.parent/'share/yosys/intel_alm/common/megafunction_bb.v'
 program=q.elaboration(source,elab,library,package_profile=True,build_id=record['experimental_id'],dos_profile=True)
 assert program.count('chparam -set MDA')==1
 program=program.replace('chparam -set MDA','chparam -set SDRAM_CAPTURE_PIPELINE 1 -set MDA')
 (elab/'elaborate.ys').write_text(program)
 record['yosys_sha256']=q.sha(a.yosys);record['program_sha256']=q.sha(elab/'elaborate.ys')
 run('elaborate',['timeout','--kill-after=10','600',a.yosys.resolve(),'-s','elaborate.ys'],elab)
 record['renaming']=q.validate_renaming(elab,package_profile=True,dos_profile=True,capture_pipeline=True)
 before=json.loads((elab/'before-names.json').read_text())['modules']['top'];after=json.loads((elab/'elaborated.json').read_text())['modules']['top']
 record['mailbox']=q.validate_mailbox(before,after,half_rate_board=False);record['audio']=q.validate_audio(before,after)
 record['dont_replicate']=q.mark_dont_replicate(elab/'elaborated.v',q.AUDIO_SINGLE_REGISTERS)
 fit=work/'seed-1';fit.mkdir();shutil.copy2(elab/'elaborated.v',fit/'elaborated.v')
 for src,name in ((q.AUDIT,'audit.tcl'),(q.MAILBOX,'mailbox.sdc'),(q.AUDIO_SDC,'audio.sdc')):shutil.copy2(source/src,fit/name)
 (fit/'audio-audit.enabled').write_text('audio interstage checks enabled\n')
 lock=q.load_quartus_lock(root,str(q.QUARTUS_LOCK));(fit/'top.qpf').write_text(q.project_qpf(lock))
 qsf=q.project_qsf(source,1,2,package_profile=True,video_mailbox_contract=True,audio_contract=True)
 qsf+='\n'+''.join(f'set_instance_assignment -name FAST_INPUT_REGISTER ON -to SDRAM_DQ[{n}]\n' for n in range(16))
 qsf+='set_instance_assignment -name GLOBAL_SIGNAL "REGIONAL CLOCK" -to clocks[2]\n'
 (fit/'top.qsf').write_text(qsf);(fit/'clocks.sdc').write_text(q.SDC+'\nsource audio.sdc\nsource mailbox.sdc\n')
 # Only the generated-clock source changes to the separate forwarded counter.
 original=a.collector.read_text();assert original.count('memory_clock|general[0]')==1
 collector=work/'collect.tcl';collector.write_text(original.replace('memory_clock|general[0]','memory_clock|general[2]'))
 record['collector_sha256']=q.sha(collector)
 record['inputs_sha256']={n:q.sha(fit/n) for n in ('elaborated.v','top.qsf','top.qpf','clocks.sdc','audio.sdc','mailbox.sdc','audit.tcl')}
 prefix=['timeout','--signal=TERM','--kill-after=10','1200','flock','-w','900','/home/deano/fes-hil/locks/lander-sharedhost.lock','flock','-w','900','/tmp/lander-quartus-lite.lock']
 for label,tool,args in [('compile','quartus_sh',['--flow','compile','top']),('audit','quartus_sta',['-t','audit.tcl']),('conditional','quartus_sta',['-t',str(collector)])]:
  binary=a.quartus_root.resolve()/'bin'/tool;record.setdefault('quartus_sha256',{})[tool]=q.sha(binary)
  run(label,prefix+[binary]+args,fit)
  shutil.copy2(work/(label+'.log'),fit/(label+'.log'))
 record['timing']=q.timing_summary(fit,mailbox_contract=True,audio_profile=True)
 record['rbf_sha256']=q.sha(fit/'output_files/top.rbf')
 assert all(q.sha(fit/n)==v for n,v in record['inputs_sha256'].items())
 record['status']='captured; acceptance requires complete internal and conditional gate assessment'
except Exception as e:
 record['status']='failed';record['error']=str(e);raise
finally:
 stop.set();record['elapsed_s']=round(time.monotonic()-start,3);(work/'receipt.json').write_text(json.dumps(record,indent=2,sort_keys=True)+'\n');print(record['status'],work,flush=True)
