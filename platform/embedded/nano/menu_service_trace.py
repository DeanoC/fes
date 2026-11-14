"""Strict menu-service evidence only; caller must independently admit its source.

This parser grants no memory, bridge, package, boot or hardware acceptance.
The hardware operator must separately check the matched build and join the
computer/USB/boot trace. In particular, two menus alone do not prove a core ran.
"""
import copy
import json
import re

from service_trace import _unique, number, require

PREFIX = b'FES_NATIVE_MENU_STATE '
MAX_LINE = 4096
MAX_RECORDS = 512
MAX_U64 = (1 << 64) - 1
IDENTITY = {'PackageID', 'CoreID', 'ABI', 'BuildID', 'Platform', 'Device',
            'ProgrammingProfile', 'ActivationReceiptSHA256', 'ProgrammedSHA256', 'Major', 'Minor'}
HASHES = {'source_sha256', 'memory_sha256', 'control_sha256', 'access_sha256'}
CAPABILITIES = dict(Mask=15, ControlMask=2, HardwareDrainMask=12, FabricHandshake=1)


def digest(value, size=64):
    return type(value) is str and re.fullmatch('[0-9a-f]{'+str(size)+'}', value) and int(value, 16) > 0


class MenuTrace:
    def __init__(self, contract):
        require(type(contract) is dict and set(contract) == HASHES | {'identity', 'owner'},
                'independently checked complete menu source contract required')
        require(all(digest(contract[k]) for k in HASHES) and number(contract['owner'], MAX_U64)
                and contract['owner'] > 0, 'invalid menu source or owner')
        identity = contract['identity']
        require(type(identity) is dict and set(identity) == IDENTITY, 'complete selected menu identity required')
        require(all(type(identity[k]) is str and len(identity[k]) <= 256 for k in IDENTITY - {'Major', 'Minor'})
                and all(type(identity[k]) is int for k in ('Major', 'Minor')), 'invalid menu identity types')
        require(identity['Platform'] == 'de25_nano' and identity['Device'] == 'A5EB013BB23BE4SCS'
                and identity['ABI'] == 'fes.de25-embedded-diagnostic' and identity['Major'] == 1
                and identity['Minor'] == 0 and identity['ProgrammingProfile'] == 'de25-embedded-hps-first-menu-prototype-v1'
                and identity['ActivationReceiptSHA256'] == '' and identity['CoreID']
                and digest(identity['PackageID']) and digest(identity['BuildID'], 32)
                and digest(identity['ProgrammedSHA256']), 'unsupported selected menu identity')
        self._contract = copy.deepcopy(contract)
        self._rows = []
        self._menu = 0
        self._opened = None
        self._last = None
        self._return = None
        self._retired = False

    def _bridge(self, value, generation, reset, contained):
        require(type(value) is dict and set(value) == {'Capabilities', 'Owner', 'Generation',
                'ResetMask', 'NeverEnabled', 'MastersContained'}, 'incomplete menu bridge observation')
        require(type(value['Capabilities']) is dict and value['Capabilities'] == CAPABILITIES
                and all(type(v) is int for v in value['Capabilities'].values()), 'menu bridge capabilities differ')
        require(type(value['Owner']) is int and value['Owner'] == self._contract['owner']
                and type(value['Generation']) is int and value['Generation'] == generation
                and type(value['ResetMask']) is int and value['ResetMask'] == reset
                and value['NeverEnabled'] is False and value['MastersContained'] is contained,
                'menu bridge source, generation or containment differs')

    def feed(self, line):
        require(not self._retired, 'menu trace retired after a failed observation')
        try:
            return self._feed(line)
        except (ValueError, TypeError, KeyError, OverflowError, RecursionError) as error:
            self._retired = True
            raise ValueError('invalid menu evidence: '+str(error)) from error

    def _feed(self, line):
        require(type(line) is bytes and len(line) <= MAX_LINE and line.startswith(PREFIX)
                and line.endswith(b'\n') and b'\n' not in line[:-1], 'bounded complete menu line required')
        def invalid(_):
            raise ValueError('nonfinite menu number')
        row = json.loads(line[len(PREFIX):], object_pairs_hook=_unique, parse_constant=invalid)
        require(type(row) is dict and set(row) == HASHES | {'scope', 'event', 'menu', 'identity',
                'source_generation', 'opened', 'after', 'frames', 'frame'}, 'menu fields differ')
        require(row['scope'] == 'nano-program-menu-v1' and row['event'] in ('frame', 'return'), 'menu scope/event differs')
        require(type(row['identity']) is dict and row['identity'] == self._contract['identity']
                and all(type(row['identity'][k]) is int for k in ('Major', 'Minor'))
                and all(row[k] == self._contract[k] for k in HASHES), 'menu identity or source hashes differ')
        require(number(row['source_generation'], MAX_U64-3) and row['source_generation'] > 0
                and number(row['menu'], MAX_U64) and row['menu'] > 0
                and number(row['frames'], (1 << 32)-1) and row['frames'] > 0, 'menu counters out of bounds')
        self._bridge(row['opened'], row['source_generation']+2, 5, False)
        frame = row['frame']
        require(type(frame) is dict and set(frame) == {'Generation', 'Sequence', 'Submitted', 'Slot', 'Underflows'}
                and all(type(v) is int for v in frame.values()), 'incomplete menu frame')
        require(frame == dict(Generation=1, Sequence=row['frames'], Submitted=row['frames'],
                Slot=(row['frames']-1) % 2, Underflows=0), 'incomplete, crossed or underflowed menu frame')
        if row['event'] == 'frame':
            require(row['after'] == row['opened'], 'frame manufactured a return')
            self._bridge(row['after'], row['source_generation']+2, 5, False)
            if self._opened is None:
                require(row['menu'] == self._menu+1 and row['frames'] == 1, 'menu opening skipped or replayed')
                if self._return is not None:
                    require(row['source_generation'] >= self._return['Generation']+3, 'menu omitted owned SD return/containment')
            else:
                require(row['menu'] == self._menu and row['opened'] == self._opened
                        and row['frames'] == self._last['frames']+1, 'menu frame skipped, replayed or changed owner')
            self._menu, self._opened = row['menu'], copy.deepcopy(row['opened'])
        else:
            require(self._opened is not None and row['menu'] == self._menu and row['opened'] == self._opened
                    and row['frames'] == self._last['frames'] and row['frame'] == self._last['frame'],
                    'return has no matching displayed menu')
            self._bridge(row['after'], row['source_generation']+3, 15, True)
            self._return, self._opened = copy.deepcopy(row['after']), None
        require(len(self._rows) < MAX_RECORDS, 'menu evidence count exceeds bound')
        self._last = copy.deepcopy(row)
        self._rows.append(copy.deepcopy(row))
        return copy.deepcopy(row)

    def finish(self):
        require(not self._retired and self._rows and self._opened is None, 'menu evidence lacks a final verified return')
        return dict(scope='menu-service-evidence-only', menu_sessions=self._menu,
                    observations=len(self._rows), final_return=copy.deepcopy(self._return),
                    boot_verified=False, core_cycle_verified=False, physical_display_verified=False)
