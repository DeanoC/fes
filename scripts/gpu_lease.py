"""Shared GPU leases for Powerboat and the CUDA box.

One flock file per GPU. gpu-run keeps those descriptors in this process and
runs the command as a child that does not inherit them, so the lease ends
when that command exits even if it backgrounded other processes. The kernel
also drops the flock if gpu-run itself exits or crashes. This module never
terminates another process. Liveness of a waiter is read from /proc.
"""
import argparse
import errno
import fcntl
import getpass
import json
import os
from pathlib import Path
import re
import selectors
import shlex
import signal
import socket
import stat
import subprocess
import sys
import time

DEFAULT_LEASE_DIR = '/var/tmp/gpu-leases'
ID_RE = re.compile(r'[A-Za-z0-9._+-]+\Z')
NVIDIA_RE = re.compile(r'^GPU\s+(\d+)\s*:', re.M)
ROCM_RE = re.compile(r'^GPU\s*\[(\d+)\]|^GPU\s+(\d+)\s*:', re.M)
POLL_INITIAL = 0.05
POLL_MAX = 0.5
GPU_MONITOR_INTERVAL = 0.05
PROC_ROOT = Path('/proc')
METADATA_LIMIT = 65536

RUN_HELP = """\
Hold a shared GPU lease and exec a command.

Powerboat (AMD, HIP; GPU0 and GPU1) and chip-30x0-ai-dev (NVIDIA RTX 3070
and RTX 3090) share GPUs through this tool. One flock file per GPU lives in
GPU_LEASE_DIR (default /var/tmp/gpu-leases). The directory is created mode
0700 and the files mode 0600, which is enough for the one Unix user these
agents share. The directory must be on a local filesystem. gpu-run holds the
flock itself and does not pass that descriptor to CMD, so the lease ends
when CMD exits even if CMD backgrounded other processes, and also if gpu-run
exits or crashes. This tool never terminates another process.

Device ids come from nvidia-smi -L or rocm-smi. If rocm-smi returns nothing,
AMD ids are the HIP order of GPU nodes in KFD topology (simd_count > 0).
DRM render minors are not HIP indices. GPU_LEASE_IDS overrides detection.
GPU_LEASE_VENDOR=amd|nvidia selects the variables exported to CMD:
  amd:     ROCR_VISIBLE_DEVICES, FES_GPU_IDS
  nvidia:  CUDA_VISIBLE_DEVICES, FES_GPU_IDS
FES_GPU_IDS retains the physical leased ids. GPU_RUN_DEVICE is the logical
index to pass to nextpnr (0 after masking), and GPU_RUN_EXPECTED_DEVICE names
the selected device node when it can be mapped. NEXTPNR_REQUIRE_GPU=1 requests
strict GPU routing from newer nextpnr builds. For nextpnr GPU routes, this
wrapper checks the process tree for the leased device nodes and accepts
nextpnr's backend-ready log when a short route closes those nodes between
polls. It rejects explicit CPU-reference fallback output. CPU-only and help
invocations do not require GPU access. If AMD KFD topology cannot map a render
node, the wrapper checks for /dev/kfd and an open DRM render node.

--wait (the default) queues first come, first served until enough GPUs are
free. --no-wait exits 75 when they cannot be taken without cutting ahead of
a waiter who can still acquire. --timeout S stops waiting after S seconds
and exits 75.

An owner holds at most GPU_LEASE_MAX_PER_OWNER GPUs at once (unset means
unlimited). That cap is recorded on the queue ticket and used only for that
waiter, so a capped waiter already at its allowance does not block anyone
else from taking a free GPU. Lander uses --owner lander and
GPU_LEASE_MAX_PER_OWNER=2.
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


KFD_NODES = '/sys/class/kfd/kfd/topology/nodes'


def parse_kfd_properties(text):
    props = {}
    for line in (text or '').splitlines():
        parts = line.split()
        if len(parts) >= 2:
            props[parts[0]] = parts[1]
    return props


def ids_from_kfd_nodes(nodes):
    """Return dense HIP ids in increasing KFD node order.

    A node is a GPU when simd_count is greater than zero. DRM render minors
    are not HIP indices: the only GPU node on a host can publish
    drm_render_minor 129 and still be HIP device 0. If any node omits
    simd_count, the mapping is incomplete and this returns no ids rather
    than renumbering the rest.
    """
    ids = []
    for _, text in sorted(nodes, key=lambda item: item[0]):
        props = parse_kfd_properties(text)
        raw = props.get('simd_count')
        if raw is None:
            return []
        try:
            simd_count = int(raw)
        except ValueError:
            return []
        if simd_count > 0:
            ids.append(str(len(ids)))
    return ids


def ids_from_kfd_sysfs(root=None):
    root = Path(root) if root is not None else Path(KFD_NODES)
    if not root.is_dir():
        return []
    try:
        entries = [entry for entry in root.iterdir() if entry.name.isdigit()]
    except OSError:
        return []
    nodes = []
    for entry in sorted(entries, key=lambda item: int(item.name)):
        try:
            text = (entry / 'properties').read_text()
        except OSError:
            return []
        nodes.append((int(entry.name), text))
    return ids_from_kfd_nodes(nodes)


def render_nodes_from_kfd_nodes(nodes):
    """Map dense HIP ids to DRM render minors from KFD topology."""
    render_nodes = {}
    gpu_id = 0
    for _, text in sorted(nodes, key=lambda item: item[0]):
        props = parse_kfd_properties(text)
        raw = props.get('simd_count')
        if raw is None:
            return {}
        try:
            simd_count = int(raw)
        except ValueError:
            return {}
        if simd_count <= 0:
            continue
        minor = props.get('drm_render_minor')
        if minor is not None and minor.isdigit():
            render_nodes[str(gpu_id)] = str(int(minor))
        gpu_id += 1
    return render_nodes


def render_nodes_from_kfd_sysfs(root=None):
    root = Path(root) if root is not None else Path(KFD_NODES)
    if not root.is_dir():
        return {}
    try:
        entries = [entry for entry in root.iterdir() if entry.name.isdigit()]
    except OSError:
        return {}
    nodes = []
    for entry in sorted(entries, key=lambda item: int(item.name)):
        try:
            text = (entry / 'properties').read_text()
        except OSError:
            return {}
        nodes.append((int(entry.name), text))
    return render_nodes_from_kfd_nodes(nodes)


def gpu_device_path(vendor, gpu_id):
    """Return the device node for a physical lease id, if it can be mapped."""
    if vendor == 'nvidia' and gpu_id.isdigit():
        return '/dev/nvidia' + gpu_id
    if vendor == 'amd':
        minor = render_nodes_from_kfd_sysfs().get(gpu_id)
        if minor is not None:
            return '/dev/dri/renderD' + minor
    return None


def process_tree_pids(root_pid, proc_root=None):
    """Yield root_pid and its live descendants using procfs task children."""
    proc_root = Path(proc_root) if proc_root is not None else PROC_ROOT
    pending = [int(root_pid)]
    visited = set()
    while pending:
        pid = pending.pop()
        if pid in visited:
            continue
        visited.add(pid)
        yield pid
        tasks = proc_root / str(pid) / 'task'
        try:
            task_entries = list(tasks.iterdir())
        except OSError:
            continue
        for task in task_entries:
            try:
                children = (task / 'children').read_text().split()
            except OSError:
                continue
            pending.extend(int(child) for child in children if child.isdigit())


def process_cmdline(pid, proc_root=None):
    proc_root = Path(proc_root) if proc_root is not None else PROC_ROOT
    try:
        return (proc_root / str(pid) / 'cmdline').read_bytes().replace(b'\0', b' ').decode(
            'utf-8', errors='replace')
    except OSError:
        return ''


def process_cmdline_args(pid, proc_root=None):
    proc_root = Path(proc_root) if proc_root is not None else PROC_ROOT
    try:
        raw = (proc_root / str(pid) / 'cmdline').read_bytes()
    except OSError:
        return []
    return [part.decode('utf-8', errors='replace') for part in raw.split(b'\0') if part]


def process_tree_has_nextpnr(root_pid, proc_root=None):
    return any('nextpnr-mistral' in process_cmdline(pid, proc_root)
               for pid in process_tree_pids(root_pid, proc_root))


def nextpnr_requires_gpu(args):
    if not any('nextpnr-mistral' in argument for argument in args):
        return False
    for index, argument in enumerate(args):
        if argument in ('--gpu-device', '--gpu-devices', '--require-gpu'):
            return True
        if argument.startswith('--gpu-device=') or argument.startswith('--gpu-devices='):
            return True
        if argument == '--router' and index + 1 < len(args) and args[index + 1].lower() == 'gpu':
            return True
        if argument.startswith('--router=') and argument.split('=', 1)[1].lower() == 'gpu':
            return True
    return False


def process_tree_requires_gpu(root_pid, proc_root=None):
    return any(nextpnr_requires_gpu(process_cmdline_args(pid, proc_root))
               for pid in process_tree_pids(root_pid, proc_root))


def process_tree_fd_targets(root_pid, proc_root=None):
    proc_root = Path(proc_root) if proc_root is not None else PROC_ROOT
    targets = set()
    for pid in process_tree_pids(root_pid, proc_root):
        fd_dir = proc_root / str(pid) / 'fd'
        try:
            entries = list(fd_dir.iterdir())
        except OSError:
            continue
        for entry in entries:
            try:
                target = os.readlink(entry)
            except OSError:
                continue
            if target.endswith(' (deleted)'):
                target = target[:-10]
            targets.add(target)
    return targets


def process_tree_uses_gpu(root_pid, vendor, device_node, proc_root=None):
    """Check that nextpnr's process tree opened the leased hardware node."""
    targets = process_tree_fd_targets(root_pid, proc_root)
    if vendor == 'amd':
        has_render_node = any(re.fullmatch(r'/dev/dri/renderD\d+', target) for target in targets)
        return '/dev/kfd' in targets and has_render_node and (
            device_node is None or device_node in targets)
    if vendor == 'nvidia':
        return device_node is not None and device_node in targets
    return False


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
    return ids_from_kfd_sysfs()


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


