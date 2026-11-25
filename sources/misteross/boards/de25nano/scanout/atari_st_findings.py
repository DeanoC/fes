"""Reviewed DE25 Atari ST placement-noise finding.

Deano's 25 November 2026 decision admits only TMC-20602 rows naming the ST's
``machine_release`` registers. The review is selected by the exact producer
record core ID and does not participate in the separate pc386 review bases.
"""
import json
from pathlib import Path
import re

from scripts.fes_build_common import BuildError

CORE = 'fes.atari-st'
RULE = 'TMC-20602'
BASIS = 'de25-atari-st-machine-release-tension-v1'
QUALIFIER = (
    "Deano 2026-11-25: D12 showed the ST design byte-identical to base, C "
    "reproduced A's RBF, and all 747/747 normalized project inputs matched; "
    "reviewed as fitter placement noise."
)
REGISTER = re.compile(
    r'soc_inst\|subsys_periph\|fes_scanout\|fes_scanout\|'
    r'machine_release\[\d+\](?:~_Duplicate(?:_\d+)?)?'
)


def require(condition, message):
    if not condition:
        raise BuildError('DE25 Atari ST reviewed signoff finding: ' + message)


def selected(directory):
    """Select this basis only for the recorded Atari ST producer."""
    path = Path(directory).parent / 'build-inputs.json'
    if not path.is_file():
        return False
    try:
        record = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as error:
        raise BuildError('DE25 Atari ST reviewed signoff finding: invalid producer record') from error
    parameters = record.get('parameters') if isinstance(record, dict) else None
    return isinstance(parameters, dict) and parameters.get('core_id') == CORE


def detail_table(drc):
    """Return the TMC-20602 column names and all rows from its detail table."""
    sections = [section for section in re.split(r'(?m)^Status:\s*', drc)[1:]
                if re.search(r'^; ' + re.escape(RULE) + r' - [^;]+;', section, re.M)]
    require(len(sections) == 1, 'missing or ambiguous detail table')
    lines = [[column.strip() for column in line.split(';')[1:-1]]
             for line in sections[0].splitlines()
             if line.startswith(';') and not line.startswith('; ' + RULE + ' -')]
    require(bool(lines), 'missing detail-table columns')
    header, rows = lines[0], lines[1:]
    require(header.count('Register Name') == 1 and header.count('Waived') == 1,
            'detail-table columns changed')
    require(rows, 'missing detail rows')
    return header, rows


def findings(directory, drc, rule_rows):
    """Return one reviewed entry, or none when this basis does not apply."""
    if not selected(directory):
        return []
    summary = next((row for row in rule_rows if row[0] == RULE), None)
    require(summary is not None, 'missing rule from the signoff inventory')
    violations = int(summary[2])
    if violations == 0:
        return []

    header, rows = detail_table(drc)
    require(len(rows) == violations, 'detail rows differ from the violation count')
    register_column = header.index('Register Name')
    waived_column = header.index('Waived')
    matched = []
    for row in rows:
        require(len(row) == len(header), 'detail row shape changed')
        require(not row[waived_column], 'a detail row is waived')
        require(REGISTER.fullmatch(row[register_column]) is not None,
                'detail row names an unreviewed register: ' + row[register_column])
        matched.append(dict(zip(header, row)))
    return [{'id': RULE, 'violations': violations, 'matched_rows': matched,
             'basis': BASIS, 'qualifier': QUALIFIER}]
