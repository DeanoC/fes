#!/usr/bin/env python3
"""Bounded synthetic controls for native library/SGM capture analysis, offline."""
import array
import hashlib
import json
import math
from pathlib import Path
import shutil
import subprocess
import sys
import wave

import analyze_factory as factory
import analyze_native as native
import fixture_oracle

BASE = Path(__file__).resolve().parent
CONTROL = BASE / 'controls'


def tone(path, frequencies):
    samples = array.array('h')
    for i in range(6 * 48000):
        value = round(sum(6000 * math.sin(2 * math.pi * f * i / 48000) for f in frequencies))
        samples.extend((value, value))
    if sys.byteorder != 'little': samples.byteswap()
    with wave.open(str(path), 'wb') as wav:
        wav.setnchannels(2); wav.setsampwidth(2); wav.setframerate(48000)
        wav.writeframes(samples.tobytes())


def image(name, rgb, frames):
    ppm = CONTROL / (name + '.ppm')
    ppm.write_bytes(b'P6\n1280 720\n255\n' + rgb)
    video = CONTROL / (name + '.mkv')
    subprocess.run(['ffmpeg', '-v', 'error', '-nostdin', '-y', '-loop', '1', '-framerate', '60',
                    '-i', str(ppm), '-frames:v', str(frames), '-an', '-c:v', 'ffv1',
                    '-level', '3', '-threads', '1', '-pix_fmt', 'yuv422p', str(video)], check=True)
    return video


def shade(rgb, height=1):
    shaded = bytearray(rgb)
    for y in range(720):
        if (y // height) & 1:
            start = y * 1280 * 3
            shaded[start:start + 1280 * 3] = bytes(c >> 1 for c in shaded[start:start + 1280 * 3])
    return bytes(shaded)


def main():
    CONTROL.mkdir(exist_ok=True)
    expected, rgb, provenance = fixture_oracle.expected()
    sn, ay = (provenance['nominal_tones_hz'][k] for k in ('SN', 'AY'))
    checks, details = {}, {}
    for fault in ('sgm_absent', 'ram_stuck_zero', 'console_mirror', 'lower_window_absent', 'ay_readback_wrong'):
        try: fixture_oracle.sgm_gate(fixture_oracle.emitted_sgm(), fault=fault)
        except fixture_oracle.ProbeRejected as rejection:
            checks[fault + '_prevents_video'] = rejection.evidence['video_writes'] == 0
            details[fault] = rejection.evidence
        else: checks[fault + '_prevents_video'] = False
    checks['SGM_pass_gate_checks_memory_and_AY'] = len(provenance['sgm_probe_gate']['memory_reads']) == 8 and \
        len(provenance['sgm_probe_gate']['ay_readbacks']) == 4
    def spectrum(frequencies, target):
        values = [sum(6000 * math.sin(2 * math.pi * f * i / 48000) for f in frequencies) for i in range(48000)]
        return native.spectral(values, 48000, target)
    checks['SN_and_AY_independently_present'] = all(factory.peak_present(spectrum([sn, ay], n), n, .5) for n in (sn, ay))
    checks['missing_AY_rejected'] = not factory.peak_present(spectrum([sn], ay), ay, .5)
    checks['missing_SN_rejected'] = not factory.peak_present(spectrum([ay], sn), sn, .5)
    checks['wrong_AY_frequency_rejected'] = not factory.peak_present(spectrum([sn, ay + 4], ay), ay, .5)
    checks['unexpected_AY_detected_in_baseline'] = factory.peak_present(spectrum([sn, ay], ay), ay, .01)
    checks['silence_rejected'] = not factory.peak_present(spectrum([], sn), sn, .5)
    direct = image('direct', rgb, 60)
    scanlines = image('scanlines', shade(rgb), 60)
    for name, profile, sgm in factory.CASES + [factory.OPTIONAL]:
        if name not in ('direct', 'scanlines'):
            shutil.copyfile(direct if profile == 'direct' else scanlines, CONTROL / (name + '.mkv'))
        tone(CONTROL / (name + '.wav'), [sn, ay] if sgm else [sn])
    shifted = bytearray(len(rgb))
    for y in range(720):
        start = y * 1280 * 3
        shifted[start + 3:start + 1280 * 3] = rgb[start:start + 1279 * 3]
    wrong = image('old-raster-offset', bytes(shifted), 12)
    png, frame = native.reference(wrong, CONTROL)
    checks['old_raster_horizontal_offset_rejected'] = bool(native.gray_geometry(wrong, frame, expected)['later_bad_frame_indexes'])
    period4 = image('wrong-period4', shade(rgb, 2), 12)
    direct_png, _ = native.reference(direct, CONTROL)
    period_png, _ = native.reference(period4, CONTROL)
    model = native.row_comparison(factory.support.image_bytes(direct_png), factory.support.image_bytes(period_png))
    checks['period4_scanlines_rejected'] = max(model['bright_rows_mae_rgb']) > 2 or max(model['dim_rows_half_brightness_mae_rgb']) > 2
    positive_path = BASE / 'results/positive-control/analysis.json'
    command = [sys.executable, str(BASE / 'analyze_factory.py'), str(CONTROL),
               '--include-sgm-relaunch', '--expected-frames', '60', '--output', str(positive_path)]
    with (BASE / 'positive-control.log').open('w') as log:
        completed = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT)
    positive = json.loads(positive_path.read_bytes())
    checks['full_six_case_CLI_passes'] = completed.returncode == 0 and positive['verdict'] == 'pass'
    receipt = {'classification': 'Synthetic and modeled offline controls only; no current hardware evidence.',
               'checks': checks, 'probe_negative_details': details,
               'positive_cli': {'command': command, 'exit_code': completed.returncode,
                                'cases': 6, 'frames_each': 60, 'gates': len(positive['checks']),
                                'passed': sum(positive['checks'].values()),
                                'analysis_sha256': hashlib.sha256(positive_path.read_bytes()).hexdigest()},
               'verdict': 'pass' if all(checks.values()) else 'investigate'}
    (BASE / 'self-controls.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    print(json.dumps(receipt, indent=2, sort_keys=True), flush=True)
    return 0 if receipt['verdict'] == 'pass' else 1


if __name__ == '__main__':
    sys.exit(main())
