"""Bounded compiler subprocess open audit for the Markdown closure policy."""
import re
import subprocess
import tempfile
import signal
import time
from pathlib import Path
import json
import os
import stat

TRACER = Path('/usr/bin/strace')
POLICY = 'compiler-markdown-v1'
MAX_TRACE_FILE = 32 * 1024 * 1024
MAX_TRACE_TOTAL = 128 * 1024 * 1024


class ReadAuditError(ValueError):
    """Fatal: this invocation cannot qualify a documentation-excluding record."""


def excluded_markdown(path, mode):
    return mode == '100644' and Path(path).suffix.lower() in ('.md', '.markdown')


def decode_path(value):
    result = bytearray()
    escapes = {'n': 10, 'r': 13, 't': 9, 'v': 11, 'f': 12, 'a': 7, 'b': 8, '\\': 92, '"': 34}
    i = 0
    while i < len(value):
        if value[i] != '\\':
            result.extend(value[i].encode('utf-8')); i += 1; continue
        i += 1
        if i >= len(value):
            raise ReadAuditError('truncated trace escape')
        if value[i] in escapes:
            result.append(escapes[value[i]]); i += 1
        elif value[i] in '01234567':
            match = re.match(r'[0-7]{1,3}', value[i:])
            result.append(int(match[0], 8)); i += len(match[0])
        else:
            raise ReadAuditError('unknown trace escape')
    try:
        return result.decode('utf-8')
    except UnicodeError as exc:
        raise ReadAuditError('unrepresentable trace pathname') from exc


def _covered(relative, roots):
    return any(relative == root or relative.startswith(root + '/') for root in roots)


def _record(event, relative):
    destination = os.environ.get('FES_SOURCE_READ_RECORD')
    if destination:
        with open(destination, 'a', encoding='utf-8') as stream:
            stream.write(json.dumps({'event': event, 'path': relative}, sort_keys=True) + '\n')


def _enforce_closure(relative, roots, event):
    _record(event, relative)
    if roots is not None and os.environ.get('FES_SOURCE_CLOSURE_RECORD_ONLY') != '1' and not _covered(relative, roots):
        raise ReadAuditError(f'source {event} outside audited closure: {relative}')


def verify_traces(directory, source_root, closure=None):
    traces = sorted(directory.glob('open.*'))
    if not traces:
        raise ReadAuditError('compiler produced no read audit')
    total = 0
    for trace in traces:
        if trace.is_symlink() or not trace.is_file():
            raise ReadAuditError('read audit must be regular')
        size = trace.stat().st_size
        total += size
        if size > MAX_TRACE_FILE or total > MAX_TRACE_TOTAL:
            raise ReadAuditError('read audit exceeds bounded trace size')
        lines = trace.read_text().splitlines()
        if not lines or not re.fullmatch(r'\+\+\+ (exited with \d+|killed by SIG[A-Z0-9]+(?: \(core dumped\))?) \+\+\+', lines[-1]):
            raise ReadAuditError('compiler trace lacks process completion: ' + ascii(lines[-1] if lines else '<empty>')[:320])
        for line in lines[:-1]:
            # strace emits a separate group-stop notification when cancellation
            # stops a multithreaded tracee before killing its private group.
            # It records no file access; completion and every open still matter.
            if not line or line == '--- stopped by SIGSTOP ---' or re.fullmatch(r'--- SIG[A-Z0-9]+ \{.*\} ---', line):
                continue
            if 'io_uring_setup(' in line or 'open_by_handle_at(' in line:
                raise ReadAuditError('compiler used an unauditable file-open mechanism')
            listing = re.fullmatch(r'getdents(?:64)?\(\d+<(.*?)>, .*\)\s+= \d+', line)
            if listing:
                listed = Path(decode_path(listing[1]))
                if not listed.is_absolute():
                    raise ReadAuditError('compiler listing has no absolute resolved path')
                if listed.is_relative_to(source_root):
                    relative = listed.relative_to(source_root).as_posix()
                    if relative != 'build' and not relative.startswith('build/'):
                        _enforce_closure(relative, closure, 'list')
                continue
            match = re.fullmatch(r'(open|openat|openat2)\((.*)\)\s+= (\d+)<(.*)>', line)
            if match is None or '<unfinished ...>' in line or ' resumed>' in line:
                raise ReadAuditError('incomplete or unrecognized compiler open trace: ' + ascii(line)[:320])
            # Character-device annotations follow the resolved pathname.
            value = match[4]
            if '<' in value:
                annotation = re.fullmatch(r'(.*)<(?:char|block) \d+:\d+>', value)
                if annotation is None:
                    raise ReadAuditError('unknown resolved descriptor annotation: ' + ascii(line)[:320])
                value = annotation[1]
            if value.endswith(' (deleted)'):
                value = value[:-10]
            try:
                path = Path(decode_path(value))
            except ReadAuditError as exc:
                raise ReadAuditError(str(exc) + ': ' + ascii(line)[:320]) from exc
            if not path.is_absolute():
                raise ReadAuditError('compiler open has no absolute resolved path')
            if not path.is_relative_to(source_root):
                continue
            relative = path.relative_to(source_root)
            arguments = re.sub(r'"(?:\\.|[^"\\])*"', '""', match[2])
            arguments = re.sub(r'<[^>]*>', '', arguments)
            if re.search(r'\bO_WRONLY\b', arguments):
                continue
            if not relative.parts or relative.parts[0] != 'build':
                if path.is_symlink() or (not path.exists() and closure is not None):
                    raise ReadAuditError('compiler opened missing or linked source: ' + ascii(str(relative))[:320])
                if path.is_file():
                    _enforce_closure(relative.as_posix(), closure, 'read')
            if path.suffix.lower() not in ('.md', '.markdown'):
                continue
            if path.is_symlink() or not path.is_file():
                raise ReadAuditError('compiler opened missing or linked Markdown')
            if path.stat().st_mode & 0o111:
                continue  # Executable Markdown remains in the source closure.
            raise ReadAuditError('compiler read excluded Markdown: ' + ascii(str(relative))[:320])


