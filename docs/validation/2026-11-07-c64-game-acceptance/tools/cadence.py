#!/usr/bin/env python3
"""cadence.py RAW SECONDS [FROM TO]: cadence of a 480x270 gray raw capture (fes#1531), with the fes#1530 cad1 metrics.

The capture holds SECONDS of video; FROM/TO (seconds) select a segment. Reports frames, capture fps, black frames
(every 7th pixel below 16), and two update rules on every third pixel:
- fes#1530 rule: a frame updates when more than 20 pixels differ by more than 40 from the previous frame;
- picture rule: a frame updates when any pixel differs by more than 4. The 4KPRO path adds +/-1 level of noise, so
  byte equality can't be used; 4 levels is above that noise and below any C64 palette step in gray.
For each rule: updates per second and the longest run of non-updating frames; for the picture rule also the
histogram of static run lengths. The file's SHA-256 covers the whole capture."""
import collections, hashlib, sys
W, H = 480, 270
N = W * H
path, secs = sys.argv[1], float(sys.argv[2])
h = hashlib.sha256()
with open(path, 'rb') as f:
    for chunk in iter(lambda: f.read(1 << 24), b''):
        h.update(chunk)
import os
total = os.path.getsize(path) // N
fps = total / secs
f0 = int(float(sys.argv[3]) * fps) if len(sys.argv) > 3 else 0
f1 = min(total, int(float(sys.argv[4]) * fps)) if len(sys.argv) > 4 else total
upd = pic = run = best = prun = pbest = black = 0
hist = collections.Counter(); prev = None
with open(path, 'rb') as f:
    f.seek(f0 * N)
    for i in range(f0, f1):
        fr = f.read(N)[::3]
        if max(fr[::3]) < 16:
            black += 1
        if prev is not None:
            if any(abs(a - b) > 4 for a, b in zip(fr, prev)):
                pic += 1
                if prun: hist[prun] += 1
                prun = 0
            else:
                prun += 1; pbest = max(pbest, prun)
            if sum(1 for a, b in zip(fr, prev) if abs(a - b) > 40) > 20:
                upd += 1; run = 0
            else:
                run += 1; best = max(best, run)
        prev = fr
if prun: hist[prun] += 1
seg = (f1 - f0) / fps
print({'sha256': h.hexdigest(), 'capture_frames': total, 'capture_seconds': secs, 'fps': round(fps, 2),
       'segment_s': [round(f0 / fps, 1), round(f1 / fps, 1)], 'black_frames': black,
       'updates_per_s': round(upd / seg, 1), 'longest_static_run_frames': best,
       'picture_updates_per_s': round(pic / seg, 1), 'longest_picture_static_run_frames': pbest,
       'picture_static_run_histogram': dict(sorted(hist.items())[:12]), 'runs_over_12': sum(v for k, v in hist.items() if k > 12)})
