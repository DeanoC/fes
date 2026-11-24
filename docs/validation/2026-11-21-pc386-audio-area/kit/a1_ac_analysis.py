#!/usr/bin/env python3
"""Post-hoc analysis of Kit B run a1's recordings, written AFTER the run's committed music gate
failed. It is a separate measurement and does not change a1's verdict (fail).

Per whole second of each recording: the left channel's mean (the capture's DC offset) and its
AC RMS (the mean removed). The committed gate used total RMS, which includes that offset.
  a1_ac_analysis.py audio-floor.wav audio-wolf3d.wav
"""
import math, struct, sys, wave


def seconds(path):
    with wave.open(path) as w:
        n, rate = w.getnframes(), w.getframerate()
        data = struct.unpack(f'<{n * w.getnchannels()}h', w.readframes(n))
    left = data[0::2]
    for start in range(0, len(left) - rate + 1, rate):
        seg = left[start:start + rate]
        mean = sum(seg) / rate
        yield round(mean, 1), round(math.sqrt(sum((v - mean) ** 2 for v in seg) / rate), 1)


for path in sys.argv[1:]:
    rows = list(seconds(path))
    print(path, 'seconds', len(rows))
    print('  mean', [m for m, _ in rows])
    print('  ac_rms', [a for _, a in rows])
