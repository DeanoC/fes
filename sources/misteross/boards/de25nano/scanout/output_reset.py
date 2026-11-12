"""User-approved review of the independent terminal HDMI output-reset chain.

Only the one source-bound RDC-50003 finding is reviewed. Every local reset
load must still pass recovery/removal in every model; no constraint is added.
"""
import json
from pathlib import Path
import subprocess

from scripts.fes_build_common import BuildError
from boards.de25nano.scanout import video_outputs

HERE = Path(__file__).resolve().parent
POLICY = 'de25-reviewed-terminal-hdmi-output-reset-v1'
CLOCK = video_outputs.CLOCK
STAGES = [f'fes_video_inst|output_release[{i}]' for i in range(2)]
LOADS = {f'fes_video_inst|HDMI_TX_D[{i}]' for i in range(24)} | {
    'fes_video_inst|HDMI_TX_DE', 'fes_video_inst|hdmi_active'}
HEADS = {STAGES[0], 'soc_inst|rst_controller_001|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain[1]'}
SOURCES = {'fes_video_inst|first_lock_seen', 'fes_video_inst|config_reset|sync_regs[1]',
           'fes_video_inst|visible_allow'}
REPORTS = {'reset-inventory.tsv', 'reset-paths.tsv'}


def selected(directory):
    fields = json.loads((Path(directory).parent / 'build-inputs.json').read_text())
    policy = fields['parameters'].get('hdmi_output_reset_review_policy')
    if policy is not None and policy != POLICY:
        raise BuildError('unknown DE25 output-reset review policy')
    return policy == POLICY


def findings(drc):
    from boards.de25nano.scanout import async_inputs as a
    sections = [s for s in drc.split('Status:')[1:] if '; RDC-50003 -' in s]
    a.require(len(sections) == 1, 'missing/ambiguous output-reset RDC detail')
    table = a.rows(sections[0])
    a.require(['Reset Clock Domain', 'Number of Reset Chains', 'Reset Chain Register Heads',
               'Reset Sources', 'Waived'] in table, 'changed output-reset RDC columns')
    rows = [r for r in table if len(r) == 5 and r[0] != 'Reset Clock Domain']
    a.require(len(rows) == 1, 'only one output-reset RDC domain is reviewed')
    clock, count, heads, sources, waived = rows[0]
    a.require(clock == CLOCK and count == '2' and not waived and
              set(heads.split()) == HEADS and len(heads.split()) == len(HEADS) and
              set(sources.split()) == SOURCES and len(sources.split()) == len(SOURCES),
              'output-reset RDC topology/sources changed')
    return {'id': 'RDC-50003', 'violations': 1, 'clock': CLOCK,
            'heads': sorted(HEADS), 'sources': sorted(SOURCES), 'basis': POLICY}


def review_reports(directory):
    from boards.de25nano.scanout import async_inputs as a
    directory = Path(directory)
    a.require({p.name for p in directory.iterdir()} == REPORTS and
              all(p.is_file() and not p.is_symlink() for p in directory.iterdir()),
              'unexpected/linked output-reset reports')
    inventory = [line.split('\t') for line in (directory / 'reset-inventory.tsv').read_text().splitlines()]
    expected = {('0', STAGES[1])} | {('1', load) for load in LOADS}
    a.require(all(len(row) == 2 for row in inventory) and len(inventory) == len(expected) and
              {tuple(row) for row in inventory} == expected, 'unreviewed output-reset fanout')
    coverage, paths = set(), []
    for line in (directory / 'reset-paths.tsv').read_text().splitlines():
        fields = line.split('\t')
        a.require(len(fields) == 7, 'invalid output-reset timing row')
        model, kind, source, target, slack, launch, latch = fields
        key = model, kind, target
        a.require(model in video_outputs.MODELS and kind in ('recovery', 'removal') and
                  source == STAGES[1] and target in LOADS and launch == latch == CLOCK and
                  key not in coverage, 'changed/duplicate output-reset timing path')
        a.number(slack, 'output-reset ' + kind)
        coverage.add(key); paths.append({'model': model, 'type': kind, 'target': target, 'slack_ns': slack})
    a.require(coverage == {(m, k, n) for m in video_outputs.MODELS
                          for k in ('recovery', 'removal') for n in LOADS},
              'incomplete output-reset timing coverage')
    return {'policy': POLICY, 'loads': sorted(LOADS), 'path_count': len(paths), 'paths': paths}


def bindings(directory):
    from boards.de25nano.scanout import async_inputs as a
    directory = Path(directory)
    a.require(selected(directory), 'output-reset policy not selected')
    board = directory / 'combined/hps/project/fes_mailbox/fes_de25_scanout_board.v'
    a.require(not board.is_symlink() and a.sha(board) == a.sha(HERE / board.name),
              'compiled output-reset board differs from reviewed source')
    return {'fit': a.bindings(directory), 'compiled_board_sha256': a.sha(board)}


def collect(directory, copied_project, output, quartus_sta):
    from boards.de25nano.scanout import async_inputs as a
    output = Path(output); output.mkdir(exist_ok=False)
    reports = output / 'reports'; reports.mkdir()
    command = [str(Path(quartus_sta).resolve()), '-t', str(HERE / 'output_reset.tcl'),
               str(Path(copied_project).resolve()), str(reports.resolve())]
    before = bindings(directory)
    tools = {str(p.resolve()): a.sha(p) for p in (Path(command[0]), HERE / 'output_reset.py', HERE / 'output_reset.tcl')}
    with (output / 'quartus-sta.log').open('wb') as stream:
        subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=240)
    result = review_reports(reports)
    a.require(bindings(directory) == before and all(a.sha(p) == digest for p, digest in tools.items()),
              'output-reset fit/tool/source changed during query')
    receipt = {'bindings': before, 'tools_sha256': tools, 'command': command,
               'reports_sha256': {p.name: a.sha(p) for p in sorted(reports.iterdir())}, 'result': result}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')


def verify(directory, output):
    from boards.de25nano.scanout import async_inputs as a
    output = Path(output); receipt = json.loads((output / 'receipt.json').read_text())
    a.require(receipt['bindings'] == bindings(directory), 'output-reset fitted binding changed')
    command = receipt['command']
    a.require(command == [command[0], '-t', str(HERE / 'output_reset.tcl'),
                         str(output.parent / 'project-inputs/combined/hps/project'), str(output / 'reports')]
              and set(receipt['tools_sha256']) == {command[0], str(HERE / 'output_reset.py'), str(HERE / 'output_reset.tcl')}
              and all(a.sha(p) == digest for p, digest in receipt['tools_sha256'].items()),
              'output-reset query/source binding changed')
    reports = output / 'reports'
    a.require(receipt['reports_sha256'] == {p.name: a.sha(p) for p in sorted(reports.iterdir())},
              'output-reset report binding changed')
    result = review_reports(reports)
    a.require(result == receipt['result'], 'output-reset result changed')
    drc = (Path(directory) / 'combined/hps/project/output_files/golden_top.tq.drc.signoff.rpt').read_text()
    return dict(result, finding=findings(drc))
