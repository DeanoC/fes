"""Immutable storage for authenticated functional-build artifacts.

Callers validate producer evidence before publication and on every cache hit.
This module copies bytes, never rewrites an artifact's original provenance.
"""
import fcntl
import hashlib
import json
from pathlib import Path
import shutil
import stat
import tempfile


def functional_key(record):
    fields = json.loads(record)
    if fields.get('format') != 2 or not fields.get('source_inputs'):
        return None
    functional = {key: value for key, value in fields.items()
                  if key not in ('repository', 'revision', 'source_path')}
    encoded = json.dumps(functional, ensure_ascii=False, sort_keys=True,
                         separators=(',', ':')).encode()
    return hashlib.sha256(b'fes-functional-inputs-v2\0' + encoded).hexdigest()


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
    for ancestor in (*reversed(root.parents), root):
        if ancestor.exists() or ancestor.is_symlink():
            plain(ancestor, directory=True)
    return root / key


def publish(root, record_path, package):
    """Publish a previously validated package atomically, retaining its evidence."""
    package = Path(package)
    record_path = Path(record_path)
    plain(package, directory=True)
    plain(record_path)
    record = record_path.read_bytes()
    store = store_for(root, record)
    store.mkdir(parents=True, exist_ok=True)
    plain(store, directory=True)
    # A single package entry is a directory with a sidecar inside it, published
    # in one rename. A cache reader never sees a half-published record/package.
    target = store / package.name
    lock = store / '.publish.lock'
    if lock.exists() or lock.is_symlink():
        plain(lock)
    with lock.open('a+b') as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        contents = {}
        for name in ('manifest.toml', 'core.rbf'):
            plain(package / name)
            contents[package.name + "/" + name] = (package / name).read_bytes()
        contents['build-inputs.json'] = record
        if target.exists() or target.is_symlink():
            plain(target, directory=True)
            plain(target / package.name, directory=True)
            if set(item.name for item in target.iterdir()) != {package.name, 'build-inputs.json'}:
                raise ValueError('existing artifact cache entry has unexpected members')
            if set(item.name for item in (target / package.name).iterdir()) != {'manifest.toml', 'core.rbf'}:
                raise ValueError('existing artifact package has unexpected members')
            for name, data in contents.items():
                plain(target / name)
                if (target / name).read_bytes() != data:
                    raise ValueError('existing immutable artifact cache entry differs')
            return target / package.name
        temporary = Path(tempfile.mkdtemp(prefix='.publish-', dir=store))
        try:
            (temporary / package.name).mkdir()
            for name, data in contents.items():
                (temporary / name).write_bytes(data)
                (temporary / name).chmod(0o444)
            (temporary / package.name).chmod(0o555)
            temporary.chmod(0o555)
            temporary.rename(target)
        finally:
            if temporary.exists():
                temporary.chmod(0o755)
                (temporary / package.name).chmod(0o755)
                shutil.rmtree(temporary)
    return target / package.name
