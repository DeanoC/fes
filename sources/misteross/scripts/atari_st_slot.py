"""Original ST expansion connector, frozen placement, and shared PLL scaffold repair."""
import json
from dataclasses import dataclass

LAYOUT = 'fes.atari-st-bus.socket/1'
INTERFACE = 'fes.expansion.atari-st-bus'
REQUEST_BITS = 56
RESPONSE_BITS = 32
COLUMN = 24

@dataclass(frozen=True)
class Socket:
    slot: int = 1
    first_row: int = 1
    last_row: int = 18
    cram: tuple[int, int, int, int] = (1769, 32, 2806, 1722)
    region: str = 'expansion'
    instance: str = 'expansion.'
    placement: str = 'expansion 24 1 28 18'

SOCKETS = (Socket(),)

def boundary_bels(socket: Socket = SOCKETS[0]) -> dict[str, str]:
    result = {}
    for offset in range(REQUEST_BITS + RESPONSE_BITS):
        bit = offset if offset < REQUEST_BITS else offset - REQUEST_BITS
        name = f'plug_request_ff_{bit}' if offset < REQUEST_BITS else f'plug_response_ff_{bit}'
        row, index = socket.first_row + offset // 20, offset % 20
        z = (index // 2) * 6 + (4 if index % 2 else 2)
        result[name] = f'MISTRAL_FF.{COLUMN}.{row}.{z}'
    anchors = ([f'MISTRAL_FF.{COLUMN}.{row}.56' for row in range(socket.first_row + 5, socket.last_row + 1)] +
               [f'MISTRAL_FF.{COLUMN+4}.{row}.56' for row in range(socket.first_row, socket.last_row + 1)])
    result.update({f'clock_coverage_ff_{i}': bel for i, bel in enumerate(anchors)})
    return result


def _pll_object(value, context: str) -> dict:
    if not isinstance(value, dict):
        raise ValueError(f'malformed {context}')
    return value


def _pll_net_bits(netnames: dict, name: str) -> list:
    alias = _pll_object(netnames.get(name), f'ST system PLL net alias: {name}')
    bits = alias.get('bits')
    if not isinstance(bits, list) or len(bits) != 1 or type(bits[0]) is not int or bits[0] < 0:
        raise ValueError(f'ST system PLL net {name} must have 1 valid bit')
    return bits


def repair_split_system_pll(top: dict) -> None:
    """Reattach or accept the split second system PLL output.

    Legacy routed JSON omits ``outclk[1]`` and records a six-pin map whose
    ``outclk[0]`` and ``outclk[1]`` entries alias themselves. That shell is
    repaired onto ``system_clock.pll_outclk_1`` and the obsolete alias is
    removed. Shared-pin nextpnr JSON already routes ``outclk[1]`` and carries
    the physical five-pin map: scalar ``outclk`` is bit 0 at cell pin C6 and
    ``outclk[1]`` is cell pin C7. That cell is preserved when its ``outclk[1]``
    connection already equals ``system_clock.pll_outclk_1`` and the port
    directions are output, output, input and output for ``outclk``,
    ``outclk[1]``, ``refclk`` and ``locked``. A missing, mismatched or
    input-direction physical output is rejected. Any other connection set or
    pin map is rejected.
    """
    top = _pll_object(top, 'ST system PLL top module')
    cells = _pll_object(top.get('cells'), 'ST system PLL cells')
    netnames = _pll_object(top.get('netnames'), 'ST system PLL netnames')
    pll = _pll_object(cells.get('system_clock.pll'), 'ST system PLL')
    connections = _pll_object(pll.get('connections'), 'ST system PLL connections')
    attributes = _pll_object(pll.get('attributes'), 'ST system PLL attributes')
    directions = _pll_object(pll.get('port_directions'), 'ST system PLL port directions')
    legacy = {'outclk', 'refclk', 'locked'}
    if pll.get('type') != 'altera_pll' or set(connections) not in (legacy, legacy | {'outclk[1]'}):
        raise ValueError('ST system PLL connection contract changed')
    encoded = attributes.get('FES_PINMAP_V1')
    if not isinstance(encoded, str):
        raise ValueError('ST system PLL frozen pin map is missing')
    mapping = _pll_object(json.loads(bytes.fromhex(encoded).decode()), 'ST system PLL pin map')
    pins = _pll_object(mapping.get('pins'), 'ST system PLL mapped pins')
    aliases = {f'outclk[{bit}]': [0, f'outclk[{bit}]'] for bit in range(2)}
    # nextpnr's shared-pin shell: scalar outclk is bit 0 (C6), outclk[1] is C7.
    physical_pins = {'locked': [0, 'locked'], 'outclk': [0, 'C6'], 'outclk[1]': [0, 'C7'],
                     'refclk': [0, 'refclk'], 'rst': [0, 'rst']}
    physical = (set(mapping) == {'count', 'pins'} and type(mapping.get('count')) is int and
                mapping['count'] == 5 and pins == physical_pins and
                all(type(entry[0]) is int and type(entry[1]) is str for entry in pins.values()))
    legacy_map = (type(mapping.get('count')) is int and mapping['count'] == 6 and len(pins) == 6 and
                  all(pins.get(name) == value for name, value in aliases.items()))
    if not physical and not legacy_map:
        raise ValueError('ST system PLL frozen pin map changed')
    # A five-pin map names C7. The routed outclk[1] connection and port
    # directions have to match before the six-pin form is rewritten.
    physical_directions = {'outclk': 'output', 'outclk[1]': 'output',
                           'refclk': 'input', 'locked': 'output'}
    if physical and directions != physical_directions:
        raise ValueError('ST system PLL physical port directions changed')
    output1 = _pll_net_bits(netnames, 'system_clock.pll_outclk_1')
    clock1 = _pll_net_bits(netnames, 'system_clock.clocks[1]')
    buffers = []
    for cell in cells.values():
        if not isinstance(cell, dict) or cell.get('type') != 'MISTRAL_CLKBUF':
            continue
        buffer_connections = _pll_object(cell.get('connections'), 'ST system PLL clock buffer connections')
        if buffer_connections.get('Q') == clock1:
            buffers.append(cell)
    if len(buffers) != 1 or buffers[0]['connections'].get('A') != output1:
        raise ValueError('ST frozen audio PLL net changed')
    if physical:
        if connections.get('outclk[1]') != output1:
            raise ValueError('ST frozen audio PLL connection changed')
        return
    connections['outclk[1]'] = output1
    directions['outclk[1]'] = 'output'
    del pins['outclk[0]']
    mapping['count'] = len(pins)
    attributes['FES_PINMAP_V1'] = json.dumps(mapping, sort_keys=True).encode().hex()
