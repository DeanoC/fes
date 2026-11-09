#!/usr/bin/env python3
"""xref.py NAME RUN: Hatari Xenon reference run (fes#1528). Cold reset, wait for the title, fire, then xplay_h's input
pattern for 660 s while a second thread screenshots every 0.5 s for the first 130 s (scroll-timing landmarks:
launch bay -> plain plating -> first level structure) and once a minute after (score, lives). Writes
hat/NAME/RUN-tl.txt: t_since_fire non_blue_count per shot, and per-minute shots RUN-pMM.png."""
import os, subprocess, sys, threading, time
sys.path.insert(0, '/home/deano/fes-scratch/st1528/tools'); import hlib
N, R = sys.argv[1], sys.argv[2]; D = f'{hlib.H}/{N}'
def nonblue(p):
    raw = subprocess.run(['ffmpeg', '-loglevel', 'error', '-i', p, '-vf', 'crop=416:384:32:8,scale=210:190', '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'], capture_output=True).stdout
    return sum(1 for q in range(0, len(raw) - 2, 21) if not (raw[q+2] > raw[q] + 40 and raw[q+2] > raw[q+1] - 10))
hlib.fifo(N, 'hatari-shortcut coldreset'); time.sleep(62); hlib.focus(N); time.sleep(0.5)
hlib.shot(N, f'{R}-title')
t0 = time.time(); hlib.key(N, 'Control_R', 0.2)
def shooter():
    out = open(f'{D}/{R}-tl.txt', 'w'); k = 0
    while time.time() - t0 < 130:
        t = time.time() - t0; p = hlib.shot(N, f'{R}-tl{k:03d}'); out.write(f'{t:.2f} {nonblue(p)}\n'); out.flush(); k += 1
        time.sleep(max(0, 0.5*(k) - (time.time() - t0)))
th = threading.Thread(target=shooter); th.start()
time.sleep(4)
subprocess.run(['python3', '/home/deano/fes-scratch/st1528/tools/xplay_h.py', N, '656'])
th.join()
for f in os.listdir(D):
    if f.startswith(f'{N}-p') or f == f'{N}-end.png': os.rename(f'{D}/{f}', f'{D}/{R}-{f[len(N)+1:]}')
print('done', time.strftime('%T'))
