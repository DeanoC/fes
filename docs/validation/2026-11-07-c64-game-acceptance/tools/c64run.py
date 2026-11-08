#!/usr/bin/env python3
"""C64 game acceptance kit driver (fes#1531, Claude for Deano). Not part of the repo.

usage: c64run.py KIT OUT PLAN.json

Precondition: run-c64.sh has claimed KIT in ~/kit-status and holds the kit flock.
Starts a private FogCast host (fogcast-api built from main, its own HOME, a
0600 config naming only KIT's target, remote input on, capture off) and drives
the normal library path through its API: import the sealed fes.c64 package,
link the locally composed 36 KiB firmware as the entry's ROM, create one
library entry per game, select a D64 as the entry's disk, launch, play, stop.

Commands are JSON objects appended to OUT/commands.jsonl while it runs; each
finished one appends a line to OUT/acks.jsonl:

  launch game [disk]      select disk (a plan disk key) for game and launch
  stop                    /api/v1/session/stop
  live disk               insert a plan disk into the running session
  live_clear              eject the running session's disk
  key name [mods] [hold]  one key tap (HID names, see KEYS)
  down name / up name
  type text               printable text; \\n is Return, " is Shift+2, * is ]
  joy port dirs [hold] [gap] [n]  port 1 or 2; dirs any of U D L R F
  joydown port dirs / joyup port
  wander port seconds [fire] [seed] [grab_every]  pseudo-random joystick play
  grab name
  video_start name s / video_stop
  cadence_start name seconds  raw gray capture for cadence; video_stop waits for it
  audio_start name / audio_stop
  session                 GET /api/v1/session (recorded)
  sleep s
  quit

The session is always stopped, the host terminated and its private config
removed on quit, error, SIGTERM, 30 idle minutes or the run limit.
"""
import hashlib
import json
import os
import random
import signal
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


class Terminated(BaseException):
    pass


def terminated(*_):
    raise Terminated('SIGTERM')


signal.signal(signal.SIGTERM, terminated)
KIT, OUT, PLAN = sys.argv[1], Path(sys.argv[2]), json.loads(Path(sys.argv[3]).read_text())
W = Path('/home/deano/fes-scratch/c64acc')
HOSTBIN = Path(os.environ.get('HOSTBIN', str(W / 'bin/fogcast-api')))
TARGET = {'A': 'dev', 'B': 'kit2'}[KIT]
TID = {'A': '73dc9f5f-1a12-4a95-a820-a9b4e600769a', 'B': '67c5f4e2-d288-49bb-9049-39ecf39cf6f6'}[KIT]
VIDEO = {'A': '/dev/v4l/by-id/fes-shadowcast-hil', 'B': '/dev/v4l/by-id/fes-4kpro-hil'}[KIT]
AUDIO = {'A': 'hw:CARD=S3', 'B': 'hw:CARD=D4KPRO'}[KIT]
IDLE_LIMIT = 1800
RUN_LIMIT = float(os.environ.get('C64_RUN_LIMIT', '7200'))
OUT.mkdir(parents=True, exist_ok=True)
(OUT / 'fr').mkdir(exist_ok=True)
(OUT / 'api').mkdir(exist_ok=True)
LOG = (OUT / 'driver.log').open('a')
COMMANDS, ACKS = OUT / 'commands.jsonl', OUT / 'acks.jsonl'
COMMANDS.touch()
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

KEYS = {**{chr(ord('a') + i): 0x04 + i for i in range(26)}, **{str(i): 0x1e + i - 1 for i in range(1, 10)},
        '0': 0x27, 'enter': 0x28, 'esc': 0x29, 'backspace': 0x2a, 'tab': 0x2b, 'space': 0x2c, '-': 0x2d,
        '=': 0x2e, '[': 0x2f, ']': 0x30, '\\': 0x31, ';': 0x33, "'": 0x34, '`': 0x35, ',': 0x36, '.': 0x37,
        '/': 0x38, **{f'f{i}': 0x3a + i - 1 for i in range(1, 13)}, 'right': 0x4f, 'left': 0x50, 'down': 0x51,
        'up': 0x52, 'home': 0x4a, 'end': 0x4d, 'pageup': 0x4b, 'pagedown': 0x4e, 'insert': 0x49, 'delete': 0x4c,
        'lctrl': 0xe0, 'lshift': 0xe1, 'lalt': 0xe2, 'rctrl': 0xe4, 'rshift': 0xe5, 'ralt': 0xe6}
