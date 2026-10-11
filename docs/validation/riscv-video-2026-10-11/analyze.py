import subprocess,json,hashlib,sys
from pathlib import Path
from collections import Counter
G=[[31,16,30,16,16,16,16],[31,16,30,16,16,16,31],[15,16,16,14,1,1,30],[0]*7,[30,17,17,30,20,18,17],[17,17,17,17,17,10,4],[30,1,1,14,1,1,30],[14,17,1,2,4,8,31],[31,4,4,4,4,4,31]]
results=[]
for name in sys.argv[1:]:
 p=Path(name);ref=None;frames=[]
 for t in range(3,10):
  a=subprocess.run(['ffmpeg','-v','error','-ss',str(t),'-i',str(p),'-frames:v','1','-f','rawvideo','-pix_fmt','rgb24','-'],capture_output=True,check=True).stdout
  assert len(a)==1920*1080*3
  if ref is None:ref=a
  changed=Counter();banner_bad=[]
  # Every capture pixel, excluding a four-pixel compression halo around the box.
  for i,(old,new) in enumerate(zip(ref,a)):
   if abs(old-new)<=30:continue
   y,x=divmod(i//3,1920)
   if 902<=x<1018 and 482<=y<598:continue
   changed[(x,y)]=1
  columns=Counter(x for x,y in changed)
  for y in range(8,22):
   for x in range(26,134):
    off=x-26;col=off%12//2;want=col<5 and bool(G[off//12][(y-8)//2]&(1<<(4-col)))
    i=((y*9+4)*1920+240+x*9+4)*3
    if (min(a[i:i+3])>200)!=want:banner_bad.append([x,y])
  frames.append({'seconds':t,'static_changed_pixels_vs_3s':len(changed),'changed_columns':dict(sorted(columns.items())),'banner_cell_mismatches':banner_bad})
 results.append({'path':str(p.resolve()),'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'size':p.stat().st_size,'frames':frames})
print(json.dumps({'threshold':30,'box_exclusion':[902,482,1018,598],'geometry':[1920,1080],'captures':results},indent=2))
