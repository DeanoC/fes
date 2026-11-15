import hashlib,json,os,re,signal,subprocess,sys
from pathlib import Path
root=Path.cwd();out=root/'out/de25-embedded-physical-preparation';prep=json.loads((out/'preparation.json').read_text());program=json.loads((out/'program.json').read_text());link=(out/'diagnostic-link.log').read_text()
def line(name):
 m=re.findall(r'^'+name+r' (.+)$',link,re.M)
 if len(m)!=1:raise ValueError('missing or ambiguous '+name)
 return m[0]
device,master=line('FES_DEVICE'),line('FES_MASTER')
if '#3-11.1-iface0#DE25-Nano' not in device or not master.startswith(device+'/') or not master.endswith('/subsys_debug_fpga_m.master'):
 raise ValueError('master is not on the selected board')
usb=Path('/sys/bus/usb/devices/3-11.1');assert (usb/'serial').read_text().strip()=='TRWU8CBL'
assert program['serial']=='TRWU8CBL' and program['image_sha256']==prep['programmed_sof_sha256']
image=out/'diagnostic-with-vendor-spl.sof';assert hashlib.sha256(image.read_bytes()).hexdigest()==prep['programmed_sof_sha256']
identity=prep['build_id']; bad=('0' if identity[0]!='0' else '1')+identity[1:]
script=root/'sources/misteross/prototypes/de25_board_control/hardware.tcl'
expected=json.loads((root/'out/de25-embedded-board-final-check/result.json').read_text())
assert hashlib.sha256(script.read_bytes()).hexdigest()==expected['tcl_source_sha256']['hardware.tcl']
baseenv=dict(os.environ,FES_DE25_SOF=str(image),FES_DE25_DEVICE=device,FES_DE25_MASTER=master,FES_DE25_BASE='0x110400')
results=[]
for name,buildid,abi,want in [('wrong-id',bad,prep['abi'],'reject'),('wrong-abi',identity,prep['abi']+1,'reject'),('identity-drain-fault',identity,prep['abi'],'pass')]:
 env=dict(baseenv,FES_DE25_BUILD_ID=buildid,FES_DE25_ABI=str(abi));log=out/(name+'.log')
 with log.open('wb') as f:
  process=subprocess.Popen(['/data/altera_pro/26.3/syscon/bin/system-console','--script='+str(script)],stdout=f,stderr=subprocess.STDOUT,env=env,start_new_session=True)
  try:code=process.wait(timeout=120)
  except subprocess.TimeoutExpired:
   os.killpg(process.pid,signal.SIGTERM)
   try:process.wait(timeout=5)
   except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
   raise RuntimeError(name+' timed out; physical outcome uncertain, do not replay')
 text=log.read_text(errors='replace')
 if 'FES_MASTER_CLOSED' not in text:raise RuntimeError(name+' did not confirm master closure; do not replay')
 if want=='reject':
  if code==0 or 'FES_IDENTITY_REJECTED writes=0' not in text:raise RuntimeError(name+' did not reject before writes')
 else:
  if code!=0 or any(marker not in text for marker in ('FES_IDENTITY_MATCH','FES_DRAIN_PASS','FES_FAULT_PASS','FES_DIAGNOSTIC_PASS')):
   raise RuntimeError(name+' did not complete; do not replay')
 results.append({'case':name,'returncode':code,'log_sha256':hashlib.sha256(log.read_bytes()).hexdigest()})
 print(name+': PASS',flush=True)
(out/'jtag-proof.json').write_text(json.dumps({'status':'passed','serial':'TRWU8CBL','image_sha256':prep['programmed_sof_sha256'],'build_id':identity,'control_abi':prep['abi'],'cases':results,'scope':'JTAG control, on-chip responder; no DDR or native firmware acceptance'},indent=2)+'\n')
