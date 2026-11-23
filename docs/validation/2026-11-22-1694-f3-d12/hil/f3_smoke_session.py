#!/usr/bin/env python3
"""Kit smoke for F3's floppy path (fes#2101): PR bundle + sealed pc386 package.

usage: f3_smoke_session.py KIT RUN BUNDLE_DIR PACKAGE.fcore FLOPPY_A FLOPPY_B NONCE

The claim, baseline probe, data-tree delta and release follow spec_session.py.
The kit must be logged FREE on the baseline named in
~/fes-hil/baselines/current-restore.json, idle with a free lease.

1. Capture the kit's FogCast catalog (WAL-aware, promote_kits.copy_live_catalog),
   then install and activate the PR kit-software bundle on the running
   baseline image with fes-software built from HEAD, as Lander's main
   promotion does. The PR's kit host migrates the catalog to schema 27.
2. hil_f3_floppy.py runs the sealed fes.pc386 package with both floppies
   (its own lease; it removes the run's FogCast data).
3. Activate the baseline bundle again (still installed: it is the previous
   release). The software supervisor restores its pre-activation catalog
   checkpoint; the captured catalog files must match the step-1 capture byte
   for byte. Image, bundle, boot ID, quirk line, launcher, idle agent, free
   lease and no path the run created may remain before logging FREE (staged
   images the agent's four-image cache evicted are recorded, not restored). Anything else logs RESTORE_INCOMPLETE and keeps the claim.
"""
import fcntl, datetime, hashlib, json, os, pathlib, re, subprocess, sys, time, tomllib, urllib.request

kit, run_name, bundle_dir, package, floppy_a, floppy_b, nonce = sys.argv[1:8]
assert kit in ('A', 'B')
home = pathlib.Path.home()
here = pathlib.Path(__file__).resolve().parent
head = pathlib.Path('/data/fes-f3-d12/head')
work = pathlib.Path('/data/fes-f3-d12/smoke'); run = work / 'runs' / run_name
run.mkdir(parents=True, mode=0o700); out = run / 'snapshots'; out.mkdir()
base = json.loads((home / 'fes-hil/baselines/current-restore.json').read_text())
image, bundle = base['image_good'], base['kit_sw']
base_tar = home / 'fes-hil/baselines/post-main-1a4bb8d4/bundle/bundle.tar'
base_tar_sha = base['bundle_tar_sha256']
status = home / 'kit-status' / f'kit{kit}-STATUS.md'; target = {'A': 'dev', 'B': 'kit2'}[kit]
t = next(t for t in tomllib.loads((home / '.config/fogcast/config.toml').read_text())['targets'] if t['name'] == target)
kssh = home / 'lander-hilq/kits/kssh'
kt = home / ('fes-scratch/m5/kB' if kit == 'B' else 'fes-scratch/m4/kA')
fs = kt / 'fs'; imagebin = kt / 'fes-update'
data = '/media/fat/fogcast/core-data /media/fat/fogcast/disk-images'

# The PR bundle: its archive and release ID from the build receipt.
receipt = json.loads((pathlib.Path(bundle_dir) / 'receipt.json').read_text())
pr_id = receipt['bundle']['id']
pr_tar = pathlib.Path(bundle_dir) / 'bundle.tar'
pr_tar_sha = hashlib.sha256(pr_tar.read_bytes()).hexdigest()
assert pr_tar_sha == receipt['artifacts']['bundle.tar']['sha256'], 'bundle tar differs from its receipt'
assert receipt['base_image_sha256'] == image, receipt['base_image_sha256']
FS = work / 'bin/fes-software'
# HEAD's FogCast decodes its config strictly, and the shared host config still
# carries keys HEAD no longer has (host_emulator, media audio/RTP). fes-software
# gets a private 0600 copy with only the connection and target fields,
# removed at exit.
_shared = tomllib.loads((home / '.config/fogcast/config.toml').read_text())


def _toml(v):
    return json.dumps(v) if isinstance(v, str) else ('true' if v is True else 'false' if v is False else str(v))


