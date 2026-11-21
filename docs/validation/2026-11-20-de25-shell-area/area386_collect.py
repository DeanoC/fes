#!/usr/bin/env python3
"""DE25 shell area study: collect whole-design ALMs, RAM, DSP, worst slacks and selected entity rows per run."""
import json, re, sys, os, glob
P='/data/projects/fes-de25-area'; J='/data/projects/fes-de25-pc386'
ENT=['soc_inst|ocm','soc_inst|mm_interconnect_0','soc_inst|mm_interconnect_2','soc_inst|subsys_hps',
     'soc_inst|subsys_periph|mm_interconnect_1','soc_inst|subsys_periph|mm_interconnect_2',
     'soc_inst|subsys_periph|fes_scanout|fes_scanout|system']
def entity(rpt):
    L=open(rpt).read().split('\n')
    i=[k for k,l in enumerate(L) if l.startswith('; Fitter Resource Utilization by Entity')][0]
    hdr=[c.strip() for c in L[i+2].split(';')]; col={h:n for n,h in enumerate(hdr)}
    out={}; path=[]
    for l in L[i+4:]:
        if l.startswith('+'): break
        c=l.split(';'); name=c[1]; d=(len(name)-len(name.lstrip()))//3; path=path[:d]+[name.strip().strip('|')]
        p='|'.join(path)
        g=lambda h: float(c[col[h]].split()[0])
        out[p]={'alm':g('ALMs needed [=A-B+C]'),'m20k':g('M20Ks'),'dsp':g('DSP Blocks needed [=A-B]'),'mem_alms':g('ALMs used for memory'),'regs':g('Dedicated Logic Registers')}
    return out
def sta(summary):
    worst={}
    for m in re.finditer(r"Type  : (\S+(?: \S+)*?) '([^']*)'\nSlack : (-?[\d.]+)", open(summary).read()):
        t=m.group(1).split()[0] if not m.group(1).startswith('Minimum') else 'Minimum Pulse Width'
        worst[t]=min(worst.get(t,1e9),float(m.group(3)))
        worst['setup:'+m.group(2)]=float(m.group(3)) if m.group(1)=='Setup' else worst.get('setup:'+m.group(2))
    return {k:v for k,v in worst.items() if v is not None}
def board(d):
    o=d+'/output_files/'; e=entity(o+'golden_top.fit.rpt'); tot=e[[k for k in e if k.count('|')==0][0]]
    net=open(d+'/fes_mailbox/pc386_system.v','rb').read(); import hashlib
    rows={k:e.get("|"+k) for k in ENT}
    extra={k:v for k,v in e.items() if ('rsp_fifo' in k.split('|')[-1] or k.endswith('agent_pipeline') or 'mux_pipeline' in k.split('|')[-1] or 'limiter_pipeline' in k.split('|')[-1] or k.endswith('hps2fpga_axi4_rl_adp_inst')) and k.count('|')<=4}
    return {'alms':tot['alm'],'m20k':tot['m20k'],'dsp':tot['dsp'],'netlist_sha256':hashlib.sha256(net).hexdigest(),'timing':sta(o+'golden_top.sta.summary'),'entities':rows,'detail':extra}
def envelope(d):
    f=json.load(open(d+'/fit.json')); t=json.load(open(d+'/timing.json')); r=f['resources']
    worst={}
    for c in t['corners']:
        for a,v in c['analyses'].items():
            if v.get('worst_slack_ns') is not None: worst[a]=min(worst.get(a,1e9),v['worst_slack_ns'])
    pick=lambda *names: next((r[n]['used'] for n in names if n in r), None)
    return {'alms':r['alms']['used'],'ram':pick('ram_blocks','m20k','memory_blocks'),'dsp':pick('dsp_blocks','dsp'),'timing_status':t['status'],'worst':worst,'resource_keys':sorted(r)}
res={'shell':{},'at':{}}
res['shell']['base']={f's{s}':board(f'{J}/board-j-out/combined/hps/project' if s==1 else f'{J}/pair-j-board-s{s}/project') for s in (1,2,3,4)}
for tag in ('ocm','rsp','lat'):
    res['shell'][tag]={}
    for s in (1,2,3,4):
        d=f'{P}/shell2-{tag}-s{s}/project'
        if os.path.exists(d+'/output_files/golden_top.sta.summary'): res['shell'][tag][f's{s}']=board(d)
for tag in ('base','dma2','fifo','kbcram','pitbcd'):
    res['at'][tag]={}
    for s in (1,2,3,4):
        d=f'{P}/fit-{tag}-s{s}'
        if os.path.exists(d+'/timing.json'): res['at'][tag][f's{s}']=envelope(d)
json.dump(res,sys.stdout,indent=1)
