"""Fitted evidence for the two explicitly reviewed asynchronous HDMI inputs.

This policy accepts only their missing-input-delay findings. It does not change
constraints, waive Design Assistant rules or qualify external/reset timing.
"""
from decimal import Decimal, InvalidOperation
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess

from scripts.export_core_package import build_identity
from scripts.fes_build_common import BuildError

POLICY = 'de25-menu-reviewed-async-inputs-v1'
PORTS = {'HDMI_I2C_SDA': 'hdmi_sda_sync', 'HDMI_TX_INT': 'hdmi_int_sync'}
CLOCK = 'fes_video_inst|video_clock|iopll_0_refclk'
HERE = Path(__file__).resolve().parent
REPORTS = ('inventory.tsv', 'metastability.rpt', *(
    f'{head}-{kind}.rpt' for head in PORTS.values() for kind in ('setup', 'hold')))


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def require(condition, message):
    if not condition:
        raise BuildError('DE25 asynchronous-input review: ' + message)


def rows(text):
    return [[c.strip() for c in line.split(';')[1:-1]]
            for line in text.splitlines() if line.startswith(';')]


def value(table, name, *, count=1):
    found = [r[1] for r in table if len(r) >= 2 and r[0] == name]
    require(len(found) == count and len(set(found)) == 1, 'missing/ambiguous property ' + name)
    return found[0]


def number(text, name):
    try:
        result = Decimal(text)
        require(result.is_finite() and result >= 0, 'nonpassing ' + name)
        return result
    except InvalidOperation as error:
        raise BuildError('DE25 asynchronous-input review: invalid ' + name) from error


def review_reports(directory):
    directory = Path(directory)
    topology = {}
    for line in (directory / 'inventory.tsv').read_text().splitlines():
        fields = line.split('\t')
        require(len(fields) == 3 and (fields[0], fields[1]) not in topology, 'invalid topology row')
        topology[fields[0], fields[1]] = fields[2]
    fields = {'input-fanout', 'stage0-fanout', 'stage1-fanin-synch',
              'stage1-fanout', 'stage0-location', 'stage1-location'}
    require(set(topology) == {(p, f) for p in PORTS for f in fields}, 'unexpected topology inventory')
    mtbf = (directory / 'metastability.rpt').read_text()
    chains = [rows(s) for s in re.split(r'(?m)^Synchronizer Chain #\d+:', mtbf)[1:]]
    result = {}
    for port, head in PORTS.items():
        zero, one = (f'fes_video_inst|{head}[{i}]' for i in range(2))
        for field, expected in [('input-fanout', zero), ('stage0-fanout', one), ('stage1-fanin-synch', zero)]:
            require(topology[port, field] == expected, port + ' unreviewed ' + field)
        consumers = topology[port, 'stage1-fanout'].split(',')
        require(len(set(consumers)) == len(consumers), port + ' duplicate consumer')
        if port == 'HDMI_I2C_SDA':
            require(set(consumers) == {f'fes_video_inst|hdmi_config|u0|ACK{i}' for i in (1, 2, 3)},
                    port + ' unreviewed controller consumer')
        else:
            matched = [re.fullmatch(r'fes_video_inst\|hdmi_config\|LUT_INDEX\[([0-5])\](?:~DUPLICATE)?', c)
                       for c in consumers]
            require(all(matched) and {m[1] for m in matched} == set('012345'),
                    port + ' unreviewed controller consumer')
        locations = [re.fullmatch(r'(FF_X\d+_Y\d+)_N\d+', topology[port, f'stage{i}-location']) for i in range(2)]
        require(all(locations) and locations[0][1] == locations[1][1] and
                topology[port, 'stage0-location'] != topology[port, 'stage1-location'],
                port + ' synchronizer registers must be distinct in one tile')
        timing = {}
        for kind in ('setup', 'hold'):
            text = (directory / f'{head}-{kind}.rpt').read_text()
            table = [r for r in rows(text) if len(r) == 2]
            require(re.search(r'Snapshot:\s*\n\s*final\b', text) is not None,
                    port + ' requires final fitted timing')
            for name, expected in [('From Node', zero), ('To Node', one), ('Launch Clock', CLOCK),
                                   ('Latch Clock', CLOCK), ('SDC Exception', 'No SDC Exception on Path')]:
                require(value(table, name) == expected, port + ' changed timed path ' + name)
            slack = value(table, 'Slack')
            number(slack, port + ' ' + kind + ' slack')
            corner = value(table, 'Worst-Case Operating Conditions')
            require(corner.endswith(' Model'), port + ' missing worst-case model')
            timing[kind] = {'slack_ns': slack, 'worst_case_model': corner}
        selected = [c for c in chains if ['Source Node', port] in c]
        require(len(selected) == 1, port + ' missing/duplicate MTBF chain')
        chain = selected[0]
        for name, expected, count in [
                ('Source Node', port, 1), ('Synchronization Node', zero, 1),
                ('Included in Design MTBF', 'Yes', 1),
                ('Method of Synchronizer Identification', 'Forced If Asynchronous', 1),
                ('Number of Synchronization Registers in Chain', '2', 2),
                ('Worst-Case MTBF (years)', 'Greater than 1 Billion', 2)]:
            require(value(chain, name, count=count) == expected, port + ' changed MTBF ' + name)
        for name, expected in [('Clock Period (ns)', '20'), ('Active Edge Rate (MHz)', '50'),
                ('Fastest Synchronization Clock Frequency - Fc (MHz)', '50'),
                ('Data Toggle Rate Used in MTBF Calculation - Fd (millions of transitions / sec)', '6.25')]:
            require(number(value(chain, name), name) == Decimal(expected), port + ' changed ' + name)
        note = 'The MTBF calculation used a default data toggle rate which is 12.5% of the fastest source clock frequency.'
        require([note, '', ''] in chain and [CLOCK, '', ''] in chain and
                [zero, '', ''] in chain and [one, '', ''] in chain, port + ' changed clock/register/default-toggle assumptions')
        for node in (zero, one):
            number(value(chain, f'Output Slack - Tmet_{node} (ns)'), port + ' MTBF settling slack')
        number(value(chain, 'Available Settling Time - Total Tmet (ns)'), port + ' MTBF total settling time')
        result[port] = {'registers': [zero, one], 'consumers': consumers,
            'locations': [topology[port, f'stage{i}-location'] for i in range(2)],
            'timing': timing, 'clock_mhz': 50, 'default_toggle_million_per_second': '6.25',
            'modeled_worst_case_mtbf_years': 'Greater than 1 Billion', 'included_in_design_mtbf': True}
    return result


