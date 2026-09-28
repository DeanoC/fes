#!/usr/bin/env python3
"""Synthetic menu-display wire oracle; never deploy these identities."""
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / 'testdata/menu-display-v1/exchanges.json'
BUILD = bytes.fromhex('00112233445566778899aabbccddeeff')

class Endpoint:
    def __init__(self):
        self.configured = False
        self.enabled = False
        self.held = True
        self.quiesced = True
        self.faulted = False
        self.pending = None
        self.displayed = 0
        self.accepted = 0
        self.underflows = 0
        self.stage = []
        self.snapshots = {}

    def frame_boundary(self):
        if self.pending is not None:
            self.displayed = self.pending[1]
            self.pending = None

    def command(self, op, index, arg):
        if op == 1:
            if index > 15: return 2, True
            if arg: return 3, True
            words = [0x4546,0x3153,1,0,3,1,0,770] + [int.from_bytes(BUILD[i:i+2],'little') for i in range(0,16,2)]
            return words[index], False
        if op == 2:
            if index: return 2, True
            if arg not in (0,1): return 3, True
            if arg == 0 and not self.quiesced: return 4, True
            self.held = arg == 0
            return 0, False
        if op == 18:
            if index > 13: return 2, True
            if arg: return 3, True
            fixed = [1280,720,5120,3686400 & 65535,3686400 >> 16,0,64,1,2]
            if index < 9: return fixed[index], False
            if index == 9:
                return (int(self.configured) | int(self.enabled)*2 | int(self.pending is not None)*4 |
                        int(self.quiesced)*8 | int(self.faulted)*16), False
            if index in (10,12):
                value = self.displayed if index == 10 else self.underflows
                self.snapshots[index+1] = value >> 16
                return value & 65535, False
            if index not in self.snapshots: return 4, True
            return self.snapshots.pop(index), False
        if op == 19:
            if index: return 2, True
            if arg != 1: return 3, True
            if self.enabled or not self.quiesced or self.faulted: return 4, True
            self.configured = True
            return 0, False
        if op == 20:
            if index: return 2, True
            if arg not in (0,1): return 3, True
            if arg == 1:
                if not self.configured or self.held or self.faulted: return 4, True
                self.enabled = True
                self.quiesced = False
            else:
                self.enabled = False
                self.quiesced = True
                self.pending = None
                self.stage = []
            return 0, False
        if op == 21:
            if index > 2: return 2, True
            if index == 2 and arg > 1: return 3, True
            if not self.enabled or self.pending is not None or self.faulted: return 4, True
            if index != len(self.stage): return 4, True
            if index < 2:
                self.stage.append(arg)
                return 0, False
            sequence = self.stage[0] | self.stage[1] << 16
            if not sequence or sequence <= self.accepted: return 4, True
            self.accepted = sequence
            self.pending = (arg, sequence)
            self.stage = []
            return 0, False
        return 1, True


def fixtures():
    endpoint = Endpoint()
    toggle = False
    exchanges = []
    def add(name, op, index=0, arg=0, **extra):
        nonlocal toggle
        data, error = endpoint.command(op,index,arg)
        word = op << 24 | index << 16 | arg
        before = word | int(toggle) << 31
        toggle = not toggle
        exchanges.append(dict(name=name,gpo=[before,word | int(toggle) << 31],
            gpi=0xf5000000 | int(toggle)*0x800000 | int(error)*0x400000 | data,
            data=data,error=error,**extra))
    for i in range(16): add(f'identity-{i:02}',1,i)
    add('disabled-enable-rejected',20,0,1)
    add('bad-layout-rejected',19,0,2)
    add('configure',19,0,1)
    for i in range(10): add(f'info-{i}',18,i)
    add('high-without-low-rejected',18,11)
    add('release',2,0,1)
    add('enable',20,0,1)
    add('configure-enabled-rejected',19,0,1)
    add('hold-before-drain-rejected',2,0,0)
    add('commit-without-fields-rejected',21,2,1)
    add('sequence-low',21,0,1)
    add('sequence-high',21,1,0)
    add('commit',21,2,1)
    add('not-displayed-yet',18,10)
    add('not-displayed-high',18,11)
    add('second-pending-rejected',21,0,2)
    endpoint.frame_boundary()
    add('displayed',18,10,event_before='frame-boundary')
    add('displayed-high',18,11)
    add('quiesce',20,0,0,wait_for='drained')
    add('held-after-drain',2,0,0)
    add('quiesced-state',18,9)
    return dict(description='Synthetic wire oracle; not hardware acceptance.',build_id=BUILD.hex(),
                capabilities=770,initial_request_toggle=False,exchanges=exchanges)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--check',action='store_true')
    args = parser.parse_args()
    data = json.dumps(fixtures(),indent=2)+'\n'
    if args.check:
        if not OUTPUT.exists() or OUTPUT.read_text() != data:
            parser.exit(1,'menu display fixtures are stale\n')
    else:
        OUTPUT.parent.mkdir(parents=True,exist_ok=True)
        OUTPUT.write_text(data)

if __name__ == '__main__': main()
