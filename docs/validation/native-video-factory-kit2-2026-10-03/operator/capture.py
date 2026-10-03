import hashlib,json,subprocess,sys,time
from pathlib import Path
name=sys.argv[1];out=Path(sys.argv[2]);time.sleep(3)
base=['ffmpeg','-hide_banner','-nostdin','-y','-loglevel','warning']
commands=[base+['-f','v4l2','-input_format','yuyv422','-video_size','1280x720','-framerate','60','-i','/dev/v4l/by-id/usb-ASUS_4KPRO_802B003090700329-video-index0','-frames:v','180','-c:v','ffv1','-level','3','-pix_fmt','yuv422p',str(out/(name+'.mkv'))],base+['-f','alsa','-ar','48000','-ac','2','-i','hw:CARD=D4KPRO,DEV=0','-t','6','-c:a','pcm_s16le',str(out/(name+'.wav'))]]
logs=[(out/(name+'-video.log')).open('w'),(out/(name+'-audio.log')).open('w')];procs=[]
try:
 for cmd,log in zip(commands,logs):procs.append(subprocess.Popen(cmd,stdout=subprocess.DEVNULL,stderr=log))
 for proc in procs:
  if proc.wait(timeout=30)!=0:raise RuntimeError('Capture failed; inspect local capture logs')
finally:
 cleanup_errors=[]
 for proc in procs:
  if proc.poll() is None:
   try:proc.terminate()
   except ProcessLookupError:pass
   except OSError as error:cleanup_errors.append(type(error).__name__)
 for proc in procs:
  try:
   try:proc.wait(timeout=10)
   except subprocess.TimeoutExpired:proc.kill();proc.wait(timeout=10)
  except (OSError,subprocess.TimeoutExpired) as error:cleanup_errors.append(type(error).__name__)
 for log in logs:log.close()
 if cleanup_errors:raise RuntimeError('Owned capture cleanup failed: '+','.join(cleanup_errors))
subprocess.run(base+['-i',str(out/(name+'.mkv')),'-vf','select=eq(n\\,120)','-frames:v','1','-update','1',str(out/(name+'.png'))],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
files={}
for suffix in ('.mkv','.wav','.png'):
 p=out/(name+suffix);p.chmod(0o600);files[suffix]={'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'size':p.stat().st_size}
video=json.loads(subprocess.check_output(['ffprobe','-v','error','-count_frames','-show_streams','-of','json',str(out/(name+'.mkv'))]));audio=json.loads(subprocess.check_output(['ffprobe','-v','error','-show_streams','-of','json',str(out/(name+'.wav'))]))
p=out/(name+'-capture.json');p.write_text(json.dumps({'settle_seconds':3,'device':'ASUS 4KPRO','files':files,'video':video,'audio':audio},indent=2)+'\n');p.chmod(0o600)
print(json.dumps({'stage':'captured','case':name,'frames':video['streams'][0]['nb_read_frames']}),flush=True)
