#!/usr/bin/env python3
"""k1444-run.py: fes#1444 supervisor receipt bound, idle CPU on Kit A (hardware diagnostic, not acceptance).

Claims Kit A. Measures idle supervisor/agent CPU on the image's own supervisor,
then stops the software group, bind-mounts the #1444 branch's fes-kit-software
(a tmpfs copy) over /usr/sbin/fes-kit-software, starts it, and measures again.
The new supervisor prunes the receipt ledger when it opens state, so state.json
is backed up to Powerboat first. Restore: stop, unmount, start the image's
supervisor, verify its binary, the baseline bundle, the agent and a free lease.
No image, boot-config or software-selection change. Shared host untouched.
"""
import fcntl, hashlib, json, os, re, subprocess, sys, time, tomllib, urllib.request
from pathlib import Path

D = Path('/home/deano/fes-scratch/k863'); OUT = D / 'out1444'; OUT.mkdir(exist_ok=True)
KIT, KNAME, KADDR = 'A', 'dev', '192.168.10.84'
STATUS = Path('/home/deano/kit-status/kitA-STATUS.md')
LOCK = str(D / 'k863-kitA.lock')
KSSH = '/home/deano/lander-hilq/kits/kssh'
FS = '/home/deano/fes-hil/bin/fs'
NEW_BIN = Path('/home/deano/fes/.claude/worktrees/wt1444/sources/FogCast/bin/fes-kit-software-linux-armv7')
NEW_SHA = hashlib.sha256(NEW_BIN.read_bytes()).hexdigest()
IMAGE_SHA = '901a51bfb7a9728fc480e5399e6dafeaba01193bd102aee68c6c3851395e41af'
BASE_SW = '8917374885b2ffa1af5510acb41f34b3e749ea25ff9a3eff69a482b9207aab0f'
GOOD = 'f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4'
HEAD = subprocess.check_output(['git', '-C', '/home/deano/fes/.claude/worktrees/wt1444', 'rev-parse', 'HEAD'], text=True).strip()
KDIR = '/run/fogcast/k1444'
STATE = '/run/fogcast/fesdata3/software-releases/control/state.json'
SV = '/usr/sbin/fes-kit-software'
WINDOW, TAG = 120, os.environ.get('K1444_TAG', 's1')
RESTORE_STATE = os.environ.get('K1444_RESTORE_STATE') == '1'
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
    return subprocess.run(cmd, capture_output=True, timeout=timeout, **kw)


def kit(cmd, timeout=300):
    r = run([KSSH, KIT, cmd], timeout=timeout, text=True)
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
    try:
        d = agent('/v1/kit/software')
    except Exception as e:  # noqa: BLE001
        return {'error': repr(e)}
    d['receipt_count'] = len(d.pop('receipts', None) or {})
    return d


def running_supervisor():
    rc, out, _ = kit(f'p=$(pidof fes-kit-software); echo "pid=$p"; s=""; [ -n "$p" ] && [ "${{p#* }}" = "$p" ] && s=$(sha256sum /proc/$p/exe | cut -d" " -f1); echo "sha=$s"; echo "mounts=$(grep -c " {SV} " /proc/mounts)"')
    d = dict(l.split('=', 1) for l in out.splitlines() if '=' in l)
    return {'pid': d.get('pid', ''), 'sha': d.get('sha', ''), 'bind_mounts': d.get('mounts', '')}


def save_log(name):
    rc, out, _ = kit('tail -60 /run/fogcast/software/supervisor.log')
    (OUT / f'{TAG}-supervisor-{name}.log').write_text(out)


def wait_ready(tries=40):
    s = {}
    for _ in range(tries):
        s = sw_status()
        if s.get('active_bundle') == BASE_SW and s.get('selected') == BASE_SW and not s.get('pending') and not s.get('storage_error') and not s.get('service_fault'):
            ok, lease = lease_free(9)
            if ok:
                return True, s
        time.sleep(5)
    return False, s


