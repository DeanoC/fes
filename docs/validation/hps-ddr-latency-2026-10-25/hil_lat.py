# hil_lat: fes.ramtest latency windows on kit B through a private host (Claude, zbl-lat, FES ramtest latency PR).
# Core-only run: no bundle install, no image change. Import the sealed .fcore into a private host, launch it,
# read the LAT rows from HDMI with Linux idle, then with a CPU and memory load on the HPS (busybox dd/cat
# started under the zbl-latload name and killed by PID), stop, and check the kit is back to its baseline.
# usage: python3 hil_lat.py <fcore> <package_id> <out_dir>
from pathlib import Path
import sys, json, urllib.request, urllib.error, time, hashlib, datetime, fcntl, subprocess, os, signal
signal.signal(signal.SIGTERM, lambda *_: sys.exit('terminated'))
WT = Path.home() / 'fes/.claude/worktrees/zen-bell-09f7a3'
sys.path.insert(0, str(WT / 'scripts'))
import package_acceptance_isolated as iso
from diagnostic_identity import observe_health
sys.path.insert(0, str(Path(__file__).resolve().parent))
import ocr_ramtest

fcore, package_id, out = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
out.mkdir()
FR = out / 'fr'; FR.mkdir()
VID = '/dev/v4l/by-id/usb-ASUS_4KPRO_802B003090700329-video-index0'
KA = str(Path(__file__).resolve().parent / 'kb')
FONT = WT / 'sources/misteross/cores/fes-ramtest/rtl/ram_font.v'
TID = '67c5f4e2-d288-49bb-9049-39ecf39cf6f6'
BUNDLE = '35d584898755b4c84b68cccf2e08838b4a371cbe550b5af457481f3d5e776f9b'
BASE = '4db7486452057d8a77479125c8ee8e9a27db4f378a9d9735900e90f6f3fe7a26'
HOSTBIN = Path.home() / '.local/lib/fogcast/fogcast-api-58f029e014c9f9ad491e03b8270667c2c22c316d'
IMAGE = 'sha256:44b7b0c76728be2a697e5cd7150a1ef7ddfda6c880212d11f9fef43d950a2af6'
FONT_TABLE = ocr_ramtest.load_font(FONT)

with (Path.home() / 'kit-status/kitB-STATUS.md').open('r') as claim:
    fcntl.flock(claim, fcntl.LOCK_SH)
    heading = [l for l in claim.read().splitlines() if l.startswith('## ')][-1]
    assert 'KIT B IN USE: Claude' in heading and 'zbl-lat' in heading, 'claim not ours; no kit contact'

opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
def log(*a): print(datetime.datetime.now().strftime('%T'), *a, flush=True)
def req(origin, path, name, data=None, token='', timeout=45, method=None):
    h = {}
    if token: h['Authorization'] = 'Bearer ' + token
    if isinstance(data, dict): data = json.dumps(data).encode(); h['Content-Type'] = 'application/json'
    elif data is not None: h['Content-Type'] = 'application/octet-stream'
    try:
        with opener.open(urllib.request.Request(origin + path, data=data, headers=h, method=method), timeout=timeout) as r:
            code = r.status; raw = r.read(32 << 20)
    except urllib.error.HTTPError as e: code = e.code; raw = e.read(32 << 20)
    (out / (name + '.json')).write_bytes(raw); log(name, code)
    value = json.loads(raw) if raw else {}
    if code >= 400: raise RuntimeError(name + ': ' + str(value)[:400])
    return value
def kit(cmd, name):
    r = subprocess.run([KA, cmd], capture_output=True, text=True, timeout=60)
    (out / (name + '.txt')).write_text(r.stdout + r.stderr + f'\nexit {r.returncode}\n'); log('kit', name, r.returncode)
    return r.stdout
def grab(n):
    path = FR / (n + '.png')
    for _ in range(5):
        r = subprocess.run(['timeout', '-k', '5', '20', 'ffmpeg', '-loglevel', 'error', '-y', '-f', 'v4l2', '-i', VID,
                            '-vf', 'select=gte(n\\,15)', '-frames:v', '1', str(path)])
        if r.returncode == 0 and path.exists() and path.stat().st_size > 0: return path
        time.sleep(2)
    log('grab FAILED', n); return None
def screen(n):
    path = grab(n)
    if path is None: return None
    rows = ocr_ramtest.read_screen(str(path), FONT_TABLE)
    (FR / (n + '.txt')).write_text('\n'.join(rows) + '\n')
    return rows
