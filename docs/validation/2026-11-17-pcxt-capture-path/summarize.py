import argparse, csv, hashlib, json, sys
from pathlib import Path
p=argparse.ArgumentParser(description='Assess captured control/candidate timing; never promote')
p.add_argument('--evidence-root',required=True,type=Path)
p.add_argument('--controls-root',required=True,type=Path)
p.add_argument('--output',required=True,type=Path)
a=p.parse_args()
root=a.evidence_root.resolve()
here=Path(__file__).resolve().parent
worktree=here.parents[2]
sys.path[:0]=[str(worktree/'sources/misteross/scripts'), str(here)]
from quartus_fes_pcxt import timing_summary
from analyze_read import decompose

def sha(p):
 with p.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()

def assess(name, work):
 d=decompose(work)
 raw=list(csv.DictReader((work/'audit/endpoints.tsv').open(),delimiter='\t'))
 captures={r['capture'] for r in d['rows']}
 if len(captures)!=16:raise ValueError('missing or merged DQ capture endpoints')
 hand=[r for r in raw if r['from'] in captures and r['to'].startswith('system_storage_controller_dq_fall[')]
 if len(hand)!=128: raise ValueError('missing capture handoff rows: '+str(len(hand)))
 q=timing_summary(work,mailbox_contract=True,audio_profile=True)
 pins={}
 section=(work/'output_files/top.fit.rpt').read_text(encoding='latin1').split('; Bidir Pins',1)[1].split('; I/O Bank Usage',1)[0]
 lines=[[x.strip() for x in line.split(';')[1:-1]] for line in section.splitlines()]
 header=next(line for line in lines if line and line[0]=='Name')
 for line in lines:
  if line and line[0].startswith('SDRAM_DQ['):pins[line[0]]=dict(zip(header,line))
 if len(pins)!=16:raise ValueError('missing pin rows')
 corners=sorted(set(r['corner'] for r in d['rows']))
 out={'name':name,'work':str(work),'worst_read':d['worst_read'],
  'read_setup_by_corner':{c:min([r for r in d['rows'] if r['corner']==c],key=lambda r:r['slack_ns']) for c in corners},
  'read_hold_min_ns':min(float(r['slack_ns']) for r in csv.DictReader((work/'conditional/channels.tsv').open(),delimiter='\t') if r['role']=='read' and r['kind']=='hold'),
  'handoff_row_count':len(hand), 'handoff':{kind:min([r for r in hand if r['kind']==kind],key=lambda r:float(r['slack_ns'])) for kind in ('setup','hold')},
  'dq_input_registers':{n:p['Input Register'] for n,p in pins.items()},
  'reported_endpoint_checks_pass':q['reported_endpoint_checks_pass'],
  'internal_by_corner_and_check':q['by_corner_and_check'], 'minimum_pulse_width':q['minimum_pulse_width'],
  'audio_interstage_min_ns':min(float(r['slack_ns']) for r in q['audio_interstage']),
  'audio_payload_min_ns':min(float(r['slack_ns']) for r in q['audio_payload']),
  'mailbox_interstage_min_ns':min(float(r['slack_ns']) for r in q['mailbox_interstage']),
  'rbf_sha256':sha(work/'output_files/top.rbf'), 'reports_sha256':d['report_sha256']}
 files=[work/'top.qsf',work/'clocks.sdc',work/'audio.sdc',work/'mailbox.sdc',work/'elaborated.v',work/'audit.tcl',work/'audit.log',work/'audit/endpoints.tsv',work/'audit/clocks.tsv',work/'output_files/top.fit.rpt',work/'output_files/top.map.rpt',work/'output_files/top.sta.rpt']
 out['reports_sha256'].update({str(p.relative_to(work)):sha(p) for p in files})
 return out

runs=[assess('production-control-seed-'+str(n),a.controls_root.resolve()/f'seed-{n}') for n in (1,2,3)]
runs.append(assess('explicit-input-seed-1',root/'explicit-input-seed-1'))
result={'classification':'bounded 100 MHz forwarded-clock/DQ capture investigation; not physical qualification',
 'source_identity':json.loads((root/'source-identity.json').read_text()),'runs':runs,
 'reference_pr':'https://github.com/DeanoC/fes/pull/1940','reference_head':'70abc256fc2bcc0e99f58a6195f4864b21b63999',
 'hardware_run':False,'production_change':False,'sealed_package_created':False,
 'candidate_receipt_sha256':sha(root/'explicit-input-seed-1/receipt.json'),
 'invalid_request_receipt_sha256':sha(root/'fast-input-seed-1/receipt.json'),
 'candidate_database_inventory_sha256':sha(root/'candidate-postfit-database.json')}
a.output.write_text(json.dumps(result,indent=2,sort_keys=True)+'\n')
for r in runs: print(r['name'],r['worst_read']['slack_ns'],r['handoff']['setup']['slack_ns'],set(r['dq_input_registers'].values()),r['reported_endpoint_checks_pass'])
