#!/usr/bin/env python3
"""Check the retained input digests and sampled 86Box speed frames.

This does not OCR the score, verify a missing private disk or ROM, establish the
cache flag, or establish emulation speed between samples.
"""
from __future__ import annotations

import configparser
import hashlib
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent
MACHINES = ('asus3863364k', 'micronics386', 'micronics386px', 'tandy4000', 'deskpro386')
EXPECTED_DISK = '279ac36b8cbf0a77c99899e60be45e975fd33a828e725dd13568e451eee90027'
EXPECTED_86BOX = 'ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969'
EXPECTED_ROM_MANIFEST = 'ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94'
EXPECTED_SPEED = 'fb40d452e5c0a35acfadd3bf09ffce9d4d5d5e3a535492b2d96b32ad3543c370'


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def frames(text: str) -> list[str]:
    return [line.split(',')[-1].strip() for line in text.splitlines()
            if line and not line.startswith('#')]


def check(machine: str) -> None:
    directory = ROOT / machine
    rows = (directory / 'pre-run-sha256.txt').read_text().splitlines()
    assert len(rows) == 6, machine
    assert sha(directory / 'pre-86box.cfg') == rows[0].split()[0], machine
    assert sha(directory / (machine + '.nvr')) == rows[1].split()[0], machine
    assert rows[2].split()[0] == EXPECTED_DISK, machine
    assert rows[4].split()[0] == EXPECTED_86BOX, machine
    assert rows[5].split()[0] == EXPECTED_ROM_MANIFEST, machine
    config = configparser.ConfigParser()
    config.read(directory / 'pre-86box.cfg')
    assert config['Machine']['machine'] == machine
    assert config['Machine']['cpu_family'] == (
        'i386dx_deskpro386' if machine == 'deskpro386' else 'i386dx')
    assert config['Machine']['cpu_speed'] == '25000000'
    assert config['Machine']['cpu_use_dynarec'] == '0'
    assert config['Machine']['mem_size'] == '8192'
    assert config['Video']['gfxcard'] == 'vga'
    assert config['Storage controllers']['hdc_1'] == 'xtide_at'
    assert config['Sound']['sndcard'] == 'sb2.0'
    rate = subprocess.check_output([
        'ffprobe', '-v', 'error', '-select_streams', 'v:0',
        '-show_entries', 'stream=avg_frame_rate', '-of',
        'default=noprint_wrappers=1:nokey=1', str(directory / 'speed.mkv'),
    ], text=True).strip()
    assert rate == '4/1', (machine, rate)
    decoded = subprocess.check_output([
        'ffmpeg', '-hide_banner', '-loglevel', 'error', '-i',
        str(directory / 'speed.mkv'), '-vf',
        "format=gray,lut=y='if(gt(val,145),255,0)'", '-f', 'framehash', '-',
    ], text=True)
    actual = frames(decoded)
    assert len(actual) == 152 and all(x == EXPECTED_SPEED for x in actual), machine
    assert actual == frames((ROOT / 'speed.framehash').read_text()), machine
    assert (directory / 'post-rom-audit.log').read_bytes() == b'', machine
    assert 'FINISH ' in (directory / 'heartbeat.log').read_text(), machine
    assert 'exit=0' in (directory / 'heartbeat.log').read_text(), machine
    print(f'{machine}: config and CMOS match; 152/152 speed samples match 100%')


def main() -> None:
    manifest = ROOT.parent / '2026-11-16-pc386-386dx-reference-candidates' / 'roms.sha256'
    assert sha(manifest) == EXPECTED_ROM_MANIFEST
    assert len(manifest.read_text().splitlines()) == 1829
    for machine in MACHINES:
        check(machine)
    print('Scores require visual inspection; disk, ROM contents and cache are not verified here.')


if __name__ == '__main__':
    main()
