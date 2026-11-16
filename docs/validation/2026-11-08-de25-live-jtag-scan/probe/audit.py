"""Independent saved-output and current host cleanup audit; no device calls."""
import hashlib,json,os,re,stat
from pathlib import Path
s=Path(__file__).resolve().parent;root=s.parents[1]
assert os.geteuid()==0
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
proofs=[]
for name in ('nano-jtag-live-scan-20261108','nano-jtag-live-scan-v2-20261108'):
 stage=root/'out'/name;b=stage/'trial'
 r=json.loads((stage/'result.json').read_text());t=json.loads((b/'transport.json').read_text())
 freeze=json.loads((stage/'source.json').read_text())
 assert all(sha(stage/p)==digest for p,digest in freeze['files'].items())
 assert not r['programming_attempted'] and not r['hps_reset_attempted'] and not r['sd_write_attempted']
 assert r['prior_image_attempts']==2 and t['serial']=='TRWU8CBL' and t['shared_daemon_changed'] is False
 assert t['status']=='stopped' and t['owned_server_stopped'] and t['programming_admission'] is False
 proc=Path('/proc')/str(t['owned_pid'])
 assert not proc.exists() or proc.joinpath('stat').read_text().rsplit(')',1)[1].split()[19]!=t['process_start_ticks']
 assert r['clock_cleanup']['restored'] and r['clock_cleanup']['restored_clock']=='15M'
 notice='[ Environment variable QUARTUS_JTAG_CLIENT_CONFIG='+str(b/'private-client.conf')+' is in effect ]'
 for label,value in [('clock-before','15M'),('clock-confirm-6m','6M'),('clock-restored-readback','15M')]:
  lines=(b/(label+'.log')).read_text().strip().splitlines()
  if lines and lines[0]==notice:lines=lines[1:]
  assert lines==[value],(label,lines)
 commands=t['setup_commands'];expected=[['--addserver','localhost:'+str(t['private_port']),''],['--enum'],['--getparam',t['cable'],'JtagClock'],['--setparam',t['cable'],'JtagClock','6M'],['--getparam',t['cable'],'JtagClock'],[t['cable']],['--setparam',t['cable'],'JtagClock','15M'],['--getparam',t['cable'],'JtagClock']]
 assert [c['argv'][1:] for c in commands]==expected
 assert commands[5]['argv'][0]==str(stage/'scan-probe')
 assert all(c['returncode']==0 for i,c in enumerate(commands) if i!=5)
 output=(b/'forced-scan.log').read_text();calls=re.findall(r'^FES_JTAG_SCAN CALL phase=(\S+) rc=(\d+)$',output,re.M)
 if name.endswith('-v2-20261108'):
  assert r['pass'] and commands[5]['returncode']==0 and r['forced_scan_return_codes']==['0']
  assert calls==[(p,'0') for p in ('find','name','lock','clock','forced-scan','cached-after','unlock')]
  assert re.findall(r'^FES_JTAG_SCAN DEVICE (.+)$',output,re.M)==['phase=cached-after count=1 id=4362C0DD mask=00000000 ir=10']
  assert 'FES_JTAG_SCAN CLOCK 6000000\n' in output and 'RESULT pass=1 programming=0 cpu_reset=0\n' in output
 else:
  assert r['pass'] is False and commands[5]['returncode']==1 and r['forced_scan_return_codes']==[]
  assert calls==[(p,'0') for p in ('find','name','lock','clock')]+[('cached-before','38'),('unlock','0')]
  assert 'RESULT pass=0 programming=0 cpu_reset=0\n' in output
 paths=sorted(set(re.findall(r'^FES_JTAG_SCAN LIBRARY (.+)$',output,re.M)))
 assert paths==['/data/altera_pro/26.3/quartus/linux64/libaji_client.so.0.0.0']
 proofs.append({'stage':name,'original_helper_passed':r['pass'],'forced_scan_codes':r['forced_scan_return_codes'],'calls':calls,'loaded_library_current_sha256':{p:sha(Path(p)) for p in paths},'evidence_sha256':{str(p.relative_to(root)):sha(p) for p in (stage/'source.json',stage/'result.json',b/'transport.json',b/'forced-scan.log',b/'clock-restored-readback.log')}})
usb=Path('/sys/bus/usb/devices/3-11.1');assert (usb/'serial').read_text().strip()=='TRWU8CBL'
node=Path('/dev/bus/usb')/f'{int((usb/"busnum").read_text()):03d}'/f'{int((usb/"devnum").read_text()):03d}'
assert str(node)==r['usb_node'];now=node.stat();old=r['saved']
assert (now.st_uid,now.st_gid,stat.S_IMODE(now.st_mode))==(old['uid'],old['gid'],old['mode'])==(0,0,0o664)
uart=Path('/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8CBL-if02-port0');device=uart.stat().st_rdev;owners=[]
for p in Path('/proc').iterdir():
 if not p.name.isdigit():continue
 try:fds=list((p/'fd').iterdir())
 except (FileNotFoundError,ProcessLookupError):continue
 for fd in fds:
  try:
   v=fd.stat()
   if stat.S_ISCHR(v.st_mode) and v.st_rdev==device:owners.append((p.name,fd.name))
  except (FileNotFoundError,ProcessLookupError):pass
assert not owners
test=json.loads((s/'probe-test-proof.json').read_text());assert test['pass'] and len(test['cases'])==17
proof=dict(status='passed',classification='independent audit of one successful forced live scan and preserved earlier cache-precondition failure',probes=proofs,mock_cases=17,usb_permissions_exact=True,private_servers_gone=True,uart_owners=owners,clock_restoration='15M saved readback, no additional query',audit_device_operations=False,image_programming_attempts_total=2,programming_allowance_remaining=0,programming_failure_explained=False,core_load_acceptance=False)
(s/'audit-proof.json').write_text(json.dumps(proof,indent=2)+'\n');os.chown(s/'audit-proof.json',1000,1000)
print('PASS exact forced-scan call/output, preserved unscanned failure,17 mock cases, frozen sources, both server exits,15M restoration, exact permissions and UART free.')
