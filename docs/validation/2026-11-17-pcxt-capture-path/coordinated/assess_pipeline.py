#!/usr/bin/env python3
"""Assess the used CL2 word window, retaining generic per-edge failures unchanged."""
import argparse,csv,hashlib,importlib.util,json,sys
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--fit',type=Path,required=True);p.add_argument('--proof',type=Path,required=True);p.add_argument('--output',type=Path,required=True)
a=p.parse_args();fit=a.fit.resolve();here=Path(__file__).resolve().parent;root=here.parents[3]
spec=importlib.util.spec_from_file_location('q',root/'sources/misteross/scripts/quartus_fes_pcxt.py');q=importlib.util.module_from_spec(spec);spec.loader.exec_module(q)
proof=json.loads(a.proof.read_text());native=q.timing_summary(fit,mailbox_contract=True,audio_profile=True)
rows=list(csv.DictReader((fit/'conditional/channels.tsv').open(),delimiter='\t'))
assert len(rows)==568
clocks={r['name']:r for r in native['clocks']}
phases={}
for n,phase in ((0,0),(1,8.125),(2,1.25)):
 name=f'memory_clock|general[{n}].gpll~PLL_OUTPUT_COUNTER|divclk';r=clocks[name]
 assert float(r['period_ns'])==10 and abs(float(r['waveform_ns'].split()[0])-phase)<0.001
 phases[str(n)]={'period_ns':10,'phase_ns':phase}
model=json.loads((fit.parent/'elaboration/elaborated.json').read_text())['modules']['top']
outputs=model['cells']['memory_clock']['connections']['outclk']
forward=[c for c in model['cells'].values() if c['type']=='altddio_out']
assert len(forward)==1 and forward[0]['connections']['outclock']==outputs[2:3]
capture_bits=[]
for bit in range(16):
 bits=model['netnames'][f'sdram_pads_{bit}__captured']['bits'];capture_bits+=bits
 regs=[c for c in model['cells'].values() if c['type']=='$dff' and c['connections']['Q']==bits]
 assert len(regs)==1 and regs[0]['connections']['CLK']==outputs[1:2] and int(regs[0]['parameters']['CLK_POLARITY'],2)==1
stage_bits=model['netnames']['system_storage_controller_dq_fall']['bits']
stages=[c for c in model['cells'].values() if c['type']=='$dff' and c['connections']['Q']==stage_bits]
assert len(stages)==1 and stages[0]['connections']['CLK']==outputs[0:1] and int(stages[0]['parameters']['CLK_POLARITY'],2)==0
assert stages[0]['connections']['D']==capture_bits
read=[r for r in rows if r['role']=='read'];assert len(read)==128
expected_relation=proof['trace']['selected_capture_ps']/1000-20
setup=[r for r in read if r['kind']=='setup'];hold=[r for r in read if r['kind']=='hold']
assert len(setup)==len(hold)==64
# BL1 first word is launched at READ+2 chip clocks. The following chip edge
# bounds its release (tOH), not a new word consumed by this transaction.
credits={round(expected_relation-float(r['relationship_ns']),3) for r in setup}
assert credits=={10.0},credits
window={'classification':'algebraic first-word transaction window; not a TimeQuest exception or seal',
 'setup_edge_credit_ns':10,'hold_edge_debit_ns':10,
 'expected_launch_to_capture_ns':expected_relation,
 'setup_min_ns':round(min(float(r['slack_ns'])+10 for r in setup),3),
 'hold_min_ns':round(min(float(r['slack_ns'])-10 for r in hold),3)}
captures={r['to'] for r in setup};assert len(captures)==16
endpoints=list(csv.DictReader((fit/'audit/endpoints.tsv').open(),delimiter='\t'))
handoff=[r for r in endpoints if r['from'] in captures and r['to'].startswith('system_storage_controller_dq_fall[')]
assert len(handoff)==128 and all(r['to_inverted']=='1' for r in handoff)
commands=[r for r in rows if r['role']=='write' and r['port'].startswith(('SDRAM_A[','SDRAM_BA[','SDRAM_n'))]
mins=lambda xs:{kind:min(float(r['slack_ns']) for r in xs if r['kind']==kind) for kind in ('setup','hold')}
section=(fit/'output_files/top.fit.rpt').read_text(encoding='latin1').split('; Bidir Pins',1)[1].split('; I/O Bank Usage',1)[0]
lines=[[s.strip() for s in line.split(';')[1:-1]] for line in section.splitlines()];header=next(x for x in lines if x and x[0]=='Name')
pins=[dict(zip(header,x)) for x in lines if x and x[0].startswith('SDRAM_DQ[')];assert len(pins)==16 and all(r['Input Register']=='yes' for r in pins)
result={'classification':'100 MHz source-frozen diagnostic; not physical acceptance','clock_phases':phases,'proof_sha256':q.sha(a.proof),
 'generic_read_per_edge_min_ns':mins(read),'used_read_word_window':window,'capture_to_controller_min_ns':mins(handoff),
 'direct_command_min_ns':mins(commands),'internal_checks_pass':native['reported_endpoint_checks_pass'],
 'pulse_width_pass':all(not r['failures'] for r in native['minimum_pulse_width'].values()),
 'internal_minima':{k:float(v['worst']['slack_ns']) if v['endpoints'] else None for k,v in native['by_corner_and_check'].items()},
 'audio_payload_min_ns':min(float(r['slack_ns']) for r in native['audio_payload']),
 'audio_interstage_min_ns':min(float(r['slack_ns']) for r in native['audio_interstage']),
 'mailbox_interstage_min_ns':min(float(r['slack_ns']) for r in native['mailbox_interstage']),
 'rbf_sha256':q.sha(fit/'output_files/top.rbf'), 'report_sha256':{str(f.relative_to(fit)):q.sha(f) for f in [fit/'conditional/channels.tsv',fit/'audit/endpoints.tsv',fit/'audit/clocks.tsv',fit/'output_files/top.fit.rpt',fit/'top.qsf',fit/'clocks.sdc',fit/'audio.sdc',fit/'mailbox.sdc']}}
result['selected_path_gates_pass']=result['internal_checks_pass'] and result['pulse_width_pass'] and window['setup_min_ns']>=0 and window['hold_min_ns']>=0 and all(v>=0 for v in result['direct_command_min_ns'].values())
a.output.write_text(json.dumps(result,indent=2,sort_keys=True)+'\n');print(json.dumps(result,indent=2))
