#!/usr/bin/env python3
"""Opt-in PC/XT application diagnostic inside an already owned kit session.

The caller must hold the kit queue lock and a status-file claim, launch the
expected package with its selected floppy, and supply that launch response.
This diagnostic never installs software, claims a kit, launches/stops a
session, or restores/releases the kit. The caller remains responsible for all
session and kit cleanup even on failure.
"""
import argparse
import ctypes
import fcntl
import hashlib
import io
import json
import math
import os
import re
import select
import socket
import shutil
import signal
import subprocess
import sys
import tarfile
import threading
import time
import tomllib
import urllib.parse
from datetime import datetime, timezone
from pathlib import Path

if __package__:
    from . import package_acceptance as pa
    from . import pcxt_dos_hil_acceptance as visual
    from . import pcxt_gameplay_capture as game_capture
    from . import pcxt_gameplay_probe as gameplay
    from . import pcxt_game_audio_capture as game_audio
else:
    # Isolated mode omits the script directory from sys.path. Load only
    # the helpers adjacent to this diagnostic, even under python -I.
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import package_acceptance as pa
    import pcxt_dos_hil_acceptance as visual
    import pcxt_gameplay_capture as game_capture
    import pcxt_gameplay_probe as gameplay
    import pcxt_game_audio_capture as game_audio

FLOPPY_SIZE = 368640
COM_SHA = '475054504cb7e96268576bcc97db4fdcbf4cf66063673145df2f69f8ff859c02'
GAME_SHA = '5cbe1c156502e08abbeb7f2a9f3e3c285038b4f33e1cc25085ff98a2d5833706'
SCENARIOS = {
    'com': {'floppy': 'ad4783e8b4decc6859df9389d374cb60ba55b735a0694901e48e02038aa67480',
            'program': COM_SHA, 'name': 'FESCOM.COM', 'size': 101},
    'game': {'floppy': '8621217fce4d72cf0ec976d34b9dd302be136b21e5ff15197fee4d499b923fa3',
             'program': GAME_SHA, 'name': 'DIGGER.EXE', 'size': 58835},
}
READY = 'FES COM READY (PRESS K)'
PASS = 'FES COM PASS'
FAIL = 'FES COM FAIL'
COM_COMMAND = (0x09, 0x08, 0x16, 0x06, 0x12, 0x10, 0x28)
GAME_COMMAND = (0x07, 0x0c, 0x0a, 0x0a, 0x08, 0x15, 0x2c, 0x38, 0x06, 0x2c, 0x38, 0x14, 0x28)
DIRECTIONS = (('right', 0x4f), ('left', 0x50), ('up', 0x52), ('down', 0x51))




def inspect_archive(data, args):
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:*') as tf:
        members = tf.getmembers()
        payload = [m for m in members if m.name == 'core.rbf' and m.isfile()]
        manifest = [m for m in members if m.name == 'manifest.toml' and m.isfile()]
        if len(payload) != 1 or not 0 < payload[0].size <= 32 * 1024 * 1024:
            raise pa.AcceptanceError('archive must contain exactly one bounded RBF')
        if len(manifest) != 1 or not 0 < manifest[0].size <= 1024 * 1024:
            raise pa.AcceptanceError('archive must contain exactly one bounded manifest')
        rbf = tf.extractfile(payload[0]).read()
        manifest_bytes = tf.extractfile(manifest[0]).read()
        if getattr(args, 'manifest_sha256', None) and hashlib.sha256(manifest_bytes).hexdigest() != args.manifest_sha256:
            raise pa.AcceptanceError('archive manifest differs from prepared identity')
        doc = tomllib.loads(manifest_bytes.decode())
    digest = hashlib.sha256(rbf).hexdigest()
    if (digest != args.rbf_sha256 or doc.get('core', {}).get('id') != 'fes.pcxt-dos'
        or doc.get('build', {}).get('revision') != args.source_commit
        or doc.get('payload', {}).get('sha256') != digest
        or doc.get('payload', {}).get('size') != len(rbf)):
        raise pa.AcceptanceError('archive core, source or payload identity differs')
    return doc


def capture(device, path, phase):
    if path.exists():
        raise pa.AcceptanceError('capture already exists: ' + str(path))
    # The Kit B loopback is safe only while its paired physical USB card and
    # fanout retain the independently checked identity across this frame.
    identity = None
    if device == '/dev/v4l/by-id/fes-4kpro-hil':
        try: identity = game_audio.capture_identity('B')
        except game_audio.AudioError as exc:
            raise pa.AcceptanceError('Kit B capture identity unavailable') from exc
    subprocess.run(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-f', 'v4l2',
                    '-input_format', 'yuyv422', '-video_size', '1920x1080', '-i', device,
                    '-vf', r'select=eq(n\,119)', '-frames:v', '1', '-y', str(path)],
                   check=True, timeout=30)
    width, height, rgb, digest = visual.decode_capture(path)
    if identity is not None:
        try: after = game_audio.capture_identity('B')
        except game_audio.AudioError as exc:
            raise pa.AcceptanceError('Kit B capture identity unavailable after frame') from exc
        if after != identity: raise pa.AcceptanceError('Kit B capture identity changed during frame')
    metadata = {'file': path.name, 'sha256': digest,
                'captured_at_utc': stamp(), 'phase': phase}
    if identity is not None: metadata['capture_identity'] = identity
    return (width, height, rgb), metadata


def prompt(frame, font):
    try:
        return visual.find_prompt(*frame, font)
    except ValueError as exc:
        if str(exc) == r'final framebuffer does not contain the FreeDOS A:\> CGA glyph sequence':
            return None
        raise


def change(a, b):
    if a[:2] != b[:2]:
        raise pa.AcceptanceError('capture dimensions changed')
    return visual.measure_frame_change(a[2], b[2], a[0], a[1])


def read_anchored(frame, font, anchor, dimensions):
    width, height, rgb = frame
    if (width, height) != dimensions:
        raise ValueError('capture dimensions changed')
    fg, bg = anchor['foreground_rgb'], anchor['background_rgb']
    dx, dy = anchor['alignment_dx'], anchor['alignment_dy']
    glyphs = {code: int.from_bytes(font[code*16:code*16+8], 'big') for code in range(32,127)}
    lines = []
    for row in range(25):
        chars = []
        for col in range(80):
            observed = 0
            for gy in range(8):
                for gx in range(8):
                    sx, sy = col*8+gx, row*8+gy
                    x = round((160+(sx+0.5)*1.5)*width/1280)+dx
                    y = round((sy+0.5)*height/200)+dy
                    value = visual._pixel(rgb,width,x,y)
                    df = sum((a-b)**2 for a,b in zip(value,fg))
                    db = sum((a-b)**2 for a,b in zip(value,bg))
                    observed = (observed << 1) | (df < db)
            distances = [(observed ^ pattern).bit_count() for pattern in glyphs.values()]
            best = min(distances)
            matches = [code for code,distance in zip(glyphs,distances) if distance == best]
            chars.append(chr(matches[0]) if best <= 3 and len(matches) == 1 else '?')
        lines.append(''.join(chars))
    return lines


def marker_row(lines, marker):
    return next((i for i,line in enumerate(lines) if marker in line), None)


def ensure_fresh_before(frame, font, anchor):
    lines = read_anchored(frame, font, anchor, frame[:2])
    if any(marker_row(lines, marker) is not None for marker in (READY, PASS, FAIL)):
        raise pa.AcceptanceError('pre-input screen already contains COM output')


def ready_check(frame, font, anchor, dimensions):
    lines = read_anchored(frame, font, anchor, dimensions)
    if marker_row(lines, PASS) is not None or marker_row(lines, FAIL) is not None:
        raise pa.AcceptanceError('unexpected COM result before key press')
    row = marker_row(lines, READY)
    if row is None:
        raise ValueError('COM ready output not yet visible')
    return {'ready_row':row, 'ready':READY}


