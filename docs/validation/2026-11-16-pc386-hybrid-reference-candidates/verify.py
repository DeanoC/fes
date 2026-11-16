#!/usr/bin/env python3
"""Verify retained 86Box configs, CMOS, and sampled speed traces."""
from pathlib import Path
import hashlib
import subprocess

ROOT = Path(__file__).resolve().parent
EXPECTED = {
    'exp4349': 'c3e4dcb85088e34efdac4d6a79e00a86cd084339c21bfd64ddc91c00d97a0d00',
    'c747': 'b3132e4d7e87f55c3218972ff6412a55a805cd9f225b6dd1cb69ce74b6e459f6',
    'ami495': 'b3132e4d7e87f55c3218972ff6412a55a805cd9f225b6dd1cb69ce74b6e459f6',
}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def frame_hashes(text):
    return [line.split(',')[-1].strip() for line in text.splitlines()
            if line and not line.startswith('#')]


def main():
    assert sha(ROOT / 'roms.sha256') == 'ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94'
    for name, expected in EXPECTED.items():
        d = ROOT / name
        inputs = (d / 'input.sha256').read_text().splitlines()
        assert len(inputs) == 3, name
        assert sha(d / '86box.cfg') == inputs[0].split()[0], name
        assert sha(d / f'{name}.nvr') == inputs[1].split()[0], name
        rate = subprocess.check_output(['ffprobe', '-v', 'error', '-select_streams',
            'v:0', '-show_entries', 'stream=avg_frame_rate', '-of',
            'default=noprint_wrappers=1:nokey=1', str(d / 'speed.mkv')], text=True).strip()
        assert rate == '4/1', (name, rate)
        text = subprocess.check_output(['ffmpeg', '-hide_banner', '-loglevel', 'error',
            '-i', str(d / 'speed.mkv'), '-vf', "format=gray,lut=y='if(gt(val,145),255,0)'",
            '-f', 'framehash', '-'], text=True)
        values = frame_hashes(text)
        assert len(values) == 115 and all(value == expected for value in values), name
        assert values == frame_hashes((d / 'speed.framehash').read_text()), name
        print(f'{name}: config and CMOS match; 115 / 115 speed samples match 100%')


    probe = ROOT / 'exp4349-cache-probe'
    inputs = (probe / 'input.sha256').read_text().splitlines()
    assert len(inputs) == 3
    assert sha(probe / '86box.cfg') == inputs[0].split()[0]
    assert sha(probe / 'exp4349.nvr') == inputs[1].split()[0]
    assert sha(probe / 'exp4349.nvr') == sha(ROOT / 'exp4349' / 'exp4349.nvr')
    original = (ROOT / 'exp4349' / '86box.cfg').read_text()
    assert (probe / '86box.cfg').read_text() == original.replace(
        '../../drv/bench-exp4349.img', '../../drv/cache-exp4349.img')
    assert sha(probe / 'cache.asm') == '8415c0be695d91e8d578a1df2cded05941e9056d7f4e32c61b195db6aeaa1958'
    assert sha(probe / 'ECS.COM') == '103a06d65d9be0dcee1e8076180a3086eb3d6c60131388aa6ed37bc3fa5850d9'
    print('exp4349 cache probe: source/binary/config match and CMOS matches benchmark')


if __name__ == '__main__':
    main()