PRIVATE_DIR_MODE = 0o700
PRIVATE_FILE_MODE = 0o600
REMOTE_FILESYSTEMS = frozenset({
    '9p', 'afs', 'ceph', 'cifs', 'davfs', 'fuse.davfs', 'fuse.rclone', 'fuse.sshfs',
    'gfs2', 'glusterfs', 'gpfs', 'lustre', 'ncpfs', 'nfs', 'nfs3', 'nfs4', 'ocfs2',
    'panfs', 'smb', 'smb3', 'smbfs', 'sshfs',
})
SECRET_ARGUMENT = re.compile(
    r'(?i)(^|[^a-z0-9])(pass(word|wd)?|passwd|secret|token|api[-_]?key|'
    r'credential|authorization|auth[-_]?key)([^a-z0-9]|$)'
)
URL_USERINFO = re.compile(r'(?i)^([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@')
COMMAND_ARGUMENT_LIMIT = 256
COMMAND_ARGUMENT_COUNT = 32


def mode_of(path):
    return stat.S_IMODE(os.stat(path).st_mode)


def restrict_path(path, mode):
    try:
        os.chmod(path, mode)
        current = mode_of(path)
    except OSError as error:
        raise LeaseError('cannot restrict %s: %s' % (path, error.strerror)) from error
    if current & 0o077:
        raise LeaseError('%s is accessible to other users' % path)


