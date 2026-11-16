#!/usr/bin/env python3
"""Inventory governor classification keys in the i386 lockstep scope.

A key is a conservative classifier input, not proof that an encoding is legal
or that its timing was measured. In particular, the key expands fields that
some opcodes ignore. Every row is unmeasured until a separately reviewed
measurement ties the precise form and context to evidence. The 286 reference
cannot run the explicitly post-286 two-byte opcode families below; other
families still need a manual architecture/encoding audit.

  python3 scripts/i386_governor_inventory.py --output /tmp/inventory.json
The inventory is an audit and sampling aid, not a release or seal gate.
Its cross-product contains illegal and ignored-field combinations; it does not
require measuring every key.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_i386_governor as governor
from scripts import test_fes_i386_lockstep as lockstep

# Documented 286 two-byte instruction families in this scope: group 6 and 7,
# LAR, LSL and CLTS. Inclusion does NOT assert that every group selector,
# operand or privilege combination is legal; those are unclassified below.
POSSIBLE_286_0F = frozenset({0x00, 0x01, 0x02, 0x03, 0x06})


def unpack(key: int) -> tuple[int, int, int, int, int, int]:
    """Decode the generator's packed classifier key in KEY_FIELDS order."""
    fields = []
    for _, width in reversed(governor.KEY_FIELDS):
        fields.append(key & ((1 << width) - 1))
        key >>= width
    if key:
        raise ValueError('key exceeds governor key width')
    return tuple(reversed(fields))  # type: ignore[return-value]


def inventory() -> dict:
    table = governor.load()
    members: dict[int, list[str]] = {}
    for stem in sorted(lockstep.SCOPE):
        for key in governor.stem_keys(stem):
            members.setdefault(key, []).append(stem)
    rows = []
    for key, stems in sorted(members.items()):
        two, op, reg, rm_reg, o32, rep = unpack(key)
        cls = governor.classify(table, two, op, reg, rm_reg, o32, rep)
        reference = 'not_286_opcode' if two and op not in POSSIBLE_286_0F else 'legality_unclassified'
        rows.append({
            'key': f'{key:0{(governor.KEY_BITS + 3) // 4}x}',
            'opcode': ('0F' if two else '') + f'{op:02X}',
            'modrm_reg': reg,
            'register_form': bool(rm_reg),
            'operand32_flag': bool(o32),
            'rep_flag': bool(rep),
            'scope_stems': stems,
            'governor_class': cls.name,
            'reference_286': reference,
            'timing_status': 'unmeasured',
            'evidence': [],
        })
    return {
        'format': 1,
        'description': 'Conservative scope/classifier cross-product; keys may be ignored or illegal for an opcode.',
        'counts': {
            'scope_stems': len(lockstep.SCOPE),
            'governor_classes': len(table.classes),
            'classifier_keys': len(rows),
            'not_286_opcode_keys': sum(row['reference_286'] == 'not_286_opcode' for row in rows),
            'timing_unmeasured_keys': len(rows),
        },
        'additional_context_not_encoded_by_key': [
            'operand byte versus word for opcodes whose key does not distinguish them',
            'legal ModRM/SIB/address displacement and alignment',
            'prefix type, repetition and order',
            'branch outcome and preceding instruction/queue state',
            'data-bus width, wait states, I/O and MMIO access',
            'REP count, termination, fault, interrupt and replay',
            'mode, privilege, exception and architectural control transfer',
        ],
        'rows': rows,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--output', type=Path, help='write the inventory to this file instead of stdout')
    args = parser.parse_args()
    result = inventory()
    text = json.dumps(result, indent=2, sort_keys=True) + '\n'
    if args.output is None:
        sys.stdout.write(text)
    else:
        args.output.write_text(text)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