def pass_check(frame, font, anchor, dimensions):
    lines = read_anchored(frame, font, anchor, dimensions)
    if marker_row(lines, FAIL) is not None:
        raise pa.AcceptanceError('COM program reported failure')
    ready, passed = marker_row(lines, READY), marker_row(lines, PASS)
    if ready is None or passed is None or passed < ready:
        raise ValueError('COM pass output not yet visible')
    later = next((i for i in range(passed+1,len(lines)) if 'A:\\>' in lines[i].upper()), None)
    if later is None:
        raise ValueError('DOS prompt after COM not yet visible')
    return {'ready_row':ready, 'pass_row':passed, 'prompt_row':later,
            'ready':READY, 'pass':PASS, 'prompt':'A:\\>', 'whole_board_qualified':False}


def traced_call(events, operation, usage, call, clock=time.monotonic_ns):
    """Retain timing and success even when an input operation raises."""
    start = clock()
    entry = {'operation': operation, 'usage': usage, 'started_monotonic_ns': start}
    events.append(entry)
    try:
        value = call()
        entry['ok'] = True
        return value
    except BaseException:
        entry['ok'] = False
        raise
    finally:
        end = clock()
        entry['finished_monotonic_ns'] = end
        entry['duration_ms'] = round((end - start) / 1_000_000, 3)


def input_status(runner, session_id, events, usage, phase, clock=time.monotonic_ns):
    """Require the same attached input generation before and after each tap."""
    def check():
        status = runner.api.get('/api/v1/session/input')
        if (not isinstance(status, dict) or status.get('state') != 'attached'
                or status.get('ready') is not True or status.get('session_id') != session_id):
            raise pa.AcceptanceError('input attachment changed during HID input')
        return status
    return traced_call(events, phase, usage, check, clock)


def unit(session, state):
    p = session.get('core_package', {})
    if p.get('abi') != {'id': 'fes.computer', 'major': 1, 'minor': 1}:
        raise pa.AcceptanceError('package is not computer ABI 1.1')
    rows = p.get('media_units')
    if not isinstance(rows, list) or len(rows) != 1:
        raise pa.AcceptanceError('expected exactly one media unit')
    u = rows[0]
    if (u.get('unit') != 0 or u.get('interface') != {'id': 'fes.media.pcxt-floppy', 'major': 1, 'minor': 0}
        or u.get('min_bytes') != FLOPPY_SIZE or u.get('max_bytes') != FLOPPY_SIZE
        or u.get('chunk_bytes') != 512 or u.get('state') != state):
        raise pa.AcceptanceError('unexpected PCXT media unit identity, limits or state')


class VideoCapture:
    """One continuous FFV1 capture; its progress log timestamps are host-side."""
    def __init__(self, device, evidence, startup_timeout=30):
        self.path = evidence/'game-full.mkv'
        self.progress_path = evidence/'game-progress.jsonl'
        self.stderr_path = evidence/'game-video.stderr'
        self.ready = threading.Event()
        self.started_ns = time.monotonic_ns()
        self.stderr = self.progress = self.proc = self.reader = None
        try:
            self.stderr = self.stderr_path.open('x')
            self.progress = self.progress_path.open('x')
            self.proc = subprocess.Popen(['ffmpeg','-hide_banner','-loglevel','warning',
                '-f','v4l2','-input_format','yuyv422','-video_size','1920x1080',
                '-framerate','30','-i',device,'-map','0:v:0','-an','-c:v','ffv1',
                '-level','3','-fps_mode','passthrough','-t','240','-progress','pipe:1',
                '-stats_period','0.5','-n',str(self.path)], stdout=subprocess.PIPE,
                stderr=self.stderr,text=True,bufsize=1)
            def reader():
                for line in self.proc.stdout:
                    self.progress.write(json.dumps({'monotonic_ns':time.monotonic_ns(), 'line':line.rstrip()})+'\n')
                    self.progress.flush()
                    if line.startswith('frame='):
                        try:
                            if int(line.split('=',1)[1]) > 0: self.ready.set()
                        except ValueError: pass
            self.reader=threading.Thread(target=reader,daemon=True); self.reader.start()
            if not self.ready.wait(startup_timeout) or self.proc.poll() is not None:
                raise pa.AcceptanceError('continuous capture did not start')
            self.first_progress_ns = time.monotonic_ns()
        except BaseException as error:
            try: self.stop(require_success=False)
            except BaseException as cleanup: error.add_note('recorder cleanup failed: '+repr(cleanup))
            raise

    def stop(self, require_success=True):
        # A second termination must not strand a recorder holding the device.
        prior_handler = signal.signal(signal.SIGTERM, signal.SIG_IGN)
        try:
            was_running = self.proc is not None and self.proc.poll() is None
            if was_running:
                self.proc.send_signal(signal.SIGINT)
                try: self.proc.wait(timeout=30)
                except subprocess.TimeoutExpired:
                    self.proc.kill(); self.proc.wait(timeout=10)
            if self.reader is not None and self.reader.ident is not None:
                self.reader.join(timeout=5)
                if self.reader.is_alive():
                    raise pa.AcceptanceError('recorder progress reader did not stop')
            if self.proc is not None and self.proc.stdout is not None:
                self.proc.stdout.close()
            if self.stderr is not None: self.stderr.close()
            if self.progress is not None: self.progress.close()
            if require_success and not was_running:
                raise pa.AcceptanceError('continuous capture stopped before recording finished')
            if require_success and self.proc.returncode not in (0,255):
                raise pa.AcceptanceError('continuous capture exited with error')
            if require_success and (not self.path.is_file() or self.path.stat().st_size == 0):
                raise pa.AcceptanceError('continuous capture is missing or empty')
        finally:
            signal.signal(signal.SIGTERM, prior_handler)


def check_video(path):
    result=subprocess.run(['ffprobe','-v','error','-count_frames','-select_streams','v:0',
        '-show_entries','stream=width,height,codec_name,nb_read_frames,r_frame_rate:format=duration',
        '-of','json',str(path)],check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=120)
    doc=json.loads(result.stdout); streams=doc.get('streams',[])
    if len(streams)!=1 or streams[0].get('width')!=1920 or streams[0].get('height')!=1080 or streams[0].get('codec_name')!='ffv1' or streams[0].get('r_frame_rate')!='30/1' or int(streams[0].get('nb_read_frames','0'))<90:
        raise pa.AcceptanceError('continuous capture format or frame count differs')
    duration=float(doc.get('format',{}).get('duration','0'))
    if not math.isfinite(duration) or duration <= 0: raise pa.AcceptanceError('continuous capture duration is missing')
    return {**streams[0], 'duration_seconds':duration}


def check_video_coverage(metadata, progress_path, first_progress_ns, stop_ns, last_input_ns):
    # ffmpeg progress timestamps are received on the host after frames are
    # encoded; they bound coverage but do not timestamp guest key application.
    if not 0 < first_progress_ns <= last_input_ns <= stop_ns:
        raise pa.AcceptanceError('capture/input time order is invalid')
    elapsed=(stop_ns-first_progress_ns)/1e9
    if metadata['duration_seconds'] < elapsed-2 or int(metadata['nb_read_frames']) < max(90, int(max(0,elapsed-2)*27)):
        raise pa.AcceptanceError('video duration or decoded frames do not cover the recorded input interval')
    group={}; completed=[]
    for line in progress_path.read_text().splitlines():
        row=json.loads(line); key,sep,value=row['line'].partition('=')
        if not sep: continue
        group[key]=value
        if key=='progress':
            try:
                frame=int(group['frame']); micros=int(group['out_time_us'])
                if frame>0 and micros>=0: completed.append((frame,micros,row['monotonic_ns'],value))
            except (KeyError,ValueError): pass
            group={}
    if len(completed)<2 or completed[-1][3]!='end' or completed[-1][2] < last_input_ns:
        raise pa.AcceptanceError('video progress does not cover the last input')
    if completed[-1][0] != int(metadata['nb_read_frames']):
        raise pa.AcceptanceError('video progress and decoded frame count differ')
    origins=[ns-us*1000 for _,us,ns,_ in completed]
    return {'first_progress_monotonic_ns':first_progress_ns,'last_input_monotonic_ns':last_input_ns,
            'stop_requested_monotonic_ns':stop_ns,'estimated_video_origin_min_ns':min(origins),
            'estimated_video_origin_max_ns':max(origins),
            'note':'progress arrives after encoding; offsets are approximate and do not establish target input timing'}


_INTERRUPTED = 0