def host_bytes():
    out = run(['ss', '-tni', 'dst', f'{KADDR}:8182'], text=True).stdout
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
    m = {'health_auth_bytes': len(agent_raw('/v1/health')), 'software_status_bytes': len(agent_raw('/v1/kit/software')),
         'receipts': sw_status().get('receipt_count'), 'supervisor': running_supervisor(), 'window_s': WINDOW}
    a = host_bytes(); t0 = time.monotonic()
    rc, out, err = kit(f'sh {KDIR}/k863-idlecpu.sh {WINDOW}', timeout=WINDOW + 60)
    b = host_bytes(); dt = time.monotonic() - t0
    m.update({'host_sent_bytes_per_s': round(sum(v[0] - a.get(k, (0, 0))[0] for k, v in b.items()) / dt),
              'host_recv_bytes_per_s': round(sum(v[1] - a.get(k, (0, 0))[1] for k, v in b.items()) / dt),
              'idlecpu': out.strip().splitlines(), 'idlecpu_rc': rc})
    log(f'MEASURE {label} ' + json.dumps(m))
    return m


def stop_supervisor():
    # The init script's stop waits for the supervisor's children to exit.
    rc, out, err = kit('/etc/init.d/S40fes-kit-software stop', timeout=180)
    log(f'stop rc={rc} {out.strip()[-200:]} {err.strip()[-200:]}')
    rc, out, _ = kit('pidof fes-kit-software mister-agent mister-runtime fogcast-tenfoot; echo end')
    if out.strip() != 'end':
        raise RuntimeError(f'software group still running after stop: {out.strip()}')


def set_mount(mount):
    if mount:
        rc, out, err = kit(f'mount --bind {KDIR}/fes-kit-software {SV} && sha256sum {SV} | cut -d" " -f1')
        want = NEW_SHA
    else:
        rc, out, err = kit(f'while grep -q " {SV} " /proc/mounts; do umount {SV} || break; done; sha256sum {SV} | cut -d" " -f1')
        want = IMAGE_SHA
    log(f'{"mount" if mount else "umount"} rc={rc} {out.strip()} {err.strip()[-200:]}')
    if out.strip() != want:
        raise RuntimeError(f'{SV} is {out.strip()}, want {want}')


def start_supervisor():
    rc, out, err = kit('/etc/init.d/S40fes-kit-software start', timeout=180)
    log(f'start rc={rc} {out.strip()[-200:]} {err.strip()[-200:]}')


def restore_state(backup):
    """Put the pre-run ledger back only if nothing else in the state moved."""
    before = json.loads(backup.read_bytes())
    rc, out, _ = kit(f'cat {STATE}')
    now = json.loads(out)
    keys = ('version', 'generation', 'selected', 'previous', 'pending', 'unreleased')
    if any(before.get(k) != now.get(k) for k in keys):
        log(f'state moved during the run; leaving the pruned ledger: {[k for k in keys if before.get(k) != now.get(k)]}')
        return False
    tmp = STATE[:-len('state.json')] + 'state.tmp'
    r = run([KSSH, KIT, '-i', f'cat > {tmp} && chmod 600 {tmp} && sync && mv {tmp} {STATE} && sync && sha256sum {STATE} | cut -d" " -f1'], timeout=60, input=backup.read_bytes())
    ok = r.stdout.decode().strip() == hashlib.sha256(backup.read_bytes()).hexdigest()
    log(f'restored pre-run state.json ok={ok}')
    return ok


