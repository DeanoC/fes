#!/usr/bin/env python3
"""Core-only kit HIL for the F3 Spectrum reseals (fes#2101).

usage: spec_session.py MODEL KIT ARCHIVE.fcore HEAD RUN

MODEL is spectrum, spectrum128, pentagon or plus3. Each menu's first item is its
tape loader (Tape Loader on the 128K and Pentagon, Loader on the +3). The 48K
has no menu: it types LOAD "" (J, then SYMBOL SHIFT+P twice; Control is SYMBOL
SHIFT) and, after the break, PRINT 22 as P22 in keyword mode. The claim, baseline probe, data-tree delta
and release follow the #2128 C64 core-only wrapper: the kit must be logged
FREE, on the baseline Lander's ~/lander-hilq/current.json names for that kit (its
image and kit_sw_by_kit bundle; probed live right after the claim), with an
idle agent and a free lease, and the image, bundle, lease and launcher state
must read the same after the run. Nothing is installed, the image is not
touched and the shared FogCast host is not used: specrun.py drives a private
host built from HEAD.

The run: launch the package as a library game with the user's ROM and the
self-made tape (make_tap.py) as its cassette, grab the 128 menu, choose Tape
Loader, grab the loaded program's blue border, break it, type PRINT 22, grab
the answer, stop. The grabs are the evidence; the verdict is read from them.
"""
import argparse, datetime, fcntl, hashlib, json, pathlib, re, subprocess, sys, tarfile, time, tomllib, urllib.request

ROMS = {'spectrum': ('fes.spectrum', 'sinclair/spectrum-48.rom',
                     'd55daa439b673b0e3f5897f99ac37ecb45f974d1862b4dadb85dec34af99cb42'),
        'spectrum128': ('fes.spectrum128', 'sinclair/spectrum-128.rom',
                        'c1ff621d7910105d4ee45c31e9fd8fd0d79a545c78b66c69a562ee1ffbae8d72'),
        'pentagon': ('fes.pentagon', 'sinclair/pentagon-trd503.rom',
                     '3ef7872a0769ddf55a207b0178277fbb39d446169886c1c2f7a8b5d662b5f16f'),
        'plus3': ('fes.spectrum-plus3', 'sinclair/spectrum-plus3.rom',
                  'ee8218fa43ecb672ed45370114294228213a82318c6d1b007ec86bee3293d1f2')}
p = argparse.ArgumentParser()
p.add_argument('model', choices=ROMS); p.add_argument('kit', choices=['A', 'B'])
p.add_argument('archive', type=pathlib.Path); p.add_argument('head'); p.add_argument('run')
a = p.parse_args(); kit, model = a.kit, a.model
core_id, rom_rel, rom_sha = ROMS[model]
here = pathlib.Path(__file__).resolve().parent
work = pathlib.Path('/data/fes-f3-d12/hil'); run = work / 'runs' / a.run
home = pathlib.Path.home()

# --- the package is the sealed one from HEAD ---------------------------------
package_id = a.archive.name.split('.')[0]
assert re.fullmatch('[0-9a-f]{64}', package_id) and a.archive.is_file()
with tarfile.open(a.archive) as archive:
    manifest = tomllib.loads(archive.extractfile('manifest.toml').read().decode())
    payload = archive.extractfile('core.rbf').read()
assert manifest['core']['id'] == core_id, manifest['core']['id']
assert manifest['target']['platform'] == 'de10_nano'
assert hashlib.sha256(payload).hexdigest() == manifest['payload']['sha256']
assert manifest['build']['revision'] == a.head, manifest['build']['revision']
rom = home / 'roms' / rom_rel
assert hashlib.sha256(rom.read_bytes()).hexdigest() == rom_sha
tape = work / 'media/fes-hil.tap'
assert tape.is_file()

