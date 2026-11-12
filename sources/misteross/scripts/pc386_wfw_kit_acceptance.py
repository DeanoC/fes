#!/usr/bin/env python3
"""Windows for Workgroups 3.11 kit acceptance for fes.pc386
(docs/pc386-windows-corpus.md): a sealed package boots the wfw-msdos drive
(MS-DOS 6.22, WfW 3.11, 32-bit disk access, a permanent swap file) on a kit,
and four goals must pass, each judged by the script:

  program-manager  `win` at the DOS prompt reaches Program Manager
                   (pc386_wfw.program_manager on HDMI grabs).
  mouse            a double-click on Program Manager's title bar maximizes
                   it. The pointer is parked in the top-left corner, then
                   moved in small steps whose pixels per count are measured
                   from the grabs, so the click lands on the title bar.
  dos-window       File > Run DOSPRMPT.PIF opens a full-screen MS-DOS Prompt (a
                   text screen), which writes a line holding this run's nonce
                   to C:\\WFWDOS.TXT; `exit` returns to Program Manager.
  notepad          File > Run `notepad c:\\wfwnote.txt` saves a line holding
                   the nonce, and Notepad exits back to Program Manager.

Windows is then exited to the DOS prompt and the machine stopped, which saves
the run's ATA disk delta on the kit. The script reads that delta back through
--kit-ssh, applies it to a private copy of the drive, and both files must hold
the nonce: the DOS window's write and Notepad's save reached the disk.

Precondition, as the kit rules (CLAUDE.md) require: the caller has claimed the
kit in its status file (the latest state line is an IN USE claim naming
--owner) and holds the kit's queue lock, and logs the restore and release
afterwards. This is a core-only run: nothing is installed and the image is not
touched. The script takes the agent's kit lease, stages the drive by SHA-256,
launches the package as a fresh library game, types and moves the mouse
through the agent's input stream, grabs HDMI from --video, stops the machine
and releases the lease on every exit path. It then removes the FogCast data
trees the run created (its game records, disk delta and staged drive; never
one that was there before), and checks that the agent is idle with a free
lease and has not rebooted. It never prints a token.

  python3 scripts/pc386_wfw_kit_acceptance.py --kit B --owner Claude \\
      --package fes.pc386.fcore --out runs/wfw-k1 --kit-ssh ~/fes-scratch/m5/kB/kb

Writes OUT/result.json (each goal's pass and evidence) and OUT/frames/*.png;
exits 0 only when every goal passed and the cleanup was verified.
"""
from __future__ import annotations

import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import string
import struct
import subprocess
import sys
import tempfile
import threading
import time
import tomllib

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_pc386_game_drive as drives
from scripts import kit as kitlib
from scripts import pc386_wfw as wfw

CORE = 'fes.pc386'
DRIVE = 'wfw-msdos'
FAT = '/media/fat/fogcast'
DATA_DIRS = (f'{FAT}/disk-images', f'{FAT}/core-data/games', f'{FAT}/core-data/media')
RUN_TREE = re.compile(rf'^{re.escape(FAT)}/(core-data/(games|media)/[0-9a-f]{{64}}|disk-images/[0-9a-f]{{64}}\.img)$')
# Powerboat's capture devices and FogCast target names for the two test kits.
KITS = {'A': {'target': 'dev', 'video': '/dev/v4l/by-id/fes-shadowcast-hil'},
        'B': {'target': 'kit2', 'video': '/dev/v4l/by-id/fes-4kpro-hil'}}
KEYS = {**{chr(ord('a') + i): 0x04 + i for i in range(26)}, **{str(i): 0x1e + i - 1 for i in range(1, 10)},
        '0': 0x27, 'enter': 0x28, 'esc': 0x29, 'backspace': 0x2a, 'tab': 0x2b, 'space': 0x2c, '-': 0x2d,
        '=': 0x2e, '[': 0x2f, ']': 0x30, '\\': 0x31, ';': 0x33, "'": 0x34, '`': 0x35, ',': 0x36, '.': 0x37,
        '/': 0x38, 'lshift': 0xe1, 'lalt': 0xe2}
