"""Decode the bounded Nano serial probe; this is observation, not attestation.

The expected build ID and owner seed come from the checked matched build.
No device, programmer, firmware or filesystem operations are performed here.
"""
import re

MAX_BYTES = 1 << 20
MASTER_RESET = 0xff21bfbf  # nano_boot.h: functional and OCP resets
SD_RESET = 0x80  # controller only; unused SD ECC diagnostics must stay held
SD_BUSY = 0xb07
BRIDGES = 0xf
OWNED_FLAGS = (0x46424f31 << 32) | 0xdf
AFFINITY_MASK = 0xff00ffffff

STAGES = ('spl', 'initial', 'scope', 'mmc', 'fit', 'spl_handoff', 'provider',
          'uboot', 'dram', 'prompt', 'boot', 'memory', 'before-entry',
          'after-remove', 'live', 'handoff', 'el2', 'el1', 'go')
LOADER_STAGES = STAGES[:10]
H = r'[0-9a-f]{1,16}'
W = r'[0-9a-f]{8}'
RULES = [(name, re.compile(pattern)) for name, pattern in (
    ('spl', r'U-Boot SPL[^\r\n]*?-fes-nano-(?P<id>[0-9a-f]{16})(?:[ \t].*)?'),
    ('initial', rf'FES_NANO_SPL BEGIN cause=(?P<cause>{W}) per0=(?P<per0>{W}) bridge=(?P<bridge>{W}) cpus=(?P<cpus>{W}) release=(?P<release>{W}) kind=(?P<kind>[0-7]) valid=(?P<valid>[01])'),
    ('scope', rf'FES_NANO_SPL SCOPE mpidr=(?P<mpidr>{H}) core=(?P<core>[0-3]) cause_now=(?P<cause>{W}) cpus_now=(?P<cpus>{W}) release_now=(?P<release>{W}) reset_timeouts=(?P<timeouts>{W})'),
    ('mmc', r'Trying to boot from MMC1'),
    ('fit', r'FES_NANO_SPL FIT verified before parsing'),
    ('spl_handoff', rf'FES_NANO_SPL HANDOFF owner=(?P<owner>{H}) epoch=(?P<epoch>{H}) per0=(?P<per0>{W}) bridge=(?P<bridge>{W}) sd=(?P<sd>{W})'),
    ('provider', rf'(?:NOTICE:[ \t]+)?FES_NANO_OWNED_BOOT provider=(?P<provider>-?[0-9]{{1,10}}) result=(?P<result>-?[0-9]{{1,10}}) reset=(?P<bridge>{W})'),
    ('sd_platform', r'(?:NOTICE:[ \t]+)?FES_NANO_SD_PLATFORM setup=(?P<setup>-?[0-9]{1,10}) ready=(?P<ready>[01])'),
    ('uboot', r'U-Boot (?!SPL)[^\r\n]*?-fes-nano-(?P<id>[0-9a-f]{16})(?:[ \t].*)?'),
    ('dram', r'DRAM:[ \t]+1 GiB'),
    ('prompt', r'SOCFPGA_AGILEX5 # (?:fesboot)?'),
    ('boot', r'FES_NANO_BOOT id=(?P<id>[0-9a-f]{16})'),
    ('memory', rf'FES_NANO_MEMORY ram_top=(?P<ram_top>{H}) sp=(?P<sp>{H}) malloc=(?P<malloc_start>{H})\.\.(?P<malloc_end>{H}) reloc=(?P<reloc>{H}) fdt=(?P<fdt>{H})'),
    ('owned', rf'FES_NANO_OWNED phase=(?P<phase>[a-z-]{{1,32}}) status=(?P<status>-?[0-9]{{1,10}}) flags=(?P<flags>{H}) owner=(?P<owner>{H}) generation=(?P<generation>{H})'),
    ('live', rf'FES_NANO_HANDOFF affinity=(?P<affinity>{H}) counter=(?P<counter>[0-9]{{1,20}}) cpus=(?P<cpu0>-?[0-9]{{1,10}}),(?P<cpu1>-?[0-9]{{1,10}}),(?P<cpu2>-?[0-9]{{1,10}}),(?P<cpu3>-?[0-9]{{1,10}}) per0=(?P<per0>{W}) bridges=(?P<bridge>{W})'),
    ('handoff', r'FES_NANO_HANDOFF PASS serial-only'),
    ('el2', r'FES_ENTRY EL2 CLEAN'),
    ('el1', r'FES_ENTRY EL1 HANDOFF'),
    ('go', r'FES_NATIVE_GO_PROBE PASS os=tamago board=de25-nano'),
    ('storage', r'FES_STORAGE_PROBE board=de25-nano EL=1 counter=400000000Hz writes=disabled'),
    ('read', rf'FES_NANO_SD READ bytes=(?P<bytes>[0-9]{{1,10}}) sha256=(?P<sha256>[0-9a-f]{{64}}) cid=(?P<cid>[0-9a-f]{{32}}) sectors=(?P<sectors>[0-9]{{1,20}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('contained', rf'FES_NANO_SD CONTAINED owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('resumed', rf'FES_NANO_SD RESUMED bytes=(?P<bytes>[0-9]{{1,10}}) sha256=(?P<sha256>[0-9a-f]{{64}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('sd_pass', rf'FES_NANO_SD PASS read-only owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
)]
DECIMAL = {'kind', 'valid', 'core', 'provider', 'result', 'status', 'counter',
           'cpu0', 'cpu1', 'cpu2', 'cpu3', 'setup', 'ready', 'bytes', 'sectors', 'sd_generation'}
TEXT = {'id', 'phase', 'sha256', 'cid'}


def decode_native_boot(data, build_id, owner_seed, *, storage=None):
    """Retain partial progress, but qualify only one ordered, coherent boot.

    A bare prompt need not end in a newline. Other incomplete lines are held
    until their newline arrives, so fragmented UART reads cannot grant proof.
    Completion means the serial probe reported its handoff, not core loading,
    active-device drain, signed boot or independent memory qualification.
    """
    if (not isinstance(build_id, str) or not re.fullmatch('[0-9a-f]{16}', build_id) or
            type(owner_seed) is not int or not 0 < owner_seed < 1 << 64 or not owner_seed & 1):
        raise ValueError('checked build ID and nonzero odd uint64 owner seed required')
    if not isinstance(data, (str, bytes, bytearray, memoryview)):
        raise TypeError('serial bytes or text required')
    if storage is not None and (not isinstance(storage,dict) or set(storage)!={'bytes','sha256'} or
            type(storage['bytes']) is not int or not 0<storage['bytes']<=1<<20 or
            not isinstance(storage['sha256'],str) or not re.fullmatch('[0-9a-f]{64}',storage['sha256'])):
        raise ValueError('independently checked recovery manifest size/hash required')
    stages=STAGES if storage is None else STAGES[:7]+('sd_platform',)+STAGES[7:-1]+('storage','read','contained','resumed','sd_pass')
    loader_stages=stages[:stages.index('prompt')+1]
    report = {'records': [], 'errors': [], 'loader_ready': False, 'complete': False,
              'last_checkpoint': None, 'core_load_acceptance': False}
    if len(data) > MAX_BYTES:
        report['errors'].append('capture exceeds byte bound')
        return report
    raw = data.encode() if isinstance(data, str) else bytes(data)
    if len(raw) > MAX_BYTES:
        report['errors'].append('capture exceeds byte bound')
        return report
    lines = raw.decode(errors='replace').split('\n')
    tail = lines.pop()
    if re.fullmatch('SOCFPGA_AGILEX5 # (?:fesboot)?', tail):
        lines.append(tail)
    observed = {}
    for number, line in enumerate(lines, 1):
        line = line.rstrip('\r')
        if ('FES_' in line and re.search(r'\b(?:REJECT|FAIL|EXCEPTION)\b', line)) or line.startswith(('panic:', 'fatal error:')):
            report['errors'].append(f'failure marker at line {number}')
        for name, pattern in RULES:
            match = pattern.fullmatch(line)
            if match is None:
                continue
            values = {key: value if key in TEXT else int(value, 10 if key in DECIMAL else 16)
                      for key, value in match.groupdict().items()}
            if name == 'owned':
                name = values['phase']
            if name=='sd_platform' and storage is None:
                if ('sd_platform' in observed or not report['records'] or report['records'][-1]['stage']!='provider' or
                        values!={'setup':0,'ready':0}):
                    report['errors'].append(f'unexpected SD authority at line {number}')
                observed[name]=values
                break
            index = len(report['records'])
            if index >= len(stages) or name != stages[index]:
                report['errors'].append(f'out-of-order or duplicate {name} at line {number}')
            if 'id' in values and (values['id'] != build_id or line.count('-fes-nano-') > 1):
                report['errors'].append(f'build identity mismatch at line {number}')
            row = {'stage': name, 'line': number, 'values': values}
            report['records'].append(row)
            observed.setdefault(name, values)
            break
        else:
            if (line.startswith(('U-Boot ', 'FES_NANO_', 'FES_ENTRY', 'FES_NATIVE_GO_PROBE', 'FES_STORAGE_PROBE',
                                 'Trying to boot from ', 'DRAM:', 'SOCFPGA_AGILEX5 #')) or
                    line.startswith('NOTICE:') and 'FES_NANO_' in line):
                report['errors'].append(f'malformed or unexpected probe marker at line {number}')

    def require(condition, reason):
        if not condition:
            report['errors'].append(reason)

    first = observed.get('initial')
    scope = observed.get('scope')
    seal = observed.get('spl_handoff')
    if first:
        require(first['valid'] == 1 and first['kind'] in (0, 2, 3, 4) and not first['cause'] & ~5,
                'initial cold proof rejected')
        require(first['per0'] & (MASTER_RESET & ~SD_RESET) == MASTER_RESET & ~SD_RESET and
                first['bridge'] & BRIDGES == BRIDGES and not first['release'] & BRIDGES,
                'initial master/bridge/release scope differs')
    if scope:
        core = scope['core']
        require(core in (0, 2) and scope['mpidr'] & AFFINITY_MASK == core << 8 and not scope['timeouts'],
                'primary CPU or reset timeout scope differs')
        if first:
            spares = BRIDGES ^ (1 << core)
            require(scope['cause'] == first['cause'] and scope['release'] == first['release'] == 0 and
                    scope['cpus'] & spares == spares and first['cpus'] & spares == spares,
                    'first/fresh CPU scope differs')
    if seal:
        require(seal['epoch'] > 0 and seal['owner'] == (owner_seed ^ seal['epoch']) | 1,
                'SPL owner/epoch differs from matched chain')
        require(seal['per0'] & MASTER_RESET == MASTER_RESET and seal['bridge'] & BRIDGES == BRIDGES and
                not seal['sd'] & SD_BUSY, 'SPL containment or SD drain differs')
    if 'provider' in observed:
        provider = observed['provider']
        require(provider['provider'] == 1 and provider['result'] == 0 and provider['bridge'] & BRIDGES == BRIDGES,
                'secure provider proof rejected')
    for phase in ('before-entry', 'after-remove'):
        if phase in observed:
            value = observed[phase]
            require(value['status'] == 0 and value['flags'] == OWNED_FLAGS and value['generation'] == 1 and
                    seal is not None and value['owner'] == seal['owner'], f'{phase} owner/flags/generation differs')
    if 'live' in observed:
        live = observed['live']
        require(scope is not None and live['affinity'] == scope['core'] << 8 and live['counter'] == 400000000 and
                all(live['cpu'+str(i)] == (0 if i == scope['core'] else 1) for i in range(4)),
                'native CPU/counter scope differs')
        require(live['per0'] & MASTER_RESET == MASTER_RESET and live['bridge'] & BRIDGES == BRIDGES,
                'native master/bridge scope differs')
    if storage is not None:
        if 'sd_platform' in observed:
            require(observed['sd_platform']=={'setup':1,'ready':1},'controlled SD setup rejected')
        for phase,generation in (('read',3),('contained',4),('resumed',6),('sd_pass',7)):
            if phase in observed:
                value=observed[phase]
                require(seal is not None and value['owner']==seal['owner'] and value['sd_generation']==generation,
                        f'{phase} SD owner/generation differs')
                if phase in ('read','resumed'):
                    require(value['bytes']==storage['bytes'] and value['sha256']==storage['sha256'],
                            f'{phase} recovery manifest differs')
        if 'read' in observed:
            require(observed['read']['sectors']>0 and int(observed['read']['cid'],16)>0,
                    'SD card identity/capacity absent')
    names = tuple(row['stage'] for row in report['records'])
    report['loader_ready'] = not report['errors'] and names[:len(loader_stages)] == loader_stages
    report['complete'] = not report['errors'] and names == stages and not tail
    report['last_checkpoint'] = names[-1] if names else None
    return report
