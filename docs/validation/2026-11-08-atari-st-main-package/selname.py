#!/usr/bin/env python3
"""selname.py RUN NAME: in the GEM file selector, Esc clears Selection, type NAME, check the field shows exactly len(NAME)
name characters and no extension (a cell counts at >= 14 dark px, so the text cursor bar does not; 12 px cells from x=384, glyph rows 206-213 at 960x540), retry up to 4 times, then Enter.
Exit 0 when Enter was sent, 4 when the field never matched (nothing confirmed)."""
import json, subprocess, sys, time, urllib.request
R, NAME = sys.argv[1], sys.argv[2]; D = f'/home/deano/fes-scratch/st1528/kit/runs/{R}'
HID = {c: 4 + i for i, c in enumerate('abcdefghijklmnopqrstuvwxyz')}; HID.update({str(i): 0x1e + (i - 1) for i in range(1, 10)}); HID['0'] = 0x27
O = urllib.request.build_opener(urllib.request.ProxyHandler({})); LOG = open(f'{D}/lhrun.log', 'a')
def ev(code, down):
    b = json.dumps({'event': {'Player': 0, 'Device': 0, 'Kind': 0, 'Action': 1 if down else 0, 'Code': 0x1000 + code, 'Value': 0}}).encode()
    O.open(urllib.request.Request('http://127.0.0.1:18787/api/v1/session/input/event', b, {'Content-Type': 'application/json'}, method='POST'), timeout=20).read()
def tap(code, shift=False):
    if shift: ev(0xe1, True)
    ev(code, True); time.sleep(0.05); ev(code, False)
    if shift: ev(0xe1, False)
    time.sleep(0.25)
def field():
    time.sleep(0.8)
    im = subprocess.run(['ffmpeg', '-loglevel', 'error', '-i', f'{D}/live.png', '-f', 'rawvideo', '-pix_fmt', 'gray', '-'], capture_output=True).stdout
    ink = [sum(im[y*960 + 384 + 12*c + dx] < 100 for y in range(206, 214) for dx in range(12)) >= 14 for c in range(12)]  # text cursor bar < 14 px
    return sum(ink[:8]), sum(ink[9:12])
for t in range(1, 5):
    tap(0x29); time.sleep(0.5)
    for ch in NAME: tap(HID[ch.lower()], ch.isupper())
    n, e = field(); LOG.write(f'{time.strftime("%T")} selector try {t} {NAME!r} name_cells={n} ext_cells={e}\n'); LOG.flush()
    if n == len(NAME) and e == 0: tap(0x28); sys.exit(0)
sys.exit(4)
