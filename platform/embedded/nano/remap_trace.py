"""Decode raw remapper observations; never infer the TCU input address."""
import re

HEX = r'[0-9a-f]{1,8}'
REGS = (rf'por1=(?P<por1>{HEX}) bypass=(?P<bypass>{HEX}) ctrl=(?P<ctrl>{HEX}) '
        rf'ids=(?P<ids>{HEX}) ar=(?P<ar>{HEX}) aw=(?P<aw>{HEX}) access=unqualified')
RULES = (
    ('remap', re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_REMAP kind=(?P<kind>[12]) phase=(?P<phase>[01]) operand=(?P<operand>[0-9a-f]{{1,16}}) ' + REGS)),
    ('submit', re.compile(rf'(?:NOTICE:[ \t]+)?FES_NANO_SDM_SUBMIT phase=before address=(?P<address>{HEX}) bytes=(?P<bytes>{HEX}) ' + REGS)),
    ('result', re.compile(r'(?:NOTICE:[ \t]+)?FES_NANO_SDM_RESULT status=(?P<status>-?[0-9]{1,11})')),
)
MARKERS = (b'FES_NANO_REMAP', b'FES_NANO_SDM_SUBMIT', b'FES_NANO_SDM_RESULT')


def extract(raw, *, required=False, configuration_scope=False):
    if (type(raw) is not bytes or len(raw) > 1 << 20 or
            type(required) is not bool or type(configuration_scope) is not bool):
        raise ValueError('bounded bytes and explicit observation context required')
    parts = raw.splitlines(keepends=True)
    indices = [i for i, part in enumerate(parts) if any(marker in part for marker in MARKERS)]
    if not indices:
        if required: raise ValueError('remap observation absent')
        return raw, None
    if len(indices) != 6:
        raise ValueError('one complete six-line remap observation required')
    rows = []
    for index in indices:
        line = parts[index]
        if not line.endswith(b'\n') or b'\r' in line.removesuffix(b'\r\n'):
            raise ValueError('partial or malformed remap line')
        try: text = line.removesuffix(b'\n').removesuffix(b'\r').decode('ascii')
        except UnicodeDecodeError as error: raise ValueError('non-ASCII remap line') from error
        matches = [(kind, match) for kind, rule in RULES if (match := rule.fullmatch(text)) is not None]
        if len(matches) != 1: raise ValueError('malformed remap observation')
        kind, match = matches[0]
        values = {key: int(value, 10 if key in ('kind', 'phase', 'status') else 16)
                  for key, value in match.groupdict().items()}
        if kind == 'result' and not -(1 << 31) <= values['status'] < 1 << 31:
            raise ValueError('mailbox result exceeds its C type')
        rows.append(dict(kind=kind, line=index + 1, values=values))
        parts[index] = b'\r\n' if line.endswith(b'\r\n') else b'\n'
    if [r['kind'] for r in rows] != ['remap'] * 4 + ['submit', 'result']:
        raise ValueError('reordered remap/submission/result observation')
    values = [r['values'] for r in rows]
    if [(v['kind'], v['phase']) for v in values[:4]] != [(1, 0), (1, 1), (2, 0), (2, 1)]:
        raise ValueError('repeated/reordered remapper call phases')
    if configuration_scope:
        original = raw.splitlines()
        twelve = [i for i, line in enumerate(original) if re.fullmatch(rb'(?:NOTICE:[ \t]+)?FES_NANO_START phase=12 status=0', line)]
        thirteen = [i for i, line in enumerate(original) if re.fullmatch(rb'(?:NOTICE:[ \t]+)?FES_NANO_START phase=13 status=0', line)]
        writes = [i for i, line in enumerate(original) if b'FES_NANO_CORE_TRANSFER phase=write ' in line]
        completes = [i for i, line in enumerate(original) if b'FES_NANO_CORE_TRANSFER phase=complete ' in line]
        if (len(twelve) != 1 or len(thirteen) != 1 or not writes or len(completes) != 1 or
                not twelve[0] < indices[0] < indices[1] < thirteen[0] < writes[0] < indices[2] or
                not indices[-1] < completes[0]):
            raise ValueError('remap observations outside their first configuration calls')
    return b''.join(parts), dict(records=rows, complete=True,
        mailbox_status=values[-1]['status'], mailbox_accepted=values[-1]['status'] == 0,
        vendor_operand_subtraction_matches=(values[0]['operand'] == values[1]['operand'] == 0 and
            0x80000000 <= values[2]['operand'] < 0xc0000000 and
            values[3]['operand'] == values[2]['operand'] - 0x80000000),
        submission_starts_at_transformed_pointer=values[4]['address'] == values[3]['operand'],
        stream_identity_stable=all(v['ctrl'] == 3 and v['ids'] == 0xa000a for v in values[:5]),
        observations_atomic=False, source_attested=False, SDM_tcu_input=None,
        SDM_route_qualified=False, DMA_access_granted=False, independent_drain=False)
