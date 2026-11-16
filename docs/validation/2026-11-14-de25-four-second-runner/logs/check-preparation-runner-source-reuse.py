from pathlib import Path
import hashlib,json
Q=Path.cwd();B=Path('/home/deano/fes/out/dev/de25-preparation-clock-proof/out/publication-boot');sha=lambda p:hashlib.file_digest(p.open('rb'),'sha256').hexdigest();load=lambda n:json.loads((B/n/'result.json').read_text())
s,e,b,u=[load(n) for n in ['native-sdk','native-entry','native-bl31','native-uboot']];paths={};generated={};external={}
for mapping in [s['source_sha256'],e['sources'],e['native_build_sources'],b['producer_sources'],u['native_handoff']['producer_sources']]:
 for name,digest in mapping.items():
  key=Path(name);p=key if key.is_absolute() else (B.parents[1]/key if key.parts[0]=='out' else Q/key)
  assert p.is_file() and sha(p)==digest,name
  target=external if key.is_absolute() else generated if key.parts[0]=='out' else paths;target[name]=digest
assert 'platform/embedded/firmware/README.md' not in paths
r=dict(status='passed-unchanged-boot-producer-sources-after-runner-preparation',matched_source_commit='5c5a9a5b48470c45dc450fb1834540209c333891',source_files_checked=len(paths),generated_build_inputs_checked=len(generated),external_inputs_checked=len(external),firmware_README_is_not_a_compiled_input=True,CPU_or_firmware_source_changed=False,hardware_actions=False)
(Q/'out/publication/preparation-runner-source-reuse-proof.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r))