def install_signal_handler():
    # Latch signals rather than raising inside Popen, where the child can exist
    # before the caller receives its process handle. The next bounded checkpoint
    # unwinds the recorder while the caller still owns the kit/session.
    def interrupted(signum, _frame):
        global _INTERRUPTED
        _INTERRUPTED = signum
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)


def check_interrupted():
    if _INTERRUPTED:
        raise pa.AcceptanceError('diagnostic interrupted; caller must stop and restore the kit')


def stamp():
    return datetime.now(timezone.utc).isoformat(timespec='seconds')


def check_claim(kit, owner, target_id, lock_fd, status_root=None, queue_locks=None):
    """Verify the latest claim and an already-held per-kit queue lock.

    Lock ownership, target lease and kit restoration belong to the caller;
    this check does not claim or release anything.
    """
    if kit not in ('A', 'B') or not re.fullmatch(r'[A-Za-z0-9_.-]{1,64}', owner):
        raise pa.AcceptanceError('invalid kit or claim owner')
    home = Path.home()
    status_root = Path(status_root) if status_root is not None else home/'kit-status'
    queue_locks = queue_locks or {'A': home/'kit-status/kitA.lock', 'B': home/'tmp/lander-kitB.lock'}
    path = status_root/f'kit{kit}-STATUS.md'
    lines = path.read_text().rstrip().splitlines()
    last = lines[-1] if lines else ''
    claim = re.compile(r'\bKIT '+kit+r' IN USE:\s*'+re.escape(owner)+r'(?=\s|[—:]|$)')
    if not claim.search(last):
        raise pa.AcceptanceError('latest kit status is not the requested owner claim')
    target = re.compile(r'\btarget_id='+re.escape(target_id)+r'(?=\s|[;,—]|$)')
    if not target.search(last):
        raise pa.AcceptanceError('kit claim does not name the exact target ID')
    # Linux fdinfo exposes flock ownership for the open file description itself,
    # including inherited descriptors. A lock held by another process on a
    # separately opened descriptor does not appear here. Never acquire a lock.
    lock_path = Path(queue_locks[kit])
    try:
        held = os.fstat(lock_fd)
        named = lock_path.stat()
        info = Path(f'/proc/self/fdinfo/{lock_fd}').read_text()
    except (OSError, ValueError):
        raise pa.AcceptanceError('inherited kit queue lock descriptor is unavailable') from None
    if (held.st_dev, held.st_ino) != (named.st_dev, named.st_ino):
        raise pa.AcceptanceError('inherited queue lock descriptor names a different file')
    if not re.search(r'^lock:\s*\d+: FLOCK\s+ADVISORY\s+WRITE\s+\d+\s+\S+\s+0 EOF$', info, re.M):
        raise pa.AcceptanceError('inherited queue lock descriptor does not hold an exclusive flock')
    return {'kit': kit, 'owner': owner, 'status_path': str(path), 'status_line': last,
            'queue_lock': str(lock_path), 'queue_lock_fd': lock_fd,
            'lock_device': held.st_dev, 'lock_inode': held.st_ino}



def check_private_config(path, target_id, target_name):
    """Require one explicitly selected target on an isolated input host."""
    path = Path(path)
    if path.is_symlink() or not path.is_file() or path.stat().st_mode & 0o077:
        raise pa.AcceptanceError('private host config must be a non-public file')
    try:
        data = path.read_bytes()
        if len(data) > 1024 * 1024:
            raise ValueError('config too large')
        config = tomllib.loads(data.decode())
    except (OSError, UnicodeError, ValueError, tomllib.TOMLDecodeError):
        raise pa.AcceptanceError('private host config cannot be read or parsed') from None
    targets = config.get('targets')
    if not isinstance(targets, list) or any(not isinstance(t, dict) for t in targets):
        raise pa.AcceptanceError('private host targets are invalid')
    enabled = [t for t in targets if t.get('enabled') is True]
    remote = config.get('remote_input')
    if (len(enabled) != 1 or enabled[0].get('target_id') != target_id
            or enabled[0].get('name') != target_name or config.get('selected_target') != target_name
            or not isinstance(remote, dict) or remote.get('enabled') is not True):
        raise pa.AcceptanceError('private host must select only the owned target with remote input enabled')
    return {'config_sha256': hashlib.sha256(data).hexdigest(), 'single_target': target_name,
            'remote_input_enabled': True}


def check_live_settings(value, target_id, target_name):
    """Do not trust a config file alone as proof of the live host's targets."""
    if not isinstance(value, dict) or not isinstance(value.get('targets'), list):
        raise pa.AcceptanceError('live host target settings are unavailable')
    targets = value['targets']
    if any(not isinstance(t, dict) for t in targets):
        raise pa.AcceptanceError('live host target settings are invalid')
    enabled = [t for t in targets if t.get('enabled') is True]
    if (len(enabled) != 1 or enabled[0].get('target_id') != target_id
            or enabled[0].get('name') != target_name or value.get('selected_target') != target_name):
        raise pa.AcceptanceError('live host does not select only the owned target')


def verify_disk(path, scenario):
    spec = SCENARIOS[scenario]
    path = Path(path)
    if not path.is_file() or path.is_symlink() or path.stat().st_size != FLOPPY_SIZE:
        raise pa.AcceptanceError('floppy must be a regular 360 KiB file')
    data = path.read_bytes()
    if hashlib.sha256(data).hexdigest() != spec['floppy']:
        raise pa.AcceptanceError('acceptance floppy image identity differs')
    env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', TZ='UTC', LC_ALL='C')
    program = subprocess.run(['mcopy', '-i', str(path), '::'+spec['name'], '-'],
                             check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                             timeout=10, env=env).stdout
    if len(program) != spec['size'] or hashlib.sha256(program).hexdigest() != spec['program']:
        raise pa.AcceptanceError('acceptance executable identity differs')
    return {'floppy_sha256': spec['floppy'], 'executable_sha256': spec['program'],
            'executable': spec['name'], 'bytes': len(data)}


def canonical_package_id(archive, expected_sha):
    """Use misteross's canonical parser on a private snapshot of the bytes."""
    import tempfile
    root = Path(__file__).resolve().parents[1]
    parser_root = root/'sources/misteross'
    program = '''import json,sys
from pathlib import Path
sys.path.insert(0,sys.argv[1])
from scripts.core_package import read_package
p=read_package(Path(sys.argv[2]))
print(json.dumps({'package_id':p.package_id}))
'''
    with tempfile.TemporaryDirectory(prefix='pcxt-app-package-') as temp:
        path = Path(temp)/'core.fcore'
        path.write_bytes(archive)
        result = subprocess.run([sys.executable, '-I', '-B', '-X',
                                 'pycache_prefix='+str(Path(temp)/'empty-pycache'),
                                 '-c', program, str(parser_root), str(path)],
                                check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                text=True, timeout=30)
        value = json.loads(result.stdout)
    if hashlib.sha256(archive).hexdigest() != expected_sha or not isinstance(value, dict):
        raise pa.AcceptanceError('package archive identity differs')
    return pa._require_package_id(value.get('package_id'), 'canonical package ID')


