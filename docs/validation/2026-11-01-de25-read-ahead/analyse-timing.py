import re

def summary(text):
    declared=sorted(set(re.findall(r'Design Assistant \(Signoff\) Results - (\d+) of (\d+) Rules Failed',text)))
    if len(declared)!=1:raise ValueError('missing or ambiguous authoritative signoff summary')
    rows={}
    for line in text.splitlines():
        c=[v.strip() for v in line.split(';')]
        if len(c)<6 or not re.match(r'^[A-Z]+-\d+ - ',c[1]) or c[2] not in ('Critical','High','Medium','Low','Info'):continue
        if not c[3].isdigit() or not c[4].isdigit():raise ValueError('unsupported summary counts')
        rule=c[1].split(' ',1)[0]
        if rule in rows:raise ValueError('duplicate summary rule')
        rows[rule]={'rule':c[1],'count':int(c[3]),'waived':int(c[4]),'severity':c[2]}
    failed=[v for v in rows.values() if v['count']>v['waived']]
    if len(rows)!=int(declared[0][1]) or len(failed)!=int(declared[0][0]):raise ValueError('parsed rules disagree with authoritative signoff totals')
    if any(v['waived'] for v in rows.values()):raise ValueError('waived rules require independent review')
    return {'declared_failed_rules':int(declared[0][0]),'declared_total_rules':int(declared[0][1]),'nonzero_signoff_rules':failed}
