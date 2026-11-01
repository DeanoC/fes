#!/usr/bin/env python3
"""Boot a real 8088 XT BIOS and read-only FreeDOS floppy in host simulation."""
import argparse
import fcntl
import json
import hashlib
import os
import signal
import time
from pathlib import Path
import subprocess

import sim_fes_pcxt as sim
import build_pcxt_freedos_image as freedos
import pcxt_bios as bios
import pcxt_acceptance_media as acceptance

ROOT = sim.ROOT


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--scenario', choices=('dos', 'com', 'game'), default='dos')
    parser.add_argument('--disk', type=Path, help='previously built floppy image for the selected scenario')
    parser.add_argument('--digger-exe', type=Path, help='independently acquired pinned Digger executable (game only)')
    parser.add_argument('--prepare-only', action='store_true', help='verify inputs and compile without running')
    parser.add_argument('--bios', type=Path, help='local copy of the BIOS selected by bios.lock')
    parser.add_argument('--font', type=Path, help='external normalized 4096-byte CGA font')
    parser.add_argument('--download', action='store_true', help='fetch pinned BIOS, FreeDOS sources and assembler if absent')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    args = parser.parse_args()
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    parent = ROOT / 'build/sim/fes-pcxt'
    parent.mkdir(parents=True, exist_ok=True)
    # A simulator and its frame checker share a build directory. Serialize the
    # entire run, including publication, so evidence cannot mix across runs.
    with (parent / ('.dos-' + args.scenario + '.lock')).open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        run(args)


