#!/usr/bin/env python3
"""Check each desktop-toolbar frame for the frozen boot image's 100% glyph.
Only the top 80 screen rows are searched; unreadable/missing samples fail.
"""
import collections, hashlib, json, subprocess, sys
from pathlib import Path
p=Path(sys.argv[1]); meta=json.loads((p/'speed-trace.geom').read_text()); w,h,fps=(meta[k] for k in ('w','h','fps'))
assert (w,h,fps)==(1280,200,4)
# The private boot grab contains the reference window itself, not the desktop.
boot=p/'fr/boot.png'
dim=subprocess.check_output(['ffprobe','-v','error','-select_streams','v:0','-show_entries','stream=width','-of','csv=p=0',str(boot)],text=True).strip(); x=int(dim)-60
pattern=subprocess.check_output(['ffmpeg','-loglevel','error','-i',str(boot),'-vf',f'crop=56:18:{x}:28,format=gray','-f','rawvideo','-'])
assert len(pattern)==56*18
anchor=pattern[7*56:8*56]
counts=collections.Counter(); missing=[]; dig=hashlib.sha256()
with (p/'speed-trace.gray').open('rb') as f:
 i=0
 while True:
  frame=f.read(w*h)
  if not frame: break
  if len(frame)!=w*h: raise SystemExit('truncated frame')
  found=[]
  for anchor_y in range(7,80):
   row=frame[anchor_y*w:(anchor_y+1)*w]; start=0
   while True:
    pos=row.find(anchor,start)
    if pos<0: break
    y=anchor_y-7
    if y>=0 and y+18<=80 and all(frame[(y+j)*w+pos:(y+j)*w+pos+56]==pattern[j*56:(j+1)*56] for j in range(18)):found.append((pos,y))
    start=pos+1
  if len(found)!=1:missing.append({'frame':i,'matches':found})
  else: counts[str(found[0])]+=1
  dig.update(frame);i+=1
assert i>0
out={'frames':i,'fps':fps,'seconds':i/fps,'toolbar_positions':dict(counts),'unreadable_or_not_100_percent':missing,'raw_sha256':dig.hexdigest(),'template_sha256':hashlib.sha256(pattern).hexdigest(),'note':'Exact screenshot-template match; unreadable samples and any speed other than boot 100% fail. Sampling does not measure between frames.'}
(p/'speed-audit.json').write_text(json.dumps(out,indent=2)+'\n');print(json.dumps({k:v for k,v in out.items() if k!='unreadable_or_not_100_percent'}));print('bad frames:',len(missing));sys.exit(bool(missing))
