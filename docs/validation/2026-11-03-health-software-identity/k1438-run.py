#!/usr/bin/env python3
"""k1438-run.py: fes#1438 idle CPU and wire check on Kit A (hardware diagnostic, not acceptance).

Claims Kit A in ~/kit-status/kitA-STATUS.md. Measures idle supervisor/agent CPU,
the authenticated health size, and the bytes the shared Powerboat host receives
from the kit agent, first on the baseline bundle, then with the scratch bundle
built from the #1438 branch installed and active. Always restores the baseline
bundle, checks the lease, and logs the result. Never touches the shared host.
"""
import fcntl, json, os, re, subprocess, sys, time, tomllib, urllib.request
from pathlib import Path

D = Path('/home/deano/fes-scratch/k863'); OUT = D / 'out1438'; OUT.mkdir(exist_ok=True)
KIT, KNAME, KADDR = 'A', 'dev', '192.168.10.84'
STATUS = Path('/home/deano/kit-status/kitA-STATUS.md')
LOCK = str(D / 'k863-kitA.lock')
KSSH = '/home/deano/lander-hilq/kits/kssh'
FS = '/home/deano/fes-hil/bin/fs'
BUNDLE = str(D / 'ks1438/bundle.tar')
CAND = json.load(open(D / 'ks1438/receipt.json'))['bundle']['id']
BASE_SW = '8917374885b2ffa1af5510acb41f34b3e749ea25ff9a3eff69a482b9207aab0f'
GOOD = 'f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4'
HEAD = subprocess.check_output(['git', '-C', '/home/deano/fes/.claude/worktrees/wt1438', 'rev-parse', 'HEAD'], text=True).strip()
WINDOW = int(os.environ.get('K1438_WINDOW', '120'))
TAG = os.environ.get('K1438_TAG', 'h1')
LOG = OUT / f'driver-{TAG}.log'

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


def kit(cmd, timeout=300):
    r = run([KSSH, KIT, cmd], timeout=timeout)
    return r.returncode, r.stdout, r.stderr


def status_line(text):
    with open(STATUS, 'a') as f:
        f.write(f'\n## {time.strftime("%H:%M %Z")} {text}\n')


def agent_raw(path, auth=True):
    o = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    headers = {'Authorization': 'Bearer ' + target['agent']} if auth else {}
    return o.open(urllib.request.Request(AGENT_URL + path, headers=headers), timeout=20).read()


def agent(path):
    return json.loads(agent_raw(path))


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
    r = run([FS, '--target', KNAME, '--action', 'status'], timeout=60)
    t = r.stdout
    try:
        d = json.loads(t[t.find('{'):])
    except Exception:  # noqa: BLE001
        return {}
    d['receipt_count'] = len(d.pop('receipts', None) or {})
    return d


def wait_active(bundle, tries=36):
    s = {}
    for _ in range(tries):
        s = sw_status()
        if s.get('active_bundle') == bundle and s.get('selected') == bundle and not s.get('pending'):
            return True, s
        time.sleep(5)
    return False, s


def host_bytes():
    """Bytes the shared host has sent to and received from the kit agent, per local port."""
    out = run(['ss', '-tni', 'dst', f'{KADDR}:8182']).stdout
    conns, port = {}, None
    for line in out.splitlines():
        m = re.search(r':(\d+)\s+' + re.escape(KADDR) + r':8182', line)
        if m:
            port = m.group(1)
            continue
        if port:
            sent = re.search(r'bytes_sent:(\d+)', line); recv = re.search(r'bytes_received:(\d+)', line)
            conns[port] = (int(sent.group(1)) if sent else 0, int(recv.group(1)) if recv else 0)
            port = None
    return conns


