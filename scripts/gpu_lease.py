"""Shared GPU leases for Powerboat, the CUDA box and Apple silicon Macs.

One flock file per GPU. gpu-run keeps those descriptors in this process and
runs the command as a child that does not inherit them, so the lease ends
when that command exits even if it backgrounded other processes. The kernel
also drops the flock if gpu-run itself exits or crashes. This module never
terminates another process. Liveness of a waiter is read from /proc on Linux
and from libproc on macOS, checking both PID and start time. Tickets record
their eligible physical GPU ids and
vendor mask variable. FIFO applies to overlapping eligible GPU sets; later
tickets may backfill on disjoint GPUs. On grant, the mask value is rebuilt
from the exact GPU ids locked for that command.

gpu-run --phase-lease takes no GPU up front. Its command asks for a GPU on
GPU_LEASE_FD for each GPU phase, and each request waits through acquire(), so
a job on CPU holds no GPU. Requests from yielding owners (lander) wait in a
separate queue that is served only after every runnable main-queue ticket.
"""
import argparse
import errno
import fcntl
import getpass
import hashlib
import json
import os
from pathlib import Path
import plistlib
import re
import select
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
# macOS has no procfs: process identity, ancestry and argv come from libproc
# and sysctl, and the lease directory's filesystem from statfs(2).
DARWIN = sys.platform == 'darwin'
VENDORS = ('amd', 'nvidia', 'apple')
# Normal ioreg refresh interval after the initial Metal client discovery window.
APPLE_CLIENT_POLL = 0.5
# Force scans briefly after launch/grant so short redirected routes can prove
# Metal use even when inventory detection cached a pre-launch snapshot.
APPLE_CLIENT_DISCOVERY_WINDOW = 0.5
METADATA_LIMIT = 65536
QUEUE = 'queue'
# Phase tickets of these owners wait here and are served only after every
# runnable ticket in QUEUE. gpu-run without --phase-lease never reads it.
YIELD_QUEUE = 'yield-queue'
# Deano-approved defaults (fes#874): Lander's GPU phases yield to the harness
# whenever a GPU frees, and an owner runs at most 4 phase-lease jobs at once.
YIELDING_OWNERS = frozenset({'lander'})
DEFAULT_JOB_SLOTS = 4
MAX_JOB_SLOTS = 256
PHASE_REQUEST_LIMIT = 256
PHASE_REPLY_TIMEOUT = 5.0
ACQUIRE_RE = re.compile(r'ACQUIRE ([0-9]+)\Z')

RUN_HELP = """\
Hold a shared GPU lease and exec a command.

Powerboat (AMD, HIP; GPU0 and GPU1) and chip-30x0-ai-dev (NVIDIA RTX 3070
and RTX 3090) share GPUs through this tool, as does an Apple silicon Mac
(one Metal GPU, id 0). One flock file per GPU lives in GPU_LEASE_DIR
(default /var/tmp/gpu-leases). The directory is created mode 0700 and the
files mode 0600, which is enough for the one Unix user these agents share.
The directory must be on a local filesystem; on macOS statfs(2) must mark
it local. gpu-run holds the flock itself and does not pass that descriptor
to CMD, so the lease ends when CMD exits even if CMD backgrounded other
processes, and also if gpu-run exits or crashes. This tool never terminates another process.

Device ids come from nvidia-smi -L or rocm-smi. If rocm-smi returns nothing,
AMD ids are the HIP order of GPU nodes in KFD topology (simd_count > 0).
DRM render minors are not HIP indices. On macOS the Metal GPU comes from
ioreg's IOAccelerator services; a Mac with exactly one (every Apple silicon
Mac) has id 0, the index nextpnr's Metal backend uses. A Mac with several
has no reliable Metal device order and needs GPU_LEASE_IDS.
GPU_LEASE_IDS overrides host inventory detection; it is not a per-job pin.
Queue tickets record this eligible id set and the vendor visibility
variable that the grant will set. A later waiter may backfill only on GPUs
disjoint from earlier eligible waiters, preserving FIFO for jobs that can
use the same GPU. On grant the visibility mask is
rebuilt from exactly the leased ids, overriding inherited CUDA/HIP masks.
GPU_LEASE_VENDOR=amd|nvidia|apple selects the variables exported to CMD:
  amd:     ROCR_VISIBLE_DEVICES, FES_GPU_IDS
  nvidia:  CUDA_VISIBLE_DEVICES, CUDA_DEVICE_ORDER=PCI_BUS_ID, FES_GPU_IDS
  apple:   FES_GPU_IDS (Metal has no visibility mask)
FES_GPU_IDS retains the physical leased ids. GPU_RUN_DEVICE is the index to
pass to nextpnr: 0 after masking on AMD and NVIDIA, the physical id on Apple,
where nothing is masked or renumbered. GPU_RUN_EXPECTED_DEVICE names the
selected device node, or metal:<registry id> on Apple, when it can be mapped.
NEXTPNR_REQUIRE_GPU=1 requests strict GPU routing from newer nextpnr builds.
For nextpnr GPU routes, this wrapper checks the process tree for the leased
device nodes and accepts nextpnr's backend-ready log when a short route closes
those nodes between polls. It rejects explicit CPU-reference fallback output.
CPU-only and help invocations do not require GPU access. If AMD KFD topology
cannot map a render node, the wrapper checks for /dev/kfd and an open DRM
render node. Metal has no device nodes: on Apple the wrapper instead looks
for a Metal user client that nextpnr opened on the leased GPU (ioreg's
IOUserClientCreator) or nextpnr's "backend metal:<registry id>:... ready"
line naming the leased GPU.

--wait (the default) queues first come, first served. A later waiter may
backfill on GPUs outside the eligible sets of earlier runnable tickets;
waiters competing for the same GPU keep strict FIFO. --no-wait exits 75 when
its requested GPUs are busy or it would cut ahead of an earlier waiter for
the same GPU. --timeout S stops waiting after S seconds and exits 75.

An owner holds at most GPU_LEASE_MAX_PER_OWNER GPUs at once (unset means
unlimited). That cap is recorded on the queue ticket and used only for that
waiter, so a capped waiter already at its allowance does not block anyone
else from taking a free GPU. Lander uses --owner lander and
GPU_LEASE_MAX_PER_OWNER=2.

--phase-lease holds no GPU up front. It first waits, first come first
served, for one of the owner's job slots: GPU_LEASE_MAX_JOBS_PER_OWNER
concurrent phase-lease jobs (default 4). A job holds no GPU while it waits
for a slot. CMD then runs with no GPU visibility mask and with GPU_LEASE_FD
naming its end of an AF_UNIX socketpair, the only descriptor gpu-run adds.
The protocol is one ASCII line per request, one request at a time:
  ACQUIRE 1  ->  GRANT <physical id> | ERR <msg>   (ERR already-held if held)
  RELEASE    ->  OK | ERR not-held
  other      ->  ERR unknown-command
ACQUIRE waits in the normal queue, so FIFO, backfill and the owner cap
apply to each GPU phase. GPU_LEASE_MAX_PER_OWNER then counts only GPUs held
by active phases. A grant is released when the socket closes (CMD exited or
crashed) and when gpu-run exits. On NVIDIA, CMD also gets
CUDA_DEVICE_ORDER=PCI_BUS_ID so CUDA device numbers match the granted
nvidia-smi ids. On Apple the granted id is the Metal device index.
Phase requests from owner lander wait in a separate queue: whenever a GPU
frees, every runnable ticket in the main queue (other owners and
whole-command leases) is served first. A phase in progress is never
interrupted. --phase-lease requires --gpus 1, decimal GPU ids, and waiting
without --timeout.
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
    if vendor == 'apple':
        return apple_device_names(apple_accelerators()).get(gpu_id)
    return None


IOUSERCLIENT_CREATOR_RE = re.compile(r'pid (\d+),')


def parse_ioreg_accelerators(text):
    """Return (registry id, user-client creator pids) per Metal accelerator.

    text is the plist from ioreg -l -r -c IOAccelerator -d 2 -a. Only
    services with a MetalPluginName are Metal devices. Their children are the
    user clients that processes opened on that GPU; IOUserClientCreator names
    the opening process as "pid N, name". An entry without an integer
    IORegistryEntryID makes the list incomplete, so this returns no devices
    rather than renumbering the rest.
    """
    if not text:
        return []
    try:
        entries = plistlib.loads(text.encode('utf-8'))
    except Exception:
        return []
    if not isinstance(entries, list):
        return []
    accelerators = []
    for entry in entries:
        if not isinstance(entry, dict) or not entry.get('MetalPluginName'):
            continue
        registry_id = entry.get('IORegistryEntryID')
        if type(registry_id) is not int or registry_id < 0:
            return []
        creators = set()
        children = entry.get('IORegistryEntryChildren')
        for child in children if isinstance(children, list) else ():
            creator = child.get('IOUserClientCreator') if isinstance(child, dict) else None
            match = IOUSERCLIENT_CREATOR_RE.match(creator) if isinstance(creator, str) else None
            if match is not None:
                creators.add(int(match.group(1)))
        accelerators.append((registry_id, creators))
    return accelerators


_apple_accelerator_cache = {'checked': None, 'accelerators': []}


def apple_accelerators(refresh=False):
    """Metal accelerators and their user-client pids from ioreg.

    Normal callers reuse the snapshot for APPLE_CLIENT_POLL. Active route
    monitoring can refresh it to avoid reusing a pre-launch inventory result.
    """
    now = time.monotonic()
    checked = _apple_accelerator_cache['checked']
    if refresh or checked is None or now - checked >= APPLE_CLIENT_POLL:
        _apple_accelerator_cache['accelerators'] = parse_ioreg_accelerators(
            run_tool(['ioreg', '-l', '-r', '-c', 'IOAccelerator', '-d', '2', '-a']))
        _apple_accelerator_cache['checked'] = now
    return _apple_accelerator_cache['accelerators']


def metal_device_name(registry_id):
    """The device token nextpnr's Metal backend logs: metal:<16 hex digits>."""
    return 'metal:%016x' % registry_id


