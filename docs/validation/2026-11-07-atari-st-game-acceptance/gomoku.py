#!/usr/bin/env python3
"""gomoku.py RUN MAXMOVES: read the Gomoku board from runs/RUN/live.png (vrec, 960x540) and play White with the mouse.
Stops when no new Black stone appears after our move (game over or dialog) or after MAXMOVES. Logs moves to runs/RUN/gomoku.log."""
import json, subprocess, sys, time
from pathlib import Path
RUN, MAXM = sys.argv[1], int(sys.argv[2]); D = Path('/home/deano/fes-scratch/st1528/kit'); LIVE = D/'runs'/RUN/'live.png'
LOG = (D/'runs'/RUN/'gomoku.log').open('a'); POS = D/'runs'/RUN/'pointer.json'
def png():
    raw = subprocess.run(['ffmpeg', '-loglevel', 'error', '-i', str(LIVE), '-f', 'rawvideo', '-pix_fmt', 'gray', '-'], capture_output=True).stdout
    return raw if len(raw) == 960*540 else None
def px(img, x, y): return img[y*960 + x]
def board(img):
    b = {}
    for r in range(15):
        for c in range(15):
            x, y = 257 + 18*c, 139 + 18*r
            diag = sum(px(img, x+dx, y+dy) < 100 for dx in (-3, 3) for dy in (-3, 3))
            line = sum(px(img, x+dx, y+dy) < 100 for dx, dy in ((4, 0), (-4, 0), (0, 4), (0, -4)))
            if diag >= 3: b[(r, c)] = 'B'
            elif line <= 1: b[(r, c)] = 'W'
    return b
DIRS = ((0, 1), (1, 0), (1, 1), (1, -1))
def score(b, p, me):
    s = 0
    for who, w in ((me, 1.0), ('B' if me == 'W' else 'W', 0.9)):
        for dr, dc in DIRS:
            n, opn = 1, 0
            for sgn in (1, -1):
                r, c = p
                while True:
                    r += dr*sgn; c += dc*sgn
                    if b.get((r, c)) == who: n += 1; continue
                    if 0 <= r < 15 and 0 <= c < 15 and (r, c) not in b: opn += 1
                    break
            s += w * ({5: 1e6}.get(min(n, 5), 0) + (10**n) * (opn + 1))
    return s
def best(b):
    cands = {(r+dr, c+dc) for (r, c) in b for dr in (-2, -1, 0, 1, 2) for dc in (-2, -1, 0, 1, 2)}
    cands = [p for p in cands if 0 <= p[0] < 15 and 0 <= p[1] < 15 and p not in b] or [(7, 7)]
    return max(cands, key=lambda p: (score(b, p, 'W'), -abs(p[0]-7)-abs(p[1]-7)))
def st_xy(p): r, c = p; return round((514 + 36*c - 480)/3), round((278 + 36*r - 210)/3)
pos = json.loads(POS.read_text()) if POS.exists() else [88, 17]
for move in range(MAXM):
    img = png(); b = board(img); nb = sum(v == 'B' for v in b.values())
    p = best(b); x, y = st_xy(p)
    subprocess.run([str(D/'goto.py'), str(pos[0]), str(pos[1]), str(x), str(y), 'click']); subprocess.run([str(D/'goto.py'), str(x), str(y), '290', '150']); pos = [290, 150]; POS.write_text(json.dumps(pos))
    LOG.write(json.dumps({'t': time.strftime('%T'), 'move': move, 'white': p, 'stones': len(b), 'black': nb}) + '\n'); LOG.flush()
    for _ in range(40):
        time.sleep(0.5); img2 = png()
        if img2 is None: continue
        b2 = board(img2)
        if sum(v == 'B' for v in b2.values()) > nb and b2.get(p) == 'W': break
    else:
        LOG.write(json.dumps({'t': time.strftime('%T'), 'stop': 'no black reply', 'white_at_p': b2.get(p)}) + '\n'); print('stopped at move', move); break
print('done')