SHIFTED = dict(zip('!@#$%^&*()_+{}|:"~<>?', '1234567890-=[]\\;\'`,./'))
CHARS = {' ': 'space', '\n': 'enter'}
PARK = 40          # packets of -20 counts that pin the pointer in the top-left corner
STEP = 2           # counts per calibration packet: at or below Windows' first acceleration threshold
CALIBRATE = 40     # calibration packets per axis


class AcceptanceError(RuntimeError):
    pass


def make_nonce() -> str:
    return ''.join(secrets.choice(string.ascii_uppercase + string.digits) for _ in range(12))


def holds_nonce(data: bytes | None, nonce: str) -> bool:
    return data is not None and re.search(rb'(?<![A-Z0-9])' + nonce.encode() + rb'(?![A-Z0-9])', data) is not None


def caption_target(found: dict) -> tuple[int, int]:
    """A point on Program Manager's title bar, left of its caption text and
    clear of the system-menu box and the buttons."""
    caption = found['caption']
    return caption['x0'] + (caption['x1'] - caption['x0']) // 4, (caption['y0'] + caption['y1']) // 2


def maximized_by_click(before: dict, after: dict) -> bool:
    return bool(before.get('found') and not before.get('maximized') and after.get('found') and after.get('maximized'))


def cursor_tip(before: wfw.Frame, after: wfw.Frame, away_from: tuple[int, int]) -> tuple[int, int] | None:
    """Where the pointer went between two 640x480 pictures: the top-left of the
    pixels that changed, ignoring a 40-pixel box around where it was."""
    xs, ys = [], []
    for y in range(after.height):
        for x in range(after.width):
            if abs(x - away_from[0]) < 40 and abs(y - away_from[1]) < 40:
                continue
            if max(abs(a - b) for a, b in zip(before.pixel(x, y), after.pixel(x, y))) > 80:
                xs.append(x)
                ys.append(y)
    if len(xs) < 20:
        return None
    return min(xs), min(ys)


