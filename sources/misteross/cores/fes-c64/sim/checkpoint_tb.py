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
    for name,data in [('firmware',bytes(8192)+kernal),('drive',drive),('disk',disk)]:
        (output/f'{name}.hex').write_text(''.join(f'{b:02x}\n' for b in data))
    (output/'disk.g64').write_bytes(c64_gcr.g64(disk))
    # Hold a key across the snapshot; its release must remain queued.
    (output/'keys.txt').write_text('500000 4 1\n1000000 4 0\n')
    for mode in ('file','real'):
        build='fes-c64-boot-drive' if mode=='real' else 'fes-c64-boot'
        binary=MODULE/f'build/sim/{build}/Vc64_boot_sim_top'
        identity=hashlib.sha256(binary.read_bytes()).hexdigest()
        base=[str(binary),f'+C64_STATE_ID={identity}',f'+C64_FIRMWARE_HEX={output}/firmware.hex',
              f'+C64_DISK_HEX={output}/disk.hex',f'+C64_KEY_EVENTS={output}/keys.txt',
              '+C64_SYS_CYCLES=1566720']
        if mode=='real':base += [f'+C64_DRIVE_ROM_HEX={output}/drive.hex',f'+C64_G64_PATH={output}/disk.g64']
        paths={phase:output/f'{mode}-{phase}' for phase in ('continuous','split','restored')}
        for path in paths.values():path.mkdir(exist_ok=True)
        for phase,extra in [
            ('continuous',['+C64_CAPTURE_CYCLE=900000',f'+C64_SAVE={paths["continuous"]}/final.state']),
            ('split',['+C64_CAPTURE_CYCLE=900000',f'+C64_SAVE={paths["split"]}/checkpoint.state',
                      '+C64_SAVE_CYCLE=731119','+C64_STOP_AFTER_SAVE=1']),
            ('restored',[f'+C64_RESTORE={paths["split"]}/checkpoint.state',f'+C64_SAVE={paths["restored"]}/final.state'])]:
            with (paths[phase]/'run.log').open('w') as log:
                subprocess.run(base+extra,cwd=paths[phase],stdout=log,stderr=subprocess.STDOUT,check=True)
        names=['boot-ram.bin','boot.ppm','before-followup-ram.bin','final.state']
        if mode=='real':names.append('drive-ram.bin')
        for name in names:
            if (paths['continuous']/name).read_bytes()!=(paths['restored']/name).read_bytes():
                raise AssertionError(f'{mode}: restored {name} differs')
        # Reject a different model/input identity before state restoration.
        wrong=[arg if not arg.startswith('+C64_STATE_ID=') else '+C64_STATE_ID='+'0'*64 for arg in base]
        with (paths['restored']/'mismatch.log').open('w') as log:
            result=subprocess.run(wrong+[f'+C64_RESTORE={paths["split"]}/checkpoint.state'],
                                  cwd=paths['restored'],stdout=log,stderr=subprocess.STDOUT)
        if result.returncode==0:raise AssertionError('incompatible checkpoint accepted')
        print(f'C64 {mode} checkpoint: CPU/RAM/peripherals, pending key release/capture, complete state and frame match',flush=True)


if __name__=='__main__':main()
