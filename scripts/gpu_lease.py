"""Shared GPU leases for Powerboat and the CUDA box.

One flock file per GPU. gpu-run execs the command with those descriptors
still open, so the kernel releases the lease when that process exits,
including after a crash. This module never terminates another process.
Liveness of a waiter is read from /proc.
"""
import argparse
import errno
import fcntl
import getpass
import json
import os
from pathlib import Path
import re
import shlex
import socket
import subprocess
import sys
import time

DEFAULT_LEASE_DIR = '/var/tmp/gpu-leases'
ID_RE = re.compile(r'[A-Za-z0-9._+-]+\Z')
NVIDIA_RE = re.compile(r'^GPU\s+(\d+)\s*:', re.M)
ROCM_RE = re.compile(r'^GPU\s*\[(\d+)\]|^GPU\s+(\d+)\s*:', re.M)
POLL_INITIAL = 0.05
POLL_MAX = 0.5
METADATA_LIMIT = 65536

RUN_HELP = """\
Hold a shared GPU lease and exec a command.

Powerboat (AMD, HIP; GPU0 and GPU1) and chip-30x0-ai-dev (NVIDIA RTX 3070
and RTX 3090) share GPUs through this tool. One flock file per GPU lives in
GPU_LEASE_DIR (default /var/tmp/gpu-leases). The directory is created mode
1777 when it is missing, and it must be a local filesystem. The flock is
inherited by CMD and the kernel releases it when CMD exits, including on
crash. This tool never terminates another process.

Device ids come from nvidia-smi -L, rocm-smi, or /dev/dri/renderD*.
GPU_LEASE_IDS overrides detection. GPU_LEASE_VENDOR=amd|nvidia selects the
variables exported to CMD:
  amd:     HIP_VISIBLE_DEVICES, ROCR_VISIBLE_DEVICES, FES_GPU_IDS
  nvidia:  CUDA_VISIBLE_DEVICES, FES_GPU_IDS

--wait (the default) queues first come, first served until enough GPUs are
free. --no-wait exits 75 when they cannot be taken without cutting ahead of
a waiter. --timeout S stops waiting after S seconds and exits 75.

An owner holds at most GPU_LEASE_MAX_PER_OWNER GPUs at once (unset means
unlimited). Lander uses --owner lander and GPU_LEASE_MAX_PER_OWNER=2.
"""


class LeaseError(Exception):
    """Permanent configuration or usage failure."""


def die_text(value, name, limit, allow_empty=False):
    if not isinstance(value, str) or any(ord(ch) < 32 or ord(ch) == 127 for ch in value):
        raise LeaseError(name + ' contains control characters')
    if len(value) > limit or (not allow_empty and value == ''):
        raise LeaseError(name + ' is empty or too long')
    return value


def default_owner():
    for key in ('USER', 'LOGNAME'):
        value = os.environ.get(key, '').strip()
        if value:
            return value
    try:
        return getpass.getuser()
    except (OSError, KeyError):
        return 'unknown'


def lease_directory():
    raw = os.environ.get('GPU_LEASE_DIR', DEFAULT_LEASE_DIR)
    if not raw or not raw.strip():
        raise LeaseError('GPU_LEASE_DIR is empty')
    return Path(raw)


def id_key(value):
    return (0, int(value)) if value.isdigit() else (1, value)


def parse_id_list(raw):
    ids = []
    for part in raw.split(','):
        gpu_id = part.strip()
        if not gpu_id:
            continue
        if gpu_id in ('.', '..') or not ID_RE.fullmatch(gpu_id):
            raise LeaseError('invalid GPU id: ' + gpu_id)
        ids.append(gpu_id)
    if len(ids) != len(set(ids)):
        raise LeaseError('duplicate GPU id')
    return ids


def unique(items):
    seen = set()
    result = []
    for item in items:
        if item not in seen:
            seen.add(item)
            result.append(item)
    return result


def parse_nvidia_smi_l(text):
    return unique(NVIDIA_RE.findall(text or ''))


