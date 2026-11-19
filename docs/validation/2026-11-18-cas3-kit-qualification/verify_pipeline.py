#!/usr/bin/env python3
"""Prove unchanged controller deadline, then test phased capture and its mutations."""
import argparse, importlib.util, json, tomllib
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--module-root',type=Path,required=True)
p.add_argument('--reference-harness',type=Path,required=True)
p.add_argument('--yosys',type=Path,required=True)
p.add_argument('--work',type=Path,required=True)
a=p.parse_args();here=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('reference',a.reference_harness/'verify_windows.py')
r=importlib.util.module_from_spec(spec);spec.loader.exec_module(r)
v=r.verilator_identity('verilator');root=a.module_root.resolve();work=a.work.resolve();work.mkdir(parents=True,exist_ok=False)
assert r.sha(a.yosys)==tomllib.loads((root/'cores/fes-pcxt/quartus.lock').read_text())['yosys']['sha256']
controller=root/'cores/fes-ramtest/rtl/sdram_addon_port.v';memory=root/'cores/fes-pcxt/rtl/pcxt_memory.sv';ddio=root/'cores/fes-pcxt/sim/memory_top.sv'
script=work/'prove.ys'
script.write_text('read_verilog -formal -sv '+str(controller)+' '+str(here/'controller_contract.sv')+'\nprep -top write_contract -flatten\nmemory_map\nopt_clean\nasync2sync\ndffunmap\nchformal -lower\nsat -seq 8 -tempinduct -maxsteps 40 -prove-asserts -verify -timeout 120\n')
log=r.run(work,'formal',[a.yosys.resolve(),'-s',script]);assert 'Induction step proven: SUCCESS!' in log
capture=root/'cores/fes-ramtest/rtl/sdram_cas3_capture.v'
original=capture.read_text();assert original.count('always @(posedge clk_handoff)')==1
cases=[('positive',capture,0,None),('negative-early-handoff',work/'early-handoff.sv',1,'handoff selected wrong pin word'),('negative-next-sample',capture,1,'handoff selected wrong pin word'),('negative-no-freeze',work/'no-freeze.sv',1,'frontend lane mismatch')]
cases[1][1].write_text(original.replace('always @(posedge clk_handoff)','always @(posedge clk)'))
cases[3][1].write_text(original.replace('if (read_window[3]) read_word <= captured;', 'read_word <= captured;'))
for name,selected,rc,diagnostic in cases:
 top=root/'cores/fes-pcxt/sim/capture_pipeline_top.sv'
 if name=='negative-next-sample':
  top=work/'next-sample.sv';top.write_text((root/'cores/fes-pcxt/sim/capture_pipeline_top.sv').read_text().replace('.dq_fall(pin_capture)', '.dq_fall(dq_sample)'))
 obj=work/('obj-'+name)
 r.run(work,'build-'+name,[v['executable'],'--cc','--exe','--build','--top-module','pipeline_top','-Wno-fatal','-j','2','--Mdir',obj,controller,memory,selected,ddio,top,root/'cores/fes-pcxt/sim/capture_pipeline_tb.cpp'])
 log=r.run(work,name,[obj/'Vpipeline_top'],expected=rc)
 if diagnostic:assert diagnostic in log
 else:
  checked=json.loads(log.strip().splitlines()[-1]);assert checked['reads']==512
  if name=='positive':trace=checked
result={'formal':'PASS: controller temporal induction; CAS-3 six-cycle pin-observation deadline','trace':trace,'negative_early_handoff':'rejected after real initialization','negative_no_freeze':'rejected before frontend retirement','negative_missing_capture':'rejected after real initialization','verilator':v,'yosys_sha256':r.sha(a.yosys),'sources_sha256':{str(x.relative_to(root)):r.sha(x) for x in (controller,memory,capture,ddio,root/'cores/fes-pcxt/rtl/fes_pcxt_top.sv',root/'cores/fes-pcxt/rtl/pcxt_system.sv')},'fixtures_sha256':{str(x):r.sha(x) for x in [here/'controller_contract.sv',here/'verify_pipeline.py',root/'cores/fes-pcxt/sim/capture_pipeline_top.sv',root/'cores/fes-pcxt/sim/capture_pipeline_tb.cpp']},'classification':'CAS-3 controller induction and bounded experimental frontend-deadline trace; selected-word input contract requires separate physical timing validation'}
(work/'proof.json').write_text(json.dumps(result,indent=2,sort_keys=True)+'\n');print(json.dumps(result['trace']))
