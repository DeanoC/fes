# SPDX-License-Identifier: GPL-3.0-or-later
"""Compare the fixed BIG logo ROI in decoded HDMI input frames."""
from pathlib import Path
import argparse,collections,hashlib,json,subprocess
p=argparse.ArgumentParser();p.add_argument('capture');p.add_argument('output');a=p.parse_args()
frame_bytes=720*192*3
command=['ffmpeg','-hide_banner','-loglevel','error','-i',a.capture,'-vf','crop=720:192:260:60','-vsync','0','-pix_fmt','rgb24','-f','rawvideo','-']
frames=[]
with subprocess.Popen(command,stdout=subprocess.PIPE,stderr=subprocess.PIPE) as proc:
 while True:
  data=proc.stdout.read(frame_bytes)
  if not data:break
  if len(data)!=frame_bytes:raise RuntimeError('partial logo frame')
  frames.append({'index':len(frames),'sha256':hashlib.sha256(data).hexdigest(),'mean_rgb':sum(data)/len(data)})
 errors=proc.stderr.read().decode();code=proc.wait()
 if code:raise RuntimeError(errors)
settled=[f for f in frames if f['mean_rgb']>1]
result={'frame_indexing':'decoded input frames; timestamp resampling disabled','capture_file':Path(a.capture).name,'capture_sha256':hashlib.sha256(Path(a.capture).read_bytes()).hexdigest(),'crop_xywh':[260,60,720,192],'native_crop_xywh':[65,0,180,64],'frame_count':len(frames),'excluded_black_frames':len(frames)-len(settled),'settled_frame_count':len(settled),'unique_settled_sha256':len({f['sha256'] for f in settled}),'mean_rgb_range':[min(f['mean_rgb'] for f in settled),max(f['mean_rgb'] for f in settled)] if settled else None,'hash_counts':dict(collections.Counter(f['sha256'] for f in settled)),'frames':frames}
Path(a.output).write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps({k:v for k,v in result.items() if k not in ('frames','hash_counts')}))
