"""Replay the two passive board snapshots and bounded SDM status queries on CBL."""
import hashlib
import re

SCOPE = 'de25-cbl-post-ddr-admission-v1'
REVISION = '87b2d7f4721c819b554212f2b80c193a07a2531e'
PREFIX = 'FES_CBL_MAC_ADMIT '
PARK = PREFIX + 'PARK no-storage no-bl31'
GROUPS = {
    'CPU': 'el mpidr sctlr daif frequency scope bd bank_base bank_size ram_size',
    'BOARD': 'cause per0 bridge timeouts tsn route sid',
    'CLOCK': 'stat main_en main_bypass noc_clk noc_div per_en per_bypass emac_sel counter_rst',
    'SMMU': 'read idr0 idr1 idr5 cr0 ack secure_cr0 secure_ack cr1 cr2 gbpa irq gerror gerrorn cmdprod cmdcons evtprod evtcons',
    'SUPPLY': 'read sdm l4sp emac mailbox words w0 w1 w2 w3 w4 w5',
}
LONG_WORDS = {'mpidr', 'sctlr', 'daif', 'bank_base', 'bank_size', 'ram_size'}
SPL_BEGIN = 'FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge=0000004f cpus=0000000f release=00000000 kind=4 valid=1'
SPL_SCOPE = 'FES_NANO_SPL SCOPE mpidr=81000000 core=0 cause_now=00000004 cpus_now=0000000f release_now=00000000 reset_timeouts=00000000'


def _sample(lines, index):
    values = {}
    for group, fields in GROUPS.items():
        pattern = PREFIX + group + f' pass={index}'
        for name in fields.split():
            width = 16 if name in LONG_WORDS else 8
            pattern += f' {name}=([0-9a-f]{{{width}}})'
        match = re.fullmatch(pattern, next(lines, ''))
        if not match:
            raise ValueError(f'missing or malformed pass {index} {group}')
        values[group] = {key: int(word, 16) for key, word in zip(fields.split(), match.groups())}
    return values


def _conditions(s):
    u, b, c, m, p = (s[k] for k in GROUPS)
    if u['scope'] not in (0, 1) or u['bd'] not in (0, 1):
        raise ValueError('invalid CPU scope or bank presence flag')
    cpu = u['el'] == 3 and u['mpidr'] == 0x81000000 and u['scope'] == 1 and not u['sctlr'] & 1
    noc = (c['stat'] & 0x01030301 == 0x00030300 and c['main_en'] & 14 == 14 and
           (c['noc_clk'] >> 16) & 7 <= 1 and not c['main_bypass'] & 2)
    if not cpu and any((*b.values(), *c.values())):
        raise ValueError('MMIO data outside observed CPU scope')
    for group in (m, p):
        if group['read'] != int(cpu and noc) or (not group['read'] and any(group.values())):
            raise ValueError('supplier or SMMU read evidence contradicts its gate')
    if p['words'] > 6 or (p['mailbox'] and p['words']) or any(p[f'w{i}'] for i in range(p['words'], 6)):
        raise ValueError('invalid response count or data outside the returned words')
    held = {
        'CPU scope': cpu,
        'cold reset and closed bridges': b['cause'] == 4 and b['per0'] == 0xff7fbfff and
                                         b['bridge'] & 15 == 15 and b['timeouts'] == 0,
        'default MAC stream route': b['route'] == 0x30 and b['sid'] == 0,
        'SMMU identity': m['read'] and (m['idr0'], m['idr1'], m['idr5']) == (0x080f7e3f, 0x0e739d18, 0x00400075),
        'SMMU disabled with empty queues': not any(m[k] for k in ('cr0', 'ack', 'secure_cr0', 'secure_ack',
                                                                  'gerror', 'gerrorn', 'cmdprod', 'cmdcons', 'evtprod', 'evtcons')),
    }
    eligible = {
        'CPU MMU and data cache': not u['sctlr'] & 5,
        'CPU exception masks': u['daif'] & 0x3c0 == 0x3c0,
        'counter frequency': u['frequency'] == 400000000,
        'runtime DDR bank': u['bd'] == 1 and u['bank_base'] == 0x80000000 and u['ram_size'] == 0x40000000,
        'NoC clock': noc,
        'EMAC clock source': c['per_en'] & 1 and not (c['per_bypass'] | c['counter_rst']) & (1 << ((c['emac_sel'] >> 26) & 1)),
        'SMMU reset configuration': m['cr1'] == 0 and m['cr2'] == 4 and m['gbpa'] in (0, 1 << 20) and m['irq'] == 0,
        'SDM CONFIG_STATUS response': p['read'] and p['mailbox'] == 0 and p['words'] == 6,
        'SDM state': p['w0'] == 0,
        'SDM nSTATUS': bool(p['w2'] & (1 << 31)),
        'SDM CONF_DONE': bool(p['w3'] & 1),
        'SDM INIT_DONE': bool(p['w3'] & 2),
        'SDM SEU clear': not p['w3'] & 8,
        'vendor SDM status': p['sdm'] == 0,
        'L4SP clock rate': p['l4sp'] == 100000000,
        'EMAC clock rate': p['emac'] == 125000000,
    }
    return [k for k, passed in held.items() if not passed], [k for k, passed in eligible.items() if not passed]


