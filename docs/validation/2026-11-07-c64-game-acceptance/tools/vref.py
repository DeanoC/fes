#!/usr/bin/env python3
"""VICE x64sc reference driver for the fes#1531 C64 game matrix (Claude for Deano). Not part of the repo.

usage: vref.py OUT D64 [--script FILE]

Starts headless x64sc (PAL, 6581, true-drive 1541-II, the same BASIC/KERNAL/
character/1541-II ROM files the kit firmware is composed from) with D64 attached
to drive 8, connects its binary monitor and runs JSON commands (one per line)
from FILE or stdin, the same vocabulary as c64run.py where it applies:

  type text        keyboard feed (KERNAL key buffer, PETSCII; \\n is Return)
  joy port dirs [hold] [gap] [n]   port 1/2, dirs of U D L R F
  joydown port dirs / joyup port
  wander port seconds [fire] [seed] [grab_every] [prefix]
  grab name        VIC display to fr/<name>.png (full debug border area) + .bin indexed
  mem addr len name  read C64 memory to mem/<name>.bin
  sleep s
  reset            hard reset
  attach d64       attach another image to drive 8 (monitor `attach`)
  quit
Audio is written by VICE's wav sound device to OUT/audio.wav for the whole run.
"""
import json
import os
import random
import socket
import struct
import subprocess
import sys
import time
import zlib
from pathlib import Path

W = Path('/home/deano/fes-scratch/c64acc')
X64 = W / 'vice/inst/bin/x64sc'
ROMS = Path('/mnt/emuhd/BIOS/C64/C64 Forever 10 Plus')
OUT = Path(sys.argv[1])
D64 = Path(sys.argv[2])
SCRIPT = sys.argv[4] if len(sys.argv) > 4 and sys.argv[3] == '--script' else None
OUT.mkdir(parents=True, exist_ok=True)
(OUT / 'fr').mkdir(exist_ok=True)
(OUT / 'mem').mkdir(exist_ok=True)
LOG = (OUT / 'vref.log').open('a')
JOY = {'U': 1, 'D': 2, 'L': 4, 'R': 8, 'F': 16}
# C64 keyboard matrix: row = CIA1 PA line, column = PB bit.
_ROWS = [['delete', 'return', 'right', 'f7', 'f1', 'f3', 'f5', 'down'],
         ['3', 'w', 'a', '4', 'z', 's', 'e', 'lshift'],
         ['5', 'r', 'd', '6', 'c', 'f', 't', 'x'],
         ['7', 'y', 'g', '8', 'b', 'h', 'u', 'v'],
         ['9', 'i', 'j', '0', 'm', 'k', 'o', 'n'],
         ['+', 'p', 'l', '-', '.', ':', '@', ','],
         ['pound', '*', ';', 'home', 'rshift', '=', 'uparrow', '/'],
         ['1', 'leftarrow', 'ctrl', '2', 'space', 'cbm', 'q', 'runstop']]
MATRIX = {name: (r, c) for r, row in enumerate(_ROWS) for c, name in enumerate(row)}


def log(*parts):
    line = time.strftime('%F %T ') + ' '.join(str(p) for p in parts)
    print(line, flush=True)
    LOG.write(line + '\n')
    LOG.flush()


def png(path, w, h, rgb):
    raw = b''.join(b'\x00' + rgb[y * w * 3:(y + 1) * w * 3] for y in range(h))
    chunk = lambda t, d: struct.pack('>I', len(d)) + t + d + struct.pack('>I', zlib.crc32(t + d) & 0xffffffff)
    Path(path).write_bytes(b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0))
                           + chunk(b'IDAT', zlib.compress(raw, 6)) + chunk(b'IEND', b''))