def apple_device_names(accelerators):
    """Map lease ids to Metal device names when the mapping is certain.

    Metal numbers devices in MTLCopyAllDevices() order, which ioreg cannot
    reproduce for several GPUs. Every Apple silicon Mac has one, Metal device
    0, so only that case maps.
    """
    if len(accelerators) != 1:
        return {}
    return {'0': metal_device_name(accelerators[0][0])}


def apple_gpu_clients(refresh=False):
    """Map each Metal device name to the pids with a user client open on it."""
    return {metal_device_name(registry_id): creators
            for registry_id, creators in apple_accelerators(refresh=refresh)}


_darwin_libc = None


def darwin_libc():
    """libSystem through ctypes, loaded on first use on macOS only."""
    global _darwin_libc
    if _darwin_libc is None:
        import ctypes
        _darwin_libc = ctypes.CDLL(None, use_errno=True)
    return _darwin_libc


PROC_PIDTBSDINFO = 3
PROC_PPID_ONLY = 6
CTL_KERN = 1
KERN_ARGMAX = 8
KERN_PROCARGS2 = 49
MNT_LOCAL = 0x00001000
MAXCOMLEN = 16
MFSTYPENAMELEN = 16
MAXPATHLEN = 1024


def darwin_bsdinfo(pid):
    """struct proc_bsdinfo for a live, non-zombie pid, or None."""
    import ctypes
    u32 = ctypes.c_uint32

    class ProcBsdInfo(ctypes.Structure):
        _fields_ = [
            ('pbi_flags', u32), ('pbi_status', u32), ('pbi_xstatus', u32),
            ('pbi_pid', u32), ('pbi_ppid', u32), ('pbi_uid', u32), ('pbi_gid', u32),
            ('pbi_ruid', u32), ('pbi_rgid', u32), ('pbi_svuid', u32), ('pbi_svgid', u32),
            ('rfu_1', u32), ('pbi_comm', ctypes.c_char * MAXCOMLEN),
            ('pbi_name', ctypes.c_char * (2 * MAXCOMLEN)), ('pbi_nfiles', u32),
            ('pbi_pgid', u32), ('pbi_pjobc', u32), ('e_tdev', u32), ('e_tpgid', u32),
            ('pbi_nice', ctypes.c_int32), ('pbi_start_tvsec', ctypes.c_uint64),
            ('pbi_start_tvusec', ctypes.c_uint64),
        ]

    if pid <= 0 or pid > 0x7fffffff:
        return None
    info = ProcBsdInfo()
    size = ctypes.sizeof(info)
    # proc_pidinfo fails with ESRCH for a zombie, as for a pid that is gone.
    filled = darwin_libc().proc_pidinfo(ctypes.c_int(pid), ctypes.c_int(PROC_PIDTBSDINFO),
                                        ctypes.c_uint64(0), ctypes.byref(info), ctypes.c_int(size))
    if filled != size or info.pbi_pid != pid:
        return None
    return info


