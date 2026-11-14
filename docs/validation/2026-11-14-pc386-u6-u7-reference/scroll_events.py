#!/usr/bin/env python3
"""scroll_events.py RAW W H FPS X0 X1 Y0 Y1 [THRESH] — Ultima VI's step rate from
a raw 8-bit gray capture of the map view (fes#1368).

A step redraws the map view. This finds every frame whose mean absolute
difference from the one before exceeds THRESH levels, groups consecutive such
frames into one event, and reports the rate from the median gap between event
starts, the way method.md fixes it, plus the rate over the span.

walk_rate.py does the same with a per-pixel threshold (a pixel counts if it
moves more than 40 levels, a frame if more than 20% of them do), which is too
strict for a view that is mostly dark stone: on the same window it finds 6
events where this finds 8. The mean-absolute-difference test is what this one
changes, and the gap between a quiet frame (about 0.1) and a redraw (3 to 37)
is two orders of magnitude, so the threshold is not a tuning knob.
"""
import json
import statistics
import sys

raw, W, H, FPS, X0, X1, Y0, Y1 = (sys.argv[1], *[int(v) for v in sys.argv[2:9]])
THRESH = float(sys.argv[9]) if len(sys.argv) > 9 else 2.0
data = open(raw, 'rb').read()
frames = len(data) // (W * H)


def view(i):
    b = i * W * H
    return [data[b + y * W + X0:b + y * W + X1] for y in range(Y0, Y1)]


def mad(a, b):
    total = n = 0
    for y in range(len(a)):
        ra, rb = a[y], b[y]
        for x in range(0, len(ra), 3):          # every third column: enough, and far faster
            total += abs(ra[x] - rb[x])
            n += 1
    return total / n


prev = view(0)
diffs = []
for i in range(1, frames):
    cur = view(i)
    diffs.append(round(mad(prev, cur), 2))
    prev = cur

quiet = [d for d in diffs if d <= THRESH]
events, run = [], None
for i, d in enumerate(diffs, start=1):
    if d > THRESH:
        if run is None:
            run = i
    elif run is not None:
        events.append(run)
        run = None
if run is not None:
    events.append(run)

gaps = [b - a for a, b in zip(events, events[1:])]
out = {'raw': raw, 'frames': frames, 'fps': FPS, 'threshold': THRESH,
       'events': len(events), 'event_frames': events,
       'gaps_frames': gaps,
       'median_gap_frames': statistics.median(gaps) if gaps else None,
       'rate_from_median_gap': round(FPS / statistics.median(gaps), 3) if gaps else None,
       'span_s': round((events[-1] - events[0]) / FPS, 2) if len(events) > 1 else None,
       'rate_over_span': round((len(events) - 1) * FPS / (events[-1] - events[0]), 3) if len(events) > 1 else None,
       'max_diff': max(diffs) if diffs else None,
       # A capture whose every transition is above the threshold has no quiet frame
       # to take a median of; report null rather than raising on an empty list.
       'quiet_median_diff': statistics.median(quiet) if quiet else None}
print(json.dumps(out))
