"""Explicit initial-media service selection; no physical ownership authority."""
import shlex
import hashlib
import json

CPU_MAPPING = dict(format=2, scope='pinned-tamago-el1-resources-excluded-v2',
                   ram_base=0x84000000, ram_bytes=256 << 20,
                   control_base=0x20010400, control_bytes=256,
                   dma_base=0x94800000, dma_bytes=8 << 20,
                   reader_base=0x95000000, reader_bytes=8 << 20,
                   firmware_base=0x50000, firmware_bytes=0x10000)

MEMORY = dict(format=1, scope='nano-independent-initial-tape-memory-v1',
              base=0x95800000, bytes=2 << 20, payload_bytes=0x4000,
              metadata_base=0x959ff000, metadata_bytes=4096,
              mapping='normal-uncached-XN', lifetime='pinned-until-reboot',
              configuration_reused=False, physical_authority=False)


def memory_digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def checked_memory(record):
    value = record.get('initial_tape_memory')
    digest = record.get('initial_tape_memory_sha256')
    if record.get('initial_tape', False) is not True:
        if value is not None or digest is not None:
            raise ValueError('unselected initial tape cannot reserve storage')
        return None
    if (json.dumps(value, sort_keys=True) != json.dumps(MEMORY, sort_keys=True) or
            digest != memory_digest(MEMORY) or
            json.dumps(record.get('cpu_mapping'), sort_keys=True) != json.dumps(CPU_MAPPING, sort_keys=True)):
        raise ValueError('initial tape requires its independent source-bound memory contract')
    return dict(MEMORY)


def checked(record, program_key='program'):
    selected = record.get('initial_tape', False)
    if type(selected) is not bool:
        raise ValueError('initial tape selection must be boolean')
    checked_memory(record)
    if not selected:
        return False
    if record.get(program_key) != 'fpgaload' or any(record.get(k) is not True for k in
            ('selected_programs', 'computer_reload', 'computer_storage_return',
             'tenfoot_serial', 'usb_control', 'usb_shared')):
        raise ValueError('initial tape requires selected shared-USB computer loads and owned SD return')
    if record.get('usb_input') not in ('boot-keyboard', 'gamepad') or any(
            record.get(k, False) is not False for k in
            ('window_memory', 'window_read', 'window_present', 'window_storage_return',
             'window_reload', 'menu_present', 'ddr_read', 'smmu_observe')):
        raise ValueError('initial tape cannot inherit another memory/observation mode')
    context = record.get('hps_context')
    computer = record.get('hps_computer')
    rom = record.get('hps_rom')
    arena = record.get('hps_configuration')
    if (not isinstance(context, dict) or context.get('programming_profile') != 'de25-native-computer-v1' or
            not isinstance(computer, dict) or any(type(computer.get(k)) is not int or computer[k] != v
                for k,v in {'cpu_base':0x20010300, 'span':32, 'abi_minor':0, 'capabilities':0x803}.items()) or
            not isinstance(rom, dict) or type(rom.get('bytes')) is not int or rom['bytes'] not in (4096,8192) or
            rom.get('cpu_base') != 0x20010500 or rom.get('span') != 128 or
            not isinstance(arena, dict) or arena.get('base') != 0x94800000 or arena.get('size') != 8<<20):
        raise ValueError('initial tape requires the complete native held-ROM computer and fixed arena')
    return True


def linked(record):
    selected = checked(record)
    command = record.get('command', [])
    try:
        words = shlex.split(command[command.index('-ldflags')+1])
    except (ValueError, IndexError, TypeError, AttributeError):
        raise ValueError('initial tape requires the native link command') from None
    options = [(i,w) for i,w in enumerate(words) if 'main.expectedInitialTape' in w]
    if selected:
        if len(options) != 1 or options[0][1] != 'main.expectedInitialTape=1' or options[0][0] == 0 or words[options[0][0]-1] != '-X':
            raise ValueError('initial tape differs from native link assignment')
    elif options:
        raise ValueError('unselected initial tape appears in native link command')
    return selected


# Geometry is not physical ownership, a live mapping proof or activation.
def staging_layout(cpu_mapping, boot_reserved, entry_image):
    expected = CPU_MAPPING
    if json.dumps(cpu_mapping, sort_keys=True) != json.dumps(expected, sort_keys=True):
        raise ValueError('independent tape requires the complete paired-program resource layout')
    if not isinstance(boot_reserved, list):
        raise ValueError('complete boot reservations required')
    protected = [(expected[a], expected[b]) for a, b in (
        ('ram_base', 'ram_bytes'), ('control_base', 'control_bytes'),
        ('dma_base', 'dma_bytes'), ('reader_base', 'reader_bytes'),
        ('firmware_base', 'firmware_bytes'))]
    required = {(0, 0x80000), (0x80000000, 32 << 20),
                (0x82000000, 32 << 20), (0xb0000000, 256 << 20)}
    observed = set()
    for row in boot_reserved:
        if not isinstance(row, dict) or any(type(row.get(k)) is not int for k in ('start', 'size')):
            raise ValueError('integer boot reservation required')
        start, size = row['start'], row['size']
        if start < 0 or size <= 0 or start > (1 << 64) - size:
            raise ValueError('bounded boot reservation required')
        observed.add((start, size))
        protected.append((start, size))
    if not required <= observed:
        raise ValueError('original boot reservations cannot be omitted')
    if (not isinstance(entry_image, dict) or set(entry_image) != {'base', 'bytes'} or
            any(type(entry_image[k]) is not int for k in entry_image) or
            entry_image['base'] != 0x96080000 or not 0 < entry_image['bytes'] <= 16 << 20):
        raise ValueError('complete fixed native entry envelope required')
    protected += [(entry_image['base'], entry_image['bytes']), (0x99000000, 16 << 20)]
    base, size = 0x95800000, 2 << 20
    if any(base < first + length and first < base + size for first, length in protected):
        raise ValueError('independent tape reservation overlaps a live or boot resource')
    return dict(MEMORY)