# The core maps HID positionally onto the C64 matrix (README "Keyboard"): Shift+2 is ", ] is *, ' is :.
TYPE = {'(': ('8', 'lshift'), ')': ('9', 'lshift'), ';': (';',), ' ': ('space',), '\n': ('enter',), '"': ('2', 'lshift'), '*': (']',), ':': ("'",), ',': (',',),
        '.': ('.',), '$': ('4', 'lshift'), '/': ('/',), '-': ('-',), '=': ('=',)}
JOY = {'U': 100, 'D': 101, 'L': 102, 'R': 103, 'F': 104}


def log(*parts):
    line = time.strftime('%F %T ') + ' '.join(str(p) for p in parts)
    print(line, flush=True)
    LOG.write(line + '\n')
    LOG.flush()


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


class Host:
    def __init__(self):
        self.home = OUT / 'host-home'
        cfgdir = self.home / '.config/fogcast'
        cfgdir.mkdir(parents=True, exist_ok=True)
        os.chmod(self.home, 0o700)
        self.config = cfgdir / 'config.toml'
        self.config.touch(mode=0o600)
        self.config.write_text(private_config())
        with socket.socket() as s:
            s.bind(('127.0.0.1', 0))
            self.port = s.getsockname()[1]
        self.origin = f'http://127.0.0.1:{self.port}'
        env = {k: v for k, v in os.environ.items() if not k.startswith(('XDG_', 'DISPLAY', 'WAYLAND'))}
        env['HOME'] = str(self.home)
        self.proc = subprocess.Popen([str(HOSTBIN), '-headless', '-listen', f'127.0.0.1:{self.port}',
                                      '-config', str(self.config)], env=env, stdin=subprocess.DEVNULL,
                                     stdout=(OUT / 'host.log').open('ab'), stderr=subprocess.STDOUT,
                                     start_new_session=True)
        log('host pid', self.proc.pid, 'port', self.port, 'bin', sha(HOSTBIN)[:16])
        for _ in range(60):
            try:
                h = self.req('GET', '/api/v1/health', 'health', ok=True, quiet=True)
                if h[0] == 200:
                    return
            except OSError:
                pass
            time.sleep(1)
        raise SystemExit('private host did not become healthy')

    def req(self, method, path, name, data=None, timeout=60, headers=None, ok=False, quiet=False):
        h = dict(headers or {})
        if isinstance(data, (dict, list)):
            data = json.dumps(data).encode()
            h['Content-Type'] = 'application/json'
        elif data is not None:
            h.setdefault('Content-Type', 'application/octet-stream')
        request = urllib.request.Request(self.origin + path, data=data, headers=h, method=method)
        try:
            with opener.open(request, timeout=timeout) as r:
                code, raw = r.status, r.read(32 << 20)
        except urllib.error.HTTPError as e:
            code, raw = e.code, e.read(32 << 20)
        (OUT / 'api' / f'{time.strftime("%H%M%S")}-{name}.json').write_bytes(raw)
        if not quiet:
            log(method, path, code)
        try:
            value = json.loads(raw) if raw else {}
        except ValueError:
            value = {'raw': raw[:400].decode('latin1')}
        if code >= 400 and not ok:
            raise RuntimeError(f'{name} {code}: {str(value)[:400]}')
        return (code, value) if ok else value

    def close(self):
        if self.proc.poll() is None:
            os.killpg(self.proc.pid, signal.SIGTERM)
            try:
                self.proc.wait(20)
            except subprocess.TimeoutExpired:
                os.killpg(self.proc.pid, signal.SIGKILL)
                self.proc.wait()
        log('host exit', self.proc.returncode)
        self.config.unlink(missing_ok=True)