def decode_admission(raw: bytes, receipt: dict) -> dict:
    """Verify source, order, exclusive scope and reported decisions; keep denied snapshots."""
    identity = receipt.get('diagnostic_id')
    if (receipt.get('scope') != SCOPE or receipt.get('status') != 'prepared' or
            receipt.get('parking_revision') != REVISION or receipt.get('historical_trace') != REVISION or
            not isinstance(identity, str) or not re.fullmatch('[0-9a-f]{16}', identity) or
            not isinstance(raw, bytes) or len(raw) > 2 << 20):
        raise ValueError('invalid admission diagnostic source or UART stream')
    data = raw.replace(b'\r\n', b'\n')
    if not data.endswith((PARK + '\n').encode()):
        raise ValueError('missing terminal diagnostic Park')
    try:
        lines = data.decode('ascii').split('\n')
    except UnicodeDecodeError as error:
        raise ValueError('non-ASCII diagnostic stream') from error
    if any('Trying to boot from' in row or row.startswith(('NOTICE:  BL31:', 'U-Boot 20')) for row in lines):
        raise ValueError('unexpected storage or firmware handoff')
    rows = iter(row for row in lines if row.startswith(('U-Boot SPL ', 'FES_', 'DDR: init success')))
    if not next(rows, '').startswith('U-Boot SPL 2025.01-fes-nano-cpu-' + identity + ' '):
        raise ValueError('missing matching SPL startup')

    def checkpoint(stage):
        match = re.fullmatch(f'FES_NANO_CPU_TRACE id={identity} stage={stage:02x}' +
                             r' value=([0-9a-f]{8}) mpidr=0000000081000000 el=3', next(rows, ''))
        if not match:
            raise ValueError(f'missing or unordered qualified CPU checkpoint {stage:02x}')
        return int(match[1], 16)

    def power(stage):
        primary, cpu1, cpu2, cpu3, req, ack, boot = [checkpoint(stage + i) for i in range(7)]
        if cpu1 or cpu2 or cpu3 or req & 0x1e != 2 or ack & 0x1e != 2 or boot & 0xc00:
            raise ValueError('unexpected secondary CPU power or selected boot configuration')
        return boot

    if checkpoint(1):
        raise ValueError('initial cold CPU checkpoint differs')
    boot = power(0x20)
    if any(checkpoint(stage) for stage in range(2, 0x12)):
        raise ValueError('cold CPU checkpoint differs')
    if [next(rows, '') for _ in range(4)] != [SPL_BEGIN, SPL_SCOPE, 'DDR: init success', PREFIX + 'BEGIN id=' + identity]:
        raise ValueError('missing ordered cold scope, completed DDR setup, or admission entry')
    first, second = (_sample(rows, i) for i in range(2))
    first_reasons = _conditions(first)
    held_reasons, eligibility_reasons = _conditions(second)
    stable = first == second
    held = stable and not held_reasons
    eligible = held and not eligibility_reasons
    expected = f'END stable={int(stable)} held={int(held)} eligible={int(eligible)}'
    if next(rows, '') != PREFIX + expected:
        raise ValueError('firmware flags contradict independent snapshot replay')
    if power(0x30) != boot or checkpoint(0x12) or list(rows) != [PARK]:
        raise ValueError('missing matched late CPU ownership or additional diagnostic output')
    return dict(scope=SCOPE, diagnostic_id=identity, uart_sha256=hashlib.sha256(raw).hexdigest(),
                snapshots=[first, second], stable=stable, held=held, eligible=eligible, parked=True,
                failures=[{'held': first_reasons[0], 'eligible': first_reasons[1]},
                          {'held': held_reasons, 'eligible': eligibility_reasons}])
