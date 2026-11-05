#!/usr/bin/env python3
"""k863s4-run.py: #863 slice 4 kit-mode check on Kit A (hardware diagnostic, not acceptance).

Claims Kit A (status file plus Lander's kitA.lock), keeps the baseline bundle
(no install), and runs the branch's ARM fogcast-api in kit mode by hand:
`-kit -kit-state /run/fogcast/fesdata3/k863s4/fogcast-host` on 127.0.0.1:8797,
with the real agent.toml and the image's catalog config. Checks the generated
config against Kit A's target ID without printing the token, lists, titles and
launches the ST package already in the durable store, stops, then adds a
library to the generated config, restarts the host and checks the library
survived and the managed target row was rewritten. Removes its own files and
processes by PID, checks the lease, and logs the result.
"""
import fcntl, json, os, subprocess, sys, time, tomllib, urllib.error, urllib.request
from pathlib import Path

D = Path('/home/deano/fes-scratch/k863'); OUT = D / 'out863s4'; OUT.mkdir(exist_ok=True)
W = Path('/home/deano/fes/.claude/worktrees/wt863s4')
KIT, KNAME, KHOST = 'A', 'dev', 'fes-kit-a'
STATUS = Path('/home/deano/kit-status/kitA-STATUS.md')
LOCK = '/home/deano/kit-status/kitA.lock'
KSSH = '/home/deano/lander-hilq/kits/kssh'
FS = '/home/deano/fes-hil/bin/fs'
CLI = str(D / 'wt863s3-cli/fogcast')
API_BIN = str(D / 'fogcast-api-863s4')
ST_PKG = '2dd2271c816ad3eb83fb73be9e47b08431eaadd9a4d12484c4d684259090bdb7'
BASE_SW = json.load(open('/home/deano/lander-hilq/current.json'))['kit_sw']
GOOD = json.load(open('/home/deano/lander-hilq/current.json'))['image_good']
HEAD = subprocess.check_output(['git', '-C', str(W), 'rev-parse', 'HEAD'], text=True).strip()
KDIR = '/run/fogcast/fesdata3/k863s4'
TAG = os.environ.get('K863_TAG', 'k1')
LOG = OUT / f'driver-{TAG}.log'
API = 'http://127.0.0.1:18797'

cfg = tomllib.loads(Path('/home/deano/.config/fogcast/config.toml').read_text())
target = next(t for t in cfg['targets'] if t['name'] == KNAME)
AGENT_URL = target['address']


def log(msg):
    line = time.strftime('%F %T %Z ') + msg
    print(line, flush=True)
    with open(LOG, 'a') as f:
        f.write(line + '\n')


def run(cmd, timeout=300, **kw):
    return subprocess.run(cmd, capture_output=True, text=True, timeout=timeout, **kw)


def kit(cmd, timeout=120, stdin=None):
    args = [KSSH, KIT] + (['-i'] if stdin is not None else []) + [cmd]
    r = subprocess.run(args, input=stdin, capture_output=True, text=True, timeout=timeout)
    return r.returncode, r.stdout, r.stderr


def status_line(text):
    with open(STATUS, 'a') as f:
        f.write(f'\n## {time.strftime("%H:%M %Z")} {text}\n')


def opener():
    return urllib.request.build_opener(urllib.request.ProxyHandler({}))


def agent(path):
    req = urllib.request.Request(AGENT_URL + path, headers={'Authorization': 'Bearer ' + target['agent']})
    return json.loads(opener().open(req, timeout=20).read())


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


def cli(*args, timeout=600):
    t0 = time.monotonic()
    r = run([CLI, '--api', API, '--json', *args], timeout=timeout)
    dt = time.monotonic() - t0
    log(f'CLI {args[0]} rc={r.returncode} {dt:.1f}s {r.stdout.strip()[-400:]} {r.stderr.strip()[-200:]}')
    return r, dt