def parse_rocm_smi(text):
    ids = []
    for first, second in ROCM_RE.findall(text or ''):
        ids.append(first or second)
    ids = unique(ids)
    if ids:
        return ids
    lines = (text or '').splitlines()
    for index, line in enumerate(lines):
        if line.startswith('GPU') and 'Temp' in line:
            for row in lines[index + 1:]:
                stripped = row.strip()
                if not stripped or stripped.startswith('='):
                    break
                match = re.match(r'(\d+)\b', stripped)
                if not match:
                    break
                ids.append(match.group(1))
            break
    return unique(ids)


def ids_from_render_names(names):
    minors = []
    for name in names:
        match = re.search(r'renderD(\d+)\Z', os.path.basename(str(name)))
        if match:
            minor = int(match.group(1))
            if minor >= 128:
                minors.append(minor - 128)
    return [str(minor) for minor in sorted(set(minors))]


def run_tool(argv):
    try:
        completed = subprocess.run(argv, check=False, capture_output=True, text=True, timeout=10)
    except (OSError, subprocess.SubprocessError):
        return ''
    if completed.returncode != 0:
        return ''
    return completed.stdout


def detect_nvidia_ids():
    return parse_nvidia_smi_l(run_tool(['nvidia-smi', '-L']))


def detect_amd_ids():
    for argv in (['rocm-smi', '--showid'], ['rocm-smi']):
        ids = parse_rocm_smi(run_tool(argv))
        if ids:
            return ids
    dri = Path('/dev/dri')
    if not dri.is_dir():
        return []
    try:
        names = [path.name for path in dri.iterdir()]
    except OSError:
        return []
    return ids_from_render_names(names)


def owner_cap():
    raw = os.environ.get('GPU_LEASE_MAX_PER_OWNER')
    if raw is None or raw.strip() == '':
        return None
    if not raw.strip().isdigit() or int(raw.strip()) < 1:
        raise LeaseError('GPU_LEASE_MAX_PER_OWNER must be a positive integer')
    return int(raw.strip())


def resolve_inventory():
    vendor = os.environ.get('GPU_LEASE_VENDOR', '').strip().lower() or None
    if vendor not in (None, 'amd', 'nvidia'):
        raise LeaseError('GPU_LEASE_VENDOR must be amd or nvidia')
    raw_ids = os.environ.get('GPU_LEASE_IDS')
    if raw_ids is not None and raw_ids.strip() != '':
        ids = parse_id_list(raw_ids)
        if not ids:
            raise LeaseError('GPU_LEASE_IDS did not contain a GPU id')
        if vendor is None:
            if detect_nvidia_ids():
                vendor = 'nvidia'
            elif detect_amd_ids():
                vendor = 'amd'
            else:
                raise LeaseError('set GPU_LEASE_VENDOR when GPU ids are overridden and no GPU vendor is detectable')
        return vendor, sorted(ids, key=id_key)
    if vendor == 'nvidia':
        ids = detect_nvidia_ids()
    elif vendor == 'amd':
        ids = detect_amd_ids()
    else:
        ids = detect_nvidia_ids()
        vendor = 'nvidia' if ids else None
        if not ids:
            ids = detect_amd_ids()
            vendor = 'amd' if ids else None
    if not ids:
        raise LeaseError('no GPUs detected; set GPU_LEASE_IDS and GPU_LEASE_VENDOR')
    return vendor, sorted(ids, key=id_key)


def iso(timestamp):
    return time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(timestamp))


def process_starttime(pid):
    """Boot-relative start ticks from /proc, or None when pid is gone or a zombie."""
    try:
        raw = Path('/proc/' + str(pid) + '/stat').read_text()
    except OSError:
        return None
    right = raw.rfind(')')
    if right < 0:
        return None
    fields = raw[right + 2:].split()
    if len(fields) < 20 or fields[0] == 'Z':
        return None
    return fields[19]


def clear_cloexec(fd):
    flags = fcntl.fcntl(fd, fcntl.F_GETFD)
    fcntl.fcntl(fd, fcntl.F_SETFD, flags & ~fcntl.FD_CLOEXEC)


def open_shared(path):
    old = os.umask(0)
    try:
        fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o666)
    finally:
        os.umask(old)
    try:
        os.chmod(path, 0o666)
    except OSError:
        pass
    clear_cloexec(fd)
    return fd


