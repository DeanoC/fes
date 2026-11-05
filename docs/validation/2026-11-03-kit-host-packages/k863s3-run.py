#!/usr/bin/env python3
"""k863s3-run.py: #863 slice 3 kit check on Kit A (hardware diagnostic, not acceptance).

Claims the kit in ~/kit-status/kitX-STATUS.md, installs and activates the
scratch bundle built from the slice 3 branch (agent and runtime from main plus
slice 3), runs the branch's ARM fogcast-api by hand on the kit's loopback with
[core_packages] kit = true and private config/state on FESDATA3, and checks
that the host lists the kit's own packages without any upload. It then titles
the ST package on bundled EmuTOS, launches diskless and stops, sampling memory.
Always restores the baseline bundle, removes its own files and processes (by
PID only), checks the lease, and logs the result.
"""
import json, os, signal, subprocess, sys, time, tomllib, urllib.error, urllib.request, fcntl
from pathlib import Path

D = Path('/home/deano/fes-scratch/k863'); OUT = D / 'out863s3'; OUT.mkdir(exist_ok=True)
W = Path('/home/deano/fes/.claude/worktrees/wt863s3')
KIT = os.environ.get('K863_KIT', 'A')
KNAME = {'A': 'dev', 'B': 'kit2'}[KIT]
KHOST = {'A': 'fes-kit-a', 'B': 'fes-kit-b'}[KIT]
STATUS = Path(f'/home/deano/kit-status/kit{KIT}-STATUS.md')
LOCK = f'/home/deano/fes-hil/locks/lander-kit{KIT}.lock' if KIT == 'B' else '/home/deano/kit-status/kitA.lock'
KSSH = '/home/deano/lander-hilq/kits/kssh'
FS = '/home/deano/fes-hil/bin/fs'
CLI = str(W / 'out/native-integration-dev/fogcast')
API_BIN = str(D / 'fogcast-api-863s3')
BUNDLE = str(D / 'ks863s3/bundle.tar')
CAND = json.load(open(D / 'ks863s3/receipt.json'))['bundle']['id']
BASE_SW = os.environ.get('K863_BASE_SW') or json.load(open('/home/deano/lander-hilq/current.json'))['kit_sw']
GOOD = json.load(open('/home/deano/lander-hilq/current.json'))['image_good']
ST_PKG = '2dd2271c816ad3eb83fb73be9e47b08431eaadd9a4d12484c4d684259090bdb7'
ST_FCORE = '/home/deano/.local/share/fogcast/core-packages/' + ST_PKG + '.fcore'
HEAD = subprocess.check_output(['git', '-C', str(W), 'rev-parse', 'HEAD'], text=True).strip()
KDIR = '/run/fogcast/fesdata3/k863s3'
TAG = os.environ.get('K863_TAG', 's1')
MEMLIMIT = os.environ.get('K863_API_GOMEMLIMIT', '')
LOG = OUT / f'driver-{TAG}.log'

cfg = tomllib.loads(Path('/home/deano/.config/fogcast/config.toml').read_text())
target = next(t for t in cfg['targets'] if t['name'] == KNAME)
AGENT_URL = target['address']


def log(msg):
    line = time.strftime('%F %T %Z ') + msg
    print(line, flush=True)
    with open(LOG, 'a') as f:
        f.write(line + '\n')


def run(cmd, timeout=300, check=False, **kw):
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout, **kw)
    if check and r.returncode != 0:
        raise RuntimeError(f'{cmd[:3]} rc={r.returncode}: {r.stdout[-400:]} {r.stderr[-400:]}')
    return r


def kit(cmd, timeout=120, stdin=None):
    args = [KSSH, KIT] + (['-i'] if stdin is not None else []) + [cmd]
    r = subprocess.run(args, input=stdin, capture_output=True, text=True, timeout=timeout)
    return r.returncode, r.stdout, r.stderr


def status_line(text):
    with open(STATUS, 'a') as f:
        f.write(f'\n## {time.strftime("%H:%M %Z")} {text}\n')


def agent(path):
    o = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    req = urllib.request.Request(AGENT_URL + path, headers={'Authorization': 'Bearer ' + target['agent']})
    return json.loads(o.open(req, timeout=20).read())