def source_provenance(args, font):
    """Pin independent host, software and local diagnostic inputs in the evidence."""
    host_raw, host_sha = pa._read_archive(args.host_receipt, maximum=1024*1024)
    software_raw, software_sha = pa._read_archive(args.software_receipt, maximum=1024*1024)
    if host_sha != args.host_receipt_sha256 or software_sha != args.software_receipt_sha256:
        raise pa.AcceptanceError('host or software receipt identity differs')
    host = json.loads(host_raw)
    software = json.loads(software_raw)
    if not isinstance(host, dict) or not isinstance(software, dict):
        raise pa.AcceptanceError('host or software receipt is malformed')
    host_source = host.get('fes_revision', host.get('source_commit'))
    software_source = software.get('source_commit')
    if not all(isinstance(s, str) and re.fullmatch(r'[0-9a-f]{40}', s)
               for s in (host_source, software_source)):
        raise pa.AcceptanceError('host or software receipt lacks a full source commit')
    host_files = host.get('files', host.get('sha256'))
    _, binary_sha = pa._read_archive(args.host_binary)
    if not isinstance(host_files, dict) or host_files.get('fogcast-api') != binary_sha:
        raise pa.AcceptanceError('host binary does not match host receipt')
    if (not isinstance(software.get('bundle'), dict)
            or software['bundle'].get('id') != args.bundle_id
            or software.get('base_image_sha256') != args.image_sha256):
        raise pa.AcceptanceError('software receipt does not match bundle and base image')
    root = Path(__file__).resolve().parents[1]
    paths = ['scripts/pcxt_dos_application_acceptance.py',
             'scripts/pcxt_gameplay_capture.py', 'scripts/pcxt_gameplay_probe.py',
             'scripts/pcxt_game_audio_capture.py',
             'scripts/pcxt_capture_fanout.py', 'scripts/pcxt_v4l2_capture.c',
             'scripts/pcxt_digger_readiness.py', 'scripts/package_acceptance.py',
             'scripts/diagnostic_identity.py', 'scripts/input_diagnostic.py',
             'scripts/pcxt_dos_hil_acceptance.py', 'sources/misteross/scripts/core_package.py',
             'sources/misteross/scripts/rfc3986_validator.py',
             'sources/misteross/cores/fes-pcxt/firmware/hardware-dos.json',
             'sources/misteross/cores/fes-pcxt/sim/bios.lock']
    hashes = {name: hashlib.sha256((root/name).read_bytes()).hexdigest() for name in paths}
    bios_config = json.loads((root/paths[-2]).read_text())['bios']
    bios_path = root/'sources/misteross/cores/fes-pcxt'/bios_config['file']
    bios_sha = hashlib.sha256(bios_path.read_bytes()).hexdigest()
    if bios_sha != bios_config['sha256']:
        raise pa.AcceptanceError('BIOS changed after font extraction')
    return ({'package_source_commit': args.source_commit,
             'host': {'source_commit': host_source, 'receipt_file': 'host-receipt.json',
                      'receipt_sha256': host_sha, 'fogcast_api_sha256': binary_sha},
             'software': {'source_commit': software_source, 'receipt_file': 'software-receipt.json',
                          'receipt_sha256': software_sha, 'bundle_id': args.bundle_id,
                          'base_image_sha256': args.image_sha256},
             'diagnostic_file_sha256': hashes, 'bios_sha256': bios_sha,
             'extracted_font_sha256': hashlib.sha256(font).hexdigest()}, host_raw, software_raw)


def _validate_active_once(runner, expected, args, disk_sha, *, session_guard=None):
    check_interrupted()
    claim = check_claim(args.kit, args.owner, args.target_id, args.queue_lock_fd)
    # Re-read the pinned receipt identities and host executable before every
    # input/capture; a previously validated receipt must not be swapped later.
    source_provenance(args, visual.load_font())
    check_private_config(args.config, args.target_id, expected.target_name)
    check_live_settings(runner.api.get('/api/v1/library/settings'), args.target_id, expected.target_name)
    health = runner.api.get('/api/v1/health')
    try:
        pa.observe_health(health, args.target_id, args.bundle_id, args.image_sha256, require_ready=False)
    except ValueError as exc:
        raise pa.AcceptanceError(str(exc)) from exc
    target = health.get('target', {})
    connection, software = target.get('connection', {}), target.get('software', {})
    selected = software.get('selected')
    if (connection.get('boot_id') != args.expected_boot or connection.get('state') not in ('ready', 'active')
            or (selected not in (None, '') and selected != args.bundle_id)):
        # Retain only identity and state fields; do not weaken the guard or
        # expose the host config/token in the failure receipt.
        observed = {'observed_boot_id': connection.get('boot_id'), 'expected_boot_id': args.expected_boot,
                    'observed_connection_state': connection.get('state'),
                    'allowed_connection_states': ['ready', 'active'],
                    'observed_selected_bundle': software.get('selected'), 'expected_selected_bundle': args.bundle_id,
                    'observed_active_bundle': software.get('active_bundle'),
                    'observed_monotonic_ns': time.monotonic_ns()}
        raise pa.AcceptanceError('target boot, connection or selected software changed: ' + json.dumps(observed, sort_keys=True))
    current = runner.session()
    if runner.session_identity(current, 'foreground application diagnostic') != expected:
        raise pa.AcceptanceError('owned session identity or foreground target changed')
    target_current = runner.session(expected.target_name)
    if runner.session_identity(target_current, 'owned application diagnostic') != expected:
        raise pa.AcceptanceError('owned session identity or foreground target changed')
    package = current.get('core_package', {})
    if package.get('core_id') != 'fes.pcxt-dos':
        raise pa.AcceptanceError('active session is not the PC/XT DOS core')
    unit(current, 'ready')
    interfaces = package.get('active_interfaces', [])
    if not any(isinstance(i, dict) and i.get('id') == 'fes.keyboard.hid'
               and i.get('major') == 1 and i.get('minor') == 0 for i in interfaces):
        raise pa.AcceptanceError('keyboard HID interface is not active')
    entry = runner.validate_entry(runner.read_entry(expected.game_id), expected.game_id, 'active selection')
    if entry.get('media_role') != 'disk' or entry.get('media_id') != disk_sha:
        raise pa.AcceptanceError('active library entry does not select the expected floppy')
    # Extension checks use exactly these already identity-checked snapshots;
    # failure still precedes the final ownership check and any target input.
    if session_guard is not None:
        session_guard(current, target_current)
    # Recheck ownership after every health and session observation. In particular,
    # an incomplete response must not allow a stale claim into the next poll.
    claim = check_claim(args.kit, args.owner, args.target_id, args.queue_lock_fd)
    return claim, selected == args.bundle_id


class _DeadlineReadOnlyAPI:
    """Limit every GET to the remaining phase time and forbid mutations."""
    def __init__(self, api, deadline):
        self.api, self.deadline = api, deadline

    def get(self, *args, **kwargs):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise pa.AcceptanceError('selected software observation exceeded phase deadline')
        old_timeout = self.api.timeout
        try:
            self.api.timeout = min(old_timeout, remaining)
            result = self.api.get(*args, **kwargs)
        finally:
            self.api.timeout = old_timeout
        if time.monotonic() >= self.deadline:
            raise pa.AcceptanceError('selected software observation exceeded phase deadline')
        return result


def _observe_until(runner, expected, args, disk_sha, deadline):
    original_api = runner.api
    runner.api = _DeadlineReadOnlyAPI(original_api, deadline)
    try:
        while True:
            if time.monotonic() >= deadline:
                raise pa.AcceptanceError('selected software remained unavailable before phase deadline')
            claim, complete = _validate_active_once(runner, expected, args, disk_sha)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise pa.AcceptanceError('selected software observation exceeded phase deadline')
            if complete:
                return claim
            check_interrupted()
            time.sleep(min(0.2, remaining))
    finally:
        runner.api = original_api


_OBSERVER_GUARD_FD = None


class ObservationDeadlineExceeded(pa.AcceptanceError):
    """A read-only observation timed out; caller must verify cleanup."""


def _wait_observer_guard(fd, *, timeout=2.0):
    """Wait for the observer and any owned source supervisors to close."""
    try:
        deadline = time.monotonic() + timeout
        pending = False
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([fd], [], [], remaining)[0]:
                raise pa.AcceptanceError('observer source cleanup unverified; retain kit claim')
            data = os.read(fd, 4096)
            if not data:
                if pending:
                    raise pa.AcceptanceError('observer source cleanup unverified; retain kit claim')
                return
            for value in data:
                if value == ord('S') and not pending:
                    pending = True
                elif value == ord('C') and pending:
                    pending = False
                else:
                    raise pa.AcceptanceError('invalid observer source cleanup evidence; retain kit claim')
    finally:
        os.close(fd)


def _kill_and_reap_observer(pid, *, timeout=2.0):
    """Do not wait forever for a child stuck in uninterruptible I/O."""
    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    deadline = time.monotonic() + timeout
    while True:
        done, _ = os.waitpid(pid, os.WNOHANG)
        if done:
            return
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise pa.AcceptanceError('observer cleanup unverified; retain kit claim')
        time.sleep(min(0.01, remaining))


