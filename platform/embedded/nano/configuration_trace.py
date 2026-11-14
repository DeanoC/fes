"""Decode bounded configuration-engine observations without granting boot authority."""
import re
from remap_trace import extract as extract_remap

H=r'[0-9a-f]{1,16}'
START_PHASES=(0,1,2,3,4,12,13,14,5,6,7,8,9,10,11)

def extract(raw, configuration, *, generation, previous=0, rearm=False, require_activation=False,
            require_programmed=True, expected_owner=None):
    """Shared transfer grammar for fixed probes and request-driven service calls.

    The caller supplies checked maps and independently verified boot ownership.
    This observes only the configuration protocol; it cannot confer boot,
    bridge, identity, DDR or runtime admission authority.
    """
    if not isinstance(raw,(bytes,bytearray)) or len(raw)>1<<20:
        raise ValueError('bounded configuration bytes required')
    raw=bytes(raw)
    raw,remap_observation=extract_remap(raw,configuration_scope=True)
    if (type(generation) is not int or not 0<generation<(1<<64)-2 or
        type(previous) is not int or not 0<=previous<generation or
        any(type(flag) is not bool for flag in (rearm,require_activation,require_programmed)) or
        expected_owner is not None and (type(expected_owner) is not int or not 0<expected_owner<1<<64)):
        raise ValueError('explicit configuration generation and scope required')
    activation=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_FABRIC_ACTIVATION owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) previous=(?P<previous>[0-9]{{1,20}}) rearm=(?P<rearm>[01]) complete=1 data_held=1')
    core=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_CORE_TRANSFER phase=(?P<phase>[A-Za-z]{{1,16}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) completed=(?P<completed>[0-9]{{1,10}}) pending=(?P<pending>[0-9]{{1,10}}) total=(?P<total>[0-9]{{1,10}}) base=(?P<base>{H})(?: counter=(?P<counter>{H}) hash_ticks=(?P<hash_ticks>{H}))?')
    programmed=re.compile(rf'FES_NATIVE_FPGA_PROGRAMMED PASS package_id=(?P<package_id>[0-9a-f]{{64}}) core_sha256=(?P<core_sha256>[0-9a-f]{{64}}) bytes=(?P<bytes>[0-9]{{1,10}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) data_held=1 activation=unavailable')
    engine=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_CORE_ENGINE credits=(?P<credits>[0-9]{{1,10}}) block_bytes=(?P<block_bytes>[0-9]{{1,10}}) next_job=(?P<next_job>[0-9]{{1,2}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}})')
    mailbox=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_CORE_MAILBOX phase=(?P<phase>[1-3]) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) completed=(?P<completed>[0-9]{{1,10}}) pending=(?P<pending>[0-9]{{1,10}}) job=(?P<job>[0-9]{{1,2}}) cin=(?P<cin>[0-9a-f]{{1,8}}) cout=(?P<cout>[0-9a-f]{{1,8}}) rin=(?P<rin>[0-9a-f]{{1,8}}) rout=(?P<rout>[0-9a-f]{{1,8}}) bell=(?P<bell>[0-9a-f]{{1,8}}) status=(?P<status>[0-9a-f]{{1,8}}) state=(?P<state>[0-9]) next=(?P<next>[0-9]) slot=(?P<slot>-1|[0-9]{{1,2}}) cmd_used=(?P<cmd_used>[0-9]{{1,2}}) resp_used=(?P<resp_used>[0-9]{{1,2}})')
    parts=raw.split(b'\n');extra=[];errors=[];last=None;offset=pending=accepted=0;complete=False;programmed_row=None
    total=configuration['core_size'];chunk=configuration['chunk_bytes'];owner=None
    timed=None;first_counter=first_hash=previous_counter=previous_hash=None;engine_row=None
    core_line=None;mailbox_sources=set();activation_row=None
    start_rows=[];isolation_result=None;isolation_io=None
    start_note=re.compile(r'(?:NOTICE:[ \t]+)?FES_NANO_START phase=(?P<phase>[0-9]{1,2}) status=(?P<status>-?[0-9]{1,11})')
    isolation_note=re.compile(r'(?:NOTICE:[ \t]+)?FES_NANO_ISOLATION status=(?P<status>-?[0-9]{1,11})')
    isolation_io_note=re.compile(r'(?:NOTICE:[ \t]+)?FES_NANO_ISOLATION_IO fill=(?P<fill>-?[0-9]{1,11}) branch=(?P<branch>[0-4]) cin=(?P<cin>[0-9a-f]{1,8}) cout=(?P<cout>[0-9a-f]{1,8}) bell=(?P<bell>[0-9a-f]{1,8}) polls=(?P<polls>[0-9]{1,5}) header=(?P<header>[0-9a-f]{1,4})')
    def require(condition, reason):
        if not condition:errors.append(reason)
    def completion_extents():
        cursor=offset;extents={(offset,pending)}
        for _ in range(min(3,(pending+chunk-1)//chunk)):
            cursor+=min(chunk,total-cursor)
            extents.add((cursor,accepted-cursor))
        return extents
    def start_observed():
        if [r['phase'] for r in start_rows]!=list(START_PHASES):return False
        values={r['phase']:r['status'] for r in start_rows}
        return ((isolation_io is None or isolation_result is not None) and
                (isolation_result is None or isolation_result['status']==0) and
                all(values[i]==0 for i in (0,1,2,3,12,13,14,8,9,10,11)) and
                all(values[i]>=0 for i in (4,5,6)) and values[7] in (2,3))
    for index,fragment in enumerate(parts[:-1]):
        line=fragment.decode(errors='replace').rstrip('\r')
        if line.startswith('FES_NANO_ISOLATION_IO') or line.startswith('NOTICE:') and 'FES_NANO_ISOLATION_IO' in line:
            match=isolation_io_note.fullmatch(line)
            require(match is not None,'malformed isolation transport observation')
            if match is not None:
                values={key:int(value,16 if key in ('cin','cout','bell','header') else 10)
                        for key,value in match.groupdict().items()}
                require(isolation_io is None and isolation_result is None and last=='start' and
                        [r['phase'] for r in start_rows]==[0,1,2,3] and
                        all(r['status']==0 for r in start_rows) and
                        -(1<<31)<=values['fill']<1<<31 and values['polls']<=10200,
                        'isolation transport repeated, unbounded or outside its START call')
                branch=values['branch']
                require((branch==0 and values['fill']!=0 and values['polls']==0 and
                         values['bell']==values['header']==0) or
                        (branch!=0 and values['fill']==0 and values['polls']>0 and
                         (branch==3 or values['header']==0) and
                         (branch!=1 or values['bell']!=1) and
                         (branch not in (2,3,4) or values['bell']==1)),
                        'isolation transport branch contradicts its observed values')
                if isolation_io is None:isolation_io=dict(values,line=index+1)
            parts[index]=b'';continue
        if line.startswith('FES_NANO_ISOLATION') or line.startswith('NOTICE:') and 'FES_NANO_ISOLATION' in line:
            match=isolation_note.fullmatch(line)
            require(match is not None,'malformed raw isolation result')
            if match is not None:
                status=int(match['status'])
                require(isolation_result is None and -(1<<31)<=status<1<<31 and last=='start' and
                        [r['phase'] for r in start_rows]==[0,1,2,3] and
                        all(r['status']==0 for r in start_rows),
                        'raw isolation result repeated or outside its START call')
                if isolation_result is None:isolation_result=dict(status=status,line=index+1)
                if isolation_io is not None:
                    branch=isolation_io['branch']
                    expected=(isolation_io['fill'] if branch==0 else -2047 if branch in (1,4) else
                              -4 if branch==2 else -(isolation_io['header']&0x7ff))
                    require(status==expected,'raw isolation result contradicts its transport branch')
            parts[index]=b'';continue
        if line.startswith('FES_NANO_START') or line.startswith('NOTICE:') and 'FES_NANO_START' in line:
            match=start_note.fullmatch(line)
            require(match is not None,'malformed configuration START observation')
            if match is not None:
                phase,status=int(match['phase']),int(match['status'])
                if phase==4 and isolation_result is not None:
                    require(status==(0 if isolation_result['status']==0 else -1),
                            'isolation wrapper result differs from its raw return')
                if phase==4 and isolation_io is not None:
                    require(isolation_result is not None,'isolation transport lacks its raw return')
                require(phase in START_PHASES and -(1<<31)<=status<1<<31 and len(start_rows)<len(START_PHASES) and
                        (phase==0 if not start_rows else phase in START_PHASES and start_rows[-1]['phase'] in START_PHASES and
                         START_PHASES.index(phase)>START_PHASES.index(start_rows[-1]['phase'])) and
                        (last=='start' or last=='FAIL' and phase==11 and start_rows and start_rows[-1]['phase']==10),
                        'configuration START observation repeated or misplaced')
                if len(start_rows)<len(START_PHASES):start_rows.append(dict(phase=phase,status=status,line=index+1))
            parts[index]=b'';continue
        match=activation.fullmatch(line)
        if match:
            values={key:int(value,16 if key=='owner' else 10) for key,value in match.groupdict().items()}
            require(activation_row is None and last=='status' and offset==total and not pending,
                    'fresh fabric activation preceded final status or repeated')
            require(values=={'owner':owner,'generation':generation,
                            'previous':previous,
                            'rearm':int(rearm)},'fabric activation changed owner/configuration lineage')
            activation_row={'stage':'fabric_activation','line':index+1,'values':values}
            extra.append(activation_row);parts[index]=b'';continue
        match=core.fullmatch(line)
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
                if start_rows:require(start_observed(),'configuration WRITE preceded complete START observations')
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
                if start_rows:require(start_observed(),'configuration engine preceded complete START observations')
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
    if require_activation and complete:require(activation_row is not None,'completed reload is missing fresh fabric activation')
    if expected_owner is not None and owner is not None:require(owner==expected_owner,'configuration owner differs from matched boot')
    if not require_programmed:require(programmed_row is None,'unexpected fixed-probe programmed marker')
    if complete and start_rows:require(start_observed(),'completed transfer has incomplete or failed START observations')
    return {'remap_observation':remap_observation,'parts':parts,'records':extra,'errors':errors,'complete':complete,
            'programmed':programmed_row,'activation':activation_row,'owner':owner,'offset':offset,
            'start_observation':{'records':start_rows,'complete':start_observed(),'isolation':isolation_result,
                                 'isolation_io':isolation_io,
                                 'hardware_authority':False},
            'timing':None if previous_counter is None else {
                'counter_frequency':400000000,'first_counter':first_counter,'last_counter':previous_counter,
                'hash_ticks':previous_hash,'interval_ticks':previous_counter-first_counter,
                'interval_hash_ticks':previous_hash-first_hash}}
