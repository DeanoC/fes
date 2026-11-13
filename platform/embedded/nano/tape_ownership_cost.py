"""Read-only ownership subprofile decoder; never an access/launch receipt.

The caller must first validate the complete tape failure and matched native
boot. This helper only joins four inner sample records to that parent's owns
record. Estimates include instrumentation and are not an elapsed-time partition.
"""
import re

PARTS=('before','usb','registers','after')
MARKER='FES_NANO_TAPE_OWNERSHIP_COST'
RECORD=re.compile(MARKER+r' owner=([0-9a-f]+) generation=(\d+) kind=(\w+) '
    r'samples=(\d+) period=(\d+) ticks=([0-9a-f]+) max=([0-9a-f]+) '
    r'clock_invalid=(\d+) complete=(\d+) hz=(\d+) parent_calls=(\d+) parent_samples=(\d+)')


def require(value,message):
    if not value:raise ValueError(message)


def decode(raw,parent):
    require(type(raw) is bytes and 0<len(raw)<=8<<20,'bounded raw console required')
    require(parent.get('status')=='diagnostic-profile-observed','validated failed transfer required')
    owner=parent['owner'];generation=parent['generation']
    require(type(owner) is int and 0<owner<1<<64 and type(generation) is int and 0<generation<1<<64,
        'owned failure required')
    parents=[v for v in parent['costs'] if v['kind']=='owns']
    require(len(parents)==1,'unique outer ownership profile required')
    outer=parents[0]
    require((outer['owner'],outer['generation'],outer['hz'],outer['period'],outer['complete'],outer['clock_invalid'])==
        (owner,generation,400000000,64,0,0),'matched outer ownership profile required')
    require(type(outer['calls']) is int and 0<=outer['calls']<1<<32 and
        outer['samples']==(outer['calls']+63)//64,'valid outer sampling required')
    rows=[]
    for number,line in enumerate(raw.splitlines(),1):
        text=re.sub(r'^NOTICE:[ \t]*','',line.decode('ascii',errors='replace').strip())
        if not text.startswith(MARKER):continue
        m=RECORD.fullmatch(text);require(m is not None,'malformed ownership sample')
        a=m.groups()
        row=dict(line=number,owner=int(a[0],16),generation=int(a[1]),kind=a[2],samples=int(a[3]),
            period=int(a[4]),ticks=int(a[5],16),maximum=int(a[6],16),clock_invalid=int(a[7]),
            complete=int(a[8]),hz=int(a[9]),parent_calls=int(a[10]),parent_samples=int(a[11]))
        rows.append(row)
    require([v['kind'] for v in rows]==list(PARTS),'one ordered four-part ownership profile required')
    remaining=outer['samples']
    for row in rows:
        require((row['owner'],row['generation'],row['hz'],row['period'],row['complete'],row['clock_invalid'],
            row['parent_calls'],row['parent_samples'])==(owner,generation,outer['hz'],64,0,0,outer['calls'],outer['samples']),
            'foreign or invalid ownership sample')
        require(0<=row['samples']<=remaining and 0<=row['maximum']<=row['ticks']<1<<64,
            'invalid ownership sample bounds')
        require(row['samples'] or row['ticks']==row['maximum']==0,'cost without sample')
        require(outer['line']<row['line']<parent['failure']['line'],'ownership sample outside outer failure group')
        remaining=row['samples']
        row['sampled_mean_seconds']=row['ticks']/row['hz']/row['samples'] if row['samples'] else None
        row['scaled_outer_call_estimate_seconds']=(row['sampled_mean_seconds']*outer['calls']
            if row['samples'] and row['samples']==outer['samples'] else None)
    require(sum(v['ticks'] for v in rows)<=outer['ticks'],'inner samples exceed outer sampled interval')
    return dict(status='ownership-subprofile-observed',owner=owner,generation=generation,parts=rows,
        sample_estimates_only=True,instrumentation_overhead_included=True,elapsed_partition=False,
        performance_qualified=False,normal_return_verified=False,hardware_authority=False)