class Input:
    """The agent's remote input stream (FogCast host/remote_input.go): attach,
    CONNECT, hello, then 52-byte frames, with pings to keep it alive."""

    def __init__(self, client, lease):
        self.client, self.lease = client, lease
        self.session = secrets.randbits(63) | 1
        self.proof = secrets.token_bytes(16)
        self.seq = 0
        self.lock = threading.Lock()
        self.done = threading.Event()
        attached = post(client, '/v1/input/attach', {}, token=lease,
                        body={'session': self.session, 'token': self.proof.hex(), 'core': CORE})
        if not attached.get('ready'):
            raise AcceptanceError('input attach was not ready')
        if client.url.scheme != 'http':
            raise AcceptanceError('the input stream needs an http target')
        port = client.url.port or 80
        self.sock = socket.create_connection((client.url.hostname, port), timeout=10)
        self.sock.sendall((f'CONNECT /v1/input/stream HTTP/1.1\r\nHost: {client.url.hostname}:{port}\r\n'
                           f'Authorization: Bearer {client.bearer}\r\nX-FogCast-Input-Session: {self.session}\r\n'
                           f'{kitlib.HEADER}: {lease}\r\n\r\n').encode())
        self.reader = self.sock.makefile('rb')
        if b' 200 ' not in self.reader.readline():
            raise AcceptanceError('input stream refused')
        while self.reader.readline() not in (b'\r\n', b'\n', b''):
            pass
        hello = json.dumps({'version': 1, 'session': self.session, 'core': CORE, 'proof': self.proof.hex()})
        self.sock.sendall(hello.encode() + b'\n')
        if json.loads(self.reader.readline()).get('ok') is not True:
            raise AcceptanceError('input hello refused')
        self.sock.settimeout(None)
        threading.Thread(target=self._drain, daemon=True).start()
        threading.Thread(target=self._ping, daemon=True).start()

    def _frame(self, kind, device=0, event_kind=0, action=0, code=0, value=0):
        with self.lock:
            if kind == 1:
                self.seq += 1
            header = struct.pack('<I4sBBHQ', 48, b'MSTR', 1, kind, 0, self.session)
            payload = struct.pack('<IQQBBBBHHi', self.seq if kind == 1 else 0, time.monotonic_ns(), 0, 0, device,
                                  event_kind, action, code, 0, value)
            self.sock.sendall(header + payload)

    def _drain(self):
        while not self.done.is_set():
            try:
                if not self.reader.read(52):
                    return
            except OSError:
                return

    def _ping(self):
        while not self.done.wait(0.2):
            try:
                self._frame(2)
            except OSError:
                return

    def key(self, name, down):
        self._frame(1, device=0, event_kind=0, action=1 if down else 0, code=0x1000 + KEYS[name])

    def tap(self, name, mods=(), hold=0.06, gap=0.08):
        for mod in mods:
            self.key(mod, True)
        self.key(name, True)
        time.sleep(hold)
        self.key(name, False)
        for mod in reversed(mods):
            self.key(mod, False)
        time.sleep(gap)

    def type(self, text):
        for character in text:
            if character in CHARS:
                self.tap(CHARS[character])
            elif character.isupper():
                self.tap(character.lower(), ('lshift',))
            elif character in SHIFTED:
                self.tap(SHIFTED[character], ('lshift',))
            else:
                self.tap(character)

    def mouse(self, dx, dy, buttons=0, count=1, gap=0.03):
        for _ in range(count):
            self._frame(1, device=2, event_kind=4, action=3, code=buttons,
                        value=struct.unpack('<i', struct.pack('<HH', dx & 0xffff, dy & 0xffff))[0])
            time.sleep(gap)

    def double_click(self):
        for _ in range(2):
            self.mouse(0, 0, 1)
            time.sleep(0.05)
            self.mouse(0, 0, 0)
            time.sleep(0.08)

    def close(self):
        self.done.set()
        try:
            self.sock.close()
        finally:
            post(self.client, '/v1/input/detach', {}, token=self.lease, body={'session': self.session})


def post(client, path, headers, token=None, body_path=None, body=None, timeout=60):
    """POST with FogCast headers, as kit.Client.request does; never echoes a body."""
    head = {'Authorization': 'Bearer ' + client.bearer, **headers}
    if token:
        head[kitlib.HEADER] = token
    payload = json.dumps(body).encode() if body is not None else b''
    head['Content-Length'] = str(body_path.stat().st_size if body_path else len(payload))
    if body_path:
        head['Content-Type'] = 'application/octet-stream'
    elif body is not None:
        head['Content-Type'] = 'application/json'
    connection = http.client.HTTPConnection(client.url.hostname, client.url.port, timeout=timeout)
    try:
        connection.putrequest('POST', path)
        for key, value in head.items():
            connection.putheader(key, value)
        connection.endheaders()
        if body_path:
            with body_path.open('rb') as stream:
                while chunk := stream.read(65536):
                    connection.send(chunk)
        elif payload:
            connection.send(payload)
        response = connection.getresponse()
        raw = response.read(1 << 20)
        try:
            result = json.loads(raw)
        except ValueError:
            result = {}
        if response.status >= 300:
            error = result.get('error', result) if isinstance(result, dict) else {}
            safe = {k: error.get(k) for k in ('code', 'message', 'phase') if isinstance(error, dict) and k in error}
            raise kitlib.KitError(f'{path}: HTTP {response.status} {json.dumps(safe)}', response.status)
        return result
    finally:
        connection.close()


def target_client(config: Path, target: str):
    """The kit's agent client from the FogCast host config's target entry."""
    with config.open('rb') as stream:
        data = tomllib.load(stream)
    found = [entry for entry in data.get('targets', []) if entry.get('name') == target]
    if len(found) != 1:
        raise AcceptanceError(f'{config} has {len(found)} targets named {target}')
    return kitlib.Client(found[0].get('address', ''), found[0].get('agent', ''))