def _decode_observer_result(raw):
    if not raw or len(raw) > 8192:
        raise pa.AcceptanceError('observer returned no valid result')
    try:
        return json.loads(raw)
    except (ValueError, UnicodeDecodeError):
        raise pa.AcceptanceError('observer returned invalid result') from None


def _hard_deadline_observation(observe, seconds=5.0):
    """Reap our read-only observer before accepting its result or returning."""
    deadline = time.monotonic() + seconds
    parent, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET)
    guard_read, guard_write = os.pipe2(os.O_CLOEXEC)
    parent_pid = os.getpid()
    libc = ctypes.CDLL(None, use_errno=True)
    try:
        pid = os.fork()
    except BaseException:
        parent.close(); child.close()
        os.close(guard_read); os.close(guard_write)
        raise
    if pid == 0:
        global _OBSERVER_GUARD_FD
        os.close(guard_read)
        _OBSERVER_GUARD_FD = guard_write
        parent.close()
        # Linux delivers SIGKILL if the diagnostic parent dies unexpectedly;
        # close the race where it died before this setting was installed.
        if libc.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != parent_pid:
            os._exit(1)
        try:
            result = {'ok': True, 'claim': observe(deadline)}
        except BaseException as exc:
            result = {'ok': False, 'error': str(exc)[:2048]}
        try:
            message = json.dumps(result).encode()
            if len(message) > 8192:
                message = b'{"ok":false,"error":"observation result too large"}'
            child.send(message)
        except BaseException:
            pass
        finally:
            child.close()
            os._exit(0)
    child.close(); os.close(guard_write)
    reaped = False
    result = None
    def timeout_error():
        nonlocal result
        if result is None:
            try:
                raw = parent.recv(8193, socket.MSG_DONTWAIT)
            except BlockingIOError:
                raw = None
            if raw == b'':
                return pa.AcceptanceError('observer exited without a result before the hard phase deadline')
            if raw is not None:
                try:
                    result = _decode_observer_result(raw)
                except pa.AcceptanceError:
                    return pa.AcceptanceError('invalid observer result before the hard phase deadline')
        # Do not downgrade an explicit or malformed observer failure merely
        # because the child failed to exit before its deadline.
        if result is not None and (not isinstance(result, dict) or result.get('ok') is not True
                                   or not isinstance(result.get('claim'), dict)):
            return pa.AcceptanceError('observer failed before the hard phase deadline')
        return ObservationDeadlineExceeded('selected software observation exceeded hard phase deadline')
    try:
        while True:
            check_interrupted()
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise timeout_error()
            if result is None:
                readable, _, _ = select.select([parent], [], [], min(remaining, 0.05))
                if readable:
                    result = _decode_observer_result(parent.recv(8193))
            done, status = os.waitpid(pid, os.WNOHANG)
            if done:
                reaped = True
                if result is None:
                    # The child can send its packet just after select times out
                    # and exit before waitpid runs. Its packet remains queued
                    # after close, so drain it once without blocking.
                    try:
                        raw = parent.recv(8193, socket.MSG_DONTWAIT)
                    except BlockingIOError:
                        raw = None
                    if raw:
                        result = _decode_observer_result(raw)
                if time.monotonic() >= deadline:
                    raise timeout_error()
                if status != 0 or not isinstance(result, dict) or result.get('ok') not in (True, False):
                    raise pa.AcceptanceError('observer failed without a complete result')
                if not result['ok']:
                    raise pa.AcceptanceError(result.get('error', 'observer failed'))
                if not isinstance(result.get('claim'), dict):
                    raise pa.AcceptanceError('observer did not confirm the claim')
                return result['claim']
            if result is not None:
                time.sleep(min(0.01, remaining))
    finally:
        parent.close()
        original_error = sys.exception()
        cleanup_error = None
        if not reaped:
            try:
                _kill_and_reap_observer(pid)
            except BaseException as exc:
                cleanup_error = exc
        try:
            _wait_observer_guard(guard_read, timeout=2.0 if original_error else min(2.0, max(0, deadline-time.monotonic())))
        except BaseException as exc:
            cleanup_error = cleanup_error or exc
        if cleanup_error is not None:
            if original_error is None:
                cleanup_error.source_cleanup_incomplete = True
                raise cleanup_error
            original_error.add_note('observer cleanup unverified; retain kit claim')
            original_error.source_cleanup_incomplete = True
        if original_error is None and time.monotonic() >= deadline:
            raise ObservationDeadlineExceeded('selected software observation exceeded hard phase deadline')


def validate_active(runner, expected, args, disk_sha):
    # The child is read-only and is killed and reaped on any parent exit. The
    # parent performs no input or capture unless a complete fresh observation
    # returns before the hard deadline.
    return _hard_deadline_observation(
        lambda deadline: _observe_until(runner, expected, args, disk_sha, deadline))


class PrestartedObserver:
    """One read-only owner observer forked before game capture threads exist."""
    def __init__(self, observe, seconds=5.0):
        if threading.active_count() != 1:
            raise pa.AcceptanceError('game observer must start before capture threads')
        self.seconds = seconds
        parent, child = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET)
        guard_read, guard_write = os.pipe2(os.O_CLOEXEC)
        parent_pid = os.getpid()
        libc = ctypes.CDLL(None, use_errno=True)
        try:
            pid = os.fork()
        except BaseException:
            parent.close(); child.close()
            os.close(guard_read); os.close(guard_write)
            raise
        if pid == 0:
            global _OBSERVER_GUARD_FD
            os.close(guard_read)
            _OBSERVER_GUARD_FD = guard_write
            parent.close()
            if libc.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != parent_pid:
                os._exit(1)
            try:
                while True:
                    raw = child.recv(1024)
                    if not raw:
                        break
                    try:
                        deadline = json.loads(raw)['deadline']
                        if not isinstance(deadline, (int,float)) or not math.isfinite(deadline):
                            raise ValueError('invalid observer deadline')
                        value = {'ok':True, 'claim':observe(deadline)}
                    except BaseException as exc:
                        value = {'ok':False, 'error':str(exc)[:2048]}
                    message = json.dumps(value).encode()
                    if len(message) > 8192:
                        message = b'{"ok":false,"error":"observation result too large"}'
                    child.send(message)
            except BaseException:
                pass
            finally:
                child.close()
                os._exit(0)
        child.close(); os.close(guard_write)
        self.socket, self.pid, self.reaped, self.guard_read = parent, pid, False, guard_read

    def check(self):
        if self.reaped:
            raise pa.AcceptanceError('game ownership observer is unavailable')
        deadline = time.monotonic()+self.seconds
        try:
            self.socket.settimeout(self.seconds)
            self.socket.send(json.dumps({'deadline':deadline}).encode())
            while True:
                check_interrupted()
                remaining = deadline-time.monotonic()
                if remaining <= 0:
                    raise pa.AcceptanceError('game ownership observation exceeded hard phase deadline')
                readable, _, _ = select.select([self.socket], [], [], min(.05,remaining))
                if readable:
                    result = _decode_observer_result(self.socket.recv(8193))
                    if time.monotonic() >= deadline:
                        raise pa.AcceptanceError('game ownership observation exceeded hard phase deadline')
                    if not isinstance(result,dict) or result.get('ok') not in (True,False):
                        raise pa.AcceptanceError('game observer returned an invalid result')
                    if not result['ok']:
                        raise pa.AcceptanceError(result.get('error','game observer failed'))
                    if not isinstance(result.get('claim'),dict):
                        raise pa.AcceptanceError('game observer did not confirm the claim')
                    return result['claim']
                done, _ = os.waitpid(self.pid, os.WNOHANG)
                if done:
                    self.reaped = True
                    raise pa.AcceptanceError('game ownership observer exited unexpectedly')
        except BaseException as error:
            try:
                self.close(force=True)
            except BaseException as cleanup:
                error.source_cleanup_incomplete = True
                error.add_note('game observer cleanup unverified: '+repr(cleanup))
            raise

    def close(self, force=False):
        self.socket.close()
        try:
            if self.reaped:
                return
            if not force:
                end = time.monotonic()+2
                while time.monotonic() < end:
                    done, status = os.waitpid(self.pid, os.WNOHANG)
                    if done:
                        self.reaped = True
                        if status != 0:
                            raise pa.AcceptanceError('game ownership observer did not stop cleanly')
                        return
                    time.sleep(.01)
            _kill_and_reap_observer(self.pid)
            self.reaped = True
            if not force:
                raise pa.AcceptanceError('game ownership observer did not stop cleanly')
        finally:
            if self.guard_read is not None:
                fd, self.guard_read = self.guard_read, None
                _wait_observer_guard(fd)


