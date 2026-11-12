import pathlib,sys,subprocess,tempfile,os,json,hashlib,datetime,time
root=pathlib.Path.cwd();sys.path.insert(0,str(root))
from scripts import de25_first_boot as auth
out=root/'out/owner-bus-network-update-8e7479d48';creds=root/'out/owner-candidate-uart-20261031';key=auth.host_key((creds/'known_hosts').read_bytes(),'192.168.10.165',22);base=auth.ssh_argv('192.168.10.165',22,'root',creds/'known_hosts',None,key)
record={'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'board':'TRWU8J8G','boot_id':'b989f017-f9f3-45dd-811b-0482b1b65421','bundle_sha256':hashlib.sha256((out/'bundle.tar').read_bytes()).hexdigest(),'installer_sha256':hashlib.sha256((out/'bundle/install.sh').read_bytes()).hexdigest(),'phases':[],'reboot':'not-issued'}
start=time.monotonic()
with tempfile.TemporaryDirectory() as tmp:
 ask=pathlib.Path(tmp)/'askpass';ask.write_text('#!/bin/sh\nexec /bin/cat "$FES_DE25_PASSWORD_FILE"\n');ask.chmod(0o700)
 env={'PATH':os.environ['PATH'],'LC_ALL':'C','SSH_ASKPASS':str(ask),'SSH_ASKPASS_REQUIRE':'force','DISPLAY':'de25:0','FES_DE25_PASSWORD_FILE':str(creds/'password')}
 for name,command,data,timeout in [('upload','umask 077; test ! -e /tmp/fes-owner-bus-8e7479d48 && mkdir /tmp/fes-owner-bus-8e7479d48 && tar -xf - -C /tmp/fes-owner-bus-8e7479d48',(out/'bundle.tar').read_bytes(),120),('install','sh /tmp/fes-owner-bus-8e7479d48/install.sh',None,180)]:
  result=subprocess.run([*base[:-1],command],input=data,capture_output=True,timeout=timeout,env=env)
  (out/(name+'.stdout')).write_bytes(result.stdout);(out/(name+'.stderr')).write_bytes(result.stderr)
  record['phases'].append({'phase':name,'returncode':result.returncode,'elapsed_seconds':round(time.monotonic()-start,1)});(out/'receipt.json').write_text(json.dumps(record,indent=2)+'\n');print(json.dumps(record),flush=True)
  if result.returncode:raise RuntimeError(name+' failed; inspect retained logs before retry')
  if name=='install':assert b'DE25_OWNER_BUS_UPDATE_STAGED_PASS' in result.stdout
record['state']='staged-and-verified';(out/'receipt.json').write_text(json.dumps(record,indent=2)+'\n');print('NETWORK_UPDATE_STAGED_PASS',flush=True)