def open_private(path):
    """Open a lock file that CMD and its children must not inherit."""
    old = os.umask(0o077)
    try:
        fd = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, PRIVATE_FILE_MODE)
    finally:
        os.umask(old)
    try:
        os.fchmod(fd, PRIVATE_FILE_MODE)
        current = stat.S_IMODE(os.fstat(fd).st_mode)
    except OSError as error:
        os.close(fd)
        raise LeaseError('cannot restrict %s: %s' % (path, error.strerror)) from error
    if current & 0o077:
        os.close(fd)
        raise LeaseError('%s is accessible to other users' % path)
    return fd


def unescape_mount(value):
    return re.sub(r'\\([0-7]{3})', lambda match: chr(int(match.group(1), 8)), value)


def read_mounts():
    try:
        return Path('/proc/self/mounts').read_text()
    except OSError:
        return None


def mount_fstype(path, mounts_text):
    """Longest mountpoint match, or None when the table does not cover path."""
    try:
        resolved = os.path.realpath(path)
    except OSError:
        return None
    resolved_key = resolved.rstrip('/') or '/'
    best = None
    best_len = -1
    for line in (mounts_text or '').splitlines():
        parts = line.split()
        if len(parts) < 3:
            continue
        mount = unescape_mount(parts[1])
        if not mount.startswith('/'):
            continue
        mount_key = mount.rstrip('/') or '/'
        # The root mount is a prefix of every path, but "/" + "/" is "//".
        if mount_key == '/':
            matched = resolved_key.startswith('/')
        else:
            matched = resolved_key == mount_key or resolved_key.startswith(mount_key + '/')
        if matched and len(mount_key) > best_len:
            best = parts[2]
            best_len = len(mount_key)
    return best


