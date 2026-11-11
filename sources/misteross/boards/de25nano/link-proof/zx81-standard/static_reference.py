"""Source-bound static ZX81 findings approved on 2026-11-06.

This accepts the reviewed stock board basis, not independent PCB timing.
Numerical timing, independent RBF equality and native acceptance stay separate.
"""
import csv
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

from boards.de25nano.scanout import async_inputs, vendor_reference
from boards.de25nano.producers import review_audio_clocks as audio
from boards.de25nano.producers import build_menu_package as timing
from scripts.fes_build_common import BuildError

POLICY = 'de25-static-stock-reference-v2'
HERE = Path(__file__).resolve().parent
PINS = vendor_reference.OUTPUTS | {'HDMI_SCLK', 'HDMI_LRCLK', 'HDMI_I2S'}
RESET = 'soc_inst|subsys_periph|rst_controller|alt_rst_sync_uq1|altera_reset_synchronizer_int_chain_out'
SYSTEM_CLOCK = 'pll_inst|iopll_0_outclk0'
OPTIONAL_ENABLE = 'soc_inst|subsys_periph|fes_pr_control|fes_pr_control|first_seen'
BOUND_FILES = {'reviewed-inputs.json': '51d8e65979a42f224b7b8e41a4f74a929e9e0d7c718826767bd192d7f75c004d',
               'reviewed-topology.json': '0dbcac3fd72ccdeb60c9dc6a42968a7dd755c007ef3b06623666e30849cd7fd1'}
SLUGS = ('4_slow_fix4_0c', '4_slow_fix4_100c', '4_slow_fix4a_0c', 'MIN_fast_fix4_0c')


def expected_reports():
    names = {'async': {*async_inputs.REPORTS, 'sta.log'},
             'audio': {'clocks.tsv', 'paths.tsv', 'sta.log',
                       *(f'{corner}_{signal}_{kind}_{bound}.rpt' for corner in SLUGS
                         for signal in ('bit_clock', 'word_clock', 'data')
                         for kind in ('network', 'output') for bound in ('min', 'max')),
                       *(f'{corner}_data_all_sources.rpt' for corner in SLUGS)},
             'reset_direct': {'direct-inventory.tsv', 'sta.log'},
             'reset_paths': {'reset-inventory.tsv', 'reset-paths.tsv', 'sta.log',
                            *(f'{corner}_{check}.rpt' for corner in SLUGS
                              for check in ('setup', 'hold', 'recovery', 'removal'))}}
    return {f'reports/{revision}/{category}/{name}' for revision in ('base', 'persona_a', 'persona_b')
            for category, files in names.items() for name in files}


def async_collector():
    original = (async_inputs.HERE / 'async_inputs.tcl').read_text()
    require(original.count('project_open golden_top') == 1, 'input collector selection changed')
    return original.replace('project_open golden_top',
        'project_open -preserve_revision_order -revision [lindex $quartus(args) 2] fes_link_proof').encode()


def require(condition, message):
    if not condition:
        raise BuildError('DE25 static reference: ' + message)


def bound(name):
    raw = (HERE / name).read_bytes()
    require(hashlib.sha256(raw).hexdigest() == BOUND_FILES[name], 'reviewed binding changed: ' + name)
    return json.loads(raw)


def rows(path, fields):
    reader = csv.DictReader(Path(path).read_text().splitlines(), delimiter='\t')
    require(reader.fieldnames == fields, 'changed columns: ' + Path(path).name)
    values = list(reader)
    require(values and all(set(r) == set(fields) and None not in r.values() for r in values),
            'incomplete table: ' + Path(path).name)
    return values


def eligible(record):
    fields = json.loads(record)
    expected = {'core_id': 'fes.zx81', 'programming_profile': 'de25-linux-static-computer-v1',
                'static_profile': 'fes.de25-standard-zx81-empty-ram16k.v1',
                'standard_core': True, 'expansion_socket': True, 'signoff_policy': POLICY,
                'device': 'A5EB013BB23BE4SCS', 'static_default_persona_id': 0xc001a001,
                'placement': 'X2 Y7 X26 Y12', 'routing': 'X1 Y6 X27 Y13'}
    require(all(type(fields['parameters'].get(k)) is type(v) and fields['parameters'][k] == v
                for k, v in expected.items()), 'unselected or unsupported static profile')
    require(all(fields['source_inputs'].get(k) == v for k, v in bound('reviewed-inputs.json').items()),
            'reviewed hardware/clock/integration source changed')
    return fields