def capture_owned(runner, owned, args, disk_sha, path, phase):
    validate_active(runner, owned, args, disk_sha)
    result = capture(args.capture_device, path, phase)
    validate_active(runner, owned, args, disk_sha)
    return result


def type_keys(runner, owned, args, disk_sha, input_session_id, events, usages, *, dispatch_guard=None,
              capture_check=None, active_check=None):
    """One host-owned press/release operation per key; never retry uncertainty."""
    for usage in usages:
        (active_check or validate_active)(runner, owned, args, disk_sha)
        if capture_check is not None: capture_check()
        input_status(runner, input_session_id, events, usage, 'before tap input')
        if capture_check is not None: capture_check()
        def tap():
            response = runner.api.post_json('/api/v1/session/input/tap', {
                'session_id': input_session_id,
                'event': {'Player': 0, 'Device': 0, 'Kind': 0, 'Action': 1,
                          'Code': 0x1000 + usage, 'Value': 0}})
            events[-1]['response'] = response
            duration = response.get('host_duration_ms') if isinstance(response, dict) else None
            if (not isinstance(response, dict) or response.get('ok') is not True
                    or isinstance(duration, bool) or not isinstance(duration, (int, float))
                    or not math.isfinite(duration) or not 0 <= duration <= 400):
                raise pa.AcceptanceError('host tap receipt is missing, malformed, or out of bound')
        old_timeout = runner.api.timeout
        runner.api.timeout = min(old_timeout, 5.0)
        event_count = len(events)
        try:
            def send():
                return traced_call(events, 'host tap request', usage, tap)
            if dispatch_guard is None:
                send()
            else:
                dispatch_guard.dispatch(usage, send)
        finally:
            if len(events) > event_count:
                events[-1]['socket_timeout_seconds'] = runner.api.timeout
            runner.api.timeout = old_timeout
        input_status(runner, input_session_id, events, usage, 'after tap input')
        (active_check or validate_active)(runner, owned, args, disk_sha)
        time.sleep(0.06)


def wait_owned(runner, owned, args, disk_sha, seconds, video, *, capture_check=None, active_check=None):
    print('PC/XT game capture running; caller retains kit ownership', flush=True)
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if video.proc.poll() is not None:
            raise pa.AcceptanceError('continuous capture stopped during gameplay')
        if capture_check is not None: capture_check()
        (active_check or validate_active)(runner, owned, args, disk_sha)
        time.sleep(min(5, max(0, deadline-time.monotonic())))


def run_com(runner, owned, args, rec, input_session_id, font):
    disk_sha = SCENARIOS['com']['floppy']
    before, info = capture_owned(runner, owned, args, disk_sha,
                                 args.evidence/'before.png', 'before FESCOM input')
    rec['captures'].append(info)
    anchor = visual.find_prompt(*before, font)
    ensure_fresh_before(before, font, anchor)
    type_keys(runner, owned, args, disk_sha, input_session_id, rec['hid_events'], COM_COMMAND)
    deadline = time.monotonic() + 60
    while True:
        index = len(rec['captures'])
        ready, info = capture_owned(runner, owned, args, disk_sha,
                                    args.evidence/f'ready-{index:03d}.png', 'after FESCOM Enter')
        rec['captures'].append(info)
        try:
            proof = ready_check(ready, font, anchor, before[:2])
        except ValueError:
            if time.monotonic() >= deadline:
                raise pa.AcceptanceError('COM READY not observed')
            time.sleep(3)
        else:
            rec['ready_proof'] = {'file': info['file'], **proof}
            rec['ready_capture_change'] = change(before, ready)
            break
    type_keys(runner, owned, args, disk_sha, input_session_id, rec['hid_events'], (0x0e,))
    deadline = time.monotonic() + 60
    while True:
        index = len(rec['captures'])
        after, info = capture_owned(runner, owned, args, disk_sha,
                                    args.evidence/f'pass-{index:03d}.png', 'after K')
        rec['captures'].append(info)
        try:
            proof = pass_check(after, font, anchor, before[:2])
        except ValueError:
            if time.monotonic() >= deadline:
                raise pa.AcceptanceError('COM PASS and later DOS prompt not observed')
            time.sleep(3)
        else:
            rec['com_proof'] = {'file': info['file'], **proof}
            rec['pass_capture_change'] = change(ready, after)
            break
    rec['classification'] = 'owned-session COM execution diagnostic passed'


def check_source_coverage(path, first_input_ns, last_input_ns):
    rows = [json.loads(line) for line in path.read_text().splitlines()]
    if not rows or not 0 < first_input_ns <= last_input_ns:
        raise pa.AcceptanceError('game capture or input interval is missing')
    if not (rows[0]['arrival_ns'] <= first_input_ns <= last_input_ns <= rows[-1]['arrival_ns']):
        raise pa.AcceptanceError('game capture does not cover the host input interval')
    return {'first_sequence': rows[0]['sequence'], 'last_sequence': rows[-1]['sequence'],
            'first_arrival_ns': rows[0]['arrival_ns'], 'last_arrival_ns': rows[-1]['arrival_ns'],
            'note': 'host and V4L2 timestamps do not establish guest-input or upstream video timing'}