def run(args):
    build = ROOT / 'build/sim/fes-pcxt' / ('dos' if args.scenario == 'dos' else 'dos-' + args.scenario)
    build.mkdir(parents=True, exist_ok=True)
    for name in ('result.json', 'result-pending.json', 'dos.ppm', 'screen.bin', 'screen.txt',
                 'inputs.json', 'bus.log', 'candidate.json', 'title-before-start.ppm',
                 'start-control.ppm', 'start-key.ppm', 'right-control.ppm', 'right-key.ppm'):
        (build / name).unlink(missing_ok=True)
    bios.fixture_inputs(build, args.bios, args.font, args.download)
    # Validate the exact bytes supplied to the device against the pinned boot
    # code, geometry and all 8086 FreeDOS files, while allowing harmless
    # unallocated-sector differences between image-building tool versions.
    if args.scenario == 'dos':
        disk_path = args.disk.resolve() if args.disk else freedos.image(download=args.download)
        disk = disk_path.read_bytes()
        disk_identity = freedos.validate_selected_image(disk)
    else:
        disk_path = args.disk.resolve() if args.disk else build / 'acceptance-floppy.img'
        if not args.disk:
            acceptance.build(args.scenario, disk_path, args.digger_exe)
        disk = disk_path.read_bytes()
        disk_identity = acceptance.validate(disk, args.scenario)
    (build / 'floppy.img').write_bytes(disk)
    inputs_path = build / 'inputs.json'
    inputs = json.loads(inputs_path.read_text())
    inputs['floppy_sha256'] = disk_identity['image_sha256']
    inputs['floppy_contents'] = disk_identity['files']
    inputs['floppy_bytes'] = len(disk)
    inputs['scenario'] = args.scenario
    inputs['source_commit'] = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    changed = subprocess.check_output(['git', 'diff', '--binary', 'HEAD'], cwd=ROOT)
    inputs['source_tracked_diff_sha256'] = hashlib.sha256(changed).hexdigest()
    inputs['source_has_tracked_changes'] = bool(changed)
    inputs['runner_sha256'] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    inputs['testbench_sha256'] = hashlib.sha256((ROOT / 'cores/fes-pcxt/sim/dos_tb.cpp').read_bytes()).hexdigest()
    if args.scenario != 'dos':
        inputs['media_builder_sha256'] = hashlib.sha256(Path(acceptance.__file__).read_bytes()).hexdigest()
        inputs['program_source_sha256'] = hashlib.sha256(acceptance.COM_SOURCE.read_bytes()).hexdigest() if args.scenario == 'com' else None
        inputs['program_sha256'] = disk_identity['program_sha256']
        inputs['base_floppy_sha256'] = disk_identity['base_sha256']
    inputs_path.write_text(json.dumps(inputs, indent=2) + '\n')
    for name in sim.CHIP_VENDORS:
        sim.verify_vendor(name)
    sim.cpu_roms(build)
    sources = tuple(p for p in sim.SYSTEM_SOURCES if not p.endswith('/pcxt_boot.sv')) + (
        'cores/fes-common/rtl/fes_computer_mailbox.v',
        'cores/fes-pcxt/rtl/pcxt_hid_keyboard.sv',
        'cores/fes-pcxt/rtl/pcxt_xt_keyboard_bat.sv',
        sim.MEMORY_SOURCES[-1], 'cores/fes-pcxt/sim/bios_top.sv', 'cores/fes-pcxt/sim/dos_tb.cpp')
    subprocess.run([args.verilator, '--cc', '--exe', '--build', '-j', '4', '-Wall',
        '--timescale-override', '1ns/1ps',
        str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt'), str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt'),
        "-GWITH_FLOPPY=1'b1", "-GWITH_RUNTIME_MEDIA=1'b1", "-GWAIT_FOR_MEDIA=1'b1",
        "-GWITH_RUNTIME_KEYBOARD=1'b1", '--top-module', 'bios_top', '--Mdir', str(build), '-I' + str(build),
        '-I' + str(ROOT / 'cores/fes-common/generated'),
        *('-I' + str(sim.vendor_dir(name)) for name in sim.CHIP_VENDORS),
        '-CFLAGS', '-std=c++17 -O2', '-I' + str(ROOT / 'cores/fes-pcxt/sim'),
        *(str(ROOT / path) for path in sources)], check=True)
    if args.prepare_only:
        print('Inputs and simulation compilation complete; no simulation run.', flush=True)
        return
    cmd = [str(build / 'Vbios_top'), str(build / 'firmware.bin'), str(build / 'floppy.img'), args.scenario]
    proc = subprocess.Popen(cmd, cwd=build, start_new_session=True)
    deadline = time.monotonic() + 3 * 60 * 60
    try:
        while True:
            try:
                status = proc.wait(timeout=min(30, max(0.1, deadline - time.monotonic())))
                break
            except subprocess.TimeoutExpired:
                if time.monotonic() >= deadline:
                    raise TimeoutError('PC/XT simulation exceeded three hours')
                print('PC/XT ' + args.scenario + ' simulation is running', flush=True)
        if status:
            raise subprocess.CalledProcessError(status, cmd)
    except BaseException:
        # The game comparison forks; stop the whole owned process group.
        try:
            os.killpg(proc.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        proc.wait()
        raise
    if args.scenario == 'game':
        verify_game(build)
        return
    ppm = (build / 'dos.ppm').read_bytes()
    header = b'P6\n1280 720\n255\n'
    if not ppm.startswith(header):
        raise ValueError('DOS image has invalid PPM header')
    font = bytes.fromhex((build / 'font.hex').read_text())
    cells = (build / 'screen.bin').read_bytes()
    rows = [cells[r*160:(r+1)*160:2] for r in range(25)]
    label = b'FES COM PASS' if args.scenario == 'com' else b'FESXT'
    target_rows = [r for r, row in enumerate(rows) if label in row and
                   (args.scenario == 'com' or b'TXT' in row)]
    if len(target_rows) != 1:
        raise ValueError('DOS screen must contain exactly one expected row')
    proof = bios.verify_post_frame(ppm[len(header):], cells, font, label, target_rows[0])
    pending = build / 'result-pending.json'
    result = json.loads(pending.read_text())
    result['frame_check'] = proof
    pending.write_text(json.dumps(result, indent=2) + '\n')
    pending.replace(build / 'result.json')
    print('PASS 720p DOS ' + args.scenario + ' glyphs:', proof, flush=True)


# SHA-256 of exact 1280x720 PPM output from the independently reviewed,
# deterministic 02870402 simulation. The fixture itself is not redistributed.
GAME_FRAME_SHA256 = {
    'title-before-start': '6f7509b3c08e4dc08be1dec004e1259de90651494a2f7eadf545e5d9c847c7c8',
    'start-control': '04726527ce90309a946bf5a05a8332c138911b1be54c7106ee8cac1d71129c18',
    'start-key': 'b9a3b01486d222ea11f93f43dea34a40b2149a5f404b238608b2c3c6b9ed0121',
    'right-control': '40b7d567b4b6ed8cf7804a77bb3773070cd69571ece77bbc32aecabfda3fa9ff',
    'right-key': '35a22f48b2fb8088e22d75b7509a85acc74767b3a533febdaaf61059bb415b7c',
}


def verify_game(build):
    """Fail closed unless frames match the reviewed title, start and movement."""
    hashes = {}
    for name, expected in GAME_FRAME_SHA256.items():
        data = (build / (name + '.ppm')).read_bytes()
        if not data.startswith(b'P6\n1280 720\n255\n') or len(data) != 16 + 1280 * 720 * 3:
            raise ValueError('invalid game frame: ' + name)
        hashes[name] = hashlib.sha256(data).hexdigest()
        if hashes[name] != expected:
            raise ValueError('game frame differs from reviewed reference: ' + name)
    pending = build / 'result-pending.json'
    result = json.loads(pending.read_text())
    result['classification'] = 'host simulation only; reviewed game-frame reference matched'
    result['frame_sha256'] = hashes
    result['frame_reference_source'] = '028704027ca8e1854d516997c9fbe747da369289'
    pending.write_text(json.dumps(result, indent=2) + '\n')
    pending.replace(build / 'result.json')
    print('PASS Digger title, Enter start and right movement vs reviewed frames', flush=True)


if __name__ == '__main__':
    main()