def existing_ancestor(path):
    path = Path(path)
    while not path.exists():
        parent = path.parent
        if parent == path:
            break
        path = parent
    return path


def assert_local_filesystem(path, mounts_text=None):
    """Best-effort rejection of a lease directory on a network mount.

    An unreadable mount table does not block a lease: the check is only as
    strong as /proc/self/mounts. Unknown filesystem types are allowed.
    """
    if mounts_text is None:
        mounts_text = read_mounts()
    if mounts_text is None:
        return
    fstype = mount_fstype(existing_ancestor(path), mounts_text)
    if fstype is None:
        return
    names = {fstype, fstype.split('.')[-1]}
    if names & REMOTE_FILESYSTEMS:
        raise LeaseError('%s is on %s, which is not a local filesystem' % (path, fstype))


def redact_command(command):
    """Drop secret-shaped arguments and bound what lease files retain."""
    result = []
    redact_next = False
    for index, arg in enumerate(command):
        if len(result) >= COMMAND_ARGUMENT_COUNT:
            result.append('...')
            break
        text = arg if isinstance(arg, str) else str(arg)
        if redact_next:
            result.append('[redacted]')
            redact_next = False
            continue
        text = URL_USERINFO.sub(r'\1[redacted]@', text)
        if index > 0:
            name, separator, _value = text.partition('=')
            if separator and SECRET_ARGUMENT.search(name):
                result.append(name + '=[redacted]')
                continue
            if SECRET_ARGUMENT.search(text):
                if len(text) > COMMAND_ARGUMENT_LIMIT:
                    text = text[:COMMAND_ARGUMENT_LIMIT] + '...'
                result.append(text)
                redact_next = True
                continue
        if len(text) > COMMAND_ARGUMENT_LIMIT:
            text = text[:COMMAND_ARGUMENT_LIMIT] + '...'
        result.append(text)
    return result


def ensure_lease_dir(path):
    path = Path(path)
    assert_local_filesystem(path)
    if path.exists() and not path.is_dir():
        raise LeaseError(str(path) + ' is not a directory')

    def create(directory):
        old = os.umask(0o077)
        try:
            try:
                directory.mkdir(mode=PRIVATE_DIR_MODE)
                return True
            except FileExistsError:
                return False
        finally:
            os.umask(old)

    if not path.exists():
        create(path)
    if not path.is_dir():
        raise LeaseError(str(path) + ' is not a directory')
    restrict_path(path, PRIVATE_DIR_MODE)
    queue = path / 'queue'
    if not queue.exists():
        create(queue)
    if not queue.is_dir():
        raise LeaseError(str(queue) + ' is not a directory')
    restrict_path(queue, PRIVATE_DIR_MODE)
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
    old = os.umask(0o077)
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC,
                     PRIVATE_FILE_MODE)
    finally:
        os.umask(old)
    try:
        view = data
        while view:
            written = os.write(fd, view)
            if written <= 0:
                raise LeaseError('failed to write ' + path.name)
            view = view[written:]
        os.fchmod(fd, PRIVATE_FILE_MODE)
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
    restrict_path(path, PRIVATE_FILE_MODE)


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
    # GPU_LEASE_IDS can be narrowed to one physical device by a per-route
    # caller. Owner limits still apply across every GPU leased through this
    # shared directory, so include lock files created by other inventories.
    inventory = set(ids)
    for path in Path(directory).glob('gpu-*.lock'):
        gpu_id = path.name[len('gpu-'):-len('.lock')]
        if ID_RE.fullmatch(gpu_id):
            inventory.add(gpu_id)
    count = 0
    for gpu_id in inventory:
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
            fd = open_private(lease_path(directory, gpu_id))
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
    fd = open_private(Path(directory) / 'state.lock')
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
    old = os.umask(0o077)
    try:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW | os.O_CLOEXEC,
                     PRIVATE_FILE_MODE)
    finally:
        os.umask(old)
    try:
        os.write(fd, (str(seq) + '\n').encode())
        os.fchmod(fd, PRIVATE_FILE_MODE)
    finally:
        os.close(fd)
    restrict_path(path, PRIVATE_FILE_MODE)
    return seq


