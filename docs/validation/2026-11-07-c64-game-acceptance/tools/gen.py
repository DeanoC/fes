#!/usr/bin/env python3
"""Generate the kit (c64run.py) and VICE (vref.py) scripts for the fes#1531 matrix from one spec.

Kit keys are HID names (Ctrl is lctrl); VICE keys are C64 matrix names (ctrl). BASIC
LOAD/RUN goes through `type` on both (kit: HID key taps; VICE: KERNAL keyboard feed).
"""
import json
from pathlib import Path

W = Path('/home/deano/fes-scratch/c64acc')
G = '/mnt/emuhd/Games/C64/C64 Forever 10 Plus/Games'
MW = '/home/deano/fes-scratch/c64acc/media'
PLAY = 300  # seconds of seeded joystick play per wander block


def key(name, mods=(), hold=0.1):
    return {'op': 'key', 'name': name, 'mods': list(mods), 'hold': hold}


def kik(run):
    s = [{'op': 'sleep', 's': 5}, {'op': 'grab', 'name': f'{run}-00-basic'},
         {'op': 'type', 'text': 'LOAD"*",8,1\n'}, {'op': 'sleep', 's': 60}, {'op': 'grab', 'name': f'{run}-01-loaded'},
         {'op': 'type', 'text': 'RUN\n'}, {'op': 'sleep', 's': 20}, {'op': 'grab', 'name': f'{run}-02-trainer'},
         key('space'), {'op': 'sleep', 's': 6}, key('l'), {'op': 'sleep', 's': 10}, {'op': 'grab', 'name': f'{run}-03-hiload'},
         key('n'), {'op': 'sleep', 's': 5}, key('n'), {'op': 'sleep', 's': 12}, {'op': 'grab', 'name': f'{run}-04-title'},
         key('f1'), {'op': 'sleep', 's': 1}, key('f1'), {'op': 'sleep', 's': 2}, {'op': 'grab', 'name': f'{run}-05-twojoy'},
         key('f7'), {'op': 'sleep', 's': 6}, {'op': 'grab', 'name': f'{run}-06-start'},
         {'op': 'joydown', 'port': 2, 'dirs': 'R'}, {'op': 'sleep', 's': 2.5}, {'op': 'joyup', 'port': 2},
         {'op': 'grab', 'name': f'{run}-07-port2'},
         {'op': 'joydown', 'port': 1, 'dirs': 'R'}, {'op': 'sleep', 's': 2.5}, {'op': 'joyup', 'port': 1},
         {'op': 'grab', 'name': f'{run}-08-port1'},
         {'op': 'joydown', 'port': 1, 'dirs': 'R'}, {'op': 'joydown', 'port': 2, 'dirs': 'R'},
         {'op': 'sleep', 's': 80}, {'op': 'grab', 'name': f'{run}-09-course1'},
         {'op': 'sleep', 's': 30}, {'op': 'grab', 'name': f'{run}-10-course2'},
         {'op': 'sleep', 's': 120}, {'op': 'grab', 'name': f'{run}-11-after-race'},
         {'op': 'joyup', 'port': 1}, {'op': 'joyup', 'port': 2}, {'op': 'sleep', 's': 10}]
    # Sustained play: a race ends on the title after three courses and the title falls back to one player, so
    # F1 F1 selects two players with two joysticks and F7 starts a new race before each block.
    for i, port in enumerate((1, 2, 1, 2)):
        s += [key('f1'), {'op': 'sleep', 's': 1}, key('f1'), {'op': 'sleep', 's': 1}, key('f7'), {'op': 'sleep', 's': 4},
              {'op': 'wander', 'port': port, 'seconds': PLAY // 2, 'seed': 11 + i, 'grab_every': 30,
               'prefix': f'{run}-p{i}'}]
    return s + [{'op': 'grab', 'name': f'{run}-12-end'}]


