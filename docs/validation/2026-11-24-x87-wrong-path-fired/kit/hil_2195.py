#!/usr/bin/env python3
"""hil_2195.py: FreeDOS boot + a DOOM v1.9 smoke (-timedemo demo3) on a sealed fes.pc386 package (fes#2195 x87 fix DE10 reseal kit run; copied from ~/fes-scratch/s4kit/hil_s4_dos.py, the Wolfenstein goal replaced). Drive: pmode (DOOM at \\doom).

A scratch driver over the repo's pc386_wfw_kit_acceptance.Run (lease, staging, launch, input,
HDMI grabs, stop, cleanup, after-state checks); only the goals differ. Core-only: nothing
installed, no image change. The caller claims the kit and logs the restore.
  hil_s4_dos.py REPO --kit B --owner Claude --package P.fcore --out DIR --kit-ssh KB
"""
import sys, time
from pathlib import Path
REPO = Path(sys.argv.pop(1)).resolve()
sys.path.insert(0, str(REPO))
from scripts import pc386_wfw_kit_acceptance as acc
from scripts import pc386_wfw as wfw

acc.KEYS.update({'up': 0x52, 'down': 0x51, 'left': 0x50, 'right': 0x4f})


def busy(frame) -> float:
    """Share of non-black pixels on a 640x480 picture."""
    p = wfw.picture(frame)
    lit = sum(1 for y in range(0, p.height, 4) for x in range(0, p.width, 4) if max(p.pixel(x, y)) > 40)
    return lit / ((p.height // 4) * (p.width // 4))


def colourful(frame) -> float:
    """Share of pixels whose channels differ by more than 40 (a DOOM palette; not grey text)."""
    p = wfw.picture(frame)
    hits = sum(1 for y in range(0, p.height, 4) for x in range(0, p.width, 4)
               if max(p.pixel(x, y)) - min(p.pixel(x, y)) > 40)
    return hits / ((p.height // 4) * (p.width // 4))


def changed(one, two) -> float:
    a, b = wfw.picture(one), wfw.picture(two)
    hits = sum(1 for y in range(0, a.height, 4) for x in range(0, a.width, 4)
               if max(abs(p - q) for p, q in zip(a.pixel(x, y), b.pixel(x, y))) > 60)
    return hits / ((a.height // 4) * (a.width // 4))


class Run(acc.Run):
    def __init__(self, args):
        super().__init__(args)
        self.game = f'x2195-dos-{self.nonce.lower()}'
        self.result['library_game'] = self.game

    def boot_dos(self):
        self.load(self.game, self.image, self.record)
        time.sleep(10)
        frame, seconds = self.wait_for('freedos-prompt', lambda f: wfw.text_screen(f)['found'], 120, every=2)
        self.goal('freedos', frame is not None, seconds_to_detect=seconds, frame='frames/freedos-prompt.png')
        time.sleep(5)
        self.input().type('ver\n')
        time.sleep(2)
        self.grab('freedos-ver')

    def doom(self):
        """DOOM v1.9 (DOS/4GW, no FPU) -timedemo demo3: its 3D view drawn (colourful, which a
        grey-on-black text screen is not) and moving, then its return to a still, colourless
        DOS screen with the timedemo's result. k2195b's check used wfw.text_screen, which reads
        a dark DOOM frame between the 1080p capture's black side bars as a text screen."""
        remote = self.input()
        remote.type('cd \\doom\ndoom -timedemo demo3\n')
        time.sleep(40)
        a = self.grab('doom-demo-1')
        time.sleep(3)
        b = self.grab('doom-demo-2')
        drawn = colourful(b) > 0.02 and changed(a, b) > 0.05
        last, done, began = b, None, time.monotonic()
        while time.monotonic() - began < 600:
            time.sleep(5)
            frame = self.grab('doom-wait')
            if colourful(frame) < 0.01 and changed(last, frame) < 0.01:
                done = self.grab('doom-timedemo-done')
                break
            last = frame
        self.goal('doom', drawn and done is not None, colourful=round(colourful(b), 3),
                  changed=round(changed(a, b), 3), seconds_to_prompt=round(time.monotonic() - began, 1),
                  frames=['frames/doom-demo-1.png', 'frames/doom-demo-2.png', 'frames/doom-timedemo-done.png'])

    def program_manager(self):
        self.doom()

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
        # The parent's pass rule counts four goals; this run has two.
        self.result['pass'] = self.result.get('failure') is None and len(self.result['goals']) == 2 and \
            all(g['pass'] for g in self.result['goals'].values())
        self.save()
        print('RESULT', 'PASS' if self.result['pass'] else 'FAIL', flush=True)
        return 0 if self.result['pass'] else 1
    m.Run.main = main
    sys.argv[1:1] = []
    sys.exit(acc.main(sys.argv[1:] + (['--drive', 'pmode'] if '--drive' not in sys.argv else [])))
