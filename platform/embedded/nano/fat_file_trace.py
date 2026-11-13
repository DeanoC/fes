"""Expected FAT diagnostic contract; serial observations grant no authority."""
import hashlib
import json
import re

PHASES = ('ready', 'created', 'updated', 'contained', 'resumed', 'pass')
GENERATIONS = (3, 3, 3, 4, 6, 7)
RULES = tuple(('fat_file_'+phase,
    rf'FES_NANO_FATFILE {phase.upper()} cid=(?P<cid>[0-9a-f]{{30}}) sectors=(?P<sectors>[0-9]{{1,20}}) '
    rf'path=(?P<path>FES/SDP-[0-9a-f]{{16}}\.JSN) bytes=(?P<bytes>[0-9]{{1,10}}) '
    rf'sha256=(?P<sha256>[0-9a-f]{{64}}) owner=(?P<owner>[0-9a-f]{{1,16}}) generation=(?P<sd_generation>[0-9]{{1,20}})')
    for phase in PHASES)


def checked_plan(p):
    keys = {'format', 'scope', 'cid', 'sectors', 'mbr_sha256', 'path',
            'recovery_bytes', 'recovery_sha256', 'fit_bytes', 'fit_sha256'}
    if (not isinstance(p, dict) or set(p) != keys or type(p['format']) is not int or p['format'] != 1 or
            p['scope'] != 'nano-sd-fat-file-v1' or not isinstance(p['cid'], str) or
            not re.fullmatch('[0-9a-f]{30}', p['cid']) or p['cid'] in ('0'*30, 'f'*30) or
            type(p['sectors']) is not int or not 131072 <= p['sectors'] <= 1 << 32 or p['sectors'] % 1024 or
            not isinstance(p['path'], str) or not re.fullmatch(r'FES/SDP-[0-9a-f]{16}\.JSN', p['path']) or
            type(p['recovery_bytes']) is not int or not 0 < p['recovery_bytes'] <= 16384 or
            type(p['fit_bytes']) is not int or not 0 < p['fit_bytes'] <= 8 << 20 or
            any(not isinstance(p[k], str) or not re.fullmatch('[0-9a-f]{64}', p[k])
                for k in ('mbr_sha256', 'recovery_sha256', 'fit_sha256'))):
        raise ValueError('independently checked FAT file/card plan required')
    return dict(p)


def payload(path, *, updated):
    """Exact counterpart of fatcheck.Payload for independent file readback."""
    if not isinstance(path, str) or not re.fullmatch(r'FES/SDP-[0-9a-f]{16}\.JSN', path) or type(updated) is not bool:
        raise ValueError('bounded FAT diagnostic path and explicit state required')
    data = json.dumps(dict(format=1, scope='nano-sd-fat-file-v1', path=path,
                           state='updated' if updated else 'created'), separators=(',', ':')).encode()
    return data.ljust(1024 if updated else 4096, b' ')


def content(path, phase):
    data = payload(path, updated=phase != 'created')
    return len(data), hashlib.sha256(data).hexdigest()