LAT_ROWS = {'P0': 17, 'P1': 18, 'P2': 19, 'SD': 20}
def latency(rows):
    got = {}
    for name, index in LAT_ROWS.items():
        line = rows[index].ljust(40)
        if not line.startswith(name + ' F'): return None
        fields = [line[4:7], line[8:11], line[12:15], line[16:20], line[22:25], line[26:29], line[30:33], line[34:38]]
        try: got[name] = [int(f) for f in fields]
        except ValueError: return None
    return got
def series(tag, count, gap):
    samples = []
    for i in range(count):
        t = time.time()
        rows = screen('%s-%02d' % (tag, i))
        values = latency(rows) if rows else None
        samples.append({'i': i, 'time': datetime.datetime.now().isoformat(timespec='seconds'), 'latency': values,
                        'status': rows[:16] if rows else None})
        log(tag, i, json.dumps(values))
        time.sleep(max(0, gap - (time.time() - t)))
    (out / (tag + '.json')).write_text(json.dumps(samples, indent=1))
    return samples

R = {}
# The shared config selects kit A; load kit B from a private 0600 copy that selects it, removed straight after.
cfg = out / 'config-kitB.toml'; src = (Path.home() / '.config/fogcast/config.toml').read_text()
assert src.count("selected_target = 'dev'") == 1; cfg.touch(mode=0o600)
cfg.write_text(src.replace("selected_target = 'dev'", "selected_target = 'kit2'"))
try: target = iso._load_private_target(cfg, TID)
finally: cfg.unlink()
home = iso.prepare_private_home(out, target)
load_pids = []
status = req(target.address, '/v1/status', 'before-status', token=target.agent); assert status['state'] == 'idle'
lease = req(target.address, '/v1/kit/lease', 'before-lease', token=target.agent); assert lease['state'] == 'free'
kit('cat /run/mister-runtime-boot-hps-ddr; echo; busybox devmem 0xFFC25080 32; uptime; free; nproc; df -h /tmp; '
    'busybox --list | grep -x -e dd -e cat -e setsid -e top; cat /proc/cpuinfo | grep -i -e processor -e "model name" | head', 'before-kit')