def ensure_lease_dir(path):
    path = Path(path)
    if path.exists() and not path.is_dir():
        raise LeaseError(str(path) + ' is not a directory')

    def create(directory):
        old = os.umask(0)
        try:
            try:
                directory.mkdir(mode=0o1777)
                return True
            except FileExistsError:
                return False
        finally:
            os.umask(old)

    if not path.exists() and create(path):
        os.chmod(path, 0o1777)
    if not path.is_dir():
        raise LeaseError(str(path) + ' is not a directory')
    queue = path / 'queue'
    if not queue.exists() and create(queue):
        os.chmod(queue, 0o1777)
    if not queue.is_dir():
        raise LeaseError(str(queue) + ' is not a directory')
    return path


def lease_path(directory, gpu_id):
    if gpu_id in ('.', '..') or not ID_RE.fullmatch(gpu_id):
        raise LeaseError('invalid GPU id: ' + gpu_id)
    return Path(directory) / ('gpu-' + gpu_id + '.lock')


def read_fd_json(fd):
    try:
        os.lseek(fd, 0, os.SEEK_SET)
        raw = os.read(fd, METADATA_LIMIT + 1)
    except OSError:
        return None
    if not raw or len(raw) > METADATA_LIMIT:
        return None
    try:
        value = json.loads(raw.decode())
    except (UnicodeError, json.JSONDecodeError):
        return None
    return value if isinstance(value, dict) else None


def write_fd_json(fd, payload):
    data = (json.dumps(payload, sort_keys=True) + '\n').encode()
    os.ftruncate(fd, 0)
    os.lseek(fd, 0, os.SEEK_SET)
    view = data
    while view:
        written = os.write(fd, view)
        if written <= 0:
            raise LeaseError('failed to write lease metadata')
        view = view[written:]


def read_json(path):
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    except OSError:
        return None
    try:
        return read_fd_json(fd)
    finally:
        os.close(fd)


def write_atomic(path, payload):
    path = Path(path)
    data = (json.dumps(payload, sort_keys=True) + '\n').encode()
    temporary = path.with_name('.' + path.name + '.' + str(os.getpid()) + '.tmp')
    old = os.umask(0)
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o666)
    finally:
        os.umask(old)
    try:
        view = data
        while view:
            written = os.write(fd, view)
            if written <= 0:
                raise LeaseError('failed to write ' + path.name)
            view = view[written:]
    except Exception:
        os.close(fd)
        os.unlink(temporary)
        raise
    os.close(fd)
    try:
        os.replace(temporary, path)
    except Exception:
        os.unlink(temporary)
        raise
    try:
        os.chmod(path, 0o666)
    except OSError:
        pass


def quiet_unlink(path):
    try:
        os.unlink(path)
    except FileNotFoundError:
        pass
    except OSError:
        pass


def release(chosen):
    for _, fd in chosen:
        try:
            fcntl.flock(fd, fcntl.LOCK_UN)
        except OSError:
            pass
        try:
            os.close(fd)
        except OSError:
            pass


def try_lock(fd):
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return True
    except OSError as error:
        if error.errno in (errno.EAGAIN, errno.EWOULDBLOCK):
            return False
        raise


def read_holder(directory, gpu_id):
    """Return metadata when the GPU flock is held, else None.

    A brief probe lock is released before returning. Callers that already
    hold the GPU must not probe it: a second open in this process does not
    share the flock.
    """
    path = lease_path(directory, gpu_id)
    try:
        fd = os.open(path, os.O_RDWR | os.O_NOFOLLOW)
    except FileNotFoundError:
        return None
    except OSError:
        return None
    try:
        if try_lock(fd):
            fcntl.flock(fd, fcntl.LOCK_UN)
            return None
        return read_fd_json(fd) or {'_held': True}
    finally:
        os.close(fd)


def held_by_owner(directory, ids, owner, skip):
    count = 0
    for gpu_id in ids:
        if gpu_id in skip:
            continue
        meta = read_holder(directory, gpu_id)
        if meta and meta.get('owner') == owner:
            count += 1
    return count


def lock_gpus(directory, ids, count):
    chosen = []
    try:
        for gpu_id in ids:
            fd = open_shared(lease_path(directory, gpu_id))
            try:
                locked = try_lock(fd)
            except Exception:
                os.close(fd)
                raise
            if not locked:
                os.close(fd)
                continue
            chosen.append((gpu_id, fd))
            if len(chosen) == count:
                return chosen
    except Exception:
        release(chosen)
        raise
    release(chosen)
    return None


