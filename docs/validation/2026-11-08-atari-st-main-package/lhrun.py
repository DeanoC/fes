#!/usr/bin/env python3
"""lhrun.py RUN GAME_ID: Lurking Horror case with checked launch and checked typing (fes#1528).
- Launch: Library Play, open drive A (File > Open), read the window: LURKING.PRG is the first icon with 2 items, the
  second with 3 (FES1528.SAV sorts first). After File > Open the game's solid status bar must be on screen, else STOP.
- Typing: each command is typed without Enter, then the input row is measured on live.png (8x8 font, 12 px cells at
  960x540): the row must grow by exactly len(command) from its end before typing. A longer row (TOS key repeat from a late key-up) is erased with
  Backspace and retyped, up to 4 tries; then Enter. Every try is logged to runs/RUN/lhrun.log.
- The filename after SAVE/RESTORE goes to the GEM file selector through selname.py (Esc, type, field check, Enter).
- argv[3] FIRST: first session to run (default 1). Sessions: 1 = tools/lh-script.txt to SAVE; 2 and 3 = Stop, relaunch, RESTORE and play (runs/m1/lh-play2/3.txt)."""
import json, shutil, subprocess, sys, time, urllib.request
R, GID = sys.argv[1], sys.argv[2]; FIRST = int(sys.argv[3]) if len(sys.argv) > 3 else 1; K = '/home/deano/fes-scratch/st1528/kit'; D = f'{K}/runs/{R}'
LOG = open(f'{D}/lhrun.log', 'a')
def log(*a):
    l = time.strftime('%T ') + ' '.join(map(str, a)); print(l, flush=True); LOG.write(l + '\n'); LOG.flush()
HID = {c: 4 + i for i, c in enumerate('abcdefghijklmnopqrstuvwxyz')}; HID.update({str(i): 0x1e + (i - 1) for i in range(1, 10)}); HID['0'] = 0x27
HID.update({' ': 0x2c, '\n': 0x28, ',': 0x36, '\b': 0x2a}); SH = 0xe1
O = urllib.request.build_opener(urllib.request.ProxyHandler({}))
def ev(code, down):
    b = json.dumps({'event': {'Player': 0, 'Device': 0, 'Kind': 0, 'Action': 1 if down else 0, 'Code': 0x1000 + code, 'Value': 0}}).encode()
    O.open(urllib.request.Request('http://127.0.0.1:18787/api/v1/session/input/event', b, {'Content-Type': 'application/json'}, method='POST'), timeout=20).read()
def tap(c):
    up = c.isupper(); k = HID[c.lower()]
    if up: ev(SH, True)
    t0 = time.monotonic(); ev(k, True); time.sleep(0.05); ev(k, False); held = time.monotonic() - t0
    if up: ev(SH, False)
    time.sleep(0.25); return held
def c(*ops): subprocess.run([f'{K}/c.py', R, *map(json.dumps, ops)], capture_output=True)
def goto(*a): subprocess.run([f'{K}/goto.py', *map(str, a)])
def frame():
    time.sleep(0.6)
    return subprocess.run(['ffmpeg', '-loglevel', 'error', '-i', f'{D}/live.png', '-f', 'rawvideo', '-pix_fmt', 'gray', '-'], capture_output=True).stdout
def snap(name): shutil.copy(f'{D}/live.png', f'{D}/fr/{R}-{name}.png')
def bar(im): return max(sum(im[y*960+x] < 100 for x in range(240, 720)) / 480 for y in range(104, 120))
def icon3(im): return sum(im[y*960+x] < 100 for y in range(230, 272) for x in range(490, 530))
def typed_len(im):
    """Last text cell (ink 18..89 of 144 px) on the lowest row holding text or the cursor (ink >= 90, ignored as text);
    cells under 18 are bleed from the row above. None if no such row. Calibrated on m1/m2 frames 8 Nov."""
    def cells(k): return [sum(im[(104+12*k+dy)*960+240+12*c+dx] < 100 for dy in range(12) for dx in range(12)) for c in range(40)]
    for k in range(24, 0, -1):
        cs = cells(k)
        if any(v >= 18 for v in cs):
            text = [c for c, v in enumerate(cs) if 18 <= v < 90]
            return text[-1] if text else 0
    return None
def command(line, tries=4, prompt=True):
    """prompt=True: first page through any [MORE] (space) until the row is a bare '>' (row end 0)."""
    base = typed_len(frame()) or 0
    for _ in range(6):
        if not prompt or base == 0: break
        log(f'row end {base} before {line!r}: not a bare prompt, space'); tap(' '); time.sleep(2); base = typed_len(frame()) or 0
    for t in range(1, tries + 1):
        held = max(tap(ch) for ch in line); n = max(typed_len(frame()) or 0, typed_len(frame()) or 0)
        log(f'try {t} {line!r} row_end={n} base={base} want={base + len(line)} max_held={held:.3f}')
        if n == base + len(line): tap('\n'); return True
        for _ in range(max(n - base, 0) + 1): tap('\b')
    log('GIVING UP on', repr(line)); return False
def launch(tag):
    c({'op': 'stop'}, {'op': 'launch', 'game_id': GID}, {'op': 'sleep', 's': 30}); snap(f'{tag}-desktop')
    goto(160, 103, 35, 31, 'click'); goto(35, 31, 88, 6); time.sleep(0.6); goto(88, 6, 88, 17, 'click'); time.sleep(4)
    im = frame(); snap(f'{tag}-window'); n3 = icon3(im); x = 109 if n3 > 100 else 37
    log(f'{tag}: window third-icon ink {n3} -> LURKING.PRG at x={x}')
    goto(88, 17, x, 97, 'click'); goto(x, 97, 88, 6); time.sleep(0.6); goto(88, 6, 88, 17, 'click'); time.sleep(20)
    im = frame(); snap(f'{tag}-game'); b = bar(im); log(f'{tag}: status bar {b:.2f}')
    if b < 0.9: log(f'{tag}: game status bar missing; STOP'); c({'op': 'stop'}); sys.exit(3)
    tap(' '); time.sleep(2)
def filename(name):
    r = subprocess.run([f'{K}/selname.py', R, name]).returncode; log(f'file selector {name!r} rc={r}'); return r == 0
def step(l, prev): return filename(l) if prev in ('save', 'restore') else command(l)
def script(path): return [l for l in open(path).read().splitlines() if l]
if FIRST <= 1:
  launch('s1'); log('s1 start')
  prev = ''
  for l in script(f'{K}/../tools/lh-script.txt'):
    if l == '<SPACE>': continue  # command() pages any [MORE] before the next line
    if not step(l, prev): sys.exit(4)
    prev = l; time.sleep(4)
  time.sleep(3); snap('s1-saved'); log('s1 end')
for s in [x for x in (2, 3) if x >= FIRST]:
    launch(f's{s}'); log(f's{s} start')
    prev = ''
    for l in script(f'{K}/runs/m1/lh-play{s}.txt'):
        if not step(l, prev): sys.exit(4)
        prev = l; time.sleep(4)
    time.sleep(3); snap(f's{s}-end'); log(f's{s} end')
c({'op': 'stop'}); log('done')