screen('00-before')
binary = iso.snapshot_binary(HOSTBIN, out, hashlib.sha256(HOSTBIN.read_bytes()).hexdigest())
runtime = iso.DockerRuntime('docker', 120); name = 'claude-zbl-lat-' + str(int(time.time())); started = False
try:
    runtime.start(iso.container_command('docker', IMAGE, name, binary, home, 0), name, (target.agent,)); started = True
    origin = None
    for _ in range(30):
        port = iso._listen_port(runtime.logs(name), 0)
        if port: origin = 'http://127.0.0.1:' + str(port); break
        time.sleep(1)
    assert origin
    for _ in range(20):
        health = req(origin, '/api/v1/health', 'host-health')
        try: observe_health(health, TID, BUNDLE, BASE); break
        except ValueError as e: log('health wait', e); time.sleep(3)
    observe_health(health, TID, BUNDLE, BASE)
    data = fcore.read_bytes(); log('archive', hashlib.sha256(data).hexdigest(), len(data))
    imported = req(origin, '/api/v1/core-packages', 'import', data); assert imported['package_id'] == package_id, imported
    R['compat'] = req(origin, '/api/v1/core-packages/' + package_id + '/compatibility', 'compat', b'')
    entry = req(origin, '/api/v1/library/core-entries', 'entry', {'title': 'RAM tester latency zbl-lat', 'package_id': package_id})
    game = entry['game_id']
    try:
        launch = req(origin, '/api/v1/session/launch', 'launch', {'game_id': game}, timeout=150)
        R['launch'] = launch.get('state') == 'active' and (launch.get('core_package') or {}).get('package_id') == package_id
        t0 = time.time()
        kit('busybox devmem 0xFFC25080 32; tail -n 40 /var/log/messages 2>/dev/null | grep -i -e hps_ddr -e mister-runtime | tail -n 10', 'running-kit')
        # The full SDRAM scan takes about two minutes at 100 MHz; latency starts after every scan ends.
        ready = None
        while time.time() - t0 < 480:
            time.sleep(20)
            rows = screen('05-wait-%03d' % int(time.time() - t0))
            ready = latency(rows) if rows else None
            log('wait', int(time.time() - t0), json.dumps(ready))
            if ready and all(v[3] > 0 for v in ready.values()): break
        R['ready_after_s'] = int(time.time() - t0)
        time.sleep(10)
        kit('cat /proc/loadavg; top -b -n 1 | head -n 15', 'idle-top')
        R['idle'] = series('10-idle', 24, 4)
        kit('cat /proc/loadavg; free', 'idle-after')
        # Load: two dd writers stream 8 MiB buffers (memory writes past the 512 KiB L2) and a cat loop
        # reads a 64 MiB file from /tmp, keeping both A9 cores busy on memory traffic.
        kit("cat > /tmp/zbl-latload.sh <<'EOS'\n#!/bin/sh\n# zbl-latload: HPS CPU and memory load for the fes.ramtest latency check (Claude zbl-lat)\n"
            "dd if=/dev/zero of=/tmp/zbl-latload.bin bs=1M count=64 2>/dev/null\n"
            "dd if=/dev/zero of=/dev/null bs=8M 2>/dev/null & echo $! > /tmp/zbl-latload.pids\n"
            "dd if=/dev/zero of=/dev/null bs=8M 2>/dev/null & echo $! >> /tmp/zbl-latload.pids\n"
            "( while :; do cat /tmp/zbl-latload.bin > /dev/null; done ) & echo $! >> /tmp/zbl-latload.pids\n"
            "EOS\nsetsid sh /tmp/zbl-latload.sh </dev/null >/dev/null 2>&1 & sleep 5; cat /tmp/zbl-latload.pids", 'load-start')
        # Only the pid file's lines: kit() appends an 'exit N' line, and a stray 0 would make kill signal
        # the whole ssh session's process group (which, on this image, includes the dropbear listener).
        text = (out / 'load-start.txt').read_text().split('\nexit ')[0]
        load_pids = [int(p) for p in text.split() if p.isdigit() and int(p) > 1]
        log('load pids', load_pids); assert len(load_pids) == 3, load_pids
        time.sleep(15)
        kit('cat /proc/loadavg; top -b -n 1 | head -n 15', 'load-top')
        R['load'] = series('20-load', 24, 4)
        kit('cat /proc/loadavg; top -b -n 1 | head -n 15', 'load-top-end')
    finally:
        load_pids = [p for p in load_pids if isinstance(p, int) and p > 1]
        if load_pids:
            kit('for p in %s; do kill $p; done; sleep 3; for p in %s; do [ -d /proc/$p ] && echo alive $p; done; rm -f /tmp/zbl-latload.sh /tmp/zbl-latload.bin /tmp/zbl-latload.pids; ls /tmp | grep zbl; cat /proc/loadavg'
                % (' '.join(map(str, load_pids)), ' '.join(map(str, load_pids))), 'load-stop')
            stop_text = (out / 'load-stop.txt').read_text()
            R['load_stopped'] = 'alive' not in stop_text and stop_text.rstrip().endswith('exit 0')
            time.sleep(10)
            R['after_load'] = series('30-after-load', 4, 4)
        session = req(origin, '/api/v1/session', 'session-after')
        if session.get('game_id') == game:
            R['stop'] = req(origin, '/api/v1/session/stop', 'stop', b'', timeout=120).get('state')
        time.sleep(8); grab('40-after-stop')
except Exception as e:
    log('ERROR', type(e).__name__, str(e)[:500]); (out / 'error.txt').write_text(str(e) + '\n')
finally:
    if started:
        (out / 'host.log').write_text(iso._safe_text(runtime.logs(name), (target.agent,)))
        log('container stop', runtime.stop(name, 20)); runtime.remove(name)
    iso.remove_private_config(home)
    h = req(target.address, '/v1/health', 'after-health', token=target.agent)
    s = req(target.address, '/v1/status', 'after-status', token=target.agent)
    l = req(target.address, '/v1/kit/lease', 'after-lease', token=target.agent)
    kit('busybox devmem 0xFFC25080 32; ls /tmp | grep zbl; cat /proc/loadavg', 'after-kit')
    sw = h['software']
    R['idle_after'] = s['state'] == 'idle' and l['state'] == 'free' and sw['active_bundle'] == BUNDLE and sw['base_identity'] == BASE
    (out / 'result.json').write_text(json.dumps(R, indent=1))
    log('HIL_DONE', json.dumps({k: R.get(k) for k in ('launch', 'ready_after_s', 'load_stopped', 'stop', 'idle_after')}))
