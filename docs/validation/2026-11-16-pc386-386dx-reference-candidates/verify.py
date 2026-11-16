#!/usr/bin/env python3
"""Check recorded VM inputs and sampled 86Box speed indicators."""
from __future__ import annotations

import configparser
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


def check_inputs(directory: Path, nvr_name: str) -> None:
    inputs = (directory / 'input.sha256').read_text().splitlines()
    assert len(inputs) == 3, directory
    assert sha(directory / '86box.cfg') == inputs[0].split()[0], directory
    assert sha(directory / nvr_name) == inputs[1].split()[0], directory


def check_speed(directory: Path, count: int) -> None:
    rate = subprocess.check_output([
        'ffprobe', '-v', 'error', '-select_streams', 'v:0',
        '-show_entries', 'stream=avg_frame_rate', '-of',
        'default=noprint_wrappers=1:nokey=1', str(directory / 'speed.mkv'),
    ], text=True).strip()
    assert rate == '4/1', (directory, rate)
    decoded = subprocess.check_output([
        'ffmpeg', '-hide_banner', '-loglevel', 'error', '-i',
        str(directory / 'speed.mkv'), '-vf',
        "format=gray,lut=y='if(gt(val,145),255,0)'", '-f', 'framehash', '-',
    ], text=True)
    actual = frames(decoded)
    assert len(actual) == count and all(value == SPEED_100 for value in actual), directory
    assert actual == frames((directory / 'speed.framehash').read_text()), directory


def main() -> None:
    assert sha(ROOT / 'roms.sha256') == ROM_MANIFEST
    for machine in MACHINES:
        directory = ROOT / machine
        check_inputs(directory, f'{machine}.nvr')
        check_speed(directory, 115)
        print(f'{machine}: config and CMOS match; 115 / 115 speed samples match 100%')

    checks = ROOT / 'cache-check'
    assert sha(checks / 'cache.asm') == '8415c0be695d91e8d578a1df2cded05941e9056d7f4e32c61b195db6aeaa1958'
    assert sha(checks / 'ECS.COM') == '103a06d65d9be0dcee1e8076180a3086eb3d6c60131388aa6ed37bc3fa5850d9'
    assert sha(checks / 'ACC.COM') == '52137ed5d2b8e0989f7588a3a91155d62166c8e53899ebc6dbe4216acb1d8137'
    for machine in ('ecs386v', 'acc386'):
        probe = checks / machine
        baseline = ROOT / machine
        check_inputs(probe, f'{machine}.nvr')
        assert sha(probe / f'{machine}.nvr') == sha(baseline / f'{machine}.nvr'), machine
        original = (baseline / '86box.cfg').read_text()
        assert (probe / '86box.cfg').read_text() == original.replace(
            f'../../drv/bench-{machine}.img', f'../../drv/cache-{machine}.img')
        print(f'{machine}: register-probe CMOS matches the benchmark CMOS')

    enabled = checks / 'acc386-enabled'
    check_inputs(enabled, 'acc386.nvr')
    check_speed(enabled, 105)
    before = configparser.ConfigParser()
    after = configparser.ConfigParser()
    before.read(ROOT / 'acc386' / '86box.cfg')
    after.read(enabled / '86box.cfg')
    for config in (before, after):
        config.remove_option('General', 'uuid')
        config.remove_option('Hard disks', 'hdd_01_fn')
    assert {s: dict(before[s]) for s in before.sections()} == {
        s: dict(after[s]) for s in after.sections()}
    print('acc386 enabled: config matches baseline except UUID/path; CMOS matches its pre-run digest; 105 / 105 speed samples match 100%')


if __name__ == '__main__':
    main()
