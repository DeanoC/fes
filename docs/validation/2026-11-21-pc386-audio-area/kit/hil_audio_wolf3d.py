#!/usr/bin/env python3
"""hil_audio_wolf3d.py: FreeDOS boot + Wolfenstein 3D's AdLib music on a sealed fes.pc386 package
(pc386 audio area; copied from ~/fes-scratch/s2029kit/hil_s4_dos.py, with an ALSA recording).

A scratch driver over the repo's pc386_wfw_kit_acceptance.Run (lease, staging, launch, input,
HDMI grabs, stop, cleanup, after-state checks). Core-only: nothing installed, no image change.
The caller claims the kit and logs the restore.

Audio goal, fixed before the run: the HDMI capture's ALSA input (Kit B hw:CARD=D4KPRO) is
recorded for 8 s at the FreeDOS prompt (the floor) and for 40 s from the Wolfenstein 3D launch
(the title, menus and E1L1, where the game plays its OPL2 music). Pass when at least half of the
game recording's whole seconds have an RMS at least 20 dB above the floor's median second.
  hil_audio_wolf3d.py REPO --kit B --owner Claude --package P.fcore --out DIR --kit-ssh KB
"""
import math, struct, subprocess, sys, time, wave
from pathlib import Path
REPO = Path(sys.argv.pop(1)).resolve()
sys.path.insert(0, str(REPO))
from scripts import pc386_wfw_kit_acceptance as acc
from scripts import pc386_wfw as wfw

acc.KEYS.update({'up': 0x52, 'down': 0x51, 'left': 0x50, 'right': 0x4f})
AUDIO = 'hw:CARD=D4KPRO'


def busy(frame) -> float:
    """Share of non-black pixels on a 640x480 picture."""
    p = wfw.picture(frame)
    lit = sum(1 for y in range(0, p.height, 4) for x in range(0, p.width, 4) if max(p.pixel(x, y)) > 40)
    return lit / ((p.height // 4) * (p.width // 4))


def record(path: Path, seconds: int) -> subprocess.Popen:
    return subprocess.Popen(['timeout', '-k', '5', str(seconds + 30), 'ffmpeg', '-loglevel', 'error', '-y',
                             '-f', 'alsa', '-ac', '2', '-ar', '48000', '-i', AUDIO, '-t', str(seconds),
                             str(path)], stdin=subprocess.DEVNULL)


def seconds_rms(path: Path) -> list[float]:
    """dBFS of each whole second, both channels together."""
    with wave.open(str(path)) as w:
        rate, channels, width = w.getframerate(), w.getnchannels(), w.getsampwidth()
        assert width == 2, width
        data = w.readframes(w.getnframes())
    samples = struct.unpack(f'<{len(data) // 2}h', data)
    step = rate * channels
    out = []
    for start in range(0, len(samples) - step + 1, step):
        chunk = samples[start:start + step]
        rms = math.sqrt(sum(s * s for s in chunk) / len(chunk))
        out.append(round(20 * math.log10(max(rms, 1e-3) / 32768), 1))
    return out


class Run(acc.Run):
    def __init__(self, args):
        super().__init__(args)
        self.game = f'audio-area-{self.nonce.lower()}'
        self.result['library_game'] = self.game

    def boot_dos(self):
        self.load(self.game, self.image, self.record)
        time.sleep(10)
        frame, seconds = self.wait_for('freedos-prompt', lambda f: wfw.text_screen(f)['found'], 120, every=2)
        self.goal('freedos', frame is not None, seconds_to_detect=seconds, frame='frames/freedos-prompt.png')
        time.sleep(5)
        floor = self.out / 'audio-floor.wav'
        record(floor, 8).wait()
        self.floor = floor

    def wolf3d(self):
        remote = self.input()
        music = self.out / 'audio-wolf3d.wav'
        recorder = record(music, 40)
        remote.type('cd \\wolf3d\nwolf3d\n')
        time.sleep(2)
        for n in range(9):
            remote.tap('enter', hold=0.12)
            time.sleep(1.5)
            self.grab(f'wolf3d-menu-{n}')
        time.sleep(3)
        start = self.grab('wolf3d-floor1')
        for n in range(6):
            remote.key('up', True)
            time.sleep(0.8)
            remote.key('up', False)
            time.sleep(0.2)
        moved = self.grab('wolf3d-walked')
        a, b = wfw.picture(start), wfw.picture(moved)
        changed = sum(1 for y in range(0, a.height, 4) for x in range(0, a.width, 4)
                      if max(abs(p - q) for p, q in zip(a.pixel(x, y), b.pixel(x, y))) > 60)
        share = changed / ((a.height // 4) * (a.width // 4))
        graphics = not wfw.text_screen(moved)['found']
        self.goal('wolf3d', graphics and busy(moved) > 0.3 and share > 0.05,
                  graphics=graphics, lit=round(busy(moved), 3), changed_after_walk=round(share, 3),
                  frames=['frames/wolf3d-floor1.png', 'frames/wolf3d-walked.png'])
        recorder.wait()
        try:
            floor = seconds_rms(self.floor)
            game = seconds_rms(music)
            median = sorted(floor)[len(floor) // 2]
            loud = sum(1 for v in game if v >= median + 20)
            self.goal('music', len(game) >= 30 and loud * 2 >= len(game), floor_dbfs=floor, game_dbfs=game,
                      floor_median=median, loud_seconds=loud, seconds=len(game),
                      files=['audio-floor.wav', 'audio-wolf3d.wav'])
        except Exception as error:  # a missing or short recording fails the goal
            self.goal('music', False, error=repr(error)[:300])
        remote.tap('esc', hold=0.12)
        time.sleep(1)

    def program_manager(self):
        self.wolf3d()

    def mouse(self): pass
    def dos_window(self): pass
    def notepad(self): pass
    def exit_windows(self): pass

    def read_back(self, created):
        # The parent's WfW read-back goals do not apply: placeholders that the
        # wrapped main() below removes before it judges the run.
        self.result['goals'].setdefault('dos-window', {'pass': True, 'placeholder': True})
        self.result['goals'].setdefault('notepad', {'pass': True, 'placeholder': True})
        return {'dos': True, 'notepad': True, 'files': {'WFWDOS.TXT': None, 'WFWNOTE.TXT': None}}

    def goal(self, name, passed, **evidence):
        if name in ('dos-window', 'notepad'):
            return  # the WfW read-back goals do not apply
        super().goal(name, passed, **evidence)


if __name__ == '__main__':
    acc.Run = Run
    import scripts.pc386_wfw_kit_acceptance as m
    orig = m.Run.main

    def main(self):
        rc = orig(self)
        for name in ('dos-window', 'notepad'):
            self.result['goals'].pop(name, None)
        # Three goals: freedos, wolf3d, music.
        self.result['pass'] = self.result.get('failure') is None and len(self.result['goals']) == 3 and \
            all(g['pass'] for g in self.result['goals'].values())
        self.save()
        print('RESULT', 'PASS' if self.result['pass'] else 'FAIL', flush=True)
        return 0 if self.result['pass'] else 1
    m.Run.main = main
    sys.exit(acc.main(sys.argv[1:] + (['--drive', 'real'] if '--drive' not in sys.argv else [])))
