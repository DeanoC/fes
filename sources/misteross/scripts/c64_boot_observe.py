#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Observe real C64 firmware on the RTL machine; never implies acceptance."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import shutil
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--real-drive', action='store_true', help='use the firmware-driven 1541 diagnostic')
    parser.add_argument('--drive-rom', type=Path)
    parser.add_argument('--late-video', action='store_true', help='drive diagnostic: capture only the final HDMI frames')
    parser.add_argument('--save-state', type=Path)
    parser.add_argument('--save-final-state', type=Path, help='also capture complete state at the observation endpoint')
    parser.add_argument('--save-at-cycles', type=int, default=0)
    parser.add_argument('--restore-state', type=Path)
    parser.add_argument('--disk-library', type=Path, action='append', default=[], help='additional immutable D64 available after checkpoint restore')
    parser.add_argument('--append-disk-change', nargs=2, action='append', default=[], metavar=('CYCLE','D64'), help='append a later insertion from the captured library after restore')
    parser.add_argument('--disk-change', nargs=2, action='append', default=[], metavar=('CYCLE','D64'))
    parser.add_argument('--basic', type=Path, required=True)
    parser.add_argument('--kernal', type=Path, required=True)
    parser.add_argument('--chargen', type=Path, required=True)
    parser.add_argument('--disk', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--cycles', type=int, default=3 * 52224000)
    parser.add_argument('--type-command', default='', help='native C64 text, starting at three seconds')
    parser.add_argument('--then-command', default='', help='second native command after loading')
    parser.add_argument('--then-at-cycles', type=int, default=58 * 52224000)
    args = parser.parse_args()
    if not 1044480 <= args.cycles <= 1800 * 52224000:
        parser.error('cycles must cover 0.02 to 1800 machine seconds')
    if args.real_drive != bool(args.drive_rom):
        parser.error('--real-drive requires --drive-rom; --drive-rom requires --real-drive')
    if args.late_video and not args.real_drive:
        parser.error('--late-video is reserved for the real-drive diagnostic')
    if args.save_at_cycles and (not args.save_state or not 0 < args.save_at_cycles <= args.cycles):
        parser.error('save cycle requires a save path and must fall within the run')
    if args.restore_state and (args.type_command or args.then_command):
        parser.error('restore preserves the captured keyboard queue; do not supply new typing commands')
    restored_metadata=None
    if args.restore_state:
        restored_metadata=json.loads(args.restore_state.with_name(args.restore_state.name+'.json').read_text())
        args.late_video=restored_metadata.get('late_video_capture',args.late_video)
        if args.save_at_cycles and args.save_at_cycles<=restored_metadata['system_cycle']:
            parser.error('save cycle must be later than the restored checkpoint')
    if args.restore_state and args.disk_change:
        parser.error('restore preserves queued disk changes; use --append-disk-change for later insertions')
    if args.append_disk_change and not args.restore_state:
        parser.error('additional disk changes require a restored checkpoint')
    changes=[(event['cycle'],event['disk']['path']) for event in restored_metadata.get('disk_changes',[])] if restored_metadata else args.disk_change
    if (changes or args.append_disk_change or args.disk_library) and not args.real_drive:
        parser.error('disk library and changes require the real-drive diagnostic')
    def normalize(events,after=0):
        result=[]
        for cycle,path in events:
            try:cycle=int(cycle)
            except (ValueError,TypeError):parser.error('disk-change cycle must be an integer')
            if not after<cycle<=1800*52224000 or (result and cycle<=result[-1][0]):
                parser.error('disk-change cycles must be ordered, later than the checkpoint and within 1800 seconds')
            result.append((cycle,Path(path).resolve()))
        return result
    normalized_changes=normalize(changes)
    after=max(restored_metadata['system_cycle'] if restored_metadata else 0,
              normalized_changes[-1][0] if normalized_changes else 0)
    appended_changes=normalize(args.append_disk_change,after)
    if len(normalized_changes)+len(appended_changes)>64:parser.error('at most 64 disk changes are supported')
    library_paths=args.disk_library
    if restored_metadata and not library_paths:
        library_paths=[Path(item['disk']['path']) for item in restored_metadata['media_library'][1:]]
    library_paths=[args.disk.resolve()]+[path.resolve() for path in library_paths]
    if not restored_metadata:
        for _,path in normalized_changes:
            if path not in library_paths:library_paths.append(path)
    if len(library_paths)>64:parser.error('at most 64 library disks are supported')
    if args.save_state and args.save_final_state and args.save_state.resolve()==args.save_final_state.resolve():
        parser.error('intermediate and final checkpoints need different paths')
    inputs = {}
    blobs = {}
    for name, size in [('basic', 8192), ('kernal', 8192), ('chargen', 4096), ('disk', 174848)]:
        path = getattr(args, name).resolve()
        blob = path.read_bytes()
        if len(blob) != size:
            parser.error(f'{name}: expected {size} bytes, got {len(blob)}')
        blobs[name] = blob
        inputs[name] = {'path': str(path), 'bytes': size,
                        'sha256': hashlib.sha256(blob).hexdigest()}
    module = Path(__file__).resolve().parents[1]
    build_name = 'fes-c64-boot-drive' if args.real_drive else 'fes-c64-boot'
    binary = module / f'build/sim/{build_name}/Vc64_boot_sim_top'
    if not binary.is_file():
        parser.error(f'run make sim-{build_name}-build first')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    # A short restored observation may not collect a complete frame. Never
    # hash a stale frame or pre-command snapshot left by an earlier run.
    for name in ['boot.log','boot-ram.bin','drive-ram.bin','boot.ppm',
                 'before-followup-ram.bin','observation.json']:
        artifact=output/name
        for state in [args.restore_state,args.save_state,args.save_final_state]:
            if state and artifact==state.resolve():
                parser.error('checkpoint path conflicts with an observation output')
        artifact.unlink(missing_ok=True)
    for name, blob in [('firmware', blobs['basic'] + blobs['kernal'] + blobs['chargen']), ('disk', blobs['disk'])]:
        (output / f'{name}.hex').write_text(''.join(f'{b:02x}\n' for b in blob))
    if args.real_drive:
        import c64_gcr
        drive_blob=args.drive_rom.read_bytes()
        if len(drive_blob)!=16384:
            parser.error('drive ROM must be exactly 16384 bytes')
        (output/'drive.hex').write_text(''.join(f'{b:02x}\n' for b in drive_blob))
        raw_disk=c64_gcr.g64(blobs['disk'])
        (output/'disk.g64').write_bytes(raw_disk)
        inputs['drive_rom']={'path': str(args.drive_rom.resolve()), 'bytes': len(drive_blob),
                             'sha256': hashlib.sha256(drive_blob).hexdigest()}
        inputs['g64']={'path': str(output/'disk.g64'), 'bytes':len(raw_disk),
                      'sha256':hashlib.sha256(raw_disk).hexdigest()}
    media_library=[]
    if args.real_drive:
        for index,path in enumerate(library_paths):
            blob=path.read_bytes()
            if len(blob)!=174848:parser.error('library disk must be exactly 174848 bytes')
            raw=c64_gcr.g64(blob)
            raw_path=output/f'library-{index}.g64';raw_path.write_bytes(raw)
            disk={'path':str(path),'bytes':len(blob),'sha256':hashlib.sha256(blob).hexdigest()}
            g64={'path':str(raw_path),'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()}
            inputs[f'disk_library_{index}']=disk;inputs[f'g64_library_{index}']=g64
            media_library.append({'disk':disk,'g64':g64})
        def quoted(value):
            return '"'+value.replace(chr(92),chr(92)*2).replace(chr(34),chr(92)+chr(34))+'"'
        (output/'disk-library.txt').write_text(''.join(quoted(item['g64']['path'])+chr(10) for item in media_library))
    def materialize(events):
        result=[]
        for cycle,path in events:
            blob=path.read_bytes()
            digest=hashlib.sha256(blob).hexdigest()
            index=next((i for i,item in enumerate(media_library) if item['disk']['sha256']==digest),None)
            if index is None:parser.error('disk change must select an image from the checkpoint library')
            result.append({'cycle':cycle,'library_index':index,**media_library[index]})
        return result
    disk_changes=materialize(normalized_changes)
    added_disk_changes=materialize(appended_changes)
    for name,events in [('disk-changes',disk_changes),('append-disk-changes',added_disk_changes)]:
        if events:(output/(name+'.txt')).write_text(''.join(f"{event['cycle']} {event['library_index']}"+chr(10) for event in events))
    all_disk_changes=disk_changes+added_disk_changes
    key_events = []
    event_cycle = 3 * 52224000
    key_period = 52224000 // 20  # 50 ms held, 50 ms released
    commands = [(3 * 52224000, args.type_command)]
    if args.then_command:
        commands.append((args.then_at_cycles, args.then_command))
    for start_cycle, text in commands:
        if start_cycle < 3 * 52224000:
            parser.error('command must start at or after three machine seconds')
        if key_events and start_cycle < event_cycle:
            parser.error('second command overlaps the first command')
        event_cycle = start_cycle
        for char in text.upper():
            shift = False
            if 'A' <= char <= 'Z':
                usage = ord(char) - ord('A') + 4
            elif char in '1234567890':
                usage = '1234567890'.index(char) + 0x1e
            elif char == '"':
                usage, shift = 0x1f, True  # native C64 Shift+2
            else:
                mapping = {' ': 0x2c, ',': 0x36, '.': 0x37, '*': 0x30,
                           '\r': 0x28, '\n': 0x28, ':': 0x34, ';': 0x33,
                           '=': 0x2e, '+': 0x57, '-': 0x2d, '/': 0x38, '@': 0x2f}
                if char not in mapping:
                    parser.error(f'unsupported native C64 typing character: {char!r}')
                usage = mapping[char]
            if shift:
                key_events.append((event_cycle, 0xe1, 1))
            key_events.append((event_cycle, usage, 1))
            event_cycle += key_period
            key_events.append((event_cycle, usage, 0))
            if shift:
                key_events.append((event_cycle, 0xe1, 0))
            event_cycle += key_period
    if key_events and event_cycle >= args.cycles:
        parser.error('cycle budget must extend beyond the last typed key')
    if key_events:
        (output / 'keyboard.txt').write_text(''.join(f'{cycle} {usage} {down}\n'
            for cycle, usage, down in key_events))
    command = [str(binary), f'+C64_FIRMWARE_HEX={output / "firmware.hex"}',
               f'+C64_DISK_HEX={output / "disk.hex"}', f'+C64_SYS_CYCLES={args.cycles}']
    if args.real_drive:
        command.extend([f'+C64_DRIVE_ROM_HEX={output / "drive.hex"}', f'+C64_G64_PATH={output / "disk.g64"}'])
    if args.real_drive:
        command.append(f'+C64_DISK_LIBRARY={output / "disk-library.txt"}')
    if added_disk_changes:
        command.append(f'+C64_APPEND_DISK_CHANGES={output / "append-disk-changes.txt"}')
    if disk_changes:
        command.append(f'+C64_DISK_CHANGES={output / "disk-changes.txt"}')
    if args.late_video:
        command.append('+C64_LATE_VIDEO=1')
    if args.then_command:
        command.append(f'+C64_CAPTURE_CYCLE={args.then_at_cycles}')
    if key_events:
        command.append(f'+C64_KEY_EVENTS={output / "keyboard.txt"}')
    if args.then_command:
        outputs_to_hash = ['boot.log', 'boot-ram.bin', 'boot.ppm', 'before-followup-ram.bin']
    else:
        outputs_to_hash = ['boot.log', 'boot-ram.bin', 'boot.ppm']
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=module, text=True).strip()
    source_diff = subprocess.check_output(['git', 'diff', 'HEAD', '--',
        'cores/fes-c64', 'Makefile', 'scripts/c64_boot_observe.py'], cwd=module)
    # A long observation can span edits and a rebuild of the shared binary.
    # Execute an immutable private copy and retain its launch-time hash. The
    # checkout fields describe the invocation, not compiler input provenance.
    with tempfile.TemporaryDirectory(prefix='c64-simulator-', dir=output) as run_directory:
        frozen_binary = Path(run_directory) / binary.name
        shutil.copy2(binary, frozen_binary)
        simulator_sha256 = hashlib.sha256(frozen_binary.read_bytes()).hexdigest()
        command[0] = str(frozen_binary)
        state_identity=hashlib.sha256(json.dumps({
            'schema':'FES-C64-development-state-v2', 'simulator':simulator_sha256,
            'inputs':{name:value['sha256'] for name,value in inputs.items()},
        },sort_keys=True).encode()).hexdigest()
        command.append(f'+C64_STATE_ID={state_identity}')
        if args.restore_state:
            if restored_metadata['model_input_identity']!=state_identity:
                parser.error('checkpoint belongs to different firmware, disk or simulator')
            if hashlib.sha256(args.restore_state.read_bytes()).hexdigest()!=restored_metadata['sha256']:
                parser.error('checkpoint hash mismatch')
            if args.cycles<=restored_metadata['system_cycle']:
                parser.error('restore budget must extend beyond checkpoint time')
            command.append(f'+C64_RESTORE={args.restore_state.resolve()}')
        if args.save_state:
            args.save_state.parent.mkdir(parents=True,exist_ok=True)
            command.append(f'+C64_SAVE={args.save_state.resolve()}')
            if args.save_at_cycles:command.append(f'+C64_SAVE_CYCLE={args.save_at_cycles}')
        if args.save_final_state:
            args.save_final_state.parent.mkdir(parents=True,exist_ok=True)
            command.append(f'+C64_SAVE_FINAL={args.save_final_state.resolve()}')
        with (output / 'boot.log').open('w') as log:
            subprocess.run(command, cwd=output, stdout=log, stderr=subprocess.STDOUT, check=True)
    if args.real_drive:
        outputs_to_hash.append('drive-ram.bin')
    # Short restored runs can end before a complete HDMI frame is collected.
    outputs_to_hash=[name for name in outputs_to_hash if name!='boot.ppm' or (output/name).is_file()]
    if (output/'before-followup-ram.bin').is_file() and 'before-followup-ram.bin' not in outputs_to_hash:
        outputs_to_hash.append('before-followup-ram.bin')
    for state_path,state_cycle in [(args.save_state,args.save_at_cycles or args.cycles),(args.save_final_state,args.cycles)]:
        if not state_path:continue
        state_metadata={'model_input_identity':state_identity,
            'sha256':hashlib.sha256(state_path.read_bytes()).hexdigest(),
            'system_cycle':state_cycle, 'inputs':inputs,
            'simulator_sha256':simulator_sha256, 'real_drive':args.real_drive,
            'late_video_capture':args.late_video,
            'disk_changes':all_disk_changes, 'media_library':media_library,
            'typed_command':args.type_command, 'followup_command':args.then_command,
            'followup_cycle':args.then_at_cycles if args.then_command else None,
            'key_event_count':len(key_events), 'evidence':'development simulation checkpoint'}
        if restored_metadata:
            for name in ['typed_command','followup_command','followup_cycle','key_event_count']:
                state_metadata[name]=restored_metadata[name]
        state_path.with_name(state_path.name+'.json').write_text(json.dumps(state_metadata,indent=2)+'\n')
    outputs = {name: hashlib.sha256((output / name).read_bytes()).hexdigest()
               for name in outputs_to_hash}
    receipt = {'checkout_commit_at_start': revision,
               'checkout_diff_sha256_at_start': hashlib.sha256(source_diff).hexdigest(),
               'evidence': 'host-only observation', 'simulated_seconds': args.cycles / 52224000,
               'system_clock_hz': 52224000, 'pixel_clock_hz': 74250000,
               'cpu_clock_hz': 985248, 'inputs': inputs, 'outputs': outputs,
               'simulator_sha256': simulator_sha256,
               'typed_command': args.type_command, 'followup_command': args.then_command,
               'followup_cycle': args.then_at_cycles if args.then_command else None, 'key_event_count': len(key_events),
               'disk_changes':all_disk_changes, 'media_library':media_library,
               'appended_disk_changes':added_disk_changes,
               'real_drive': args.real_drive, 'late_video_capture':args.late_video,
               'restored_state':str(args.restore_state) if args.restore_state else None,
               'saved_final_state':str(args.save_final_state) if args.save_final_state else None,
               'saved_state':str(args.save_state) if args.save_state else None, 'boot_acceptance': False, 'demo_acceptance': False}
    if restored_metadata:
        for name in ['typed_command','followup_command','followup_cycle','key_event_count']:
            receipt[name]=restored_metadata[name]
    (output / 'observation.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print((output / 'boot.log').read_text(), end='')


if __name__ == '__main__':
    main()
