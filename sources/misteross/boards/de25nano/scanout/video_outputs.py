"""Numerical admission of every fitted HDMI video output in all four models.

This removes no constraints and grants no new exception. Board flight time is
still the explicit zero-flight assumption in clocks.sdc, not PCB qualification.
"""
import json
from pathlib import Path
import re
import subprocess

from scripts.fes_build_common import BuildError

HERE = Path(__file__).resolve().parent
POLICY = 'de25-hdmi-video-output-timing-v1'
PORTS = {f'HDMI_TX_D[{i}]' for i in range(24)} | {'HDMI_TX_DE', 'HDMI_TX_HS', 'HDMI_TX_VS'}
MODELS = {'Slow fix4 100C Model', 'Slow fix4 0C Model', 'Slow fix4a 0C Model', 'Fast fix4 0C Model'}
CLOCK = 'fes_video_inst|video_clock|iopll_0_outclk0'


def selected(directory):
    record = json.loads((Path(directory).parent / 'build-inputs.json').read_text())
    policy = record['parameters'].get('hdmi_video_timing_policy')
    if policy is not None and policy != POLICY:
        raise BuildError('unknown DE25 video output timing policy')
    return policy == POLICY


def review_reports(directory):
    from boards.de25nano.scanout import async_inputs as a
    directory = Path(directory)
    expected = {(model, port, kind) for model in MODELS for port in PORTS for kind in ('setup', 'hold')}
    seen, files, paths = set(), {'inventory.tsv'}, []
    for line in (directory / 'inventory.tsv').read_text().splitlines():
        fields = line.split('\t')
        a.require(len(fields) == 5, 'invalid video output inventory row')
        name, model, port, kind, slack = fields
        key = model, port, kind
        a.require(key in expected and key not in seen and re.fullmatch(r'path-\d{3}\.rpt', name)
                  and name not in files, 'duplicate/unexpected video output path')
        seen.add(key); files.add(name)
        text = (directory / name).read_text()
        a.require(re.search(r'Snapshot:\s*\n\s*final\b', text) is not None,
                  'video output requires final fitted timing')
        table = [row for row in a.rows(text) if len(row) == 2]
        for field, value in [('From Node', 'fes_video_inst|' + port), ('To Node', port),
                             ('Launch Clock', CLOCK), ('Latch Clock', 'HDMI_TX_CLK'),
                             ('SDC Exception', 'No SDC Exception on Path'),
                             ('Worst-Case Operating Conditions', model), ('Slack', slack)]:
            a.require(a.value(table, field) == value, 'changed video output ' + field)
        a.number(slack, f'{model} {port} {kind}')
        paths.append({'model': model, 'port': port, 'type': kind, 'slack_ns': slack})
    a.require(seen == expected, 'incomplete video output timing coverage')
    a.require({p.name for p in directory.iterdir()} == files and
              all(p.is_file() and not p.is_symlink() for p in directory.iterdir()),
              'unexpected/linked video output reports')
    return {'policy': POLICY, 'models': sorted(MODELS), 'ports': sorted(PORTS),
            'path_count': len(paths), 'paths': paths,
            'external_board_flight_time_ns': 0, 'independent_pcb_timing_qualified': False}


def collect(directory, copied_project, output, quartus_sta):
    from boards.de25nano.scanout import async_inputs as a
    output = Path(output); output.mkdir(exist_ok=False)
    reports = output / 'reports'; reports.mkdir()
    command = [str(Path(quartus_sta).resolve()), '-t', str(HERE / 'video_outputs.tcl'),
               str(Path(copied_project).resolve()), str(reports.resolve())]
    before = a.bindings(directory)
    sources = {str(p.resolve()): a.sha(p) for p in (Path(command[0]), HERE / 'video_outputs.py', HERE / 'video_outputs.tcl')}
    with (output / 'quartus-sta.log').open('wb') as stream:
        subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=240)
    result = review_reports(reports)
    a.require(a.bindings(directory) == before and all(a.sha(p) == digest for p, digest in sources.items()),
              'video output fit/tool/source changed during query')
    receipt = {'bindings': before, 'sources_sha256': sources, 'command': command,
               'reports_sha256': {p.name: a.sha(p) for p in sorted(reports.iterdir())}, 'result': result}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')


def verify(directory, output):
    from boards.de25nano.scanout import async_inputs as a
    a.require(selected(directory), 'video output timing policy was not selected')
    output = Path(output); receipt = json.loads((output / 'receipt.json').read_text())
    a.require(receipt['bindings'] == a.bindings(directory), 'video output fitted binding changed')
    command = receipt['command']
    a.require(command == [command[0], '-t', str(HERE / 'video_outputs.tcl'),
                         str(output.parent / 'project-inputs/combined/hps/project'), str(output / 'reports')]
              and set(receipt['sources_sha256']) == {command[0], str(HERE / 'video_outputs.py'), str(HERE / 'video_outputs.tcl')}
              and all(a.sha(p) == digest for p, digest in receipt['sources_sha256'].items()),
              'video output query/source binding changed')
    reports = output / 'reports'
    a.require(receipt['reports_sha256'] == {p.name: a.sha(p) for p in sorted(reports.iterdir())},
              'video output report binding changed')
    result = review_reports(reports)
    a.require(result == receipt['result'], 'video output result changed')
    return result