run.mkdir(parents=True, mode=0o700)
out = run / 'snapshots'; out.mkdir()
plan = {'package': str(a.archive.resolve()), 'package_id': package_id, 'firmware': str(rom),
        'games': {'machine': f'{core_id} F3 D12 {a.run}'}, 'disks': {'tape': str(tape)},
        'media_role': 'cassette', 'rom_id': {'spectrum': 'spectrum-firmware', 'spectrum128': 'spectrum128-firmware', 'pentagon': 'pentagon-firmware', 'plus3': 'spectrum-plus3-firmware'}[model]}
(run / 'plan.json').write_text(json.dumps(plan, indent=1))
script = [
    {'op': 'launch', 'game': 'machine', 'disk': 'tape'},
    {'op': 'sleep', 's': 8}, {'op': 'grab', 'name': 'menu'},
    *([{'op': 'key', 'name': 'j'}, {'op': 'key', 'name': 'p', 'mods': ['lctrl']},   # 48K: LOAD ""
       {'op': 'key', 'name': 'p', 'mods': ['lctrl']}, {'op': 'key', 'name': 'enter'}] if model == 'spectrum'
      else [{'op': 'key', 'name': 'enter'}]),             # the tape loader is the menu's first item
    {'op': 'sleep', 's': 25}, {'op': 'grab', 'name': 'loaded'},
    {'op': 'key', 'name': 'space'},                       # breaks PAUSE 0
    {'op': 'sleep', 's': 2}, {'op': 'grab', 'name': 'break'},
    {'op': 'type', 'text': 'p22\n' if model == 'spectrum' else 'print 22\n'},
    {'op': 'sleep', 's': 2}, {'op': 'grab', 'name': 'typed'},
    {'op': 'stop'}, {'op': 'quit'}]

# --- claim -------------------------------------------------------------------
base = json.loads((home / 'lander-hilq/current.json').read_text())
status = home / 'kit-status' / f'kit{kit}-STATUS.md'; target = {'A': 'dev', 'B': 'kit2'}[kit]


def command(cmd, secs=60):
    return subprocess.run(list(map(str, cmd)), stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=secs,
                          check=True, text=True).stdout


def append(line):
    with status.open('a') as f:
        f.write('\n## ' + datetime.datetime.now().astimezone().isoformat(timespec='seconds') + ' KIT ' + kit + ' ' + line + '\n')


def last():
    return [s for s in status.read_text().splitlines() if s.startswith('## ') and f'KIT {kit} ' in s][-1]


locks = []
for path in [home / 'fes-hil/locks' / f'lander-kit{kit}.lock', home / 'kit-status' / f'kit{kit}.lock',
             home / 'fes-hil/locks' / f'kit{kit}.lock']:
    f = path.open('a'); fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB); locks.append(f)
if f'KIT {kit} FREE' not in last():
    sys.exit('Kit busy: ' + last()[:160])
# FREE lines from Lander's maintenance name no hashes; probe('pre') below checks the
# live image, bundle, idle agent and free lease before anything changes, and
# withdraws the claim if they differ.
image, bundle = base['image_good'], (base.get('kit_sw_by_kit') or {}).get(kit, [base['kit_sw']])[0]
append(f'IN USE — Claude fes#2101 (F3 D12) {core_id} reseal HIL, package {package_id} from {a.head[:12]}, core only '
       f'(private host, no install, no image change); restore image {image}, bundle {bundle}, idle/lease-free. Evidence {run}')
t = next(t for t in tomllib.loads((home / '.config/fogcast/config.toml').read_text())['targets'] if t['name'] == target)
kt = home / ('fes-scratch/m5/kB' if kit == 'B' else 'fes-scratch/m4/kA'); kssh = home / 'lander-hilq/kits/kssh'
fs = kt / 'fs'; imagebin = kt / 'fes-update'
data = '/media/fat/fogcast/core-data /media/fat/fogcast/disk-images'


def remote(s):
    return command([kssh, kit, s])