def run_game(runner, owned, args, rec, input_session_id, font, *, probe=None, video_factory=None,
             audio_factory=None, observer_factory=None):
    if not isinstance(probe, gameplay.PlaybackProbe) or video_factory is None or audio_factory is None:
        raise pa.AcceptanceError('game requires owned video, audio and playback probe; no game keys sent')
    if threading.active_count() != 1:
        raise pa.AcceptanceError('game diagnostics must start before capture threads; no game keys sent')
    disk_sha = SCENARIOS['game']['floppy']
    before, info = capture_owned(runner, owned, args, disk_sha,
                                 args.evidence/'before.png', 'before Digger input')
    rec['captures'].append(info)
    if prompt(before, font) is None:
        raise pa.AcceptanceError('DOS prompt not observed before the game')
    observer_factory = observer_factory or PrestartedObserver
    observer = observer_factory(lambda deadline: _observe_until(runner, owned, args, disk_sha, deadline))
    def game_active(*_):
        return observer.check()
    try:
        video = video_factory(args.capture_device, args.evidence, probe)
    except BaseException as error:
        try:
            observer.close()
        except BaseException as cleanup:
            error.source_cleanup_incomplete = True
            error.add_note('game observer cleanup failed: '+repr(cleanup))
        raise
    if getattr(video, 'observer_guard', None) is not probe:
        mismatch = pa.AcceptanceError('game recorder does not feed the playback probe; no game keys sent')
        try:
            video.stop()
        except BaseException as cleanup:
            if getattr(cleanup, 'source_cleanup_incomplete', False):
                mismatch.source_cleanup_incomplete = True
            mismatch.add_note('recorder cleanup failed: '+repr(cleanup))
        try:
            observer.close()
        except BaseException as cleanup:
            mismatch.source_cleanup_incomplete = True
            mismatch.add_note('game observer cleanup failed: '+repr(cleanup))
        raise mismatch
    rec['video_start'] = {'requested_monotonic_ns': video.started_ns,
                          'first_progress_observed_monotonic_ns': video.first_progress_ns,
                          'frame_converter':video.converter_identity,
                          'note': 'host timestamps are not exact target capture times'}
    error = None
    audio = None
    audio_started = False
    try:
        audio = audio_factory(args.evidence, args.kit)
        audio.start()
        audio_started = True
        if audio.mapping.get('hil_video') != args.capture_device:
            raise pa.AcceptanceError('audio capture mapping does not match the selected video path')
        def check_recorders():
            if video.proc.poll() is not None or probe.fault.is_set():
                raise pa.AcceptanceError('game capture or observation failed')
            audio.check()
        check_interrupted()
        rec['game_command_started_monotonic_ns'] = time.monotonic_ns()
        type_keys(runner, owned, args, disk_sha, input_session_id, rec['hid_events'], GAME_COMMAND,
                  capture_check=check_recorders, active_check=game_active)
        wait_owned(runner, owned, args, disk_sha, 30, video, capture_check=check_recorders, active_check=game_active)
        rec['title_start_monotonic_ns'] = time.monotonic_ns()
        type_keys(runner, owned, args, disk_sha, input_session_id, rec['hid_events'], (0x28,),
                  capture_check=check_recorders, active_check=game_active)
        probe.arm(time.monotonic_ns())
        deadline = time.monotonic()+30
        next_claim = time.monotonic()
        while True:
            check_interrupted()
            if video.proc.poll() is not None or probe.fault.is_set() or audio.proc.poll() is not None:
                raise pa.AcceptanceError('game video, audio or observation stopped')
            try:
                rec['neutral_observation'] = probe.snapshot(require_player=True)
                break
            except gameplay.ProbeError:
                if time.monotonic() >= deadline:
                    raise pa.AcceptanceError('active game observation was not available') from None
            if time.monotonic() >= next_claim:
                check_recorders()
                game_active()
                next_claim = time.monotonic()+3
            time.sleep(.05)
        rec['neutral_started_monotonic_ns'] = time.monotonic_ns()
        time.sleep(.5)
        for direction, usage in (('left',0x50), ('right',0x4f), ('left',0x50)):
            interval = {'direction':direction, 'started_monotonic_ns':time.monotonic_ns()}
            rec.setdefault('direction_intervals', []).append(interval)
            type_keys(runner, owned, args, disk_sha, input_session_id, rec['hid_events'], (usage,),
                      dispatch_guard=probe, capture_check=check_recorders, active_check=game_active)
            interval['finished_monotonic_ns'] = time.monotonic_ns()
            time.sleep(1)
        rec['direction_observations'] = probe.sent
        for phase in range(3):
            toggle = {'index':phase+1, 'started_monotonic_ns':time.monotonic_ns()}
            rec.setdefault('f9_toggles', []).append(toggle)
            type_keys(runner, owned, args, disk_sha, input_session_id, rec['hid_events'], (0x42,),
                      capture_check=check_recorders, active_check=game_active)
            toggle['finished_monotonic_ns'] = time.monotonic_ns()
            toggle['observation_started_monotonic_ns'] = time.monotonic_ns()
            wait_owned(runner, owned, args, disk_sha, 6, video, capture_check=check_recorders, active_check=game_active)
            toggle['observation_finished_monotonic_ns'] = time.monotonic_ns()
        audio.wait_advanced_after(rec['f9_toggles'][-1]['observation_finished_monotonic_ns'])
    except BaseException as exc:
        error = exc
        raise
    finally:
        if audio_started:
            try:
                rec['audio_capture'] = audio.stop(phases=rec.get('f9_toggles') if error is None else None)
            except BaseException as cleanup:
                if error is None:
                    error = cleanup
                else:
                    if getattr(cleanup, 'audio_cleanup_incomplete', False):
                        error.audio_cleanup_incomplete = True
                    error.add_note('audio cleanup failed: '+repr(cleanup))
        rec['video_stop_requested_monotonic_ns'] = time.monotonic_ns()
        try:
            if error is not None:
                skip = getattr(video, 'skip_failed_recording_verification', None)
                if callable(skip):
                    skip()
                    rec['failed_video_verification'] = 'not performed; diagnostic had already failed'
            video.stop()
        except BaseException as cleanup:
            if error is None:
                error = cleanup
            else:
                if getattr(cleanup, 'source_cleanup_incomplete', False):
                    error.source_cleanup_incomplete = True
                error.add_note('recorder cleanup failed: '+repr(cleanup))
        try:
            observer.close()
        except BaseException as cleanup:
            cleanup.source_cleanup_incomplete = True
            if error is None:
                error = cleanup
            else:
                error.source_cleanup_incomplete = True
                error.add_note('game observer cleanup failed: '+repr(cleanup))
        if error is not None and sys.exception() is None:
            raise error
    check_interrupted()
    metadata = check_video(video.path)
    coverage = check_source_coverage(video.progress_path, rec['game_command_started_monotonic_ns'],
                                    rec['f9_toggles'][-1]['observation_finished_monotonic_ns'])
    with video.path.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    with video.progress_path.open('rb') as stream:
        frame_metadata_sha = hashlib.file_digest(stream, 'sha256').hexdigest()
    rec['game_video'] = {'file':video.path.name, 'sha256':digest, 'metadata':metadata,
                         'coverage':coverage, 'frame_metadata_file':video.progress_path.name,
                         'frame_metadata_sha256':frame_metadata_sha,
                         'classification':'recorded; neutral and left/right/left need independent visual review'}
    rec['classification'] = 'game and audio captured; independent gameplay and speaker review pending'


def parser():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument('scenario', choices=tuple(SCENARIOS))
    ap.add_argument('--kit', choices=('A', 'B'), required=True)
    ap.add_argument('--owner', required=True, help='owner of the existing status-file claim and queue lock')
    ap.add_argument('--queue-lock-fd', type=int, required=True, help='inherited descriptor holding the kit exclusive flock')
    ap.add_argument('--host-receipt', type=Path, required=True)
    ap.add_argument('--host-receipt-sha256', required=True)
    ap.add_argument('--host-binary', type=Path, required=True, help='FogCast API binary named by the host receipt')
    ap.add_argument('--software-receipt', type=Path, required=True)
    ap.add_argument('--software-receipt-sha256', required=True)
    ap.add_argument('--host-api', required=True, help='isolated loopback host API origin')
    ap.add_argument('--config', type=Path, required=True, help='private host TOML with only the owned target enabled')
    ap.add_argument('--owned-session', type=Path, required=True,
                    help='saved caller-owned active session response with attached input')
    ap.add_argument('--archive', type=Path, required=True)
    ap.add_argument('--archive-sha256', required=True)
    ap.add_argument('--rbf-sha256', required=True)
    ap.add_argument('--package-id', required=True)
    ap.add_argument('--source-commit', required=True)
    ap.add_argument('--bundle-id', required=True)
    ap.add_argument('--image-sha256', required=True)
    ap.add_argument('--target-id', required=True)
    ap.add_argument('--expected-boot', required=True)
    ap.add_argument('--disk', type=Path, required=True)
    ap.add_argument('--capture-device', required=True)
    ap.add_argument('--game-template', type=Path, action='append', help='private 32x34 RGB opening-player template')
    ap.add_argument('--game-template-sha256', action='append', help='corresponding private template SHA-256')
    ap.add_argument('--game-max-host-age-ms', type=int, help='maximum local observation age; not a target timing bound')
    ap.add_argument('--evidence', type=Path, required=True, help='new, private output directory')
    ap.add_argument('--execute', action='store_true')
    return ap


