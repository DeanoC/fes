#!/usr/bin/env python3
"""Verify the board evidence archive without original sources/tools/baseline."""
from pathlib import Path
import argparse,hashlib,io,json,tarfile
p=argparse.ArgumentParser();p.add_argument('directory',nargs='?',type=Path,default=Path(__file__).resolve().parent);a=p.parse_args();R=a.directory
H=lambda b:hashlib.sha256(b).hexdigest()
def members(data):
 with tarfile.open(fileobj=io.BytesIO(data)) as t:
  names=[m.name for m in t.getmembers()];assert len(names)==len(set(names))
  assert all(m.isfile() for m in t.getmembers())
  return {n:t.extractfile(n).read() for n in names}
manifest=json.loads((R/'board-evidence.json').read_text());archive=(R/'board-capture.tar.gz').read_bytes();assert H(archive)==manifest['archive_sha256'];data=members(archive)
assert {n:H(b) for n,b in data.items()}==manifest['files_sha256']
result={'outer_members':len(data),'nested':{},'source_archives':{},'native_routes':0,'external_baseline':'Not embedded or independently checked by this archive-only readback.'}
for label in ['standalone','subsets']:
 rec=json.loads(data[label+'-evidence.json']);raw=data[label+'-capture.tar.gz'];assert H(raw)==rec.get('capture_sha256',rec.get('archive_sha256'))
 sub=members(raw);assert {n:H(b) for n,b in sub.items()}==rec['files_sha256'];result['nested'][label]=len(sub)
for label in ['board','board-AP']:
 snapshot=json.loads(data[label+'/snapshot.json']);raw=data[label+'/derived-source.tar.gz'];assert H(raw)==snapshot['derived_archive_sha256']
 sub=members(raw);assert {n:H(b) for n,b in sub.items()}==snapshot['files_sha256'];result['source_archives'][label]=len(sub)
 rec=json.loads(data[label+'/receipt.json']);assert rec['returncode']==0
 for n,h in rec['artifacts'].items():assert H(data[label+'/synthesis/'+n])==h
 comparison=json.loads(data[label+'/comparison.json']);assert comparison['candidate']['synth_sha256']==H(data[label+'/synthesis/synth.json'])
 assert comparison['candidate']['receipt_sha256']==H(data[label+'/receipt.json'])
result['status']='PASS';print(json.dumps(result,indent=2))