def _trace_sizes(directory):
    total = 0
    for path in directory.glob('open.*'):
        if path.is_symlink() or not path.is_file():
            raise ReadAuditError('read audit must be regular')
        size = path.stat().st_size
        total += size
        if size > MAX_TRACE_FILE or total > MAX_TRACE_TOTAL:
            raise ReadAuditError('read audit exceeds bounded trace size')


def _stop_tracees(process):
    # Stop the entire private group before enumerating it, so ordinary child
    # forks cannot race cleanup. Leave strace alive to record child termination.
    try:
        os.killpg(process.pid, signal.SIGSTOP)
        for entry in Path('/proc').iterdir():
            if entry.name.isdigit() and int(entry.name) != process.pid:
                try:
                    if os.getpgid(int(entry.name)) == process.pid:
                        os.kill(int(entry.name), signal.SIGKILL)
                except ProcessLookupError:
                    pass
        os.kill(process.pid, signal.SIGCONT)
        return process.communicate(timeout=5)
    except (ProcessLookupError, subprocess.TimeoutExpired):
        process.kill()  # --kill-on-exit also terminates any escaped tracee.
        return process.communicate()


def audited_run(command, *, source_root, **kwargs):
    source_root = Path(source_root).absolute()
    if any(path.is_symlink() for path in (source_root, *source_root.parents)):
        raise ReadAuditError('compiler source must not traverse symlinks')
    source_root = source_root.resolve()
    active = _ACTIVE.get()
    closure = active[1] if active is not None and active[2] else None
    timeout = kwargs.pop('timeout', None)
    check = kwargs.pop('check', False)
    deadline = time.monotonic() + timeout if timeout is not None else None
    with tempfile.TemporaryDirectory(prefix='fes-read-audit-') as temporary:
        directory = Path(temporary)
        args = [str(TRACER), '--kill-on-exit', '-ff', '-yy', '-s', '0',
                '-e', 'status=successful', '-e', 'trace=open,openat,openat2,open_by_handle_at,io_uring_setup,getdents,getdents64',
                '-o', str(directory / 'open'), '--', *map(str, command)]
        try:
            process = subprocess.Popen(args, start_new_session=True, **kwargs)
        except OSError as exc:
            raise ReadAuditError('compiler read audit could not start') from exc
        try:
            while True:
                _trace_sizes(directory)
                remaining = deadline - time.monotonic() if deadline is not None else 0.1
                if remaining <= 0:
                    stdout, stderr = _stop_tracees(process)
                    verify_traces(directory, source_root, closure)
                    raise subprocess.TimeoutExpired(command, timeout, output=stdout, stderr=stderr)
                try:
                    stdout, stderr = process.communicate(timeout=min(0.1, remaining))
                    break
                except subprocess.TimeoutExpired:
                    pass
            verify_traces(directory, source_root, closure)
        except BaseException:
            if process.poll() is None:
                _stop_tracees(process)
            raise
        result = subprocess.CompletedProcess(command, process.returncode, stdout, stderr)
        if check:
            result.check_returncode()
        return result

# Python audit hooks are permanent, but this policy is active only inside an
# explicit context in the calling thread. It covers ordinary Python file APIs,
# not adversarial native extensions issuing raw system calls.
import contextvars
import contextlib
import functools
import inspect
import os
import sys

