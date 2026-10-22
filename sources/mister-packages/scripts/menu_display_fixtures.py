#!/usr/bin/env python3
"""Synthetic menu-display and menu-blitter wire oracles; never deploy these identities."""
import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / 'testdata/menu-display-v1/exchanges.json'
BLITTER_OUTPUT = ROOT / 'testdata/menu-blitter-v1/exchanges.json'
BLIT_COMMANDS = 32768
BUILD = bytes.fromhex('00112233445566778899aabbccddeeff')

class Endpoint:
    def __init__(self, blitter=False):
        self.blitter = blitter
        self.capabilities = 770 | (1024 if blitter else 0)
        self.blit_stage = []
        self.blit_busy = False
        self.blit_completed = 0
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

    def blit_complete(self):
        self.blit_busy = False
        self.blit_completed = (self.blit_completed + 1) & 65535

    def frame_boundary(self):
        if self.pending is not None:
            self.displayed = self.pending[1]
            self.pending = None

    def command(self, op, index, arg):
        if op == 1:
            if index > 15: return 2, True
            if arg: return 3, True
            words = [0x4546,0x3153,1,0,3,1,0,self.capabilities] + [int.from_bytes(BUILD[i:i+2],'little') for i in range(0,16,2)]
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
                self.blit_stage = []
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
        if op == 22 and self.blitter:
            if index > 4: return 2, True
            if index in (3,4):
                if arg: return 3, True
                return (int(self.blit_busy) if index == 3 else self.blit_completed), False
            if index == 0 and arg >= BLIT_COMMANDS: return 3, True
            if index == 2 and arg > 1: return 3, True
            if not self.enabled or self.blit_busy or index != len(self.blit_stage): return 4, True
            if index == 1 and (arg == 0 or self.blit_stage[0] + arg > BLIT_COMMANDS): return 3, True
            if index < 2:
                self.blit_stage.append(arg)
                return 0, False
            self.blit_busy = True
            self.blit_stage = []
            return 0, False
        return 1, True


def fixtures(blitter=False):
    endpoint = Endpoint(blitter)
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
    if blitter:
        add('blit-state-idle',22,3)
        add('blit-disabled-rejected',22,0,0)
    add('release',2,0,1)
    add('enable',20,0,1)
    add('configure-enabled-rejected',19,0,1)
    if blitter:
        add('blit-index-rejected',22,5)
        add('blit-state-argument-rejected',22,3,1)
        add('blit-first-range-rejected',22,0,BLIT_COMMANDS)
        add('blit-run-without-fields-rejected',22,2,1)
        add('blit-count-without-first-rejected',22,1,1)
        add('blit-first',22,0,0)
        add('blit-count-zero-rejected',22,1,0)
        add('blit-count-past-region-rejected',22,1,BLIT_COMMANDS + 1)
        add('blit-count',22,1,2)
        add('blit-slot-rejected',22,2,2)
        add('blit-run',22,2,1)
        add('blit-busy',22,3)
        add('blit-busy-rejected',22,0,0)
        add('blit-not-completed',22,4)
        endpoint.blit_complete()
        add('blit-completed',22,4,event_before='blit-complete')
        add('blit-idle',22,3)
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
    if blitter:
        add('blit-quiesced-rejected',22,0,0)
    description = 'Synthetic wire oracle; not hardware acceptance.'
    if blitter:
        description += (' The run stages commands 0 and 1 into slot 1, which the submit then shows;'
                        ' they keep the blitter busy until the blit-complete event.')
    return dict(description=description,build_id=BUILD.hex(),
                capabilities=endpoint.capabilities,initial_request_toggle=False,exchanges=exchanges)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--check',action='store_true')
    args = parser.parse_args()
    for output, blitter in ((OUTPUT, False), (BLITTER_OUTPUT, True)):
        data = json.dumps(fixtures(blitter),indent=2)+'\n'
        if args.check:
            if not output.exists() or output.read_text() != data:
                parser.exit(1,'menu display fixtures are stale\n')
        else:
            output.parent.mkdir(parents=True,exist_ok=True)
            output.write_text(data)

if __name__ == '__main__': main()
