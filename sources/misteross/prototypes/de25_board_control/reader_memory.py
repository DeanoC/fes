"""Explicit diagnostic bus window; never a CPU mapping or DDR qualification."""
CONTROL_ABI = 0x10203040


def selection(backend, base, size):
    if backend not in ('onchip', 'hps-ddr', 'hps-ddr-window') or type(base) is not int or type(size) is not int:
        raise ValueError('reader memory requires an explicit backend and integer window')
    if backend == 'onchip':
        if base or size:
            raise ValueError('on-chip reader cannot declare a DDR window')
    elif not (0 <= base < 1 << 32 and 4096 <= size < 1 << 32 and
              not (base | size) & 4095 and base + size <= 1 << 32):
        raise ValueError('DDR reader requires a nonempty page-aligned 32-bit bus window')
    if backend == 'hps-ddr-window' and size != 4 << 20:
        raise ValueError('fixed 720p window reader requires a 4 MiB compiled bus window')
    return {'backend': backend, 'bus_base': base, 'bytes': size}


def checked(value):
    if not isinstance(value, dict) or set(value) != {'backend', 'bus_base', 'bytes'}:
        raise ValueError('reader memory selection is missing or malformed')
    return selection(value['backend'], value['bus_base'], value['bytes'])


def control_abi(value):
    return CONTROL_ABI ^ mode(value)


def mode(value):
    return {'onchip': 0, 'hps-ddr': 1, 'hps-ddr-window': 2}[checked(value)['backend']]


def parameters(value):
    value = checked(value)
    return {'reader_memory_backend': value['backend'], 'reader_memory_base': value['bus_base'],
            'reader_memory_bytes': value['bytes']}
