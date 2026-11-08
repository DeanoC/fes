"""Reuse off-board native validation under a fresh, complete input snapshot.

This record describes files only. It grants no live memory/device/programming
permission and does not replace the boot firmware's physical-state checks.
The operator binds its digest into the frozen preflight and retains its claim,
source, live JTAG, per-operation and hardware observation checks.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import sys

SCOPE = 'nano-checked-native-files-v1'
CODE_TREES = ('platform/embedded/nano', 'platform/embedded/entry', 'platform/embedded/firmware',
              'sources/misteross/prototypes/de25_board_control',
              'sources/misteross/prototypes/embedded_control', 'sources/misteross/scripts')
MAX_RECORD = 16 << 20


def _plain(path, *, directory=False):
    path = Path(path).absolute()
    if '..' in path.parts or any(p.is_symlink() for p in (path, *path.parents)):
        raise ValueError('native admission paths must not traverse symlinks')
    mode = path.stat().st_mode
    if not (stat.S_ISDIR(mode) if directory else stat.S_ISREG(mode)):
        raise ValueError('native admission requires regular files/directories')
    return path


def _sha(path):
    with _plain(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def _json(path):
    path = _plain(path)
    if path.stat().st_size > MAX_RECORD:
        raise ValueError('native admission JSON exceeds its bound')
    value = json.loads(path.read_bytes())
    if not isinstance(value, dict):
        raise ValueError('native admission JSON object required')
    return value


def _relative(root, name):
    p = Path(name)
    if p.is_absolute() or '..' in p.parts or not p.parts:
        raise ValueError('source input must be relative to the workspace')
    return root/p


def _selection(root, uboot, entry):
    root = _plain(root, directory=True)
    uboot, entry = (_plain(p, directory=True) for p in (uboot, entry))
    if any(not p.is_relative_to(root/'out') for p in (uboot, entry)) or uboot == entry:
        raise ValueError('distinct private native build selections required')
    return root, uboot, entry


def _paths(root, uboot, entry):
    return (uboot/'build/u-boot.itb', uboot/'build/spl/u-boot-spl-dtb.hex',
            entry/'entry.img', root/'platform/embedded/nano/sd-profile.json')


def snapshot(root, uboot, entry, extra_inputs=()):
    """Cover complete sealed trees, checker/source roots and selected artifacts.

    Membership is checked as well as bytes. Only interpreter bytecode caches
    are excluded; generated FPGA inputs, vendor archives and reports remain.
    Additional operator inputs, including pinned external tool files, augment
    this mandatory closure. Their explicit paths come from the frozen operator.
    """
    root, uboot, entry = _selection(root, uboot, entry)
    u = _json(uboot/'result.json'); e = _json(entry/'result.json')
    if e.get('target') != 'de25-nano' or e.get('native_program') != 'fpgaload':
        raise ValueError('native file reuse requires the selected fpgaload prototype')
    projects = {_plain(e[name], directory=True) for name in ('hps_project', 'hps_menu_project')}
    if len(projects) != 2 or any(not p.is_relative_to(root/'out') for p in projects):
        raise ValueError('complete distinct sealed computer/menu projects required')
    trees = {root/name for name in CODE_TREES} | projects
    files = set(_paths(root, uboot, entry)) | {uboot/'result.json', entry/'result.json',
            uboot/'build/.config', entry/'payload.elf', Path(__file__).absolute(),
            Path(sys.executable).resolve(strict=True), Path(shutil.which('git') or '').resolve(strict=True)}
    for mapping in (e.get('native_build_sources'), e.get('sources'), u.get('native_handoff', {}).get('producer_sources')):
        if not isinstance(mapping, dict) or not mapping:
            raise ValueError('complete native producer source closure required')
        for name, expected in mapping.items():
            path = _relative(root, name)
            if _sha(path) != expected:
                raise ValueError('selected native source changed: '+name)
            files.add(path)
    for project in projects:
        record = _json(project/'board/hps/build-inputs.json')
        roots = record.get('source_roots')
        if not isinstance(roots, list) or not roots:
            raise ValueError('sealed producer source roots required')
        for name in roots:
            path = _relative(root, name)
            if path.is_dir(): trees.add(_plain(path, directory=True))
            else: files.add(_plain(path))
    for p in extra_inputs:
        path = _plain(p)
        files.add(path)
    directory_entries = {}
    for tree in sorted(trees):
        tree = _plain(tree, directory=True)
        entries = []
        for path in sorted(tree.rglob('*')):
            if '__pycache__' in path.relative_to(tree).parts:
                continue
            if path.is_symlink():
                raise ValueError('symlink in native source/project tree')
            if path.is_dir(): entries.append(str(path.relative_to(tree))+'/')
            else:
                files.add(_plain(path)); entries.append(str(path.relative_to(tree)))
        directory_entries[str(tree)] = entries
    return dict(selection=dict(root=str(root), uboot=str(uboot), entry=str(entry)),
                trees=directory_entries, files={str(p):_sha(p) for p in sorted(files)})


def _value(root, uboot, entry, checked):
    if not isinstance(checked, (tuple, list)) or len(checked) != 6:
        raise ValueError('complete checked_native result required')
    fit, spl, image, u, e, profile = checked
    if tuple(Path(p).absolute() for p in (fit, spl, image, profile)) != _paths(root, uboot, entry):
        raise ValueError('checked native file selection differs')
    if u != _json(uboot/'result.json') or e != _json(entry/'result.json'):
        raise ValueError('checked native receipts differ from current bytes')
    return dict(paths=[str(p) for p in _paths(root, uboot, entry)], uboot=u, entry=e)


def prepare(root, uboot, entry, output, *, validate, extra_inputs=()):
    """Run the unchanged full validator once, then persist its file-only result.

    validate must perform the full source/package/firmware/memory checks. Its
    caller and extra dependencies are frozen by the operator, just as for the
    other injected bring-up hooks. Failed validation never creates a record.
    """
    root, uboot, entry = _selection(root, uboot, entry)
    output = Path(output).absolute()
    _plain(output.parent, directory=True)
    if not output.is_relative_to(root/'out'):
        raise ValueError('native preparation must be private below out/')
    if output.exists() or output.is_symlink():
        raise FileExistsError(output)
    before = snapshot(root, uboot, entry, extra_inputs)
    if (str(output) in before['files'] or
            any(output.is_relative_to(Path(tree)) for tree in before['trees'])):
        raise ValueError('admission record cannot be its own input')
    value = _value(root, uboot, entry, validate(uboot, entry))
    if snapshot(root, uboot, entry, extra_inputs) != before:
        raise ValueError('native inputs changed during full validation')
    record = dict(format=1, scope=SCOPE, live_authority=False, inputs=before, checked=value)
    raw = (json.dumps(record, sort_keys=True, separators=(',', ':'))+'\n').encode()
    if len(raw) > MAX_RECORD:
        raise ValueError('native admission record exceeds bound')
    with output.open('xb') as stream:
        stream.write(raw); stream.flush(); os.fsync(stream.fileno())
    directory = os.open(output.parent, os.O_RDONLY | os.O_DIRECTORY)
    try: os.fsync(directory)
    finally: os.close(directory)
    return hashlib.sha256(raw).hexdigest()


def load(root, uboot, entry, record_path, *, expected_sha256, owns, extra_inputs=()):
    """Return the same checked tuple after fresh closure/claim verification."""
    if not owns(): raise ValueError('native preparation claim lost')
    root, uboot, entry = _selection(root, uboot, entry)
    record_path = _plain(record_path)
    if record_path.stat().st_size > MAX_RECORD:
        raise ValueError('native admission record exceeds bound')
    raw = record_path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected_sha256:
        raise ValueError('native preparation differs from frozen preflight digest')
    record = json.loads(raw)
    if (not isinstance(record, dict) or set(record) != {'format','scope','live_authority','inputs','checked'} or
            record['format'] != 1 or record['scope'] != SCOPE or record['live_authority'] is not False):
        raise ValueError('unsupported native preparation record')
    if snapshot(root, uboot, entry, extra_inputs) != record['inputs']:
        raise ValueError('native preparation source/artifact closure changed')
    value = record['checked']
    if not isinstance(value, dict) or set(value) != {'paths','uboot','entry'} or len(value['paths']) != 4:
        raise ValueError('incomplete native preparation result')
    fit, spl, image, profile = map(Path, value['paths'])
    checked = (fit, spl, image, value['uboot'], value['entry'], profile)
    _value(root, uboot, entry, checked)
    if not owns(): raise ValueError('native preparation claim lost after input checks')
    return checked
