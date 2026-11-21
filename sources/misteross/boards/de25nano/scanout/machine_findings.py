"""Reviewed DE25 signoff findings for a machine compiled from a shared netlist.

pc386's machine is the DE10's pc386_system, compiled from the pc386 lock's
Yosys netlist, and Quartus Pro's design assistant does not recognise every
crossing in it. Deano chose (21 November 2026, #2029) to admit those findings
by reviewed bases, as the Spectrum's are admitted, without changing the
shared RTL and without widening any rule.

A basis admits rows of one rule. It names:

- the exact rows, by a node pattern for each column it binds;
- the constraint in the compiled pc386-clocks.sdc that bounds them, quoted
  verbatim from the file, and that file's digest;
- the synchroniser or handshake that qualifies them, with the source lines
  that show it.

Every row of a reviewed rule must match exactly one basis. A crossing
basis's row count is exact, and a crossing basis that matches nothing is
stale and refused. The advisory rules bind every row by hierarchy and record
the count, which moves from fit to fit. The gate itself is unchanged:
build_menu_package.timing_evidence still requires every failed rule to be
reviewed, and each rule once.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import re

from scripts.fes_build_common import BuildError

POLICY = 'de25-pc386-reviewed-signoff-findings-v1'
HERE = Path(__file__).resolve().parent
CORE = 'fes.pc386'
SDC = 'combined/hps/project/fes_mailbox/machine-clocks.sdc'
QSF = 'combined/hps/project/golden_top.qsf'
E = re.escape('soc_inst|subsys_periph|fes_scanout|fes_scanout|')
S = E + re.escape('system|')
MACHINE = 'fes_machine_pll|machine_pll_outclk0'
PIXEL = 'fes_video_inst|video_clock|iopll_0_outclk0'
CLOCKS = {'machine-to-pixel': (MACHINE, PIXEL), 'pixel-to-machine': (PIXEL, MACHINE)}


def require(condition, message):
    if not condition:
        raise BuildError('DE25 pc386 reviewed signoff findings: ' + message)


def nodes(pattern):
    """A pattern for one cell: one or more space-separated nodes, each matching pattern."""
    return r'(?:%s)(?:\s+(?:%s))*' % (pattern, pattern)


def bus(*names):
    return S + '(?:' + '|'.join(re.escape(n) for n in names) + r')(?:\[(?:\d+|\*)\])?'


def crossing(rule, name, sources, sinks, direction, count, sdc, qualifier):
    return {'rule': rule, 'name': name, 'count': count, 'sdc': sdc, 'qualifier': qualifier,
            'match': {0: nodes(sources), 1: nodes(sinks), 2: re.escape(CLOCKS[direction][0]),
                      3: re.escape(CLOCKS[direction][1])}}


def advisory(rule, name, column, pattern, qualifier):
    return {'rule': rule, 'name': name, 'count': None, 'sdc': (), 'qualifier': qualifier,
            'match': {column: pattern}}


def lint(name, driver, qualifier):
    # The driver, or a fitter duplicate of it: duplication varies from fit to fit.
    return {'rule': 'LNT-30010', 'name': name, 'count': None, 'sdc': (), 'qualifier': qualifier,
            'match': {0: S + re.escape(driver) + r'(?:~_Duplicate_\d+)?'}}


def bases():
    """The pc386 basis table (boards/de25nano/pc386/signoff_bases.py), in pc386's own source closure."""
    path = HERE.parent / 'pc386/signoff_bases.py'
    spec = importlib.util.spec_from_file_location('de25_pc386_signoff_bases', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def selected(directory):
    # Without a producer record nothing is reviewed, so every finding stays
    # unreviewed and the gate refuses: the absence can only make it stricter.
    path = Path(directory).parent / 'build-inputs.json'
    if not path.exists():
        return False
    record = json.loads(path.read_text())
    policy = record['parameters'].get('signoff_findings_review_policy')
    require(policy in (None, POLICY), 'unknown policy ' + str(policy))
    require(policy is None or record['parameters']['core_id'] == CORE, 'policy selected for another core')
    return policy == POLICY


def table(drc, rule):
    """The header and data rows of one rule's detail table."""
    sections = [s for s in re.split(r'(?m)^Status:\s*', drc)[1:]
                if re.search('^; ' + re.escape(rule) + r' - [^;]+;', s, re.M)]
    require(len(sections) == 1, 'missing/ambiguous detail for ' + rule)
    lines = [[c.strip() for c in line.split(';')[1:-1]] for line in sections[0].splitlines()
             if line.startswith(';') and not line.startswith('; ' + rule + ' - ')]
    require(lines and lines[0] and lines[0][-1] == 'Waived', rule + ' detail columns changed')
    return lines[0], lines[1:]


def review(drc, sdc_text, bases):
    """Match every row of each rule the bases name; return one reviewed entry per failed rule."""
    summary = {r[0]: int(r[1]) for r in re.findall(
        r'^; ([A-Z]+-\d+) - [^;]+; (?:Critical|High|Medium|Low|Info)\s*;\s*(\d+)\s*;', drc, re.M)}
    sdc_lines = set(sdc_text.splitlines())
    entries = []
    for rule in sorted({b['rule'] for b in bases}):
        if not summary.get(rule):
            continue
        header, rows = table(drc, rule)
        require(len(rows) == summary[rule], rule + ' detail rows differ from its violation count')
        selected_bases = [b for b in bases if b['rule'] == rule]
        matched = {b['name']: [] for b in selected_bases}
        for row in rows:
            require(len(row) == len(header) and not row[-1], rule + ' row changed shape or is waived')
            hits = [b for b in selected_bases
                    if all(re.fullmatch(pattern, row[column]) for column, pattern in b['match'].items())]
            require(len(hits) == 1, rule + ' row matches %d bases: %s' % (len(hits), ' ; '.join(row)[:300]))
            matched[hits[0]['name']].append(row)
        groups = []
        for basis in selected_bases:
            found = matched[basis['name']]
            if basis['count'] is not None:
                require(len(found) == basis['count'],
                        '%s %s covers %d rows, reviewed %d' % (rule, basis['name'], len(found), basis['count']))
            missing = [line for line in basis['sdc'] if line not in sdc_lines]
            require(not missing, '%s %s constraint not in the compiled SDC: %s' % (rule, basis['name'], missing))
            groups.append({'name': basis['name'], 'rows': len(found), 'constraint': list(basis['sdc']),
                           'qualifier': basis['qualifier']})
        entries.append({'id': rule, 'violations': summary[rule], 'basis': POLICY, 'columns': header,
                        'groups': groups})
    return entries


def assignments(qsf_text, names):
    """The toggle basis relies on the board naming the three chains synchronisers."""
    lines = {' '.join(line.split()) for line in qsf_text.splitlines()}
    missing = [name for name in names if
               'set_instance_assignment -name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS -to "%ssystem|%s"'
               % ('soc_inst|subsys_periph|fes_scanout|fes_scanout|', name) not in lines]
    require(not missing, 'compiled project lacks the synchroniser assignments: ' + ', '.join(missing))


def sources(record, table):
    """The shared RTL the bases' arguments rest on, and the table itself, as the record captured them."""
    inputs = record['source_inputs']
    changed = [name for name, digest in table.SOURCES.items() if inputs.get(name) != digest]
    require(not changed, 'a handshake source the bases cite changed; review it again: ' + ', '.join(changed))
    require(inputs.get(table.TABLE) == hashlib.sha256((HERE.parent / 'pc386/signoff_bases.py').read_bytes()).hexdigest(),
            'the basis table is not the one the record captured')
    return {name: inputs[name] for name in (*table.SOURCES, table.TABLE)}


def findings(directory, drc):
    directory = Path(directory)
    table = bases()
    record = json.loads((directory.parent / 'build-inputs.json').read_text())
    bound = sources(record, table)
    sdc, qsf = directory / SDC, directory / QSF
    require(all(p.is_file() and not p.is_symlink() for p in (sdc, qsf)), 'missing/linked compiled SDC or QSF')
    assignments(qsf.read_text(), table.TOGGLE_ASSIGNMENT)
    entries = review(drc, sdc.read_text(), table.BASES)
    digest = hashlib.sha256(sdc.read_bytes()).hexdigest()
    return [dict(entry, sdc_sha256=digest, sources_sha256=bound) for entry in entries]