def ticket_cap(ticket):
    """Cap recorded for this waiter, or None when that waiter was uncapped."""
    cap = ticket.get('cap') if isinstance(ticket, dict) else None
    if type(cap) is int and cap >= 1:
        return cap
    return None


def enqueue(directory, owner, label, gpus, command, cap):
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
            'cap': cap,
            'enqueued': iso(now),
            'enqueued_unix': now,
            'starttime': process_starttime(pid),
            'command': redact_command(command),
        }
        path = Path(directory) / 'queue' / ('%08d.json' % seq)
        write_atomic(path, ticket)
        return path, ticket
    return with_state_lock(directory, operation)


def ticket_blocked_by_own_cap(directory, ids, ticket):
    cap = ticket_cap(ticket)
    if cap is None:
        return False
    return held_by_owner(directory, ids, ticket['owner'], ()) + ticket['gpus'] > cap


def first_runnable(tickets, directory, ids):
    """First live ticket that can still acquire under its own recorded cap.

    A waiter blocked only by its own cap is not ahead of anyone. A waiter
    that can still acquire keeps its place even when the free GPUs are not
    enough for it yet.
    """
    for ticket in tickets:
        if ticket_blocked_by_own_cap(directory, ids, ticket):
            continue
        return ticket
    return None


def attempt(directory, ids, count, owner, label, command, cap, ticket_path, ticket):
    def operation():
        tickets = iter_tickets(directory)
        runnable = first_runnable(tickets, directory, ids)
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
                    'command': redact_command(command),
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
            ticket_path, ticket = enqueue(directory, owner, label, count, command, cap)
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


def child_env(vendor, ids, device_node=None):
    env = os.environ.copy()
    joined = ','.join(ids)
    for key in ('HIP_VISIBLE_DEVICES', 'ROCR_VISIBLE_DEVICES', 'CUDA_VISIBLE_DEVICES'):
        env.pop(key, None)
    env['FES_GPU_IDS'] = joined
    env['GPU_RUN_DEVICE'] = '0'
    env['GPU_RUN_EXPECTED_DEVICE'] = device_node or ('%s:%s' % (vendor, ids[0]))
    env['NEXTPNR_REQUIRE_GPU'] = '1'
    if vendor == 'amd':
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


def exec_command(chosen, command, env):
    """Replace the forked child. Never returns."""
    signal.signal(signal.SIGINT, signal.SIG_DFL)
    for _, fd in chosen:
        try:
            os.close(fd)
        except OSError:
            pass
    try:
        os.execvpe(command[0], command, env)
    except OSError as error:
        message = 'gpu-run: exec %s: %s\n' % (command[0], error.strerror or 'failed')
        try:
            os.write(2, message.encode())
        except OSError:
            pass
        os._exit(127)
    os._exit(127)


GPU_BACKEND_READY_RE = re.compile(rb'\bbackend\s+(hip|cuda):[^\n]*\bready\b', re.I)
GPU_CPU_FALLBACK_MARKERS = (
    b'falling back to the cpu reference backend',
    b'backend cpu-reference',
)


class GpuRouteOutput:
    """Track nextpnr backend evidence while forwarding output unchanged."""

    def __init__(self, vendor):
        self.expected_backend = 'hip' if vendor == 'amd' else 'cuda'
        self.tail = b''
        self.backend = None
        self.cpu_fallback = False

    def consume(self, chunk):
        combined = self.tail + chunk.lower()
        if any(marker in combined for marker in GPU_CPU_FALLBACK_MARKERS):
            self.cpu_fallback = True
        if self.backend is None:
            match = GPU_BACKEND_READY_RE.search(combined)
            if match is not None:
                self.backend = match.group(1).decode('ascii').lower()
        self.tail = combined[-8192:]


def drain_route_output(selector, evidence):
    for key, _mask in selector.select(0):
        fd = key.fd
        target_fd = key.data
        while True:
            try:
                chunk = os.read(fd, 65536)
            except BlockingIOError:
                break
            except OSError:
                chunk = b''
            if not chunk:
                selector.unregister(fd)
                os.close(fd)
                break
            evidence.consume(chunk)
            view = memoryview(chunk)
            while view:
                try:
                    written = os.write(target_fd, view)
                except OSError:
                    break
                if written <= 0:
                    break
                view = view[written:]


