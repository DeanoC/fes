#!/usr/bin/env python3
"""u7pace.py GEOM GRAY --crop=TOP,BOTTOM [--speed=RECT --speed-sha=HEX] — NPC pace.

fes#1853. `method.md` defines NPC pace as a non-player character's
displacement per second of wall-clock time in a fixed scene with no input.
The scene is the one a new game of Ultima VII: The Black Gate opens in, in
Trinsic, between the world first appearing and the opening conversation
starting; no input is sent in between. A single townsperson paces a fixed
beat on the right of the view, and that is the character measured.

The window is found in the capture, not assumed, so the two machines need
not reach it at the same wall-clock time:

  t0  the world appears        the first frame whose mean brightness crosses
                               70, which separates the dark load screen from
                               the lit Trinsic scene
  t1  the conversation starts  the first frame after t0 that redraws more
                               than a tenth of the picture

Between them, a frame is a motion frame when enough pixels differ from the
previous one; the centroid of those pixels is the character's position. Two
figures come out of the series:

  pace          the summed centroid displacement over the window's length,
                given as a fraction of the picture's height per second so
                that the two sides' capture resolutions cannot enter it
  cycle         the character's walk is a closed beat, so the autocorrelation
                of its centroid's vertical position has a sharp first peak;
                that lag is the time it takes to walk the beat once. It needs
                no pixel scale at all and is the stronger of the two.

The capture is of 86Box's whole window, so it carries the emulator's own
speed readout. `--speed` names that rectangle, in capture pixels, and
`--speed-sha` the SHA-256 of that rectangle rendering **100%**, which
`speed-100.png` in the record holds for inspection. Both are echoed into the
output, so a gate result can be rederived from the record alone without
knowing how it was invoked. The gate is that every
frame of the window matches that hash: constancy alone is not enough, because
a run held steady at 97% would pass it and inflate the measured period. Both
answers are reported separately — whether the readout was steady, and whether
it was 100 — and `speed_gate` is the two together.

Prints JSON so the record's figures can be rederived.
"""
import hashlib
import json
import sys

import numpy as np

THRESH = 24          # grey levels of difference that count as changed
MOVE_FRAC = 0.0015   # changed area that counts as a motion frame
BIG_FRAC = 0.10      # changed area that is a scene change, not a character
JUMP_FRAC = 0.125    # centroid jump that is a different character
LIT_SPAN = 0.5       # where between the dark and lit plateaus the mark sits


