import datetime,json,os,signal,subprocess,sys,threading,time,shutil
from pathlib import Path
w=Path(sys.argv[1]).resolve();cap=600
if shutil.disk_usage(w).free<30*1024**3:raise SystemExit('refuse: under 30GiB')
start=time.monotonic();done=threading.Event()
command=['/home/deano/intelFPGA_lite/17.0/quartus/bin/quartus_sta','-t',str(Path(sys.argv[2]).resolve())]
(w/'conditional-command.json').write_text(json.dumps({'command':command,'cwd':str(w),'cap_seconds':cap,'start_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2)+'\n')
def beat():
 while not done.wait(15):
  (w/'conditional-heartbeat.json').write_text(json.dumps({'elapsed_s':round(time.monotonic()-start),'pid':os.getpid()})+'\n')
  print('SDRAM STA heartbeat',round(time.monotonic()-start),flush=True)
threading.Thread(target=beat,daemon=True).start()
with (w/'conditional-sta.log').open('w') as log:
 p=subprocess.Popen(command,cwd=w,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 try:code=p.wait(timeout=cap)
 except subprocess.TimeoutExpired:
  os.killpg(p.pid,signal.SIGTERM)
  try:p.wait(timeout=10)
  except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);p.wait()
  code=124
result={'exit_code':code,'elapsed_s':round(time.monotonic()-start,3),'classification':'conditional postfit STA only; no build/seal/hardware acceptance'}
(w/'conditional-result.json').write_text(json.dumps(result,indent=2)+'\n');done.set();print('SDRAM STA finished',json.dumps(result),flush=True);raise SystemExit(code)
