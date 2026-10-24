#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Compare uninterrupted/restored trajectories using original test firmware."""
import hashlib
import csv
import json
from pathlib import Path
import subprocess
import sys

MODULE=Path(__file__).resolve().parents[3]
sys.path.insert(0,str(MODULE))
from scripts import c64_gcr

# The active-first CTA raster raises VS after its active 720 lines. Run for two
# full raster periods so the first VS edge is followed by a complete frame.
SYS_CYCLES=1740800  # 2 * 1650 * 750 pixels at 74.25/52.224 MHz.


def check_sid_stores(stores):
    # Independent expectations from the fixture's 6502 instruction stream.
    expected=[(0xd400,0x34),(0xd401,0x12),(0xd405,0),(0xd406,0xf0),
              (0xd404,0x21),(0xd418,15),(0xd418,15),(0xd500,0x66),
              (0xd418,0),(0xd418,1),(0xd418,15)]
    assert [(r['address'],r['value']) for r in stores[:len(expected)]]==expected
    assert all((r['address'],r['value'])==(0xd418,15) for r in stores[len(expected):])
    assert [r['delta_phi'] for r in stores[:12]]==[-1,6,6,6,6,6,4,22,5,1,6,12]
    assert all(r['delta_phi']==15 for r in stores[12:])
    # Eleven setup stores, then the 15-cycle loop beginning at Phi135.
    last_phi=(SYS_CYCLES-26)*985248//52224000
    assert len(stores)==11+(last_phi-135)//15+1, 'accepted store count differs from the program'
    for index,row in enumerate(stores,1):
        assert row['store_index']==index and row['register']==row['address']%32
        # Independent rational clock crossing: Phi p is at ceil(p*S/C)-1.
        phi_edge=(row['phi_cycle']*52224000+985247)//985248-1
        assert row['sys_cycle']==phi_edge+27, 'store is not on the physical accepting edge'


