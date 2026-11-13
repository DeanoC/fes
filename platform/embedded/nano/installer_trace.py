"""Read-only installer navigation records; these confer no install authority.

The operator must separately verify the boot, SD/media bindings, menu/USB
closure and restoration, immutable content readback and lifecycle sequence.
An explicit generation origin counts completed installs without rewriting
the native counters or weakening the ordinary launch/return decoder.
"""
import json

from service_trace import _unique, number, require
from tenfoot_trace import packet

PREFIX = b'FES_NATIVE_INSTALLER_VIEW '
MAX_BYTES = 32768


def record(line):
    require(type(line) is bytes and 0 < len(line) <= MAX_BYTES and
            line.startswith(PREFIX) and line.endswith(b'\n') and b'\n' not in line[:-1], 'bounded complete installer view required')
    def invalid(_):
        raise ValueError('nonfinite installer view')
    r = json.loads(line[len(PREFIX):], object_pairs_hook=_unique, parse_constant=invalid)
    require(type(r) is dict and set(r) == {'scope','owner','generation','state','settings','systems'} and
            r['scope'] == 'nano-installer-view-v1', 'installer view fields/scope differ')
    require(number(r['owner'], (1<<64)-1) and r['owner'] > 0 and
            number(r['generation'], (1<<64)-1), 'invalid installer ownership observation')
    require(r['state'] in ('ready','loading','running','returning'), 'installer runtime failed or retired')
    for key in ('settings','systems'):
        p = r[key]
        require(type(p) is dict and set(p) == {'open','index','rows','busy','status'}, 'installer panel fields differ')
        require(type(p['open']) is type(p['busy']) is bool and number(p['index'],63), 'invalid installer panel flags/index')
        require(type(p['rows']) is list and len(p['rows']) <= 64 and all(
            type(v) is str and 0 < len(v.encode('utf-8')) <= 128 for v in p['rows']), 'invalid installer panel rows')
        require(type(p['status']) is str and len(p['status'].encode('utf-8')) <= 1024, 'invalid installer panel status')
        require(p['index'] < len(p['rows']) if p['rows'] else p['index'] == 0, 'installer panel index outside rows')
        if not p['open']:
            require(p == dict(open=False,index=0,rows=[],busy=False,status=''), 'closed installer panel retained controls')
    return r


def app_state(line, *, generation_origin, installing=False):
    """Decode actual counters under the caller's explicit operation context."""
    states, evidence, exited = packet(line, generation_origin=generation_origin, installing=installing)
    require(len(states) == 1 and not evidence and exited is None, 'one standalone App state required')
    return states[0]


def matches_state(view, state):
    """Compare cached observations only; not proof of storage/menu ownership."""
    n = state['native']
    return (view['owner'],view['generation'],view['state']) == (n['Owner'],n['Generation'],n['State'])
