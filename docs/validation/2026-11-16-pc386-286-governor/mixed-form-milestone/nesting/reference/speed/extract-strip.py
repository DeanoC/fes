#!/usr/bin/env python3
"""Extract the 56x18 toolbar speed glyph at x=690,y=28 from 1280x200 gray frames."""
import gzip,hashlib,json,sys
from pathlib import Path
raw=Path(sys.argv[1]);dest=Path(sys.argv[2]);w,h=1280,200;strip_hash=hashlib.sha256();full_hash=hashlib.sha256();n=0
with raw.open('rb') as src,dest.open('wb') as out,gzip.GzipFile(filename='',mode='wb',fileobj=out,mtime=0) as gz:
 while True:
  frame=src.read(w*h)
  if not frame:break
  if len(frame)!=w*h:raise SystemExit('partial frame')
  full_hash.update(frame);strip=b''.join(frame[y*w+690:y*w+746] for y in range(28,46));strip_hash.update(strip);gz.write(strip);n+=1
assert n==1020
print(json.dumps({'frames':n,'fps':4,'strip_width':56,'strip_height':18,'x':690,'y':28,'full_raw_sha256':full_hash.hexdigest(),'strip_raw_sha256':strip_hash.hexdigest(),'gzip_sha256':hashlib.sha256(dest.read_bytes()).hexdigest()},indent=2))
