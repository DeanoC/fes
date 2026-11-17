#!/usr/bin/env python3
"""Wait at most 180 seconds for a frozen screen template twice."""
import os, subprocess, sys, time
from pathlib import Path
expected = Path(sys.argv[1]).read_bytes()
geometry = sys.argv[2]
sizes = {'35:12:0:455': 420, '450:14:0:337': 6300}
if geometry not in sizes or len(expected) != sizes[geometry]:
    raise SystemExit('bad template or geometry')
end=time.monotonic()+180
stable=0
while time.monotonic()<end:
    frame=subprocess.run(['ffmpeg','-v','error','-f','x11grab','-video_size','800x520','-i',os.environ['DISPLAY']+'+0,0','-vf','crop='+geometry+',format=gray','-frames:v','1','-f','rawvideo','-'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=5,check=True).stdout
    if len(frame)!=len(expected): raise SystemExit('bad frame')
    errors=sum((x>128)!=(y>128) for x,y in zip(frame,expected))
    stable=stable+1 if errors<=2 else 0
    print('prompt-errors',errors,'stable',stable,flush=True)
    if stable>=2: sys.exit(0)
    time.sleep(1)
raise SystemExit('prompt timeout')
