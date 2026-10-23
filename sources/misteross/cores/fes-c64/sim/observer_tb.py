#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
from pathlib import Path
import subprocess,json,hashlib,csv
module=Path(__file__).resolve().parents[3]

# Keep one complete frame after the first active-first-raster VS edge.
SYS_CYCLES=1740800  # 2 * 1650 * 750 pixels at 74.25/52.224 MHz.
EXTENDED_SYS_CYCLES=SYS_CYCLES+96*1088

def run(medium_oracle):
    p=module/'build/sim/fes-c64-observer'/('oracle' if medium_oracle else 'rtl')
    p.mkdir(parents=True,exist_ok=True)
    fixture=module/'build/sim/fes-c64-checkpoint'
    firmware=bytes(int(line,16) for line in (fixture/'firmware.hex').read_text().splitlines())
    (p/'basic.rom').write_bytes(firmware[:8192]);(p/'kernal.rom').write_bytes(firmware[8192:16384]);(p/'chargen.rom').write_bytes(firmware[16384:20480])
    (p/'drive.rom').write_bytes(bytes(int(line,16) for line in (fixture/'drive.hex').read_text().splitlines()))
    for i in range(3):(p/f'disk{i}.d64').write_bytes(bytes((n*37+i*41)&255 for n in range(174848)))
    base=['python3',str(module/'scripts/c64_boot_observe.py'),'--basic',str(p/'basic.rom'),'--kernal',str(p/'kernal.rom'),'--chargen',str(p/'chargen.rom'),'--drive-rom',str(p/'drive.rom'),'--disk',str(p/'disk0.d64'),'--cycles',str(SYS_CYCLES)]
    if medium_oracle:base.append('--medium-oracle')
    original=p/'helper original ü "x"';restored=p/'helper restored µ'
    original.mkdir(exist_ok=True);restored.mkdir(exist_ok=True)
    state=original/'saved.state'
    with (p/'helper-continuous.log').open('w') as log:
     subprocess.run(base+['--late-video','--sid-trace','--audio-capture','--output',str(original),'--save-state',str(state),'--save-at-cycles','1031119','--save-final-state',str(original/'end.state'),'--disk-change','1000000',str(p/'disk1.d64'),'--disk-change','1200000',str(p/'disk2.d64')],stdout=log,stderr=subprocess.STDOUT,check=True)
    with (p/'helper-restored.log').open('w') as log:
     subprocess.run(base+['--sid-trace','--audio-capture','--output',str(restored),'--restore-state',str(state),'--save-final-state',str(restored/'end.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
    for name in ['boot-ram.bin','drive-ram.bin','boot.ppm','end.state']:
     assert (original/name).read_bytes()==(restored/name).read_bytes(),name
    metadata=json.loads(state.with_name(state.name+'.json').read_text())
    receipt=json.loads((restored/'observation.json').read_text())
    assert [e['cycle'] for e in receipt['disk_changes']]==[1000000,1200000]
    assert metadata['model_input_identity']
    assert metadata['state_schema']=='FES-C64-development-state-v4'
    def stores(path):
        with (path/'sid-writes.csv').open() as source:
            return list(csv.DictReader(source))
    assert stores(restored)==[row for row in stores(original) if int(row['sys_cycle'])>=1031119]
    assert (restored/'audio.pcm').read_bytes()==(original/'audio.pcm').read_bytes()[2*(1031119//1088):]
    for path in (original,restored):
        observed=json.loads((path/'observation.json').read_text())
        for name in ('capture.json','sid-writes.csv','audio.pcm'):
            assert observed['outputs'][name]==hashlib.sha256((path/name).read_bytes()).hexdigest()
        assert observed['capture']['end_cycle_exclusive']==SYS_CYCLES
    # Reject the previous private format before starting any simulator.
    legacy=p/'legacy.state';legacy.write_bytes(state.read_bytes())
    legacy_metadata=dict(metadata,state_schema='FES-C64-development-state-v3')
    legacy.with_name(legacy.name+'.json').write_text(json.dumps(legacy_metadata))
    with (p/'helper-legacy-state.log').open('w') as log:
        result=subprocess.run(base+['--output',str(p/'legacy-output'),'--restore-state',str(legacy)],stdout=log,stderr=subprocess.STDOUT)
    assert result.returncode!=0 and 'requires recapture' in (p/'helper-legacy-state.log').read_text()
    assert metadata['late_video_capture'] and receipt['late_video_capture']
    assert json.loads((original/'end.state.json').read_text())['system_cycle']==SYS_CYCLES
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
     subprocess.run(base+['--sid-trace','--output',str(branch),'--restore-state',str(state),'--append-disk-change','1420000',str(p/'disk0.d64'),'--save-final-state',str(branch/'end.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
    with (p/'helper-reference.log').open('w') as log:
     subprocess.run(base+['--late-video','--audio-capture','--output',str(reference),'--disk-change','1000000',str(p/'disk1.d64'),'--disk-change','1200000',str(p/'disk2.d64'),'--disk-change','1420000',str(p/'disk0.d64'),'--save-final-state',str(reference/'end.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
    for name in ['boot-ram.bin','drive-ram.bin','boot.ppm','end.state']:
     assert (branch/name).read_bytes()==(reference/name).read_bytes(),name
    receipt=json.loads((branch/'observation.json').read_text());assert len(receipt['disk_changes'])==3
    assert receipt['capture']['sid_trace_enabled'] and not receipt['capture']['audio_enabled']
    assert not (branch/'audio.pcm').exists()
    reference_receipt=json.loads((reference/'observation.json').read_text())
    assert reference_receipt['capture']['audio_enabled'] and not reference_receipt['capture']['sid_trace_enabled']
    assert not (reference/'sid-writes.csv').exists(), 'stale capture survived output reuse'
    with (p/'helper-past-event.log').open('w') as log:
     result=subprocess.run(base+['--output',str(p/'past-event'),'--restore-state',str(state),'--append-disk-change','1031119',str(p/'disk0.d64')],stdout=log,stderr=subprocess.STDOUT)
    assert result.returncode!=0,'past event accepted'
    print('appended future insertion matches continuous full state; immutable pool changes and past events rejected')
    if not medium_oracle:
        with (p/'helper-overlapping-upload.log').open('w') as log:
            result=subprocess.run(base+['--output',str(p/'overlapping-upload'),
                '--disk-change','1000000',str(p/'disk1.d64'),
                '--disk-change','1000001',str(p/'disk2.d64')],stdout=log,stderr=subprocess.STDOUT)
        assert result.returncode!=0,'overlapping D64 uploads accepted'
        assert '87425 system cycles' in (p/'helper-overlapping-upload.log').read_text()


    # Hold A across a checkpoint with its release already queued, then append
    # B press/release after that queue tail. Compare with cold prequeued input.
    key_state=p/'keys.state';key_branch=p/'keys appended';key_branch.mkdir(exist_ok=True)
    hold=['--key-event','500000','4','1','--key-event','1300000','4','0']
    press=['--key-event','1400000','5','1','--key-event','1500000','5','0']
    common=['--late-video','--sid-trace','--audio-capture','--disk-change','1000000',str(p/'disk1.d64'),'--disk-change','1200000',str(p/'disk2.d64')]
    with (p/'helper-key-split.log').open('w') as log:
        subprocess.run(base+common+hold+['--output',str(original),'--save-state',str(key_state),'--save-at-cycles','1031119'],stdout=log,stderr=subprocess.STDOUT,check=True)
    with (p/'helper-key-appended.log').open('w') as log:
        subprocess.run(base+['--sid-trace','--audio-capture','--output',str(key_branch),'--restore-state',str(key_state),'--append-key-event','1400000','5','1','--append-key-event','1500000','5','0','--save-final-state',str(key_branch/'end.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
    with (p/'helper-key-reference.log').open('w') as log:
        subprocess.run(base+common+hold+press+['--output',str(original),'--save-final-state',str(original/'end.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
    for name in ['boot-ram.bin','drive-ram.bin','boot.ppm','end.state']:
        assert (key_branch/name).read_bytes()==(original/name).read_bytes(),name
    assert stores(key_branch)==[row for row in stores(original)if int(row['sys_cycle'])>=1031119]
    assert (key_branch/'audio.pcm').read_bytes()==(original/'audio.pcm').read_bytes()[2*(1031119//1088):]
    key_receipt=json.loads((key_branch/'observation.json').read_text())
    assert key_receipt['key_events']==[[500000,4,1],[1300000,4,0],[1400000,5,1],[1500000,5,0]]
    assert key_receipt['appended_key_events']==[[1400000,5,1],[1500000,5,0]]
    assert json.loads((key_branch/'end.state.json').read_text())['key_event_count']==4
    for label,fields in [('past',['1031119','5','1']),('tail',['1200000','5','1']),('usage',['1400000','128','1']),('down',['1400000','5','2']),('negative',['-1','5','1'])]:
        with (p/f'helper-key-rejected-{label}.log').open('w') as log:
            result=subprocess.run(base+['--output',str(p/f'key-invalid-{label}'),'--restore-state',str(key_state),'--append-key-event']+fields,stdout=log,stderr=subprocess.STDOUT)
        assert result.returncode!=0,label
    # A second restore must retain the accumulated queue without resubmitting it.
    twice=p/'keys restored twice';twice.mkdir(exist_ok=True)
    longer=[str(EXTENDED_SYS_CYCLES)if x==str(SYS_CYCLES)else x for x in base]
    for target,statefile in [(twice,key_branch/'end.state'),(original,original/'end.state')]:
        with (p/('helper-key-twice-'+target.name+'.log')).open('w') as log:
            subprocess.run(longer+['--output',str(target),'--restore-state',str(statefile),'--save-final-state',str(target/'long.state')],stdout=log,stderr=subprocess.STDOUT,check=True)
    assert (twice/'long.state').read_bytes()==(original/'long.state').read_bytes()
    assert json.loads((twice/'observation.json').read_text())['key_events']==key_receipt['key_events']
    print('appended keyboard input preserves held keys, release queue, full state, PCM/SID and subsequent restores; invalid input rejected')

    with (p/'helper-past-save.log').open('w') as log:
     result=subprocess.run(base+['--output',str(p/'past-save'),'--restore-state',str(state),'--save-state',str(p/'invalid.state'),'--save-at-cycles','700000'],stdout=log,stderr=subprocess.STDOUT)
    assert result.returncode!=0,'checkpoint save time predates restored clock'


if __name__=='__main__':
    run(False)
    run(True)