def with_state_lock(directory, function):
    fd = open_shared(Path(directory) / 'state.lock')
    try:
        fcntl.flock(fd, fcntl.LOCK_EX)
        return function()
    finally:
        os.close(fd)


def valid_ticket(ticket):
    if not isinstance(ticket, dict):
        return False
    number = ticket.get('ticket')
    gpus = ticket.get('gpus')
    pid = ticket.get('pid')
    if type(number) is not int or number < 1:
        return False
    if type(gpus) is not int or gpus < 1:
        return False
    if type(pid) is not int or pid <= 0:
        return False
    if not isinstance(ticket.get('owner'), str) or not isinstance(ticket.get('host'), str):
        return False
    return True


def ticket_live(ticket):
    if not valid_ticket(ticket):
        return False
    if ticket['host'] != socket.gethostname():
        return True
    identity = process_starttime(ticket['pid'])
    if identity is None:
        return False
    expected = ticket.get('starttime')
    if expected is None:
        return True
    return str(expected) == identity


def iter_tickets(directory):
    queue = Path(directory) / 'queue'
    if not queue.is_dir():
        return []
    found = []
    try:
        paths = list(queue.glob('[0-9]*.json'))
    except OSError:
        return []
    for path in paths:
        data = read_json(path)
        if ticket_live(data):
            found.append(data)
    found.sort(key=lambda item: item['ticket'])
    return found


def next_seq(directory):
    path = Path(directory) / 'queue.seq'
    try:
        current = int(path.read_text().strip())
    except (OSError, ValueError):
        current = 0
    highest = current
    queue = Path(directory) / 'queue'
    if queue.is_dir():
        for entry in queue.glob('[0-9]*.json'):
            try:
                highest = max(highest, int(entry.stem))
            except ValueError:
                pass
    seq = highest + 1
    old = os.umask(0)
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, 0o666)
    finally:
        os.umask(old)
    try:
        os.write(fd, (str(seq) + '\n').encode())
    finally:
        os.close(fd)
    try:
        os.chmod(path, 0o666)
    except OSError:
        pass
    return seq


def enqueue(directory, owner, label, gpus, command):
    def operation():
        seq = next_seq(directory)
        now = time.time()
        pid = os.getpid()
        ticket = {
            'ticket': seq,
            'owner': owner,
            'label': label,
            'pid': pid,
            'host': socket.gethostname(),
            'gpus': gpus,
            'enqueued': iso(now),
            'enqueued_unix': now,
            'starttime': process_starttime(pid),
            'command': list(command),
        }
        path = Path(directory) / 'queue' / ('%08d.json' % seq)
        write_atomic(path, ticket)
        return path, ticket
    return with_state_lock(directory, operation)


def first_runnable(tickets, directory, ids, cap):
    for ticket in tickets:
        if cap is not None and held_by_owner(directory, ids, ticket['owner'], ()) + ticket['gpus'] > cap:
            continue
        return ticket
    return None


def attempt(directory, ids, count, owner, label, command, cap, ticket_path, ticket):
    def operation():
        tickets = iter_tickets(directory)
        runnable = first_runnable(tickets, directory, ids, cap)
        if ticket is None:
            if runnable is not None:
                return None
        elif runnable is None or runnable['ticket'] != ticket['ticket']:
            return None
        chosen = lock_gpus(directory, ids, count)
        if chosen is None:
            return None
        try:
            skip = {gpu_id for gpu_id, _ in chosen}
            if cap is not None and held_by_owner(directory, ids, owner, skip) + count > cap:
                release(chosen)
                return None
            now = time.time()
            for gpu_id, fd in chosen:
                write_fd_json(fd, {
                    'command': list(command),
                    'gpu': gpu_id,
                    'host': socket.gethostname(),
                    'label': label,
                    'owner': owner,
                    'pid': os.getpid(),
                    'start': iso(now),
                    'start_unix': now,
                })
            if ticket_path is not None:
                try:
                    os.unlink(ticket_path)
                except FileNotFoundError:
                    pass
            return chosen
        except Exception:
            release(chosen)
            raise
    return with_state_lock(directory, operation)