_ACTIVE = contextvars.ContextVar('fes_source_read_guard', default=None)


def _python_open(event, args):
    active = _ACTIVE.get()
    if active is None or event not in ('open', 'os.listdir', 'os.scandir'):
        return
    root, roots, narrowed = active
    if event in ('os.listdir', 'os.scandir'):
        path = args[0] if args else '.'
        if isinstance(path, int):
            try:
                if sys.platform == 'darwin':
                    import fcntl
                    path = fcntl.fcntl(path, 50, bytes(1024)).split(b'\0', 1)[0]
                else:
                    path = os.readlink('/proc/self/fd/' + str(path))
            except OSError as exc:
                raise ReadAuditError('cannot resolve listed directory descriptor') from exc
        resolved = Path(os.fsdecode(path or '.')).resolve()
        if resolved.is_relative_to(root):
            relative = resolved.relative_to(root).as_posix()
            if relative != 'build' and not relative.startswith('build/'):
                _enforce_closure(relative, roots if narrowed else None, 'list')
        return
    path, mode, flags = args
    if flags & os.O_ACCMODE == os.O_WRONLY:
        return
    if isinstance(path, int):
        if not stat.S_ISREG(os.fstat(path).st_mode):
            return
        try:
            if sys.platform == 'darwin':
                import fcntl
                path = fcntl.fcntl(path, 50, bytes(1024)).split(b'\0', 1)[0]
            else:
                path = os.readlink('/proc/self/fd/' + str(path))
        except OSError as exc:
            raise ReadAuditError('cannot resolve helper file descriptor') from exc
    try:
        resolved = Path(os.fsdecode(path)).resolve()
    except (TypeError, ValueError, OSError) as exc:
        raise ReadAuditError('cannot resolve helper open') from exc
    if not resolved.is_relative_to(root):
        return
    # The open event carries no dir_fd, so a relative name opened under a
    # directory descriptor (e.g. shutil.rmtree's os.open(name, dir_fd=...))
    # resolves against the working directory here. Only an existing regular
    # file can be a source read; directories are covered by the list events
    # and a missing path cannot be read.
    if not resolved.is_file():
        return
    relative = resolved.relative_to(root).as_posix()
    if '__pycache__' in resolved.parts and resolved.suffix == '.pyc':
        source = resolved.parent.parent / (resolved.name.split('.', 1)[0] + '.py')
        if not source.is_file():
            raise ReadAuditError('Python imported bytecode without source: ' + ascii(relative)[:320])
        relative = source.relative_to(root).as_posix()
    if relative != 'build' and not relative.startswith('build/'):
        _enforce_closure(relative, roots if narrowed else None, 'read')
    if resolved.suffix.lower() in ('.md', '.markdown'):
        if not resolved.is_file() or not resolved.stat().st_mode & 0o111:
            raise ReadAuditError('Python helper read excluded Markdown: ' + ascii(relative)[:320])


sys.addaudithook(_python_open)


@contextlib.contextmanager
def python_source_guard(root, roots):
    from scripts.functional_execution import AuditedRoots
    token = _ACTIVE.set((Path(root).resolve(), tuple(roots), isinstance(roots, AuditedRoots)))
    try:
        yield
    finally:
        _ACTIVE.reset(token)


def guard_functional_source(function):
    """Scope ordinary helper reads for an explicit v2 producer entry point."""
    signature = inspect.signature(function)
    @functools.wraps(function)
    def guarded(*args, **kwargs):
        bound = signature.bind(*args, **kwargs)
        bound.apply_defaults()
        if bound.arguments.get('identity_version', 1) != 2:
            return function(*args, **kwargs)
        from scripts.functional_execution import producer_name, source_roots_for_producer
        inputs = function.__globals__['PINNED_INPUTS']
        # A producer run as a script has __module__ == '__main__'.
        name = producer_name(function.__module__, bound.arguments['root']) or function.__module__
        if name.endswith('build_fes_ramtest'):
            inputs = function.__globals__['inputs_for'](bound.arguments.get('memory_mhz', 100))
        elif name.endswith('build_fes_coleco_socket_v2'):
            inputs = function.__globals__['video_profile'](
                video_socket=bound.arguments.get('video_socket', False),
                native_video=bound.arguments.get('native_video', False))[2]
        with python_source_guard(bound.arguments['root'],
                                 source_roots_for_producer(function.__module__, inputs, bound.arguments['root'])):
            return function(*args, **kwargs)
    return guarded


def read_execution_data(path):
    """Read bytes that the caller immediately binds into execution evidence."""
    token = _ACTIVE.set(None)
    try:
        return Path(path).read_bytes()
    finally:
        _ACTIVE.reset(token)