def findings(drc, timing):
    expected = {'TMC-20012': ('High', 33), 'TMC-20011': ('High', 2),
                'CDC-50102': ('Low', 1), 'LNT-30010': ('Low', 1)}
    actual = {r['id']: (r['severity'], r['violations']) for r in timing['rules'] if r['violations']}
    require(actual == expected and all(r['waived'] == 0 for r in timing['rules']),
            'unreviewed findings or nonzero waivers')
    tables = {}
    for section in re.split(r'(?m)^Status:\s*', drc)[1:]:
        match = re.search(r'^; ([A-Z]+-\d+) - [^;]+;', section, re.M)
        if match and match[1] in expected:
            require(match[1] not in tables, 'duplicate reviewed rule detail')
            tables[match[1]] = async_inputs.rows(section)[1:]
    require(set(tables) == set(expected), 'missing reviewed rule detail')
    outputs = tables['TMC-20012']
    require(outputs and outputs[0] == ['Port', 'Reason', 'Waived'] and len(outputs) == 34 and
            {r[0] for r in outputs[1:]} == PINS and all(len(r) == 3 and
                r[1:] == ['No output delay was set on the output port.', ''] for r in outputs[1:]),
            'output finding ports/reasons changed')
    async_inputs.input_findings(drc)
    raw_reset_count = raw_enable_count = None
    for rule, table in bound('reviewed-topology.json')['findings'].items():
        actual = tables[rule]
        if rule == 'LNT-30010':
            # DA's descriptive load quantity varies with fitter replication;
            # it is not the violation count or the physical endpoint inventory.
            # Keep the driver, representative reset/enable and all other
            # fields exact. Retain both physical-load quantities; the enable
            # quantity must match the complete fitted inventory below.
            require(len(actual) == 2 and actual[0] == table[0] and len(actual[1]) == 8 and
                    all(actual[1][i] == table[1][i] for i in range(8) if i not in (4, 6)),
                    'changed reviewed topology: ' + rule)
            require(re.fullmatch(r'[1-9][0-9]*', actual[1][4]) is not None,
                    'invalid raw asynchronous reset quantity')
            raw_reset_count = int(actual[1][4])
            require(re.fullmatch(r'[1-9][0-9]*', actual[1][6]) is not None,
                    'invalid raw clock enable quantity')
            raw_enable_count = int(actual[1][6])
        else:
            require(actual == table, 'changed reviewed topology: ' + rule)
    return [{'id': rule, 'severity': severity, 'violations': count, 'basis': POLICY,
             **({'raw_async_reset_signals': raw_reset_count,
                 'raw_clock_enable_signals': raw_enable_count} if rule == 'LNT-30010' else {})}
            for rule, (severity, count) in expected.items()]


def audio_reports(directory):
    directory = Path(directory)
    columns = ['corner', 'clock', 'path', 'bound', 'count', 'delay_ns']
    values = rows(directory / 'paths.tsv', columns)
    # Reuse the unchanged clock/waveform checker on its exact two-clock view;
    # then require complete additional live-data coverage below.
    with tempfile.TemporaryDirectory(prefix='de25-static-clock-view-') as temporary:
        view = Path(temporary)
        (view / 'clocks.tsv').write_bytes((directory / 'clocks.tsv').read_bytes())
        with (view / 'paths.tsv').open('w') as stream:
            writer = csv.DictWriter(stream, fieldnames=columns, delimiter='\t')
            writer.writeheader()
            writer.writerows(r for r in values if r['clock'] != 'data')
        clocks = audio.observations(view)['clocks']
    paths = {}
    for row in values:
        key = tuple(row[k] for k in columns[:4])
        require(key not in paths and row['count'] == '1', 'missing/duplicate live audio path')
        paths[key] = audio.number(row['delay_ns'])
    require(set(paths) == {(corner, signal, kind, bound) for corner in audio.CORNERS
            for signal in ('bit_clock', 'word_clock', 'data')
            for kind in ('network', 'output') for bound in ('min', 'max')},
            'incomplete live audio corner/path coverage')
    period = Decimal(clocks[audio.MASTER]['period_ns'])
    budgets = []
    for corner in sorted(audio.CORNERS):
        for signal in ('bit_clock', 'word_clock', 'data'):
            for kind in ('network', 'output'):
                require(paths[corner, signal, kind, 'min'] <= paths[corner, signal, kind, 'max'],
                        'inverted live audio path bounds')
        def delay(signal, bound):
            return sum(paths[corner, signal, kind, bound] for kind in ('network', 'output'))
        for signal in ('data', 'word_clock'):
            setup = period * 2 + delay('bit_clock', 'min') - delay(signal, 'max') - Decimal('2.010')
            hold = period * 2 + delay(signal, 'min') - delay('bit_clock', 'max') - Decimal('2.010')
            require(setup > 0 and hold > 0, 'nonpassing conditional external audio budget')
            budgets.append(dict(corner=corner, signal=signal, setup_ns=str(setup), hold_ns=str(hold)))
    reports = list(directory.glob('*_data_all_sources.rpt'))
    require(len(reports) == 4, 'incomplete live data source inventory')
    for report in reports:
        text = report.read_text()
        counts = re.findall(r'Report Path: Found ([0-9]+) paths', text)
        nodes = re.findall(r'^; From Node[ \t]*; ([^;\n]+);', text, re.M)
        require(counts == ['2'] and len(nodes) == 2 and {n.strip() for n in nodes} == {
            'fes_audio_inst|audio|serializer|sdata',
            'fes_audio_inst|clock|iopll_0|tennm_ph2_iopll~pll_ctrl_reg'},
            'unexpected/truncated I2S output source inventory')
    return {'conditional_external_budgets': budgets,
            'unmodeled': 'PCB skew, jitter, uncertainty and signal integrity; electrical startup/unlock',
            'independent_pcb_timing_qualified': False}


