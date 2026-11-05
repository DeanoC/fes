#!/usr/bin/env python3
"""Re-resolve an identity-v2 PC/XT package using the selected main source.

The owner authenticates both source snapshots and the live main branch. This
helper runs the selected source's own resolver with building disabled, compares
its selection and provenance bytes, and reconstructs the exact archive. It
never contacts a kit.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
import core_dev_accept

HEX40 = re.compile('[0-9a-f]{40}\\Z')
HEX64 = re.compile('[0-9a-f]{64}\\Z')
REPOSITORY = 'https://github.com/DeanoC/fes.git'


def unique_pairs(pairs):
    value={}
    for key,item in pairs:
        if key in value: raise ValueError('duplicate source receipt key')
        value[key]=item
    return value


WORKER = r'''
import hashlib,json,sys
from pathlib import Path
root=Path(sys.argv[1]); output=Path(sys.argv[2])/'prepared'
sys.path.insert(0,str(root/'scripts'))
import bundle, core_dev
def no_build(*args,**kwargs): raise ValueError('selected package cache miss; build prohibited')
bundle._build_package=no_build
receipt=core_dev.prepare('fes.pcxt-dos',output)
print(json.dumps({'prepared_file':str(output/'prepared.json')},sort_keys=True))
'''



def compare_resolution(*, doc, sidecar, resolved_doc, selection_bytes, provenance_bytes,
                       resolved_selection, resolved_provenance, prepared_sha256,
                       archive_sha256, package_id, rbf_sha256, producer, selected):
    if (doc.get('core_id')!='fes.pcxt-dos' or doc.get('format')!=2
            or doc['archive']['sha256']!=archive_sha256 or doc['package_id']!=package_id
            or doc['selection']['payload_sha256']!=rbf_sha256
            or sidecar['original_revision']!=producer or sidecar['selected_revision']!=selected
            or sidecar['original_repository']!=REPOSITORY or sidecar['selected_repository']!=REPOSITORY
            or sidecar['original_source_path']!='sources/misteross' or sidecar['selected_source_path']!='sources/misteross'
            or any(x!=selected for x in doc['sources'].values())):
        raise ValueError('selected package provenance differs')
    if (selection_bytes!=resolved_selection or provenance_bytes!=resolved_provenance
            or resolved_doc!=doc):
        raise ValueError('current selected resolver result differs')
    return {'policy':'selected-identity-v2-functional-inputs-v1','prepared_sha256':prepared_sha256,
       'selection_sha256':doc['source_selection']['sha256'],'selected_commit':selected,
       'producer_commit':producer,'functional_inputs_sha256':sidecar['functional_inputs_sha256'],
       'original_record_sha256':sidecar['original_record_sha256'],'selected_record_sha256':sidecar['selected_record_sha256'],
       'package_id':package_id,'archive_sha256':archive_sha256,'rbf_sha256':rbf_sha256,
       'scope':'exact selected package only; producer remains original build source'}


def verify(*, prepared, prepared_sha256, source_checkout, producer_checkout,
           archive_sha256, package_id, rbf_sha256, producer, selected):
    if any(not isinstance(x,str) or not HEX64.fullmatch(x)
           for x in (prepared_sha256,archive_sha256,package_id,rbf_sha256)):
        raise ValueError('invalid selection or artifact digest')
    if any(not isinstance(x,str) or not HEX40.fullmatch(x) for x in (producer,selected)) or producer==selected:
        raise ValueError('invalid selected source identities')
    prepared=Path(prepared)
    if prepared.is_symlink() or hashlib.sha256(prepared.read_bytes()).hexdigest()!=prepared_sha256:
        raise ValueError('prepared receipt identity differs')
    core_dev_accept.candidate_arguments(prepared,{})
    doc=json.loads(prepared.read_bytes(),object_pairs_hook=unique_pairs)
    selection_path=prepared.parent/doc['selection']['path']
    sidecar_path=prepared.parent/doc['source_selection']['path']
    selection_bytes=selection_path.read_bytes();provenance_bytes=sidecar_path.read_bytes()
    sidecar=json.loads(provenance_bytes,object_pairs_hook=unique_pairs)
    with tempfile.TemporaryDirectory(prefix='pcxt-source-selection-') as temporary:
        output=Path(temporary);cache=output/'pycache'
        result=subprocess.run([sys.executable,'-I','-B','-X','pycache_prefix='+str(cache),'-c',WORKER,
                    str(source_checkout),str(output)],check=True,stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,timeout=80)
        result=json.loads(result.stdout,object_pairs_hook=unique_pairs)
        selected_output=output/'prepared'
        if result!={'prepared_file':str(selected_output/'prepared.json')}:
            raise ValueError('selected resolver output differs')
        selected_raw=(selected_output/'prepared.json').read_bytes()
        if selected_raw!=prepared.read_bytes():
            raise ValueError('current selected prepared receipt differs')
        evidence=compare_resolution(doc=doc,sidecar=sidecar,
            resolved_doc=json.loads(selected_raw,object_pairs_hook=unique_pairs),selection_bytes=selection_bytes,
            provenance_bytes=provenance_bytes,resolved_selection=(selected_output/selection_path.name).read_bytes(),
            resolved_provenance=(selected_output/sidecar_path.name).read_bytes(),prepared_sha256=prepared_sha256,
            archive_sha256=archive_sha256,package_id=package_id,rbf_sha256=rbf_sha256,
            producer=producer,selected=selected)
        if hashlib.sha256((selected_output/'core.fcore').read_bytes()).hexdigest()!=archive_sha256:
            raise ValueError('reconstructed selected archive differs')
    if hashlib.sha256(prepared.read_bytes()).hexdigest()!=prepared_sha256:
        raise ValueError('prepared receipt changed while verifying')
    return evidence


def main():
    p=argparse.ArgumentParser()
    for name in ('prepared','prepared-sha256','source-checkout','producer-checkout','archive-sha256',
                 'package-id','rbf-sha256','producer','selected'):
        p.add_argument('--'+name,required=True)
    print(json.dumps(verify(**vars(p.parse_args())),sort_keys=True))

if __name__=='__main__': main()
