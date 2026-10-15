#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Compare uninterrupted/restored trajectories using original test firmware."""
import hashlib
from pathlib import Path
import subprocess
import sys

MODULE=Path(__file__).resolve().parents[3]
sys.path.insert(0,str(MODULE))
from scripts import c64_gcr


def main():
    output=MODULE/'build/sim/fes-c64-checkpoint'
    output.mkdir(parents=True,exist_ok=True)
    kernal=bytearray([0xea]*8192)
    # Continuously change C64 RAM through ordinary CPU instructions.
    kernal[:11]=bytes.fromhex('a9 2a 8d 00 c0 ee 00 c0 4c 05 e0')
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
              '+C64_SYS_CYCLES=1566720']
        suffix='g64' if mode=='oracle' else 'd64'
        (output/'library.txt').write_text(''.join('"'+str(output/(name+'.'+suffix))+'"\n'
                                               for name in ('disk','second')))
        base += [f'+C64_DISK_LIBRARY={output}/library.txt',f'+C64_DISK_CHANGES={output}/changes.txt']
        if mode=='oracle':base.append(f'+C64_G64_PATH={output}/disk.g64')
        paths={phase:output/f'{mode}-{phase}' for phase in
               ('continuous','split','restored','append-split','appended')}
        for path in paths.values():path.mkdir(exist_ok=True)
        for phase,extra in [
            ('continuous',['+C64_CAPTURE_CYCLE=900000',f'+C64_SAVE={paths["continuous"]}/final.state']),
            ('split',['+C64_CAPTURE_CYCLE=900000',f'+C64_SAVE={paths["split"]}/checkpoint.state',
                      '+C64_SAVE_CYCLE=731119','+C64_STOP_AFTER_SAVE=1']),
            ('restored',[f'+C64_RESTORE={paths["split"]}/checkpoint.state',f'+C64_SAVE={paths["restored"]}/final.state'])]:
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
        for phase in ('restored','appended'):
            for name in names:
                if (paths['continuous']/name).read_bytes()!=(paths[phase]/name).read_bytes():
                    raise AssertionError(f'{mode}: {phase} {name} differs')
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
        # Reject a different model/input identity before state restoration.
        wrong=[arg if not arg.startswith('+C64_STATE_ID=') else '+C64_STATE_ID='+'0'*64 for arg in base]
        with (paths['restored']/'mismatch.log').open('w') as log:
            result=subprocess.run(wrong+[f'+C64_RESTORE={paths["split"]}/checkpoint.state'],
                                  cwd=paths['restored'],stdout=log,stderr=subprocess.STDOUT)
        if result.returncode==0:raise AssertionError('incompatible checkpoint accepted')
        print(f'C64 {mode} checkpoint: CPU/RAM/peripherals, pending key release/capture, queued and appended media, complete state and frame match; invalid future events rejected',flush=True)


if __name__=='__main__':main()
