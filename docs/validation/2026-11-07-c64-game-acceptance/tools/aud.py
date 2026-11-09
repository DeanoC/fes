#!/usr/bin/env python3
"""aud.py WAV [dc]: duration, overall RMS dBFS (DC removed), fraction of 1 s windows above -50 dBFS, and peak."""
import sys, wave, array, math
w = wave.open(sys.argv[1]); n = w.getnframes(); ch = w.getnchannels(); rate = w.getframerate()
a = array.array('h', w.readframes(n))
mono = [sum(a[i:i+ch]) / ch for i in range(0, len(a), ch)]
dc = sum(mono) / max(1, len(mono)); mono = [x - dc for x in mono]
win = rate; act = 0; tot = 0; levels = []
for i in range(0, len(mono) - win, win):
    s = mono[i:i+win]; r = math.sqrt(sum(x*x for x in s) / win) / 32768
    db = 20 * math.log10(r) if r > 0 else -120; levels.append(db); tot += 1; act += db > -50
rms = math.sqrt(sum(x*x for x in mono) / max(1, len(mono))) / 32768
print(f'{sys.argv[1].split("/")[-1]}: {n/rate:.1f} s, dc {dc:.0f}, rms {20*math.log10(rms) if rms else -120:.1f} dBFS, active {act}/{tot} s, peak {max(abs(x) for x in mono)/32768:.3f}')