def lease_free(wait=45):
    for _ in range(wait // 3):
        try:
            s, l = agent('/v1/status'), agent('/v1/kit/lease')
            if s.get('state') == 'idle' and l.get('state') == 'free':
                return True, f"{s.get('state')} {l.get('state')}"
            last = f"{s.get('state')} {l.get('state')} {l.get('owner', '')}"
        except Exception as e:  # noqa: BLE001
            last = repr(e)
        time.sleep(3)
    return False, last


def sw_status():
    r = run([FS, '--target', KNAME, '--action', 'status'], timeout=60)
    t = r.stdout
    try:
        d = json.loads(t[t.find('{'):])
    except Exception:  # noqa: BLE001
        return {}
    d.pop('receipts', None)
    return d


def wait_active(bundle, tries=36):
    s = {}
    for _ in range(tries):
        s = sw_status()
        if s.get('active_bundle') == bundle and s.get('selected') == bundle and not s.get('pending'):
            return True, s
        time.sleep(5)
    return False, s


def cli(*args, timeout=600):
    t0 = time.monotonic()
    r = run([CLI, '--api', 'http://127.0.0.1:18797', '--json', *args], timeout=timeout)
    dt = time.monotonic() - t0
    log(f'CLI {args[0]} rc={r.returncode} {dt:.1f}s {r.stdout.strip()[-600:]} {r.stderr.strip()[-300:]}')
    return r, dt


def main():
    lockf = open(LOCK, 'w')
    fcntl.flock(lockf, fcntl.LOCK_EX | fcntl.LOCK_NB)
    last = [l for l in STATUS.read_text().splitlines() if l.startswith('## ')][-1]
    if f'KIT {KIT} FREE' not in last:
        sys.exit(f'kit {KIT} not FREE: {last[:160]}')
    ok, lease = lease_free(6)
    if not ok:
        sys.exit(f'kit {KIT} lease not free: {lease}')
    s0 = sw_status()
    if s0.get('active_bundle') != BASE_SW or s0.get('pending'):
        sys.exit(f'kit {KIT} not on baseline sw {BASE_SW[:8]}')
    status_line(f'KIT {KIT} IN USE: deano (Claude session) fes#863 slice 3 kit-host packages check @ {HEAD[:8]} ({TAG}). Installs and activates scratch bundle {CAND[:8]} (slice 3 branch) over good {GOOD[:8]} (no image change, no reboot), runs the branch fogcast-api by hand on 127.0.0.1:8797 with [core_packages] kit = true and private config/state in {KDIR}, lists the kit packages, launches and stops an ST on bundled EmuTOS. Nothing is uploaded to the host; if the ST package 2dd2271c is missing it is installed into the kit durable store through the agent API and stays there (no delete exists). Shared Powerboat host untouched. Restore target: sw active {BASE_SW[:8]}, image good {GOOD[:8]}. Workdir {D}.')
    log(f'claimed; cand={CAND} base_sw={BASE_SW} head={HEAD}')
    tunnel = None
    results = {'tag': TAG, 'head': HEAD, 'bundle': CAND, 'api_gomemlimit': MEMLIMIT}
    try:
        r = run([FS, '--target', KNAME, '--action', 'install', '--bundle', BUNDLE, '--requester', 'deano-k863s3'], timeout=300)
        log(f'install rc={r.returncode} {r.stdout[-300:]}')
        r = run([FS, '--target', KNAME, '--action', 'activate', '--candidate', CAND, '--requester', 'deano-k863s3'], timeout=300)
        log(f'activate rc={r.returncode} {r.stdout[-300:]}')
        ok, s = wait_active(CAND)
        if not ok:
            raise RuntimeError(f'candidate not active: {s}')
        time.sleep(15)
        rc, rel, _ = kit('p=$(pidof fogcast-tenfoot | cut -d" " -f1); readlink /proc/$p/exe')
        rel = rel.strip().rsplit('/bin/', 1)[0]
        log(f'release dir {rel}')
        rc, out, err = kit(f'test -d /run/fogcast/fesdata3 && rm -rf {KDIR} && mkdir -m 700 {KDIR} && echo ok')
        if 'ok' not in out:
            raise RuntimeError(f'kit prep failed: {out} {err}')
        conf = (f"selected_target = '{KNAME}'\nrequest_timeout_seconds = 12\nupload_timeout_seconds = 300\n"
                f"state = '{KDIR}/state'\nstaging = '{KDIR}/staging'\n\n[firmware]\nbundled_dir = '{rel}/share/fogcast/firmware'\n\n"
                f"[core_packages]\nkit = true\n\n"
                f"[[targets]]\ntarget_id = '{target['target_id']}'\nname = '{KNAME}'\nenabled = true\naddress = 'http://127.0.0.1:8182'\nagent = '{target['agent']}'\n")
        kit(f'umask 077; cat > {KDIR}/config.toml', stdin=conf)
        kit(f'cat > {KDIR}/k863-sampler.sh && chmod 700 {KDIR}/k863-sampler.sh', stdin=(D / 'kit/k863-sampler.sh').read_text())
        kit(f'cd {KDIR} && setsid nohup ./k863-sampler.sh {KDIR}/samples.tsv > /dev/null 2>&1 < /dev/null & echo $! > {KDIR}/sampler.pid')
        time.sleep(12)
        run(['sshpass', '-p', '1', 'scp', '-O', '-q', '-o', 'StrictHostKeyChecking=no', '-o', 'UserKnownHostsFile=/dev/null', '-o', 'PubkeyAuthentication=no', '-o', 'PreferredAuthentications=password', API_BIN, f'root@{KHOST}:{KDIR}/fogcast-api'], check=True)
        api_bin = f'{KDIR}/fogcast-api'
        log(f'branch fogcast-api from {API_BIN}')
        env = f'GOMEMLIMIT={MEMLIMIT} ' if MEMLIMIT else ''
        kit(f'cd {KDIR} && {env}setsid nohup {api_bin} -headless -listen 127.0.0.1:8797 -config {KDIR}/config.toml > {KDIR}/api.log 2>&1 < /dev/null & echo $! > {KDIR}/api.pid')
        t_api = time.monotonic()
        tunnel = subprocess.Popen(['sshpass', '-p', '1', 'ssh', '-N', '-o', 'ConnectTimeout=8', '-o', 'StrictHostKeyChecking=no', '-o', 'UserKnownHostsFile=/dev/null', '-o', 'LogLevel=ERROR', '-o', 'PubkeyAuthentication=no', '-o', 'PreferredAuthentications=password', '-L', '18797:127.0.0.1:8797', f'root@{KHOST}'])
        for _ in range(60):
            try:
                urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://127.0.0.1:18797/api/v1/health', timeout=3).read()
                break
            except Exception:  # noqa: BLE001
                time.sleep(1)
        results['api_ready_s'] = round(time.monotonic() - t_api, 1)
        log(f'api ready after {results["api_ready_s"]}s')
        time.sleep(20)
        rc, out, err = kit('ls /usr/share/mister-runtime/core-packages | tr "\\n" " "; echo; ls /run/fogcast/fesdata3/core-library/archives | tr "\\n" " "')
        factory_ids, durable_files = (out.strip().split('\n') + ['', ''])[:2]
        factory_ids = sorted(factory_ids.split())
        durable_ids = sorted(n[:-6] for n in durable_files.split() if n.endswith('.fcore'))
        t0 = time.monotonic()
        unavailable = 0
        while True:
            try:
                listing = json.loads(urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://127.0.0.1:18797/api/v1/core-packages', timeout=300).read())
                break
            except urllib.error.HTTPError as e:
                if e.code != 503 or time.monotonic() - t0 > 300:
                    raise
                unavailable += 1
                time.sleep(1)
        results['list_s'] = round(time.monotonic() - t0, 1)
        results['list_unavailable_polls'] = unavailable
        results['api_open_since_ready_s'] = round(time.monotonic() - t_api, 1)
        packages = listing if isinstance(listing, list) else listing.get('packages', listing)
        listed = {p['package_id']: p['descriptor']['core']['id'] for p in packages}
        results.update({'factory_ids': factory_ids, 'durable_ids': durable_ids, 'listed': listed,
                        'listed_matches_kit': sorted(listed) == sorted(set(factory_ids) | set(durable_ids))})
        log(f'packages: factory={len(factory_ids)} durable={len(durable_ids)} listed={len(listed)} match={results["listed_matches_kit"]} in {results["list_s"]}s {listed}')
        if ST_PKG not in listed:
            # Install into the kit's durable store through the agent's own API,
            # as a kit-installed package arrives. The host gets no upload.
            body = Path(ST_FCORE).read_bytes()
            req = urllib.request.Request(AGENT_URL + '/v1/core-packages', method='POST', data=body,
                                         headers={'Authorization': 'Bearer ' + target['agent'], 'Content-Type': 'application/octet-stream'})
            t0 = time.monotonic()
            resp = json.loads(urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=600).read())
            results['agent_install_s'] = round(time.monotonic() - t0, 1)
            results['agent_install'] = {'package_id': resp.get('package', {}).get('package_id'), 'created': resp.get('created')}
            log(f'agent durable install {results["agent_install"]} in {results["agent_install_s"]}s')
            listing = json.loads(urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://127.0.0.1:18797/api/v1/core-packages', timeout=300).read())
            packages = listing if isinstance(listing, list) else listing.get('packages', listing)
            listed = {p['package_id']: p['descriptor']['core']['id'] for p in packages}
            results['listed_after_agent_install'] = listed
            log(f'host lists after agent install: {listed}')
        st = ST_PKG if ST_PKG in listed else next((i for i, c in sorted(listed.items()) if c == 'fes.atari-st'), None)
        results['st_package'] = st
        results['st_source'] = 'factory' if st in factory_ids else 'durable' if st in durable_ids else None
        if not st:
            raise RuntimeError('no ST package on the kit')
        rc, before, _ = kit('ls /run/fogcast/fesdata3/core-library/archives | wc -l')
        r, _ = cli('core-entry', 'ST k863s3', st)
        game = json.loads(r.stdout).get('game_id') or json.loads(r.stdout).get('entry', {}).get('game_id')
        results['game_id'] = game
        rc, out, err = kit(f'ls {rel}/share/fogcast/firmware 2>&1 | tr "\\n" " "')
        log(f'firmware dir: {out.strip()}')
        try:
            fw = urllib.request.build_opener(urllib.request.ProxyHandler({})).open('http://127.0.0.1:18797/api/v1/library/firmware/bundled', timeout=20).read().decode()
        except Exception as e:  # noqa: BLE001
            fw = repr(e)
        log(f'bundled firmware: {fw[:400]}')
        results['bundled_firmware'] = fw[:2000]
        time.sleep(15)
        mark = run([KSSH, KIT, 'cut -d" " -f1 /proc/uptime']).stdout.strip()
        results['launch_start_uptime'] = mark
        # The CLI's launch omits the target, which asks mesh placement; name it
        # as Lander's HIL does.
        t0 = time.monotonic()
        req = urllib.request.Request('http://127.0.0.1:18797/api/v1/session/launch', method='POST',
                                     data=json.dumps({'game_id': game, 'target': KNAME}).encode(),
                                     headers={'Content-Type': 'application/json', 'Accept': 'application/json'})
        try:
            body = urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=600).read().decode()
            results['launch_rc'] = 0
        except urllib.error.HTTPError as e:
            body = e.read().decode()
            results['launch_rc'] = e.code
        results['launch_s'] = round(time.monotonic() - t0, 1)
        log(f'launch {results["launch_rc"]} {results["launch_s"]}s {body[-600:]}')
        results['launch_end_uptime'] = run([KSSH, KIT, 'cut -d" " -f1 /proc/uptime']).stdout.strip()
        time.sleep(30)
        r, results['stop_s'] = cli('stop', timeout=120)
        time.sleep(20)
        rc, after, _ = kit('ls /run/fogcast/fesdata3/core-library/archives | wc -l')
        results['durable_count_before_after'] = [before.strip(), after.strip()]
    except Exception as e:  # noqa: BLE001
        log(f'ERROR {e!r}')
        results['error'] = repr(e)
    finally:
        rc, out, err = kit(f'for f in api sampler; do p=$(cat {KDIR}/$f.pid 2>/dev/null); [ -n "$p" ] && kill $p; done; sleep 2; ls {KDIR}')
        log(f'stopped own processes: {out.strip()}')
        if tunnel:
            tunnel.terminate()
        for name in ('samples.tsv', 'api.log'):
            rc, out, err = kit(f'cat {KDIR}/{name}', timeout=60)
            (OUT / f'{TAG}-{name}').write_text(out)
        kit(f'rm -rf {KDIR}; ls -d {KDIR} 2>&1')
        restored = False
        for i in range(3):
            r = run([FS, '--target', KNAME, '--action', 'activate', '--candidate', BASE_SW, '--requester', 'deano-k863s3-restore'], timeout=300)
            log(f'restore activate rc={r.returncode} {r.stdout[-200:]}')
            ok, s = wait_active(BASE_SW)
            if ok:
                restored = True
                break
        ok_lease, lease = lease_free(60)
        rc, left, _ = kit(f'ls -d {KDIR} 2>/dev/null; pidof fogcast-api; echo end')
        results.update({'restored_sw': restored, 'lease': lease, 'leftover': left.strip()})
        (OUT / f'{TAG}-results.json').write_text(json.dumps(results, indent=1))
        log('RESULTS ' + json.dumps(results))
        if restored and ok_lease and left.strip() == 'end':
            status_line(f'KIT {KIT} FREE — deano fes#863 slice 3 kit-host packages check ({TAG}) done. Restored sw active {BASE_SW[:8]} (verified), image unchanged good {GOOD[:8]}, kit-hosted fogcast-api stopped and {KDIR} removed, lease {lease}. ST package 2dd2271c left installed in the durable core library (agent install; the store has no delete). Evidence {OUT}.')
        else:
            status_line(f'KIT {KIT} HELD — deano fes#863 slice 3 ({TAG}) restore incomplete: sw restored={restored} lease={lease} leftover={left.strip()!r}. Needs attention.')


if __name__ == '__main__':
    main()