def start_api(results, key):
    kit(f'cd {KDIR} && setsid nohup ./fogcast-api -kit -kit-state {KDIR}/fogcast-host -headless -listen 127.0.0.1:8797 >> {KDIR}/api.log 2>&1 < /dev/null & echo $! > {KDIR}/api.pid')
    t0 = time.monotonic()
    for _ in range(120):
        try:
            opener().open(API + '/api/v1/health', timeout=3).read()
            break
        except Exception:  # noqa: BLE001
            time.sleep(1)
    results[key + '_api_ready_s'] = round(time.monotonic() - t0, 1)
    unavailable = 0
    while True:
        try:
            listing = json.loads(opener().open(API + '/api/v1/core-packages', timeout=300).read())
            break
        except urllib.error.HTTPError as e:
            if e.code != 503 or time.monotonic() - t0 > 300:
                raise
            unavailable += 1
            time.sleep(1)
    packages = listing if isinstance(listing, list) else listing.get('packages', listing)
    results[key + '_packages_ready_s'] = round(time.monotonic() - t0, 1)
    results[key + '_listed'] = {p['package_id']: p['descriptor']['core']['id'] for p in packages}
    log(f'{key}: api ready {results[key + "_api_ready_s"]}s, packages ready {results[key + "_packages_ready_s"]}s after {unavailable} 503s: {results[key + "_listed"]}')


def stop_api():
    kit(f'p=$(cat {KDIR}/api.pid 2>/dev/null); [ -n "$p" ] && kill $p; for i in 1 2 3 4 5 6 7 8 9 10; do [ -n "$p" ] && kill -0 $p 2>/dev/null || break; sleep 1; done; rm -f {KDIR}/api.pid')


