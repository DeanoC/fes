#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
from pathlib import Path
import subprocess,json,hashlib
module=Path(__file__).resolve().parents[3]
p=module/'build/sim/fes-c64-observer'
p.mkdir(parents=True,exist_ok=True)
fixture=module/'build/sim/fes-c64-checkpoint'
firmware=bytes(int(line,16) for line in (fixture/'firmware.hex').read_text().splitlines())
(p/'basic.rom').write_bytes(firmware[:8192]);(p/'kernal.rom').write_bytes(firmware[8192:16384]);(p/'chargen.rom').write_bytes(firmware[16384:])
(p/'drive.rom').write_bytes(bytes(int(line,16) for line in (fixture/'drive.hex').read_text().splitlines()))
for i in range(3):(p/f'disk{i}.d64').write_bytes(bytes((n*37+i*41)&255 for n in range(174848)))
base=['python3',str(module/'scripts/c64_boot_observe.py'),'--real-drive','--basic',str(p/'basic.rom'),'--kernal',str(p/'kernal.rom'),'--chargen',str(p/'chargen.rom'),'--drive-rom',str(p/'drive.rom'),'--disk',str(p/'disk0.d64'),'--cycles','1566720']
original=p/'helper original ü "x"';restored=p/'helper restored µ'
original.mkdir(exist_ok=True);restored.mkdir(exist_ok=True)
state=original/'saved.state'
with (p/'helper-continuous.log').open('w') as log:
 subprocess.run(base+['--late-video','--output',str(original),'--save-state',str(state),'--save-at-cycles','731119','--save-final-state',str(original/'end.state'),'--disk-change','1000000',str(p/'disk1.d64'),'--disk-change','1200000',str(p/'disk2.d64')],stdout=log,stderr=subprocess.STDOUT,check=True)
with (p/'helper-restored.log').open('w') as log:
 subprocess.run(base+['--output',str(restored),'--restore-state',str(state),'--save-final-state',str(restored/'end.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
for name in ['boot-ram.bin','drive-ram.bin','boot.ppm','end.state']:
 assert (original/name).read_bytes()==(restored/name).read_bytes(),name
metadata=json.loads(state.with_name(state.name+'.json').read_text())
receipt=json.loads((restored/'observation.json').read_text())
assert [e['cycle'] for e in receipt['disk_changes']]==[1000000,1200000]
assert metadata['model_input_identity']
assert metadata['late_video_capture'] and receipt['late_video_capture']
assert json.loads((original/'end.state.json').read_text())['system_cycle']==1566720
for label,extra in [('time',['--disk-change','1000001',str(p/'disk1.d64'),'--disk-change','1200000',str(p/'disk2.d64')]),('bytes',['--disk-library',str(p/'disk2.d64'),'--disk-library',str(p/'disk2.d64')])]:
 with (p/f'helper-rejected-{label}.log').open('w') as log:
  result=subprocess.run(base+['--output',str(p/f'rejected-{label}'),'--restore-state',str(state)]+extra,stdout=log,stderr=subprocess.STDOUT)
 assert result.returncode!=0,label
print('observer: automatic schedule restore, full RAM/frame matches, relocated Unicode/quoted output paths; altered times/media hashes rejected')

branch=p/'helper appended';reference=original
# Saved RTL includes ROM filenames. Use the original paths for the cold
# reference so complete serialized state can be compared without normalization.
branch.mkdir(exist_ok=True);reference.mkdir(exist_ok=True)
with (p/'helper-append.log').open('w') as log:
 subprocess.run(base+['--output',str(branch),'--restore-state',str(state),'--append-disk-change','1420000',str(p/'disk0.d64'),'--save-final-state',str(branch/'end.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
with (p/'helper-reference.log').open('w') as log:
 subprocess.run(base+['--late-video','--output',str(reference),'--disk-change','1000000',str(p/'disk1.d64'),'--disk-change','1200000',str(p/'disk2.d64'),'--disk-change','1420000',str(p/'disk0.d64'),'--save-final-state',str(reference/'end.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
for name in ['boot-ram.bin','drive-ram.bin','boot.ppm','end.state']:
 assert (branch/name).read_bytes()==(reference/name).read_bytes(),name
receipt=json.loads((branch/'observation.json').read_text());assert len(receipt['disk_changes'])==3
with (p/'helper-past-event.log').open('w') as log:
 result=subprocess.run(base+['--output',str(p/'past-event'),'--restore-state',str(state),'--append-disk-change','731119',str(p/'disk0.d64')],stdout=log,stderr=subprocess.STDOUT)
assert result.returncode!=0,'past event accepted'
print('appended future insertion matches continuous full state; immutable pool changes and past events rejected')

with (p/'helper-past-save.log').open('w') as log:
 result=subprocess.run(base+['--output',str(p/'past-save'),'--restore-state',str(state),'--save-state',str(p/'invalid.state'),'--save-at-cycles','700000'],stdout=log,stderr=subprocess.STDOUT)
assert result.returncode!=0,'checkpoint save time predates restored clock'
