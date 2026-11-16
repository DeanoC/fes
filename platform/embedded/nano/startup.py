"""Explicit, source-bound startup policy for the dedicated native U-Boot."""
import hashlib
import importlib.util
import json
from pathlib import Path
import struct

ROOT=Path(__file__).resolve().parents[3]
_spec=importlib.util.spec_from_file_location('fes_startup_fdt',ROOT/'scripts/de25_boot_contract.py')
_fdt=importlib.util.module_from_spec(_spec);_spec.loader.exec_module(_fdt)


def policy(autoboot):
    if type(autoboot) is not bool:
        raise ValueError('native autoboot selection must be boolean')
    return {'scope': 'nano-native-startup-v1',
            'mode': 'autoboot' if autoboot else 'manual',
            'bootcommand': 'fesboot', 'bootdelay': 2 if autoboot else -1}


def checked(value):
    if not isinstance(value, dict) or value != policy(value.get('mode') == 'autoboot'):
        raise ValueError('explicit native startup policy required')
    if type(value.get('bootdelay')) is not int:
        raise ValueError('native startup delay must be an integer')
    return dict(value)


def select(autoboot, entry):
    value = policy(autoboot)
    if autoboot and (entry.get('native_program') != 'fpgaload' or
            any(entry.get(k) is not True for k in
                ('selected_programs', 'tenfoot_serial', 'computer_storage_return')) or
            not isinstance(entry.get('hps_menu_program'), dict)):
        raise ValueError('native autoboot requires the selected paired Tenfoot launcher')
    return value


def identity(sources, entry, bl31, value):
    return hashlib.sha256(json.dumps({'sources': sources, 'entry': entry, 'bl31': bl31,
        'startup': checked(value)}, sort_keys=True).encode()).hexdigest()[:16]


ENABLE = ('AUTOBOOT', 'USE_BOOTCOMMAND')
DISABLE = ('AUTOBOOT_KEYED', 'AUTOBOOT_USE_MENUKEY', 'AUTOBOOT_MENU_SHOW',
           'BOOT_RETRY', 'BOOTCOUNT_LIMIT')


def check_config(value, config):
    value = checked(value)
    required = {'CONFIG_BOOTCOMMAND': json.dumps(value['bootcommand']),
                'CONFIG_BOOTDELAY': str(value['bootdelay'])}
    required.update({'CONFIG_' + name: 'y' for name in ENABLE})
    for key, expected in required.items():
        if [s for s in config.splitlines() if s.startswith(key + '=')] != [key + '=' + expected]:
            raise ValueError('native startup configuration differs: ' + key)
    if any('CONFIG_' + name + '=y' in config.splitlines() for name in DISABLE):
        raise ValueError('native startup has an alternate boot or retry path')
    return value


def dts(value):
    value = checked(value)
    # U-Boot's /config override takes precedence over CONFIG_BOOTDELAY.
    return '\n/ { config { bootdelay = <0x%x>; }; };\n' % (value['bootdelay'] & 0xffffffff)


def check_dtb(value, path):
    value = checked(value)
    data=path.read_bytes()
    if not 40 <= len(data) <= 1 << 20:
        raise ValueError('native startup requires a bounded device tree')
    nodes,_=_fdt.parse_fdt(data)
    if nodes.get('/config',{}).get('bootdelay') != struct.pack('>I',value['bootdelay'] & 0xffffffff):
        raise ValueError('native device-tree startup delay differs')