def lode(run):
    return [{'op': 'sleep', 's': 5}, {'op': 'grab', 'name': f'{run}-00-basic'},
            {'op': 'type', 'text': 'LOAD"*",8,1\n'}, {'op': 'sleep', 's': 20}, {'op': 'type', 'text': 'RUN\n'},
            {'op': 'sleep', 's': 30}, {'op': 'grab', 'name': f'{run}-01-loading'},
            {'op': 'sleep', 's': 75}, {'op': 'grab', 'name': f'{run}-02-attract'},
            {'op': 'joy', 'port': 2, 'dirs': 'F', 'hold': 0.2}, {'op': 'sleep', 's': 5},
            {'op': 'grab', 'name': f'{run}-03-level1'},
            {'op': 'joydown', 'port': 2, 'dirs': 'L'}, {'op': 'sleep', 's': 1.5}, {'op': 'joyup', 'port': 2},
            {'op': 'grab', 'name': f'{run}-04-moved'},
            key('a', ('ctrl',), 0.3), {'op': 'sleep', 's': 6}, {'op': 'grab', 'name': f'{run}-05-ctrl-a'},
            key('u', ('ctrl',), 0.3), {'op': 'sleep', 's': 8}, {'op': 'grab', 'name': f'{run}-06-ctrl-u'},
            {'op': 'wander', 'port': 2, 'seconds': 2 * PLAY, 'seed': 21, 'grab_every': 30, 'prefix': f'{run}-p'},
            {'op': 'grab', 'name': f'{run}-07-end'}]


def mw4(run):
    return [{'op': 'sleep', 's': 5}, {'op': 'grab', 'name': f'{run}-00-basic'},
            {'op': 'type', 'text': 'LOAD"*",8,1\n'}, {'op': 'sleep', 's': 15}, {'op': 'type', 'text': 'RUN\n'},
            {'op': 'sleep', 's': 25}, {'op': 'grab', 'name': f'{run}-01-config'},
            {'op': 'joy', 'port': 2, 'dirs': 'U', 'n': 3, 'hold': 0.2, 'gap': 0.3},
            {'op': 'joy', 'port': 2, 'dirs': 'F', 'hold': 0.2}, {'op': 'sleep', 's': 1},
            {'op': 'grab', 'name': f'{run}-02-fastloader-on'},
            {'op': 'joy', 'port': 2, 'dirs': 'D', 'n': 3, 'hold': 0.2, 'gap': 0.3},
            {'op': 'joy', 'port': 2, 'dirs': 'F', 'hold': 0.2},
            {'op': 'sleep', 's': 30}, {'op': 'grab', 'name': f'{run}-03-picture'},
            {'op': 'sleep', 's': 70}, {'op': 'grab', 'name': f'{run}-04-side2-prompt'},
            {'op': 'live', 'disk': 'mw4b'}, {'op': 'sleep', 's': 2}, {'op': 'grab', 'name': f'{run}-05-swapped'},
            {'op': 'joy', 'port': 2, 'dirs': 'F', 'hold': 0.3}, {'op': 'sleep', 's': 40},
            {'op': 'grab', 'name': f'{run}-06-title'},
            {'op': 'joy', 'port': 2, 'dirs': 'F', 'hold': 0.3}, {'op': 'sleep', 's': 8},
            {'op': 'joy', 'port': 2, 'dirs': 'F', 'hold': 0.3}, {'op': 'sleep', 's': 8},
            {'op': 'grab', 'name': f'{run}-07-difficulty'},
            {'op': 'joy', 'port': 2, 'dirs': 'F', 'hold': 0.3}, {'op': 'sleep', 's': 90},
            {'op': 'grab', 'name': f'{run}-08-ingame'},
            {'op': 'wander', 'port': 2, 'seconds': 2 * PLAY, 'seed': 31, 'grab_every': 30, 'prefix': f'{run}-p'},
            {'op': 'grab', 'name': f'{run}-09-end'}]


def kikport(run):
    s = kik(run)[:26]  # boot, two joysticks, F7, grab of the race start
    for i, port in enumerate((2, 1, 2)):
        s += [{'op': 'joydown', 'port': port, 'dirs': 'R'}, {'op': 'sleep', 's': 8},
              {'op': 'grab', 'name': f'{run}-iso{i}-port{port}-held'},
              {'op': 'joyup', 'port': port}, {'op': 'sleep', 's': 3}, {'op': 'grab', 'name': f'{run}-iso{i}-port{port}-released'}]
    return s


