"""Fixed private post-exception inventory; never a runtime ownership proof.

MMIO definitions are the retained official CS/R1 revB0 RC28 register map.
Reads do not acknowledge status. No aliases, injectors, initializers or memory
access ports are included. Architectural registers need no optional RAS support.
"""
import re

CPU = (
    ('midr', 'midr_el1'), ('pfr0', 'id_aa64pfr0_el1'),
    ('sctlr', 'sctlr_el3'), ('scr', 'scr_el3'),
    ('tcr', 'tcr_el3'), ('mair', 'mair_el3'), ('ttbr0', 'ttbr0_el3'),
)
MMIO = (
    ('dsu_status_before', 0x1c000108), ('dsu_location0', 0x1c00010c),
    ('dsu_location1', 0x1c000110), ('dsu_status_after', 0x1c000108),
    ('boot_attributes', 0x1c000390), ('boot_base_low', 0x1c000394),
    ('boot_base_high', 0x1c000398),
    ('ocram_status_before', 0x1c00c120), ('ocram_location0', 0x1c00c124),
    ('ocram_location1', 0x1c00c128), ('ocram_status_after', 0x1c00c120),
    ('ecc_config', 0x108cc004), ('ecc_control', 0x108cc008),
    ('ecc_initialized', 0x108cc00c), ('ecc_pending', 0x108cc020),
    ('ecc_mode', 0x108cc028), ('ecc_double_address_a', 0x108cc02c),
    ('ecc_double_address_b', 0x108cc034), ('ecc_decoder', 0x108cc084),
    ('firewall_enabled', 0x108cc400),
) + tuple((f'firewall{n}_{name}', 0x108cc410+12*n+offset)
          for n in range(4) for name, offset in (('base', 0), ('limit', 4), ('access', 8))) + (
    ('dsu_detect', 0x1c000100), ('dsu_interrupt', 0x1c000104),
    ('ocram_detect', 0x1c00c118), ('ocram_interrupt', 0x1c00c11c),
)
PROBES = tuple((name, 'cpu', register) for name, register in CPU) + tuple(
    (name, 'mmio', address) for name, address in MMIO)

# Reviewed Cortex-A55 r2p0 record layout: record0 is local L1/L2, record1
# is the shared DSU. Only ERRSELR is written (constant0/1, then saved0/1).
# Each selector operation includes ISB and its own readback. Detection,
# interrupt, status, ADDR/MISC1 and pseudo-fault controls are never written.
RAS_MIDR = 0x412fd050
RAS_STEPS = (
    ('pfr0', 'read', 'id_aa64pfr0_el1', None),
    ('midr', 'read', 'midr_el1', None),
    ('disr', 'read', 'disr_el1', None),
    ('count', 'read', 'erridr_el1', None),
    ('original_selector', 'read', 'errselr_el1', None),
) + tuple(step for bank in (0, 1) for step in (
    (f'select{bank}', 'select', 'errselr_el1', bank),
    (f'features{bank}', 'read', 'erxfr_el1', None),
    (f'control{bank}', 'read', 'erxctlr_el1', None),
    (f'status{bank}_before', 'read', 'erxstatus_el1', None),
    (f'misc{bank}', 'read', 'erxmisc0_el1', None),
    (f'status{bank}_after', 'read', 'erxstatus_el1', None),
    (f'selector{bank}_after', 'read', 'errselr_el1', None),
)) + (('restore', 'restore', 'errselr_el1', None),)


