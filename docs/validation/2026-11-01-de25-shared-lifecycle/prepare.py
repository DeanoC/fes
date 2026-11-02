"""Prepare the known non-DMA ZX81 diagnostic from verified compiler outputs."""
from pathlib import Path
import hashlib,json,re,shutil,struct,sys
source=Path(sys.argv[1]).resolve();output=Path(sys.argv[2]).resolve();revision=sys.argv[3]
assert not output.exists()
record=json.loads((source/'linux-programming.json').read_text())
inputs=json.loads((source/'inputs.json').read_text())
build=json.loads((source/'build.json').read_text())
assert record['fes_revision']==inputs['fes_revision']==build['fes_revision']==revision
assert record['hps_initialization']=='HPS FIRST' and inputs['computer']['hps_initialization']=='HPS FIRST'
assert build['device']=='A5EB013BB23BE4SCS' and not inputs['computer']['new_dma_master']
assert record['computer_build_id']==inputs['computer']['build_id']
for name,digest in build['report_sha256'].items():
 assert not Path(name).is_absolute() and '..' not in Path(name).parts
 assert hashlib.sha256((source/name).read_bytes()).hexdigest()==digest
text=(source/'output_files/golden_top.sta.summary').read_text()
rows=[(kind,float(slack)) for kind,slack in re.findall(r'Type\s*:\s*([^\n]+)\nSlack\s*:\s*([-+0-9.]+)',text)]
assert rows and any(k.startswith('Setup ') for k,_ in rows) and any(k.startswith('Hold ') for k,_ in rows)
assert all(slack>=0 for _,slack in rows), 'negative reported timing; preserve control and fix before board trial'
artifact=record['artifacts']['linux-boot.core.rbf'];payload=(source/'linux-boot.core.rbf').read_bytes()
assert len(payload)==artifact['size'] and hashlib.sha256(payload).hexdigest()==artifact['sha256']
recipe=hashlib.sha256(json.dumps(inputs['source_sha256'],sort_keys=True,separators=(',',':')).encode()).hexdigest()
manifest='''format = 2
[core]
id = "zx81"
name = "ZX81 DE25 shared-lifecycle diagnostic"
description = "Known BRAM computer; not a production factory or normal-kit acceptance claim."
version = "0.1.0"
[target]
platform = "de25_nano"
device = "A5EB013BB23BE4SCS"
programming_profile = "de25-hps-first-v1"
[payload]
file = "core.rbf"
size = SIZE
sha256 = "DIGEST"
[abi]
id = "fes.computer"
major = 1
minor = 0
'''.replace('SIZE',str(len(payload))).replace('DIGEST',artifact['sha256'])
for interface in ('fes.keyboard.hid','fes.media.zx81-tape','fes.video.fixed-720p60'):
 manifest+='\n[[interfaces]]\nid = '+json.dumps(interface)+'\nmajor = 1\nminor = 0\nrequired = true\n'
manifest+='\n[build]\nid = '+json.dumps(record['computer_build_id'])+'\nrepository = "https://github.com/DeanoC/fes"\nrevision = '+json.dumps(revision)+'\nrecipe_sha256 = '+json.dumps(recipe)+'\ntoolchain = "Quartus Prime Pro 26.3.0 Build 114"\n'
data=manifest.encode();identifier=hashlib.sha256(b'FES-CORE-PACKAGE-2\n'+struct.pack('<Q',len(data))+data+struct.pack('<Q',len(payload))+payload).hexdigest()
output.mkdir(parents=True);(output/'manifest.toml').write_bytes(data);(output/'core.rbf').write_bytes(payload)
(output.parent/(output.name+'-receipt.json')).write_text(json.dumps({'source_revision':revision,'package_id':identifier,'rbf_sha256':artifact['sha256'],'build_id':record['computer_build_id'],'manifest_sha256':hashlib.sha256(data).hexdigest(),'reported_timing_rows':rows,'classification':'unadmitted shared-lifecycle diagnostic; timing and hardware not qualified'},indent=2)+'\n')
print(identifier)