GAMES = {'kik': (kik, 'kik'), 'lode': (lode, 'lode'), 'mw4': (mw4, 'mw4a')}


def kit_script(game):
    fn, disk = GAMES[game]
    out = []
    for run in ('r1', 'r2'):
        out += [{'op': 'launch', 'game': game, 'disk': disk}, {'op': 'session'},
                {'op': 'audio_start', 'name': f'{run}-audio'}]
        out += fn(run)
        out += [{'op': 'audio_stop'}, {'op': 'video_start', 'name': f'{run}-video', 'seconds': 20},
                {'op': 'video_stop'}]
        if game == 'mw4' and run == 'r1':
            out += [{'op': 'live_clear'}, {'op': 'sleep', 's': 2}, {'op': 'grab', 'name': 'r1-10-ejected'},
                    {'op': 'session'}]
        out += [{'op': 'session'}, {'op': 'stop'}, {'op': 'sleep', 's': 8}]
    return out + [{'op': 'quit'}]


def vice_script(game):
    fn, _ = GAMES[game]
    out = []
    for op in fn('ref'):
        op = dict(op)
        if op['op'] == 'key':
            op['mods'] = ['ctrl' if m == 'ctrl' else m for m in op['mods']]
        if op['op'] == 'live':
            op = {'op': 'attach', 'd64': MW + '/mw42.d64'}
        out.append(op)
    return out + [{'op': 'quit'}]


def kit_key(op):
    if op['op'] == 'type':
        # Unshifted HID letters are C64 capitals; Shift+letter would type PETSCII graphics.
        op = dict(op, text=op['text'].lower())
    if op['op'] == 'key':
        op = dict(op)
        op['mods'] = ['lctrl' if m == 'ctrl' else m for m in op['mods']]
    return op


if __name__ == '__main__':
    (W / 'scripts').mkdir(exist_ok=True)
    for g in GAMES:
        (W / 'scripts' / f'kit-{g}.jsonl').write_text('\n'.join(json.dumps(kit_key(o)) for o in kit_script(g)) + '\n')
        (W / 'scripts' / f'vice-{g}.jsonl').write_text('\n'.join(json.dumps(o) for o in vice_script(g)) + '\n')
    print('ok')


def extra():
    """Port isolation diagnostic on Kikstart, then the Lode Runner case, in one claim."""
    iso = [{'op': 'launch', 'game': 'kik', 'disk': 'kik'}, {'op': 'session'}] + [kit_key(o) for o in kikport('iso')] + \
          [{'op': 'session'}, {'op': 'stop'}, {'op': 'sleep', 's': 8}]
    lode = [kit_key(o) for o in kit_script('lode')]
    (W / 'scripts' / 'kit-iso-lode.jsonl').write_text('\n'.join(json.dumps(o) for o in iso + lode) + '\n')
    (W / 'scripts' / 'vice-iso.jsonl').write_text('\n'.join(json.dumps(o) for o in kikport('ref') + [{'op': 'quit'}]) + '\n')


if __name__ == '__main__':
    extra()