def check_claim(status_file: Path, kit: str, owner: str) -> str:
    """The latest state line of the kit's status file must be an IN USE claim by owner."""
    lines = [line for line in status_file.read_text().splitlines()
             if re.search(rf'\bKIT {kit} (FREE|IN USE|RESTORE_INCOMPLETE|HELD)\b', line)]
    last = lines[-1] if lines else ''
    if not re.search(rf'\bKIT {kit} IN USE:\s*{re.escape(owner)}', last):
        raise AcceptanceError(f'{status_file}: the latest state line is not an IN USE claim by {owner}; '
                              'claim the kit first (CLAUDE.md, "Shared kits")')
    return last


class Run:
    def __init__(self, args):
        self.args = args
        self.out = args.out
        (self.out / 'frames').mkdir(parents=True, exist_ok=True)
        self.client = target_client(args.config, args.target)
        self.image, self.record = drives.resolve(args.drive)
        self.nonce = make_nonce()
        self.game = f'wfw-acceptance-{self.nonce.lower()}'
        self.session = None
        self.remote = None
        self.recovery_required = False
        self.result = {'kit': args.kit, 'package': args.package.name, 'drive': self.record.get('drive'),
                       'drive_sha256': self.record['sha256'], 'library_game': self.game, 'nonce': self.nonce,
                       'goals': {}, 'events': []}

    # -- bookkeeping --------------------------------------------------------
    def log(self, *parts):
        line = time.strftime('%F %T ') + ' '.join(str(part) for part in parts)
        print(line, flush=True)
        with (self.out / 'run.log').open('a') as stream:
            stream.write(line + '\n')

    def save(self):
        (self.out / 'result.json').write_text(json.dumps(self.result, indent=1) + '\n')

    def goal(self, name, passed, **evidence):
        evidence.pop('pass', None)
        self.result['goals'][name] = {'pass': bool(passed), **evidence}
        self.log('GOAL', name, 'PASS' if passed else 'FAIL', json.dumps(evidence)[:300])
        self.save()
        if not passed:
            raise AcceptanceError(f'goal {name} failed')

    def ssh(self, command, timeout=120, binary=False):
        result = subprocess.run([str(self.args.kit_ssh), command], stdin=subprocess.DEVNULL, capture_output=True,
                                timeout=timeout)
        if result.returncode:
            raise AcceptanceError(f'kit command failed ({result.returncode}): {command[:80]}')
        return result.stdout if binary else result.stdout.decode()

    def kit_paths(self) -> list[str]:
        return sorted(line for line in self.ssh(f'find {" ".join(DATA_DIRS)} -maxdepth 1 2>/dev/null; true')
                      .splitlines() if RUN_TREE.match(line))

    # -- frames -------------------------------------------------------------
    def grab(self, name) -> wfw.Frame:
        dest = self.out / 'frames' / f'{name}.png'
        for attempt in range(4):
            command = ['timeout', '-k', '5', '20', 'ffmpeg', '-loglevel', 'error', '-y', '-f', 'v4l2',
                       *(['-input_format', 'mjpeg'] if attempt % 2 else []), '-i', self.args.video,
                       '-vf', 'select=gte(n\\,5)', '-frames:v', '1', str(dest)]
            if subprocess.run(command, capture_output=True).returncode == 0 and dest.is_file() \
                    and dest.stat().st_size > 2000:
                return wfw.load(dest)
            time.sleep(1)
        raise AcceptanceError(f'HDMI grab {name} failed')

    def wait_for(self, name, test, timeout, every=1.0):
        """Grab until test(frame) holds; the last grab keeps the name."""
        began = time.monotonic()
        index = 0
        while True:
            frame = self.grab(f'{name}-{index:03d}')
            found = test(frame)
            if found:
                shutil.copyfile(self.out / 'frames' / f'{name}-{index:03d}.png', self.out / 'frames' / f'{name}.png')
                for path in (self.out / 'frames').glob(f'{name}-[0-9][0-9][0-9].png'):
                    path.unlink()
                return frame, round(time.monotonic() - began, 1)
            if time.monotonic() - began > timeout:
                return None, round(time.monotonic() - began, 1)
            index += 1
            time.sleep(every)

    # -- the kit ------------------------------------------------------------
    def input(self):
        if self.remote is None:
            self.remote = Input(self.client, self.session.token)
        return self.remote

    def detach(self):
        if self.remote is not None:
            remote, self.remote = self.remote, None
            remote.close()

    def stage(self, image: Path, digest: str):
        if post(self.client, '/v1/library/disk-images/probe', {'X-FogCast-Disk-Image': digest}).get('staged'):
            return
        post(self.client, '/v1/library/disk-images', {'X-FogCast-Disk-Image': digest},
             token=self.session.token, body_path=image, timeout=600)

    def load(self, game, image, record):
        self.stage(image, record['sha256'])
        # A drive without a record (the scrub disk) has 16 heads and 63 sectors per track.
        geometry = drives.geometry(record) if 'geometry' in record else \
            f'{image.stat().st_size // (16 * 63 * 512)}/16/63'
        post(self.client, '/v1/library/core/load', {
            'X-FogCast-Package-ID': self.args.package.stem, 'X-FogCast-Library-Game': game,
            'X-FogCast-Disk-Image': record['sha256'], 'X-FogCast-Disk-Geometry': geometry,
        }, token=self.session.token, body_path=self.args.package, timeout=360)
        self.session.needs_stop = True

    def stop(self):
        self.detach()
        stopped = self.client.request('/v1/stop', token=self.session.token, timeout=180)
        self.session.needs_stop = False
        if stopped.get('recovery') == 'reboot_required':
            self.recovery_required = True
            raise AcceptanceError('stop reported reboot_required; no recovery attempted')

    # -- goals ----------------------------------------------------------------
    def boot_dos(self):
        if self.args.scrub:
            scrub, record = drives.resolve(str(self.args.scrub))
            self.log('scrub', record['sha256'][:16])
            self.load(f'{self.game}-scrub', scrub, record)
            time.sleep(self.args.scrub_seconds)
            self.stop()
        self.load(self.game, self.image, self.record)
        time.sleep(10)
        frame, seconds = self.wait_for('dos-prompt', lambda frame: wfw.text_screen(frame)['found'], 60, every=2)
        if frame is None:
            raise AcceptanceError('MS-DOS did not reach a text prompt')
        time.sleep(5)  # SMARTDRV and the prompt

    def program_manager(self):
        self.input().type('win\n')
        frame, seconds = self.wait_for('program-manager', lambda frame: wfw.program_manager(frame)['found'], 90)
        self.goal('program-manager', frame is not None, seconds_to_detect=seconds,
                  frame='frames/program-manager.png')
        self.manager = wfw.program_manager(frame)

    def mouse(self):
        remote = self.input()
        remote.mouse(-20, -20, count=PARK)
        time.sleep(1)
        parked = wfw.picture(self.grab('mouse-parked'))
        remote.mouse(STEP, STEP, count=CALIBRATE)
        time.sleep(1)
        moved = wfw.picture(self.grab('mouse-moved'))
        tip = cursor_tip(parked, moved, (0, 0))
        if tip is None or tip[0] <= 0 or tip[1] <= 0:
            self.goal('mouse', False, reason=f'the pointer did not move visibly ({tip})')
        scale = (tip[0] / (STEP * CALIBRATE), tip[1] / (STEP * CALIBRATE))
        target = caption_target(self.manager)
        dx, dy = round((target[0] - tip[0]) / scale[0]), round((target[1] - tip[1]) / scale[1])
        for axis, counts in (('x', dx), ('y', dy)):
            step = STEP if counts > 0 else -STEP
            for _ in range(abs(counts) // STEP):
                remote.mouse(step if axis == 'x' else 0, step if axis == 'y' else 0)
        time.sleep(1)
        aimed = wfw.picture(self.grab('mouse-aimed'))
        landed = cursor_tip(moved, aimed, tip)
        caption = self.manager['caption']
        on_caption = landed is not None and caption['x0'] < landed[0] < caption['x1'] \
            and caption['y0'] - 2 <= landed[1] <= caption['y1'] + 2
        remote.double_click()
        frame, seconds = self.wait_for('mouse-maximized', lambda frame: wfw.program_manager(frame)['maximized'], 10)
        after = wfw.program_manager(frame) if frame is not None else {}
        self.goal('mouse', maximized_by_click(self.manager, after), counts_per_pixel=[round(1 / s, 3) for s in scale],
                  pointer=list(landed) if landed else None, target=list(target), on_caption=on_caption,
                  frame='frames/mouse-maximized.png')
        remote.tap('space', ('lalt',))  # the system menu, then Restore
        time.sleep(0.5)
        remote.tap('r')
        frame, _ = self.wait_for('mouse-restored', lambda frame: (lambda found: found['found'] and not
                                                                  found['maximized'])(wfw.program_manager(frame)), 10)
        if frame is None:
            raise AcceptanceError('Program Manager did not restore after the mouse goal')

    def run_command(self, text):
        remote = self.input()
        remote.tap('f', ('lalt',))
        time.sleep(0.6)
        remote.tap('r')
        time.sleep(1.5)
        remote.type(text + '\n')

    def dos_window(self):
        self.run_command('dosprmpt.pif')
        frame, opened = self.wait_for('dos-window', lambda frame: wfw.text_screen(frame)['found'], 30)
        if frame is None:
            self.goal('dos-window', False, reason='no text screen after File > Run DOSPRMPT.PIF')
        time.sleep(2)
        self.input().type(f'echo FESWFW DOS {self.nonce} > c:\\wfwdos.txt\n')
        time.sleep(3)
        self.grab('dos-window-echo')
        self.input().type('exit\n')
        frame, back = self.wait_for('dos-window-exit', lambda frame: wfw.program_manager(frame)['found'], 30)
        self.goal('dos-window', frame is not None, opened_seconds=opened, returned_seconds=back,
                  frames=['frames/dos-window.png', 'frames/dos-window-exit.png'], readback='after stop')

    def notepad(self):
        self.run_command('notepad c:\\wfwnote.txt')
        time.sleep(5)
        self.grab('notepad-new-file')
        remote = self.input()
        remote.tap('enter')  # Yes: create the new file
        time.sleep(2)
        remote.type(f'FESWFW NOTE {self.nonce}\n')
        self.grab('notepad-typed')
        remote.tap('f', ('lalt',))
        time.sleep(0.6)
        remote.tap('s')
        time.sleep(3)
        self.grab('notepad-saved')
        remote.tap('f', ('lalt',))
        time.sleep(0.6)
        remote.tap('x')
        frame, back = self.wait_for('notepad-exit', lambda frame: wfw.program_manager(frame)['found'], 20)
        self.goal('notepad', frame is not None, returned_seconds=back, readback='after stop',
                  frames=['frames/notepad-saved.png', 'frames/notepad-exit.png'])

    def exit_windows(self):
        remote = self.input()
        remote.tap('f', ('lalt',))
        time.sleep(0.6)
        remote.tap('x')
        time.sleep(1.5)
        remote.tap('enter')
        frame, _ = self.wait_for('dos-after-windows', lambda frame: wfw.text_screen(frame)['found'], 30)
        if frame is None:
            raise AcceptanceError('Windows did not exit to the DOS prompt')
        time.sleep(2)
        remote.type('type c:\\wfwnote.txt\n')
        time.sleep(2)
        self.grab('dos-type-note')

    def read_back(self, created: list[str]):
        """Apply the run's disk delta to a private copy of the drive and read both files."""
        media = [path for path in created if '/core-data/media/' in path]
        found = None
        for directory in media:
            # The scrub's media directory may hold no record: it saved nothing.
            record = self.ssh(f"if [ -f '{directory}/record.bin' ]; then cat '{directory}/record.bin'; fi",
                              binary=True)
            if len(record) >= 124 + 32 and record[72:104].hex() == self.record['sha256']:
                found = (directory, record)
        if found is None:
            raise AcceptanceError(f'no disk delta for drive {self.record["sha256"][:16]} among {media}')
        directory, record = found
        if hashlib.sha256(record[:-32]).digest() != record[-32:]:
            raise AcceptanceError('the disk delta record fails its own hash')
        unit, major, minor, length, base, chunk, count = struct.unpack('<HHHHIII', record[104:124])
        if record[124:124 + length] != b'fes.ata-disk.delta' or base != self.image.stat().st_size:
            raise AcceptanceError('the disk delta is not this drive\'s ATA delta')
        with tempfile.TemporaryDirectory(prefix='wfw-readback-') as private:
            os.chmod(private, 0o700)
            copy = Path(private) / 'drive.img'
            shutil.copyfile(self.image, copy)
            at = 124 + length
            with copy.open('r+b') as stream:
                for _ in range(count):
                    index, digest = struct.unpack('<I', record[at:at + 4])[0], record[at + 4:at + 36].hex()
                    at += 36
                    data = self.ssh(f"cat '{directory}/chunk-{digest}'", binary=True)
                    if hashlib.sha256(data).hexdigest() != digest:
                        raise AcceptanceError(f'delta chunk {index} fails its hash')
                    stream.seek(index * chunk)
                    stream.write(data)
            files = {}
            volume = f'{copy}@@{63 * 512}'
            env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null')
            for name in ('WFWDOS.TXT', 'WFWNOTE.TXT'):
                result = subprocess.run(['mtype', '-i', volume, f'::{name}'], capture_output=True, env=env)
                files[name] = result.stdout if result.returncode == 0 else None
        return {'delta_chunks': count, 'files': {name: (data.decode('ascii', 'replace').strip() if data else None)
                                                 for name, data in files.items()},
                'dos': holds_nonce(files['WFWDOS.TXT'], self.nonce),
                'notepad': holds_nonce(files['WFWNOTE.TXT'], self.nonce)}

    # -- the run ------------------------------------------------------------
    def main(self) -> int:
        self.result['claim'] = check_claim(self.args.status_file, self.args.kit, self.args.owner)[:200]
        status, lease = self.client.request('/v1/status'), self.client.request('/v1/kit/lease')
        if status.get('state') != 'idle' or lease.get('state') != 'free':
            raise AcceptanceError('the agent is not idle with a free lease; not touched')
        boot_id = self.client.request('/v1/health').get('boot_id')
        before = self.kit_paths()
        self.session = kitlib.Session(self.client, 'pc386-wfw-acceptance', f'pc386 WfW 3.11 acceptance {self.game}')
        self.session.claim()
        self.log('lease held', self.session.generation, 'game', self.game)
        failure = None
        try:
            self.boot_dos()
            self.program_manager()
            self.mouse()
            self.dos_window()
            self.notepad()
            self.exit_windows()
        except (AcceptanceError, kitlib.KitError, OSError, subprocess.SubprocessError) as error:
            failure = f'{type(error).__name__}: {error}'
            self.log('FAILED', failure)
        finally:
            try:
                if self.session.needs_stop:
                    self.stop()
            except (AcceptanceError, kitlib.KitError, OSError) as error:
                failure = failure or f'stop: {type(error).__name__}: {error}'
                self.log('STOP FAILED', error)
            try:
                self.detach()
            except (kitlib.KitError, OSError) as error:
                self.log('input detach failed', error)
            if self.recovery_required:
                # A kit that needs recovery keeps this run's lease: it is not
                # released as if it were idle. The caller keeps its claim.
                self.session.done.set()
                self.log('lease kept: the kit needs recovery')
            else:
                try:
                    self.session.close()
                    self.log('lease released')
                except (kitlib.KitError, OSError) as error:
                    failure = failure or f'lease release: {type(error).__name__}: {error}'
                    self.log('LEASE RELEASE FAILED', error)
        created = [path for path in self.kit_paths() if path not in before]
        self.result['kit_created'] = created
        try:
            if failure is None:
                readback = self.read_back(created)
                self.result['readback'] = readback
                self.goal('dos-window', readback['dos'] and self.result['goals']['dos-window']['pass'],
                          **{**self.result['goals']['dos-window'], 'readback': readback['files']['WFWDOS.TXT']})
                self.goal('notepad', readback['notepad'] and self.result['goals']['notepad']['pass'],
                          **{**self.result['goals']['notepad'], 'readback': readback['files']['WFWNOTE.TXT']})
        except Exception as error:  # noqa: BLE001 - the kit is cleaned up whatever the read-back raised
            failure = f'read-back: {type(error).__name__}: {error}'
        finally:
            for path in created:
                try:
                    self.ssh(f"rm -r -- '{path}'")
                except (AcceptanceError, OSError, subprocess.SubprocessError) as error:
                    failure = failure or f'removing {path}: {error}'  # the check below lists it as left
        left = [path for path in self.kit_paths() if path not in before]
        status, lease = self.client.request('/v1/status'), self.client.request('/v1/kit/lease')
        after = {'agent': status.get('state'), 'lease': lease.get('state'),
                 'rebooted': self.client.request('/v1/health').get('boot_id') != boot_id, 'left': left,
                 'recovery_required': self.recovery_required}
        self.result['after'] = after
        if left or after['agent'] != 'idle' or after['lease'] != 'free' or after['rebooted'] or \
                self.recovery_required:
            failure = failure or f'the kit is not as found: {after}'
        self.result['failure'] = failure
        self.result['pass'] = failure is None and all(goal['pass'] for goal in self.result['goals'].values()) \
            and len(self.result['goals']) == 4
        self.save()
        self.log('PASS' if self.result['pass'] else f'FAIL {failure}')
        return 0 if self.result['pass'] else 1


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--kit', choices=sorted(KITS), required=True)
    parser.add_argument('--owner', required=True, help='the owner named in the kit status file\'s IN USE claim')
    parser.add_argument('--package', type=Path, required=True, help='the sealed fes.pc386 package (.fcore)')
    parser.add_argument('--out', type=Path, required=True, help='a new directory for result.json and frames')
    parser.add_argument('--kit-ssh', type=Path, required=True,
                        help='a command that runs its one argument as a shell command on the kit')
    parser.add_argument('--drive', default=DRIVE, help='the WfW drive by name, SHA-256 or path')
    parser.add_argument('--status-file', type=Path, help='default ~/kit-status/kit<KIT>-STATUS.md')
    parser.add_argument('--target', help="the FogCast host config's target name (default by kit)")
    parser.add_argument('--video', help='the HDMI capture device (default by kit)')
    parser.add_argument('--config', type=Path, default=Path.home() / '.config/fogcast/config.toml')
    parser.add_argument('--scrub', help='a memory-scrub drive to boot first (kits whose baseline does not zero RAM)')
    parser.add_argument('--scrub-seconds', type=float, default=25)
    args = parser.parse_args(argv)
    args.status_file = args.status_file or Path.home() / f'kit-status/kit{args.kit}-STATUS.md'
    args.target = args.target or KITS[args.kit]['target']
    args.video = args.video or KITS[args.kit]['video']
    if args.out.exists() and any(args.out.iterdir()):
        parser.error(f'{args.out} is not empty')

    def terminated(*_):
        raise AcceptanceError('SIGTERM')
    signal.signal(signal.SIGTERM, terminated)
    try:
        return Run(args).main()
    except (AcceptanceError, kitlib.KitError, drives.DriveError, OSError) as error:
        print(f'pc386_wfw_kit_acceptance: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
