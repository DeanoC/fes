import copy,json,os,pathlib,sys
sys.path.insert(0,str(pathlib.Path.cwd()))
from scripts import de25_owner_image as image
source=pathlib.Path('/home/deano/fes/out/artifacts/de25-owner-image/829cf0353/candidate')
output=pathlib.Path('out/owner-image-reverify-829cf0353')
print(json.dumps(image.verify_candidate(source)),flush=True)
original=json.loads((source/'owner-image.json').read_text())
mutations={
 'open-admission':lambda r:r.update(normal_runtime_admission='open'),
 'wrong-recipe':lambda r:r.update(recipe_sha256='0'*64),
 'empty-assemblies':lambda r:r.update(assembly_hashes=[]),
 'inconsistent-assembly':lambda r:r['assembly_hashes'][0].update({'rootfs.ext4':'0'*64}),
 'wrong-selected-kernel':lambda r:r['kernel']['files']['Image'].update(sha256='0'*64),
 'changed-partition':lambda r:r['derived_boot_layout']['partitions'][0].update(start_sector=2049),
 'changed-fat-alias':lambda r:r['derived_boot_layout']['boot_files'][1].update(fat_name='BAD        '),
}
results={}
for name,mutate in mutations.items():
 root=output/name;root.mkdir()
 for path in [*original['files'],'first-boot-expectation.json']:
  dest=root/path;dest.parent.mkdir(parents=True,exist_ok=True);os.link(source/path,dest)
 record=copy.deepcopy(original);mutate(record)
 (root/'owner-image.json').write_text(json.dumps(record))
 try:image.verify_candidate(root)
 except ValueError as e:results[name]=str(e);print(name+': REFUSED: '+str(e),flush=True)
 else:raise AssertionError('accepted '+name)
(output/'result.json').write_text(json.dumps({'positive':'passed','refused_mutations':results,'hardware_access':'not-performed'},indent=2)+'\n')