CFG = run / 'fes-software.toml'
_fd = os.open(CFG, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(_fd, 'w') as _f:
    for _k in ('base_url', 'token', 'request_timeout_seconds', 'upload_timeout_seconds', 'selected_target'):
        if _k in _shared:
            _f.write(f'{_k} = {_toml(_shared[_k])}\n')
    for _t in _shared['targets']:
        _f.write('\n[[targets]]\n' + ''.join(f'{k} = {_toml(v)}\n' for k, v in _t.items()))
import atexit  # noqa: E402
atexit.register(lambda: CFG.unlink(missing_ok=True))
sys.path.insert(0, str(home / 'fes-hil/baselines/post-1640-2320f64d'))
import promote_kits as pk  # noqa: E402  copy_live_catalog, catalog_schema


def command(cmd, secs=60):
    return subprocess.run(list(map(str, cmd)), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=secs,
                          check=True, text=True).stdout


def append(line):
    with status.open('a') as f:
        f.write('\n## ' + datetime.datetime.now().astimezone().isoformat(timespec='seconds') + ' KIT ' + kit + ' ' + line + '\n')


def last():
    return [s for s in status.read_text().splitlines() if s.startswith('## ') and f'KIT {kit} ' in s][-1]


def remote(s):
    return command([kssh, kit, s])


def files():
    return set(remote('find ' + data + ' 2>/dev/null | sort').splitlines())


def probe(tag, want_bundle):
    command([home / 'lander-hilq/kits/snap.sh', kit, tag, out, fs], 150)
    img = json.loads(command([imagebin, '--target', target, '--action', 'status']).strip().splitlines()[-1])
    (out / (tag + '-image.json')).write_text(json.dumps(img))
    assert img['good'] == img['image_sha256'] == image and not img['pending'] and not img['trial'], img
    sw = (out / (tag + '-sw.txt')).read_text().strip()
    assert 'selected ' + want_bundle + ' active ' + want_bundle + ' no-pending' in sw, sw
    snap = (out / (tag + '-kit.txt')).read_text(); assert kit == 'A' or 'usb-storage.quirks=152d:0583:i' in snap
    state = {}
    for ep in ('status', 'kit/lease'):
        for _ in range(10):  # a lease reads `revoking` for a few seconds after a session closes
            with urllib.request.urlopen(urllib.request.Request(t['address'] + '/v1/' + ep, headers={'Authorization': 'Bearer ' + t['agent']}), timeout=8) as r:
                j = json.load(r)
            state[ep] = (j.get('state'), bool(j.get('owner')))
            if state[ep] in (('idle', False), ('free', False)):
                break
            time.sleep(3)
    assert state == {'status': ('idle', False), 'kit/lease': ('free', False)}, state
    keys = {s for s in snap.splitlines() if s.startswith(('state.', 'images ', 'cmdline_quirk ', 'fat_idle ', 'launcher.json '))}
    return keys | {'boot_id ' + snap.split('boot_id ')[1].split()[0]}


def sw_status():
    r = subprocess.run([str(FS), '--config', str(CFG), '--target', target, '--action', 'status'], capture_output=True, text=True, timeout=65)
    raw = r.stdout + r.stderr
    return json.loads(raw[raw.index('{'):])


def wait_selected(want, seconds=300):
    end, last_seen = time.monotonic() + seconds, None
    while time.monotonic() < end:
        try:
            last_seen = sw_status()
            if last_seen.get('active_bundle') == last_seen.get('selected') == want and not last_seen.get('pending'):
                return last_seen
        except Exception as e:  # noqa: BLE001
            last_seen = str(e)
        time.sleep(5)
    raise RuntimeError('activation wait expired: ' + str(last_seen)[:200])


def activate(name, candidate, tar=None):
    if tar is not None:
        r = subprocess.run([str(FS), '--config', str(CFG), '--target', target, '--action', 'install', '--bundle', str(tar)],
                           capture_output=True, text=True, timeout=400)
        (run / f'{name}-install.log').write_text(r.stdout + r.stderr)
        if r.returncode:
            raise RuntimeError(f'{name}: install rc {r.returncode}')
    r = subprocess.run([str(FS), '--config', str(CFG), '--target', target, '--action', 'activate', '--candidate', candidate,
                        '--requester', f'claude-fes2101-{name}'], capture_output=True, text=True, timeout=300)
    (run / f'{name}-activate.log').write_text(r.stdout + r.stderr)
    if r.returncode:
        raise RuntimeError(f'{name}: activate rc {r.returncode}')
    (run / f'{name}-sw.json').write_text(json.dumps(wait_selected(candidate), indent=1))


def catalog(name):
    live, files = pk.copy_live_catalog(kit, run / f'catalog-{name}')
    schema = pk.catalog_schema(live / 'library.sqlite3')
    (run / f'catalog-{name}.json').write_text(json.dumps({'schema': schema, 'files': files}, indent=1))
    return schema, files


locks = []
for path in [home / 'fes-hil/locks' / f'lander-kit{kit}.lock', home / 'kit-status' / f'kit{kit}.lock',
             home / 'fes-hil/locks' / f'kit{kit}.lock']:
    f = path.open('a'); fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB); locks.append(f)
if f'KIT {kit} FREE' not in last():
    sys.exit('Kit busy: ' + last()[:160])
