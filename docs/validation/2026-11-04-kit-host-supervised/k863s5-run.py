#!/usr/bin/env python3
"""k863s5-run.py: #863 slice 5 Kit A check with an image change (Deano OK, 4 Nov).

Order, per the kit rules (software before the image; restore the bundle, then
the image, then verify both):
  1. Claim Kit A (status file + Lander's kitA.lock). Baseline: image good
     f904961e, software b3444abf.
  2. fes-update the slice 5 image while b3444abf stays selected (same base
     contract), verify the boot, and check three services and no host.
  3. Install and activate the slice 5 bundle (advertises kit-host): check four
     direct children, the host config at /run/fogcast/fesdata3/fogcast-host,
     and the host API on 127.0.0.1:8787 through a tunnel.
  4. Activation-to-ready and agent package-list stall, three vs four services.
  5. Launch the ST through the kit host, kill the host by PID, observe the
     core and the host restart.
  6. Restore: activate b3444abf (verify), fes-update rollback to f904961e
     (verify the boot), verify the active bundle, delete the host state, log.
Agent tokens are read only inside Python and never printed.
"""
import fcntl, json, os, re, subprocess, sys, time, tomllib, urllib.error, urllib.request
from pathlib import Path

D = Path('/home/deano/fes-scratch/k863'); OUT = D / 'out863s5'; OUT.mkdir(exist_ok=True)
IMG = D / 'img5'
STATUS = Path('/home/deano/kit-status/kitA-STATUS.md')
LOCK = '/home/deano/kit-status/kitA.lock'
KSSH = '/home/deano/lander-hilq/kits/kssh'
KIT, KNAME, KHOST = 'A', 'dev', 'fes-kit-a'
FS = str(IMG / 'bin/fs')
UPDATE = str(IMG / 'bin/fes-update')
CLI = str(D / 'wt863s3-cli/fogcast')
CAND = json.load(open(IMG / 'candidate.json'))
RELEASE = CAND['release']
NEW_IMAGE = CAND['built_image']
BUNDLE = str(IMG / 'ks/bundle.tar')
NEW_SW = CAND['bundle']
CURRENT = json.load(open('/home/deano/lander-hilq/current.json'))
BASE_SW = CURRENT['kit_sw']
GOOD = CURRENT['image_good']
ST_PKG = '2dd2271c816ad3eb83fb73be9e47b08431eaadd9a4d12484c4d684259090bdb7'
HOST_STATE = '/run/fogcast/fesdata3/fogcast-host'
TAG = os.environ.get('K863_TAG', 'i1')
LOG = OUT / f'driver-{TAG}.log'
def free_port():
    import socket
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


TUNNEL_PORT = free_port()
API = f'http://127.0.0.1:{TUNNEL_PORT}'

cfg = tomllib.loads(Path('/home/deano/.config/fogcast/config.toml').read_text())
target = next(t for t in cfg['targets'] if t['name'] == KNAME)
AGENT_URL = target['address']
results = {'tag': TAG, 'revision': CAND['revision'], 'new_image': NEW_IMAGE, 'new_sw': NEW_SW, 'base_sw': BASE_SW, 'good': GOOD}


def log(msg):
    line = time.strftime('%F %T %Z ') + msg
    print(line, flush=True)
    with open(LOG, 'a') as f:
        f.write(line + '\n')


def save():
    (OUT / f'{TAG}-results.json').write_text(json.dumps(results, indent=1))


def run(cmd, timeout=300, **kw):
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout, **kw)


def kit(cmd, timeout=120):
    r = run([KSSH, KIT, cmd], timeout=timeout)
    return r.returncode, r.stdout, r.stderr


def status_line(text):
    with open(STATUS, 'a') as f:
        f.write(f'\n## {time.strftime("%H:%M %Z")} {text}\n')


def opener():
    return urllib.request.build_opener(urllib.request.ProxyHandler({}))


def agent(path, timeout=20):
    req = urllib.request.Request(AGENT_URL + path, headers={'Authorization': 'Bearer ' + target['agent']})
    return json.loads(opener().open(req, timeout=timeout).read())


