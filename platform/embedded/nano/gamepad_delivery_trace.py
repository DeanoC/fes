"""Validate reported owned key writes, not physical USB or emulation fidelity."""
import json
import re
import hashlib
import copy
from service_trace import _unique, number, require

PREFIX = b'FES_NATIVE_CORE_GAMEPAD '
CAPTURED_DESCRIPTOR_SHA256 = '171f93692f2d718af6c6e9fc5b7d8c2580ac33fa2e3044ee2393c07ec839dc11'


def decode_captured_report(descriptor, report):
    """Independent decoder for the one captured 98-byte joystick schema.

    Input bits 0..15 are unsigned X/Y, 16..43 are constant padding,
    44..53 are ten buttons and 54..63 are vendor bits. No report IDs or
    hat exist. A different descriptor requires a separately checked decoder.
    """
    require(type(descriptor) is bytes and len(descriptor) == 98 and hashlib.sha256(descriptor).hexdigest() == CAPTURED_DESCRIPTOR_SHA256,
            'captured joystick descriptor required')
    require(type(report) is bytes and len(report) == 8, 'captured joystick requires eight input bytes')
    return dict(Axes=[report[0]*65534//255-32767, report[1]*65534//255-32767, 0, 0, 0, 0],
                Buttons=(int.from_bytes(report, 'little') >> 44) & 1023, Hat=-1)


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


def validate_captured_cycle(records, *, descriptor, pressed_rows, **expected):
    result = validate_cycle(records, **expected)
    require(expected['descriptor_sha256'] == CAPTURED_DESCRIPTOR_SHA256, 'source descriptor differs from captured joystick')
    require(type(pressed_rows) is list and len(pressed_rows) == 9 and any(pressed_rows) and
            all(number(v, 65535 if i < 8 else 255) for i, v in enumerate(pressed_rows)), 'explicit expected pressed rows required')
    require(len(records) == 2, 'one exact press and release required')
    for index, raw in enumerate(records):
        r = observation(raw)
        require(decode_captured_report(descriptor, bytes.fromhex(r['report'])) == r['state'], 'raw joystick bytes differ from reported normalized state')
        require(r['rows'] == (pressed_rows if index == 0 else [0]*9), 'unexpected control or incomplete release')
    return dict(result, descriptor_specific_report_decoded=True)


def observe_captured_cycle(client, receive, *, descriptor, mapping, pressed_rows, bound_seconds=60):
    """Receive one press/release during an already verified running play.

    receive(bound, done) must only read, return complete captured bytes and
    honor the bound. This helper sends no UART command or lifecycle request.
    A timeout, source error or changed ownership retires the client once.
    Software callers can synthesize bytes, so this alone is not hardware proof.
    """
    from tenfoot_trace import PREFIX as APP_PREFIX, MAX_BYTES, quiet
    require(callable(receive) and number(bound_seconds, 60) and bound_seconds > 0, 'bounded input receive hook required')
    require(not client.retired and client.usb is not None and client.usb.policy['input'] == 'gamepad', 'live shared gamepad App required')
    view = copy.deepcopy(client.view)
    native = view['native']
    require(native['State'] == 'running' and not native['Held'] and view['session'] == 'active', 'input observation requires a running play')
    require(hashlib.sha256(descriptor).hexdigest() == client.usb.policy['descriptor_sha256'] == CAPTURED_DESCRIPTOR_SHA256,
            'live USB descriptor differs from independent decoder')
    pressed_rows = copy.deepcopy(pressed_rows)
    require(client.active.acquire(blocking=False), 'another App observation is active')
    expected = dict(owner=client.owner, generation=native['Generation'], package_id=native['Identity']['PackageID'],
                    build_id=native['Identity']['BuildID'], descriptor_sha256=client.usb.policy['descriptor_sha256'], mapping=copy.deepcopy(mapping))
    def records(data):
        require(type(data) is bytes and len(data) <= MAX_BYTES, 'bounded input capture required')
        values = []
        for line in data.splitlines():
            if line.startswith(PREFIX):
                raw = line[len(PREFIX):]
                r = observation(raw)
                require(all(r[k] == v for k, v in expected.items()), 'input belongs to another source or play')
                require(r['sequence'] == len(values)+1, 'input receipt sequence changed')
                require(decode_captured_report(descriptor, bytes.fromhex(r['report'])) == r['state'], 'raw joystick bytes differ from reported normalized state')
                require(r['before'] == ([0]*9 if not values else pressed_rows), 'input receipt chain changed')
                require(r['rows'] == (pressed_rows if not values else [0]*9), 'unexpected control or incomplete release')
                values.append(raw)
                require(len(values) <= 2, 'extra input after release')
            elif line.startswith(APP_PREFIX):
                def invalid(_): raise ValueError('nonfinite cached App JSON')
                require(json.loads(line[len(APP_PREFIX):], object_pairs_hook=_unique, parse_constant=invalid) == view, 'App changed during passive input observation')
            else:
                require(quiet(line), 'unexpected firmware or input observation')
        return values
    try:
        require(client.owns(), 'input console claim lost')
        before = client.now()
        def done(data):
            require(client.owns() and client.view == view and not client.retired, 'input ownership changed while waiting')
            require(type(data) is bytes and len(data) <= MAX_BYTES, 'bounded input capture required')
            if not data.endswith(b'\n'):
                return False
            return len(records(data)) == 2
        raw = receive(bound_seconds, done)
        after = client.now()
        require(before <= after < before+bound_seconds and client.owns() and client.view == view and not client.retired, 'input deadline/clock/ownership failed')
        require(type(raw) is bytes and raw.endswith(b'\n'), 'incomplete input capture')
        values = records(raw)
        result = validate_captured_cycle(values, descriptor=descriptor, pressed_rows=pressed_rows, **expected)
        return dict(result, scope='nano-gamepad-input-cycle-v1', **expected,
                    records=[json.loads(v) for v in values], elapsed_seconds=after-before, bound_seconds=bound_seconds)
    except BaseException:
        client.retired = True
        raise
    finally:
        client.active.release()