def execute(vendor, chosen, command):
    """Run command without the lease fds and release them when it exits.

    The child closes the lease descriptors before exec, and they are also
    close-on-exec, so a shell that backgrounds work cannot keep the flock.
    This waits only for that direct child. SIGINT stays with the child so
    this process keeps the lease until the child exits. It does not signal
    the child or any process the child started.
    """
    gpu_ids = [gpu_id for gpu_id, _ in chosen]
    device_node = gpu_device_path(vendor, gpu_ids[0])
    env = child_env(vendor, gpu_ids, device_node)
    requires_gpu = nextpnr_requires_gpu(command)
    output_pipes = None
    if requires_gpu:
        stdout_read, stdout_write = os.pipe()
        stderr_read, stderr_write = os.pipe()
        os.set_blocking(stdout_read, False)
        os.set_blocking(stderr_read, False)
        output_pipes = (stdout_read, stdout_write, stderr_read, stderr_write)
    previous_interrupt = signal.signal(signal.SIGINT, signal.SIG_IGN)
    try:
        pid = os.fork()
    except OSError:
        signal.signal(signal.SIGINT, previous_interrupt)
        if output_pipes is not None:
            for fd in output_pipes:
                os.close(fd)
        release(chosen)
        raise
    if pid == 0:
        if output_pipes is not None:
            stdout_read, stdout_write, stderr_read, stderr_write = output_pipes
            os.close(stdout_read)
            os.close(stderr_read)
            os.dup2(stdout_write, 1)
            os.dup2(stderr_write, 2)
            os.close(stdout_write)
            os.close(stderr_write)
        exec_command(chosen, command, env)
    selector = selectors.DefaultSelector()
    evidence = GpuRouteOutput(vendor)
    if output_pipes is not None:
        stdout_read, stdout_write, stderr_read, stderr_write = output_pipes
        os.close(stdout_write)
        os.close(stderr_write)
        selector.register(stdout_read, selectors.EVENT_READ, 1)
        selector.register(stderr_read, selectors.EVENT_READ, 2)
    opened_gpu = False
    try:
        while True:
            if not requires_gpu:
                requires_gpu = process_tree_requires_gpu(pid)
            if requires_gpu and not opened_gpu:
                opened_gpu = process_tree_uses_gpu(pid, vendor, device_node)
            if output_pipes is not None:
                selector.select(GPU_MONITOR_INTERVAL)
                drain_route_output(selector, evidence)
            try:
                waited, status = os.waitpid(pid, os.WNOHANG)
            except InterruptedError:
                continue
            if waited == pid:
                break
            if output_pipes is None:
                time.sleep(GPU_MONITOR_INTERVAL)
        if output_pipes is not None:
            while selector.get_map():
                if not selector.select(0):
                    break
                drain_route_output(selector, evidence)
    finally:
        for key in list(selector.get_map().values()):
            try:
                selector.unregister(key.fd)
            except (KeyError, ValueError):
                pass
            try:
                os.close(key.fd)
            except OSError:
                pass
        selector.close()
        signal.signal(signal.SIGINT, previous_interrupt)
        release(chosen)
    # FES-pinned DeanoC/nextpnr 9128800f passes cfg.device to
    # create_device_backend() and logs "backend ... ready" only after
    # backend->init() succeeds (common/route/gpurouter.cc:2709-2759). HIP and
    # CUDA share DeviceBackend::init (common/route/gpu/gpuroute_kernel.cuh:
    # 725-769); it selects device_ before querying/allocating state and
    # synchronizes before success. CUDA's runtime aliases are in
    # common/route/gpu/gpuroute_compat.h:26-54.
    backend_evidence = (
        output_pipes is not None
        and evidence.backend == evidence.expected_backend
        and not evidence.cpu_fallback
    )
    if requires_gpu and (evidence.cpu_fallback or not (opened_gpu or backend_evidence)):
        if evidence.cpu_fallback:
            print('gpu-run: nextpnr reported the CPU reference backend; refusing silent CPU fallback',
                  file=sys.stderr)
        else:
            expected = device_node or ('a device node for %s GPU %s (mapping unavailable)' % (vendor, gpu_ids[0]))
            if vendor == 'amd':
                opened = '/dev/kfd and %s' % expected
            else:
                opened = expected
            print('gpu-run: nextpnr never opened leased GPU device %s and did not prove a live backend; refusing silent CPU fallback' % opened,
                  file=sys.stderr)
        if os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0:
            return 70
    if os.WIFEXITED(status):
        return os.WEXITSTATUS(status)
    if os.WIFSIGNALED(status):
        return 128 + os.WTERMSIG(status)
    return 1


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
