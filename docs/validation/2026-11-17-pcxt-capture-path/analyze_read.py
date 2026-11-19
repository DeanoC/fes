#!/usr/bin/env python3
"""Decompose every detailed SDRAM input path without replacing timing analysis."""
import argparse
import csv
import hashlib
import json
from pathlib import Path
import re


def decompose(work):
    rows = []
    reports = sorted((work/'conditional').glob('*/read-setup.rpt'))
    if len(reports) != 4:
        raise ValueError('expected four operating corners')
    for report in reports:
        paths = re.split(r'\nPath #\d+:', report.read_text(encoding='latin1'))[1:]
        if len(paths) != 16:
            raise ValueError('expected all sixteen DQ paths')
        for path in paths:
            summary, arrival = path.split('; Data Arrival Path',1)
            arrival = arrival.split('; Data Required Path',1)[0]
            fields = {}
            for line in summary.splitlines():
                cells=[x.strip() for x in line.split(';')[1:-1]]
                if len(cells)==2:
                    fields[cells[0]]=cells[1]
            steps=[]
            for line in arrival.splitlines():
                c=[x.strip() for x in line.split(';')[1:-1]]
                if len(c)==7:
                    try:
                        steps.append({'time':float(c[0]),'delay':float(c[1]),'type':c[3],'location':c[5],'element':c[6]})
                    except ValueError:
                        pass
            launch=next(x['time'] for x in steps if x['element']=='launch edge time')
            pin=next(x for x in steps if x['element']=='SDRAM_CLK')
            external=next(i for i,x in enumerate(steps) if x['type']=='iExt')
            data=[x for x in steps[external+1:] if x['type'] in ('IC','CELL')]
            row={'corner':report.parent.name,'port':fields['From Node'],
                 'capture':fields['To Node'],'slack_ns':float(fields['Slack'].split()[0]),
                 'forwarded_clock_latency_ns':round(pin['time']-launch,3),
                 'reference_tac_ns':steps[external]['delay'],
                 'dq_interconnect_ns':round(sum(x['delay'] for x in data if x['type']=='IC'),3),
                 'dq_cells_ns':round(sum(x['delay'] for x in data if x['type']=='CELL'),3),
                 'data_delay_ns':round(sum(x['delay'] for x in data),3),
                 'capture_location':data[-1]['location'],
                 'arrival_ns':float(fields['Data Arrival Time']),
                 'required_ns':float(fields['Data Required Time'])}
            if abs(row['required_ns']-row['arrival_ns']-row['slack_ns'])>0.002:
                raise ValueError('non-additive slack: '+str(row))
            if abs(pin['time']+row['reference_tac_ns']+row['data_delay_ns']-row['arrival_ns'])>0.003:
                raise ValueError('non-additive data delay')
            rows.append(row)
    channels=list(csv.DictReader((work/'conditional/channels.tsv').open(),delimiter='\t'))
    expected={(r['corner'],r['port']):r for r in channels if r['role']=='read' and r['kind']=='setup'}
    if len(expected)!=64 or len(rows)!=64:
        raise ValueError('missing or duplicated read rows')
    for r in rows:
        e=expected[(r['corner'],r['port'])]
        if r['capture']!=e['to'] or abs(r['slack_ns']-float(e['slack_ns']))>0.001 or abs(r['data_delay_ns']-float(e['data_delay_ns']))>0.003:
            raise ValueError('detailed report differs from channel table')
    return {'work':str(work),'rows':rows,'worst_read':min(rows,key=lambda r:r['slack_ns']),
            'report_sha256':{str(p.relative_to(work)):hashlib.sha256(p.read_bytes()).hexdigest() for p in reports+[work/'conditional/channels.tsv']}}


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('work',type=Path)
    p.add_argument('--output',required=True,type=Path)
    a=p.parse_args()
    result=decompose(a.work.resolve())
    a.output.write_text(json.dumps(result,indent=2,sort_keys=True)+'\n')
    print(json.dumps(result['worst_read'],sort_keys=True))