def darwin_process_starttime(pid):
    """Start time in microseconds since the epoch as a digit string, or None."""
    info = darwin_bsdinfo(pid)
    if info is None:
        return None
    return '%d%06d' % (info.pbi_start_tvsec, info.pbi_start_tvusec)


def darwin_child_pids(pid):
    import ctypes
    libc = darwin_libc()
    count = 256
    while True:
        buffer = (ctypes.c_int * count)()
        filled = libc.proc_listpids(ctypes.c_uint32(PROC_PPID_ONLY), ctypes.c_uint32(pid),
                                    buffer, ctypes.c_int(ctypes.sizeof(buffer)))
        if filled <= 0:
            return []
        found = filled // ctypes.sizeof(ctypes.c_int)
        if found < count:
            return [child for child in buffer[:found] if child > 0]
        count *= 4


def darwin_process_args(pid):
    """argv of pid from sysctl KERN_PROCARGS2, or [] when it cannot be read."""
    import ctypes
    import struct
    libc = darwin_libc()
    argmax = ctypes.c_int(0)
    size = ctypes.c_size_t(ctypes.sizeof(argmax))
    mib = (ctypes.c_int * 2)(CTL_KERN, KERN_ARGMAX)
    if libc.sysctl(mib, 2, ctypes.byref(argmax), ctypes.byref(size), None, ctypes.c_size_t(0)) != 0:
        return []
    if argmax.value <= 0:
        return []
    buffer = ctypes.create_string_buffer(argmax.value)
    size = ctypes.c_size_t(argmax.value)
    mib = (ctypes.c_int * 3)(CTL_KERN, KERN_PROCARGS2, pid)
    if libc.sysctl(mib, 3, buffer, ctypes.byref(size), None, ctypes.c_size_t(0)) != 0:
        return []
    raw = buffer.raw[:size.value]
    if len(raw) < 4:
        return []
    argc = struct.unpack('=i', raw[:4])[0]
    # argc, the executable path, NUL padding, then argv strings.
    rest = raw[4:]
    end = rest.find(b'\0')
    if argc <= 0 or end < 0:
        return []
    parts = rest[end:].lstrip(b'\0').split(b'\0')
    return [part.decode('utf-8', errors='replace') for part in parts[:argc]]


def darwin_statfs(path):
    """(filesystem type name, MNT_LOCAL) for path from statfs(2), or None."""
    import ctypes
    u32 = ctypes.c_uint32
    u64 = ctypes.c_uint64

    class StatFs(ctypes.Structure):
        # The 64-bit inode layout, the only one on arm64 and the default since
        # macOS 10.6 on x86_64.
        _fields_ = [
            ('f_bsize', u32), ('f_iosize', ctypes.c_int32), ('f_blocks', u64),
            ('f_bfree', u64), ('f_bavail', u64), ('f_files', u64), ('f_ffree', u64),
            ('f_fsid', ctypes.c_int32 * 2), ('f_owner', u32), ('f_type', u32),
            ('f_flags', u32), ('f_fssubtype', u32),
            ('f_fstypename', ctypes.c_char * MFSTYPENAMELEN),
            ('f_mntonname', ctypes.c_char * MAXPATHLEN),
            ('f_mntfromname', ctypes.c_char * MAXPATHLEN),
            ('f_flags_ext', u32), ('f_reserved', u32 * 7),
        ]

    result = StatFs()
    try:
        encoded = os.fsencode(str(path))
    except (TypeError, ValueError):
        return None
    if darwin_libc().statfs(encoded, ctypes.byref(result)) != 0:
        return None
    name = result.f_fstypename.decode('utf-8', errors='replace')
    return name, bool(result.f_flags & MNT_LOCAL)


def process_tree_pids(root_pid, proc_root=None):
    """Yield root_pid and its live descendants.

    Linux reads procfs task children; macOS asks libproc for each pid's
    children. A test passes proc_root to read a fake procfs on either.
    """
    darwin = proc_root is None and DARWIN
    proc_root = Path(proc_root) if proc_root is not None else PROC_ROOT
    pending = [int(root_pid)]
    visited = set()
    while pending:
        pid = pending.pop()
        if pid in visited:
            continue
        visited.add(pid)
        yield pid
        if darwin:
            pending.extend(darwin_child_pids(pid))
            continue
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
    if proc_root is None and DARWIN:
        return ' '.join(darwin_process_args(pid))
    proc_root = Path(proc_root) if proc_root is not None else PROC_ROOT
    try:
        return (proc_root / str(pid) / 'cmdline').read_bytes().replace(b'\0', b' ').decode(
            'utf-8', errors='replace')
    except OSError:
        return ''


def process_cmdline_args(pid, proc_root=None):
    if proc_root is None and DARWIN:
        return darwin_process_args(pid)
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
    if any(argument in ('--help', '-h', '--version', '-V') for argument in args):
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
        targets.update(process_fd_targets(pid, proc_root))
    return targets


def process_fd_targets(pid, proc_root=None):
    proc_root = Path(proc_root) if proc_root is not None else PROC_ROOT
    fd_dir = proc_root / str(pid) / 'fd'
    try:
        entries = list(fd_dir.iterdir())
    except OSError:
        return set()
    targets = set()
    for entry in entries:
        try:
            target = os.readlink(entry)
        except OSError:
            continue
        if target.endswith(' (deleted)'):
            target = target[:-10]
        targets.add(target)
    return targets


def process_tree_nextpnr_fd_targets(root_pid, proc_root=None):
    proc_root = Path(proc_root) if proc_root is not None else PROC_ROOT
    targets = set()
    for pid in process_tree_pids(root_pid, proc_root):
        if 'nextpnr-mistral' in process_cmdline(pid, proc_root):
            targets.update(process_fd_targets(pid, proc_root))
    return targets


