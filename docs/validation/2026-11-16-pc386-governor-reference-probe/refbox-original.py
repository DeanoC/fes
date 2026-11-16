#!/usr/bin/env python3
"""refbox.py DISPLAY VMDIR OUT COMMANDS.jsonl — drive the 86Box reference VM
(fes#1368) with the same command vocabulary as the kit runner hil_x87.py, so the
same sequence runs on both sides.

Commands (one JSON object per line):
  sleep s | burst key n [delay_ms] | type text [gap] | tap key [hold] [gap] | down key | up key
  grab name                 one PNG of the guest area
  video_start name s        s seconds of the guest area, raw gray at FPS,
                            plus a 1 Hz strip of 86Box's speed indicator
  video_stop | audio_start name | audio_stop | quit

Not part of the repo. Kills only the 86Box it started.
"""
import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

DISP, VMDIR, OUT, CMDS = sys.argv[1], Path(sys.argv[2]), Path(sys.argv[3]), Path(sys.argv[4])
U = Path('/home/deano/fes-scratch/u6ref')
FPS = int(os.environ.get('REF_FPS', '30'))
OUT.mkdir(parents=True, exist_ok=True)
(OUT / 'fr').mkdir(exist_ok=True)
LOG = (OUT / 'driver.log').open('a')
ENV = {**os.environ, 'DISPLAY': DISP}
KEYMAP = {'enter': 'Return', 'esc': 'Escape', 'space': 'space', 'tab': 'Tab', 'up': 'Up', 'down': 'Down',
          'left': 'Left', 'right': 'Right', 'backspace': 'BackSpace', 'delete': 'Delete', 'home': 'Home',
          'end': 'End', 'pageup': 'Prior', 'pagedown': 'Next', 'insert': 'Insert', 'lshift': 'Shift_L',
          'lctrl': 'Control_L', 'lalt': 'Alt_L'}
for i in range(1, 13):
    KEYMAP[f'f{i}'] = f'F{i}'


def log(*p):
    line = time.strftime('%F %T ') + ' '.join(str(x) for x in p)
    print(line, flush=True)
    LOG.write(line + '\n')
    LOG.flush()


def xdo(*args, **kw):
    return subprocess.run(['xdotool', *args], env=ENV, capture_output=True, text=True, **kw)


def window(pid=None):
    # Match this run's own 86Box. Several can share a display, and picking by
    # name alone sent one run's keys into another's window (fes#1368).
    args = ['search', '--name', '86Box 6.0']
    if pid is not None:
        args = ['search', '--pid', str(pid), '--name', '86Box 6.0']
    r = xdo(*args)
    ids = [w for w in r.stdout.split() if w]
    return ids[-1] if ids else None


def geometry(win):
    r = xdo('getwindowgeometry', win)
    pos = size = None
    for line in r.stdout.splitlines():
        line = line.strip()
        if line.startswith('Position:'):
            pos = line.split()[1].split('(')[0]
        if line.startswith('Geometry:'):
            size = line.split()[1]
    x, y = (int(v) for v in pos.split(','))
    w, h = (int(v) for v in size.split('x'))
    return x, y, w, h


TOP, BOTTOM = 55, 41  # menu+toolbar above the guest area, status bar below


def guest_rect():
    """The whole 86Box window: menu bar, tool bar (which carries the emulation
    speed), the guest picture and the status bar. The analysis crops what it
    needs, so nothing depends on the widget heights."""
    return geometry(WIN)


def speed_rect():
    x, y, w, h = geometry(WIN)
    return x + w - 60, y + 28, 56, 18


def grab(name, rect=None):
    x, y, w, h = rect or guest_rect()
    dest = OUT / 'fr' / f'{name}.png'
    subprocess.run(['ffmpeg', '-loglevel', 'error', '-y', '-f', 'x11grab', '-video_size', f'{w}x{h}',
                    '-i', f'{DISP}+{x},{y}', '-frames:v', '1', str(dest)], env=ENV, capture_output=True)
    return dest


VIDEO = None
SPEED = None
AUDIO = None


