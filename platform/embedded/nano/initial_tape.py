"""Explicit initial-media service selection; no physical ownership authority."""
import shlex


def checked(record, program_key='program'):
    selected = record.get('initial_tape', False)
    if type(selected) is not bool:
        raise ValueError('initial tape selection must be boolean')
    if not selected:
        return False
    if record.get(program_key) != 'fpgaload' or any(record.get(k) is not True for k in
            ('selected_programs', 'computer_reload', 'computer_storage_return',
             'tenfoot_serial', 'usb_control', 'usb_shared')):
        raise ValueError('initial tape requires selected shared-USB computer loads and owned SD return')
    if record.get('usb_input') not in ('boot-keyboard', 'gamepad') or any(
            record.get(k, False) is not False for k in
            ('window_memory', 'window_read', 'window_present', 'window_storage_return',
             'window_reload', 'window_service', 'menu_present', 'ddr_read', 'smmu_observe')):
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
