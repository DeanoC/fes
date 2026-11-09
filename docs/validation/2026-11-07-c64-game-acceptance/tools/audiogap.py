#!/usr/bin/env python3
"""audiogap.py WAV [START END]: audio continuity (fes#1531). Within [START, END) seconds (default all), after removing
DC from the mono mix: sample count and duration; 1 s windows active above -50 dBFS RMS; and dropouts, i.e. runs of
at least 1 ms (48 samples) where the stereo samples stay exactly constant while the signal just before it was
moving right up to it (mean absolute step over the 8 samples before it above 32 LSB, and a step into the flat
run above 8 LSB, on either channel) and the surrounding 1 s
window is active. A buffer underrun or frozen sample freezes a moving signal at once; the SID or its output
network settling onto its silence level (steps shrinking to 1 LSB), or a quiet tail stepping one LSB every few milliseconds, is not a dropout."""
import math, sys, wave, array, json
w = wave.open(sys.argv[1]); rate = w.getframerate(); ch = w.getnchannels(); n = w.getnframes()
a = array.array('h', w.readframes(n))
s0 = int(float(sys.argv[2]) * rate) if len(sys.argv) > 2 else 0
s1 = min(n, int(float(sys.argv[3]) * rate)) if len(sys.argv) > 3 else n
mono = [(a[i * ch] + a[i * ch + ch - 1]) / 2 for i in range(s0, s1)]
dc = sum(mono) / max(1, len(mono))
act = []
for i in range(0, len(mono) - rate + 1, rate):
    r = math.sqrt(sum((x - dc) ** 2 for x in mono[i:i + rate]) / rate) / 32768
    act.append(r > 0 and 20 * math.log10(r) > -50)
gaps = []; run = 0
for i in range(1, len(mono) + 1):
    if i < len(mono) and a[(s0 + i) * ch] == a[(s0 + i - 1) * ch] and a[(s0 + i) * ch + ch - 1] == a[(s0 + i - 1) * ch + ch - 1]:
        run += 1; continue
    if run + 1 >= 48:
        start = i - run - 1; win = start // rate
        b = s0 + start
        if win < len(act) and act[win] and b >= s0 + 8:
            slope = max(sum(abs(a[(k + 1) * ch + c] - a[k * ch + c]) for k in range(b - 8, b)) / 8 for c in (0, ch - 1))
            entry = max(abs(a[b * ch + c] - a[(b - 1) * ch + c]) for c in (0, ch - 1))
            if slope > 32 and entry > 8:
                gaps.append(round(b / rate, 3))
    run = 0
print(json.dumps({'file': sys.argv[1].split('/')[-1], 'rate': rate, 'from_s': s0 / rate, 'to_s': s1 / rate,
                  'active_windows': f'{sum(act)}/{len(act)}', 'dropouts_ge_1ms_in_active': len(gaps),
                  'first_dropouts_s': gaps[:10]}))
