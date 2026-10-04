#!/usr/bin/env python3
"""Build the menu DDR or DDR-free test-pattern diagnostic with authenticated OSS tools."""
from __future__ import annotations
import argparse
import json
from pathlib import Path
import sys
if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import fes_build_common as board
from scripts import fes_de10nano_evidence as evidence
from scripts.compiler_read_audit import python_source_guard
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
DDR_SOURCES = ('cores/fes-common/rtl/fes_hps_ddr.v',
               'cores/fes-common/rtl/fes_hps_ddr_guard.v',
               'cores/fes-menu/rtl/fes_menu_ddr.v')
DDR_GENERATED = 'cores/fes-common/generated/fes_application.vh'
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


def output_for(mode):
    if mode not in ('test-pattern', 'ddr'):
        raise ValueError('unknown menu diagnostic mode')
    return OUTPUT if mode == 'test-pattern' else Path('build/oss/fes-menu')


def inputs_for(mode):
    output_for(mode)
    if mode == 'test-pattern':
        return INPUTS
    return tuple(p for p in INPUTS if p not in (CONTRACT, 'toolchain.lock')) + (
        'cores/fes-menu/ddr-contract.toml', 'toolchain.lock', DDR_GENERATED, *DDR_SOURCES)


def authenticate(root, cache_root, mode):
    if mode == 'test-pattern':
        return board._authenticate_tools(root, cache_root=cache_root)
    return board._authenticate_tools(root, cache_root=cache_root)


def seed_for(mode, seed):
    output_for(mode)
    if seed is None:
        return 4 if mode == 'ddr' else 1
    if type(seed) is not int or not 1 <= seed <= 8:
        raise ValueError('menu diagnostic seed must be an integer from 1 to 8')
    return seed


def build_commands(tools, *, mode='test-pattern', seed=None):
    selected_seed = seed_for(mode, seed)
    if set(tools) != {'yosys','nextpnr-mistral'}:
        raise board.BuildError('menu diagnostic requires authenticated Yosys and nextpnr')
    output = output_for(mode)
    sources = SOURCES + (DDR_SOURCES if mode == 'ddr' else ())
    select = 'chparam -set TEST_PATTERN 0 top; ' if mode == 'ddr' else ''
    program = (f"read_verilog -sv -I cores/fes-common/generated {' '.join(sources)}; {select}"
               f"synth_intel_alm -nolutram -nodsp -top top; stat; write_json {output}/synth.json")
    return ((str(tools['yosys']), '-p', program),
            (str(tools['nextpnr-mistral']), '--json', f'{output}/synth.json',
             '--device', board.TARGET, '--qsf', QSF, '--sdc', evidence.SDC,
             '--freq', '74.25', '--seed', str(selected_seed), '--router', 'gpu', '--gpu-device', '0',
             '--rbf', f'{output}/core.rbf', '--compress-rbf',
             '--write', f'{output}/routed.json', '--report', f'{output}/timing.json',
             '--detailed-timing-report'))


def layout_graph_with_constants(graph):
    # nextpnr packs static cfg bits onto MISTRAL_CONST nets. Resolve only its
    # explicit zero/one cells; arbitrary signals remain nonconstant and fail.
    cells = graph['modules']['top']['cells']
    constants = {}
    for cell in cells.values():
        lut = cell.get('parameters', {}).get('LUT')
        q = cell.get('connections', {}).get('Q')
        if (cell.get('type') == 'MISTRAL_CONST' and type(lut) is str
                and len(lut) == 32 and set(lut) <= {'0','1'}
                and int(lut,2) in (0,1) and type(q) is list and len(q) == 1
                and type(q[0]) is int):
            if q[0] in constants:
                raise board.BuildError('multiple constant drivers on DDR configuration net')
            constants[q[0]] = str(int(lut,2))
    normalized = dict(cells)
    for name, cell in cells.items():
        if cell.get('type') != 'cyclonev_hps_interface_fpga2sdram':
            continue
        connections = dict(cell.get('connections', {}))
        for port in evidence.HPS_DDR_CFG_PORTS:
            bits = connections.get(port)
            if isinstance(bits,list):
                connections[port] = [constants.get(bit,bit) if type(bit) is int else bit for bit in bits]
        normalized[name] = {**cell, 'connections':connections}
    return {'modules':{'top':{**graph['modules']['top'], 'cells':normalized}}}


