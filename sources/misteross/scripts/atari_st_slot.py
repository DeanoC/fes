"""Original ST's wide registered expansion connector and frozen placement."""
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
