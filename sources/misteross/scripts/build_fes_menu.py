#!/usr/bin/env python3
"""Build the DDR-free menu test-pattern diagnostic with authenticated OSS tools."""
from __future__ import annotations
import argparse
import json
from pathlib import Path
import sys
if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import fes_build_common as board
from scripts import fes_de10nano_evidence as evidence
from scripts.compiler_read_audit import guard_functional_source
from scripts.export_core_package import encode_build_record, build_identity, functional_record_fields
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = Path('build/oss/fes-menu-pattern')
RECIPE = 'scripts/build_fes_menu.py'
CONTRACT = 'cores/fes-menu/pattern-contract.toml'
QSF = 'cores/fes-splash/constraints.qsf'
SOURCES = ('cores/fes-splash/rtl/pixel_pll.v', 'cores/fes-menu/rtl/fes_menu_reader.v',
           'cores/fes-menu/rtl/fes_menu_pattern_memory.v', 'cores/fes-menu/rtl/fes_menu_video.v',
           'cores/fes-menu/rtl/top.v')
INPUTS = (RECIPE, CONTRACT, QSF, evidence.SDC, 'toolchain.lock',
          'scripts/fes_build_common.py', 'scripts/fes_de10nano_evidence.py',
          'scripts/functional_execution.py', 'scripts/compiler_read_audit.py', *SOURCES)
OUTPUTS = ('synth.json', 'routed.json', 'timing.json', 'core.rbf', 'yosys.log',
           'nextpnr.log', 'build-inputs.json', 'build-summary.json')
ORDINARY = frozenset({'MISTRAL_BUF','MISTRAL_CLKENA','MISTRAL_COMB','MISTRAL_FF','MISTRAL_IO',
                      'MISTRAL_M10K','MISTRAL_M10K_TDP'})
FORBIDDEN = frozenset({'MISTRAL_MLAB','MISTRAL_MUL9X9','MISTRAL_MUL18X18',
                       'MISTRAL_MUL18X19','MISTRAL_MUL18X19_COMBINED','MISTRAL_MUL27X27',
                       'cyclonev_hps_interface_fpga2sdram',
                       'cyclonev_hps_interface_mpu_general_purpose'})


def build_commands(tools):
    if set(tools) != {'yosys','nextpnr-mistral'}:
        raise board.BuildError('menu pattern requires authenticated Yosys and nextpnr')
    program = (f"read_verilog -sv {' '.join(SOURCES)}; "
               f"synth_intel_alm -nolutram -nodsp -top top; stat; write_json {OUTPUT}/synth.json")
    return ((str(tools['yosys']), '-p', program),
            (str(tools['nextpnr-mistral']), '--json', f'{OUTPUT}/synth.json',
             '--device', board.TARGET, '--qsf', QSF, '--sdc', evidence.SDC,
             '--freq', '74.25', '--seed', '1', '--router', 'gpu', '--gpu-device', '0',
             '--rbf', f'{OUTPUT}/core.rbf', '--compress-rbf',
             '--write', f'{OUTPUT}/routed.json', '--report', f'{OUTPUT}/timing.json',
             '--detailed-timing-report'))


def validate_build_evidence(output, root):
    # A test-pattern artifact must not acquire a DDR or GP block in either graph.
    for filename in ('synth.json', 'routed.json'):
        counts = board._cell_counts(board._read_json(Path(output)/filename, filename))
        for name in ('cyclonev_hps_interface_fpga2sdram',
                     'cyclonev_hps_interface_mpu_general_purpose'):
            if counts.get(name, 0):
                raise board.BuildError(f'pattern diagnostic uses forbidden {name}')
    return evidence.validate_build_evidence(output, root, ordinary_resources=ORDINARY,
        required_resources={'cyclonev_hps_interface_peripheral_i2c':1},
        forbidden_resources=FORBIDDEN, required_zero_resources=frozenset({'cyclonev_oscillator'}))


@guard_functional_source
def record(root, repository, revision, identities, execution):
    fields = {'format':1, 'repository':repository, 'revision':revision,
              'recipe':RECIPE, 'recipe_sha256':board._sha256(board._regular_input(root, RECIPE)),
              'abi_definition':CONTRACT,
              'abi_definition_sha256':board._sha256(board._regular_input(root, CONTRACT)),
              'dependencies':{}, 'tools':identities,
              'parameters':{'device':board.TARGET, 'top':'top', 'mode':'test-pattern',
                  'ddr':False, 'format2_package':False, 'gpu_backend':'hip', 'router':'gpu',
                  'gpu_architectures':board.FES_GPU_ARCHITECTURES, 'seed':1,
                  'pixel_clock_hz':74250000, 'reference_clock_hz':50000000}}
    return encode_build_record(functional_record_fields(root, fields,
        source_roots_for_inputs(INPUTS), execution, pinned_inputs=INPUTS))


@guard_functional_source
def build(root=ROOT, *, cache_root=None):
    root = Path(root).resolve()
    repository, revision = board._require_clean_source(root, pinned_inputs=INPUTS)
    authenticated = board._authenticate_tools(root, cache_root=cache_root)
    identities = {name:tool.identity for name,tool in authenticated.items()}
    invocation = FunctionalInvocation(authenticated, 0)
    output = board._prepare_output(root, relative=OUTPUT, build_outputs=OUTPUTS)
    try:
        inputs = record(root, repository, revision, identities, invocation.inputs)
        board._write_atomic(output/'build-inputs.json', inputs)
        commands = build_commands({name:authenticated[name].path for name in ('yosys','nextpnr-mistral')})
        for command,log in zip(commands,('yosys.log','nextpnr.log')):
            board._run_tool(command,root,output/log,output_relative=OUTPUT,
                            env=invocation.env,audit_source_root=root)
        result = validate_build_evidence(output,root)
        invocation.verify()
        final = board._authenticate_tools(root, cache_root=cache_root)
        if {name:tool.identity for name,tool in final.items()} != identities:
            raise board.BuildError('tool identity changed during pattern build')
        if board._require_clean_source(root,pinned_inputs=INPUTS) != (repository,revision):
            raise board.BuildError('source identity changed during pattern build')
        if record(root,repository,revision,identities,invocation.inputs) != inputs:
            raise board.BuildError('pattern functional inputs changed during build')
        result.update({'build_id':build_identity(inputs), 'execution':invocation.inputs,
                       'mode':'test-pattern', 'ddr':False, 'format2_package':False,
                       'source_commit':revision, 'tools':identities,
                       'inputs':{p:board._sha256(root/p) for p in INPUTS}})
        board._write_atomic(output/'build-summary.json',(json.dumps(result,indent=2)+'\n').encode())
        return output/'core.rbf'
    except Exception:
        board._invalidate_failed_artifact(output)
        raise
    finally:
        invocation.close()


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,default=ROOT)
    parser.add_argument('--cache-root',type=Path)
    args=parser.parse_args()
    try:print(build(args.root,cache_root=args.cache_root))
    except (board.BuildError,ValueError) as exc:
        print(f'FES menu pattern: {exc}',file=sys.stderr);return 1
    return 0
if __name__=='__main__':raise SystemExit(main())