def portprobe():
    """BASIC loop printing CIA1 PRA/PRB ($DC00/$DC01) while each port is held, on a fresh launch (no disk load)."""
    s = [{'op': 'launch', 'game': 'kik', 'disk': 'kik'}, {'op': 'session'}, {'op': 'sleep', 's': 5},
         {'op': 'type', 'text': '10 print peek(56320);peek(56321):goto 10\nrun\n'}, {'op': 'sleep', 's': 3},
         {'op': 'grab', 'name': 'pp-00-idle'}]
    for name, port, dirs in (('p2-right', 2, 'R'), ('p1-right', 1, 'R'), ('p2-fire', 2, 'F'), ('p1-fire', 1, 'F'),
                             ('p2-up', 2, 'U'), ('p1-left', 1, 'L')):
        s += [{'op': 'joydown', 'port': port, 'dirs': dirs}, {'op': 'sleep', 's': 2}, {'op': 'grab', 'name': f'pp-{name}'},
              {'op': 'joyup', 'port': port}, {'op': 'sleep', 's': 2}, {'op': 'grab', 'name': f'pp-{name}-released'}]
    s += [{'op': 'joydown', 'port': 2, 'dirs': 'R'}, {'op': 'joydown', 'port': 1, 'dirs': 'L'}, {'op': 'sleep', 's': 2},
          {'op': 'grab', 'name': 'pp-p2R-p1L'}, {'op': 'joyup', 'port': 2}, {'op': 'joyup', 'port': 1},
          {'op': 'session'}, {'op': 'stop'}, {'op': 'sleep', 's': 8}]
    mw = [kit_key(o) for o in kit_script('mw4')]
    (W / 'scripts' / 'kit-probe-mw4.jsonl').write_text('\n'.join(json.dumps(kit_key(o)) for o in s + mw) + '\n')
    # VICE: same probe
    # VICE's keyboard feed takes ASCII capitals for C64 capitals.
    v = [dict(o, text=o['text'].upper()) if o['op'] == 'type' else o for o in s if o['op'] not in ('launch', 'session', 'stop')]
    (W / 'scripts' / 'vice-portprobe.jsonl').write_text('\n'.join(json.dumps(o) for o in v + [{'op': 'quit'}]) + '\n')


if __name__ == '__main__':
    portprobe()


def kik2j(run):
    """Kikstart with the #1542 timing: port 2 held from before F7, port 1 only after race time ~15 s."""
    s = kik(run)[:23]  # boot to the title with two players, two joysticks selected (grab 05-twojoy)
    s += [{'op': 'joydown', 'port': 2, 'dirs': 'R'}, {'op': 'sleep', 's': 1}, key('f7'), {'op': 'sleep', 's': 6},
          {'op': 'grab', 'name': f'{run}-06-start-p2held'}, {'op': 'sleep', 's': 8},
          {'op': 'grab', 'name': f'{run}-07-p2held'}, {'op': 'joyup', 'port': 2}, {'op': 'sleep', 's': 1},
          {'op': 'joydown', 'port': 1, 'dirs': 'R'}, {'op': 'sleep', 's': 8},
          {'op': 'grab', 'name': f'{run}-08-p1held'},
          {'op': 'joydown', 'port': 2, 'dirs': 'R'},
          {'op': 'sleep', 's': 75}, {'op': 'grab', 'name': f'{run}-09-course1'},
          {'op': 'sleep', 's': 30}, {'op': 'grab', 'name': f'{run}-10-course2'},
          {'op': 'sleep', 's': 120}, {'op': 'grab', 'name': f'{run}-11-after-race'},
          {'op': 'joyup', 'port': 1}, {'op': 'joyup', 'port': 2}, {'op': 'sleep', 's': 10}]
    for i, port in enumerate((1, 2, 1, 2)):
        s += [key('f1'), {'op': 'sleep', 's': 1}, key('f1'), {'op': 'sleep', 's': 1}, key('f7'), {'op': 'sleep', 's': 4},
              {'op': 'wander', 'port': port, 'seconds': PLAY // 2, 'seed': 11 + i, 'grab_every': 30,
               'prefix': f'{run}-p{i}'}]
    return s + [{'op': 'grab', 'name': f'{run}-12-end'}]


def kik2j_scripts():
    kit = []
    for run in ('r1', 'r2'):
        kit += [{'op': 'launch', 'game': 'kik', 'disk': 'kik'}, {'op': 'session'},
                {'op': 'audio_start', 'name': f'{run}-audio'}] + [kit_key(o) for o in kik2j(run)] + \
               [{'op': 'audio_stop'}, {'op': 'video_start', 'name': f'{run}-video', 'seconds': 20},
                {'op': 'video_stop'}, {'op': 'session'}, {'op': 'stop'}, {'op': 'sleep', 's': 8}]
    (W / 'scripts' / 'kit-kik2j.jsonl').write_text('\n'.join(json.dumps(o) for o in kit + [{'op': 'quit'}]) + '\n')
    (W / 'scripts' / 'vice-kik2j.jsonl').write_text('\n'.join(json.dumps(o) for o in kik2j('ref') + [{'op': 'quit'}]) + '\n')


if __name__ == '__main__':
    kik2j_scripts()