def measure(label):
    health = agent_raw('/v1/health')
    software = json.loads(health).get('software') or {}
    m = {'health_auth_bytes': len(health), 'health_anon_bytes': len(agent_raw('/v1/health', auth=False)),
         'health_software_keys': sorted(software), 'window_s': WINDOW}
    a = host_bytes(); t0 = time.monotonic()
    rc, out, err = kit(f'sh /tmp/k863-idlecpu.sh {WINDOW}', timeout=WINDOW + 60)
    b = host_bytes(); dt = time.monotonic() - t0
    sent = sum(v[0] - a.get(k, (0, 0))[0] for k, v in b.items())
    recv = sum(v[1] - a.get(k, (0, 0))[1] for k, v in b.items())
    m.update({'host_sent_bytes_per_s': round(sent / dt), 'host_recv_bytes_per_s': round(recv / dt),
              'host_connections': sorted(b), 'idlecpu': out.strip().splitlines(), 'idlecpu_rc': rc})
    log(f'MEASURE {label} ' + json.dumps(m))
    return m


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
        sys.exit(f'kit A not on baseline sw {BASE_SW[:8]}: {s0}')
    status_line(f'KIT A IN USE: deano (Claude session) fes#1438 idle CPU check @ {HEAD[:8]} ({TAG}). Measures idle supervisor/agent CPU on baseline, then installs and activates scratch bundle {CAND[:8]} (#1438 branch) over good {GOOD[:8]} (no image change, no reboot) and measures again. Shared Powerboat host untouched. Restore target: sw active {BASE_SW[:8]}, image good {GOOD[:8]}. Workdir {D}.')
    log(f'claimed; cand={CAND} base_sw={BASE_SW} head={HEAD}')
    results = {'tag': TAG, 'head': HEAD, 'bundle': CAND, 'baseline_sw': BASE_SW, 'receipts_before': s0.get('receipt_count')}
    try:
        r = run([KSSH, KIT, '-i', 'cat > /tmp/k863-idlecpu.sh'], timeout=60, input=(D / 'kit/k863-idlecpu.sh').read_text())
        if r.returncode != 0:
            raise RuntimeError(f'idlecpu upload failed: {r.stderr[-200:]}')
        results['baseline'] = measure('baseline')
        r = run([FS, '--target', KNAME, '--action', 'install', '--bundle', BUNDLE, '--requester', 'deano-k1438'], timeout=300)
        log(f'install rc={r.returncode} {r.stdout[-300:]}')
        r = run([FS, '--target', KNAME, '--action', 'activate', '--candidate', CAND, '--requester', 'deano-k1438'], timeout=300)
        log(f'activate rc={r.returncode} {r.stdout[-300:]}')
        ok, s = wait_active(CAND)
        if not ok:
            raise RuntimeError(f'candidate not active: {s}')
        ok, lease = lease_free(60)
        log(f'candidate active; lease {lease}; settling 30 s')
        time.sleep(30)
        results['candidate'] = measure('candidate')
    except Exception as e:  # noqa: BLE001
        log(f'ERROR {e!r}')
        results['error'] = repr(e)
    finally:
        kit('rm -f /tmp/k863-idlecpu.sh /tmp/k863-a /tmp/k863-b')
        restored = False
        for _ in range(3):
            if sw_status().get('active_bundle') == BASE_SW:
                restored, _s = wait_active(BASE_SW, tries=2)
                if restored:
                    break
            r = run([FS, '--target', KNAME, '--action', 'activate', '--candidate', BASE_SW, '--requester', 'deano-k1438-restore'], timeout=300)
            log(f'restore activate rc={r.returncode} {r.stdout[-200:]}')
            restored, _s = wait_active(BASE_SW)
            if restored:
                break
        ok_lease, lease = lease_free(60)
        rc, left, _ = kit('ls /tmp/k863-* 2>/dev/null; echo end')
        results.update({'restored_sw': restored, 'lease': lease, 'leftover': left.strip()})
        (OUT / f'{TAG}-results.json').write_text(json.dumps(results, indent=1))
        log('RESULTS ' + json.dumps(results))
        if restored and ok_lease and left.strip() == 'end':
            status_line(f'KIT A FREE — deano fes#1438 idle CPU check ({TAG}) done. Restored sw active {BASE_SW[:8]} (verified), image unchanged good {GOOD[:8]}, lease {lease}. Evidence {OUT}.')
        else:
            status_line(f'KIT A HELD — deano fes#1438 ({TAG}) restore incomplete: sw restored={restored} lease={lease} leftover={left.strip()!r}. Needs attention.')


if __name__ == '__main__':
    main()
