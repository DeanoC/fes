#!/usr/bin/env python3
"""Check recorded VM inputs and sampled 86Box speed indicators."""
from __future__ import annotations

import hashlib
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent
MACHINES = ('award495', 'dataexpert386wb', 'asus386', 'ecs386v', 'acc386')
SPEED_100 = 'b3132e4d7e87f55c3218972ff6412a55a805cd9f225b6dd1cb69ce74b6e459f6'
ROM_MANIFEST = 'ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94'


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def frames(data: str) -> list[str]:
    return [line.split(',')[-1].strip() for line in data.splitlines()
            if line and not line.startswith('#')]


def main() -> None:
    assert sha(ROOT / 'roms.sha256') == ROM_MANIFEST
    for machine in MACHINES:
        directory = ROOT / machine
        inputs = (directory / 'input.sha256').read_text().splitlines()
        assert len(inputs) == 3, machine
        assert sha(directory / '86box.cfg') == inputs[0].split()[0], machine
        assert sha(directory / f'{machine}.nvr') == inputs[1].split()[0], machine
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
        assert len(actual) == 115 and all(value == SPEED_100 for value in actual), machine
        assert actual == frames((directory / 'speed.framehash').read_text()), machine
        print(f'{machine}: config and CMOS match; {len(actual)} / 115 speed samples match 100%')


if __name__ == '__main__':
    main()