def main():
    geom_path, gray_path = sys.argv[1], sys.argv[2]
    top = bottom = 0
    decimate = 1
    speed_rect = speed_sha = None
    for arg in sys.argv[3:]:
        if arg.startswith('--crop='):
            top, bottom = (int(v) for v in arg.split('=', 1)[1].split(','))
        elif arg.startswith('--speed-sha='):
            speed_sha = arg.split('=', 1)[1]
        elif arg.startswith('--speed='):
            speed_rect = [int(v) for v in arg.split('=', 1)[1].split(',')]
        elif arg.startswith('--decimate='):
            decimate = int(arg.split('=', 1)[1])
    geom = json.load(open(geom_path))
    w, h, fps = geom['w'], geom['h'], geom['fps']
    raw = np.fromfile(gray_path, dtype=np.uint8)
    n = raw.size // (w * h)
    frames = raw[:n * w * h].reshape(n, h, w)
    # Deriving both sides at one rate: the captures ran at different rates and
    # the method asks for one, so the faster side is thinned to the slower by
    # keeping every Nth frame. It is derivation, not capture, and the record
    # says so where it uses it.
    if decimate > 1:
        frames = frames[::decimate]
        n = frames.shape[0]
        fps = fps / decimate

    pic = frames[:, top:h - bottom, :].astype(np.int16)
    ph, pw = pic.shape[1], pic.shape[2]
    area = ph * pw
    bright = pic.reshape(n, -1).mean(axis=1)
    # The two sides capture through different paths -- 86Box's window through
    # x11grab, the kit's HDMI through a capture card -- so the lit scene is not
    # the same grey level on both. The mark is taken from the capture's own
    # range instead of a fixed level: halfway between its dark and its bright
    # plateau.
    dark, light = np.percentile(bright, 10), np.percentile(bright, 90)
    level = dark + LIT_SPAN * (light - dark)
    lit = np.nonzero(bright > level)[0]
    if not len(lit):
        raise SystemExit('the scene never lights: no world frame in this capture')
    t0 = int(lit[0])
    diff = np.abs(np.diff(pic, axis=0)) > THRESH
    count = diff.sum(axis=(1, 2))
    big = np.nonzero(count[t0 + 5:] > BIG_FRAC * area)[0]
    t1 = t0 + 5 + int(big[0]) if len(big) else n - 1

    # The emulator's speed readout, over the measured window only. No click is
    # ever sent, so the message beside the percentage never changes and any
    # difference in the strip is the percentage itself moving off 100.
    speed_steady = speed_is_100 = speed_gate = None
    if speed_rect:
        x0, y0, x1, y1 = speed_rect
        strip = frames[t0:t1, y0:y1, x0:x1]
        speed_steady = bool((strip == strip[0]).all())
        if speed_sha:
            hashes = {hashlib.sha256(strip[i].tobytes()).hexdigest()
                      for i in range(strip.shape[0])}
            speed_is_100 = (hashes == {speed_sha})
        speed_gate = bool(speed_steady and speed_is_100)

    # One character is measured, not everything that moves. Its band is found
    # from the window itself: the column of most redrawing, widened by a tenth
    # of the picture on each side, which carries over to any capture scale.
    activity = diff[t0:t1].sum(axis=0)
    profile = activity.sum(axis=0).astype(float)
    kernel = max(3, pw // 40)
    smooth = np.convolve(profile, np.ones(kernel) / kernel, 'same')
    band = int(np.argmax(smooth))
    half = max(8, pw // 10)
    lo_x, hi_x = max(0, band - half), min(pw, band + half)

    events = []
    for i in range(t0, t1):
        sub = diff[i][:, lo_x:hi_x]
        c = int(sub.sum())
        if c < MOVE_FRAC * area:
            continue
        ys, xs = sub.nonzero()
        events.append((i / fps, float(xs.mean()) + lo_x, float(ys.mean())))
    if len(events) < 8:
        raise SystemExit('too few motion frames between the two marks to measure')

    path = 0.0
    jumps = 0
    for (_, xa, ya), (_, xb, yb) in zip(events, events[1:]):
        step = ((xb - xa) ** 2 + (yb - ya) ** 2) ** 0.5
        if step > JUMP_FRAC * pw:
            jumps += 1
            continue
        path += step
    span = events[-1][0] - events[0][0]

    times = np.array([e[0] for e in events])
    vertical = np.array([e[2] for e in events])
    grid = np.arange(times[0], times[-1], 1 / fps)
    signal = np.interp(grid, times, vertical)
    signal = signal - signal.mean()
    acorr = np.correlate(signal, signal, 'full')[len(signal) - 1:]
    acorr = acorr / acorr[0]
    low = int(2 * fps)
    high = int(len(acorr) * 0.6)
    cycle = peak = None
    if high > low:
        lag = low + int(np.argmax(acorr[low:high]))
        cycle, peak = lag / fps, float(acorr[lag])

    out = {'source': gray_path, 'frames': n, 'fps': fps,
           'lit_level': round(float(level), 1),
           'picture': [pw, ph], 'crop': [top, bottom], 'decimate': decimate,
           'world_s': round(t0 / fps, 2), 'conversation_s': round(t1 / fps, 2),
           'window_s': round((t1 - t0) / fps, 2),
           'character_band_x': [lo_x, hi_x],
           'motion_frames': len(events), 'jumps_dropped': jumps,
           'path_px': round(path, 1), 'measured_span_s': round(span, 2),
           'pace_px_per_s': round(path / span, 3),
           'pace_heights_per_s': round(path / ph / span, 5),
           'cycle_s': None if cycle is None else round(cycle, 3),
           'cycle_correlation': None if peak is None else round(peak, 3),
           'speed_rect': speed_rect, 'speed_sha': speed_sha,
           'speed_readout_steady': speed_steady,
           'speed_readout_is_100': speed_is_100,
           'speed_gate': speed_gate}
    json.dump(out, sys.stdout, indent=1)
    print()


if __name__ == '__main__':
    main()
