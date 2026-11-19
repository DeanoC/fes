"""Immutable storage for authenticated functional-build artifacts.

Callers validate producer evidence before publication and on every cache hit.
This module copies bytes, never rewrites an artifact's original provenance.
"""
import fcntl
import hashlib
import json
import re
from pathlib import Path
import shutil
import stat
import tempfile
import importlib.util

# One projection implementation for producer BUILD_ID and the parent cache.
_spec = importlib.util.spec_from_file_location('fes_input_identity',
    Path(__file__).resolve().parents[1] / 'sources/misteross/scripts/input_identity.py')
identity = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(identity)


def functional_key(record):
    fields = json.loads(record)
    return identity.functional_key(fields)


def plain(path, directory=False):
    metadata = Path(path).lstat()
    expected = stat.S_ISDIR if directory else stat.S_ISREG
    if not expected(metadata.st_mode):
        raise ValueError('artifact cache member must not be linked or special')


def store_for(root, record):
    key = functional_key(record)
    if key is None:
        raise ValueError('shared artifact cache requires functional record v2')
    root = Path(root).absolute()
    # Never reinterpret or relabel historical combined-key cache entries.
    if 'identities' in json.loads(record):
        root = root / 'design-v1'
    for ancestor in (*reversed(root.parents), root):
        if ancestor.exists() or ancestor.is_symlink():
            plain(ancestor, directory=True)
    return root / key


def publish(root, record_path, package, *, evidence=None, validate=None):
    """Publish package, record and optional validated evidence in one rename.

    A caller supplying evidence must validate the staged bytes, since checking
    its source before copying would leave a change window before publication.
    """
    if (evidence is None) != (validate is None):
        raise ValueError('evidence and its validator are required together')
    if evidence is not None:
        if not isinstance(evidence, dict) or not evidence:
            raise ValueError('artifact evidence must be a nonempty mapping')
        if any(not isinstance(name, str) or re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]*', name) is None
               or not isinstance(data, bytes) for name, data in evidence.items()):
            raise ValueError('invalid artifact evidence member')
    package = Path(package)
    record_path = Path(record_path)
    plain(package, directory=True)
    plain(record_path)
    record = record_path.read_bytes()
    if json.loads(record).get('parameters', {}).get('selection_only'):
        raise ValueError('selection-only inputs cannot enter the artifact cache')
    store = store_for(root, record)
    store.mkdir(parents=True, exist_ok=True)
    plain(store, directory=True)
    # A single package entry is a directory with a sidecar inside it, published
    # in one rename. A cache reader never sees a half-published record/package.
    fields = json.loads(record)
    evidence_id = fields.get('identities', {}).get('record', package.name)
    target = store / evidence_id
    lock = store / '.publish.lock'
    if lock.exists() or lock.is_symlink():
        plain(lock)
    with lock.open('a+b') as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        contents = {}
        members = {item.name for item in package.iterdir()}
        if members not in ({'manifest.toml', 'core.rbf'}, {'manifest.toml', 'core.rbf', 'rom-map.json'}):
            raise ValueError('artifact package has unexpected members')
        for name in sorted(members):
            plain(package / name)
            contents[package.name + "/" + name] = (package / name).read_bytes()
        contents['build-inputs.json'] = record
        if evidence is not None:
            contents.update({'evidence/' + name: data for name, data in evidence.items()})
        if target.exists() or target.is_symlink():
            plain(target, directory=True)
            plain(target / package.name, directory=True)
            expected_members = {package.name, 'build-inputs.json'} | ({'evidence'} if evidence is not None else set())
            if set(item.name for item in target.iterdir()) != expected_members:
                raise ValueError('existing artifact cache entry has unexpected members')
            if set(item.name for item in (target / package.name).iterdir()) != members:
                raise ValueError('existing artifact package has unexpected members')
            if evidence is not None:
                plain(target / 'evidence', directory=True)
                if {p.name for p in (target / 'evidence').iterdir()} != set(evidence):
                    raise ValueError('existing artifact evidence has unexpected members')
            for name, data in contents.items():
                plain(target / name)
                if (target / name).read_bytes() != data:
                    raise ValueError('existing immutable artifact cache entry differs')
            if validate is not None:
                validate(target / package.name, target / 'build-inputs.json', target / 'evidence')
            return target / package.name
        temporary = Path(tempfile.mkdtemp(prefix='.publish-', dir=store))
        try:
            (temporary / package.name).mkdir()
            if evidence is not None:
                (temporary / 'evidence').mkdir()
            for name, data in contents.items():
                (temporary / name).write_bytes(data)
                (temporary / name).chmod(0o444)
            (temporary / package.name).chmod(0o555)
            if evidence is not None:
                (temporary / 'evidence').chmod(0o555)
                validate(temporary / package.name, temporary / 'build-inputs.json', temporary / 'evidence')
            temporary.chmod(0o555)
            temporary.rename(target)
        finally:
            if temporary.exists():
                temporary.chmod(0o755)
                (temporary / package.name).chmod(0o755)
                if (temporary / 'evidence').exists():
                    (temporary / 'evidence').chmod(0o755)
                shutil.rmtree(temporary)
    return target / package.name