def main():
    output=MODULE/'build/sim/fes-c64-checkpoint'
    output.mkdir(parents=True,exist_ok=True)
    kernal=bytearray([0xea]*8192)
    # Original real-CPU fixture: disable badlines, configure a saw voice,
    # repeat a store, hide I/O, use a SID mirror, and issue both RMW writes.
    # SID's documented current unreadable-register value is zero, so INC
    # $D418 writes 00 then 01. This does not qualify physical SID bus decay.
    program=bytes.fromhex('78 a2 ff 9a a9 00 8d 11 d0 a9 37 85 01 a9 07 85 00 '
        'a9 34 8d 00 d4 a9 12 8d 01 d4 a9 00 8d 05 d4 '
        'a9 f0 8d 06 d4 a9 21 8d 04 d4 a9 0f 8d 18 d4 8d 18 d4 '
        'a9 33 85 01 a9 55 8d 00 d4 a9 37 85 01 a9 66 8d 00 d5 '
        'ee 18 d4 a9 0f 8d 18 d4 a9 2a 8d 00 c0')
    loop=0xe000+len(program)
    program+=bytes.fromhex('a9 0f 8d 18 d4 ee 00 c0')+bytes([0x4c,loop&255,loop>>8])
    kernal[:len(program)]=program
    kernal[-6:]=bytes.fromhex('00 e0 00 e0 00 e0')
    drive=bytearray([0xea]*16384)
    # Enable the motor/read channel and continuously change drive RAM.
    drive[:19]=bytes.fromhex('78 a2 ff 9a a9 6f 8d 02 1c a9 6c 8d 00 1c e6 00 4c 0e c0')
    drive[-6:]=bytes.fromhex('00 c0 00 c0 00 c0')
    disk=bytes((i*37)&255 for i in range(174848))
    second=bytes(value^255 for value in disk)
    for name,data in [('firmware',bytes(8192)+kernal+bytes(4096)+drive),('drive',drive),('disk',disk)]:
        (output/f'{name}.hex').write_text(''.join(f'{b:02x}\n' for b in data))
    (output/'disk.g64').write_bytes(c64_gcr.g64(disk))
    (output/'second.g64').write_bytes(c64_gcr.g64(second))
    # Hold a key across the snapshot; its release must remain queued.
    (output/'keys.txt').write_text('500000 4 1\n1000000 4 0\n')
    (output/'disk.d64').write_bytes(disk)
    (output/'second.d64').write_bytes(second)
    (output/'changes.txt').write_text('700000 0\n1100000 1\n')
    (output/'first-change.txt').write_text('700000 0\n')
    (output/'append-change.txt').write_text('1100000 1\n')
    for mode in ('rtl','oracle'):
        build='fes-c64-boot-oracle' if mode=='oracle' else 'fes-c64-boot'
        binary=MODULE/f'build/sim/{build}/Vc64_boot_sim_top'
        identity=hashlib.sha256(binary.read_bytes()).hexdigest()
        base=[str(binary),f'+C64_STATE_ID={identity}',f'+C64_FIRMWARE_HEX={output}/firmware.hex',
              f'+C64_DISK_HEX={output}/disk.hex',f'+C64_KEY_EVENTS={output}/keys.txt',
              f'+C64_SYS_CYCLES={SYS_CYCLES}']
        suffix='g64' if mode=='oracle' else 'd64'
        (output/'library.txt').write_text(''.join('"'+str(output/(name+'.'+suffix))+'"\n'
                                               for name in ('disk','second')))
        base += [f'+C64_DISK_LIBRARY={output}/library.txt',f'+C64_DISK_CHANGES={output}/changes.txt']
        if mode=='oracle':base.append(f'+C64_G64_PATH={output}/disk.g64')
        paths={phase:output/f'{mode}-{phase}' for phase in
               ('continuous','captured','split','restored','append-split','appended')}
        for path in paths.values():path.mkdir(exist_ok=True)
        for phase,extra in [
            ('continuous',['+C64_CAPTURE_CYCLE=900000',f'+C64_SAVE={paths["continuous"]}/final.state']),
            ('captured',['+C64_CAPTURE_CYCLE=900000',f'+C64_SAVE={paths["captured"]}/final.state']),
            ('split',['+C64_CAPTURE_CYCLE=900000',f'+C64_SAVE={paths["split"]}/checkpoint.state',
                      '+C64_SAVE_CYCLE=731119','+C64_STOP_AFTER_SAVE=1']),
            ('restored',[f'+C64_RESTORE={paths["split"]}/checkpoint.state',f'+C64_SAVE={paths["restored"]}/final.state'])]:
            if phase!='continuous':
                extra += [f'+C64_SID_TRACE={paths[phase]}/sid-writes.csv',
                          f'+C64_AUDIO_PCM={paths[phase]}/audio.pcm']
            with (paths[phase]/'run.log').open('w') as log:
                subprocess.run(base+extra,cwd=paths[phase],stdout=log,stderr=subprocess.STDOUT,check=True)
        # Restore the same in-flight first insertion, then append a later event
        # from the original immutable library. Compare with the queued path.
        first=[arg if not arg.startswith('+C64_DISK_CHANGES=') else
               f'+C64_DISK_CHANGES={output}/first-change.txt' for arg in base]
        for phase,extra in [
            ('append-split',['+C64_CAPTURE_CYCLE=900000',f'+C64_SAVE={paths["append-split"]}/checkpoint.state',
                             '+C64_SAVE_CYCLE=731119','+C64_STOP_AFTER_SAVE=1']),
            ('appended',[f'+C64_RESTORE={paths["append-split"]}/checkpoint.state',
                         f'+C64_APPEND_DISK_CHANGES={output}/append-change.txt',
                         f'+C64_SAVE={paths["appended"]}/final.state'])]:
            with (paths[phase]/'run.log').open('w') as log:
                subprocess.run(first+extra,cwd=paths[phase],stdout=log,stderr=subprocess.STDOUT,check=True)
        names=['boot-ram.bin','boot.ppm','before-followup-ram.bin','final.state']
        names.append('drive-ram.bin')
        for phase in ('captured','restored','appended'):
            for name in names:
                if (paths['continuous']/name).read_bytes()!=(paths[phase]/name).read_bytes():
                    raise AssertionError(f'{mode}: {phase} {name} differs')
        assert not (paths['continuous']/'capture.json').exists(), 'capture must default off'
        def rows(phase):
            with (paths[phase]/'sid-writes.csv').open() as source:
                return [{key:int(value) for key,value in row.items()} for row in csv.DictReader(source)]
        stores=rows('captured')
        assert stores==rows('split')+rows('restored'), 'SID boundary duplicated or omitted'
        assert (paths['captured']/'audio.pcm').read_bytes()==(
            (paths['split']/'audio.pcm').read_bytes()+(paths['restored']/'audio.pcm').read_bytes()), 'PCM phase changed across restore'
        check_sid_stores(stores)
        assert (paths['captured']/'boot-ram.bin').read_bytes()[0xd400]==0x55, 'hidden I/O write missed RAM'
        samples=(paths['captured']/'audio.pcm').read_bytes()
        assert len(samples)==2*(SYS_CYCLES//1088)
        assert len(set(samples[i:i+2] for i in range(0,len(samples),2)))>20, 'audio fixture is constant'
        for phase,start,end in [('captured',0,SYS_CYCLES),('split',0,731119),('restored',731119,SYS_CYCLES)]:
            capture=json.loads((paths[phase]/'capture.json').read_text())
            assert capture['start_cycle_inclusive']==start and capture['end_cycle_exclusive']==end
            assert capture['audio_samples']==end//1088-start//1088
            assert capture['end_store_count']-capture['start_store_count']==len(rows(phase))
        print(f'C64 {mode} capture: {len(stores)} accepted stores, repeated/mirrored/hidden/RMW cases; PCM and SID cold/split concatenation exact; capture on/off RAM/frame/fullstate identical',flush=True)
        for label,event in [('past','731119 1\n'),('unknown-media','1100000 2\n')]:
            event_path=output/f'{label}.txt';event_path.write_text(event)
            log_path=paths['appended']/f'{label}.log'
            with log_path.open('w') as log:
                result=subprocess.run(first+[f'+C64_RESTORE={paths["append-split"]}/checkpoint.state',
                                             f'+C64_APPEND_DISK_CHANGES={event_path}'],
                                      cwd=paths['appended'],stdout=log,stderr=subprocess.STDOUT)
            reason='invalid future disk-change schedule' if mode=='oracle' else 'invalid future D64 schedule'
            if result.returncode==0 or reason not in log_path.read_text():
                raise AssertionError(f'{mode}: {label} insertion was not rejected by the schedule guard')
        for label,event in [('key-past','731119 5 1\n'),('key-tail','900000 5 1\n'),('key-usage','1300000 128 1\n'),('key-down','1300000 5 2\n'),('key-partial','1300000 5\n')]:
            event_path=output/f'{label}.txt';event_path.write_text(event)
            log_path=paths['appended']/f'{label}.log'
            with log_path.open('w') as log:
                result=subprocess.run(base+[f'+C64_RESTORE={paths["split"]}/checkpoint.state',
                                           f'+C64_APPEND_KEY_EVENTS={event_path}'],
                                      cwd=paths['appended'],stdout=log,stderr=subprocess.STDOUT)
            assert result.returncode!=0 and 'key event' in log_path.read_text(),label
        # Reject a different model/input identity before state restoration.
        wrong=[arg if not arg.startswith('+C64_STATE_ID=') else '+C64_STATE_ID='+'0'*64 for arg in base]
        with (paths['restored']/'mismatch.log').open('w') as log:
            result=subprocess.run(wrong+[f'+C64_RESTORE={paths["split"]}/checkpoint.state'],
                                  cwd=paths['restored'],stdout=log,stderr=subprocess.STDOUT)
        if result.returncode==0:raise AssertionError('incompatible checkpoint accepted')
        print(f'C64 {mode} checkpoint: CPU/RAM/peripherals, pending key release/capture, queued and appended media, complete state and frame match; invalid future events rejected',flush=True)


if __name__=='__main__':main()