def reset_reports(inventory, paths):
    table = rows(inventory, ['kind', 'name'])
    require(len(table) == len({(r['kind'], r['name']) for r in table}), 'duplicate reset inventory row')
    kinds = {'direct_async', 'all_async', 'direct_ena', 'all_ena'}
    require({r['kind'] for r in table} == kinds, 'incomplete reset inventory')
    groups = {kind: {r['name'] for r in table if r['kind'] == kind} for kind in kinds}
    expected = bound('reviewed-topology.json')['reset']
    # Quartus may create/remove physical copies of a reviewed register. Only
    # this exact fitter suffix identifies a copy; new logical consumers
    # still fail. Coverage below uses the original physical names, including
    # every copy, without coalescing a single timing endpoint.
    logical = lambda names: {re.sub(r'~DUPLICATE[0-9]*$', '', name) for name in names}
    require(logical(groups['direct_async']) == logical(expected['direct_async']),
            'changed direct reset/enable consumers: direct_async')
    required_enable = logical(expected['direct_ena'])
    require(required_enable <= logical(groups['direct_ena']) <= required_enable | {OPTIONAL_ENABLE},
            'changed direct reset/enable consumers: direct_ena')
    require(groups['direct_async'] == groups['all_async'] and groups['direct_ena'] <= groups['all_ena'],
            'reset topology coverage differs')
    timing = rows(paths, ['corner', 'check', 'from', 'to', 'slack_ns', 'from_clock', 'to_clock'])
    require(len(timing) == len({(r['corner'], r['check'], r['to']) for r in timing}),
            'duplicate reset timing endpoint')
    require({r['corner'] for r in timing} == audio.CORNERS and
            {r['check'] for r in timing} == {'setup', 'hold', 'recovery', 'removal'},
            'incomplete reset timing corner/check coverage')
    minimum = {}
    for corner in audio.CORNERS:
        for check in ('setup', 'hold', 'recovery', 'removal'):
            selected = [r for r in timing if (r['corner'], r['check']) == (corner, check)]
            targets = groups['direct_async'] if check in ('recovery', 'removal') else groups['direct_ena']
            require({r['to'] for r in selected} == targets, 'missing direct reset/enable timed endpoint')
            for row in selected:
                require(row['from'] == RESET and row['from_clock'] == row['to_clock'] == SYSTEM_CLOCK,
                        'changed reset launch/receiving clock')
                slack = audio.number(row['slack_ns'])
                minimum[check] = min(slack, minimum.get(check, slack))
    return {'direct_async': len(groups['direct_async']), 'logical_async': len(logical(groups['direct_async'])),
            'direct_ena': len(groups['direct_ena']),
            'timed_paths': len(timing), 'minimum_slack_ns': {k: str(v) for k, v in minimum.items()}}


