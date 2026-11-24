#!/usr/bin/env python3
"""Kit B run a2's gate (method.md, "Kit B run a2"), committed before either capture.

Inputs: each package's two recordings from hil_audio_wolf3d_a2.py, 48 kHz stereo s16: the floor
(8 s at the FreeDOS prompt) and the game (40 s from the Wolfenstein 3D launch).

Per whole second: AC RMS in counts, each channel's mean over that second removed, both channels
together. Each package's game seconds from the 5th on (index 4..) are scored; the launch takes the
first seconds.

  presence (each package): T = max(20, 10 * the floor's median AC RMS). Pass when at least half of
                           the scored game seconds have AC RMS >= T.
  level (main against the cut): L = 20 * log10(cut median / main median) over the scored game
                           seconds' AC RMS. Pass when |L| <= 3 dB.
  a2 passes when both presences and the level comparison pass.

  a2_analyse.py MAIN_FLOOR.wav MAIN_GAME.wav CUT_FLOOR.wav CUT_GAME.wav
Prints JSON and exits 0 on pass, 1 on fail, 2 when a recording cannot be read or is shorter than
30 s (not measurable).
"""
import json, math, statistics, struct, sys, wave

FIRST_SCORED = 4
PRESENCE_MIN, PRESENCE_FACTOR, LEVEL_DB = 20.0, 10.0, 3.0


def seconds(path):
    with wave.open(path) as w:
        rate, channels, width, n = w.getframerate(), w.getnchannels(), w.getsampwidth(), w.getnframes()
        if (rate, channels, width) != (48000, 2, 2):
            raise ValueError(f'{path}: {rate} Hz, {channels} channels, {width} bytes')
        data = struct.unpack(f'<{n * 2}h', w.readframes(n))
    out = []
    for start in range(0, n - rate + 1, rate):
        total = 0.0
        for channel in (0, 1):
            seg = data[2 * start + channel:2 * (start + rate):2]
            mean = sum(seg) / rate
            total += sum((v - mean) ** 2 for v in seg)
        out.append(round(math.sqrt(total / (2 * rate)), 1))
    return out


def package(floor_path, game_path):
    floor, game = seconds(floor_path), seconds(game_path)
    if len(game) < 30 or len(floor) < 5:
        raise ValueError(f'short recording: floor {len(floor)} s, game {len(game)} s')
    scored = game[FIRST_SCORED:]
    threshold = max(PRESENCE_MIN, PRESENCE_FACTOR * statistics.median(floor))
    loud = sum(1 for v in scored if v >= threshold)
    return {'floor_ac_rms': floor, 'game_ac_rms': game, 'threshold': threshold, 'scored': len(scored),
            'loud': loud, 'presence': loud * 2 >= len(scored), 'median': statistics.median(scored)}


def main(argv):
    try:
        main_pkg, cut = package(argv[0], argv[1]), package(argv[2], argv[3])
    except (OSError, ValueError, EOFError, struct.error) as error:
        print(json.dumps({'measurable': False, 'error': repr(error)}))
        return 2
    level = 20 * math.log10(max(cut['median'], 1e-9) / max(main_pkg['median'], 1e-9))
    result = {'measurable': True, 'main': main_pkg, 'cut': cut, 'level_db': round(level, 2),
              'level': abs(level) <= LEVEL_DB}
    result['pass'] = main_pkg['presence'] and cut['presence'] and result['level']
    print(json.dumps(result, indent=1))
    return 0 if result['pass'] else 1


if __name__ == '__main__':
    raise SystemExit(main(sys.argv[1:]))