def process_tree_uses_gpu(root_pid, vendor, device_nodes, proc_root=None,
                          refresh_apple_clients=False):
    """Check that nextpnr itself opened a leased hardware node.

    Metal has no device nodes. On Apple the evidence is a user client of the
    leased GPU whose creator is a nextpnr process in the tree.
    """
    if device_nodes is None:
        expected_nodes = []
    elif isinstance(device_nodes, str):
        expected_nodes = [device_nodes]
    else:
        expected_nodes = list(device_nodes)
    mapped_nodes = {node for node in expected_nodes if isinstance(node, str)}
    first_node = expected_nodes[0] if expected_nodes else None
    if vendor == 'apple':
        nextpnr_pids = {pid for pid in process_tree_pids(root_pid, proc_root)
                        if 'nextpnr-mistral' in process_cmdline(pid, proc_root)}
        if not nextpnr_pids:
            return False
        return any(creators & nextpnr_pids
                   for device, creators in apple_gpu_clients(
                       refresh=refresh_apple_clients).items()
                   if first_node is None or device in mapped_nodes)
    targets = process_tree_nextpnr_fd_targets(root_pid, proc_root)
    if vendor == 'amd':
        render_nodes = {target for target in targets
                        if re.fullmatch(r'/dev/dri/renderD\d+', target)}
        return '/dev/kfd' in targets and bool(render_nodes) and (
            first_node is None or bool(render_nodes & mapped_nodes))
    if vendor == 'nvidia':
        return bool(mapped_nodes & targets)
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


def detect_apple_ids():
    return list(apple_device_names(apple_accelerators()))


def owner_cap():
    raw = os.environ.get('GPU_LEASE_MAX_PER_OWNER')
    if raw is None or raw.strip() == '':
        return None
    if not raw.strip().isdigit() or int(raw.strip()) < 1:
        raise LeaseError('GPU_LEASE_MAX_PER_OWNER must be a positive integer')
    return int(raw.strip())


def job_slot_cap():
    raw = os.environ.get('GPU_LEASE_MAX_JOBS_PER_OWNER')
    if raw is None or raw.strip() == '':
        return DEFAULT_JOB_SLOTS
    value = raw.strip()
    if (not re.fullmatch(r'[0-9]{1,3}', value)) or not 1 <= int(value) <= MAX_JOB_SLOTS:
        raise LeaseError('GPU_LEASE_MAX_JOBS_PER_OWNER must be an integer from 1 to %d' % MAX_JOB_SLOTS)
    return int(value)


def resolve_inventory():
    vendor = os.environ.get('GPU_LEASE_VENDOR', '').strip().lower() or None
    if vendor is not None and vendor not in VENDORS:
        raise LeaseError('GPU_LEASE_VENDOR must be amd, nvidia or apple')
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
            elif detect_apple_ids():
                vendor = 'apple'
            else:
                raise LeaseError('set GPU_LEASE_VENDOR when GPU ids are overridden and no GPU vendor is detectable')
        return vendor, sorted(ids, key=id_key)
    if vendor == 'nvidia':
        ids = detect_nvidia_ids()
    elif vendor == 'amd':
        ids = detect_amd_ids()
    elif vendor == 'apple':
        ids = detect_apple_ids()
    else:
        ids = detect_nvidia_ids()
        vendor = 'nvidia' if ids else None
        if not ids:
            ids = detect_amd_ids()
            vendor = 'amd' if ids else None
        if not ids:
            ids = detect_apple_ids()
            vendor = 'apple' if ids else None
    if not ids:
        raise LeaseError('no GPUs detected; set GPU_LEASE_IDS and GPU_LEASE_VENDOR')
    return vendor, sorted(ids, key=id_key)


def vendor_mask_variable(vendor):
    """The visibility mask a grant sets, or None for Metal, which has none."""
    if vendor == 'apple':
        return None
    return 'ROCR_VISIBLE_DEVICES' if vendor == 'amd' else 'CUDA_VISIBLE_DEVICES'


def iso(timestamp):
    return time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime(timestamp))


