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


# Current nextpnr: scalar outclk is bit 0 at C6, outclk[1] is C7.
PHYSICAL_PINS = {'locked': [0, 'locked'], 'outclk': [0, 'C6'], 'outclk[1]': [0, 'C7'],
                 'refclk': [0, 'refclk'], 'rst': [0, 'rst']}
PHYSICAL_DIRECTIONS = {'outclk': 'output', 'outclk[1]': 'output',
                       'refclk': 'input', 'locked': 'output'}
_LEGACY_CONNECTIONS = frozenset({'outclk', 'refclk', 'locked'})
_CURRENT_CONNECTIONS = _LEGACY_CONNECTIONS | {'outclk[1]'}


def _load_system_pll(top: dict) -> dict:
    """Parse the system PLL. A map that is neither current nor legacy is rejected."""
    top = _pll_object(top, 'ST system PLL top module')
    cells = _pll_object(top.get('cells'), 'ST system PLL cells')
    netnames = _pll_object(top.get('netnames'), 'ST system PLL netnames')
    pll = _pll_object(cells.get('system_clock.pll'), 'ST system PLL')
    connections = _pll_object(pll.get('connections'), 'ST system PLL connections')
    attributes = _pll_object(pll.get('attributes'), 'ST system PLL attributes')
    directions = _pll_object(pll.get('port_directions'), 'ST system PLL port directions')
    if pll.get('type') != 'altera_pll' or set(connections) not in (_LEGACY_CONNECTIONS, _CURRENT_CONNECTIONS):
        raise ValueError('ST system PLL connection contract changed')
    encoded = attributes.get('FES_PINMAP_V1')
    if not isinstance(encoded, str):
        raise ValueError('ST system PLL frozen pin map is missing')
    mapping = _pll_object(json.loads(bytes.fromhex(encoded).decode()), 'ST system PLL pin map')
    pins = _pll_object(mapping.get('pins'), 'ST system PLL mapped pins')
    aliases = {f'outclk[{bit}]': [0, f'outclk[{bit}]'] for bit in range(2)}
    physical = (set(mapping) == {'count', 'pins'} and type(mapping.get('count')) is int and
                mapping['count'] == 5 and pins == PHYSICAL_PINS and
                all(type(entry[0]) is int and type(entry[1]) is str for entry in pins.values()))
    legacy_map = (type(mapping.get('count')) is int and mapping['count'] == 6 and len(pins) == 6 and
                  all(pins.get(name) == value for name, value in aliases.items()))
    if physical:
        kind = 'physical'
    elif legacy_map:
        kind = 'legacy'
    else:
        raise ValueError('ST system PLL frozen pin map changed')
    return {'cells': cells, 'netnames': netnames, 'connections': connections, 'attributes': attributes,
            'directions': directions, 'mapping': mapping, 'pins': pins, 'kind': kind}


def _require_audio_clock_branch(cells: dict, netnames: dict) -> list:
    """The one buffer from ``pll_outclk_1`` onto ``system_clock.clocks[1]``."""
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
    return output1


def validate_current_system_pll(top: dict) -> None:
    """Admit the current compiler's system PLL. The cell is not rewritten.

    A current shell connects ``outclk``, ``outclk[1]``, ``refclk`` and
    ``locked``, drives ``system_clock.clocks[1]`` from
    ``system_clock.pll_outclk_1`` through one clock buffer, and uses output,
    output, input and output directions for ``outclk``, ``outclk[1]``,
    ``refclk`` and ``locked``. Its physical pin map puts scalar ``outclk`` at
    C6 and ``outclk[1]`` at C7. ``outclk[1]`` must already be that audio net.
    A legacy six-pin map is not a current shell.
    """
    loaded = _load_system_pll(top)
    if loaded['kind'] != 'physical':
        raise ValueError('ST system PLL frozen pin map changed')
    if loaded['directions'] != PHYSICAL_DIRECTIONS:
        raise ValueError('ST system PLL physical port directions changed')
    output1 = _require_audio_clock_branch(loaded['cells'], loaded['netnames'])
    if loaded['connections'].get('outclk[1]') != output1:
        raise ValueError('ST frozen audio PLL connection changed')


def repair_split_system_pll(top: dict) -> None:
    """Repair a legacy six-pin system PLL onto the frozen audio net.

    Legacy routed JSON omits a connected ``outclk[1]`` and records a six-pin
    map whose ``outclk[0]`` and ``outclk[1]`` entries alias themselves. That
    shell is rewritten onto ``system_clock.pll_outclk_1`` and the obsolete
    alias is removed. A current-compiler physical map is not repaired here;
    ``validate_current_system_pll`` admits that shell without rewriting it.
    """
    loaded = _load_system_pll(top)
    if loaded['kind'] != 'legacy':
        raise ValueError('ST system PLL legacy repair requires the six-pin map')
    output1 = _require_audio_clock_branch(loaded['cells'], loaded['netnames'])
    loaded['connections']['outclk[1]'] = output1
    loaded['directions']['outclk[1]'] = 'output'
    del loaded['pins']['outclk[0]']
    loaded['mapping']['count'] = len(loaded['pins'])
    loaded['attributes']['FES_PINMAP_V1'] = json.dumps(loaded['mapping'], sort_keys=True).encode().hex()


def adopt_system_pll(top: dict) -> None:
    """Admit a current shell, or repair a legacy six-pin map.

    Current-shell admission checks the PLL connections, audio clock branch,
    port directions and physical pin map, and does not rewrite the cell.
    Legacy-map repair is the only path that attaches ``outclk[1]``.
    """
    if _load_system_pll(top)['kind'] == 'physical':
        validate_current_system_pll(top)
        return
    repair_split_system_pll(top)