if image not in last() or bundle not in last():
    sys.exit('Verify ledger baseline manually: ' + last()[:200])
append(f'IN USE: Claude fes#2101 (F3 #1694) floppy kit smoke: installs and activates PR kit-software bundle {pr_id} '
       f'(tar {pr_tar_sha[:16]}, built from {head.name} d68224547 on base image {image[:8]}), runs the sealed fes.pc386 '
       f'package {pathlib.Path(package).stem} with two self-made FAT12 floppies through the agent (no image change, no '
       f'reboot, no host change), then activates baseline bundle {bundle} again; restore image {image}, bundle {bundle}, '
       f'idle/lease-free. Evidence {run}')

try:
    before = probe('pre', bundle)
except Exception as e:
    append(f'FREE — Claude fes#2101 floppy smoke claim withdrawn before any change ({type(e).__name__}: {str(e)[:160]}); kit as found')
    raise
old = files(); (out / 'paths-pre.txt').write_text('\n'.join(sorted(old)))

cat_schema, cat_files = catalog('pre')
rc_drive, problem = None, None
try:
    activate('install', pr_id, pr_tar)
    probe('mid', pr_id)
    ka = run / 'kssh1'; ka.write_text(f'#!/bin/sh\nexec {kssh} {kit} "$1"\n'); ka.chmod(0o700)
    r = subprocess.run([sys.executable, str(here / 'hil_f3_floppy.py'), str(head / 'sources/misteross'),
                        '--kit', kit, '--owner', 'Claude', '--package', package, '--out', str(run / 'drive'),
                        '--kit-ssh', str(ka), '--drive', 'real', '--floppy-a', floppy_a, '--floppy-b', floppy_b,
                        '--nonce', nonce], stdout=(run / 'driver.out').open('w'), stderr=subprocess.STDOUT, timeout=2400)
    rc_drive = r.returncode
except Exception as e:  # noqa: BLE001  every path below reaches the restore decision
    problem = f'{type(e).__name__}: {str(e)[:200]}'
try:
    result = json.loads((run / 'drive/result.json').read_text())
except (OSError, ValueError):
    result = {}
if (result.get('after') or {}).get('recovery_required'):
    append(f'RESTORE_INCOMPLETE — Claude fes#2101 floppy smoke: the driver reports recovery_required (stop not '
           f'confirmed); no restore attempted, claim retained, NOT FREE. Evidence {run}')
    sys.exit(3)
try:
    active = sw_status().get('active_bundle')
    if active != bundle:
        assert active == pr_id, f'unexpected active bundle {active}'
        activate('restore', bundle)
    after = probe('post', bundle)
    assert after == before, ('kit state changed', sorted(before ^ after))
    schema, files_after = catalog('post')
    assert (schema, files_after) == (cat_schema, cat_files), ('catalog not restored', schema, cat_schema)
    for path in sorted(files() - old):
        if re.fullmatch(r'/media/fat/fogcast/(core-data/(media|games)/[a-f0-9]{64}|disk-images/[a-f0-9]{64}\.img)', path):
            remote("rm -r -- '" + path + "'")
    now = files()
    assert not now - old, ('the run left kit data', sorted(now - old))
    # The agent's staging cache keeps the four most recently used disk images
    # (FogCast misterruntime/disk_images.go, diskImagesKept, main since #1483),
    # so staging this run's drive and floppies can evict older cached images.
    # Those are content-addressed copies a later launch re-stages; anything
    # else gone is a failure.
    evicted = sorted(old - now)
    assert all(re.fullmatch(r'/media/fat/fogcast/disk-images/[a-f0-9]{64}\.img', p) for p in evicted), \
        ('kit data removed', evicted)
    (run / 'evicted.txt').write_text('\n'.join(evicted))
except Exception as e:  # noqa: BLE001
    append(f'RESTORE_INCOMPLETE — Claude fes#2101 floppy smoke: restore check failed ({type(e).__name__}: '
           f'{str(e)[:200]}; run problem {problem}); claim retained, NOT FREE. Evidence {run}')
    raise
append(f'FREE — Claude fes#2101 floppy smoke done (drive rc {rc_drive}, run problem {problem}); verified baseline image '
       f'{image}, bundle {bundle} selected=active no pending, same boot ID, catalog files equal to the pre-install '
       f'capture (schema {cat_schema}), idle and ownerless free lease; the run left no kit data; staging-cache evictions '
       f'(diskImagesKept = 4): {", ".join(p.rsplit("/", 1)[1][:16] for p in evicted) or "none"}. Evidence {run}')
sys.exit(0 if rc_drive == 0 and problem is None else 1)