def main():
    lockf = open(LOCK, 'w')
    fcntl.flock(lockf, fcntl.LOCK_EX | fcntl.LOCK_NB)
    last = [l for l in STATUS.read_text().splitlines() if l.startswith('## ')][-1]
    if 'KIT A FREE' not in last:
        sys.exit(f'kit A not FREE: {last[:160]}')
    ok, lease = lease_free(6)
    s0 = sw_status()
    sup0 = running_supervisor()
    if not ok or s0.get('active_bundle') != BASE_SW or s0.get('pending') or sup0['sha'] != IMAGE_SHA or sup0['bind_mounts'] != '0':
        sys.exit(f'kit A not at baseline: lease={lease} sw={s0.get("active_bundle", "")[:8]} supervisor={sup0}')
    status_line(f'KIT A IN USE: deano (Claude session) fes#1444 supervisor idle CPU check @ {HEAD[:8]} ({TAG}). Deano OK for a runtime bind-mount: measures on the image supervisor, then stops the software group, bind-mounts the branch fes-kit-software {NEW_SHA[:8]} from {KDIR} (tmpfs) over {SV}, starts it and measures again. The new supervisor prunes receipts in state.json (backup on Powerboat). No image, boot-config or selection change; sw stays {BASE_SW[:8]}. Shared Powerboat host untouched. Restore: stop, umount, start the image supervisor {IMAGE_SHA[:8]}, verify. Workdir {D}.')
    log(f'claimed; new={NEW_SHA} image={IMAGE_SHA} head={HEAD}')
    results = {'tag': TAG, 'head': HEAD, 'new_supervisor': NEW_SHA, 'image_supervisor': IMAGE_SHA, 'baseline_sw': BASE_SW, 'receipts_before': s0.get('receipt_count')}
    mounted = False
    try:
        r = run([KSSH, KIT, f'cat {STATE}'], timeout=60)
        if r.returncode != 0 or not r.stdout.startswith(b'{'):
            raise RuntimeError('state backup failed')
        backup = OUT / f'{TAG}-state-before.json'
        backup.write_bytes(r.stdout); backup.chmod(0o600)
        log(f'state backup {len(r.stdout)} B sha256 {hashlib.sha256(r.stdout).hexdigest()}')
        kit(f'mkdir -p {KDIR}')
        for src, dst in ((D / 'kit/k863-idlecpu.sh', f'{KDIR}/k863-idlecpu.sh'), (NEW_BIN, f'{KDIR}/fes-kit-software')):
            r = run([KSSH, KIT, '-i', f'cat > {dst}'], timeout=120, input=src.read_bytes())
            if r.returncode != 0:
                raise RuntimeError(f'upload {src.name} failed')
        rc, out, _ = kit(f'chmod 755 {KDIR}/fes-kit-software; sha256sum {KDIR}/fes-kit-software | cut -d" " -f1')
        if out.strip() != NEW_SHA:
            raise RuntimeError(f'uploaded supervisor hash {out.strip()}')
        save_log('claim')
        results['baseline'] = measure('baseline')
        mounted = True
        stop_supervisor()
        set_mount(True)
        start_supervisor()
        ok, s = wait_ready()
        sup = running_supervisor()
        save_log('new-start')
        log(f'new supervisor ready={ok} {sup} receipts={s.get("receipt_count")}')
        if not ok or sup['sha'] != NEW_SHA:
            raise RuntimeError(f'new supervisor not ready: {s} {sup}')
        if not s.get('receipt_count') or s['receipt_count'] >= 100:
            raise RuntimeError(f'ledger not pruned at start: {s.get("receipt_count")} receipts')
        time.sleep(30)
        results['candidate'] = measure('candidate')
    except Exception as e:  # noqa: BLE001
        log(f'ERROR {e!r}')
        results['error'] = repr(e)
    finally:
        restored = False
        try:
            if mounted or running_supervisor()['bind_mounts'] not in ('0', ''):
                stop_supervisor()
                set_mount(False)
                if RESTORE_STATE:
                    results['state_restored'] = restore_state(OUT / f'{TAG}-state-before.json')
                start_supervisor()
            ok, s = wait_ready()
            if not ok and not running_supervisor()['pid']:
                log('image supervisor not running; starting it again')
                start_supervisor()
                ok, s = wait_ready()
            sup = running_supervisor()
            restored = ok and sup['sha'] == IMAGE_SHA and sup['bind_mounts'] == '0'
            results['restored_supervisor'] = sup
            results['receipts_after'] = s.get('receipt_count')
            save_log('restore')
        except Exception as e:  # noqa: BLE001
            log(f'restore ERROR {e!r}')
        if restored:
            kit(f'rm -rf {KDIR}')
        ok_lease, lease = lease_free(30)
        rc, left, _ = kit(f'ls -d {KDIR} 2>/dev/null; echo end')
        results.update({'restored': restored, 'lease': lease, 'leftover': left.strip()})
        (OUT / f'{TAG}-results.json').write_text(json.dumps(results, indent=1))
        log('RESULTS ' + json.dumps(results))
        if restored and ok_lease and left.strip() == 'end':
            status_line(f'KIT A FREE — deano fes#1444 supervisor check ({TAG}) done. Image supervisor {IMAGE_SHA[:8]} running again with no bind mount (verified), sw active {BASE_SW[:8]}, image unchanged good {GOOD[:8]}, lease {lease}. state.json receipts {results.get("receipts_before")} -> {results.get("receipts_after")} (pre-run backup {OUT}/{TAG}-state-before.json). Evidence {OUT}.')
        else:
            status_line(f'KIT A HELD — deano fes#1444 ({TAG}) restore incomplete: restored={restored} lease={lease} leftover={left.strip()!r}. Needs attention.')


if __name__ == '__main__':
    main()