def main(argv=None):
    args = parser().parse_args(argv)
    if not args.execute:
        raise pa.AcceptanceError('--execute is required inside an already owned session')
    templates = []
    if args.scenario == 'game':
        if threading.active_count() != 1:
            raise pa.AcceptanceError('game diagnostics must start before capture threads')
        if (not args.game_template or len(args.game_template) != len(args.game_template_sha256 or [])
                or type(args.game_max_host_age_ms) is not int or args.game_max_host_age_ms <= 0):
            raise pa.AcceptanceError('game requires pinned private templates and a local age policy')
        for path, expected in zip(args.game_template, args.game_template_sha256):
            pa._require_sha256(expected, 'game template SHA-256')
            if path.is_symlink() or not path.is_file() or path.stat().st_size != 32*34*3:
                raise pa.AcceptanceError('game template must be a regular file')
            data = path.read_bytes()
            if len(data) != 32*34*3 or hashlib.sha256(data).hexdigest() != expected:
                raise pa.AcceptanceError('game template size or digest differs')
            templates.append(data)
    if not re.fullmatch('[0-9a-f]{40}', args.source_commit):
        raise pa.AcceptanceError('source commit must be a full lowercase SHA')
    for value, name in ((args.archive_sha256, 'archive'), (args.rbf_sha256, 'RBF'),
                        (args.package_id, 'package'), (args.bundle_id, 'bundle'),
                        (args.image_sha256, 'image'), (args.host_receipt_sha256, 'host receipt'),
                        (args.software_receipt_sha256, 'software receipt')):
        pa._require_sha256(value, name+' SHA-256')
    pa._require_string(args.target_id, 'target ID')
    pa._require_string(args.expected_boot, 'boot ID')
    endpoint = urllib.parse.urlsplit(args.host_api)
    if endpoint.scheme != 'http' or endpoint.hostname != '127.0.0.1' or not endpoint.port:
        raise pa.AcceptanceError('host API must be an isolated loopback HTTP listener with an explicit port')
    expected_device = {'A': '/dev/v4l/by-id/fes-shadowcast-hil', 'B': '/dev/v4l/by-id/fes-4kpro-hil'}
    if args.capture_device != expected_device[args.kit]:
        raise pa.AcceptanceError('capture must use the claimed kit stable HIL mapping')
    claim = check_claim(args.kit, args.owner, args.target_id, args.queue_lock_fd)
    archive, archive_sha = pa._read_archive(args.archive)
    if archive_sha != args.archive_sha256:
        raise pa.AcceptanceError('archive digest differs from the expected artifact')
    manifest = inspect_archive(archive, args)
    if canonical_package_id(archive, archive_sha) != args.package_id:
        raise pa.AcceptanceError('canonical package ID differs')
    if manifest.get('abi') != {'id': 'fes.computer', 'major': 1, 'minor': 1} or not any(
        isinstance(i, dict) and i.get('id') == 'fes.keyboard.hid' and i.get('major') == 1
        and i.get('minor') == 0 and i.get('required') for i in manifest.get('interfaces', [])):
        raise pa.AcceptanceError('package does not declare required computer and keyboard contracts')
    disk = verify_disk(args.disk, args.scenario)
    session_bytes, session_sha = pa._read_archive(args.owned_session, maximum=1024*1024)
    session = json.loads(session_bytes)
    opts = argparse.Namespace(host_api=args.host_api, timeout=20, archive=str(args.archive),
        receipt=str(args.evidence/'receipt.json'), expected_core_id='fes.pcxt-dos',
        expected_target_id=args.target_id, expected_package_id=args.package_id,
        expected_selected_package=None, expected_bundle_id=args.bundle_id,
        expected_base_image_sha256=args.image_sha256, poll_attempts=90, poll_interval=1.0)
    runner = pa.Runner(opts)
    owned = runner.session_identity(session, 'caller-owned launch response')
    if owned.package_id != args.package_id:
        raise pa.AcceptanceError('owned session has a different package')
    config = check_private_config(args.config, args.target_id, owned.target_name)
    input_status_value = session.get('input')
    if (not isinstance(input_status_value, dict) or input_status_value.get('state') != 'attached'
            or input_status_value.get('ready') is not True
            or not isinstance(input_status_value.get('session_id'), str)
            or not input_status_value['session_id']):
        raise pa.AcceptanceError('caller-owned response has no attached input session')
    input_session_id = input_status_value['session_id']
    font = visual.load_font()
    provenance, host_receipt, software_receipt = source_provenance(args, font)
    if not args.evidence.parent.is_dir() or shutil.disk_usage(args.evidence.parent).free < 30*1024**3:
        raise pa.AcceptanceError('evidence parent must exist and have at least 30 GiB free')
    args.evidence.mkdir(mode=0o700, exist_ok=False)
    rec = {'format': 1, 'classification': 'in progress', 'scenario': args.scenario,
           'source_commit': args.source_commit, 'provenance': provenance, 'manifest_build_id': manifest['build']['id'],
           'archive_sha256': archive_sha, 'rbf_sha256': args.rbf_sha256, 'package_id': args.package_id,
           'bundle_id': args.bundle_id, 'base_image_sha256': args.image_sha256,
           'target_id': args.target_id, 'boot_id': args.expected_boot, 'claim': claim,
           'config': config, 'owned_session_sha256': session_sha, 'session': {
               'id': owned.session_id, 'target': owned.target_name, 'target_id': owned.target_id,
               'flight_id': owned.flight_id, 'game_id': owned.game_id,
               'package_id': owned.package_id, 'generation': owned.generation,
               'input_session_id': input_session_id}, 'disk': disk,
           'capture_device': args.capture_device, 'started_at_utc': stamp(),
           'captures': [], 'hid_events': [], 'limitations': [
               'library selection and media-ready do not establish independent target-side digest',
               'host tap timing does not establish target-applied keyboard timing',
               'caller owns launch, boot evidence, Stop, software restoration and kit release',
               'gameplay and speaker audio require independent review; audio/video clocks are not synchronized',
               'disk writes are not tested',
               'diagnostic does not qualify whole-board timing or general XT compatibility']}
    try:
        if getattr(args, 'source_historical_main', False):
            observations = getattr(args, 'source_tip_observations', None)
            if observations is None:
                observations = []
                args.source_tip_observations = observations
            rec['source_minimum_tip'] = args.source_minimum_tip
            # Keep the same list as subsequent admission checks, including if
            # an initial ownership/input observation or later phase fails.
            rec['source_tip_observations'] = observations
        (args.evidence/'host-receipt.json').write_bytes(host_receipt)
        (args.evidence/'software-receipt.json').write_bytes(software_receipt)
        validate_active(runner, owned, args, disk['floppy_sha256'])
        input_status(runner, input_session_id, rec['hid_events'], 0, 'initial input')
        if args.scenario == 'com':
            run_com(runner, owned, args, rec, input_session_id, font)
        else:
            helper_source = Path(__file__).resolve().parent/'pcxt_v4l2_capture.c'
            helper, helper_build = game_capture.build_capture_helper(args.evidence, helper_source,
                provenance['diagnostic_file_sha256']['scripts/pcxt_v4l2_capture.c'],
                check_interrupted=check_interrupted)
            probe = gameplay.PlaybackProbe(threading.Event(), max_host_age_ns=args.game_max_host_age_ms*1000000)
            rec['game_probe'] = {'capture_helper':helper_build, 'template_sha256':args.game_template_sha256,
                                 'max_host_age_ms':args.game_max_host_age_ms,
                                 'controls':['neutral','left','right','left'],
                                 'note':'host age is not a target timing bound; audio/video clocks are not synchronized'}
            def factory(device, evidence, selected_probe):
                return game_capture.GameCapture(device, evidence, selected_probe, helper,
                                                 helper_build['binary_sha256'], templates,
                                                 check_interrupted=check_interrupted)
            run_game(runner, owned, args, rec, input_session_id, font, probe=probe, video_factory=factory,
                     audio_factory=game_audio.AudioRecorder)
        validate_active(runner, owned, args, disk['floppy_sha256'])
        input_status(runner, input_session_id, rec['hid_events'], 0, 'final input')
        verify_disk(args.disk, args.scenario)
        final_provenance, _, _ = source_provenance(args, visual.load_font())
        if final_provenance != provenance:
            raise pa.AcceptanceError('diagnostic or host/software provenance changed during run')
        check_interrupted()
        rec['finished_at_utc'] = stamp()
        with (args.evidence/'receipt.json').open('x') as output:
            json.dump(rec, output, indent=2); output.write('\n')
        print(rec['classification'], flush=True)
    except BaseException as exc:
        rec['classification'] = 'failed; owned session and kit cleanup remain with caller'
        rec['failed_at_utc'] = stamp(); rec['failure'] = repr(exc)
        rec['failure_notes'] = [str(note)[:1000] for note in getattr(exc, '__notes__', [])[:20]]
        rec['source_cleanup_incomplete'] = bool(getattr(exc, 'source_cleanup_incomplete', False))
        rec['audio_cleanup_incomplete'] = bool(getattr(exc, 'audio_cleanup_incomplete', False))
        with (args.evidence/'failure.json').open('x') as output:
            json.dump(rec, output, indent=2); output.write('\n')
        raise


if __name__ == '__main__':
    install_signal_handler()
    try:
        main()
    except (pa.AcceptanceError, gameplay.ProbeError, game_capture.capture.CaptureError, game_audio.AudioError,
            ValueError, OSError, subprocess.CalledProcessError,
            subprocess.TimeoutExpired, json.JSONDecodeError) as error:
        raise SystemExit(str(error)) from error