def ras_assembly():
    """Fixed two-record observation; selector-only writes after identity gates."""
    rows = ['/* RAS2 A55 bank snapshot; selector-only writes, never status clears. */']
    for index, (_, kind, register, requested) in enumerate(RAS_STEPS):
        rows += ['mov w17, #FES_RAM_UART_BYTES', 'fes_ram_uart_text fes_ras_before, 1',
                 f'mov x19, #{index}', 'fes_ram_uart_hex x19',
                 'fes_ram_uart_text fes_nano_ram_fault_newline, 1']
        if kind in ('select', 'restore'):
            rows += [f'mov x0, #{requested}' if kind == 'select' else 'mov x0, x25',
                     'msr errselr_el1, x0', 'isb']
        rows += [f'mrs x0, {register}', 'mov w17, #FES_RAM_UART_BYTES',
                 'fes_ram_uart_text fes_ras_value, 1', 'fes_ram_uart_hex x19',
                 'fes_ram_uart_text fes_probe_data, 1', 'fes_ram_uart_hex x0',
                 'fes_ram_uart_text fes_nano_ram_fault_newline, 1']
        if index == 0:
            rows += ['ubfx x8, x0, #28, #4', 'cmp x8, #1', 'b.ne .Lfes_ras_feature']
        elif index == 1:
            rows += [f'ldr x8, ={RAS_MIDR:#x}', 'cmp x0, x8', 'b.ne .Lfes_ras_cpu']
        elif index == 3:
            rows += ['cmp x0, #2', 'b.ne .Lfes_ras_count']
        elif index == 4:
            rows += ['cmp x0, #2', 'b.hs .Lfes_ras_selector', 'mov x25, x0']
        elif index in (5, 11):
            rows += ['cbnz x0, .Lfes_ras_changed']
        elif index in (12, 18):
            rows += ['cmp x0, #1', 'b.ne .Lfes_ras_changed']
        elif index == 19:
            rows += ['cmp x0, x25', 'b.ne .Lfes_ras_changed']
    rows += ['mov w17, #FES_RAM_UART_BYTES', 'fes_ram_uart_text fes_ras_end, 1',
             'b .Lfes_ras_done']
    for reason in ('feature', 'cpu', 'count', 'selector', 'changed'):
        rows += [f'.Lfes_ras_{reason}:', 'mov w17, #FES_RAM_UART_BYTES',
                 f'fes_ram_uart_text fes_ras_skip_{reason}, 1', 'b .Lfes_ras_done']
    rows += ['.Lfes_ras_done:', '.pushsection .rodata.fes_ras,"a"',
             'fes_ras_before: .asciz "FES_NANO_RAM RAS2 before="',
             'fes_ras_value: .asciz "FES_NANO_RAM RAS2 value="',
             'fes_ras_end: .asciz "FES_NANO_RAM RAS2 END\\r\\n"']
    for reason in ('feature', 'cpu', 'count', 'selector', 'changed'):
        rows += [f'fes_ras_skip_{reason}: .asciz "FES_NANO_RAM RAS2 SKIP reason={reason}\\r\\n"']
    return '\n'.join(rows + ['.popsection'])+'\n'


def assembly(test_addresses=None):
    """Emit canonical reads; only off-board fixtures substitute MMIO targets."""
    if test_addresses is not None and (not isinstance(test_addresses, dict) or
            any(type(index) is not int or not 0 <= index < len(PROBES) or PROBES[index][1] != 'mmio' or
                type(address) is not int or not 0 <= address < 1 << 64 or address % 4
                for index, address in test_addresses.items())):
        raise ValueError('invalid injected status address map')
    rows = ['/* Fixed post-fault reads only; no status clear, memory scrub or return. */']
    for index, (_, kind, target) in enumerate(PROBES):
        rows += ['mov w17, #FES_RAM_UART_BYTES', 'fes_ram_uart_text fes_probe_before, 1',
                 f'mov x19, #{index}', 'fes_ram_uart_hex x19',
                 'fes_ram_uart_text fes_nano_ram_fault_newline, 1']
        if kind == 'cpu':
            rows += ['mrs x0, '+target]
        else:
            address = target if test_addresses is None else test_addresses.get(index, target)
            rows += [f'ldr x8, ={address:#x}', 'ldr w0, [x8]']
        rows += ['mov w17, #FES_RAM_UART_BYTES', 'fes_ram_uart_text fes_probe_value, 1',
                 'fes_ram_uart_hex x19', 'fes_ram_uart_text fes_probe_data, 1',
                 'fes_ram_uart_hex x0', 'fes_ram_uart_text fes_nano_ram_fault_newline, 1']
    rows += ['mov w17, #FES_RAM_UART_BYTES', 'fes_ram_uart_text fes_probe_end, 1',
             '.pushsection .rodata.fes_probe,"a"',
             'fes_probe_before: .asciz "FES_NANO_RAM PROBE before="',
             'fes_probe_value: .asciz "FES_NANO_RAM PROBE value="',
             'fes_probe_data: .asciz " data="',
             'fes_probe_end: .asciz "FES_NANO_RAM PROBE END\\r\\n"', '.popsection']
    return '\n'.join(rows)+'\n'+ras_assembly()


def state():
    return dict(records=[], pending=None, complete=False, valid=True)


