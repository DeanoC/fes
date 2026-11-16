"""Compile USB board readiness pins from the existing verified HPS handoff.

The result is a compiled-policy input, not runtime authority or physical proof.
The selecting boot producer must first verify the sealed project and context.
"""
import hashlib
import json
try:
    from . import hps_io
except ImportError:
    import hps_io


def derive(project, context):
    return {'format': 1, 'scope': 'nano-usb3320-board-policy-v1',
            'bus_clock_hz': 200000000, 'phy_clock_hz': 60000000,
            'phy_vendor': 0x424, 'phy_product': 7,
            'hps_io': hps_io.derive(project, context)}


def header(project, context):
    # Always re-derive from the compiled handoff; never accept an edited saved
    # descriptor as the source of runtime register expectations.
    policy = derive(project, context)
    raw = (json.dumps(policy, sort_keys=True, separators=(',', ':')) + '\n').encode()
    digest = hashlib.sha256(raw).digest()
    rows = policy['hps_io']['registers']
    return ('/* Generated from the compiled sealed HPS handoff; not physical qualification. */\n'
            '#ifndef FES_NANO_USB_BOARD_POLICY_H\n#define FES_NANO_USB_BOARD_POLICY_H\n'
            '#include "nano_usb_board.h"\n'
            '#if FES_NANO_USB_BUS_HZ != 200000000U || FES_NANO_USB_PHY_VENDOR != 0x424U || FES_NANO_USB_PHY_PRODUCT != 7U\n'
            '#error "USB board policy and implementation differ"\n#endif\n'
            '#define FES_NANO_USB_BOARD_PINS {' + ','.join(
                '{' + hex(r['offset']) + 'U,' + hex(r['value']) + 'U,0xffffffffU}' for r in rows) + '}\n'
            '#define FES_NANO_USB_BOARD_DIGEST {' + ','.join(hex(b) for b in digest) + '}\n'
            '#endif\n')
