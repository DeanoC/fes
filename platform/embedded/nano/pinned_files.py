"""Phase-local digest admission with live filesystem incarnation checks.

This reduces repeated immutable source/tool hashing in a cooperative host. It
does not defend against privileged metadata forgery or authorize device I/O.
Full digest audits belong at phase boundaries; every live guard checks stamps.
"""
import hashlib
import ctypes
from collections import deque
import os
from pathlib import Path
import stat


def _observe_path(path, stamps, targets):
    """Include intermediate links and directories used during resolution."""
    current = Path(path.anchor)
    pending = deque(path.parts[1:])
    def observe(node):
        if node not in stamps:
            s = node.lstat()
            base = (s.st_dev, s.st_ino, s.st_mode, s.st_uid, s.st_gid)
            # Directory contents may grow; replacing a parent may not.
            stamps[node] = base if stat.S_ISDIR(s.st_mode) else base + (
                s.st_size, s.st_mtime_ns, s.st_ctime_ns)
        return stamps[node][2]

    observe(current)
    links = 0
    while pending:
        name = pending.popleft()
        if name == '..':
            current = current.parent
            continue
        node = current / name
        if not stat.S_ISLNK(observe(node)):
            current = node
            continue
        links += 1
        if links > 40:
            raise ValueError('too many symlinks in pinned path')
        if node not in targets:
            targets[node] = node.readlink()
        target = targets[node]
        parts = target.parts
        if target.is_absolute():
            current = Path(target.anchor)
            parts = parts[1:]
        pending.extendleft(reversed(parts))
    return observe(current)


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
            before = self._stamp()
            for name, stamp in before.items():
                # Watch self-move/delete without subscribing to child changes.
                # In particular, sibling outputs may grow during a phase.
                mask = 0x400 | 0x800 | 0x02000000  # DELETE_SELF, MOVE_SELF, DONT_FOLLOW
                if not stat.S_ISDIR(stamp[2]):
                    mask |= 0x2 | 0x4  # MODIFY, ATTRIB
                if libc.inotify_add_watch(self.fd, os.fsencode(name), mask) < 0:
                    raise OSError(ctypes.get_errno(), 'inotify_add_watch')
            if self._stamp() != before:
                raise ValueError('pinned file incarnation changed while adding watches')
            self._unchanged()
            self.watch_stamps = before
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
        values, targets = {}, {}
        for name in self.expected:
            path = Path(name)
            if not path.is_absolute() or '..' in path.parts:
                raise ValueError('absolute pinned path required')
            if not stat.S_ISREG(_observe_path(path, values, targets)):
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
            if before != self.watch_stamps:
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
