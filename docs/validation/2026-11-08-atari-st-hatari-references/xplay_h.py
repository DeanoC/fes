#!/usr/bin/env python3
"""xplay_h.py NAME SECONDS: kit/xplay.py's input pattern (random.Random(1528) choice of direction, held while four
fire taps of 0.08 s go out 0.12 s apart) sent to an hx.sh Hatari NAME as host keys (--joy1 keys: cursor keys, fire
Right Ctrl). Saves a Hatari screenshot each minute as NAME-pMM.png (fes#1528 Xenon reference)."""
import random, sys, time
sys.path.insert(0, '/home/deano/fes-scratch/st1528/tools'); import hlib
n, secs = sys.argv[1], float(sys.argv[2]); rng = random.Random(1528); t0 = time.time(); nxt = 60
K = {'left': 'Left', 'right': 'Right', 'up': 'Up', 'down': 'Down'}
moves = ['left', 'right', 'up', 'down', 'left', 'right']
hlib.focus(n)
while time.time() - t0 < secs:
    m = K[rng.choice(moves)]
    cmd = f'xdotool keydown {m}; ' + ''.join('xdotool keydown Control_R; sleep 0.08; xdotool keyup Control_R; sleep 0.12; ' for _ in range(4)) + f'xdotool keyup {m}'
    hlib.sh(n, 'sh', '-c', cmd)
    if time.time() - t0 >= nxt: hlib.shot(n, f'{n}-p{nxt // 60:02d}'); nxt += 60
hlib.shot(n, f'{n}-end')