def acquire(directory, ids, count, owner, label, command, cap, wait, timeout):
    ticket_path = None
    ticket = None
    got = None
    deadline = None if timeout is None else time.monotonic() + timeout
    try:
        if wait:
            ticket_path, ticket = enqueue(directory, owner, label, count, command)
        backoff = POLL_INITIAL
        while True:
            got = attempt(directory, ids, count, owner, label, command, cap, ticket_path, ticket)
            if got is not None:
                return got
            if not wait:
                return None
            if deadline is not None and time.monotonic() >= deadline:
                return None
            delay = min(backoff, POLL_MAX)
            if deadline is not None:
                delay = min(delay, max(0.0, deadline - time.monotonic()))
            if delay > 0:
                time.sleep(delay)
            backoff = min(backoff * 2, POLL_MAX)
    finally:
        if ticket_path is not None and got is None:
            quiet_unlink(ticket_path)


def child_env(vendor, ids):
    env = os.environ.copy()
    joined = ','.join(ids)
    env['FES_GPU_IDS'] = joined
    if vendor == 'amd':
        env['HIP_VISIBLE_DEVICES'] = joined
        env['ROCR_VISIBLE_DEVICES'] = joined
    else:
        env['CUDA_VISIBLE_DEVICES'] = joined
    return env


def string_list(value):
    if isinstance(value, list) and all(isinstance(part, str) for part in value):
        return value
    return None


def public_held(gpu_id, meta):
    start = meta.get('start_unix')
    age = None
    if type(start) in (int, float):
        age = max(0, int(time.time() - start))
    return {
        'id': gpu_id,
        'state': 'held',
        'owner': meta.get('owner') if isinstance(meta.get('owner'), str) else None,
        'label': meta.get('label') if isinstance(meta.get('label'), str) else None,
        'pid': meta.get('pid') if type(meta.get('pid')) is int else None,
        'host': meta.get('host') if isinstance(meta.get('host'), str) else None,
        'start': meta.get('start') if isinstance(meta.get('start'), str) else None,
        'age_seconds': age,
        'command': string_list(meta.get('command')),
    }


def public_ticket(ticket):
    start = ticket.get('enqueued_unix')
    age = 0
    if type(start) in (int, float):
        age = max(0, int(time.time() - start))
    return {
        'ticket': ticket['ticket'],
        'owner': ticket['owner'],
        'label': ticket.get('label') if isinstance(ticket.get('label'), str) else '',
        'pid': ticket['pid'],
        'host': ticket['host'],
        'gpus': ticket['gpus'],
        'enqueued': ticket.get('enqueued') if isinstance(ticket.get('enqueued'), str) else None,
        'age_seconds': age,
        'command': string_list(ticket.get('command')) or [],
    }


def report(directory, vendor, ids):
    directory = Path(directory)
    gpus = []
    for gpu_id in ids:
        meta = read_holder(directory, gpu_id) if directory.is_dir() else None
        if meta is None:
            gpus.append({'id': gpu_id, 'state': 'free'})
        else:
            gpus.append(public_held(gpu_id, meta))
    queue = [public_ticket(ticket) for ticket in iter_tickets(directory)] if directory.is_dir() else []
    return {'lease_dir': str(directory), 'vendor': vendor, 'gpus': gpus, 'queue': queue}


def format_age(seconds):
    if seconds is None:
        return '-'
    seconds = max(0, int(seconds))
    if seconds < 60:
        return '%ds' % seconds
    minutes, seconds = divmod(seconds, 60)
    if minutes < 60:
        return '%dm%ds' % (minutes, seconds)
    hours, minutes = divmod(minutes, 60)
    return '%dh%dm' % (hours, minutes)


def format_status(payload):
    lines = []
    for gpu in payload['gpus']:
        if gpu['state'] == 'free':
            lines.append('GPU %s  free' % gpu['id'])
            continue
        command = shlex.join(gpu['command']) if gpu.get('command') else '-'
        lines.append('GPU %s  held  owner=%s label=%s pid=%s host=%s age=%s command=%s' % (
            gpu['id'], gpu.get('owner') or '-', gpu.get('label') or '-',
            gpu.get('pid') if gpu.get('pid') is not None else '-',
            gpu.get('host') or '-', format_age(gpu.get('age_seconds')), command))
    if not payload['queue']:
        lines.append('queue empty')
    for item in payload['queue']:
        command = shlex.join(item['command']) if item.get('command') else '-'
        lines.append('waiting %s  owner=%s gpus=%s label=%s pid=%s host=%s age=%s command=%s' % (
            item['ticket'], item['owner'], item['gpus'], item.get('label') or '-',
            item['pid'], item['host'], format_age(item['age_seconds']), command))
    return '\n'.join(lines) + '\n'