def process_starttime(pid):
    """Process start identity as a digit string, or None when pid is gone or a zombie.

    Linux: boot-relative start ticks from /proc. macOS: start time in
    microseconds from libproc.
    """
    if DARWIN:
        return darwin_process_starttime(pid)
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
    strong as /proc/self/mounts. Unknown filesystem types are allowed. macOS
    has no mount table file; statfs(2) reports the filesystem holding the
    directory, and one the kernel does not mark local (MNT_LOCAL) is refused.
    """
    if mounts_text is None and DARWIN:
        found = darwin_statfs(existing_ancestor(path))
        if found is None:
            return
        fstype, local = found
        if not local or fstype in REMOTE_FILESYSTEMS:
            raise LeaseError('%s is on %s, which is not a local filesystem' % (path, fstype))
        return
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


def create_private_dir(directory):
    old = os.umask(0o077)
    try:
        try:
            directory.mkdir(mode=PRIVATE_DIR_MODE)
            return True
        except FileExistsError:
            return False
    finally:
        os.umask(old)


def ensure_private_subdir(path):
    """Create or tighten one private directory inside a prepared lease directory."""
    path = Path(path)
    if not path.exists():
        create_private_dir(path)
    if not path.is_dir():
        raise LeaseError(str(path) + ' is not a directory')
    restrict_path(path, PRIVATE_DIR_MODE)
    return path


def ensure_lease_dir(path):
    path = Path(path)
    assert_local_filesystem(path)
    if path.exists() and not path.is_dir():
        raise LeaseError(str(path) + ' is not a directory')
    if not path.exists():
        create_private_dir(path)
    if not path.is_dir():
        raise LeaseError(str(path) + ' is not a directory')
    restrict_path(path, PRIVATE_DIR_MODE)
    ensure_private_subdir(path / QUEUE)
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
    # Owner limits apply across every GPU leased through this shared
    # directory, including lock files created under other inventories.
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
    expected = ticket.get('starttime')
    if not isinstance(expected, str) or not expected.isdigit():
        return False
    identity = process_starttime(ticket['pid'])
    if identity is None:
        return False
    return str(expected) == identity


def iter_tickets(directory, queue_name=QUEUE):
    queue = Path(directory) / queue_name
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
    for queue_name in (QUEUE, YIELD_QUEUE):
        queue = Path(directory) / queue_name
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


def enqueue(directory, owner, label, gpus, eligible_ids, vendor, command, cap, queue_name=QUEUE):
    def operation():
        seq = next_seq(directory)
        now = time.time()
        pid = os.getpid()
        starttime = process_starttime(pid)
        if starttime is None:
            raise LeaseError('cannot read the current process start time')
        ticket = {
            'ticket': seq,
            'owner': owner,
            'label': label,
            'pid': pid,
            'host': socket.gethostname(),
            'gpus': gpus,
            'eligible_ids': list(eligible_ids),
            'vendor': vendor,
            'vendor_mask': vendor_mask_variable(vendor),
            'cap': cap,
            'enqueued': iso(now),
            'enqueued_unix': now,
            'starttime': starttime,
            'command': redact_command(command),
        }
        path = Path(directory) / queue_name / ('%08d.json' % seq)
        write_atomic(path, ticket)
        return path, ticket
    return with_state_lock(directory, operation)


def ticket_blocked_by_own_cap(directory, ids, ticket):
    cap = ticket_cap(ticket)
    if cap is None:
        return False
    return held_by_owner(directory, ids, ticket['owner'], ()) + ticket['gpus'] > cap


def ticket_eligible_ids(ticket, vendor, fallback_ids):
    """Return recorded eligibility, conservatively falling back for old tickets."""
    eligible = ticket.get('eligible_ids')
    if ticket.get('vendor') != vendor or ticket.get('vendor_mask') != vendor_mask_variable(vendor):
        return list(fallback_ids)
    if not isinstance(eligible, list) or not eligible or not all(
            isinstance(gpu_id, str) and ID_RE.fullmatch(gpu_id) for gpu_id in eligible):
        return list(fallback_ids)
    if len(eligible) != len(set(eligible)):
        return list(fallback_ids)
    return eligible


def known_gpu_ids(directory, requested_ids, tickets):
    """Return ids known to this lease directory for owner-cap checks."""
    known = set(requested_ids)
    for ticket in tickets:
        eligible = ticket.get('eligible_ids')
        if isinstance(eligible, list):
            known.update(gpu_id for gpu_id in eligible
                         if isinstance(gpu_id, str) and ID_RE.fullmatch(gpu_id))
    try:
        for path in Path(directory).glob('gpu-*.lock'):
            gpu_id = path.name[len('gpu-'):-len('.lock')]
            if ID_RE.fullmatch(gpu_id):
                known.add(gpu_id)
    except OSError:
        pass
    return sorted(known, key=id_key)


def earlier_ticket_gpu_ids(directory, tickets, ids, vendor, ticket):
    """Reserve eligible GPUs for earlier tickets that are not cap-blocked."""
    own_ticket = ticket.get('ticket') if ticket is not None else None
    cap_ids = known_gpu_ids(directory, ids, tickets)
    reserved = set()
    for earlier in tickets:
        if own_ticket is not None and earlier['ticket'] >= own_ticket:
            break
        if ticket_blocked_by_own_cap(directory, cap_ids, earlier):
            continue
        reserved.update(ticket_eligible_ids(earlier, vendor, ids))
    return reserved


def attempt(directory, ids, count, vendor, owner, label, command, cap, ticket_path, ticket,
            queue_name=QUEUE):
    def operation():
        tickets = iter_tickets(directory, queue_name)
        if ticket is not None and not any(item['ticket'] == ticket['ticket'] for item in tickets):
            return None
        reserved = earlier_ticket_gpu_ids(directory, tickets, ids, vendor, ticket)
        if queue_name == YIELD_QUEUE:
            # Every runnable main-queue ticket is served first, whatever its
            # number, so a yielding phase takes only GPUs none of them can use.
            # A main-queue waiter that needs more GPUs than are free keeps its
            # reservation, as it does against later main-queue tickets;
            # otherwise phases taking each GPU as it frees could starve it.
            reserved |= earlier_ticket_gpu_ids(directory, iter_tickets(directory), ids, vendor, None)
        available_ids = [gpu_id for gpu_id in ids if gpu_id not in reserved]
        if len(available_ids) < count:
            return None
        chosen = lock_gpus(directory, available_ids, count)
        if chosen is None:
            return None
        try:
            skip = {gpu_id for gpu_id, _ in chosen}
            cap_ids = known_gpu_ids(directory, ids, tickets)
            if cap is not None and held_by_owner(directory, cap_ids, owner, skip) + count > cap:
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


def acquire(directory, ids, count, vendor, owner, label, command, cap, wait, timeout,
            queue_name=QUEUE, pause=None):
    """Lock count GPUs, waiting in queue_name when wait is set.

    pause(delay), when given, replaces the sleep between attempts and returns
    True to stop waiting; acquire() then returns None.
    """
    ticket_path = None
    ticket = None
    got = None
    deadline = None if timeout is None else time.monotonic() + timeout
    try:
        if wait:
            ticket_path, ticket = enqueue(directory, owner, label, count, ids, vendor,
                                          command, cap, queue_name)
        backoff = POLL_INITIAL
        while True:
            got = attempt(directory, ids, count, vendor, owner, label, command, cap,
                          ticket_path, ticket, queue_name)
            if got is not None:
                return got
            if not wait:
                return None
            if deadline is not None and time.monotonic() >= deadline:
                return None
            delay = min(backoff, POLL_MAX)
            if deadline is not None:
                delay = min(delay, max(0.0, deadline - time.monotonic()))
            if pause is not None:
                if pause(delay):
                    return None
            elif delay > 0:
                time.sleep(delay)
            backoff = min(backoff * 2, POLL_MAX)
    finally:
        if ticket_path is not None and got is None:
            quiet_unlink(ticket_path)


def child_env(vendor, ids, device_node=None):
    env = os.environ.copy()
    joined = ','.join(ids)
    for key in ('HIP_VISIBLE_DEVICES', 'ROCR_VISIBLE_DEVICES', 'CUDA_VISIBLE_DEVICES', 'CUDA_DEVICE_ORDER'):
        env.pop(key, None)
    env['FES_GPU_IDS'] = joined
    env['GPU_RUN_DEVICE'] = '0'
    env['GPU_RUN_EXPECTED_DEVICE'] = device_node or ('%s:%s' % (vendor, ids[0]))
    env['NEXTPNR_REQUIRE_GPU'] = '1'
    if vendor == 'amd':
        env['ROCR_VISIBLE_DEVICES'] = joined
    elif vendor == 'apple':
        # Metal cannot hide devices, so nothing is renumbered: nextpnr
        # selects the physical leased id itself.
        env['GPU_RUN_DEVICE'] = ids[0]
    else:
        env['CUDA_VISIBLE_DEVICES'] = joined
        # Granted ids are nvidia-smi indices, which follow PCI bus order. CUDA's
        # default order is fastest first, under which the mask selects another
        # GPU on a box whose fastest device is not at the lowest bus address.
        env['CUDA_DEVICE_ORDER'] = 'PCI_BUS_ID'
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
    eligible = ticket.get('eligible_ids')
    if not isinstance(eligible, list) or not all(
            isinstance(gpu_id, str) and ID_RE.fullmatch(gpu_id) for gpu_id in eligible):
        eligible = None
    return {
        'ticket': ticket['ticket'],
        'owner': ticket['owner'],
        'label': ticket.get('label') if isinstance(ticket.get('label'), str) else '',
        'pid': ticket['pid'],
        'owner_pid': ticket['pid'],
        'host': ticket['host'],
        'gpus': ticket['gpus'],
        'eligible_ids': eligible,
        'vendor': ticket.get('vendor') if isinstance(ticket.get('vendor'), str) else None,
        'vendor_mask': ticket.get('vendor_mask') if isinstance(ticket.get('vendor_mask'), str) else None,
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
    if directory.is_dir():
        # Yielding phase requests are served after the main queue, so list them last.
        queue.extend(dict(public_ticket(ticket), yields=True)
                     for ticket in iter_tickets(directory, YIELD_QUEUE))
    payload = {'lease_dir': str(directory), 'vendor': vendor, 'gpus': gpus, 'queue': queue}
    jobs = job_report(directory) if directory.is_dir() else None
    if jobs is not None:
        payload['jobs'] = jobs
    return payload


def job_report(directory):
    """Phase-lease jobs in a job slot or waiting for one, or None if none ever ran."""
    root = Path(directory) / 'jobs'
    if not root.is_dir():
        return None
    try:
        slot_directories = sorted(entry for entry in root.iterdir() if entry.is_dir())
    except OSError:
        return []
    jobs = []
    for slot_directory in slot_directories:
        slot_ids = []
        for path in slot_directory.glob('gpu-*.lock'):
            slot_id = path.name[len('gpu-'):-len('.lock')]
            if ID_RE.fullmatch(slot_id):
                slot_ids.append(slot_id)
        for slot_id in sorted(slot_ids, key=id_key):
            meta = read_holder(slot_directory, slot_id)
            if meta is not None:
                job = public_held(slot_id, meta)
                job['slot'] = job.pop('id')
                job['state'] = 'running'
                jobs.append(job)
        for ticket in iter_tickets(slot_directory):
            waiting = public_ticket(ticket)
            jobs.append({'state': 'waiting', 'ticket': waiting['ticket'], 'owner': waiting['owner'],
                         'label': waiting['label'], 'pid': waiting['pid'], 'host': waiting['host'],
                         'age_seconds': waiting['age_seconds'], 'command': waiting['command']})
    return jobs


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
        eligible = ','.join(item['eligible_ids']) if item.get('eligible_ids') else '?'
        mask = item.get('vendor_mask') or ('none' if item.get('vendor') == 'apple' else '?')
        lines.append('waiting %s  owner=%s%s gpus=%s eligible=%s mask=%s label=%s owner_pid=%s host=%s age=%s command=%s' % (
            item['ticket'], item['owner'], ' yields' if item.get('yields') else '', item['gpus'], eligible,
            mask, item.get('label') or '-', item['pid'], item['host'],
            format_age(item['age_seconds']), command))
    for job in payload.get('jobs', ()):
        command = shlex.join(job['command']) if job.get('command') else '-'
        if job['state'] == 'running':
            lines.append('phase job running  owner=%s slot=%s label=%s pid=%s host=%s age=%s command=%s' % (
                job.get('owner') or '-', job['slot'], job.get('label') or '-',
                job.get('pid') if job.get('pid') is not None else '-', job.get('host') or '-',
                format_age(job.get('age_seconds')), command))
        else:
            lines.append('phase job waiting %s  owner=%s label=%s owner_pid=%s host=%s age=%s command=%s' % (
                job['ticket'], job['owner'], job.get('label') or '-', job['pid'], job['host'],
                format_age(job['age_seconds']), command))
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
    parser.add_argument('--phase-lease', action='store_true',
                        help='take no GPU up front; serve GPU phase requests on GPU_LEASE_FD')
    parser.add_argument('cmd', nargs=argparse.REMAINDER, help='command to exec, after --')
    args = parser.parse_args(argv)
    if args.wait and args.no_wait:
        parser.error('--wait and --no-wait are mutually exclusive')
    if args.no_wait and args.timeout is not None:
        parser.error('--timeout applies only when waiting')
    if args.gpus < 1:
        parser.error('--gpus must be at least 1')
    if args.phase_lease and (args.no_wait or args.timeout is not None):
        parser.error('--phase-lease always waits; --no-wait and --timeout do not apply')
    if args.phase_lease and args.gpus != 1:
        parser.error('--phase-lease grants one GPU per phase; use --gpus 1')
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


GPU_BACKEND_READY_RE = re.compile(rb'\bbackend\s+(hip|cuda|metal):([^\s:]*)[^\n]*\bready\b', re.I)
EXPECTED_BACKEND = {'amd': 'hip', 'nvidia': 'cuda', 'apple': 'metal'}
GPU_CPU_FALLBACK_MARKERS = (
    b'falling back to the cpu reference backend',
    b'backend cpu-reference',
)


class GpuRouteOutput:
    """Track nextpnr backend evidence while forwarding output unchanged."""

    def __init__(self, vendor):
        self.expected_backend = EXPECTED_BACKEND.get(vendor, 'cuda')
        self.tail = b''
        self.backend = None
        # The first field after the backend name. Metal logs its registry id
        # there, which names the GPU the backend initialised.
        self.device = None
        self.cpu_fallback = False

    def consume(self, chunk):
        combined = self.tail + chunk.lower()
        if any(marker in combined for marker in GPU_CPU_FALLBACK_MARKERS):
            self.cpu_fallback = True
        if self.backend is None:
            match = GPU_BACKEND_READY_RE.search(combined)
            if match is not None:
                self.backend = match.group(1).decode('ascii').lower()
                self.device = match.group(2).decode('ascii', errors='replace').lower()
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
    device_nodes = [gpu_device_path(vendor, gpu_id) for gpu_id in gpu_ids]
    device_node = device_nodes[0]
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
    apple_discovery_deadline = time.monotonic() + APPLE_CLIENT_DISCOVERY_WINDOW
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
                opened_gpu = process_tree_uses_gpu(
                    pid, vendor, device_nodes,
                    refresh_apple_clients=(vendor == 'apple' and evidence.backend is None
                                           and not evidence.cpu_fallback
                                           and time.monotonic() < apple_discovery_deadline))
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
    return route_status(vendor, gpu_ids, device_nodes, requires_gpu, opened_gpu, evidence,
                        output_pipes is not None, status)


def route_status(vendor, gpu_ids, device_nodes, requires_gpu, opened_gpu, evidence, captured,
                 status):
    """Exit status for CMD, failing a GPU route that did not prove GPU use."""
    # FES-pinned DeanoC/nextpnr 9128800f passes cfg.device to
    # create_device_backend() and logs "backend ... ready" only after
    # backend->init() succeeds (common/route/gpurouter.cc:2709-2759). HIP and
    # CUDA share DeviceBackend::init (common/route/gpu/gpuroute_kernel.cuh:
    # 725-769); it selects device_ before querying/allocating state and
    # synchronizes before success. CUDA's runtime aliases are in
    # common/route/gpu/gpuroute_compat.h:26-54.
    backend_evidence = (
        captured
        and evidence.backend == evidence.expected_backend
        and not evidence.cpu_fallback
    )
    mapped_nodes = [node for node in device_nodes if node]
    if vendor == 'apple' and backend_evidence and mapped_nodes:
        # nextpnr's Metal backend logs "backend metal:<registry id>:<name>
        # ready" only after it initialised that MTLDevice. It must be the
        # leased one.
        backend_evidence = 'metal:' + (evidence.device or '') in mapped_nodes
    if requires_gpu and (evidence.cpu_fallback or not (opened_gpu or backend_evidence)):
        if evidence.cpu_fallback:
            print('gpu-run: nextpnr reported the CPU reference backend; refusing silent CPU fallback',
                  file=sys.stderr)
        else:
            if mapped_nodes:
                expected = (mapped_nodes[0] if len(mapped_nodes) == 1 else
                            'one of %s' % ', '.join(mapped_nodes))
            else:
                expected = 'a device node for %s GPU %s (mapping unavailable)' % (vendor, gpu_ids[0])
            if vendor == 'amd':
                opened = '/dev/kfd and %s' % expected
            elif vendor == 'apple':
                opened = 'Metal GPU %s' % expected
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


def job_slot_directory(directory, owner):
    """Per-owner job-slot lease directory inside the GPU lease directory."""
    if ID_RE.fullmatch(owner) and owner not in ('.', '..'):
        name = owner
    else:
        name = 'sha256-' + hashlib.sha256(owner.encode('utf-8', 'surrogateescape')).hexdigest()
    return Path(directory) / 'jobs' / name


def acquire_job_slot(directory, vendor, owner, label, command, slots):
    """Wait first come, first served for one of the owner's phase-lease job slots.

    A slot directory has the GPU lease layout with one lock file per slot, so
    acquire() provides the flock, FIFO and crash behaviour. Passing the slot
    count as the owner cap counts every held slot, so jobs started with
    different caps still respect their own. The job holds no GPU while it
    waits here.
    """
    ensure_private_subdir(Path(directory) / 'jobs')
    slot_directory = ensure_lease_dir(job_slot_directory(directory, owner))
    slot_ids = [str(index) for index in range(slots)]
    return acquire(slot_directory, slot_ids, 1, vendor, owner, label, command, slots, True, None)


def phase_child_env(vendor, lease_fd):
    """Environment for a --phase-lease command, which starts without a grant."""
    env = os.environ.copy()
    for key in ('HIP_VISIBLE_DEVICES', 'ROCR_VISIBLE_DEVICES', 'CUDA_VISIBLE_DEVICES',
                'CUDA_DEVICE_ORDER', 'FES_GPU_IDS', 'GPU_RUN_DEVICE', 'GPU_RUN_EXPECTED_DEVICE'):
        env.pop(key, None)
    env['GPU_LEASE_FD'] = str(lease_fd)
    env['NEXTPNR_REQUIRE_GPU'] = '1'
    if vendor == 'nvidia':
        # Granted ids are nvidia-smi indices, which follow PCI bus order.
        env['CUDA_DEVICE_ORDER'] = 'PCI_BUS_ID'
    return env


class PhaseConnection:
    """gpu-run's end of the GPU_LEASE_FD socketpair."""

    def __init__(self, sock):
        sock.setblocking(False)
        self.sock = sock
        self.buffer = b''
        self.discarding = False

    @property
    def open(self):
        return self.sock is not None

    def fileno(self):
        return self.sock.fileno()

    def close(self):
        if self.sock is not None:
            self.sock.close()
            self.sock = None
        self.buffer = b''

    def receive(self):
        """Buffer what has arrived. EOF or a socket error closes the connection."""
        while self.sock is not None and len(self.buffer) <= 4 * PHASE_REQUEST_LIMIT:
            try:
                chunk = self.sock.recv(4096)
            except (BlockingIOError, InterruptedError):
                return
            except OSError:
                chunk = b''
            if not chunk:
                self.close()
                return
            self.buffer += chunk

    def requests(self):
        """Yield complete request lines; an overlong line yields one '' instead."""
        while self.sock is not None:
            newline = self.buffer.find(b'\n')
            if newline < 0:
                if len(self.buffer) > PHASE_REQUEST_LIMIT:
                    self.buffer = b''
                    if not self.discarding:
                        self.discarding = True
                        yield ''
                return
            raw = self.buffer[:newline]
            self.buffer = self.buffer[newline + 1:]
            if self.discarding:
                self.discarding = False
                continue
            if len(raw) > PHASE_REQUEST_LIMIT:
                yield ''
                continue
            try:
                yield raw.decode('ascii')
            except UnicodeDecodeError:
                yield ''

    def reply(self, text):
        """Send one reply line. A peer that cannot take it is treated as closed."""
        data = (text + '\n').encode('ascii')
        deadline = time.monotonic() + PHASE_REPLY_TIMEOUT
        while data and self.sock is not None:
            try:
                data = data[self.sock.send(data):]
            except (BlockingIOError, InterruptedError):
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    self.close()
                    return
                select.select([], [self.sock], [], remaining)
            except OSError:
                self.close()