def video_start(name, seconds):
    global VIDEO, SPEED
    x, y, w, h = guest_rect()
    (OUT / f'{name}.geom').write_text(json.dumps({'w': w, 'h': h, 'fps': FPS}))
    VIDEO = subprocess.Popen(['ffmpeg', '-loglevel', 'error', '-y', '-f', 'x11grab', '-framerate', str(FPS),
                              '-video_size', f'{w}x{h}', '-i', f'{DISP}+{x},{y}', '-t', str(seconds),
                              '-pix_fmt', 'gray', '-f', 'rawvideo', str(OUT / f'{name}.gray')], env=ENV,
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    SPEED = None


def video_stop():
    global VIDEO, SPEED
    for p in (VIDEO, SPEED):
        if p:
            try:
                p.wait(timeout=120)
            except subprocess.TimeoutExpired:
                p.kill()
    VIDEO = SPEED = None


def audio_start(name):
    """Mark the sink's current length; audio_stop keeps what was written since."""
    global AUDIO
    try:
        offset = os.path.getsize(ALSA_SINK)
    except OSError:
        offset = 0
    AUDIO = (OUT / f'{name}.s16', offset)


def audio_stop():
    global AUDIO
    if AUDIO is not None:
        dest, offset = AUDIO
        try:
            with open(ALSA_SINK, 'rb') as fh:
                fh.seek(offset)
                data = fh.read()
            dest.write_bytes(data)
            log('audio', dest.name, len(data), 'bytes from', offset)
        except OSError as exc:
            log('audio MISSING', dest.name, exc)
        AUDIO = None


ALSA_SINK = str(OUT / 'audio-sink.wav')  # OpenAL's wave writer, one per run
                                         # (44-byte header, 48 kHz S16 stereo)
AUDIO_NAME = []


BUTTONS = {0: None, 1: '1', 2: '3', 4: '2'}   # kit mask -> X button: left, right, middle
HELD = {'mask': 0}


def capture_mouse():
    """86Box only passes the pointer to the guest once the window has captured it."""
    x, y, w, h = guest_rect()
    xdo('mousemove', str(x + w // 2), str(y + h // 2))
    time.sleep(0.2)
    xdo('click', '1')
    time.sleep(0.5)
    log('mouse captured')


def mouse(dx, dy, buttons, n=1, gap=0.05):
    for _ in range(n):
        if dx or dy:
            xdo('mousemove_relative', '--', str(dx), str(dy))
        if buttons != HELD['mask']:
            for bit, btn in ((1, '1'), (2, '3'), (4, '2')):
                was, now = HELD['mask'] & bit, buttons & bit
                if now and not was:
                    xdo('mousedown', btn)
                elif was and not now:
                    xdo('mouseup', btn)
            HELD['mask'] = buttons
        time.sleep(gap)


def key(name, hold=0.05):
    k = KEYMAP.get(name, name)
    xdo('keydown', k)
    time.sleep(hold)
    xdo('keyup', k)


def run(cmd):
    op = cmd['op']
    if op == 'sleep':
        time.sleep(float(cmd['s']))
    elif op == 'type':
        gap = float(cmd.get('gap', 0.12))
        for ch in cmd['text']:
            if ch == '\n':
                key('enter')
            elif ch == ' ':
                key('space')
            else:
                xdo('type', '--delay', '40', ch)
            time.sleep(gap)
    elif op == 'tap':
        key(cmd['key'], float(cmd.get('hold', 0.05)))
        time.sleep(float(cmd.get('gap', 0.1)))
    elif op == 'burst':
        # n presses of one key at a fixed delay, in a single xdotool call, so the
        # rate is set by X and not by process startup (fes#1368: saturating input).
        # 'keys' cycles a list, so the Avatar can be walked round a square and never
        # run out of floor; 'key' is the single-key form.
        cycle = [KEYMAP.get(k, k) for k in cmd.get('keys', [cmd.get('key')])]
        seq = [cycle[i % len(cycle)] for i in range(int(cmd['n']))]
        xdo('key', '--delay', str(int(cmd.get('delay_ms', 40))), *seq)
    elif op == 'down':
        xdo('keydown', KEYMAP.get(cmd['key'], cmd['key']))
    elif op == 'up':
        xdo('keyup', KEYMAP.get(cmd['key'], cmd['key']))
    elif op == 'mouse':
        mouse(int(cmd.get('dx', 0)), int(cmd.get('dy', 0)), int(cmd.get('buttons', 0)),
              int(cmd.get('n', 1)), float(cmd.get('gap', 0.05)))
    elif op == 'capture_mouse':
        capture_mouse()
    elif op == 'grab':
        grab(cmd['name'])
    elif op == 'video_start':
        video_start(cmd['name'], float(cmd['s']))
    elif op == 'video_stop':
        video_stop()
    elif op == 'audio_start':
        audio_start(cmd['name'])
    elif op == 'audio_stop':
        audio_stop()
    elif op in ('stop', 'quit', 'package', 'settings', 'load', 'wait_stable', 'kitdate'):
        log('skipped', op)
    else:
        raise SystemExit(f'unknown op {op}')


WIN = None
box = None
try:
    try:
        os.unlink(ALSA_SINK)
    except OSError:
        pass
    conf = OUT / 'alsoft.conf'
    conf.write_text((U / 'alsoft.conf').read_text().replace(str(U / 'audio-sink.wav'), ALSA_SINK))
    env = {**os.environ, 'DISPLAY': DISP, 'ALSOFT_CONF': str(conf),
           'ALSOFT_DRIVERS': 'wave', 'ALSOFT_LOGLEVEL': '2'}
    box = subprocess.Popen([str(U / 'box.sh'), '-P', str(VMDIR), '-R', str(U / 'roms'), '--noconfirm'],
                           env=env, stdout=(OUT / 'box.out').open('w'), stderr=subprocess.STDOUT,
                           stdin=subprocess.DEVNULL, start_new_session=True)
    log('86Box pid', box.pid)
    for _ in range(60):
        WIN = window(box.pid)
        if WIN:
            break
        time.sleep(1)
    if not WIN:
        raise SystemExit('no 86Box window')
    subprocess.run(['xset', 'r', 'off'], env=ENV, capture_output=True)
    xdo('windowfocus', WIN)
    log('window', WIN, geometry(WIN))
    for line in CMDS.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        cmd = json.loads(line)
        log('cmd', line[:120])
        xdo('windowfocus', WIN)
        run(cmd)
finally:
    video_stop()
    if box is not None:
        try:
            os.killpg(os.getpgid(box.pid), signal.SIGKILL)
        except Exception:
            try:
                box.kill()
            except Exception:
                pass
    log('done')