class Vice:
    def __init__(self):
        with socket.socket() as s:
            s.bind(('127.0.0.1', 0))
            self.port = s.getsockname()[1]
        with socket.socket() as s:
            s.bind(('127.0.0.1', 0))
            self.tport = s.getsockname()[1]
        # Never attach a library image: VICE writes to attached D64s (high scores, configs). Use a private copy.
        self.work = OUT / 'disk'
        self.work.mkdir(exist_ok=True)
        disk = self.work / D64.name
        disk.write_bytes(D64.read_bytes())
        args = [str(X64), '-default', '-pal', '-sidmodel', '0', '-drive8type', '1542', '-drive8truedrive',
                '+virtualdev8', '-kernal', str(ROMS / 'c-64-kernal.rom'), '-basic', str(ROMS / 'c-64-basic.rom'),
                '-chargen', str(ROMS / 'c-64-chargen.rom'), '-dos1541II', str(ROMS / 'c-drive-1541-ii.rom'),
                '-sounddev', 'wav', '-soundarg', str(OUT / 'audio.wav'), '-sound', '-soundrate', '48000',
                '-binarymonitor', '-binarymonitoraddress', f'ip4://127.0.0.1:{self.port}',
                '-remotemonitor', '-remotemonitoraddress', f'ip4://127.0.0.1:{self.tport}',
                '-8', str(disk)]
        log('x64sc', ' '.join(args))
        self.proc = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=(OUT / 'x64sc.log').open('ab'),
                                     stderr=subprocess.STDOUT, start_new_session=True)
        for _ in range(100):
            try:
                self.sock = socket.create_connection(('127.0.0.1', self.port), timeout=10)
                break
            except OSError:
                time.sleep(0.2)
        else:
            raise SystemExit('no binary monitor')
        self.rid = 0
        self.palette = None

    def cmd(self, ctype, body=b'', resume=True):
        self.rid += 1
        self.sock.sendall(struct.pack('<BBIIB', 2, 2, len(body), self.rid, ctype) + body)
        while True:
            hdr = self._recv(12)
            _, _, length, rtype, err, rid = struct.unpack('<BBIBBI', hdr)
            data = self._recv(length)
            if rid == self.rid:
                if err:
                    raise RuntimeError(f'monitor cmd {ctype:#x} error {err:#x}')
                break
        if resume:
            self._exit()
        return data

    def _exit(self):
        self.rid += 1
        self.sock.sendall(struct.pack('<BBIIB', 2, 2, 0, self.rid, 0xaa))
        while True:
            hdr = self._recv(12)
            _, _, length, rtype, err, rid = struct.unpack('<BBIBBI', hdr)
            self._recv(length)
            if rid == self.rid:
                return

    def _recv(self, n):
        buf = b''
        while len(buf) < n:
            chunk = self.sock.recv(n - len(buf))
            if not chunk:
                raise SystemExit('monitor closed')
            buf += chunk
        return buf

    def joy(self, port, value):
        self.cmd(0xa4, bytes([port - 1, value]))

    def keyset(self, names, value):
        for n in names:
            r, c = MATRIX[n]
            self.cmd(0xa3, bytes([r, c, value]))

    def type(self, text):
        data = text.replace('\n', '\r').encode('latin1')
        for i in range(0, len(data), 8):
            part = data[i:i + 8]
            self.cmd(0x72, bytes([len(part)]) + part)
            time.sleep(0.35)

    def grab(self, name):
        if self.palette is None:
            p = self.cmd(0x91, b'\x01', resume=False)
            n = struct.unpack('<H', p[:2])[0]
            pal, i = [], 2
            for _ in range(n):
                size = p[i]
                pal.append(bytes(p[i + 1:i + 4]))
                i += 1 + size
            self.palette = pal
        d = self.cmd(0x84, b'\x01\x00')
        fl = struct.unpack('<I', d[:4])[0]
        dw, dh, xo, yo, iw, ih, bp = struct.unpack('<HHHHHHB', d[4:17])
        bl = struct.unpack('<I', d[17:21])[0]
        buf = d[21:21 + bl]
        # VICE 3.9 sizes this response without the 4-byte length field, so the
        # last 4 pixels (bottom-right border) are cut off; pad them with the first pixel.
        buf = buf + bytes([buf[0]]) * (dw * dh - len(buf))
        (OUT / 'fr' / f'{name}.idx').write_bytes(struct.pack('<HHHHHH', dw, dh, xo, yo, iw, ih) + buf)
        rgb = b''.join(self.palette[b] for b in buf)
        png(OUT / 'fr' / f'{name}.png', dw, dh, rgb)
        return {'grab': name, 'w': dw, 'h': dh, 'xo': xo, 'yo': yo, 'iw': iw, 'ih': ih}

    def mem(self, addr, length, name):
        body = struct.pack('<BHHBH', 0, addr, addr + length - 1, 0, 0)
        d = self.cmd(0x01, body)
        n = struct.unpack('<H', d[:2])[0]
        (OUT / 'mem' / f'{name}.bin').write_bytes(d[2:2 + n])
        return {'mem': name, 'bytes': n}

    def attach(self, path):
        # Text remote monitor: attach a disk image to drive 8 while the machine runs, then leave the monitor.
        copy = self.work / Path(path).name
        copy.write_bytes(Path(path).read_bytes())
        path = str(copy)
        with socket.create_connection(('127.0.0.1', self.tport), timeout=10) as t:
            t.sendall(f'attach "{path}" 8\n'.encode())
            time.sleep(1.0)
            t.sendall(b'x\n')
            time.sleep(0.5)
            try:
                t.settimeout(1)
                reply = t.recv(4096).decode('latin1', 'replace')
            except OSError:
                reply = ''
        return {'attach': path, 'monitor': reply[-200:]}

    def quit(self):
        try:
            self.rid += 1
            self.sock.sendall(struct.pack('<BBIIB', 2, 2, 0, self.rid, 0xbb))
        except OSError:
            pass
        try:
            self.proc.wait(10)
        except subprocess.TimeoutExpired:
            self.proc.kill()


