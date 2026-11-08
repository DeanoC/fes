"""hlib.py: drive an hx.sh Hatari (Xvfb) the way the kit runners drive the kit (fes#1528 references).
- live(N): Hatari screenshot (640x400, 2x low res) laid out like the kit's live.png: nearest-neighbour to 480x300 at
  (240,105) on a white 960x540 canvas, so the kit's 960x540 readers (gomoku.py board(), st_xy) apply unchanged.
- goto(N, fx, fy, tx, ty, click): relative host mouse moves of 2 px per ST count (Hatari window zoom 2), then a click.
- key(N, keysym, hold): host key down/up (joystick 1 = cursor keys, fire = Right Ctrl with --joy1 keys)."""
import subprocess, time
H = '/home/deano/fes-scratch/st1528/hat'
def sh(n, *cmd): return subprocess.run(['docker', 'exec', f'st1528-x-{n}', *cmd], capture_output=True, text=True).stdout
def fifo(n, line): open(f'{H}/{n}/fifo', 'w').write(line + '\n')
def shot(n, name):
    p = f'{H}/{n}/{name}.png'; fifo(n, f'hatari-debug screenshot /w/hat/{n}/{name}.png')
    for _ in range(50):
        time.sleep(0.1)
        try:
            if open(p, 'rb').read()[-8:-4] == b'IEND': return p
        except OSError: pass
    return p
def live(n):
    p = shot(n, 'live-src'); out = f'{H}/{n}/live.png'
    subprocess.run(['ffmpeg', '-loglevel', 'error', '-y', '-i', p, '-vf', 'scale=480:300:flags=neighbor,pad=960:540:240:105:white', out])
    return out
def focus(n): sh(n, 'sh', '-c', 'W=$(xdotool search --name Hatari | head -1); xdotool windowfocus $W; xdotool mousemove --window $W 320 200')
def goto(n, fx, fy, tx, ty, click=False):
    dx, dy = tx - fx, ty - fy
    while dx or dy:
        sx = max(-6, min(6, dx)); sy = max(-6, min(6, dy)); sh(n, 'xdotool', 'mousemove_relative', '--', str(2*sx), str(2*sy)); dx -= sx; dy -= sy
    time.sleep(0.3)
    if click: sh(n, 'sh', '-c', 'xdotool mousedown 1; sleep 0.12; xdotool mouseup 1')
def key(n, k, hold=0.1): sh(n, 'sh', '-c', f'xdotool keydown {k}; sleep {hold}; xdotool keyup {k}')