def generated():
    """The generated config, checked here so the token is never printed."""
    rc, out, _ = kit(f'cat {KDIR}/fogcast-host/config.toml')
    conf = tomllib.loads(out)
    targets = conf.get('targets', [])
    t0 = targets[0] if targets else {}
    return {
        'targets': len(targets), 'target_name': t0.get('name'), 'target_enabled': t0.get('enabled'),
        'target_address': t0.get('address'), 'target_id': t0.get('target_id'),
        'target_id_matches_kit': t0.get('target_id') == target['target_id'],
        'token_matches_agent': t0.get('agent') == target['agent'],
        'selected_target': conf.get('selected_target'), 'state': conf.get('state'), 'staging': conf.get('staging'),
        'core_packages': conf.get('core_packages'), 'folder_watch_seconds': (conf.get('library') or {}).get('folder_watch_seconds'),
        'upload_timeout_seconds': conf.get('upload_timeout_seconds'), 'firmware': conf.get('firmware'),
        'libraries': [l.get('id') for l in conf.get('libraries', [])],
    }, out


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
    if s0.get('active_bundle') != BASE_SW or s0.get('pending'):
        sys.exit(f'kit A not on baseline sw {BASE_SW[:8]}')
    status_line(f'KIT A IN USE: deano (Claude session) fes#863 slice 4 kit-mode check @ {HEAD[:8]} ({TAG}). Baseline bundle {BASE_SW[:8]} stays active (no install, no image change, no reboot). Runs the branch fogcast-api by hand in kit mode on 127.0.0.1:8797 with state in {KDIR}, launches and stops the ST, restarts the host once. Shared Powerboat host untouched. Restore target: sw active {BASE_SW[:8]}, image good {GOOD[:8]}. Workdir {D}.')
    log(f'claimed; base_sw={BASE_SW} head={HEAD}')
    tunnel = None
    results = {'tag': TAG, 'head': HEAD, 'baseline_sw': BASE_SW}
    try:
        rc, out, err = kit('test -e /run/fogcast/kit-host && echo present || echo absent')
        results['fallback_root_before'] = out.strip()
        rc, out, err = kit(f'test -d /run/fogcast/fesdata3 && test -f /media/fat/fogcast/agent.toml && rm -rf {KDIR} && mkdir -m 700 {KDIR} && echo ok')
        if 'ok' not in out:
            raise RuntimeError(f'kit prep failed: {out} {err}')
        run(['sshpass', '-p', '1', 'scp', '-O', '-q', '-o', 'StrictHostKeyChecking=no', '-o', 'UserKnownHostsFile=/dev/null', '-o', 'PubkeyAuthentication=no', '-o', 'PreferredAuthentications=password', API_BIN, f'root@{KHOST}:{KDIR}/fogcast-api']).check_returncode()
        kit(f'cat > {KDIR}/k863-sampler.sh && chmod 700 {KDIR}/k863-sampler.sh', stdin=(D / 'kit/k863-sampler.sh').read_text())
        kit(f'cd {KDIR} && setsid nohup ./k863-sampler.sh {KDIR}/samples.tsv > /dev/null 2>&1 < /dev/null & echo $! > {KDIR}/sampler.pid')
        time.sleep(5)
        tunnel = subprocess.Popen(['sshpass', '-p', '1', 'ssh', '-N', '-o', 'ConnectTimeout=8', '-o', 'StrictHostKeyChecking=no', '-o', 'UserKnownHostsFile=/dev/null', '-o', 'LogLevel=ERROR', '-o', 'PubkeyAuthentication=no', '-o', 'PreferredAuthentications=password', '-L', '18797:127.0.0.1:8797', f'root@{KHOST}'])
        start_api(results, 'first')
        results['first_config'], _ = generated()
        log('first config: ' + json.dumps(results['first_config']))
        listed = results['first_listed']
        if ST_PKG not in listed:
            raise RuntimeError('ST package not listed')
        r, results['title_s'] = cli('core-entry', 'ST k863s4', ST_PKG)
        game = json.loads(r.stdout).get('game_id')
        results['game_id'] = game
        time.sleep(10)
        results['launch_start_uptime'] = kit('cut -d" " -f1 /proc/uptime')[1].strip()
        t0 = time.monotonic()
        req = urllib.request.Request(API + '/api/v1/session/launch', method='POST',
                                     data=json.dumps({'game_id': game, 'target': 'kit'}).encode(),
                                     headers={'Content-Type': 'application/json', 'Accept': 'application/json'})
        try:
            body = opener().open(req, timeout=600).read().decode()
            results['launch_rc'] = 0
        except urllib.error.HTTPError as e:
            body = e.read().decode()
            results['launch_rc'] = e.code
        results['launch_s'] = round(time.monotonic() - t0, 1)
        log(f'launch {results["launch_rc"]} {results["launch_s"]}s {body[-300:]}')
        results['launch_end_uptime'] = kit('cut -d" " -f1 /proc/uptime')[1].strip()
        time.sleep(30)
        r, results['stop_s'] = cli('stop', timeout=120)
        time.sleep(10)
        # Idle with the watch off: host CPU over 60 s.
        rc, a, _ = kit(f'awk \'{{print $14+$15}}\' /proc/$(cat {KDIR}/api.pid)/stat')
        time.sleep(60)
        rc, b, _ = kit(f'awk \'{{print $14+$15}}\' /proc/$(cat {KDIR}/api.pid)/stat')
        results['idle_host_cpu_percent'] = round((int(b) - int(a)) / 100 / 60 * 100, 1)
        log(f'idle host CPU {results["idle_host_cpu_percent"]}% of a core')
        # A library added through the API is kept across a restart.
        stop_api()
        _, before = generated()
        added = before + '\n[[libraries]]\nid = "k863s4-extra"\nsystem = "megadrive"\nroot = "/media/fat/games/k863s4-extra"\n'
        kit(f'umask 077; cat > {KDIR}/fogcast-host/config.toml', stdin=added)
        start_api(results, 'restart')
        results['restart_config'], _ = generated()
        log('restart config: ' + json.dumps(results['restart_config']))
        results['library_kept'] = 'k863s4-extra' in results['restart_config']['libraries']
    except Exception as e:  # noqa: BLE001
        log(f'ERROR {e!r}')
        results['error'] = repr(e)
    finally:
        stop_api()
        kit(f'p=$(cat {KDIR}/sampler.pid 2>/dev/null); [ -n "$p" ] && kill $p')
        if tunnel:
            tunnel.terminate()
        for name in ('samples.tsv', 'api.log'):
            (OUT / f'{TAG}-{name}').write_text(kit(f'cat {KDIR}/{name}', timeout=60)[1])
        kit(f'rm -rf {KDIR}')
        if results.get('fallback_root_before') == 'absent':
            kit('rm -rf /run/fogcast/kit-host')
        ok_lease, lease = lease_free(60)
        sw = sw_status().get('active_bundle')
        rc, left, _ = kit(f'ls -d {KDIR} 2>/dev/null; pidof fogcast-api; echo end')
        results.update({'sw_active': sw, 'lease': lease, 'leftover': left.strip()})
        (OUT / f'{TAG}-results.json').write_text(json.dumps(results, indent=1))
        log('RESULTS ' + json.dumps(results))
        if sw == BASE_SW and ok_lease and left.strip() == 'end':
            status_line(f'KIT A FREE — deano fes#863 slice 4 kit-mode check ({TAG}) done. Baseline sw {BASE_SW[:8]} still active (verified), image unchanged good {GOOD[:8]}, hand-run fogcast-api stopped and {KDIR} removed, lease {lease}. Evidence {OUT}.')
        else:
            status_line(f'KIT A HELD — deano fes#863 slice 4 ({TAG}) cleanup incomplete: sw={str(sw)[:8]} lease={lease} leftover={left.strip()!r}. Needs attention.')


if __name__ == '__main__':
    main()