def private_config():
    """Only KIT's target from the shared config, with remote input on and capture, libraries, metadata off."""
    src = Path('/home/deano/.config/fogcast/config.toml').read_text()
    head, _, rest = src.partition('[[targets]]')
    top = {}
    for line in head.splitlines():
        if '=' in line and not line.startswith('['):
            k, v = line.split('=', 1)
            top[k.strip()] = v.strip()
    target = None
    for block in ('[[targets]]' + rest).split('[[targets]]')[1:]:
        body = block.split('\n[')[0]
        if f"target_id = '{TID}'" in body:
            target = body.strip()
    assert target, 'target not found in shared config'
    return '\n'.join([f"base_url = {top.get('base_url', repr(''))}", f"token = {top['token']}",
                      f"selected_target = '{TARGET}'", 'request_timeout_seconds = 5',
                      'upload_timeout_seconds = 120', '', '[[targets]]', target, '',
                      '[remote_input]', 'enabled = true', '', '[media]', 'enabled = false', '',
                      '[metadata]', 'enabled = false', ''])


def capture(args, dest, timeout=20):
    for attempt in range(5):
        cmd = ['timeout', '-k', '5', str(timeout), 'ffmpeg', '-loglevel', 'error', '-y', '-f', 'v4l2', '-i', VIDEO,
               *args, str(dest)]
        if subprocess.run(cmd, capture_output=True).returncode == 0 and dest.exists() and dest.stat().st_size > 2000:
            return True
        time.sleep(1.5)
    return False