def main():
    v = Vice()
    held = {1: 0, 2: 0}
    t0 = time.monotonic()
    def follow(path):
        # Follow an append-only command file until a quit line.
        with open(path) as f:
            while True:
                line = f.readline()
                if not line:
                    time.sleep(0.2)
                    continue
                yield line
    lines = follow(SCRIPT) if SCRIPT else sys.stdin
    events = []
    try:
        for line in lines:
            line = line.strip()
            if not line or line.startswith('#'):
                continue
            op = json.loads(line)
            c = op['op']
            reply = {}
            if c == 'quit':
                break
            if c == 'type':
                v.type(op['text'])
            elif c == 'key':
                names = op.get('mods', []) + [op['name']]
                v.keyset(names, 1)
                time.sleep(op.get('hold', 0.1))
                v.keyset(list(reversed(names)), 0)
                time.sleep(op.get('gap', 0.1))
            elif c == 'joy':
                val = sum(JOY[d] for d in op['dirs'])
                for _ in range(op.get('n', 1)):
                    v.joy(op['port'], val)
                    time.sleep(op.get('hold', 0.15))
                    v.joy(op['port'], 0)
                    time.sleep(op.get('gap', 0.1))
            elif c == 'joydown':
                held[op['port']] = sum(JOY[d] for d in op['dirs'])
                v.joy(op['port'], held[op['port']])
            elif c == 'joyup':
                held[op['port']] = 0
                v.joy(op['port'], 0)
            elif c == 'wander':
                rng = random.Random(op.get('seed', 1))
                end = time.monotonic() + op['seconds']
                ge = op.get('grab_every', 30)
                nxt, n, moves = time.monotonic() + ge, 0, 0
                while time.monotonic() < end:
                    d = rng.choice(['U', 'D', 'L', 'R', 'UL', 'UR', 'DL', 'DR', '', 'R', 'L'])
                    if op.get('fire', True) and rng.random() < 0.45:
                        d += 'F'
                    v.joy(op['port'], sum(JOY[x] for x in d))
                    moves += 1
                    time.sleep(rng.uniform(0.15, 0.7))
                    if time.monotonic() >= nxt:
                        v.joy(op['port'], 0)
                        v.grab(f"{op.get('prefix', 'w')}-{n:03d}")
                        n += 1
                        nxt = time.monotonic() + ge
                v.joy(op['port'], 0)
                reply = {'moves': moves, 'grabs': n}
            elif c == 'grab':
                reply = v.grab(op['name'])
            elif c == 'mem':
                reply = v.mem(op['addr'], op['len'], op['name'])
            elif c == 'sleep':
                time.sleep(op['s'])
            elif c == 'reset':
                v.cmd(0xcc, b'\x01')
            elif c == 'attach':
                reply = v.attach(op['d64'])
            else:
                raise ValueError(c)
            ev = {'t': round(time.monotonic() - t0, 2), 'op': c, 'args': {k: x for k, x in op.items() if k != 'op'},
                  'reply': reply}
            events.append(ev)
            log(json.dumps(ev)[:300])
    finally:
        (OUT / 'events.json').write_text(json.dumps(events, indent=1))
        v.quit()


if __name__ == '__main__':
    main()