def reset_finding_count(reviewed, reset):
    counts = [r.get('raw_async_reset_signals') for r in reviewed if r['id'] == 'LNT-30010']
    require(len(counts) == 1 and type(counts[0]) is int and 0 < counts[0] <= reset['direct_async'],
            'raw reset quantity exceeds the complete physical reset inventory')
    enables = [r.get('raw_clock_enable_signals') for r in reviewed if r['id'] == 'LNT-30010']
    require(len(enables) == 1 and type(enables[0]) is int and enables[0] == reset['direct_ena'],
            'raw enable quantity differs from the complete physical enable inventory')
    return counts[0]


def project_bindings(project, record):
    project = Path(project)
    eligible(record)
    names = ['inputs.json', 'build.json', 'artifacts.json', 'root_partition.qdb',
             'golden_top.v', 'fes_mailbox/av_pll.ip', 'fes_combined/sys_pll.ip']
    names += [f'{revision}.qsf' for revision in ('base', 'persona_a', 'persona_b')]
    names += [f'output_files/{revision}.{suffix}' for revision in ('base', 'persona_a', 'persona_b')
              for suffix in ('pin', 'sta.summary', 'sta.rpt', 'tq.drc.signoff.rpt')]
    return {'build_record_sha256': hashlib.sha256(record).hexdigest(),
            'fitted_files_sha256': {n: async_inputs.sha(project / n) for n in names}}


def stock_aliases(revision):
    return {'golden_top.qsf': revision + '.qsf',
             'output_files/golden_top.pin': 'output_files/' + revision + '.pin',
             'fes_combined/sys_pll.ip': 'fes_combined/sys_pll.ip',
             'fes_mailbox/av_pll.ip': 'fes_mailbox/av_pll.ip',
             'fes_mailbox/fes_de25_scanout_board.v': 'fes_mailbox/fes_de25_scanout_board.v',
             **{'fes_mailbox/' + n: 'fes_mailbox/' + n for n in (
                 'fes_de25_scanout_hdmi_config.v', 'fes_de25_scanout_i2c_controller.v')}}


def stock_view(project, revision, destination):
    """Use unchanged reference checkers with byte-preserving revision aliases."""
    project, destination = Path(project), Path(destination)
    names = stock_aliases(revision)
    destination.mkdir()
    for target, source in names.items():
        path = destination / target
        path.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(project / source, path)
    return names


def collect(project, record, quartus_sta, archive):
    project = Path(project)
    before = project_bindings(project, record)
    output = project.parents[3] / 'static-board-review'
    require(not output.exists(), 'review destination exists')
    output.mkdir()
    copied = output / 'project-inputs'
    shutil.copytree(project, copied)
    require(project_bindings(copied, record) == before, 'copied fit differs from selected inputs')
    # This is the existing asynchronous-input collector with only its explicit
    # project/revision selection adapted. Its constraints and checks are intact.
    async_script = output / 'async-inputs.tcl'
    async_script.write_bytes(async_collector())
    collectors = {'async': async_script, 'audio': HERE / 'review-audio.tcl',
                  'reset_direct': HERE / 'review-reset-direct.tcl',
                  'reset_paths': HERE / 'review-reset-paths.tcl'}
    collector_hashes = {k: async_inputs.sha(p) for k, p in collectors.items()}
    tool_hash = async_inputs.sha(quartus_sta)
    archive = Path(archive)
    require(async_inputs.sha(archive) == vendor_reference.ARCHIVE_SHA256, 'vendor archive changed')
    import zipfile
    with zipfile.ZipFile(archive) as package:
        require(all(package.namelist().count(n) == 1 for n in vendor_reference.MEMBERS),
                'missing/duplicate vendor member')
        members = {n: package.read(n) for n in vendor_reference.MEMBERS}
    vendor = output / 'vendor'
    for name, data in members.items():
        path = vendor / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    stock = {}
    for revision in ('base', 'persona_a', 'persona_b'):
        print('DE25 static reference: ' + revision, flush=True)
        view = output / ('stock-' + revision)
        aliases = stock_view(project, revision, view)
        stock[revision] = {'aliases': aliases,
            'video': vendor_reference.validate_project(view, vendor_reference.profile(members),
                                                       'fes.zx81', standard_core=True),
            'audio': audio.stock_audio(view, archive)}
        for category, script in collectors.items():
            reports = output / 'reports' / revision / category
            reports.mkdir(parents=True)
            command = [str(quartus_sta), '-t', str(script), str(copied), str(reports), revision]
            with (reports / 'sta.log').open('w') as stream:
                subprocess.run(command, check=True, stdout=stream, stderr=subprocess.STDOUT, timeout=240)
    files = {p.relative_to(output).as_posix(): async_inputs.sha(p)
             for p in (output / 'reports').rglob('*') if p.is_file()}
    require(set(files) == expected_reports(), 'collector report inventory differs')
    require(async_inputs.sha(quartus_sta) == tool_hash and
            {k: async_inputs.sha(p) for k, p in collectors.items()} == collector_hashes,
            'STA tool or collector changed during review')
    require(project_bindings(project, record) == before, 'original fitted inputs changed during review')
    receipt = {'policy': POLICY, 'bindings': before, 'reports_sha256': files,
               'collector_sha256': collector_hashes,
               'tool': {'path': str(Path(quartus_sta).resolve()), 'sha256': tool_hash},
               'vendor_archive': str(archive.resolve()), 'stock': stock}
    (output / 'receipt.json').write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')
    return verify(project, record, output)


