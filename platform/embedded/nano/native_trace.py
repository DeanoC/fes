"""Decode the bounded Nano serial probe; this is observation, not attestation.

The expected build ID and owner seed come from the checked matched build.
No device, programmer, firmware or filesystem operations are performed here.
"""
import hashlib
import re
from hps_context import reader_pattern, digest as context_digest
from reader_trace import extract as extract_reader_trace,validate as validate_reader_trace

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
    ('smmu', r'(?:NOTICE:[ \t]+)?FES_NANO_SMMU status=(?P<status>-?[0-9]{1,10}) reads=(?P<reads>[0-9]{1,3}) '+
        ' '.join(key+r'=(?P<'+key+r'>[0-9a-f]{1,8})' for key in
                 ('idr0','idr1','aidr','s_idr1','cr0','ack','gbpa','agbpa','s_cr0','s_ack','s_init','s_gbpa','s_agbpa'))+
        r' identified=(?P<identified>[01]) settled=(?P<settled>[01]) bypass_controls_clear=(?P<bypass_controls_clear>[01]) access=unqualified'),
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
    ('hps_context', rf'FES_NATIVE_HPS_CONTEXT PASS sha256=(?P<sha256>[0-9a-f]{{64}}) package_id=(?P<package_id>[0-9a-f]{{64}}) build_id=(?P<build_id>[0-9a-f]{{32}}) owner=(?P<owner>{H}) generation=(?P<generation>{H}) (?:runtime_)?programming=unavailable'),
    ('configuration_package', rf'FES_NANO_LOAD_PACKAGE CONTAINED package_id=(?P<package_id>[0-9a-f]{{64}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}}) source=sd storage_active=0'),
    ('identity_transition', rf'(?:NOTICE:[ \t]+)?FES_NANO_IDENTITY phase=(?P<phase>[a-z-]{{1,24}}) owner=(?P<owner>{H}) generation=(?P<generation>{H}) reset=(?P<reset>{H}) base=(?P<base>{H})'),
    ('fpga_query', r'(?:NOTICE:[ \t]+)?FES_NANO_CONFIG_QUERY command=4 words=6'),
    ('fpga_status', rf'(?:NOTICE:[ \t]+)?FES_NANO_CONFIG_STATUS status=(?P<status>-?[0-9]{{1,10}}) words=(?P<words>[0-9]{{1,3}}) state=(?P<state>{H}) version=(?P<version>{H}) pins=(?P<pins>{H}) soft=(?P<soft>{H}) error_location=(?P<error_location>{H}) error_details=(?P<error_details>{H}) ready=(?P<ready>[01])'),
    ('identity_access', rf'(?:NOTICE:[ \t]+)?FES_NANO_IDENTITY_ACCESS phase=(?P<phase>before|after) ordinal=(?P<ordinal>[0-9]{{1,2}}) offset=(?P<offset>{H}) value=(?P<value>{H})'),
    ('fpga_unready', rf'FES_NATIVE_FPGA_UNREADY CLOSED owner=(?P<owner>{H}) generation=(?P<generation>{H}) programming=unavailable'),
    ('fpga_status_closed', rf'FES_NATIVE_FPGA_STATUS CLOSED ready=(?P<ready>[01]) owner=(?P<owner>{H}) generation=(?P<generation>{H}) programming=unavailable'),
    ('fpga_identity', rf'FES_NATIVE_FPGA_IDENTITY PASS package_id=(?P<package_id>[0-9a-f]{{64}}) build_id=(?P<build_id>[0-9a-f]{{32}}) abi=(?P<abi>{H}) base=(?P<base>{H}) map_sha256=(?P<map_sha256>[0-9a-f]{{64}}) owner=(?P<owner>{H}) generation=(?P<generation>{H}) data_held=1 (?:runtime_)?programming=unavailable'),
    ('reader_memory', rf'FES_NATIVE_FPGA_READER_MEMORY PINNED cpu_base=(?P<cpu_base>{H}) bus_base=(?P<bus_base>{H}) bytes=(?P<bytes>[0-9]{{1,10}}) arena_bytes=(?P<arena_bytes>[0-9]{{1,10}}) map_sha256=(?P<map_sha256>[0-9a-f]{{64}}) pattern_sha256=(?P<pattern_sha256>[0-9a-f]{{64}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) fpga_access=unqualified'),
    ('master_stop', rf'(?:NOTICE:[ \t]+)?FES_NANO_MASTER_STOP status=(?P<status>-?[0-9]{{1,10}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) reset=(?P<reset>{H}) reader_reads=(?P<reader_reads>[0-9]{{1,10}}) reader_polls=(?P<reader_polls>[0-9]{{1,10}}) disable=(?P<disable>[01]) probe_polls=(?P<probe_polls>[0-9]{{1,10}}) cancel=(?P<cancel>[01]) complete=(?P<complete>[01]) data=held'),
    ('ddr_firewall', rf'(?:NOTICE:[ \t]+)?FES_NANO_DDR_FIREWALL status=(?P<status>-?[0-9]{{1,10}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) reset=(?P<reset>{H}) mpfe_reset=(?P<mpfe_reset>[01]) reads=(?P<reads>[0-9]{{1,10}}) enable=(?P<enable>[0-9a-f]{{1,8}}) base=(?P<base>{H}) bytes=(?P<bytes>[0-9]{{1,10}}) access=unqualified'),
    ('ddr_region', r'(?:NOTICE:[ \t]+)?FES_NANO_DDR_REGION index=(?P<index>[0-7]) '+
        ' '.join(key+r'=(?P<'+key+r'>[0-9a-f]{1,8})' for key in ('base','baseext','limit','limitext'))),
    ('ddr_firewall_end', r'(?:NOTICE:[ \t]+)?FES_NANO_DDR_FIREWALL_END layout_known=(?P<layout_known>[01]) window_covered=(?P<window_covered>[01]) access=unqualified'),
    ('ddr_blocked', rf'FES_NATIVE_FPGA_DDR_READER BLOCKED owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) data_held=1 translation=unqualified drain=unqualified runtime_programming=unavailable'),
    ('storage', r'FES_STORAGE_PROBE board=de25-nano EL=1 counter=400000000Hz writes=disabled'),
    # SDHCI R2 supplies CID[127:8], exactly 15 bytes without CRC/end bits.
    ('read', rf'FES_NANO_SD READ bytes=(?P<bytes>[0-9]{{1,10}}) sha256=(?P<sha256>[0-9a-f]{{64}}) cid=(?P<cid>[0-9a-f]{{30}}) sectors=(?P<sectors>[0-9]{{1,20}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('contained', rf'FES_NANO_SD CONTAINED owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('resumed', rf'FES_NANO_SD RESUMED bytes=(?P<bytes>[0-9]{{1,10}}) sha256=(?P<sha256>[0-9a-f]{{64}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('sd_pass', rf'FES_NANO_SD PASS read-only owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('package_probe', r'FES_SD_PACKAGE_PROBE board=de25-nano EL=1 counter=400000000Hz writes=disabled activation=unavailable'),
    ('package_retained', rf'FES_NANO_PACKAGE RETAINED package_id=(?P<package_id>[0-9a-f]{{64}}) payload_sha256=(?P<payload_sha256>[0-9a-f]{{64}}) payload_bytes=(?P<payload_bytes>[0-9]{{1,10}}) build_id=(?P<build_id>[0-9a-f]{{32}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('package_contained', rf'FES_NANO_PACKAGE CONTAINED package_id=(?P<package_id>[0-9a-f]{{64}}) payload_sha256=(?P<payload_sha256>[0-9a-f]{{64}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('package_resumed', rf'FES_NANO_PACKAGE RESUMED package_id=(?P<package_id>[0-9a-f]{{64}}) payload_sha256=(?P<payload_sha256>[0-9a-f]{{64}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
    ('package_pass', rf'FES_NANO_PACKAGE PASS read-only package_id=(?P<package_id>[0-9a-f]{{64}}) payload_sha256=(?P<payload_sha256>[0-9a-f]{{64}}) payload_bytes=(?P<payload_bytes>[0-9]{{1,10}}) owner=(?P<owner>{H}) generation=(?P<sd_generation>[0-9]{{1,20}})'),
)]
DECIMAL = {'kind', 'valid', 'core', 'provider', 'result', 'status', 'counter',
           'cpu0', 'cpu1', 'cpu2', 'cpu3', 'setup', 'ready', 'bytes', 'sectors', 'sd_generation', 'payload_bytes','words','ordinal',
           'reads','identified','settled','bypass_controls_clear','arena_bytes','reader_reads','reader_polls','disable','probe_polls','cancel','complete',
           'index','layout_known','window_covered','mpfe_reset'}
TEXT = {'id', 'phase', 'sha256', 'cid', 'package_id', 'payload_sha256', 'build_id','map_sha256','pattern_sha256'}


def decode_native_boot(data, build_id, owner_seed, *, storage=None, package=None, hps_context=None, fpga_identity=None,
                       fpga_configuration=False, fpga_unready=False, fpga_status_only=False, configuration_storage=False,
                       smmu_observe=False, reader_memory=None):
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
    if package is not None and (not isinstance(package,dict) or
            set(package)!={'package_id','payload_sha256','payload_bytes','build_id'} or
            type(package['payload_bytes']) is not int or not 0<package['payload_bytes']<=8<<20 or
            any(not isinstance(package[key],str) or not re.fullmatch('[0-9a-f]{'+str(size)+'}',package[key])
                for key,size in (('package_id',64),('payload_sha256',64),('build_id',32)))):
        raise ValueError('independently checked package/payload/build identities required')
    if storage is not None and package is not None:
        raise ValueError('select exactly one native SD diagnostic')
    if (type(fpga_configuration) is not bool or type(fpga_unready) is not bool or type(fpga_status_only) is not bool or
            fpga_configuration and fpga_identity is None or fpga_unready and not fpga_configuration or
            fpga_status_only and (not fpga_configuration or fpga_unready)):
        raise ValueError('configuration/readiness observation requires the explicit FPGA identity diagnostic')
    if fpga_identity is not None:
        if (not isinstance(fpga_identity,dict) or set(fpga_identity)!={'sha256','package_id','build_id','control_abi','cpu_base','map_sha256'} or
                type(fpga_identity['control_abi']) is not int or fpga_identity['control_abi']!=(0x10203041 if reader_memory is not None else 0x10203040) or
                type(fpga_identity['cpu_base']) is not int or fpga_identity['cpu_base']!=0x20010400 or
                not isinstance(fpga_identity['map_sha256'],str) or not re.fullmatch('[0-9a-f]{64}',fpga_identity['map_sha256']) or
                int(fpga_identity['map_sha256'],16)==0):
            raise ValueError('independently checked source, endpoint and control-map identities required')
        if hps_context is not None or storage is not None or package is not None:
            raise ValueError('select exactly one native diagnostic')
        hps_context={key:fpga_identity[key] for key in ('sha256','package_id','build_id')}
    if hps_context is not None:
        if (not isinstance(hps_context,dict) or set(hps_context)!={'sha256','package_id','build_id'} or
                any(not isinstance(hps_context[key],str) or not re.fullmatch('[0-9a-f]{'+str(size)+'}',hps_context[key]) or
                    int(hps_context[key],16)==0 for key,size in (('sha256',64),('package_id',64),('build_id',32)))):
            raise ValueError('independently checked HPS source-context identities required')
        if storage is not None or package is not None:
            raise ValueError('select exactly one SD or HPS context diagnostic')
    if type(configuration_storage) is not bool or (configuration_storage and
            (fpga_identity is None or not fpga_configuration or fpga_unready or fpga_status_only)):
        raise ValueError('stored configuration requires the complete FPGA configuration diagnostic')
    if reader_memory is not None:
        expected={'format':1,'scope':'sealed-cold-ddr-reader-memory-v1',
            'package_id':fpga_identity['package_id'] if fpga_identity else None,
            'build_id':fpga_identity['build_id'] if fpga_identity else None,
            'cpu_base':0x95000000,'bus_base':0x95000000,'bytes':8192,'arena_base':0x95000000,'arena_bytes':2<<20,
            'cpu_attributes':'normal-uncached','lifetime':'until-reboot','fpga_addressing':'physical-identity-unqualified',
            'pattern':'le32-xor-nano-golden-v1','pattern_sha256':hashlib.sha256(reader_pattern()).hexdigest()}
        if (not configuration_storage or not isinstance(reader_memory,dict) or reader_memory!=expected or
                any(type(reader_memory[key]) is not int for key in ('format','cpu_base','bus_base','bytes','arena_base','arena_bytes'))):
            raise ValueError('independently checked cold DDR reservation and stored configuration required')
    source_generation=4 if configuration_storage else 1
    controlled = storage is not None or package is not None or configuration_storage
    if type(smmu_observe) is not bool or smmu_observe and (controlled or hps_context is not None or fpga_identity is not None):
        raise ValueError('SMMU observation requires its separate serial-only diagnostic')
    stages=STAGES
    if hps_context is not None:stages=STAGES[:-1]+('hps_context',)
    if fpga_identity is not None:
        if fpga_configuration:stages+=('fpga_query','fpga_status')
        if fpga_status_only:stages+=('fpga_status_closed',)
        elif fpga_unready:stages+=('fpga_unready',)
        else:
            stages+=('identity_open','identity_read')
            if fpga_configuration:
                stages+=tuple('identity_access_'+str(i)+'_'+phase for i in range(1,31) for phase in ('before','after'))
            stages+=('identity_close','identity_closed','fpga_identity')
    if configuration_storage:
        stages=STAGES[:7]+('sd_platform',)+STAGES[7:-1]+('configuration_package',)+stages[len(STAGES)-1:]
    elif controlled:
        suffix=(('storage','read','contained','resumed','sd_pass') if storage is not None else
                ('package_probe','package_retained','package_contained','package_resumed','package_pass'))
        stages=STAGES[:7]+('sd_platform',)+STAGES[7:-1]+suffix
    if reader_memory is not None:
        for before,added in (('hps_context','reader_memory'),('identity_open','smmu'),('identity_closed','master_stop')):
            index=stages.index(before);stages=stages[:index]+(added,)+stages[index:]
        index=stages.index('identity_open')
        stages=stages[:index]+('ddr_firewall',)+tuple('ddr_region_'+str(i) for i in range(8))+('ddr_firewall_end',)+stages[index:]
        stages+=('ddr_blocked',)
    if smmu_observe:stages=STAGES[:6]+('smmu',)+STAGES[6:]
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
            values = {key: value if key in TEXT else int(value, 10 if key in DECIMAL or name in ('reader_memory','master_stop','ddr_blocked','ddr_firewall') and key=='generation' else 16)
                      for key, value in match.groupdict().items()}
            if name == 'owned':
                name = values['phase']
            if name=='identity_transition':name='identity_'+values['phase']
            if name=='identity_access':name='identity_access_'+str(values['ordinal'])+'_'+values['phase']
            if name=='ddr_region':name+='_'+str(values['index'])
            if name=='sd_platform' and not controlled:
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
            if (line.startswith(('U-Boot ', 'FES_NANO_', 'FES_ENTRY', 'FES_NATIVE_GO_PROBE', 'FES_STORAGE_PROBE', 'FES_SD_PACKAGE_PROBE',
                                 'FES_NATIVE_HPS_CONTEXT','FES_NATIVE_FPGA_IDENTITY','FES_NATIVE_FPGA_UNREADY','FES_NATIVE_FPGA_STATUS','FES_NATIVE_FPGA_PROGRAMMED', 'Trying to boot from ', 'DRAM:', 'SOCFPGA_AGILEX5 #')) or
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
    if 'smmu' in observed:
        v=observed['smmu'];secure=bool(v['s_idr1']&(1<<31))
        identified=bool(v['idr0'] and v['idr0']!=0xffffffff and
                        all(v[key]!=0xffffffff for key in ('idr1','aidr','s_idr1')))
        settled=(v['cr0']==v['ack'] and not v['gbpa']&(1<<31) and
                 (not secure or v['s_cr0']==v['s_ack'] and not v['s_gbpa']&(1<<31) and not v['s_init']))
        clear=(identified and settled and not any(v[key] for key in ('cr0','gbpa','agbpa')) and
               (not secure or not any(v[key] for key in ('s_cr0','s_gbpa','s_agbpa'))))
        require(v['status']==0 and v['reads']==(26 if secure else 16), 'SMMU observation failed or has wrong read count')
        require(secure or not any(v[key] for key in ('s_cr0','s_ack','s_init','s_gbpa','s_agbpa')),
                'SMMU observation accessed an absent secure control bank')
        require(v['identified']==int(identified) and v['settled']==int(settled) and
                v['bypass_controls_clear']==int(clear), 'SMMU control interpretation differs from its raw registers')
        report['smmu_observation']=dict(v,secure_implemented=secure,access_qualified=False)
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
    if hps_context is not None and 'hps_context' in observed:
        value=observed['hps_context']
        require(all(value[key]==hps_context[key] for key in ('sha256','package_id','build_id')),
                'native HPS source context differs')
        require(seal is not None and value['owner']==seal['owner'] and value['generation']==source_generation,
                'native HPS source-context owner/generation differs')
    if fpga_identity is not None:
        value=observed.get('fpga_status')
        if value:
            ready=(value['state']==0 and bool(value['pins']&(1<<31)) and
                   value['soft']&3==3 and not value['soft']&8)
            require(value['status']==0 and value['words']==6 and
                    all(0<=value[key]<=0xffffffff for key in ('state','version','pins','soft','error_location','error_details')),
                    'SDM configuration response is incomplete or invalid')
            require(value['ready']==int(ready) and (fpga_status_only or ready==(not fpga_unready)),
                    'SDM configuration readiness differs from selected diagnostic')
        value=observed.get('fpga_unready')
        if value:
            require(seal is not None and value['owner']==seal['owner'] and value['generation']==1,
                    'unready probe lost original closed owner/generation')
        value=observed.get('fpga_status_closed')
        if value:
            require(seal is not None and value['owner']==seal['owner'] and value['generation']==1 and
                    'fpga_status' in observed and value['ready']==observed['fpga_status']['ready'],
                    'configuration-only query lost original closed scope or readiness')
        offsets=(0,4,8,12,16,20,24,28,32,64,68,72,76,56,60)
        expected=(0x46455345,0x10000,fpga_identity['control_abi'],
                  *((int(fpga_identity['build_id'],16)>>(32*i))&0xffffffff for i in range(4)),
                  0,0,0,0,0,0,0,0)
        for row in report['records']:
            if not row['stage'].startswith('identity_access_'):continue
            access=row['values'];ordinal=access['ordinal'];index=(ordinal-1)%15
            require(1<=ordinal<=30 and access['offset']==offsets[index] and
                    access['value']==(expected[index] if access['phase']=='after' else 0),
                    'FPGA register access offset/order/value differs')
        for phase,generation,reset in (('open',source_generation,15),('read',source_generation+1,13),('close',source_generation+1,13),('closed',source_generation+2,15)):
            value=observed.get('identity_'+phase)
            if value:
                require(seal is not None and value['owner']==seal['owner'] and value['generation']==generation and
                        value['reset']==reset and value['base']==fpga_identity['cpu_base'],
                        'FPGA identity transition lost owned control/data scope: '+phase)
        value=observed.get('fpga_identity')
        if value:
            require(all(value[key]==fpga_identity[key] for key in ('package_id','build_id','map_sha256')) and
                    value['abi']==fpga_identity['control_abi'] and value['base']==fpga_identity['cpu_base'],
                    'live FPGA identity differs from the compiled package/control map')
            require(seal is not None and value['owner']==seal['owner'] and value['generation']==source_generation+2,
                    'FPGA identity returned outside the same closed owner')
    if reader_memory is not None:
        firewall=observed.get('ddr_firewall');end=observed.get('ddr_firewall_end')
        if firewall:
            require(firewall['status']==0 and firewall['mpfe_reset']==0 and firewall['reads']==66 and firewall['base']==0x95000000 and firewall['bytes']==8192,
                    'DDR firewall sample failed or changed the fixed window')
            require(seal is not None and firewall['owner']==seal['owner'] and firewall['generation']==source_generation and firewall['reset']==15,
                    'DDR firewall observation lost closed ownership')
        regions=[observed.get('ddr_region_'+str(i)) for i in range(8)]
        if firewall and end and all(r is not None for r in regions):
            known=not firewall['enable']&~255;covered=False
            for r in regions:
                first=r['base']|(r['baseext']<<32);last=r['limit']|(r['limitext']<<32)
                enabled=bool(firewall['enable']&(1<<r['index']))
                known=known and all(0<=r[key]<=0xffffffff for key in ('base','baseext','limit','limitext')) and not (
                    r['base']&0xffff or r['limit']&0xffff!=0xffff or (r['baseext']|r['limitext'])&~255 or enabled and first>last)
                covered=covered or enabled and first<=0x95000000 and last>=0x95001fff
            covered=bool(known and covered)
            require(end['layout_known']==int(known) and end['window_covered']==int(covered),
                    'DDR firewall interpretation differs from its raw region words')
            report['ddr_firewall_observation']=dict(firewall,regions=regions,layout_known=bool(known),window_covered=covered,access_qualified=False)
        value=observed.get('reader_memory')
        if value:
            require(all(value[key]==reader_memory[key] for key in ('cpu_base','bus_base','bytes','arena_bytes','pattern_sha256')) and
                    value['map_sha256']==context_digest(reader_memory),'DDR reservation differs from its sealed map/pattern')
            require(seal is not None and value['owner']==seal['owner'] and value['generation']==source_generation,
                    'DDR reservation changed closed owner/generation')
            report['reader_memory_observation']=dict(value,fpga_access_qualified=False)
        value=observed.get('master_stop')
        if value:
            require(value['status']==0 and value['reader_reads']==88 and value['reader_polls']==1 and
                    value['disable']==0 and value['probe_polls']==2 and value['cancel']==0 and value['complete']==1,
                    'cold DDR idle master stop failed or differs from the fixed recipe')
            require(seal is not None and value['owner']==seal['owner'] and value['generation']==source_generation+1 and value['reset']==13,
                    'master stop lost the matched open control/held data scope')
            report['master_stop_observation']=dict(value,physical_ddr_drain_qualified=False)
        value=observed.get('ddr_blocked')
        if value:require(seal is not None and value['owner']==seal['owner'] and value['generation']==source_generation+2,
                         'DDR access block lost the reclosed identity owner/generation')
    if controlled:
        if 'sd_platform' in observed:
            require(observed['sd_platform']=={'setup':1,'ready':1},'controlled SD setup rejected')
        if configuration_storage and 'configuration_package' in observed:
            value=observed['configuration_package']
            require(seal is not None and value['owner']==seal['owner'] and value['sd_generation']==source_generation and
                    value['package_id']==fpga_identity['package_id'],'stored configuration package/owner/generation differs')
        phases=(('read','contained','resumed','sd_pass') if storage is not None else
                ('package_retained','package_contained','package_resumed','package_pass'))
        for phase,generation in zip(phases,(3,4,6,7)):
            if phase in observed:
                value=observed[phase]
                require(seal is not None and value['owner']==seal['owner'] and value['sd_generation']==generation,
                        f'{phase} SD owner/generation differs')
                if storage is not None and phase in ('read','resumed'):
                    require(value['bytes']==storage['bytes'] and value['sha256']==storage['sha256'],
                            f'{phase} recovery manifest differs')
                if package is not None:
                    require(all(value[key]==package[key] for key in ('package_id','payload_sha256')),
                            f'{phase} retained package identity differs')
                    for key in ('payload_bytes','build_id'):
                        if key in value:
                            require(value[key]==package[key],f'{phase} package {key} differs')
        if 'read' in observed:
            require(observed['read']['sectors']>0 and int(observed['read']['cid'],16)>0,
                    'SD card identity/capacity absent')
    names = tuple(row['stage'] for row in report['records'])
    report['loader_ready'] = not report['errors'] and names[:len(loader_stages)] == loader_stages
    report['complete'] = not report['errors'] and names == stages and not tail
    report['last_checkpoint'] = names[-1] if names else None
    return report


def decode_native_configuration(data, build_id, owner_seed, *, fpga_identity, configuration, from_storage=False, reader_memory=None, ddr_read=None):
    """Observe a contained phase-two transfer followed by reclosed live identity.

    The maps come from independently checked build receipts. Variable-length
    WRITE/completion polling is checked here, while the existing decoder still
    checks every SPL, CPU, ownership, readiness and identity observation. Neither
    completion establishes runtime activation or core launch/return acceptance.
    """
    if ddr_read is not None:
        if from_storage is not True or reader_memory is None:
            raise ValueError('active DDR read requires the exact stored configuration and pinned memory')
        from ddr_trace import decode
        return decode(data,build_id,owner_seed,identity=fpga_identity,memory=reader_memory,contract=ddr_read,rules=dict(RULES),
            base_decode=lambda prefix:decode_native_configuration(prefix,build_id,owner_seed,fpga_identity=fpga_identity,
                configuration=configuration,from_storage=from_storage,reader_memory=reader_memory))
    if type(from_storage) is not bool:raise ValueError('configuration source must be selected by the checked build profile')
    generation=4 if from_storage else 1
    fields={'format','scope','package_id','build_id','core_sha256','core_sha384','core_size',
            'base','size','dram_base','chunk_bytes','chunk_sha384'}
    def digest(value, length):return isinstance(value,str) and re.fullmatch('[0-9a-f]{'+str(length)+'}',value) is not None
    if (not isinstance(configuration,dict) or set(configuration)!=fields or not isinstance(fpga_identity,dict) or
        configuration['format']!=1 or type(configuration['format']) is not int or
        configuration['scope']!='sealed-cold-configuration-v1' or
        any(type(configuration[k]) is not int for k in ('core_size','base','size','dram_base','chunk_bytes')) or
        not 0<configuration['core_size']<=8<<20 or configuration['core_size']&3 or
        configuration['base']!=0x94800000 or configuration['size']!=8<<20 or configuration['dram_base']!=0x80000000 or
        not 0<configuration['chunk_bytes']<=0xffff0 or configuration['chunk_bytes']&63 or
        configuration['package_id']!=fpga_identity.get('package_id') or
        configuration['build_id']!=fpga_identity.get('build_id') or not digest(configuration['core_sha256'],64) or
        not digest(configuration['core_sha384'],96) or not isinstance(configuration['chunk_sha384'],list) or
        len(configuration['chunk_sha384'])!=(configuration['core_size']+configuration['chunk_bytes']-1)//configuration['chunk_bytes'] or
        not all(digest(value,96) for value in configuration['chunk_sha384'])):
        raise ValueError('independently checked sealed configuration map required')
    if not isinstance(data,(str,bytes,bytearray,memoryview)):
        raise TypeError('serial bytes or text required')
    raw=data.encode() if isinstance(data,str) else bytes(data)
    if len(raw)>MAX_BYTES:
        return decode_native_boot(raw,build_id,owner_seed,fpga_identity=fpga_identity,fpga_configuration=True,configuration_storage=from_storage,reader_memory=reader_memory)
    core=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_CORE_TRANSFER phase=(?P<phase>[A-Za-z]{{1,16}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) completed=(?P<completed>[0-9]{{1,10}}) pending=(?P<pending>[0-9]{{1,10}}) total=(?P<total>[0-9]{{1,10}}) base=(?P<base>{H})(?: counter=(?P<counter>{H}) hash_ticks=(?P<hash_ticks>{H}))?')
    programmed=re.compile(rf'FES_NATIVE_FPGA_PROGRAMMED PASS package_id=(?P<package_id>[0-9a-f]{{64}}) core_sha256=(?P<core_sha256>[0-9a-f]{{64}}) bytes=(?P<bytes>[0-9]{{1,10}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) data_held=1 activation=unavailable')
    engine=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_CORE_ENGINE credits=(?P<credits>[0-9]{{1,10}}) block_bytes=(?P<block_bytes>[0-9]{{1,10}}) next_job=(?P<next_job>[0-9]{{1,2}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}})')
    mailbox=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_CORE_MAILBOX phase=(?P<phase>[1-3]) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) completed=(?P<completed>[0-9]{{1,10}}) pending=(?P<pending>[0-9]{{1,10}}) job=(?P<job>[0-9]{{1,2}}) cin=(?P<cin>[0-9a-f]{{1,8}}) cout=(?P<cout>[0-9a-f]{{1,8}}) rin=(?P<rin>[0-9a-f]{{1,8}}) rout=(?P<rout>[0-9a-f]{{1,8}}) bell=(?P<bell>[0-9a-f]{{1,8}}) status=(?P<status>[0-9a-f]{{1,8}}) state=(?P<state>[0-9]) next=(?P<next>[0-9]) slot=(?P<slot>-1|[0-9]{{1,2}}) cmd_used=(?P<cmd_used>[0-9]{{1,2}}) resp_used=(?P<resp_used>[0-9]{{1,2}})')
    parts=raw.split(b'\n');extra=[];errors=[];last=None;offset=pending=accepted=0;complete=False;programmed_row=None
    total=configuration['core_size'];chunk=configuration['chunk_bytes'];owner=None
    timed=None;first_counter=first_hash=previous_counter=previous_hash=None;engine_row=None
    core_line=None;mailbox_sources=set()
    def require(condition, reason):
        if not condition:errors.append(reason)
    def completion_extents():
        cursor=offset;extents={(offset,pending)}
        for _ in range(min(3,(pending+chunk-1)//chunk)):
            cursor+=min(chunk,total-cursor)
            extents.add((cursor,accepted-cursor))
        return extents
    for index,fragment in enumerate(parts[:-1]):
        line=fragment.decode(errors='replace').rstrip('\r');match=core.fullmatch(line)
        if match:
            values={key:int(value,16 if key in ('owner','base','counter','hash_ticks') else 10) if key!='phase' else value
                    for key,value in match.groupdict().items() if value is not None}
            phase=values['phase']
            # Retain the first diagnostic's untimed grammar for its saved
            # evidence. A trace must use one complete grammar throughout.
            row_timed='counter' in values
            if timed is None:timed=row_timed
            require(timed==row_timed,'configuration trace mixes timed and untimed observations')
            if row_timed:
                counter,hash_ticks=values['counter'],values['hash_ticks']
                require(counter>0 and hash_ticks<=counter,'configuration timing exceeds its counter')
                if previous_counter is not None:
                    require(counter>=previous_counter and hash_ticks>=previous_hash and
                            hash_ticks-previous_hash<=counter-previous_counter,
                            'configuration clock or digest accounting regressed')
                else:first_counter,first_hash=counter,hash_ticks
                previous_counter,previous_hash=counter,hash_ticks
            require(values['generation']==generation and values['total']==total and values['base']==configuration['base'],
                    'configuration transfer changed generation, total or arena')
            if owner is None:owner=values['owner']
            require(values['owner']==owner and owner!=0,'configuration transfer changed owner')
            if phase=='arm':
                require(last is None and values['completed']==values['pending']==0,'configuration preparation repeated or reordered')
            elif phase=='start':
                require(last=='arm' and values['completed']==values['pending']==0,'configuration START repeated or reordered')
            elif phase=='write':
                require(values['completed'] in ({x for x,_ in completion_extents()} if last=='poll' else {offset}),
                        'configuration WRITE invented or skipped acknowledgements')
                offset=values['completed']
                amount=min(chunk,total-accepted)
                require(last in ('start','write','poll') and accepted<total and offset<=accepted and
                        values['pending']==accepted-offset+amount and 0<values['pending']<=4*chunk,
                        'configuration WRITE replay, gap or invalid window extent')
                accepted+=amount
                pending=values['pending']
            elif phase=='poll':
                require(values['completed'] in ({x for x,_ in completion_extents()} if last=='poll' else {offset}),
                        'configuration poll invented or skipped acknowledgements')
                offset=values['completed'];pending=accepted-offset
                require(last in ('write','poll') and values['pending']==pending and 0<pending<=4*chunk,
                        'configuration completion has no matching issued buffer')
            elif phase=='status':
                require(values['completed'] in ({x for x,_ in completion_extents()} if last=='poll' else {offset}),
                        'configuration final status invented or skipped acknowledgements')
                offset=values['completed'];pending=accepted-offset
                require(last in ('poll','status') and accepted==offset==total and values['pending']==pending==0,
                        'configuration final status preceded full transfer completion')
            elif phase=='complete':
                require(last=='status' and values['completed']==total and values['pending']==0,
                        'configuration completion is missing verified final status')
                complete=True
            else:require(False,'failed or unknown configuration phase')
            extra.append({'stage':'configuration_'+phase,'line':index+1,'values':values})
            last=phase;core_line=index+1;parts[index]=b''
        else:
            match=mailbox.fullmatch(line)
            if match:
                values={key:int(value,16 if key in ('owner','cin','cout','rin','rout','bell','status') else 10)
                        for key,value in match.groupdict().items()}
                phase=values['phase']
                require(engine_row is not None and core_line not in mailbox_sources and
                        last=={1:'start',2:'write',3:'poll'}[phase],
                        'configuration mailbox observation repeated or misplaced')
                mailbox_sources.add(core_line)
                extents={(0,0)} if phase==1 else completion_extents() if phase==3 else {(offset,pending)}
                require(values['owner']==owner and values['generation']==generation and values['job']<14 and
                        (values['completed'],values['pending']) in extents and
                        values['state']<=3 and values['next']<=3 and -1<=values['slot']<32 and
                        values['cmd_used']<=32 and values['resp_used']<=32,
                        'configuration mailbox observation has invalid scope or state')
                # Raw hardware indices/status are observations, not constraints
                # or transfer acknowledgements. Never advance authority here.
                extra.append({'stage':'configuration_mailbox','line':index+1,'values':values})
                parts[index]=b'';continue
            match=engine.fullmatch(line)
            if match:
                values={key:int(value,16 if key=='owner' else 10) for key,value in match.groupdict().items()}
                require(engine_row is None and last=='start','configuration engine observation repeated or misplaced')
                require(values['owner']==owner and values['generation']==generation and 0<values['credits']<=0x7fffffff and
                        0<values['block_bytes']<=0xffffffff and values['next_job']<14,
                        'configuration engine observation has invalid scope or limits')
                engine_row={'stage':'configuration_engine','line':index+1,'values':values}
                extra.append(engine_row);parts[index]=b'';continue
            match=programmed.fullmatch(line)
            if not match:continue
            values={key:int(value,16 if key=='owner' else 10) if key in ('owner','generation','bytes') else value for key,value in match.groupdict().items()}
            require(complete and last=='complete' and programmed_row is None,'native programmed marker preceded or repeated transfer completion')
            require(values['package_id']==configuration['package_id'] and values['core_sha256']==configuration['core_sha256'] and
                    values['bytes']==total and values['owner']==owner and values['generation']==generation,
                    'native programmed identity, bytes or ownership differ')
            programmed_row={'stage':'configuration_programmed','line':index+1,'values':values}
            extra.append(programmed_row);last='programmed';parts[index]=b''
    report=decode_native_boot(b'\n'.join(parts),build_id,owner_seed,fpga_identity=fpga_identity,fpga_configuration=True,configuration_storage=from_storage,reader_memory=reader_memory)
    observed={row['stage']:row for row in report['records']}
    source=observed.get('hps_context');query=observed.get('fpga_query')
    if extra:
        require(source is not None and source['line']<extra[0]['line'] and source['values']['owner']==owner,
                'configuration preceded or changed the closed HPS source')
    if query:
        require(programmed_row is not None and programmed_row['line']<query['line'] and all(row['line']<query['line'] for row in extra),
                'FPGA identity/readiness preceded contained configuration completion')
    if report['complete']:
        require(complete and programmed_row is not None,'native identity is missing contained configuration proof')
    report['errors'].extend(errors)
    report['complete']=report['complete'] and complete and programmed_row is not None and not report['errors']
    report['records']=sorted(report['records']+extra,key=lambda row:row['line'])
    report['last_checkpoint']=report['records'][-1]['stage'] if report['records'] else None
    report['configuration_transfer']={'complete':complete and programmed_row is not None and not errors,
                                      'completed_bytes':offset,'core_sha256':configuration['core_sha256'],
                                      'arena_base':configuration['base'],'arena_size':configuration['size']}
    if previous_counter is not None:
        report['configuration_timing']={'counter_frequency':400000000,'first_counter':first_counter,
                                        'last_counter':previous_counter,'hash_ticks':previous_hash,
                                        'interval_ticks':previous_counter-first_counter,
                                        'interval_hash_ticks':previous_hash-first_hash}
    return report


def decode_native_reader_configuration(data, build_id, owner_seed, *, fpga_identity, configuration, from_storage=False):
    """Require the contained transfer, exact reader recipe and reclosed identity."""
    captured=extract_reader_trace(data)
    report=decode_native_configuration(captured['stripped'],build_id,owner_seed,
                                      fpga_identity=fpga_identity,configuration=configuration,from_storage=from_storage)
    rows={row['stage']:row for row in report['records']}
    source=rows.get('hps_context')
    reader={'complete':False,'errors':[],'records':[]}
    if source:
        reader=validate_reader_trace(captured['events'],owner=source['values']['owner'],source_generation=4 if from_storage else 1,
                        abi=fpga_identity['control_abi'],build_id=fpga_identity['build_id'])
    errors=captured['errors']+reader['errors']
    def require(condition,message):
        if not condition:errors.append(message)
    if captured['events']:
        first=captured['events'][0]['line'];last=captured['events'][-1]['line']
        prior=rows.get('identity_access_30_after');closed=rows.get('identity_close')
        require(prior is not None and first>prior['line'],'reader started before verified idle identity')
        if closed:require(last<closed['line'],'reader continued after control close')
    receipt=captured['receipt'];identity=rows.get('fpga_identity')
    if receipt:
        require(identity is not None and identity['line']<receipt['line'],'reader receipt preceded reclosed identity')
        require(source is not None and receipt['values']=={'owner':source['values']['owner'],
                'generation':(4 if from_storage else 1)+2},'reader receipt changed closed ownership')
        require(reader['complete'],'native reader receipt lacks complete firmware observations')
    report['errors'].extend(errors)
    report['complete']=bool(report['complete'] and reader['complete'] and receipt and not report['errors'])
    extra=list(reader['records'])
    if receipt:extra.append({'stage':'fpga_reader','line':receipt['line'],'values':receipt['values']})
    report['records']=sorted(report['records']+extra,key=lambda row:row['line'])
    report['last_checkpoint']=report['records'][-1]['stage'] if report['records'] else None
    report['reader_cycle']={key:value for key,value in reader.items() if key not in ('records','errors')}
    report['reader_cycle']['complete']=report['complete']
    return report
