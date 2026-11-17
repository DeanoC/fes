#!/usr/bin/env python3
"""Verify the included reference-screen evidence, not omitted private inputs.

Score and prompt interpretation remain visual. This checks recorded input
identities, retained bytes and speed samples; it cannot establish effective
cache, game behavior, kit acceptance or bytes absent from this directory.
"""
from __future__ import annotations

import configparser
import hashlib
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent
SPECS = {
    'samsung': {
        'machine': 'spc6000a', 'bios': None,
        'config': 'f7ea9d86317cc2c11f8a139ce19cf6db4ed949618cae5cb3027187ef0d8e051b',
        'nvr': 'fc6711966d760fea031aba8adb877d33f8d7a77c89e3ec671ea4fcabd20e3830',
        'disk': '279ac36b8cbf0a77c99899e60be45e975fd33a828e725dd13568e451eee90027',
        'video': 'screening.mkv', 'offset': '100', 'crop': '65:22:550:26',
        'speed_hash': 'fb40d452e5c0a35acfadd3bf09ffce9d4d5d5e3a535492b2d96b32ad3543c370',
        'events': ('F1 ', 'DOS_PROMPT ', 'AWAIT_marks ', 'AWAIT_topbench ', 'BENCHMARK_KEYS ', 'exit=0'),
    },
    'ami': {
        'machine': 'dataexpert386wb', 'bios': 'dataexpert386wb_050591',
        'config': '40276780fc645806241d328a38247654f212710420dbd66ea7e3256932f41fa6',
        'nvr': '4b684a11fce2c469ea8275d02a9ae5caa4d1a487a4907b2152f903d64249b912',
        'disk': '802fccc74abf296ed4f96bbe7343bd4685bf5566246a9a481901b6b2ccd3f073',
        'video': 'discovery.mkv', 'offset': '245', 'crop': '48:20:550:24',
        'speed_hash': '5b435fa87600e9e1a254e88eaa2b8391985bead3116ce6859d9a9e89a7026b8e',
        'events': ('AWAIT_dos ', 'AWAIT_cache ', 'AWAIT_marks ', 'AWAIT_topbench ', 'BENCHMARK_KEYS ', 'exit=0'),
    },
}
EMULATOR = 'ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969'
ROM_MANIFEST = 'ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94'


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def hashes(raw: str) -> list[str]:
    return [line.split(',')[-1].strip() for line in raw.splitlines()
            if line and not line.startswith('#')]


def main() -> None:
    for line in (ROOT / 'FILES.sha256').read_text().splitlines():
        expected, relative = line.split('  ', 1)
        path = ROOT / relative
        if path.is_symlink() or not path.is_file() or ROOT not in path.resolve().parents:
            raise ValueError(f'invalid evidence path: {relative}')
        if digest(path) != expected:
            raise ValueError(f'evidence digest mismatch: {relative}')
    # The original manifests also list private disks and tool installations.
    # Check only entries whose exact bytes are included in this record.
    for name, manifest, included in (
        ('samsung', 'FROZEN.sha256', {'PLAN.md', 'run.sh', 'prompt_gate.py', 'templates/bios.raw', 'templates/dos.raw', 'STAGED.sha256'}),
        ('ami', 'SCORE2-FROZEN.sha256', {'CORRECTION.md', 'SCORE2-PLAN.md', 'score2.sh'}),
    ):
        d = ROOT / name
        found = set()
        for line in (d / manifest).read_text().splitlines():
            expected, original = line.split('  ', 1)
            relative = Path(original).name if name == 'ami' else original
            if relative in included:
                if relative in found or digest(d / relative) != expected:
                    raise ValueError(f'{name}: precommitted file differs: {relative}')
                found.add(relative)
        if found != included:
            raise ValueError(f'{name}: precommitted included files missing')
    ami_supervisor = (ROOT / 'ami' / 'supervise-score2.sh').read_text()
    if ('expected_driver_sha256=7c39d9caafa80e19d426e7143fe2f887c26579acb4bb0ac78c6c67fa23aef6bb' not in ami_supervisor
            or 'timeout 480s bash "$driver" "$root"' not in ami_supervisor
            or '$root/bench.sh' in ami_supervisor):
        raise ValueError('AMI supervisor does not bind and invoke the retained driver')
    rom = ROOT.parent / '2026-11-16-pc386-386dx-reference-candidates' / 'roms.sha256'
    if digest(rom) != ROM_MANIFEST:
        raise ValueError('referenced ROM-manifest digest differs')
    for name, spec in SPECS.items():
        d = ROOT / name
        if digest(d / 'pre-86box.cfg') != spec['config'] or digest(d / 'pre.nvr') != spec['nvr']:
            raise ValueError(f'{name}: retained pre-run configuration differs')
        c = configparser.ConfigParser()
        c.read(d / 'pre-86box.cfg')
        machine = c['Machine']
        if (machine['machine'], machine['cpu_family'], machine['cpu_speed'],
                machine['cpu_use_dynarec'], machine['mem_size']) != (spec['machine'], 'i386dx', '25000000', '0', '8192'):
            raise ValueError(f'{name}: machine configuration differs')
        if spec['bios'] and c['DataExpert 386C']['bios'] != spec['bios']:
            raise ValueError('AMI BIOS selection differs')
        recorded = [line.split()[0] for line in (d / 'pre-run-sha256.txt').read_text().splitlines()]
        if recorded != [spec['config'], spec['nvr'], spec['disk'],
                        'c74e7e3a085c624260f0d144c02968d00fa9383f5d7fc46b32f1dc80d371d7ff',
                        EMULATOR, ROM_MANIFEST]:
            raise ValueError(f'{name}: recorded private-input identities differ')
        if (d / 'post-rom-audit.log').read_bytes() != b'':
            raise ValueError(f'{name}: post-ROM audit is not clean')
        log = (d / 'heartbeat.log').read_text()
        if not all(event in log for event in spec['events']):
            raise ValueError(f'{name}: expected execution event missing')
        fps = subprocess.check_output([
            'ffprobe', '-v', 'error', '-select_streams', 'v:0', '-show_entries',
            'stream=avg_frame_rate', '-of', 'default=noprint_wrappers=1:nokey=1',
            str(d / spec['video']),
        ], text=True).strip()
        if fps != '4/1':
            raise ValueError(f'{name}: expected four decoded frames per second')
        actual = hashes(subprocess.check_output([
            'ffmpeg', '-v', 'error', '-ss', spec['offset'], '-t', '38', '-i',
            str(d / spec['video']), '-vf',
            f"crop={spec['crop']},format=gray,lut=y='if(gt(val,145),255,0)'",
            '-f', 'framehash', '-',
        ], text=True))
        recorded_frames = hashes((d / 'speed.framehash').read_text())
        if len(actual) != 152 or set(actual) != {spec['speed_hash']} or actual != recorded_frames:
            raise ValueError(f'{name}: sampled speed frames differ')
        print(f'{name}: retained bytes and 152 sampled indicator frames verified')
    for gate in ('bios-gate.log', 'prompt-gate.log'):
        if 'prompt-errors 0 stable 2' not in (ROOT / 'samsung' / gate).read_text():
            raise ValueError(f'Samsung gate did not stabilize: {gate}')
    if (ROOT / 'ami' / 'supervisor-exit').read_text().strip() != '0':
        raise ValueError('AMI supervisor did not finish successfully')
    print('Scores and prompts require visual review; omitted disk/ROM bytes and effective cache are not verified.')


if __name__ == '__main__':
    main()