def parse_timeout(value):
    try:
        timeout = float(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError('timeout must be a number of seconds') from error
    if timeout < 0 or timeout != timeout or timeout == float('inf'):
        raise argparse.ArgumentTypeError('timeout must be a non-negative number of seconds')
    return timeout


def parse_run(argv):
    parser = argparse.ArgumentParser(prog='gpu-run', description=RUN_HELP,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--gpus', type=int, default=1, help='how many GPUs to hold (default 1)')
    parser.add_argument('--owner', default=None, help='lease owner (default $USER)')
    parser.add_argument('--label', default='', help='short description stored in the lease')
    parser.add_argument('--wait', action='store_true', help='queue until the GPUs are free (default)')
    parser.add_argument('--no-wait', action='store_true', help='exit 75 instead of queueing')
    parser.add_argument('--timeout', type=parse_timeout, help='seconds to wait before exiting 75')
    parser.add_argument('cmd', nargs=argparse.REMAINDER, help='command to exec, after --')
    args = parser.parse_args(argv)
    if args.wait and args.no_wait:
        parser.error('--wait and --no-wait are mutually exclusive')
    if args.no_wait and args.timeout is not None:
        parser.error('--timeout applies only when waiting')
    if args.gpus < 1:
        parser.error('--gpus must be at least 1')
    command = list(args.cmd)
    if command and command[0] == '--':
        command = command[1:]
    if not command:
        parser.error('command is required after --')
    args.cmd = command
    args.wait_for_lease = not args.no_wait
    return args


def execute(vendor, chosen, command):
    env = child_env(vendor, [gpu_id for gpu_id, _ in chosen])
    try:
        os.execvpe(command[0], command, env)
    except OSError as error:
        release(chosen)
        print('gpu-run: exec %s: %s' % (command[0], error.strerror), file=sys.stderr)
        return 127


def main_run(argv=None):
    try:
        args = parse_run(sys.argv[1:] if argv is None else argv)
        vendor, ids = resolve_inventory()
        if args.gpus > len(ids):
            raise LeaseError('requested %d GPUs but only %d are visible' % (args.gpus, len(ids)))
        cap = owner_cap()
        if cap is not None and args.gpus > cap:
            raise LeaseError('requested %d GPUs but GPU_LEASE_MAX_PER_OWNER is %d' % (args.gpus, cap))
        owner = die_text(args.owner if args.owner is not None else default_owner(), 'owner', 64)
        label = die_text(args.label, 'label', 200, allow_empty=True)
        directory = ensure_lease_dir(lease_directory())
        chosen = acquire(directory, ids, args.gpus, owner, label, args.cmd, cap,
                         args.wait_for_lease, args.timeout)
        if chosen is None:
            if args.wait_for_lease:
                print('gpu-run: timed out waiting for a GPU lease', file=sys.stderr)
            else:
                print('gpu-run: GPU lease unavailable', file=sys.stderr)
            return 75
        return execute(vendor, chosen, args.cmd)
    except LeaseError as error:
        print('gpu-run: %s' % error, file=sys.stderr)
        return 2
    except KeyboardInterrupt:
        print('gpu-run: interrupted', file=sys.stderr)
        return 130


def main_status(argv=None):
    try:
        parser = argparse.ArgumentParser(prog='gpu-status',
                                         description='Show GPU lease holders, ages, and the waiting queue.')
        parser.add_argument('--json', action='store_true', help='print a JSON object')
        args = parser.parse_args(sys.argv[1:] if argv is None else argv)
        vendor, ids = resolve_inventory()
        payload = report(lease_directory(), vendor, ids)
        if args.json:
            sys.stdout.write(json.dumps(payload, indent=2) + '\n')
        else:
            sys.stdout.write(format_status(payload))
        return 0
    except LeaseError as error:
        print('gpu-status: %s' % error, file=sys.stderr)
        return 2
    except KeyboardInterrupt:
        print('gpu-status: interrupted', file=sys.stderr)
        return 130
