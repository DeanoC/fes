"""Reviewed reset synchronizers of the HPS window's clock-crossing bridge.

A machine with a window in HPS DDR (fes.fabric.de25-hps-window) crosses its
master into the 100 MHz F2SDRAM side through fes_win_cdc, Platform Designer's
mm_ccb. Its two DC FIFOs each put one reset synchronizer on each clock
(altera_avalon_dc_fifo.v, lines 238-255 of the Quartus Pro 26.3 IP source;
lines 247-266 of the compiled copy pinned below). They sit outside the
SYNC_RESET generate, so no bridge parameter removes them, and each clock
domain then has three reset chains: the board's and the two FIFOs'
(RDC-50003). Deano chose (21 November 2026, option a) to admit them by this
named basis, keeping mm_ccb.

The basis names each synchronizer instance and its source reset, pins the
compiled FIFO source that instantiates them, requires the RDC-50003 rows to
be exactly these two bridge rows beside the reviewed HDMI output-reset row,
and requires recovery and removal to pass, at every corner, on every load of
every one of the four chains. No constraint is added and no rule changes.
"""
import json
from pathlib import Path
import subprocess

from boards.de25nano.scanout import video_outputs

HERE = Path(__file__).resolve().parent
POLICY = 'de25-reviewed-window-bridge-fifo-reset-v1'
REPORTS = {'chains.tsv', 'loads.tsv', 'paths.tsv'}
BRIDGE = 'soc_inst|subsys_periph|fes_win_cdc|fes_win_cdc|'
MACHINE = 'fes_machine_pll|machine_pll_outclk0'
PERIPHERAL = 'pll_inst|iopll_0_outclk0'
# Each FIFO's write side runs on its in_clk and its read side on its out_clk:
# the command FIFO from the machine clock to the peripheral clock, the response
# FIFO back.
INSTANCES = {BRIDGE + f'{fifo}_fifo|my_altera_avalon_dc_fifo_{fifo}_fifo|{side}_reset_sync': clock
             for fifo, side, clock in (('cmd', 'write', MACHINE), ('cmd', 'read', PERIPHERAL),
                                       ('rsp', 'write', PERIPHERAL), ('rsp', 'read', MACHINE))}
STAGES = [f'altera_reset_synchronizer_int_chain[{i}]' for i in range(4)] + ['altera_reset_synchronizer_int_chain_out']
# Every FIFO's reset_merged is (~in_reset_n | ~out_reset_n); both resets come
# from these two reset controllers.
SOURCE_RESETS = {'soc_inst|rst_controller_002|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain_out',
                 'soc_inst|subsys_periph|rst_controller_001|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain_out'}
# The compiled FIFO module both wrappers instantiate: altera_avalon_dc_fifo
# renamed by generation, its synchronizers at lines 247-266.
COMPILED = ('peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_win_cdc/st_dc_fifo_1953/synth/'
            'peripheral_subsys_fes_win_cdc_st_dc_fifo_1953_phmrs5y.v')
COMPILED_SHA256 = 'd705d581f70598b64d10168c48aedadb0db6556d5d1c83b2142f79c49f1edaaa'
WRAPPERS = 'peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_win_cdc/mm_ccb_1941/synth'
# The two RDC-50003 rows this basis admits, exactly (the third is the HDMI
# output-reset row, admitted by output_reset.py).
ROWS = (
    (PERIPHERAL, '3',
     {'fes_top_reset_inst|sync_regs[0]',
      BRIDGE + 'cmd_fifo|my_altera_avalon_dc_fifo_cmd_fifo|read_reset_sync|altera_reset_synchronizer_int_chain[3]',
      BRIDGE + 'rsp_fifo|my_altera_avalon_dc_fifo_rsp_fifo|write_reset_sync|altera_reset_synchronizer_int_chain[3]'},
     {'pll_inst|iopll_0|tennm_ph2_iopll~pll_ctrl_reg',
      'auto_fab_0|alt_sld_fab_0|alt_sld_fab_0|agilexconfigreset|user_reset|sdm_gpo_out_user_reset~internal_ctrl_clock.reg',
      *SOURCE_RESETS}),
    (MACHINE, '3',
     {'soc_inst|subsys_periph|rst_controller_001|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[1]',
      BRIDGE + 'cmd_fifo|my_altera_avalon_dc_fifo_cmd_fifo|write_reset_sync|altera_reset_synchronizer_int_chain[3]',
      BRIDGE + 'rsp_fifo|my_altera_avalon_dc_fifo_rsp_fifo|read_reset_sync|altera_reset_synchronizer_int_chain[3]'},
     set(SOURCE_RESETS)),
)


