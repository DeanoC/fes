#!/usr/bin/env python3
"""Seal described menu firmware; no image selection or hardware programming."""
from __future__ import annotations
import argparse
import json
from pathlib import Path
import sys
if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import build_fes_menu as menu
from scripts import fes_build_common as board
from scripts.compiler_read_audit import python_source_guard
from scripts.core_package import encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = Path('build/oss/fes-menu-package')
RECIPE = 'scripts/build_fes_menu_package.py'
CONTRACT = 'cores/fes-common/generated/fes_application.vh'
SOURCES = tuple(p for p in menu.SOURCES if not p.endswith('/top.v')) + menu.DDR_SOURCES + (
    'cores/fes-common/rtl/fes_application_gp.v', 'cores/fes-menu/rtl/fes_menu_control.v',
    'cores/fes-menu/rtl/fes_menu_endpoint.v', 'cores/fes-menu/rtl/menu_top.v')
INPUTS = tuple(dict.fromkeys(tuple(p for p in menu.inputs_for('ddr') if p not in (
    menu.RECIPE, 'cores/fes-menu/rtl/top.v')) + (RECIPE, menu.RECIPE,
    'scripts/core_package.py', 'scripts/export_core_package.py', *SOURCES)))
OUTPUTS = (*menu.OUTPUTS, 'manifest.toml')


def build_commands(build_id, tools, *, seed=4):
    seed = menu.seed_for('ddr', seed)
    if board.HEX32_RE.fullmatch(build_id) is None:
        raise board.BuildError('invalid menu package build ID')
    if set(tools) != {'yosys','nextpnr-mistral'}:
        raise board.BuildError('menu package requires authenticated tools')
    program = (f"read_verilog -sv -I cores/fes-common/generated {' '.join(SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} top; "
        f"synth_intel_alm -nolutram -nodsp -top top; stat; write_json {OUTPUT}/synth.json")
    return ((str(tools['yosys']), '-p', program),
        (str(tools['nextpnr-mistral']), '--json', f'{OUTPUT}/synth.json',
         '--device', board.TARGET, '--qsf', menu.QSF, '--sdc', menu.evidence.SDC,
         '--freq', '74.25', '--seed', str(seed), '--router', 'gpu', '--gpu-device', '0',
         '--rbf', f'{OUTPUT}/core.rbf', '--compress-rbf', '--write', f'{OUTPUT}/routed.json',
         '--report', f'{OUTPUT}/timing.json', '--detailed-timing-report'))


def create_manifest(root, repository, revision, record):
    fields = json.loads(record)
    payload = Path(root)/OUTPUT/'core.rbf'
    return encode_manifest({
        'format':2,
        'core':{'id':'fes.menu', 'name':'FES Menu Display', 'version':'1.0.0',
                'description':'Runtime-owned fixed 720p DDR menu display'},
        'target':{'platform':'de10_nano','device':board.TARGET,'programming_profile':'fes-gp-v1'},
        'payload':{'file':'core.rbf','size':payload.stat().st_size,'sha256':board._sha256(payload)},
        'abi':{'id':'fes.application','major':1,'minor':0},
        'interfaces':[{'id':name,'major':1,'minor':0,'required':True} for name in (
            'fes.video.fixed-720p60','fes.memory.hps-ddr','fes.video.menu-display')],
        'build':{'id':build_identity(record),'repository':repository,'revision':revision,
            'recipe_sha256':fields['recipe_sha256'],
            'toolchain':'; '.join(f'{name} {identity}' for name,identity in sorted(fields['tools'].items()))}})


def record(root, repository, revision, identities, execution, seed):
    with python_source_guard(root, source_roots_for_inputs(INPUTS)):
        fields = {'format':1,'repository':repository,'revision':revision,'recipe':RECIPE,
            'recipe_sha256':board._sha256(board._regular_input(root,RECIPE)),
            'abi_definition':CONTRACT,'abi_definition_sha256':board._sha256(board._regular_input(root,CONTRACT)),
            'dependencies':{},'tools':identities,
            'parameters':{'device':board.TARGET,'top':'top','gpu_backend':'hip','router':'gpu',
                'gpu_architectures':board.FES_GPU_ARCHITECTURES,'seed':menu.seed_for('ddr',seed),
                'pixel_clock_hz':74250000,'reference_clock_hz':50000000,
                'format2_package':True,'menu_display':True,'ddr':True,'diagnostic_enable':False}}
        return encode_build_record(functional_record_fields(root,fields,
            source_roots_for_inputs(INPUTS),execution,pinned_inputs=INPUTS))


def build(root=ROOT, *, cache_root=None, package_output=None, seed=4):
    with python_source_guard(root, source_roots_for_inputs(INPUTS)):
        return _build(Path(root).resolve(),cache_root=cache_root,package_output=package_output,seed=seed)


def _build(root, *, cache_root, package_output, seed):
    seed = menu.seed_for('ddr', seed)
    repository, revision = board._require_clean_source(root,pinned_inputs=INPUTS)
    authenticated = menu.authenticate(root, cache_root, 'ddr')
    identities = {name:tool.identity for name,tool in authenticated.items()}
    invocation = FunctionalInvocation(authenticated,0)
    output = board._prepare_output(root,relative=OUTPUT,build_outputs=OUTPUTS)
    try:
        inputs = record(root,repository,revision,identities,invocation.inputs,seed)
        board._write_atomic(output/'build-inputs.json',inputs)
        commands = build_commands(build_identity(inputs),
            {name:authenticated[name].path for name in ('yosys','nextpnr-mistral')},seed=seed)
        for command,log in zip(commands,('yosys.log','nextpnr.log')):
            board._run_tool(command,root,output/log,output_relative=OUTPUT,
                env=invocation.env,audit_source_root=root)
        result = menu.validate_build_evidence(output,root,mode='ddr',menu_gp=True)
        invocation.verify()
        if {name:tool.identity for name,tool in menu.authenticate(root,cache_root,'ddr').items()} != identities:
            raise board.BuildError('menu package tool identity changed')
        if board._require_clean_source(root,pinned_inputs=INPUTS) != (repository,revision):
            raise board.BuildError('menu package source changed')
        if record(root,repository,revision,identities,invocation.inputs,seed) != inputs:
            raise board.BuildError('menu package functional inputs changed')
        manifest = create_manifest(root,repository,revision,inputs)
        board._write_atomic(output/'manifest.toml',manifest)
        result.update({'build_id':build_identity(inputs),'source_commit':revision,'tools':identities,
            'execution':invocation.inputs,'seed':seed,'format2_package':True,
            'inputs':{path:board._sha256(root/path) for path in INPUTS}})
        board._write_atomic(output/'build-summary.json',(json.dumps(result,indent=2)+'\n').encode())
        return export_package(manifest,output/'core.rbf', package_output or root/'build/packages')
    except Exception:
        board._invalidate_failed_artifact(output)
        manifest = output/'manifest.toml'
        if manifest.exists(): manifest.unlink()
        raise
    finally:
        invocation.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,default=ROOT)
    parser.add_argument('--cache-root',type=Path)
    parser.add_argument('--package-output',type=Path)
    parser.add_argument('--seed',type=int,choices=range(1,9),default=4)
    args = parser.parse_args()
    try: print(build(args.root,cache_root=args.cache_root,package_output=args.package_output,seed=args.seed))
    except (board.BuildError,ValueError) as exc:
        print(f'FES menu package: {exc}',file=sys.stderr); return 1
    return 0
if __name__ == '__main__': raise SystemExit(main())
