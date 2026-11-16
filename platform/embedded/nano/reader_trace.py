"""Observe the fixed secure on-chip reader recipe; never grant DDR authority."""
import re

H=r'[0-9a-f]{1,16}'
ACCESS=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_IDENTITY_ACCESS phase=(?P<phase>before|after) ordinal=(?P<ordinal>[1-9][0-9]{{0,3}}) offset=(?P<offset>{H}) value=(?P<value>{H})')
WRITE=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_READER_WRITE phase=(?P<phase>before|after) offset=(?P<offset>{H}) value=(?P<value>{H})')
CYCLE=re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_READER_CYCLE phase=(?P<stage>[0-9]{{1,2}}) reader_generation=(?P<reader_generation>[0-9]{{1,20}}) cookie=(?P<cookie>[0-9]{{1,20}}) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) reset=(?P<reset>{H})')
RECEIPT=re.compile(rf'FES_NATIVE_FPGA_READER PASS owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) reader_generation=1 cookie=7 data_held=1 runtime_programming=unavailable')


def extract(data):
    if not isinstance(data,(str,bytes,bytearray,memoryview)):
        raise TypeError('serial bytes or text required')
    raw=data.encode() if isinstance(data,str) else bytes(data)
    report={'stripped':raw,'events':[],'receipt':None,'errors':[]}
    if len(raw)>1<<20:
        report['errors'].append('reader capture exceeds byte bound');return report
    parts=raw.split(b'\n')
    for index,part in enumerate(parts[:-1]):
        line=part.decode(errors='replace').rstrip('\r')
        for kind,pattern in (('access',ACCESS),('write',WRITE),('cycle',CYCLE),('receipt',RECEIPT)):
            match=pattern.fullmatch(line)
            if not match:continue
            values={key:value if key=='phase' else int(value,16 if key in ('offset','value','owner','reset') else 10)
                    for key,value in match.groupdict().items()}
            if kind=='access' and values['ordinal']<=30:break
            row={'kind':kind,'line':index+1,'values':values}
            if kind=='receipt':
                if report['receipt'] is not None:report['errors'].append('duplicate native reader receipt')
                else:report['receipt']=row
            else:report['events'].append(row)
            parts[index]=b'';break
        else:
            if 'FES_NANO_READER_' in line or line.startswith('FES_NATIVE_FPGA_READER'):
                report['errors'].append('malformed reader marker at line '+str(index+1))
    report['stripped']=b'\n'.join(parts)
    return report


class _Incomplete(Exception):pass
class _Invalid(Exception):pass


def validate(events, *, owner, source_generation, abi, build_id):
    """Check complete before/after pairs and exact command/ACK order.

    A prefix remains incomplete. Only state reads may repeat, at most 128 sets
    per state. This checks the C recipe, independently of any native PASS text.
    """
    if (type(owner) is not int or not 0<owner<1<<64 or type(source_generation) is not int or
        not 0<source_generation<(1<<64)-2 or abi!=0x10203040 or
        not isinstance(build_id,str) or not re.fullmatch('[0-9a-f]{32}',build_id)):
        raise ValueError('independently checked reader owner, generation and identity required')
    result={'complete':False,'errors':[],'records':[],'reads':0,'writes':0,
            'reader_generation':1,'cookie':7,'data_release':False,'ddr_drain':False}
    cursor=0;ordinal=31;write_number=0
    def require(ok,message):
        if not ok:result['errors'].append(message);raise _Invalid()
    def take(kind,phase=None):
        nonlocal cursor
        if cursor==len(events):raise _Incomplete()
        row=events[cursor];cursor+=1
        require(row['kind']==kind and (phase is None or row['values'].get('phase')==phase),
                'reader event repeated or reordered')
        return row
    def read(offset):
        nonlocal ordinal
        before=take('access','before');after=take('access','after')
        a,b=before['values'],after['values']
        require(a['ordinal']==b['ordinal']==ordinal and a['offset']==b['offset']==offset and a['value']==0 and
                0<=b['value']<=0xffffffff,'reader access ordinal, offset or value differs')
        for row in (before,after):
            result['records'].append({'stage':'reader_access_'+str(ordinal)+'_'+row['values']['phase'],
                                      'line':row['line'],'values':row['values']})
        ordinal+=1;result['reads']+=1
        return b['value']
    def write(offset,value):
        nonlocal write_number
        before=take('write','before');after=take('write','after')
        require(all(row['values']['offset']==offset and row['values']['value']==value for row in (before,after)),
                'reader command/beat changed or replayed')
        write_number+=1;result['writes']+=1
        for row in (before,after):
            result['records'].append({'stage':'reader_write_'+str(write_number)+'_'+row['values']['phase'],
                                      'line':row['line'],'values':row['values']})
    def state(status,pending,ack):
        for _ in range(128):
            values=[read(offset) for offset in (0x38,0x3c,0x1c,0x20,0x40,0x44,0x48,0x4c)]
            s,n,gen,hi,ackgen,ackhi,cookie,cookiehi=values
            require(s in (1,2,4) and not n&~0x10007 and n&7<=4 and gen==1 and hi==ackhi==cookiehi==0,
                    'reader state, pending extent or generation is invalid')
            if not ack:
                require(s!=4 and ackgen==cookie==0,'reader acknowledged a held address or partial burst')
            else:
                require(ackgen in (0,1) and cookie in (0,7),'reader final acknowledgement has wrong generation/cookie')
            if values==[status,pending,1,0,int(ack),0,7 if ack else 0,0]:return
        require(False,'reader observation exceeds the firmware poll bound')
    def checkpoint(stage,name):
        row=take('cycle');v=row['values']
        require(v=={'stage':stage,'reader_generation':1,'cookie':7,'owner':owner,
                    'generation':source_generation+1,'reset':13},'reader checkpoint lost owned control/data scope')
        result['records'].append({'stage':'reader_'+name,'line':row['line'],'values':v})
    try:
        write(0x24,1);write(0x28,0);write(0x34,1)
        state(1,0,False);checkpoint(9,'enabled')
        write(0x84,0x1000);write(0x88,1);write(0x80,0x0c)
        state(1,0x10000,False)
        require(read(0x8c)&4!=0 and read(0x90)==0x1000,'reader did not retain its offered AXI address')
        checkpoint(10,'held')
        write(0x2c,7);write(0x30,0);write(0x34,2)
        state(2,0x10000,False)
        require(read(0x8c)&4!=0 and read(0x90)==0x1000,'reader disable lost the held AXI address')
        checkpoint(11,'disabled')
        write(0x80,0x05);state(2,1,False)
        write(0x80,0x07);state(2,1,False)
        require(read(0x8c)>>16==1,'reader partial burst did not retain one beat')
        checkpoint(12,'partial')
        write(0x80,0x07);state(4,0,True);checkpoint(13,'drained')
        offsets=(0,4,8,12,16,20,24,28,32,64,68,72,76,56,60)
        values=(0x46455345,0x10000,abi,*((int(build_id,16)>>(32*i))&0xffffffff for i in range(4)),
                1,0,1,0,7,0,4,0)
        for _ in range(2):
            for offset,value in zip(offsets,values):
                require(read(offset)==value,'reader final identity/state differs')
        require(cursor==len(events),'reader has extra or repeated operations')
        result['complete']=True
    except (_Incomplete,_Invalid):pass
    return result