def files():
    return set(remote('find ' + data + ' 2>/dev/null | sort').splitlines())


def probe(tag):
    command([home / 'lander-hilq/kits/snap.sh', kit, tag, out, fs], 150)
    img = json.loads(command([imagebin, '--target', target, '--action', 'status']).strip().splitlines()[-1])
    (out / (tag + '-image.json')).write_text(json.dumps(img))
    assert img['good'] == img['image_sha256'] == image and not img['pending'] and not img['trial'] and img['raw_idle_ready']
    sw = (out / (tag + '-sw.txt')).read_text().strip(); assert 'selected ' + bundle + ' active ' + bundle + ' no-pending' in sw
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
    (out / (tag + '-lease.json')).write_text(json.dumps(state))
    return (img, {s for s in snap.splitlines() if s.startswith(('state.', 'images ', 'cmdline_quirk ', 'fat_idle ', 'launcher.json '))},
            sw.split(' selected ')[1])


try:
    before = probe('pre')
except Exception as e:
    append(f'FREE — Claude fes#2101 {core_id} claim withdrawn before any change ({type(e).__name__}: {str(e)[:160]}); kit as found')
    raise
old = files(); (out / 'paths-pre.txt').write_text('\n'.join(sorted(old)))

# --- drive -------------------------------------------------------------------
drv = run / 'drive'
driver = subprocess.Popen([sys.executable, str(here / 'specrun.py'), kit, str(drv), str(run / 'plan.json')],
                          env={**__import__('os').environ, 'HOSTBIN': str(work / 'bin/fogcast-api'), 'C64_RUN_LIMIT': '1200'},
                          stdout=(run / 'driver.out').open('w'), stderr=subprocess.STDOUT)
cmds, acks = drv / 'commands.jsonl', drv / 'acks.jsonl'
ok = True
for i, op in enumerate(script):
    while not cmds.exists():
        time.sleep(0.5)
    with cmds.open('a') as f:
        f.write(json.dumps(op) + '\n')
    if op['op'] == 'quit':
        break
    deadline = time.monotonic() + 400
    while (len(acks.read_text().splitlines()) if acks.exists() else 0) < i + 1:
        if driver.poll() is not None or time.monotonic() > deadline:
            ok = False; break
        time.sleep(0.3)
    if not ok or json.loads(acks.read_text().splitlines()[i])['status'] != 'ok':
        ok = False
        with cmds.open('a') as f:
            f.write(json.dumps({'op': 'stop'}) + '\n' + json.dumps({'op': 'quit'}) + '\n')
        break
try:
    rc = driver.wait(300)
except subprocess.TimeoutExpired:
    driver.terminate(); rc = driver.wait(60)

# --- restore and release -----------------------------------------------------
after = probe('post'); assert after == before, 'kit state changed'
paths = files() - old
for name in sorted(paths):
    if re.fullmatch(r'/media/fat/fogcast/(core-data/(media|games)/[a-f0-9]{64}|disk-images/[a-f0-9]{64}\.img)', name):
        remote("rm -r -- '" + name + "'")
for path in sorted(files() - old):
    if re.fullmatch('/media/fat/fogcast/core-data/[a-f0-9]{64}', path):
        remote("rmdir -- '" + path + "'")
# A first launch on a kit creates the parent too; rmdir refuses one that is not empty.
for path in sorted(files() - old, reverse=True):
    if path in ('/media/fat/fogcast/core-data/games', '/media/fat/fogcast/core-data/media'):
        remote("rmdir -- '" + path + "'")
assert files() == old, 'Kit data delta must be investigated'
append(f'FREE — Claude fes#2101 {core_id} HIL completed (driver rc {rc}, script {"complete" if ok else "halted"}); verified baseline '
       f'image {image}, bundle {bundle}, same boot ID, idle and ownerless free lease; pre-run data listing restored. Evidence {run}')
sys.exit(0 if ok and rc == 0 else 1)
