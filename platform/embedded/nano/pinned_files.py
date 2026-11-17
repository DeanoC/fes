"""Phase-local digest admission with live filesystem incarnation checks.

This reduces repeated immutable source/tool hashing in a cooperative host. It
does not defend against privileged metadata forgery or authorize device I/O.
Full digest audits belong at phase boundaries; every live guard checks stamps.
"""
import hashlib
import ctypes
import os
from pathlib import Path
import stat


class PinnedFiles:
    def __init__(self, expected):
        if not expected:
            raise ValueError('explicit file digests required')
        self.expected = dict(expected)
        self.stamps = None
        self.retired = False
        self.audits = self.checks = 0
        self.fd = None
        # Stat timestamps alone can miss edit-and-restore within one filesystem
        # tick. Watch writes/attribute changes as well; overflow also fails shut.
        libc = ctypes.CDLL(None, use_errno=True)
        self.fd = libc.inotify_init1(os.O_NONBLOCK | os.O_CLOEXEC)
        if self.fd < 0:
            self.fd = None
            raise OSError(ctypes.get_errno(), 'inotify_init1')
        try:
            for name in self.expected:
                if libc.inotify_add_watch(self.fd, os.fsencode(Path(name).resolve(strict=True)),
                                          0x2 | 0x4 | 0x400 | 0x800) < 0:
                    raise OSError(ctypes.get_errno(), 'inotify_add_watch')
        except BaseException:
            self.close()
            raise

    def close(self):
        self.retired = True
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None

    def _unchanged(self):
        try:
            event = os.read(self.fd, 65536)
        except BlockingIOError:
            return
        if event:
            raise ValueError('pinned file mutation observed')
        raise ValueError('file mutation watch closed')

    def _stamp(self):
        values = {}
        for name in self.expected:
            path = Path(name)
            if not path.is_absolute() or '..' in path.parts:
                raise ValueError('absolute pinned path required')
            resolved = path.resolve(strict=True)
            for p in (path, *path.parents, resolved, *resolved.parents):
                s = p.lstat()
                # Directory contents may grow; replacing a parent may not.
                base = (s.st_dev, s.st_ino, s.st_mode, s.st_uid, s.st_gid)
                values[str(p)] = base if stat.S_ISDIR(s.st_mode) else base + (
                    s.st_size, s.st_mtime_ns, s.st_ctime_ns)
            if not resolved.is_file():
                raise ValueError('pinned regular file required')
        return values

    def check(self):
        try:
            if self.retired or self.stamps is None or self._stamp() != self.stamps:
                raise ValueError('pinned file incarnation changed')
            self._unchanged()
            self.checks += 1
            return True
        except BaseException:
            self.close()
            raise

    def audit(self):
        try:
            if self.retired:
                raise ValueError('file admission is retired')
            self._unchanged()
            before = self._stamp()
            if self.stamps is not None and before != self.stamps:
                raise ValueError('pinned file incarnation changed')
            for name, digest in self.expected.items():
                with Path(name).open('rb') as stream:
                    if hashlib.file_digest(stream, 'sha256').hexdigest() != digest:
                        raise ValueError('pinned file digest changed')
            if self._stamp() != before:
                raise ValueError('pinned file changed during digest audit')
            self._unchanged()
            self.stamps = before
            self.audits += 1
            return True
        except BaseException:
            self.close()
            raise
