#!/usr/bin/env python3
"""Prove null-safe remapper initialization in locked diagnostic BL31 instructions.

Requires unicorn==2.1.4. Never performs hardware, firmware or image operations.
"""
import argparse
import datetime
import hashlib
import json
from pathlib import Path
import struct
import sys
from unicorn import Uc, UcError, UC_ARCH_ARM64, UC_MODE_ARM, UC_HOOK_CODE, UC_HOOK_MEM_READ, UC_HOOK_MEM_WRITE, UC_HOOK_MEM_INVALID
from unicorn.arm64_const import UC_ARM64_REG_X0, UC_ARM64_REG_X30, UC_ARM64_REG_PC, UC_ARM64_REG_SP
R = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(R / 'scripts'))
import de25_sdm_diagnostic as diagnostic
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--before', required=True, type=Path)
parser.add_argument('--after', required=True, type=Path)
parser.add_argument('--output', required=True, type=Path)
args = parser.parse_args()
lock = diagnostic.load_lock()
before = diagnostic.regular(args.before)
after = diagnostic.regular(args.after)
if hashlib.sha256(before).hexdigest() != '6cd789a05f3cbbe66afae9efbc9d7067469c03129a1e9a1b0043f8a60b03c383' or hashlib.sha256(after).hexdigest() != lock['bl31_sha256']:
    raise ValueError('remapper proof requires the exact previous and guarded diagnostic binaries')
# Authenticate the adjacent MOV X0, #0 and BL instructions at configuration
# start, as well as executing the helper itself below. No source-only inference
# supplies the null argument. Complete binary identities bind these coordinates.
for blob, offset in [(before, 0x8ad4), (after, 0x8adc)]:
    move, call = struct.unpack_from('<II', blob, offset)
    immediate = call & 0x3ffffff
    if immediate & (1 << 25):
        immediate -= 1 << 26
    assert move == 0xd2800000 and call & 0xfc000000 == 0x94000000
    assert 0x80000000 + offset + 4 + 4 * immediate == 0x8000878c
D = args.output
D.mkdir(parents=True, exist_ok=False)
old = {'artifacts': {'bl31.bin': {'path': str(args.before), 'sha256': hashlib.sha256(before).hexdigest()}}}
new = {'artifacts': {'bl31.bin': {'path': str(args.after), 'sha256': lock['bl31_sha256']}}}
OLD = {'intel_smmu_hps_remapper_init': 0x8000878c, 'g_remapper_bypass': 0x800184dc}
NEW = dict(OLD)
BASE = 0x80000000
SYS = 0x10d12000
END = 0x90030000
PTR = 0x90020000
SENTINEL = 0x1122334455667788