def observe(probe, line, number, *, exception):
    """Retain bounded partial progress; completion never grants boot or recovery."""
    def invalid():
        probe['valid'] = False
        probe['complete'] = False
        return f'invalid early status probe at line {number}'
    if not exception or not probe['valid'] or probe['complete']:
        return invalid()
    match = re.fullmatch(r'FES_NANO_RAM PROBE (before|value)=([0-9a-f]{16})(?: data=([0-9a-f]{16}))?', line)
    if match:
        kind, raw_index, raw_value = match.groups()
        index = int(raw_index, 16)
        if index != len(probe['records']) or index >= len(PROBES):
            return invalid()
        if kind == 'before':
            if raw_value is not None or probe['pending'] is not None:
                return invalid()
            probe['pending'] = dict(index=index, line=number)
        else:
            if raw_value is None or probe['pending'] is None or probe['pending']['index'] != index:
                return invalid()
            name, source, target = PROBES[index]
            value = int(raw_value, 16)
            if source == 'mmio' and value > 0xffffffff:
                return invalid()
            probe['records'].append(dict(index=index, name=name, source=source, target=target,
                                         value=value, before_line=probe['pending']['line'], line=number))
            probe['pending'] = None
        return None
    if line == 'FES_NANO_RAM PROBE END' and probe['pending'] is None and len(probe['records']) == len(PROBES):
        probe['complete'] = True
        return None
    return invalid()


def ras_state():
    return dict(records=[], pending=None, complete=False, valid=True,
                outcome=None, original_selector=None, selector_may_have_changed=False,
                selector_restored=False, record_complete=False)


def _ras_stop(records):
    """Return the first failed gate; only a genuine complete path returns END."""
    for index, row in enumerate(records):
        value = row['value']
        if index == 0 and (value >> 28) & 15 != 1:
            return 'feature'
        if index == 1 and value != RAS_MIDR:
            return 'cpu'
        if index == 3 and value != 2:
            return 'count'
        if index == 4 and value >= 2:
            return 'selector'
        if (index in (5, 11) and value != 0) or (index in (12, 18) and value != 1):
            return 'changed'
        if index == 19 and value != records[4]['value']:
            return 'changed'
    return 'both-records' if len(records) == len(RAS_STEPS) else None


def observe_ras(probe, line, number, *, exception, inventory_complete):
    """Require identity gates, fixed selections and restoration for RAS2 END.

    A partial operation marks the selector as potentially changed as soon as its
    before marker is observed. No unsuccessful readback proves restoration.
    All RAS2 outcomes remain terminal exception evidence, never boot permission.
    """
    def invalid():
        probe['valid'] = False
        probe['complete'] = probe['record_complete'] = False
        return f'invalid early RAS probe at line {number}'

    if not exception or not inventory_complete or not probe['valid'] or probe['complete']:
        return invalid()
    records = probe['records']
    match = re.fullmatch(r'FES_NANO_RAM RAS2 (before|value)=([0-9a-f]{16})(?: data=([0-9a-f]{16}))?', line)
    if match:
        kind, raw_index, raw_value = match.groups()
        index = int(raw_index, 16)
        if index != len(records) or index >= len(RAS_STEPS) or _ras_stop(records) is not None:
            return invalid()
        name, operation, target, requested = RAS_STEPS[index]
        if kind == 'before':
            if raw_value is not None or probe['pending'] is not None:
                return invalid()
            probe['pending'] = dict(index=index, line=number)
            if operation in ('select', 'restore'):
                probe['selector_may_have_changed'] = True
        else:
            if raw_value is None or probe['pending'] is None or probe['pending']['index'] != index:
                return invalid()
            value = int(raw_value, 16)
            records.append(dict(index=index, name=name, operation=operation, target=target,
                                requested=requested, value=value,
                                before_line=probe['pending']['line'], line=number))
            probe['pending'] = None
            if index == 4:
                probe['original_selector'] = value
            if index == 19 and value == records[4]['value']:
                probe['selector_restored'] = True
                probe['selector_may_have_changed'] = False
        return None
    if probe['pending'] is not None:
        return invalid()
    expected = _ras_stop(records)
    end = ('FES_NANO_RAM RAS2 END' if expected == 'both-records'
           else 'FES_NANO_RAM RAS2 SKIP reason='+str(expected))
    if expected is None or line != end:
        return invalid()
    probe['complete'] = True
    probe['outcome'] = expected
    probe['record_complete'] = expected == 'both-records' and probe['selector_restored']
    return None