def lease_free(wait=45):
    last = ''
    for _ in range(max(1, wait // 3)):
        try:
            s, l = agent('/v1/status'), agent('/v1/kit/lease')
            if s.get('state') == 'idle' and l.get('state') == 'free':
                return True, 'idle free'
            last = f"{s.get('state')} {l.get('state')} {l.get('owner', '')}"
        except Exception as e:  # noqa: BLE001
            last = repr(e)
        time.sleep(3)
    return False, last


def sw_status():
    t = run([FS, '--target', KNAME, '--action', 'status'], timeout=60).stdout
    try:
        d = json.loads(t[t.find('{'):])
    except Exception:  # noqa: BLE001
        return {}
    d.pop('receipts', None)
    return d


def wait_active(bundle, tries=48):
    s = {}
    for _ in range(tries):
        s = sw_status()
        if s.get('active_bundle') == bundle and s.get('selected') == bundle and not s.get('pending'):
            return True, s
        time.sleep(5)
    return False, s


def image_status():
    r = run([UPDATE, '--target', KNAME, '--action', 'status'], timeout=120)
    try:
        return json.loads(r.stdout)
    except Exception:  # noqa: BLE001
        return {'error': (r.stdout + r.stderr)[-300:]}


def update(action, release=None, timeout=1500):
    args = [UPDATE, '--target', KNAME, '--action', action] + (['--release', release] if release else [])
    t0 = time.monotonic()
    r = run(args, timeout=timeout)
    dt = round(time.monotonic() - t0, 1)
    (OUT / f'{TAG}-fes-update-{action}.log').write_text(r.stderr)
    log(f'fes-update {action} rc={r.returncode} {dt}s {r.stdout.strip()[-400:]} {r.stderr.strip()[-300:]}')
    return r.returncode, dt


def services():
    """Direct children of the supervisor: pid, executable, first arguments."""
    # The kit kernel has no /proc/<pid>/task/*/children; match the parent PID.
    rc, out, _ = kit('sup=$(cat /run/fogcast/software/supervisor.pid); for s in /proc/[0-9]*/stat; do set -- $(cat $s 2>/dev/null); [ "$4" = "$sup" ] && echo "$1 $(readlink /proc/$1/exe) $(tr "\\0" " " < /proc/$1/cmdline | cut -c1-80)"; done')
    rows = []
    for line in out.strip().splitlines():
        parts = line.split(' ', 2)
        if len(parts) >= 2:
            rows.append({'pid': int(parts[0]), 'exe': os.path.basename(parts[1]), 'args': parts[2] if len(parts) > 2 else ''})
    return rows


def install_activate(bundle_tar, bundle_id, label):
    if bundle_tar:
        r = run([FS, '--target', KNAME, '--action', 'install', '--bundle', bundle_tar, '--requester', 'deano-k863s5'], timeout=300)
        log(f'install {label} rc={r.returncode} {r.stdout[-200:]}')
    t0 = time.monotonic()
    r = run([FS, '--target', KNAME, '--action', 'activate', '--candidate', bundle_id, '--requester', 'deano-k863s5'], timeout=300)
    log(f'activate {label} rc={r.returncode} {r.stdout[-200:]}')
    ok, s = wait_active(bundle_id)
    results[f'{label}_activate_s'] = round(time.monotonic() - t0, 1)
    return ok, s


def package_list_stall(label, window=70):
    """Time each agent GET /v1/core-packages for window seconds after activation."""
    times, errors = [], 0
    end = time.monotonic() + window
    while time.monotonic() < end:
        t0 = time.monotonic()
        try:
            agent('/v1/core-packages', timeout=60)
            times.append(round(time.monotonic() - t0, 2))
        except Exception:  # noqa: BLE001
            errors += 1
        time.sleep(1)
    results[f'{label}_core_packages_max_s'] = max(times) if times else None
    results[f'{label}_core_packages_errors'] = errors
    log(f'{label}: agent core-packages max {max(times) if times else None}s over {len(times)} calls, {errors} errors')


def host_config():
    rc, out, _ = kit(f'cat {HOST_STATE}/config.toml 2>/dev/null')
    if not out.strip():
        return None
    conf = tomllib.loads(out)
    t0 = (conf.get('targets') or [{}])[0]
    return {'target_id_matches_kit': t0.get('target_id') == target['target_id'], 'token_matches_agent': t0.get('agent') == target['agent'],
            'address': t0.get('address'), 'core_packages': conf.get('core_packages'), 'state': conf.get('state')}


def main():
    lockf = open(LOCK, 'w')
    fcntl.flock(lockf, fcntl.LOCK_EX | fcntl.LOCK_NB)
    last = [l for l in STATUS.read_text().splitlines() if l.startswith('## ')][-1]
    if 'KIT A FREE' not in last:
        sys.exit(f'kit A not FREE: {last[:160]}')
    ok, lease = lease_free(6)
    if not ok:
        sys.exit(f'kit A lease not free: {lease}')
    s0 = sw_status()
    i0 = image_status()
    results['before'] = {'sw': s0.get('active_bundle'), 'image': i0.get('image_sha256'), 'good': i0.get('good'), 'previous': i0.get('previous')}
    if s0.get('active_bundle') != BASE_SW or s0.get('pending') or i0.get('image_sha256') != GOOD or i0.get('trial') or i0.get('pending'):
        sys.exit(f'kit A not on baseline: {results["before"]}')
    status_line(f'KIT A IN USE: deano (Claude session) fes#863 slice 5 supervisor check @ {CAND["revision"][:8]} ({TAG}). IMAGE CHANGE (Deano OK 4 Nov): fes-update to slice 5 image {NEW_IMAGE[:8]} with sw {BASE_SW[:8]} kept, then installs and activates bundle {NEW_SW[:8]}, launches an ST and kills the kit host by PID. Restore target: sw active {BASE_SW[:8]}, then fes-update rollback to image good {GOOD[:8]}, both verified, host state {HOST_STATE} removed. Shared Powerboat host untouched. Workdir {D}.')
    log(f'claimed; before={results["before"]}')
    save()
    tunnel = None
    try:
        # 2. image change, baseline software kept
        rc, results['update_s'] = update('update', RELEASE)
        i1 = image_status()
        results['after_update'] = {k: i1.get(k) for k in ('image_sha256', 'good', 'previous', 'trial', 'pending')}
        if rc != 0 or i1.get('image_sha256') != NEW_IMAGE or i1.get('trial'):
            raise RuntimeError(f'image update did not confirm: {results["after_update"]}')
        ok, s = wait_active(BASE_SW, tries=24)
        results['old_bundle_on_new_image'] = {'active': s.get('active_bundle'), 'held_back': s.get('held_back')}
        time.sleep(20)
        results['old_bundle_services'] = services()
        log(f'new image, old bundle: {results["old_bundle_services"]}')
        save()
        # 3. activate the slice 5 bundle
        ok, s = install_activate(BUNDLE, NEW_SW, 'new_bundle')
        if not ok:
            raise RuntimeError(f'slice 5 bundle not active: {s}')
        package_list_stall('new_bundle')
        results['new_bundle_services'] = services()
        results['host_config'] = host_config()
        log(f'new bundle: {results["new_bundle_services"]} host config {results["host_config"]}')
        rc, out, _ = kit('tail -n 40 /run/fogcast/software/supervisor.log | grep -E "fogcast:|host" | tail -n 12')
        results['host_log'] = out.strip().splitlines()
        save()
        # 4. the same activation timing with three services (re-activate the baseline)
        ok, s = install_activate(None, BASE_SW, 'old_bundle')
        package_list_stall('old_bundle')
        ok, s = install_activate(None, NEW_SW, 'new_bundle_again')
        package_list_stall('new_bundle_again')
        save()
        # 5. kit host launch, then kill the host
        tunnel = subprocess.Popen(['sshpass', '-p', '1', 'ssh', '-N', '-o', 'ConnectTimeout=8', '-o', 'StrictHostKeyChecking=no', '-o', 'UserKnownHostsFile=/dev/null', '-o', 'LogLevel=ERROR', '-o', 'ExitOnForwardFailure=yes', '-o', 'PubkeyAuthentication=no', '-o', 'PreferredAuthentications=password', '-L', f'{TUNNEL_PORT}:127.0.0.1:8787', f'root@{KHOST}'])
        t0 = time.monotonic()
        listed = None
        while time.monotonic() - t0 < 240:
            try:
                listing = json.loads(opener().open(API + '/api/v1/core-packages', timeout=60).read())
                packages = listing if isinstance(listing, list) else listing.get('packages', listing)
                listed = {p['package_id']: p['descriptor']['core']['id'] for p in packages}
                break
            except Exception:  # noqa: BLE001
                time.sleep(2)
        results['host_listed'] = listed
        log(f'kit host lists {listed}')
        if tunnel.poll() is not None:
            raise RuntimeError('the SSH tunnel to the kit host exited')
        if not listed or ST_PKG not in listed:
            raise RuntimeError('kit host does not list the ST package; skipping the launch and kill-host checks')
        r = run([CLI, '--api', API, '--json', 'core-entry', 'ST k863s5', ST_PKG], timeout=600)
        game = json.loads(r.stdout).get('game_id')
        req = urllib.request.Request(API + '/api/v1/session/launch', method='POST', data=json.dumps({'game_id': game, 'target': 'kit'}).encode(),
                                     headers={'Content-Type': 'application/json', 'Accept': 'application/json'})
        t0 = time.monotonic()
        try:
            opener().open(req, timeout=600).read()
            results['launch_rc'] = 0
        except urllib.error.HTTPError as e:
            results['launch_rc'] = e.code
        results['launch_s'] = round(time.monotonic() - t0, 1)
        log(f'launch via kit host {results["launch_rc"]} {results["launch_s"]}s')
        time.sleep(15)
        before = agent('/v1/status')
        host = next(p for p in services() if p['exe'] == 'fogcast-kit')
        kit(f'kill {host["pid"]}')
        log(f'killed kit host pid {host["pid"]} (started by the supervisor for the bundle under test)')
        observed = []
        for _ in range(30):
            time.sleep(2)
            try:
                st = agent('/v1/status')
                observed.append({'state': st.get('state'), 'game': st.get('game_id')})
            except Exception as e:  # noqa: BLE001
                observed.append({'error': repr(e)[:80]})
        after_services = services()
        new_host = [p for p in after_services if p['exe'] == 'fogcast-kit']
        results['kill_host'] = {'before_state': before.get('state'), 'observed': observed,
                                'host_restarted': bool(new_host and new_host[0]['pid'] != host['pid']),
                                'services_after': after_services}
        log(f'after host kill: {results["kill_host"]}')
        save()
        try:
            agent_stop = urllib.request.Request(API + '/api/v1/session/stop', method='POST', data=b'{}', headers={'Content-Type': 'application/json'})
            opener().open(agent_stop, timeout=120).read()
        except Exception as e:  # noqa: BLE001
            log(f'stop via host: {e!r}')
    except Exception as e:  # noqa: BLE001
        log(f'ERROR {e!r}')
        results['error'] = repr(e)
    finally:
        if tunnel:
            tunnel.terminate()
        save()
        restore()


def restore():
    steps = {}
    try:
        st = agent('/v1/status')
        if st.get('state') != 'idle':
            log(f'kit not idle before restore: {st.get("state")}; waiting')
            lease_free(90)
    except Exception:  # noqa: BLE001
        pass
    s = sw_status()
    if s.get('active_bundle') != BASE_SW or s.get('selected') != BASE_SW:
        ok, s = install_activate(None, BASE_SW, 'restore_sw')
    steps['sw_before_image_restore'] = s.get('active_bundle')
    i = image_status()
    if i.get('image_sha256') != GOOD:
        rc, _ = update('rollback')
        i = image_status()
    steps['image_after'] = {k: i.get(k) for k in ('image_sha256', 'good', 'previous', 'trial', 'pending')}
    ok_sw, s = wait_active(BASE_SW, tries=24)
    steps['sw_after'] = s.get('active_bundle')
    kit(f'rm -rf {HOST_STATE}; ls -d {HOST_STATE} 2>&1 | head -1')
    rc, left, _ = kit(f'ls -d {HOST_STATE} 2>/dev/null; echo end')
    ok_lease, lease = lease_free(90)
    svc = services()
    steps.update({'host_state_left': left.strip(), 'lease': lease, 'services': [p['exe'] for p in svc]})
    results['restore'] = steps
    save()
    log('RESTORE ' + json.dumps(steps))
    good = (i.get('image_sha256') == GOOD and not i.get('trial') and ok_sw and steps['sw_after'] == BASE_SW
            and ok_lease and left.strip() == 'end' and 'fogcast-kit' not in steps['services'])
    if good:
        status_line(f'KIT A FREE — deano fes#863 slice 5 supervisor check ({TAG}) done. Restored sw {BASE_SW[:8]} active (verified), then image rolled back to good {GOOD[:8]} (boot verified, not a trial), active bundle {BASE_SW[:8]} verified after the image restore, three services, host state {HOST_STATE} removed, lease {lease}. Evidence {OUT}.')
    else:
        status_line(f'KIT A HELD — deano fes#863 slice 5 ({TAG}) restore incomplete: {json.dumps(steps)[:400]}. Needs attention; do not use Kit A.')


if __name__ == '__main__':
    main()
