"""Compare two kit HDMI audio captures (pure Python): DC-removed 12.5 ms RMS envelopes, music onset,
and the envelopes' correlation at the best lag within +-5 s."""
import array, math, statistics, sys, wave
def env(p):
    w = wave.open(p); r, ch = w.getframerate(), w.getnchannels()
    d = array.array('h', w.readframes(w.getnframes()))
    mono = [(d[i] + d[i + 1]) / 2 for i in range(0, len(d) - 1, ch)]
    dc = statistics.median(mono[::97]); hop = r // 80
    e = []
    for s in range(0, len(mono) - hop, hop):
        seg = mono[s:s + hop]; e.append(math.sqrt(sum((v - dc) ** 2 for v in seg) / hop))
    peak = max(abs(v - dc) for v in mono)
    return e, dc, peak
def corr(x, y):
    mx, my = sum(x) / len(x), sum(y) / len(y)
    sxy = sum((a - mx) * (b - my) for a, b in zip(x, y)); sx = math.sqrt(sum((a - mx) ** 2 for a in x)); sy = math.sqrt(sum((b - my) ** 2 for b in y))
    return sxy / (sx * sy)
ea, dca, pa = env(sys.argv[1]); eb, dcb, pb = env(sys.argv[2])
for name, e, dc, pk in (('A', ea, dca, pa), ('B', eb, dcb, pb)):
    floor = statistics.median(e[:80]); th = 4 * floor + 30
    on = next(i for i, v in enumerate(e) if v > th)
    print(name, 'DC', dc, 'AC peak', round(pk), 'floor rms', round(floor, 1), 'onset s', on / 80,
          'active s', round(sum(v > th for v in e) / 80, 1), 'mean rms after onset', round(statistics.mean(e[on:]), 1))
best = (-2, 0)
for lag in range(-400, 401, 2):
    x, y = (ea[lag:], eb) if lag >= 0 else (ea, eb[-lag:]); n = min(len(x), len(y)) 
    c = corr(x[:n], y[:n]); best = max(best, (c, lag))
c, lag = best
for l in range(lag - 2, lag + 3):
    x, y = (ea[l:], eb) if l >= 0 else (ea, eb[-l:]); n = min(len(x), len(y)); best = max(best, (corr(x[:n], y[:n]), l))
print('best lag s', best[1] / 80, 'envelope corr', round(best[0], 4))
