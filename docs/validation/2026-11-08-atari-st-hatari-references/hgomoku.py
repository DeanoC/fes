#!/usr/bin/env python3
"""hgomoku.py NAME GAMES: Hatari Gomoku reference (fes#1528). Each game: cold reset (the kit's Stop + library relaunch),
open drive A and GOMOKU.PRG by File > Open with the mouse, then gomoku_h.py plays White exactly as gomoku.py does on the
kit. Start/end frames go to hat/NAME/fr in the kit live.png layout; the log is hat/NAME/gomoku.log."""
import json, os, shutil, subprocess, sys, time
sys.path.insert(0, '/home/deano/fes-scratch/st1528/tools'); import hlib
N, G = sys.argv[1], int(sys.argv[2]); D = f'{hlib.H}/{N}'; os.makedirs(f'{D}/fr', exist_ok=True)
for g in range(1, G + 1):
    hlib.fifo(N, 'hatari-shortcut coldreset'); time.sleep(30); hlib.focus(N); time.sleep(0.5)
    hlib.goto(N, 160, 100, 35, 31, True); hlib.goto(N, 35, 31, 88, 6); time.sleep(0.6); hlib.goto(N, 88, 6, 88, 17, True); time.sleep(4)
    hlib.goto(N, 88, 17, 124, 125, True); hlib.goto(N, 124, 125, 88, 6); time.sleep(0.6); hlib.goto(N, 88, 6, 88, 17, True); time.sleep(15)
    hlib.live(N); shutil.copy(f'{D}/live.png', f'{D}/fr/{N}-game{g}-start.png'); open(f'{D}/pointer.json', 'w').write('[88, 17]')
    print(time.strftime('%T'), 'game', g, 'start', flush=True)
    subprocess.run(['timeout', '900', 'python3', '/home/deano/fes-scratch/st1528/tools/gomoku_h.py', N, '90'])
    time.sleep(1); hlib.live(N); shutil.copy(f'{D}/live.png', f'{D}/fr/{N}-game{g}-end.png'); print(time.strftime('%T'), 'game', g, 'end', flush=True)
