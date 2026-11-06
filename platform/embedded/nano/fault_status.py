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
          for n in range(4) for name, offset in (('base', 0), ('limit', 4), ('access', 8)))
PROBES = tuple((name, 'cpu', register) for name, register in CPU) + tuple(
    (name, 'mmio', address) for name, address in MMIO)

# RAS v1 registers with an architecturally defined read, including MISC0.
# ADDR/MISC1 and pseudo-fault generators are deliberately excluded. Inspect
# only the currently selected record; never write a selector or acknowledge it.
RAS_READS = (
    ('pfr0', 'id_aa64pfr0_el1'), ('disr', 'disr_el1'),
    ('count', 'erridr_el1'), ('selector_before', 'errselr_el1'),
    ('features', 'erxfr_el1'), ('control', 'erxctlr_el1'),
    ('status_before', 'erxstatus_el1'), ('misc0', 'erxmisc0_el1'),
    ('status_after', 'erxstatus_el1'), ('selector_after', 'errselr_el1'),
)


def ras_assembly():
    """Bound optional reads by observed RAS support, NUM and current SEL."""
    rows = ['/* Read-only RAS v1 snapshot; no selector changes or error clears. */']
    for index, (_, register) in enumerate(RAS_READS):
        rows += ['mov w17, #FES_RAM_UART_BYTES', 'fes_ram_uart_text fes_ras_before, 1',
                 f'mov x19, #{index}', 'fes_ram_uart_hex x19',
                 'fes_ram_uart_text fes_nano_ram_fault_newline, 1',
                 f'mrs x0, {register}', 'mov w17, #FES_RAM_UART_BYTES',
                 'fes_ram_uart_text fes_ras_value, 1', 'fes_ram_uart_hex x19',
                 'fes_ram_uart_text fes_probe_data, 1', 'fes_ram_uart_hex x0',
                 'fes_ram_uart_text fes_nano_ram_fault_newline, 1']
        if index == 0:
            rows += ['ubfx x8, x0, #28, #4', 'cmp x8, #1', 'b.ne .Lfes_ras_feature']
        elif index == 2:
            # NUM==0 can make even ERRSELR undefined. Reserved bits must be 0.
            rows += ['lsr x8, x0, #16', 'cbnz x8, .Lfes_ras_count',
                     'cbz x0, .Lfes_ras_count', 'mov x24, x0']
        elif index == 3:
            rows += ['cmp x0, x24', 'b.hs .Lfes_ras_selector', 'mov x25, x0']
        elif index == 9:
            rows += ['cmp x0, x25', 'b.ne .Lfes_ras_changed']
    rows += ['mov w17, #FES_RAM_UART_BYTES', 'fes_ram_uart_text fes_ras_end, 1',
             'b .Lfes_ras_done']
    for reason in ('feature', 'count', 'selector', 'changed'):
        rows += [f'.Lfes_ras_{reason}:', 'mov w17, #FES_RAM_UART_BYTES',
                 f'fes_ram_uart_text fes_ras_skip_{reason}, 1', 'b .Lfes_ras_done']
    rows += ['.Lfes_ras_done:', '.pushsection .rodata.fes_ras,"a"',
             'fes_ras_before: .asciz "FES_NANO_RAM RAS before="',
             'fes_ras_value: .asciz "FES_NANO_RAM RAS value="',
             'fes_ras_end: .asciz "FES_NANO_RAM RAS END\\r\\n"']
    for reason in ('feature', 'count', 'selector', 'changed'):
        rows += [f'fes_ras_skip_{reason}: .asciz "FES_NANO_RAM RAS SKIP reason={reason}\\r\\n"']
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
                outcome=None, selected=None, record_complete=False)


def observe_ras(probe, line, number, *, exception, inventory_complete):
    """Retain only ordered, feature/selector-qualified selected-record reads.

    END means this one selected record was read, never that all RAS records
    were covered or that status is fresh, stable, healthy or recoverable.
    """
    def invalid():
        probe['valid'] = False
        probe['complete'] = probe['record_complete'] = False
        return f'invalid early RAS probe at line {number}'

    if not exception or not inventory_complete or not probe['valid'] or probe['complete']:
        return invalid()
    records = probe['records']

    def allowed(index):
        if index >= 1 and (records[0]['value'] >> 28) & 15 != 1:
            return False
        if index >= 3 and not 0 < records[2]['value'] <= 0xffff:
            return False
        if index >= 4 and records[3]['value'] >= records[2]['value']:
            return False
        return True

    match = re.fullmatch(r'FES_NANO_RAM RAS (before|value)=([0-9a-f]{16})(?: data=([0-9a-f]{16}))?', line)
    if match:
        kind, raw_index, raw_value = match.groups()
        index = int(raw_index, 16)
        if index != len(records) or index >= len(RAS_READS) or not allowed(index):
            return invalid()
        if kind == 'before':
            if raw_value is not None or probe['pending'] is not None:
                return invalid()
            probe['pending'] = dict(index=index, line=number)
        else:
            if raw_value is None or probe['pending'] is None or probe['pending']['index'] != index:
                return invalid()
            name, target = RAS_READS[index]
            records.append(dict(index=index, name=name, target=target, value=int(raw_value, 16),
                                before_line=probe['pending']['line'], line=number))
            probe['pending'] = None
        return None
    if probe['pending'] is not None:
        return invalid()
    expected = None
    if len(records) == 1 and not allowed(1):
        expected = 'feature'
    elif len(records) == 3 and not allowed(3):
        expected = 'count'
    elif len(records) == 4 and not allowed(4):
        expected = 'selector'
    elif len(records) == len(RAS_READS):
        expected = 'changed' if records[9]['value'] != records[3]['value'] else 'selected-record'
    end = ('FES_NANO_RAM RAS END' if expected == 'selected-record'
           else 'FES_NANO_RAM RAS SKIP reason='+str(expected))
    if expected is None or line != end:
        return invalid()
    probe['complete'] = True
    probe['outcome'] = expected
    if len(records) >= 4:
        probe['selected'] = records[3]['value']
    probe['record_complete'] = expected == 'selected-record'
    return None
