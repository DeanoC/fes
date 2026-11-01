"""Derive immutable HPS-first source context without a native SPL hash cycle.

The descriptor binds the sealed FPGA inputs before the native image is built.
SPL and BL31 bind its digest through the existing cold chain. This is a local
source-bound diagnostic assertion, not measured/signed boot or admission.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
PROTOTYPE = ROOT/'sources/misteross/prototypes/de25_board_control'
SCOPE = 'source-bound-hps-first-context-v1'


def encode(context):
    return (json.dumps(context, sort_keys=True, separators=(',', ':'))+'\n').encode()


def digest(context):
    return hashlib.sha256(encode(context)).hexdigest()


def inputs():
    return [Path(__file__), PROTOTYPE/'verify.py', PROTOTYPE/'seal.py',
            ROOT/'sources/misteross/scripts/core_package.py',
            ROOT/'sources/misteross/scripts/export_core_package.py',
            ROOT/'sources/misteross/scripts/source_repository.py']


def derive(project):
    # The FPGA producer imports private modules named seal/scripts/verify and
    # adjusts sys.path. Isolate those imports from the firmware builders, whose
    # own verify module must remain authoritative after this call returns.
    result = subprocess.run([sys.executable, str(Path(__file__).resolve()), str(project)],
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
    if result.returncode:
        raise ValueError('sealed HPS context verification failed: '+result.stderr.decode(errors='replace')[-4096:])
    return json.loads(result.stdout)


def _derive(project):
    project = Path(project).absolute()
    if (not project.is_relative_to(ROOT/'out') or '..' in project.parts or
            any(p.is_symlink() for p in (project, *project.parents)) or not project.is_dir()):
        raise ValueError('HPS-first project must be an existing private directory below out/')
    sys.path.insert(0, str(PROTOTYPE))
    spec = importlib.util.spec_from_file_location('nano_hps_context_verifier', PROTOTYPE/'verify.py')
    verifier = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(verifier)
    source, _, _ = verifier.inputs_for(project)
    import seal
    if not source['embedded_control'].get('build_inputs_sha256') or seal.boot_order(source) != 'HPS FIRST':
        raise ValueError('HPS context requires a sealed HPS-first project')
    hps = project/'board/hps'
    pair = json.loads((hps/'hps-first-pair.json').read_text())
    record = json.loads((hps/'build-inputs.json').read_text())
    package = seal.read_package(hps/'packages'/pair['package_id'])
    abi = package.fields['abi']
    return {
        'format': 1, 'scope': SCOPE, 'platform': seal.PLATFORM, 'device': seal.DEVICE,
        'programming_profile': seal.HPS_PROFILE, 'core_id': package.fields['core']['id'],
        'abi': abi['id'], 'abi_major': abi['major'], 'abi_minor': abi['minor'],
        'package_id': pair['package_id'], 'build_id': pair['build_id'],
        'build_inputs_sha256': pair['build_inputs_sha256'], 'core_sha256': pair['core_sha256'],
        'core_size': pair['core_size'], 'hps_io_hash': pair['hps_io_hash'],
        'raw_sof_sha256': verifier.sha(hps/'project/output_files/golden_top.sof'),
        'original_pair_sha256': verifier.sha(hps/'hps-first-pair.json'),
        'pfg_identity': pair['pfg_identity'], 'hps_initialization': 'HPS FIRST',
        'overlay_revision': record['revision'], 'board_revision': source['board_fes_revision'],
        'control_abi': source['embedded_control']['control_abi'],
        'peripheral_offset': source['embedded_control']['peripheral_offset'],
        'peripheral_span': source['embedded_control']['span'],
    }


def checked(record, program_key='program'):
    keys = ('hps_context', 'hps_context_sha256', 'hps_project')
    if all(record.get(key) is None for key in keys):
        if record.get(program_key) == 'bootcontext':
            raise ValueError('native bootcontext image is missing its sealed HPS context')
        return None
    if any(record.get(key) is None for key in keys) or record.get(program_key) != 'bootcontext':
        raise ValueError('HPS source context requires the dedicated native bootcontext image')
    expected = derive(record['hps_project'])
    if record['hps_context'] != expected or record['hps_context_sha256'] != digest(expected):
        raise ValueError('native HPS context differs from the sealed FPGA source')
    return expected


if __name__ == '__main__':
    if len(sys.argv) != 2:
        raise SystemExit('usage: hps_context.py SEALED_PROJECT')
    sys.stdout.buffer.write(encode(_derive(sys.argv[1])))
