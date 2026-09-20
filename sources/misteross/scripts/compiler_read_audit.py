"""Bounded compiler subprocess open audit for the Markdown closure policy."""
import re
import subprocess
import tempfile
import signal
import time
from pathlib import Path

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


def verify_traces(directory, source_root):
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
            if not line or re.fullmatch(r'--- SIG[A-Z0-9]+ \{.*\} ---', line):
                continue
            if 'io_uring_setup(' in line or 'open_by_handle_at(' in line:
                raise ReadAuditError('compiler used an unauditable file-open mechanism')
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
            if path.suffix.lower() not in ('.md', '.markdown'):
                continue
            if path.is_symlink() or not path.is_file():
                raise ReadAuditError('compiler opened missing or linked Markdown')
            if path.stat().st_mode & 0o111:
                continue  # Executable Markdown remains in the source closure.
            arguments = re.sub(r'"(?:\\.|[^"\\])*"', '""', match[2])
            arguments = re.sub(r'<[^>]*>', '', arguments)
            if re.search(r'\bO_WRONLY\b', arguments):
                continue
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
    timeout = kwargs.pop('timeout', None)
    check = kwargs.pop('check', False)
    deadline = time.monotonic() + timeout if timeout is not None else None
    with tempfile.TemporaryDirectory(prefix='fes-read-audit-') as temporary:
        directory = Path(temporary)
        args = [str(TRACER), '--kill-on-exit', '-ff', '-yy', '-s', '0',
                '-e', 'status=successful', '-e', 'trace=open,openat,openat2,open_by_handle_at,io_uring_setup',
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
                    verify_traces(directory, source_root)
                    raise subprocess.TimeoutExpired(command, timeout, output=stdout, stderr=stderr)
                try:
                    stdout, stderr = process.communicate(timeout=min(0.1, remaining))
                    break
                except subprocess.TimeoutExpired:
                    pass
            verify_traces(directory, source_root)
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
    if event != 'open' or active is None:
        return
    path, mode, flags = args
    if flags & os.O_ACCMODE == os.O_WRONLY:
        return
    root, roots = active
    if isinstance(path, int):
        try:
            path = os.readlink('/proc/self/fd/' + str(path))
        except OSError as exc:
            raise ReadAuditError('cannot resolve helper file descriptor') from exc
    try:
        resolved = Path(os.fsdecode(path)).resolve()
    except (TypeError, ValueError, OSError) as exc:
        raise ReadAuditError('cannot resolve helper open') from exc
    if not resolved.is_relative_to(root):
        return
    relative = resolved.relative_to(root).as_posix()
    if resolved.suffix.lower() in ('.md', '.markdown'):
        if not resolved.is_file() or not resolved.stat().st_mode & 0o111:
            raise ReadAuditError('Python helper read excluded Markdown: ' + ascii(relative)[:320])


sys.addaudithook(_python_open)


@contextlib.contextmanager
def python_source_guard(root, roots):
    token = _ACTIVE.set((Path(root).resolve(), tuple(roots)))
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
        from scripts.functional_execution import source_roots_for_inputs
        with python_source_guard(bound.arguments['root'],
                                 source_roots_for_inputs(function.__globals__['PINNED_INPUTS'])):
            return function(*args, **kwargs)
    return guarded


def read_execution_data(path):
    """Read bytes that the caller immediately binds into execution evidence."""
    token = _ACTIVE.set(None)
    try:
        return Path(path).read_bytes()
    finally:
        _ACTIVE.reset(token)