def validate_build_evidence(output, root, *, mode='test-pattern', menu_gp=False):
    output_for(mode)
    if menu_gp and mode != 'ddr':
        raise board.BuildError('menu GP requires DDR package evidence')
    # A test-pattern artifact must not acquire a DDR or GP block in either graph.
    for filename in ('synth.json', 'routed.json'):
        graph = board._read_json(Path(output)/filename, filename)
        counts = board._cell_counts(graph)
        for module in graph.get('modules', {}).values():
            for cell in module.get('cells', {}).values():
                if cell.get('type') == 'MISTRAL_M10K' and int(
                        str(cell.get('parameters', {}).get('CFG_ASYNC_READ', '0')), 2):
                    raise board.BuildError('menu FIFO requires synchronous M10K reads')
        for name in (('cyclonev_hps_interface_fpga2sdram',) if mode == 'test-pattern' else ()) + (
                     ('cyclonev_hps_interface_mpu_general_purpose',) if not menu_gp else ()):
            if counts.get(name, 0):
                raise board.BuildError(f'pattern diagnostic uses forbidden {name}')
    required = {'cyclonev_hps_interface_peripheral_i2c':1}
    forbidden = FORBIDDEN
    if mode == 'ddr':
        required['cyclonev_hps_interface_fpga2sdram'] = 1
        forbidden = forbidden - {'cyclonev_hps_interface_fpga2sdram'}
    if menu_gp:
        required['cyclonev_hps_interface_mpu_general_purpose'] = 1
        forbidden = forbidden - {'cyclonev_hps_interface_mpu_general_purpose'}
    result = evidence.validate_build_evidence(output, root, ordinary_resources=ORDINARY,
        required_resources=required, forbidden_resources=forbidden,
        required_zero_resources=frozenset({'cyclonev_oscillator'}))
    if mode == 'ddr':
        for filename in ('synth.json','routed.json'):
            graph = board._read_json(Path(output)/filename,filename)
            result['hps_ddr'] = evidence.hps_ddr_layout_evidence(
                layout_graph_with_constants(graph),filename,Path(root),idle=False)
            cells = list(graph['modules']['top']['cells'].values())
            grounds = [['0']]
            if filename == 'routed.json':
                grounds += [c.get('connections',{}).get('Q') for c in cells
                    if c.get('type') == 'MISTRAL_CONST'
                    and set(str(c.get('parameters',{}).get('LUT',''))) == {'0'}
                    and isinstance(c.get('connections',{}).get('Q'),list)
                    and len(c['connections']['Q']) == 1]
            connections = next(c['connections'] for c in cells
                if c.get('type') == 'cyclonev_hps_interface_fpga2sdram')
            inactive = tuple(f'cmd_valid_{port}' for port in range(1,6)) + tuple(
                f'wr_valid_{port}' for port in range(4))
            if any(connections.get(port) not in grounds for port in inactive):
                raise board.BuildError('menu DDR writes and unused ports must be tied low')
            if connections.get('cmd_data_0', [])[1:2] not in grounds:
                raise board.BuildError('menu DDR command write bit must be tied low')
    return result


def record(root, repository, revision, identities, execution, *, mode='test-pattern', seed=None):
    with python_source_guard(root, source_roots_for_inputs(inputs_for(mode))):
        return _record(root, repository, revision, identities, execution, mode=mode, seed=seed)