class Run:
    def __init__(self, host):
        self.host = host
        self.started = time.monotonic()
        self.result = {'kit': KIT, 'plan': PLAN, 'events': []}
        self.media = {}
        self.games = {}
        self.selected = {}
        self.active = None
        self.video = self.audio = None
        self.held = {1: set(), 2: set()}

    def event(self, **fields):
        fields['t'] = round(time.monotonic() - self.started, 2)
        self.result['events'].append(fields)
        log('event', json.dumps(fields)[:600])
        (OUT / 'result.json').write_text(json.dumps(self.result, indent=1))

    def upload(self, path):
        if path not in self.media:
            data = Path(path).read_bytes()
            mid = self.host.req('POST', '/api/v1/core-media', 'media-' + hashlib.sha256(data).hexdigest()[:8],
                                data, timeout=120)['media_id']
            self.media[path] = mid
            self.event(op='upload', file=Path(path).name, sha256=hashlib.sha256(data).hexdigest(), media_id=mid)
        return self.media[path]

    def setup(self):
        pkg_path = Path(PLAN['package'])
        data = pkg_path.read_bytes()
        pkg = self.host.req('POST', '/api/v1/core-packages', 'import', data, timeout=180)['package_id']
        assert pkg == PLAN['package_id'], (pkg, PLAN['package_id'])
        compat = self.host.req('POST', f'/api/v1/core-packages/{pkg}/compatibility', 'compat', b'', timeout=120)
        self.event(op='import', package_id=pkg, archive_sha256=hashlib.sha256(data).hexdigest(),
                   compatible=compat.get('compatible'), compat=str(compat)[:300])
        self.pkg = pkg
        detail = self.host.req('GET', f'/api/v1/core-packages/{pkg}', 'package')
        rom_id = PLAN.get('rom_id') or find_key(detail, 'rom_id')
        fw = self.upload(PLAN['firmware'])
        for key, title in PLAN['games'].items():
            game = self.host.req('POST', '/api/v1/library/core-entries', 'entry-' + key,
                                 {'title': title, 'package_id': pkg})['game_id']
            self.host.req('PUT', f'/api/v1/library/core-entries/{game}/rom', 'rom-' + key,
                          {'package_id': pkg, 'rom_id': rom_id, 'expected_media_id': '', 'media_id': fw})
            self.games[key] = game
            self.selected[key] = ''
            self.event(op='entry', game=key, game_id=game, rom_id=rom_id)

    def select(self, game, disk):
        mid = self.upload(PLAN['disks'][disk])
        if self.selected[game] == mid:
            return
        self.host.req('PUT', f'/api/v1/library/core-entries/{self.games[game]}/media', 'select-' + game,
                      {'expected_package_id': self.pkg, 'expected_media_id': self.selected[game],
                       'media_role': 'disk', 'media_id': mid})
        self.selected[game] = mid

    def launch(self, game, disk=None):
        if disk:
            self.select(game, disk)
        began = time.monotonic()
        launch = self.host.req('POST', '/api/v1/session/launch', 'launch-' + game, {'game_id': self.games[game]},
                               timeout=300)
        cp = launch.get('core_package') or {}
        self.active = game
        return {'state': launch.get('state'), 'package_id': cp.get('package_id'), 'generation': cp.get('generation'),
                'media_units': cp.get('media_units'), 'build_id': cp.get('build_id'),
                'seconds': round(time.monotonic() - began, 2)}

    def session_headers(self):
        sess = self.host.req('GET', '/api/v1/session', 'session')
        cp = sess.get('core_package') or {}
        return sess, {'X-FogCast-Session-ID': sess.get('id', ''), 'X-FogCast-Package-ID': cp.get('package_id', ''),
                      'X-FogCast-Core-Generation': str(cp.get('generation', 0)),
                      'X-FogCast-Target': sess.get('target', ''), 'X-FogCast-Target-ID': sess.get('target_id', '')}

    def live(self, disk):
        sess, hdr = self.session_headers()
        mid = self.upload(PLAN['disks'][disk])
        code, after = self.host.req('POST', '/api/v1/session/live-media', 'live', {'media_id': mid,
                                    'name': Path(PLAN['disks'][disk]).name}, headers=hdr, timeout=180, ok=True)
        cp = after.get('core_package') or {}
        return {'code': code, 'same_session': after.get('id') == sess.get('id'),
                'generation_before': (sess.get('core_package') or {}).get('generation'),
                'generation_after': cp.get('generation'), 'media_units': cp.get('media_units'),
                'error': None if code < 400 else str(after)[:300]}

    def live_clear(self):
        sess, hdr = self.session_headers()
        code, after = self.host.req('POST', '/api/v1/session/live-media/clear', 'live-clear', b'', headers=hdr,
                                    timeout=180, ok=True)
        cp = after.get('core_package') or {}
        return {'code': code, 'same_session': after.get('id') == sess.get('id'),
                'generation_after': cp.get('generation'), 'media_units': cp.get('media_units'),
                'error': None if code < 400 else str(after)[:300]}

    def stop(self):
        self.release_all()
        code, st = self.host.req('POST', '/api/v1/session/stop', 'stop', b'', timeout=180, ok=True)
        self.active = None
        return {'code': code, 'state': st.get('state'), 'error': None if code < 400 else str(st)[:300]}

    def ev(self, player, device, kind, action, code):
        self.host.req('POST', '/api/v1/session/input/event', 'in',
                      {'event': {'Player': player, 'Device': device, 'Kind': kind, 'Action': action,
                                 'Code': code, 'Value': 0}}, quiet=True)

    def key(self, name, down):
        self.ev(0, 0, 0, 1 if down else 0, 0x1000 + KEYS[name])

    def tap(self, name, mods=(), hold=0.06, gap=0.08):
        for m in mods:
            self.key(m, True)
        self.key(name, True)
        time.sleep(hold)
        self.key(name, False)
        for m in reversed(mods):
            self.key(m, False)
        time.sleep(gap)

    def type(self, text, hold=0.06, gap=0.08):
        for c in text:
            if c in TYPE:
                k = TYPE[c]
                self.tap(k[0], k[1:], hold, gap)
            elif c.isupper():
                self.tap(c.lower(), ('lshift',), hold, gap)
            else:
                self.tap(c.lower(), (), hold, gap)

    # C64 port 2 is controller port 0 (player 0); C64 port 1 is player 1 (core README).
    def joy_set(self, port, dirs):
        player = {2: 0, 1: 1}[port]
        want = {JOY[d] for d in dirs}
        for code in sorted(self.held[port] - want):
            self.ev(player, 1, 1, 0, code)
        for code in sorted(want - self.held[port]):
            self.ev(player, 1, 1, 1, code)
        self.held[port] = want

    def joy(self, port, dirs, hold=0.15, gap=0.1, n=1):
        for _ in range(n):
            self.joy_set(port, dirs)
            time.sleep(hold)
            self.joy_set(port, '')
            time.sleep(gap)

    def wander(self, port, seconds, fire=True, seed=1, grab_every=30, prefix='w'):
        rng = random.Random(seed)
        end = time.monotonic() + seconds
        next_grab, n, moves = time.monotonic() + grab_every, 0, 0
        while time.monotonic() < end:
            d = rng.choice(['U', 'D', 'L', 'R', 'UL', 'UR', 'DL', 'DR', '', 'R', 'L'])
            if fire and rng.random() < 0.45:
                d += 'F'
            self.joy_set(port, d)
            moves += 1
            time.sleep(rng.uniform(0.15, 0.7))
            if time.monotonic() >= next_grab:
                self.joy_set(port, '')
                self.grab(f'{prefix}-{n:03d}')
                n += 1
                next_grab = time.monotonic() + grab_every
        self.joy_set(port, '')
        return {'moves': moves, 'grabs': n}

    def release_all(self):
        for port in (1, 2):
            if self.held[port]:
                try:
                    self.joy_set(port, '')
                except Exception as e:  # noqa: BLE001
                    log('release failed', e)

    def grab(self, name):
        dest = OUT / 'fr' / f'{name}.png'
        if self.video:
            return {'grab': name, 'ok': False, 'why': 'video recording holds the device'}
        ok = capture(['-vf', 'select=gte(n\\,10)', '-frames:v', '1'], dest)
        return {'grab': name, 'ok': ok, 'sha256': sha(dest) if ok else None}

    def video_start(self, name, seconds):
        dest = OUT / 'fr' / f'{name}.mkv'
        self.video = (subprocess.Popen(['timeout', '-k', '5', str(seconds + 30), 'ffmpeg', '-loglevel', 'error', '-y',
                                        '-f', 'v4l2', '-i', VIDEO, '-t', str(seconds), '-vf', 'scale=480:270',
                                        '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '23', str(dest)]), dest)
        return {'video': name}

    def cadence_start(self, name, seconds):
        """Lossless 480x270 gray raw capture at the capture's own frame rate, for cadence (as fes#1530's cad1)."""
        dest = OUT / 'fr' / f'{name}.gray'
        self.video = (subprocess.Popen(['timeout', '-k', '5', str(seconds + 30), 'ffmpeg', '-loglevel', 'error', '-y',
                                        '-f', 'v4l2', '-i', VIDEO, '-t', str(seconds), '-vf',
                                        'scale=480:270,format=gray', '-fps_mode', 'passthrough', '-f', 'rawvideo',
                                        str(dest)], stderr=(OUT / f'{name}-ffmpeg.txt').open('w')), dest)
        return {'cadence': name, 'seconds': seconds, 'wall': time.time()}

    def video_stop(self):
        if not self.video:
            return {}
        proc, dest = self.video
        try:
            proc.wait(timeout=600)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        self.video = None
        return {'video_rc': proc.returncode, 'sha256': sha(dest) if dest.exists() else None}

    def audio_start(self, name):
        dest = OUT / 'fr' / f'{name}.wav'
        self.audio = (subprocess.Popen(['timeout', '-k', '5', '1800', 'ffmpeg', '-loglevel', 'error', '-y', '-f',
                                        'alsa', '-ac', '2', '-ar', '48000', '-i', AUDIO, str(dest)]), dest)
        return {'audio': name}

    def audio_stop(self):
        if not self.audio:
            return {}
        proc, dest = self.audio
        proc.send_signal(signal.SIGINT)
        try:
            proc.wait(timeout=20)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        self.audio = None
        return {'audio_rc': proc.returncode, 'sha256': sha(dest) if dest.exists() else None,
                'bytes': dest.stat().st_size if dest.exists() else 0}

    def run(self, op):
        c = op['op']
        if c == 'launch':
            return self.launch(op['game'], op.get('disk'))
        if c == 'stop':
            return self.stop()
        if c == 'select':
            self.select(op['game'], op['disk'])
            return {'selected': self.selected[op['game']]}
        if c == 'live':
            return self.live(op['disk'])
        if c == 'live_clear':
            return self.live_clear()
        if c == 'key':
            self.tap(op['name'], tuple(op.get('mods', ())), op.get('hold', 0.08))
            return {}
        if c == 'down':
            self.key(op['name'], True)
            return {}
        if c == 'up':
            self.key(op['name'], False)
            return {}
        if c == 'type':
            self.type(op['text'], op.get('hold', 0.06), op.get('gap', 0.08))
            return {}
        if c == 'joy':
            self.joy(op['port'], op['dirs'], op.get('hold', 0.15), op.get('gap', 0.1), op.get('n', 1))
            return {}
        if c == 'joydown':
            self.joy_set(op['port'], op['dirs'])
            return {}
        if c == 'joyup':
            self.joy_set(op['port'], '')
            return {}
        if c == 'wander':
            return self.wander(op['port'], op['seconds'], op.get('fire', True), op.get('seed', 1),
                               op.get('grab_every', 30), op.get('prefix', 'w'))
        if c == 'grab':
            return self.grab(op['name'])
        if c == 'video_start':
            return self.video_start(op['name'], op['seconds'])
        if c == 'cadence_start':
            return self.cadence_start(op['name'], op['seconds'])
        if c == 'video_stop':
            return self.video_stop()
        if c == 'audio_start':
            return self.audio_start(op['name'])
        if c == 'audio_stop':
            return self.audio_stop()
        if c == 'session':
            sess = self.host.req('GET', '/api/v1/session', 'session')
            cp = sess.get('core_package') or {}
            return {'state': sess.get('state'), 'generation': cp.get('generation'),
                    'media_units': cp.get('media_units'), 'package_id': cp.get('package_id')}
        if c == 'sleep':
            time.sleep(op['s'])
            return {}
        raise ValueError(f'unknown op {c}')

    def main(self):
        self.setup()
        pos, last = 0, time.monotonic()
        with COMMANDS.open() as f:
            while True:
                if time.monotonic() - self.started > RUN_LIMIT:
                    self.result['halted'] = 'run limit'
                    return
                line = f.readline()
                if not line:
                    if time.monotonic() - last > IDLE_LIMIT:
                        self.result['halted'] = 'idle limit'
                        return
                    time.sleep(0.3)
                    continue
                last = time.monotonic()
                line = line.strip()
                if not line:
                    continue
                op = json.loads(line)
                if op['op'] == 'quit':
                    return
                try:
                    reply = self.run(op)
                    self.event(op=op['op'], args={k: v for k, v in op.items() if k != 'op'}, reply=reply)
                    status = 'ok'
                except Exception as e:  # noqa: BLE001
                    self.event(op=op['op'], error=f'{type(e).__name__}: {e}'[:500])
                    status = 'error'
                with ACKS.open('a') as a:
                    a.write(json.dumps({'op': op['op'], 'status': status, 't': time.strftime('%T')}) + '\n')
                pos += 1

    def cleanup(self):
        for fn in (self.video_stop, self.audio_stop):
            try:
                fn()
            except Exception as e:  # noqa: BLE001
                log('cleanup recorder', e)
        if self.active:
            try:
                self.event(op='final-stop', reply=self.stop())
            except Exception as e:  # noqa: BLE001
                self.result['close_error'] = str(e)[:300]
        (OUT / 'result.json').write_text(json.dumps(self.result, indent=1))


def find_key(value, key):
    if isinstance(value, dict):
        if key in value and value[key]:
            return value[key]
        for v in value.values():
            found = find_key(v, key)
            if found:
                return found
    if isinstance(value, list):
        for v in value:
            found = find_key(v, key)
            if found:
                return found
    return None


def main():
    host = None
    run = None
    try:
        host = Host()
        run = Run(host)
        run.main()
    except (Exception, Terminated) as e:  # noqa: BLE001
        log('ERROR', type(e).__name__, str(e)[:600])
        if run:
            run.result['halted'] = f'{type(e).__name__}: {e}'[:400]
    finally:
        if run:
            run.cleanup()
        if host:
            host.close()
        log('done')


if __name__ == '__main__':
    main()
