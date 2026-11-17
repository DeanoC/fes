#!/usr/bin/env python3
"""Read-only identity and result-absence checks immediately before VM start."""
from pathlib import Path
import hashlib, json, os, subprocess, sys
p=Path(__file__).resolve().parent; q=p/'reference-vm'; disk=(q/'run'/'disk.img').resolve(); expected='29ea2eea8330c54a95786b98e4c973148bcf11366e8415d18bb4681d7a95768a'
def digest(path): return hashlib.sha256(path.read_bytes()).hexdigest()
assert disk.parent==(q/'run').resolve() and not (q/'run/disk.img').is_symlink() and disk.stat().st_nlink==1
assert digest(disk)==expected and digest(q/'prerun-disk-readonly.img')==expected
assert digest(q/'SURVEY-readback.COM')==digest(p/'SURVEY.COM')=='23f8f0392d651254737ec33ae80213e85f7a734d2239988fb973de3a0e8d2f6b'
assert 'hdd_01_fn = '+str(disk) in (q/'vm/86box.cfg').read_text()
env={**os.environ,'MTOOLSRC':'/dev/null'}
listing=subprocess.run(['mdir','-i',f'{disk}@@32256','::/'],env=env,capture_output=True,check=True,text=True).stdout
for name in ('CM1','CM2','CM3'):
 assert not any(line.split()[:2]==[name,'TXT'] for line in listing.splitlines()), name
readback=subprocess.run(['mcopy','-i',f'{disk}@@32256','::/SURVEY.COM','-'],env=env,capture_output=True,check=True).stdout
assert hashlib.sha256(readback).hexdigest()==digest(p/'SURVEY.COM')
print(json.dumps({'verified_disk':str(disk),'disk_sha256':expected,'readback_sha256':digest(q/'SURVEY-readback.COM'),'result_filenames_absent':['CM1.TXT','CM2.TXT','CM3.TXT']},indent=2))
