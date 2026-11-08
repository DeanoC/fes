"""Typed input diagnostics, separate from firmware and lifecycle receipts."""
import re

from service_trace import require

PREFIX = b'FES_NATIVE_INPUT_ACTION'
ACTION = re.compile(
    rb'FES_NATIVE_INPUT_ACTION source=(uart|usb-boot-keyboard|usb-gamepad) '
    rb'action=(select|stop|quit) state=(ready|loading|running|returning|parked|reboot_required) '
    rb'owner=([1-9a-f][0-9a-f]{0,15}) native_generation=(0|[1-9][0-9]{0,19})')


def record(raw, line, input_mode):
    match = ACTION.fullmatch(raw)
    require(match is not None, 'malformed input-action observation')
    source, action, state = (value.decode('ascii') for value in match.group(1, 2, 3))
    owner, generation = int(match[4], 16), int(match[5])
    require(owner > 0 and generation < 1 << 64, 'input-action ownership overflow or zero owner')
    require(input_mode in ('uart', 'uart+shared-usb-boot-keyboard', 'uart+shared-usb-gamepad') and
            (source == 'uart' or input_mode == 'uart+shared-'+source),
            'input-action source differs from selected input')
    return dict(source=source, action=action, state=state, owner=owner,
                native_generation=generation, line=line)


def validate(rows, op, owner, generation):
    # These diagnostics grant no permission and replace no existing receipt.
    # Require the complete ordered command sequence of the current UART test
    # controller, including Quit's Stop. Historical captures are not upgraded.
    require(op in ('startup', 'focus', 'launch', 'stop', 'quit'), 'unknown input-action operation')
    expected = {'launch': [('select', 'ready', generation)],
                'stop': [('stop', 'running', generation)],
                'quit': [('stop', 'running', generation), ('quit', 'ready', generation+1)],
                'startup': [], 'focus': []}[op]
    require(len(rows) == len(expected), 'input-action count differs from issued command')
    previous = 0
    for row, values in zip(rows, expected):
        require(row['source'] == 'uart', 'uncommanded USB lifecycle input')
        require(row['owner'] == owner and
                (row['action'], row['state'], row['native_generation']) == values,
                'input-action differs from issued command ownership/state')
        require(row['line'] > previous, 'input-action order regressed')
        previous = row['line']
