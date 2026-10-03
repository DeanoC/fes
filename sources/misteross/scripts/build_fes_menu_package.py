#!/usr/bin/env python3
"""Build and seal the described menu firmware for FES image selection."""
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
from scripts.search_placer_qor import SearchError, route_after_synth

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = Path('build/oss/fes-menu-package')
RECIPE = 'scripts/build_fes_menu_package.py'
MENU_TOOLCHAIN_LOCK = 'toolchains/ramtest.lock'
CONTRACT = 'cores/fes-common/generated/fes_application.vh'
SOURCES = tuple(p for p in menu.SOURCES if not p.endswith('/top.v')) + menu.DDR_SOURCES + (
    'cores/fes-common/rtl/fes_application_gp.v', 'cores/fes-menu/rtl/fes_menu_control.v',
    'cores/fes-menu/rtl/fes_menu_endpoint.v', 'cores/fes-menu/rtl/menu_top.v')
INPUTS = tuple(dict.fromkeys(tuple(p for p in menu.inputs_for('ddr') if p not in (
    menu.RECIPE, 'cores/fes-menu/rtl/top.v')) + (RECIPE, menu.RECIPE,
    'scripts/core_package.py', 'scripts/export_core_package.py', *SOURCES)))
OUTPUTS = (*menu.OUTPUTS, 'manifest.toml', 'qor-ranking.json')
PLACER_SEEDS = (5, 1, 2, 3, 4, 6, 7, 8)
PLACER_TIMING_WEIGHT = 10
PLACER_CRITICALITY_EXPONENT = 2
ROUTE_TIMEOUT_SECONDS = 1800
PLACER_QOR_CLOCKS = ((None, 74.25),)


def seed_order(seed):
    first = menu.seed_for('ddr', seed)
    return (first, *(candidate for candidate in PLACER_SEEDS if candidate != first))


def _route_placement(root, output, nextpnr, invocation, seed):
    return route_after_synth(
        nextpnr=nextpnr, fixture=output/'synth.json', dest=output, device=board.TARGET,
        qsf=root/menu.QSF, sdc=root/menu.evidence.SDC, freq='74.25',
        seeds=seed_order(seed), weights=(PLACER_TIMING_WEIGHT,),
        critexp=PLACER_CRITICALITY_EXPONENT, budget=len(PLACER_SEEDS),
        mode='first-pass', extra=('--router','gpu','--gpu-device','0'),
        required=PLACER_QOR_CLOCKS, timeout=ROUTE_TIMEOUT_SECONDS,
        env=invocation.env, audit_source_root=root)


def build_commands(build_id, tools, *, seed=5):
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


def _require_clean_source(root, *, identity_version=2):
    return board._require_clean_source(root, pinned_inputs=INPUTS, identity_version=identity_version)


def _authenticate_tools(root, *, cache_root=None):
    return menu.authenticate(root, cache_root, 'ddr')


def create_build_record(root, repository, revision, identities, *, identity_version=2, execution=None, seed=5):
    if identity_version != 2:
        raise board.BuildError('unsupported menu package identity version')
    with python_source_guard(root, source_roots_for_inputs(INPUTS)):
        fields = {'format':1,'repository':repository,'revision':revision,'recipe':RECIPE,
            'recipe_sha256':board._sha256(board._regular_input(root,RECIPE)),
            'abi_definition':CONTRACT,'abi_definition_sha256':board._sha256(board._regular_input(root,CONTRACT)),
            'dependencies':{},'tools':identities,
            'parameters':{'device':board.TARGET,'top':'top','gpu_backend':'hip','router':'gpu',
                'gpu_architectures':board.FES_GPU_ARCHITECTURES,'seed':menu.seed_for('ddr',seed),
                'seed_order':','.join(str(candidate) for candidate in seed_order(seed)),
                'pixel_clock_hz':74250000,'reference_clock_hz':50000000,
                'format2_package':True,'menu_display':True,'ddr':True,'diagnostic_enable':False}}
        return encode_build_record(functional_record_fields(root,fields,
            source_roots_for_inputs(INPUTS),execution,pinned_inputs=INPUTS))


def build(root=ROOT, *, cache_root=None, package_output=None, seed=5, identity_version=2):
    with python_source_guard(root, source_roots_for_inputs(INPUTS)):
        return _build(Path(root).resolve(),cache_root=cache_root,package_output=package_output,
            seed=seed,identity_version=identity_version)


def _build(root, *, cache_root, package_output, seed, identity_version):
    seed = menu.seed_for('ddr', seed)
    repository, revision = _require_clean_source(root,identity_version=identity_version)
    authenticated = _authenticate_tools(root,cache_root=cache_root)
    identities = {name:tool.identity for name,tool in authenticated.items()}
    invocation = FunctionalInvocation(authenticated,0)
    output = board._prepare_output(root,relative=OUTPUT,build_outputs=OUTPUTS)
    try:
        inputs = create_build_record(root,repository,revision,identities,
            identity_version=identity_version,execution=invocation.inputs,seed=seed)
        board._write_atomic(output/'build-inputs.json',inputs)
        commands = build_commands(build_identity(inputs),
            {name:authenticated[name].path for name in ('yosys','nextpnr-mistral')},seed=seed)
        board._run_tool(commands[0],root,output/'yosys.log',output_relative=OUTPUT,
            env=invocation.env,audit_source_root=root)
        try:
            winner = _route_placement(root, output, authenticated['nextpnr-mistral'].path,
                                      invocation, seed)
        except SearchError as exc:
            raise board.BuildError(str(exc)) from exc
        result = menu.validate_build_evidence(output,root,mode='ddr',menu_gp=True)
        result['route']['placer_seed'] = winner.seed
        result['route']['placer_heap_timingweight'] = winner.weight
        invocation.verify()
        if {name:tool.identity for name,tool in _authenticate_tools(root,cache_root=cache_root).items()} != identities:
            raise board.BuildError('menu package tool identity changed')
        if _require_clean_source(root,identity_version=identity_version) != (repository,revision):
            raise board.BuildError('menu package source changed')
        if create_build_record(root,repository,revision,identities,
                identity_version=identity_version,execution=invocation.inputs,seed=seed) != inputs:
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
    parser.add_argument('--seed',type=int,choices=range(1,9),default=5)
    parser.add_argument('--identity-version',type=int,choices=(2,),default=2)
    args = parser.parse_args()
    try: print(build(args.root,cache_root=args.cache_root,package_output=args.package_output,
        seed=args.seed,identity_version=args.identity_version))
    except (board.BuildError,ValueError) as exc:
        print(f'FES menu package: {exc}',file=sys.stderr); return 1
    return 0
if __name__ == '__main__': raise SystemExit(main())