def _record(root, repository, revision, identities, execution, *, mode='test-pattern', seed=None):
    contract = CONTRACT if mode == 'test-pattern' else 'cores/fes-menu/ddr-contract.toml'
    fields = {'format':1, 'repository':repository, 'revision':revision,
              'recipe':RECIPE, 'recipe_sha256':board._sha256(board._regular_input(root, RECIPE)),
              'abi_definition':contract,
              'abi_definition_sha256':board._sha256(board._regular_input(root, contract)),
              'dependencies':{}, 'tools':identities,
              'parameters':{'device':board.TARGET, 'top':'top', 'mode':mode,
                  'ddr':mode == 'ddr', 'format2_package':False, 'gpu_backend':'hip', 'router':'gpu',
                  'gpu_architectures':board.FES_GPU_ARCHITECTURES, 'seed':seed_for(mode, seed),
                  'pixel_clock_hz':74250000, 'reference_clock_hz':50000000,
                  'diagnostic_enable':True, 'ddr_slot':0}}
    return encode_build_record(functional_record_fields(root, fields,
        source_roots_for_inputs(inputs_for(mode)), execution, pinned_inputs=inputs_for(mode)))


def build(root=ROOT, *, cache_root=None, mode='test-pattern', seed=None):
    with python_source_guard(root, source_roots_for_inputs(inputs_for(mode))):
        return _build(root, cache_root=cache_root, mode=mode, seed=seed)


def _build(root=ROOT, *, cache_root=None, mode='test-pattern', seed=None):
    seed = seed_for(mode, seed)
    selected_inputs = inputs_for(mode)
    output_relative = output_for(mode)
    root = Path(root).resolve()
    repository, revision = board._require_clean_source(root, pinned_inputs=selected_inputs)
    authenticated = authenticate(root, cache_root, mode)
    identities = {name:tool.identity for name,tool in authenticated.items()}
    invocation = FunctionalInvocation(authenticated, 0)
    output = board._prepare_output(root, relative=output_relative, build_outputs=OUTPUTS)
    try:
        inputs = record(root, repository, revision, identities, invocation.inputs, mode=mode, seed=seed)
        board._write_atomic(output/'build-inputs.json', inputs)
        commands = build_commands({name:authenticated[name].path for name in ('yosys','nextpnr-mistral')}, mode=mode, seed=seed)
        for command,log in zip(commands,('yosys.log','nextpnr.log')):
            board._run_tool(command,root,output/log,output_relative=output_relative,
                            env=invocation.env,audit_source_root=root)
        result = validate_build_evidence(output,root,mode=mode)
        invocation.verify()
        final = authenticate(root, cache_root, mode)
        if {name:tool.identity for name,tool in final.items()} != identities:
            raise board.BuildError('tool identity changed during menu diagnostic build')
        if board._require_clean_source(root,pinned_inputs=selected_inputs) != (repository,revision):
            raise board.BuildError('source identity changed during menu diagnostic build')
        if record(root,repository,revision,identities,invocation.inputs,mode=mode,seed=seed) != inputs:
            raise board.BuildError('menu diagnostic functional inputs changed during build')
        result.update({'build_id':build_identity(inputs), 'execution':invocation.inputs,
                       'mode':mode, 'seed':seed_for(mode,seed), 'ddr':mode == 'ddr', 'format2_package':False,
                       'source_commit':revision, 'tools':identities,
                       'inputs':{p:board._sha256(root/p) for p in selected_inputs}})
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
    parser.add_argument('--seed',type=int,choices=range(1,9))
    parser.add_argument('--mode',choices=('test-pattern','ddr'),default='test-pattern')
    args=parser.parse_args()
    try:print(build(args.root,cache_root=args.cache_root,mode=args.mode,seed=args.seed))
    except (board.BuildError,ValueError) as exc:
        print(f'FES menu diagnostic: {exc}',file=sys.stderr);return 1
    return 0
if __name__=='__main__':raise SystemExit(main())
