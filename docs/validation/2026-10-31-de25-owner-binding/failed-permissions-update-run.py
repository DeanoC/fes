import pathlib,sys,subprocess,tempfile,os,json,hashlib,datetime,time
root=pathlib.Path.cwd();sys.path.insert(0,str(root))
from scripts import de25_first_boot as auth
out=root/'out/owner-mdev-network-update-c778626e5';known=root/'out/owner-bus-uart-20261031/known_hosts';password=root/'out/owner-candidate-uart-20261031/password';key=auth.host_key(known.read_bytes(),'192.168.10.137',22);base=auth.ssh_argv('192.168.10.137',22,'root',known,None,key)
record={'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'board':'TRWU8J8G','boot_id':'aef6b113-efe8-4d0c-b164-efbf302bfd70','bundle_sha256':hashlib.sha256((out/'bundle.tar').read_bytes()).hexdigest(),'phases':[],'reboot':'not-issued'};start=time.monotonic()
with tempfile.TemporaryDirectory() as tmp:
 ask=pathlib.Path(tmp)/'askpass';ask.write_text('#!/bin/sh\nexec /bin/cat "$FES_DE25_PASSWORD_FILE"\n');ask.chmod(0o700)
 env={'PATH':os.environ['PATH'],'LC_ALL':'C','SSH_ASKPASS':str(ask),'SSH_ASKPASS_REQUIRE':'force','DISPLAY':'de25:0','FES_DE25_PASSWORD_FILE':str(password)}
 phases=[('upload','umask 077; test ! -e /tmp/fes-owner-mdev-c778626e5 && mkdir /tmp/fes-owner-mdev-c778626e5 && tar -xf - -C /tmp/fes-owner-mdev-c778626e5',(out/'bundle.tar').read_bytes(),60),('install','sh /tmp/fes-owner-mdev-c778626e5/install.sh',None,60),('syscalls','/tmp/fes-owner-mdev-c778626e5/owner_unconfigured --boot-id aef6b113-efe8-4d0c-b164-efbf302bfd70',None,30)]
 for name,command,data,timeout in phases:
  r=subprocess.run([*base[:-1],command],input=data,capture_output=True,timeout=timeout,env=env)
  (out/(name+'.stdout')).write_bytes(r.stdout);(out/(name+'.stderr')).write_bytes(r.stderr);record['phases'].append({'phase':name,'returncode':r.returncode,'elapsed_seconds':round(time.monotonic()-start,1)});(out/'receipt.json').write_text(json.dumps(record,indent=2)+'\n');print(name,r.returncode,r.stdout.decode(),flush=True)
  if r.returncode:raise RuntimeError(name+' failed; inspect retained logs')
  if name=='syscalls':assert b'DE25_OWNER_UNCONFIGURED_USERSPACE_PASS' in r.stdout
record['state']='persistent-rule-and-actual-userspace-pass';(out/'receipt.json').write_text(json.dumps(record,indent=2)+'\n')