def verify(project, record, output=None):
    project = Path(project)
    output = Path(output) if output is not None else project.parents[3] / 'static-board-review'
    require((output / 'receipt.json').is_file(), 'missing fitted static board review')
    receipt = json.loads((output / 'receipt.json').read_text())
    require(receipt['policy'] == POLICY and receipt['bindings'] == project_bindings(project, record),
            'review belongs to different source or fitted inputs')
    require(async_inputs.sha(receipt['tool']['path']) == receipt['tool']['sha256'], 'STA tool binding changed')
    observed = {p.relative_to(output).as_posix(): async_inputs.sha(p)
                for p in (output / 'reports').rglob('*') if p.is_file()}
    require(set(observed) == expected_reports() and observed == receipt['reports_sha256'],
            'review reports changed or missing')
    require((output / 'async-inputs.tcl').read_bytes() == async_collector(), 'input collector changed')
    require(receipt['collector_sha256'] == {'async': hashlib.sha256(async_collector()).hexdigest(),
            **{k: async_inputs.sha(HERE / n) for k, n in (
                ('audio', 'review-audio.tcl'), ('reset_direct', 'review-reset-direct.tcl'),
                ('reset_paths', 'review-reset-paths.tcl'))}}, 'review collector binding changed')
    members = {n: (output / 'vendor' / n).read_bytes() for n in vendor_reference.MEMBERS}
    expected = vendor_reference.profile(members)
    revisions = ('base', 'persona_a', 'persona_b')
    require(set(receipt['stock']) == set(revisions), 'incomplete stock reference revisions')
    result = {}
    for revision in revisions:
        selected = receipt['stock'][revision]
        require(selected['aliases'] == stock_aliases(revision), 'changed stock revision aliases')
        # Compare the raw originals again, not just the byte-preserving views.
        for alias, original in selected['aliases'].items():
            require((output / ('stock-' + revision) / alias).read_bytes() == (project / original).read_bytes(),
                    'stock view differs from selected revision')
        video = vendor_reference.validate_project(output / ('stock-' + revision), expected,
                                                  'fes.zx81', standard_core=True)
        require(video == selected['video'], 'stock video reference changed')
        require(audio.stock_audio(output / ('stock-' + revision), Path(receipt['vendor_archive'])) ==
                selected['audio'], 'stock audio reference changed')
        reports = output / 'reports' / revision
        raw = timing.timing_evidence(project.parents[2], revision=revision)
        reviewed = findings((project / 'output_files' / (revision + '.tq.drc.signoff.rpt')).read_text(), raw)
        reset = reset_reports(reports / 'reset_direct/direct-inventory.tsv', reports / 'reset_paths/reset-paths.tsv')
        reset['raw_DA_async_load_count'] = reset_finding_count(reviewed, reset)
        proof = {'policy': POLICY, 'receipt_sha256': async_inputs.sha(output / 'receipt.json'),
                 'stock_video_reference': video, 'stock_audio_reference': selected['audio'],
                 'async_inputs': async_inputs.review_reports(reports / 'async'),
                 'audio': audio_reports(reports / 'audio'),
                 'reset': reset,
                 'independent_pcb_timing_qualified': False, 'hardware_acceptance': False}
        result[revision] = dict(raw, reviewed_findings=reviewed, unreviewed_failed_rules=0,
            static_board_review=proof, package_timing_admission=raw['all_reported_timing_pass'])
    return result
