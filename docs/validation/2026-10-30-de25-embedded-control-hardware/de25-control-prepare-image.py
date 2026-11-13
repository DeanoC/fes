from pathlib import Path
from decimal import Decimal
import hashlib,json,re,subprocess,zipfile
root=Path.cwd()
result=root/'out/de25-embedded-board-final'
board=result/'board'; hps=board/'hps'; project=hps/'project'
inputs=json.loads((hps/'inputs.json').read_text())
build=json.loads((hps/'build.json').read_text())
generation=json.loads((hps/'generation.json').read_text())
sha=lambda p: hashlib.sha256(Path(p).read_bytes()).hexdigest()
assert inputs['embedded_control']['board_fes_revision']=='c413228810a4f9e74318f161e75c952327e08da8'
head=subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()
assert head.startswith('cddc80f26') and inputs['embedded_control']['overlay_fes_revision']==head
assert inputs['embedded_control']['overlay_source_state']=='committed'
assert inputs['embedded_control']['build_id']==inputs['computer']['build_id']
assert generation['embedded_control']['maps']['jtag']['base']==0x110400
for path,digest in inputs['functional_snapshot_sha256'].items():
 assert sha(board/'functional-source'/path)==digest,path
raw=hps/build['artifact'];assert raw.is_file() and sha(raw)==build['sha256']
text=(project/'output_files/golden_top.sta.summary').read_text()
rows=[]
for block in re.split(r'\n\s*\n',text):
    kind=re.search(r'^Type\s*:\s*(.+)$',block,re.M)
    if not kind: continue
    slack=re.search(r'^Slack\s*:\s*([-+0-9.]+)$',block,re.M)
    tns=re.search(r'^TNS\s*:\s*([-+0-9.]+)$',block,re.M)
    assert slack, 'timing row without slack: '+kind.group(1)
    assert tns or ('Max Skew ' in kind.group(1) or kind.group(1).startswith('Net Delay ')), 'timing row without TNS: '+kind.group(1)
    rows.append((kind.group(1),Decimal(slack.group(1)),Decimal(tns.group(1)) if tns else None))
assert len(rows)==len(re.findall(r'^Type\s*:',text,re.M)) and len(rows)>=46
assert all(slack>=0 and (tns is None or tns==0) for _,slack,tns in rows), 'timing summary reports negative slack/TNS'
baseline=Path('/home/deano/.codex/worktrees/c134/fes/out/dev/de25-zx81-hardware/out/zx81-jtag/hps/project/output_files/golden_top.sta.summary')
baseline_types=set(re.findall(r'^Type\s*:\s*(.+)$',baseline.read_text(),re.M))
missing=baseline_types-{row[0] for row in rows}
assert not missing, 'baseline timing summary rows missing: '+repr(sorted(missing))
archive=board/'vendor.zip'; assert sha(archive)==inputs['vendor_archive_sha256']
spl=project/'software/u-boot/spl/u-boot-spl-dtb.hex'
member='Demonstration/SoC_FPGA/GHRD/software/u-boot/spl/u-boot-spl-dtb.hex'
with zipfile.ZipFile(archive) as z:
 assert hashlib.sha256(z.read(member)).hexdigest()==sha(spl)
out=root/'out/de25-embedded-physical-preparation';out.mkdir(exist_ok=False)
stock=out/'vendor-factory-reference.sof'
stock_member='Demonstration/SoC_FPGA/GHRD/output_files/golden_top_hps.sof'
with zipfile.ZipFile(archive) as z: stock.write_bytes(z.read(stock_member))
assert sha(stock)=='e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09'
full=out/'diagnostic-with-vendor-spl.sof'
cmd=['/data/altera_pro/26.3/quartus/bin/quartus_pfg','-c',str(raw),str(full),'-o','hps_path='+str(spl)]
with (out/'pfg-combine.log').open('wb') as log:
 subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=120)
assert full.is_file() and full.stat().st_size>0
with (out/'pfg-info.log').open('wb') as log:
 subprocess.run(['/data/altera_pro/26.3/quartus/bin/quartus_pfg','-i',str(full)],stdout=log,stderr=subprocess.STDOUT,check=True,timeout=60)
info=(out/'pfg-info.log').read_text(errors='replace');assert 'HPS_AFTER_INIT_DONE' in info
record={'status':'prepared','board_revision':inputs['embedded_control']['board_fes_revision'],
 'overlay_revision':inputs['embedded_control']['overlay_fes_revision'],'build_id':inputs['embedded_control']['build_id'],
 'abi':inputs['embedded_control']['control_abi'],'raw_sof_sha256':sha(raw),'programmed_sof_sha256':sha(full),
 'vendor_archive_sha256':sha(archive),'spl_sha256':sha(spl),'spl_member':member,
 'vendor_reference_sof_sha256':sha(stock),'vendor_reference_sof_member':stock_member,
 'generation_sha256':sha(hps/'generation.json'),'input_sha256':sha(hps/'inputs.json'),
 'sta_summary_sha256':sha(project/'output_files/golden_top.sta.summary'),'baseline_sta_sha256':sha(baseline),
 'baseline_summary_rows':len(baseline_types),'timing_summary_rows':len(rows),
 'minimum_reported_slack':str(min(row[1] for row in rows)),
 'scope':'temporary diagnostic SOF with on-chip responder; no package, DDR master, global containment or whole-board timing acceptance'}
(out/'preparation.json').write_text(json.dumps(record,indent=2)+'\n')
print(json.dumps(record,indent=2))
