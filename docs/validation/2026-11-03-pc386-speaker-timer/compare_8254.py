#!/usr/bin/env python3
"""Check kf8253_routine_bench.sv's traces (#1457).

For each trace-<mode>.txt given: PIT clocks, clocks with the gate high, OUT2
high and the speaker on. For the at trace (counter 2 in mode 3, count 60),
also compare OUT2 with an 8254 model. In the model a low gate drives OUT
high and holds the count, and a rising gate reloads the count with OUT high.
While the gate stays high, each clock takes 2 from the count, and OUT toggles
and the count reloads at 0. The model counts on the clock that samples the
gate, and KF8253 on the next falling edge, so the check allows one PIT clock
of lag. It also prints the run lengths of OUT2 while the gate stays high.
"""
import collections
import sys


def rows(path):
    with open(path) as trace:
        return [tuple(map(int, line.split())) for line in trace]


def model(trace, count=60):
    value, out, previous, outs = None, 1, 0, []
    for _, gate, _, _ in trace:
        if not gate:
            out = 1
        elif not previous:
            value, out = count, 1
        elif value is not None:
            value -= 2
            if value == 0:
                out, value = out ^ 1, count
        previous = gate
        outs.append(out)
    return outs


for path in sys.argv[1:]:
    trace = rows(path)
    print(path, 'PIT clocks', len(trace), 'gate high', sum(r[1] for r in trace),
          'OUT2 high', sum(r[3] for r in trace), 'speaker on', sum(r[2] and r[3] for r in trace))
    if 'at' not in path.rsplit('/', 1)[-1]:
        continue
    expected = model(trace)
    beyond = [i for i in range(1, len(trace)) if trace[i][3] not in (expected[i], expected[i - 1])]
    print('  clocks where OUT2 differs from the 8254 model by more than one PIT clock:', len(beyond))
    runs, level, length = collections.Counter(), None, 0
    for _, gate, _, out in trace:
        if not gate:
            level, length = None, 0
        elif out == level:
            length += 1
        else:
            if level is not None:
                runs[(level, length)] += 1
            level, length = out, 1
    print('  OUT2 runs with the gate high (level, clocks): count', dict(runs.most_common(6)))
