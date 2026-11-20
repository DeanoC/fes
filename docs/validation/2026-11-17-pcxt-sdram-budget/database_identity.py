"""Complete file identities for the Quartus 17 fitted timing databases."""
import hashlib
import json
from pathlib import Path


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def identity(root):
    files = {}
    for name in ('db', 'incremental_db'):
        directory = Path(root) / name
        if not directory.is_dir() or directory.is_symlink():
            raise ValueError('missing or symlinked timing database: ' + str(directory))
        for path in sorted(directory.rglob('*')):
            if path.is_symlink():
                raise ValueError('symlink in timing database: ' + str(path))
            if path.is_file():
                files[path.relative_to(root).as_posix()] = {'size': path.stat().st_size, 'sha256': sha(path)}
    if not files:
        raise ValueError('empty fitted timing database')
    encoded = json.dumps(files, sort_keys=True, separators=(',', ':')).encode()
    return {'files': files, 'sha256': hashlib.sha256(encoded).hexdigest()}


def verify(native, work, collector):
    receipt = json.loads((Path(work) / 'database-receipt.json').read_text())
    current_native = identity(native)
    current_work = identity(work)
    if not (receipt['native_before'] == receipt['pre_asm'] == receipt['pre_sta'] == current_native):
        raise ValueError('fitted timing database does not match native seed')
    if receipt['post_sta'] != current_work:
        raise ValueError('timing database changed after report collection')
    if receipt['collector_sha256'] != sha(collector):
        raise ValueError('timing collector differs from database receipt')
    if receipt['assembled_rbf_sha256'] != sha(Path(native) / 'output_files/top.rbf'):
        raise ValueError('assembled fitted database does not reproduce native RBF')
    actual_reports = {str(p.relative_to(work)): sha(p)
                      for p in (Path(work) / 'conditional').rglob('*') if p.is_file()}
    if not actual_reports or actual_reports != receipt['reports_sha256']:
        raise ValueError('timing reports changed after bound database collection')
    if receipt['sta_exit_code'] != 0 or receipt['asm_exit_code'] != 0:
        raise ValueError('incomplete database binding run')
    return receipt