def execute_phase_lease(directory, ids, vendor, owner, label, command, cap, slot):
    """Run command with GPU_LEASE_FD, holding a GPU only while it asks for one.

    gpu-run keeps the job slot until the command exits and serves one request
    at a time. ACQUIRE waits through acquire() and RELEASE calls release(), so
    the flock, FIFO, backfill and owner cap rules are those of whole-command
    leases. The command never inherits the slot or a grant: a grant ends at
    RELEASE, when the socket closes, or when the command exits. Like execute(),
    this waits only for the direct child and never signals any process.
    """
    queue_name = YIELD_QUEUE if owner in YIELDING_OWNERS else QUEUE
    try:
        if queue_name == YIELD_QUEUE:
            ensure_private_subdir(Path(directory) / YIELD_QUEUE)
        broker, client = socket.socketpair(socket.AF_UNIX, socket.SOCK_STREAM)
    except BaseException:
        release(slot)
        raise
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
        broker.close()
        client.close()
        release(slot)
        raise
    if pid == 0:
        try:
            # The command gets a copy above stdio that survives exec; every
            # other descriptor gpu-run opened is close-on-exec.
            lease_fd = fcntl.fcntl(client.fileno(), fcntl.F_DUPFD, 3)
            broker.close()
            client.close()
            if output_pipes is not None:
                stdout_read, stdout_write, stderr_read, stderr_write = output_pipes
                os.close(stdout_read)
                os.close(stderr_read)
                os.dup2(stdout_write, 1)
                os.dup2(stderr_write, 2)
                os.close(stdout_write)
                os.close(stderr_write)
            env = phase_child_env(vendor, lease_fd)
        except BaseException:
            os._exit(127)
        exec_command(slot, command, env)
    client.close()
    connection = PhaseConnection(broker)
    selector = selectors.DefaultSelector()
    evidence = GpuRouteOutput(vendor)
    if output_pipes is not None:
        stdout_read, stdout_write, stderr_read, stderr_write = output_pipes
        os.close(stdout_write)
        os.close(stderr_write)
        selector.register(stdout_read, selectors.EVENT_READ, 1)
        selector.register(stderr_read, selectors.EVENT_READ, 2)
    chosen = None
    granted_ids = []
    opened_gpu = False
    apple_discovery_deadline = None
    status = None

    def reap():
        nonlocal status
        if status is None:
            try:
                waited, raw_status = os.waitpid(pid, os.WNOHANG)
            except InterruptedError:
                return False
            if waited == pid:
                status = raw_status
        return status is not None

    def wait_for_input(timeout):
        fds = list(selector.get_map())
        if connection.open:
            fds.append(connection.fileno())
        try:
            select.select(fds, [], [], timeout)
        except InterruptedError:
            pass
        if output_pipes is not None:
            drain_route_output(selector, evidence)
        connection.receive()

    def pause(delay):
        # acquire() waits here. Keep forwarding output, and stop waiting once
        # the command has exited or closed its end of the socket.
        end = time.monotonic() + delay
        while True:
            wait_for_input(max(0.0, min(GPU_MONITOR_INTERVAL, end - time.monotonic())))
            if not connection.open or reap():
                return True
            if time.monotonic() >= end:
                return False

    def answer(request):
        nonlocal chosen, apple_discovery_deadline
        if request == 'RELEASE':
            if chosen is None:
                return 'ERR not-held'
            release(chosen)
            chosen = None
            apple_discovery_deadline = None
            return 'OK'
        match = ACQUIRE_RE.fullmatch(request)
        if match is None:
            return 'ERR unknown-command'
        if chosen is not None:
            return 'ERR already-held'
        if int(match.group(1)) != 1:
            return 'ERR unsupported-count'
        try:
            got = acquire(directory, ids, 1, vendor, owner, label, command, cap, True, None,
                          queue_name, pause)
        except (LeaseError, OSError) as error:
            print('gpu-run: phase lease failed: %s' % error, file=sys.stderr)
            return 'ERR lease-failed'
        if got is None:
            return None
        chosen = got
        apple_discovery_deadline = time.monotonic() + APPLE_CLIENT_DISCOVERY_WINDOW
        granted_ids.extend(gpu_id for gpu_id, _ in got if gpu_id not in granted_ids)
        return 'GRANT ' + got[0][0]

    try:
        while True:
            if not requires_gpu:
                requires_gpu = process_tree_requires_gpu(pid)
            if requires_gpu and chosen is not None and granted_ids and not opened_gpu:
                opened_gpu = process_tree_uses_gpu(
                    pid, vendor, [gpu_device_path(vendor, gpu_id) for gpu_id in granted_ids],
                    refresh_apple_clients=(vendor == 'apple' and evidence.backend is None
                                           and not evidence.cpu_fallback
                                           and apple_discovery_deadline is not None
                                           and time.monotonic() < apple_discovery_deadline))
            wait_for_input(GPU_MONITOR_INTERVAL)
            for request in connection.requests():
                reply = answer(request)
                if reply is not None:
                    connection.reply(reply)
                if not connection.open:
                    break
            if not connection.open and chosen is not None:
                release(chosen)
                chosen = None
            if reap():
                break
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
        connection.close()
        signal.signal(signal.SIGINT, previous_interrupt)
        if chosen is not None:
            release(chosen)
        release(slot)
    if requires_gpu and not granted_ids:
        print('gpu-run: nextpnr never requested a GPU phase on GPU_LEASE_FD; refusing GPU use without a lease',
              file=sys.stderr)
        if os.WIFEXITED(status) and os.WEXITSTATUS(status) == 0:
            return 70
        requires_gpu = False
    device_nodes = [gpu_device_path(vendor, gpu_id) for gpu_id in granted_ids]
    return route_status(vendor, granted_ids, device_nodes, requires_gpu, opened_gpu, evidence,
                        output_pipes is not None, status)


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
        if args.phase_lease:
            slots = job_slot_cap()
            if not all(gpu_id.isdigit() for gpu_id in ids):
                raise LeaseError('--phase-lease grants decimal GPU ids; the inventory has another id')
        directory = ensure_lease_dir(lease_directory())
        if args.phase_lease:
            slot = acquire_job_slot(directory, vendor, owner, label, args.cmd, slots)
            return execute_phase_lease(directory, ids, vendor, owner, label, args.cmd, cap, slot)
        chosen = acquire(directory, ids, args.gpus, vendor, owner, label, args.cmd, cap,
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
                                         description='Show GPU lease holders, ages, the waiting queue, '
                                                     'and phase-lease jobs.')
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
