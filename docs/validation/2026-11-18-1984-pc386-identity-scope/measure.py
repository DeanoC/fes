#!/usr/bin/env python3
"""Measure one pc386 checkout for counterexamples.py: its design identity under
design-v1 (the producer's own create_build_record, tool-free) and under the
design-v2 prototype, its evidence digest, and the bytes the compilers would
read (the Yosys program, top.qsf and pc386-rom.hex).

  python3 -I measure.py <sources/misteross> <rom pin sha256 or ''>

Needs build/firmware/pc386/pc386-bios.bin and pc386-vgabios.bin in the checkout.
"""
import hashlib
import json
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
root = Path(sys.argv[1]).absolute()
pin = sys.argv[2] or None
sys.path[:0] = [str(root), str(root / 'cores/fes-pc386/tools'), str(HERE)]

import build_pc386_product as product  # noqa: E402  (imports the legacy sealer and its imports)
from scripts import build_fes_pc386_quartus as legacy  # noqa: E402
from scripts.input_identity import input_role  # noqa: E402
import proposal  # noqa: E402


def sha(text):
    return hashlib.sha256(text.encode() if isinstance(text, str) else text).hexdigest()


record = product.create_build_record(root, 'https://github.com/DeanoC/fes', '0' * 40,
                                     {'yosys': 'selection-only', 'quartus': 'selection-only'}, 1, 'HIP',
                                     execution={'toolchain': 'quartus', 'selection_only': True})
fields = json.loads(record)
images = {n: root / 'build/firmware/pc386' / product.firmware.IMAGES[n]['file'] for n in product.firmware.IMAGES}
rom = product.firmware.rom_hex(images)
qsf = product.project_qsf(root, root / product.OUTPUT / 'project', '0' * 32, 1,
                          threads=product.selected_recipe(root)['quartus']['threads'])
rtl = (root / 'cores/fes-pc386/rtl/pc386_rom.sv').read_text()
rtl_rom_path = re.search(r'parameter ROM_HEX = "([^"]+)"', rtl).group(1)
# The seal's current ROM step and check (build_fes_pc386_quartus.firmware_rom), run as the seal runs it.
target = root / rtl_rom_path
if target.exists():
    target.unlink()
try:
    written = product.firmware_rom(root)
    current_check = 'pass'
except Exception as exc:  # noqa: BLE001 - report whatever the seal's check raises
    written, current_check = None, f'refuse: {exc}'
rtl_file_written = written is not None and written.resolve() == target.resolve()
v2 = proposal.identities_v2(fields, input_role, rom_hex_sha256=pin)
v2_f0 = proposal.identities_v2(fields, input_role, firmware_roles=False)
json.dump({
    'v1_build_id': fields['identities']['design'][:32],
    'v2_build_id': v2['design'][:32],
    'v2_build_id_evidence_only': v2_f0['design'][:32],
    'evidence_sha256': v2['evidence'],
    'program_sha256': sha(legacy.machine_program(Path(legacy.MACHINE_NETLIST))),
    'qsf_sha256': sha(qsf),
    'qsf_record_parameter': fields['parameters']['quartus_qsf_sha256'],
    'rom_hex_sha256': sha(rom),
    'producer_rom_path': legacy.ROM_HEX,
    'rtl_rom_path': rtl_rom_path,
    'current_rom_check': current_check,
    'rtl_rom_file_written_by_seal_step': rtl_file_written,
}, sys.stdout, sort_keys=True)
