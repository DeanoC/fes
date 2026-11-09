"""Check frozen scan-tool bytes and executable mode; no JTAG access or grant."""
import hashlib
import os
from pathlib import Path
import re
import stat


def checked(path, digest, mode):
    path = Path(path)
    if (not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest) or
            type(mode) is not int or not 0 < mode <= 0o777 or not mode & 0o111):
        raise ValueError('explicit scan-tool digest and executable mode required')
    if (not path.is_absolute() or '..' in path.parts or
            any(p.is_symlink() for p in (path, *path.parents))):
        raise ValueError('scan tool must be an absolute regular file without symlinks')
    before = path.stat()
    if (not stat.S_ISREG(before.st_mode) or stat.S_IMODE(before.st_mode) != mode or
            not 0 < before.st_size <= 8 << 20 or not os.access(path, os.X_OK)):
        raise ValueError('scan tool executable mode or access differs')
    with path.open('rb') as stream:
        opened = os.fstat(stream.fileno())
        actual = hashlib.file_digest(stream, 'sha256').hexdigest()
    after = path.stat()
    fields = ('st_dev', 'st_ino', 'st_mode', 'st_uid', 'st_gid', 'st_size',
              'st_mtime_ns', 'st_ctime_ns')
    if (any(getattr(before, k) != getattr(s, k) for s in (opened, after) for k in fields) or
            actual != digest or not os.access(path, os.X_OK)):
        raise ValueError('scan tool changed during executable-file validation')
    return dict(path=str(path), sha256=actual, mode=mode)