def input_findings(drc):
    sections = [s for s in re.split(r'(?m)^Status:\s*', drc)[1:]
                if re.search(r'^; TMC-20011 - [^;]+;', s, re.M)]
    require(len(sections) == 1, 'missing/ambiguous TMC-20011 detail')
    table = rows(sections[0])
    require(['Port', 'Reason', 'Waived'] in table, 'missing input finding columns')
    findings = [r for r in table if len(r) == 3 and r[0] != 'Port']
    require(len(findings) == 2 and {r[0] for r in findings} == set(PORTS) and
            all(r[1] == 'No input delay was set on the input port.' and not r[2] for r in findings),
            'only the two reviewed, unwaived missing-input-delay findings are allowed')
    return sorted(PORTS)


def bindings(directory):
    directory = Path(directory)
    hps = directory / 'combined/hps'
    record = (directory.parent / 'build-inputs.json').read_bytes()
    inputs = json.loads((hps / 'inputs.json').read_text())
    require(inputs['fes_revision'] == json.loads(record)['revision'] and
            inputs['scanout']['build_id'] == build_identity(record), 'source/BUILD_ID mismatch')
    database = hps / 'project/qdb'
    files = sorted(p for p in database.rglob('*') if p.is_file())
    require(files and not any(p.is_symlink() for p in database.rglob('*')), 'missing/linked compiled database')
    return {'policy': POLICY, 'policy_sha256': sha(HERE / 'async_inputs.py'),
        'tcl_sha256': sha(HERE / 'async_inputs.tcl'),
        'build_record_sha256': hashlib.sha256(record).hexdigest(),
        'artifacts': {name: sha(hps / name) for name in ('inputs.json', 'generation.json',
            'linux-boot.core.rbf', 'project/golden_top.qsf',
            'project/output_files/golden_top.tq.drc.signoff.rpt', 'project/output_files/golden_top.sta.summary')},
        'compiled_database': {p.relative_to(database).as_posix(): sha(p) for p in files}}


def collect(directory, quartus_sta):
    """Query a private copy, retaining the original fit and raw failed rules."""
    directory = Path(directory).resolve()
    output = directory.with_name(directory.name + '-async-input-review')
    output.mkdir(exist_ok=False)
    before = bindings(directory)
    copied = output / 'project-inputs'
    shutil.copytree(directory, copied)
    shutil.copyfile(directory.parent / 'build-inputs.json', output / 'build-inputs.json')
    require(bindings(copied) == before, 'copied project differs from fitted inputs')
    reports = output / 'reports'
    reports.mkdir()
    tool = Path(quartus_sta).resolve()
    tool_identity = {'path': str(tool), 'sha256': sha(tool)}
    command = [str(tool), '-t', str(HERE / 'async_inputs.tcl'),
               str(copied / 'combined/hps/project'), str(reports)]
    with (output / 'quartus-sta.log').open('wb') as stream:
        subprocess.run(command, stdout=stream, stderr=subprocess.STDOUT, check=True, timeout=240)
    require(bindings(directory) == before and sha(tool) == tool_identity['sha256'],
            'original fit or STA tool changed during review')
    review_reports(reports)
    receipt = {'bindings': before, 'tool': tool_identity, 'command': command,
               'reports_sha256': {name: sha(reports / name) for name in REPORTS}}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    return output


def verify(directory, review):
    receipt = json.loads((Path(review) / 'receipt.json').read_text())
    require(receipt['bindings'] == bindings(directory), 'fitted input binding changed')
    require(sha(receipt['tool']['path']) == receipt['tool']['sha256'], 'STA tool binding changed')
    reports = Path(review) / 'reports'
    require(receipt['reports_sha256'] == {n: sha(reports / n) for n in REPORTS}, 'report binding changed')
    return {'policy': POLICY, 'receipt_sha256': sha(Path(review) / 'receipt.json'),
            'fitted_inputs': review_reports(reports)}