def require(condition, message):
    from boards.de25nano.scanout import async_inputs
    async_inputs.require(condition, 'window bridge reset: ' + message)


def selected(directory):
    fields = json.loads((Path(directory).parent / 'build-inputs.json').read_text())
    policy = fields['parameters'].get('window_bridge_reset_review_policy')
    require(policy in (None, POLICY), 'unknown policy')
    require(policy is None or fields['parameters'].get('guest_memory') == 'hps-window',
            'selected for a build without the window')
    return policy == POLICY


def matches(row, expected):
    clock, count, heads, sources = expected
    return (row[0] == clock and row[1] == count and set(row[2].split()) == heads and
            len(row[2].split()) == len(heads) and set(row[3].split()) == sources and
            len(row[3].split()) == len(sources) and not row[4])


def bridge_rows(rows):
    """The RDC-50003 rows this basis admits; each expected row must be there exactly once."""
    found = [[r for r in rows if matches(r, expected)] for expected in ROWS]
    require(all(len(f) == 1 for f in found), 'bridge RDC rows changed')
    return [f[0] for f in found]


def compiled_source(directory):
    project = Path(directory) / 'combined/hps/project'
    from boards.de25nano.scanout import async_inputs as a
    module = project / COMPILED
    require(module.is_file() and not module.is_symlink() and a.sha(module) == COMPILED_SHA256,
            'compiled DC FIFO source differs from the reviewed one')
    name = module.stem
    wrappers = sorted((project / WRAPPERS).glob('*st_dc_fifo*.v'))
    require(len(wrappers) == 2 and all(w.read_text().count(name) == 1 for w in wrappers),
            'the bridge FIFOs no longer instantiate the reviewed DC FIFO')
    return {str(module.relative_to(project)): COMPILED_SHA256}


def review_reports(directory):
    from boards.de25nano.scanout import async_inputs as a
    directory = Path(directory)
    require({p.name for p in directory.iterdir()} == REPORTS and
            all(p.is_file() and not p.is_symlink() for p in directory.iterdir()), 'unexpected/linked reports')
    def table(name, width):
        rows = [line.split('\t') for line in (directory / name).read_text().splitlines()]
        require(all(len(r) == width for r in rows), 'invalid row in ' + name)
        return rows
    chains = {}
    for inst, register, source in table('chains.tsv', 3):
        require(inst in INSTANCES and register.startswith(inst + '|'), 'unexpected synchronizer register')
        chains.setdefault(inst, {}).setdefault(register[len(inst) + 1:], set()).add(source)
    require(set(chains) == set(INSTANCES) and
            all(set(regs) == set(STAGES) and all(s == SOURCE_RESETS for s in regs.values())
                for regs in chains.values()),
            'synchronizer stages or their source resets changed')
    loads = {}
    for inst, load in table('loads.tsv', 2):
        fifo = inst.rsplit('|', 1)[0] + '|'
        require(inst in INSTANCES and load.startswith(fifo), 'a synchronizer resets outside its own FIFO')
        loads.setdefault(inst, set()).add(load)
    require(set(loads) == set(INSTANCES), 'a synchronizer has no loads')
    coverage, slack = set(), {}
    for model, kind, inst, target, value, launch, latch in table('paths.tsv', 7):
        key = model, kind, inst, target
        require(model in video_outputs.MODELS and kind in ('recovery', 'removal') and inst in INSTANCES and
                target in loads[inst] and launch == latch == INSTANCES[inst] and key not in coverage,
                'changed/duplicate reset timing path')
        number = a.number(value, 'bridge reset ' + kind)
        coverage.add(key)
        slack[kind] = min(slack.get(kind, number), number)
    require(coverage == {(m, k, inst, load) for m in video_outputs.MODELS for k in ('recovery', 'removal')
                         for inst, targets in loads.items() for load in targets},
            'a load lacks recovery or removal timing at some corner')
    return {'policy': POLICY, 'instances': {inst: {'clock': INSTANCES[inst], 'loads': len(loads[inst])}
                                            for inst in sorted(INSTANCES)},
            'source_resets': sorted(SOURCE_RESETS), 'path_count': len(coverage),
            'worst_slack_ns': {k: str(v) for k, v in sorted(slack.items())}}


