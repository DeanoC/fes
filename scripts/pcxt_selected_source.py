#!/usr/bin/env python3
"""Recompute an identity-v2 selected PC/XT package without starting a build.

The caller authenticates both source checkouts and the live main branch. This
helper verifies the prepared receipt, both canonical records, and the original
package build record before reporting scoped provenance. It never contacts a kit.
"""
import argparse
import hashlib
import json
from pathlib import Path
import sys
import re
import tarfile
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent))

import artifact_cache
import bundle
import core_dev_accept
import recipes

HEX40 = re.compile('[0-9a-f]{40}\\Z')
HEX64 = re.compile('[0-9a-f]{64}\\Z')


def unique_pairs(pairs):
    value={}
    for key,item in pairs:
        if key in value: raise ValueError('duplicate source receipt key')
        value[key]=item
    return value


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
    provenance={}
    core_dev_accept.candidate_arguments(prepared,provenance)
    doc=json.loads(prepared.read_bytes(),object_pairs_hook=unique_pairs)
    sidecar_path=prepared.parent/doc['source_selection']['path']
    json.loads(sidecar_path.read_bytes(),object_pairs_hook=unique_pairs)
    source=core_dev_accept.source_selection(prepared.parent,doc['source_selection'],doc['sources'],
        {**__import__('tomllib').loads((prepared.parent/doc['selection']['path']).read_text()),'path':doc['selection']['path']},
        doc['package_id'],doc['selection']['payload_sha256'])
    if (doc.get('core_id')!='fes.pcxt-dos' or doc.get('format')!=2
            or doc['archive']['sha256']!=archive_sha256 or doc['package_id']!=package_id
            or doc['selection']['payload_sha256']!=rbf_sha256
            or source['original_revision']!=producer or source['selected_revision']!=selected
            or source['original_repository']!=bundle.FES_REPOSITORY
            or source['selected_repository']!=bundle.FES_REPOSITORY
            or source['original_source_path']!='sources/misteross'
            or source['selected_source_path']!='sources/misteross'
            or any(x!=selected for x in doc['sources'].values())):
        raise ValueError('selected package provenance differs')
    recipe=recipes.recipe_for('fes.pcxt-dos')
    selected_record=bundle.canonical_package_record(Path(source_checkout)/'sources/misteross',recipe=recipe)
    producer_record=bundle.canonical_package_record(Path(producer_checkout)/'sources/misteross',recipe=recipe)
    if (hashlib.sha256(selected_record).hexdigest()!=source['selected_record_sha256']
            or hashlib.sha256(producer_record).hexdigest()!=source['original_record_sha256']
            or artifact_cache.functional_key(selected_record)!=source['functional_inputs_sha256']
            or artifact_cache.functional_key(producer_record)!=source['functional_inputs_sha256']):
        raise ValueError('canonical build inputs do not match selected provenance')
    for record,expected in ((selected_record,selected),(producer_record,producer)):
        fields=json.loads(record)
        if fields.get('repository')!=bundle.FES_REPOSITORY or fields.get('revision')!=expected or fields.get('source_path')!='sources/misteross':
            raise ValueError('canonical build record source differs')
    archive=prepared.parent/'core.fcore'
    with tempfile.TemporaryDirectory(prefix='pcxt-source-selection-') as temporary:
        temp=Path(temporary); pkg=temp/package_id;pkg.mkdir()
        with tarfile.open(archive,mode='r:*') as tf:
            members=tf.getmembers()
            if len(members)!=2 or {m.name for m in members}!={'manifest.toml','core.rbf'} or any(not m.isfile() for m in members):
                raise ValueError('unexpected PC/XT package members')
            for m in members:
                if m.size<1 or m.size>(32<<20):raise ValueError('package member size invalid')
                (pkg/m.name).write_bytes(tf.extractfile(m).read())
                (pkg/m.name).chmod(0o444)
        pkg.chmod(0o555)
        record=temp/'build-inputs.json';record.write_bytes(producer_record);record.chmod(0o444)
        inspected=bundle._inspect_package_candidate(Path(source_checkout)/'sources/misteross',pkg,record,recipe=recipe)
        if (inspected['package_id']!=package_id or inspected['core_rbf_sha256']!=rbf_sha256
                or inspected['manifest_sha256']!=doc['selection']['manifest_sha256']):
            raise ValueError('producer package does not match verified build record')
    if hashlib.sha256(prepared.read_bytes()).hexdigest()!=prepared_sha256:
        raise ValueError('prepared receipt changed while verifying')
    return {'policy':'selected-identity-v2-functional-inputs-v1','prepared_sha256':prepared_sha256,
       'selection_sha256':doc['source_selection']['sha256'], 'selected_commit':selected,
       'producer_commit':producer,'functional_inputs_sha256':source['functional_inputs_sha256'],
       'original_record_sha256':source['original_record_sha256'],'selected_record_sha256':source['selected_record_sha256'],
       'package_id':package_id,'archive_sha256':archive_sha256,'rbf_sha256':rbf_sha256,
       'scope':'exact selected package only; producer remains original build source'}


def main():
    p=argparse.ArgumentParser()
    for name in ('prepared','prepared-sha256','source-checkout','producer-checkout','archive-sha256',
                 'package-id','rbf-sha256','producer','selected'):
        p.add_argument('--'+name,required=True)
    print(json.dumps(verify(**vars(p.parse_args())),sort_keys=True))

if __name__=='__main__': main()