def run(label, record, syms, *, null, lowmap, scratch, bypass):
    blob = Path(record['artifacts']['bl31.bin']['path']).read_bytes()
    assert hashlib.sha256(blob).hexdigest() == record['artifacts']['bl31.bin']['sha256']
    u = Uc(UC_ARCH_ARM64, UC_MODE_ARM)
    u.mem_map(BASE, 0x20000)
    u.mem_write(BASE, blob)
    u.mem_map(SYS, 0x1000)
    u.mem_map(0x90000000, 0x40000)
    if lowmap:
        u.mem_map(0, 0x1000)
        u.mem_write(0, struct.pack('<Q', SENTINEL))
    u.mem_write(SYS + 604, struct.pack('<I', scratch))
    u.mem_write(syms['g_remapper_bypass'], struct.pack('<I', bypass))
    u.mem_write(PTR, struct.pack('<Q', 0x9ff80000))
    u.reg_write(UC_ARM64_REG_SP, 0x90010000)
    u.reg_write(UC_ARM64_REG_X30, END)
    u.reg_write(UC_ARM64_REG_X0, 0 if null else PTR)
    accesses = []
    invalid = []

    def mem(uc, access, address, size, value, user):
        accesses.append(dict(kind=access, address=hex(address), size=size, value=hex(value)))

    def bad(uc, access, address, size, value, user):
        invalid.append(dict(kind=access, address=hex(address), size=size))
        return False

    def code(uc, address, size, user):
        if address == END:
            uc.emu_stop()
    u.hook_add(UC_HOOK_MEM_READ | UC_HOOK_MEM_WRITE, mem)
    u.hook_add(UC_HOOK_MEM_INVALID, bad)
    u.hook_add(UC_HOOK_CODE, code)
    error = None
    try:
        u.emu_start(syms['intel_smmu_hps_remapper_init'], END + 4, timeout=1000000, count=1000)
    except UcError as e:
        error = str(e)
    low = [x for x in accesses if int(x['address'], 16) < 0x1000]
    mmio = [x for x in accesses if x['kind'] == 17 and SYS <= int(x['address'], 16) < SYS + 0x1000]
    expected_mmio = [SYS + 644, SYS + 640] if scratch & 2 == 0 and bypass == 0 else []
    if syms is OLD and null and (not lowmap):
        assert error and invalid == [dict(kind=19, address='0x0', size=8)], (error, invalid)
    elif syms is OLD and null:
        assert error is None and len(low) == 2 and (struct.unpack('<Q', u.mem_read(0, 8))[0] == SENTINEL - 0x80000000 & (1 << 64) - 1)
    else:
        assert error is None and (not low) and (not invalid)
        assert [int(x['address'], 16) for x in mmio] == expected_mmio, mmio
        assert all((int(x['value'], 16) == 0x80000000 for x in mmio))
        if not null:
            assert struct.unpack('<Q', u.mem_read(PTR, 8))[0] == 0x1ff80000
    return dict(name=label, null_pointer=null, low_page_mapped=lowmap, scratch=scratch, bypass=bypass, low_page_accesses=low, MMIO_writes=mmio, invalid_access=invalid, error=error, valid_IOVA=hex(struct.unpack('<Q', u.mem_read(PTR, 8))[0]) if not null else None)
cases = [run('old helper faults with unmapped null page', old, OLD, null=True, lowmap=False, scratch=0, bypass=0), run('old helper modifies a mapped null page', old, OLD, null=True, lowmap=True, scratch=0, bypass=0)]
for scratch in [0, 2]:
    for bypass in [0, 1]:
        cases.append(run('guarded null init preserves required MMIO only', new, NEW, null=True, lowmap=False, scratch=scratch, bypass=bypass))
        a = run('old valid address translation', old, OLD, null=False, lowmap=False, scratch=scratch, bypass=bypass)
        b = run('guarded valid address translation', new, NEW, null=False, lowmap=False, scratch=scratch, bypass=bypass)
        assert a['MMIO_writes'] == b['MMIO_writes'] and a['valid_IOVA'] == b['valid_IOVA']
        cases.extend([a, b])
result = dict(passed=True, utc=datetime.datetime.now(datetime.timezone.utc).isoformat(), old_candidate_sha256=old['artifacts']['bl31.bin']['sha256'], guarded_candidate_sha256=new['artifacts']['bl31.bin']['sha256'], cases=cases, hardware_access=False, root_cause_proven=False, new_candidate_deployed=False, scope='Actual compiled remapper instructions and globals in Unicorn, emulated low/MMIO pages. No EL3 mappings, interconnect, remapper/SMMU hardware, SDM or board execution.', finding='Configuration-start source passes a null pointer; old compiled helper dereferences it. Guarded helper avoids null-page access while preserving required MMIO and exact valid-address translation for all four remap/bypass combinations.', native_counterevidence='Native START returned OK. The emulator unmapped-page control does not prove a physical fault or establish the null access as the native upload-stall cause.', script_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest())
(D / 'verified.json').write_text(json.dumps(result, indent=2) + '\n')
print('PASS', len(cases), 'actual-instruction remapper cases; null guard preserves MMIO and IOVA translation; no native-cause claim')
