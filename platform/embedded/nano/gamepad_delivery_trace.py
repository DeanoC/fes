"""Validate reported owned key writes, not physical USB or emulation fidelity."""
import json
import re
from service_trace import _unique, number, require

PREFIX = b'FES_NATIVE_CORE_GAMEPAD '


def observation(raw):
    def invalid(_):
        raise ValueError('nonfinite gamepad JSON')
    require(isinstance(raw, bytes) and 0 < len(raw) <= 4096, 'bounded gamepad receipt required')
    r = json.loads(raw, object_pairs_hook=_unique, parse_constant=invalid)
    require(type(r) is dict and set(r) == {'scope', 'owner', 'generation', 'package_id', 'build_id', 'sequence', 'descriptor_sha256', 'report', 'state', 'mapping', 'before', 'rows'}, 'gamepad receipt fields differ')
    require(r['scope'] == 'nano-computer-gamepad-v1', 'gamepad scope differs')
    for key in ('owner', 'generation', 'sequence'):
        require(number(r[key], (1 << 64)-1) and r[key] > 0, 'invalid gamepad '+key)
    require(r['generation'] % 2 == 1, 'gamepad generation is not a play')
    for key, size in (('package_id', 64), ('build_id', 32), ('descriptor_sha256', 64)):
        require(type(r[key]) is str and re.fullmatch('[0-9a-f]{'+str(size)+'}', r[key]), 'invalid gamepad '+key)
    require(type(r['report']) is str and re.fullmatch('(?:[0-9a-f]{2}){1,64}', r['report']), 'invalid gamepad report bytes')
    s = r['state']
    require(type(s) is dict and set(s) == {'Axes', 'Buttons', 'Hat'}, 'gamepad state fields differ')
    require(type(s['Axes']) is list and len(s['Axes']) == 6 and all(type(a) is int and -32767 <= a <= 32767 for a in s['Axes']), 'invalid gamepad axes')
    require(number(s['Buttons'], (1 << 32)-1) and type(s['Hat']) is int and -1 <= s['Hat'] <= 7, 'invalid gamepad buttons/hat')
    m = r['mapping']
    require(type(m) is dict and set(m) == {'Up', 'Down', 'Left', 'Right', 'Buttons'}, 'gamepad mapping fields differ')
    require(type(m['Buttons']) is list and len(m['Buttons']) == 32, 'invalid gamepad button mapping')
    usages = [m[k] for k in ('Up', 'Down', 'Left', 'Right')] + m['Buttons']
    require(all(type(u) is int and (u == 0 or 4 <= u <= 127 or 224 <= u <= 231) for u in usages) and any(usages), 'invalid gamepad key usage')
    for key in ('before', 'rows'):
        require(type(r[key]) is list and len(r[key]) == 9 and all(number(v, 65535 if i < 8 else 255) for i, v in enumerate(r[key])), 'invalid gamepad key rows')
    directions = [s['Axes'][1] < -16384, s['Axes'][1] > 16384, s['Axes'][0] < -16384, s['Axes'][0] > 16384]
    h = s['Hat']
    if h >= 0:
        directions = [h == 7 or h <= 1, 3 <= h <= 5, 5 <= h <= 7, 1 <= h <= 3]
    pressed = directions + [bool(s['Buttons'] & (1 << n)) for n in range(32)]
    rows = [0]*9
    for usage, down in zip(usages, pressed):
        if usage and down:
            row, bit = (8, usage-224) if usage >= 224 else divmod(usage, 16)
            rows[row] |= 1 << bit
    require(rows == r['rows'] and rows != r['before'], 'gamepad rows differ from mapped state or did not change')
    return r


def validate_cycle(records, *, owner, generation, package_id, build_id, descriptor_sha256, mapping):
    """Require a complete, session-bound press/release chain from neutral.

    Raw-report decoding and USB provenance require separate source evidence.
    This function does not infer either from a receipt's normalized state.
    """
    require(type(records) is list and 2 <= len(records) <= 256, 'bounded press/release receipts required')
    previous = [0]*9
    pressed = False
    for sequence, raw in enumerate(records, 1):
        r = observation(raw)
        require(all(r[k] == v for k, v in dict(owner=owner, generation=generation, package_id=package_id, build_id=build_id, descriptor_sha256=descriptor_sha256, mapping=mapping, sequence=sequence).items()), 'gamepad session/source/mapping/sequence changed')
        require(r['before'] == previous, 'gamepad receipt chain has a gap')
        previous = r['rows']
        pressed |= any(previous)
    require(pressed and not any(previous), 'gamepad cycle lacks press or final release')
    return {'completed_writes': len(records), 'released': True, 'physical_input_verified': False}
