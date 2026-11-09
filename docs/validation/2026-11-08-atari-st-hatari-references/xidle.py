#!/usr/bin/env python3
"""xidle.py NAME RUN [SECS]: Hatari Xenon scroll-timing control (fes#1528). Cold reset, title, one 0.2 s fire, then no
input; screenshots every 0.5 s for SECS (default 130) with the non-blue playfield count in hat/NAME/RUN-tl.txt."""
import subprocess, sys, time
sys.path.insert(0, '/home/deano/fes-scratch/st1528/tools'); import hlib
N, R = sys.argv[1], sys.argv[2]; S = float(sys.argv[3]) if len(sys.argv) > 3 else 130; D = f'{hlib.H}/{N}'
def nonblue(p):
    raw = subprocess.run(['ffmpeg', '-loglevel', 'error', '-i', p, '-vf', 'crop=416:384:32:8,scale=210:190', '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'], capture_output=True).stdout
    return sum(1 for q in range(0, len(raw) - 2, 21) if not (raw[q+2] > raw[q] + 40 and raw[q+2] > raw[q+1] - 10))
hlib.fifo(N, 'hatari-shortcut coldreset'); time.sleep(62); hlib.focus(N); time.sleep(0.5); hlib.shot(N, f'{R}-title')
t0 = time.time(); hlib.key(N, 'Control_R', 0.2); out = open(f'{D}/{R}-tl.txt', 'w'); k = 0
while time.time() - t0 < S:
    t = time.time() - t0; p = hlib.shot(N, f'{R}-tl{k:03d}'); out.write(f'{t:.2f} {nonblue(p)}\n'); out.flush(); k += 1
    time.sleep(max(0, 0.5*k - (time.time() - t0)))
print('done')