def collect(directory, copied_project, output, quartus_sta):
    from boards.de25nano.scanout import async_inputs as a
    output = Path(output); output.mkdir(exist_ok=False)
    reports = output / 'reports'; reports.mkdir()
    command = [str(Path(quartus_sta).resolve()), '-t', str(HERE / 'bridge_reset.tcl'),
               str(Path(copied_project).resolve()), str(reports.resolve())]
    before = a.bindings(directory)
    source = compiled_source(directory)
    tools = {str(p.resolve()): a.sha(p) for p in (Path(command[0]), HERE / 'bridge_reset.py', HERE / 'bridge_reset.tcl')}
    with (output / 'quartus-sta.log').open('wb') as stream:
        subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=240)
    result = review_reports(reports)
    require(a.bindings(directory) == before and all(a.sha(p) == d for p, d in tools.items()),
            'fit/tool/source changed during query')
    receipt = {'bindings': before, 'compiled_source': source, 'tools_sha256': tools, 'command': command,
               'reports_sha256': {p.name: a.sha(p) for p in sorted(reports.iterdir())}, 'result': result}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')


def verify(directory, output, output_reset_finding):
    """The combined RDC-50003 entry: the HDMI output-reset row and the two bridge rows."""
    from boards.de25nano.scanout import async_inputs as a, machine_findings
    output = Path(output); receipt = json.loads((output / 'receipt.json').read_text())
    require(receipt['bindings'] == a.bindings(directory) and receipt['compiled_source'] == compiled_source(directory),
            'fitted binding changed')
    command = receipt['command']
    require(command == [command[0], '-t', str(HERE / 'bridge_reset.tcl'),
                        str(output.parent / 'project-inputs/combined/hps/project'), str(output / 'reports')]
            and set(receipt['tools_sha256']) == {command[0], str(HERE / 'bridge_reset.py'), str(HERE / 'bridge_reset.tcl')}
            and all(a.sha(p) == d for p, d in receipt['tools_sha256'].items()), 'query/source binding changed')
    reports = output / 'reports'
    require(receipt['reports_sha256'] == {p.name: a.sha(p) for p in sorted(reports.iterdir())}, 'report binding changed')
    result = review_reports(reports)
    require(result == receipt['result'], 'result changed')
    drc = (Path(directory) / 'combined/hps/project/output_files/golden_top.tq.drc.signoff.rpt').read_text()
    _, rows = machine_findings.table(drc, 'RDC-50003')
    admitted = bridge_rows(rows)
    finding = {'id': 'RDC-50003', 'violations': 3, 'basis': [output_reset_finding['basis'], POLICY],
               'output_reset': output_reset_finding,
               'window_bridge': [{'clock': r[0], 'heads': sorted(r[2].split()), 'sources': sorted(r[3].split())}
                                 for r in admitted]}
    return dict(result, finding=finding)
