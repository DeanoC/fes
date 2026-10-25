# hil_lat: fes.ramtest latency windows on kit A through a private host (Claude, zbl-lat, FES ramtest latency PR).
# Core-only run: no bundle install, no image change. Import the sealed .fcore into a private host, launch it,
# read the LAT rows from HDMI with Linux idle, then with a CPU and memory load on the HPS (busybox dd/cat
# started under the zbl-latload name and killed by PID), stop, and check the kit is back to its baseline.
# usage: python3 hil_lat.py <fcore> <package_id> <out_dir>
from pathlib import Path
import sys, json, urllib.request, urllib.error, time, hashlib, datetime, fcntl, subprocess, os, signal, re
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
VID = '/dev/v4l/by-id/usb-GENKI_ShadowCast_3_KT044001-video-index0'
KA = str(Path(__file__).resolve().parent / 'ka')
FONT = WT / 'sources/misteross/cores/fes-ramtest/rtl/ram_font.v'
TID = '73dc9f5f-1a12-4a95-a820-a9b4e600769a'
BUNDLE = '35d584898755b4c84b68cccf2e08838b4a371cbe550b5af457481f3d5e776f9b'
BASE = '4db7486452057d8a77479125c8ee8e9a27db4f378a9d9735900e90f6f3fe7a26'
HOSTBIN = Path.home() / '.local/lib/fogcast/fogcast-api-58f029e014c9f9ad491e03b8270667c2c22c316d'
IMAGE = 'sha256:44b7b0c76728be2a697e5cd7150a1ef7ddfda6c880212d11f9fef43d950a2af6'
FONT_TABLE = ocr_ramtest.load_font(FONT)
BASE_IMAGE = BASE
# Stops only the load: each PID must be in the load's own session, must not be a dropbear process and must
# not be this shell. It never signals 0, -1, a negative PID or a process group.
STOP_SCRIPT = r"""#!/bin/sh
# zbl-latstop: stop only the zbl-latload processes (Claude zbl-lat)
SID=$1; shift
case "$SID" in ''|*[!0-9]*|0|1) echo "REFUSED bad session $SID"; exit 1;; esac
DROP=" $(pidof dropbear) "
MYSID=$(cut -d' ' -f6 /proc/$$/stat)
[ "$SID" = "$MYSID" ] && { echo "REFUSED: load session is this shell's"; exit 1; }
for d in $DROP; do [ "$(cut -d' ' -f6 /proc/$d/stat)" = "$SID" ] && { echo "REFUSED: load session is dropbear's"; exit 1; }; done
echo "this shell $$ session $MYSID; dropbear$DROP; load session $SID; asked: $*"
ps -o pid,pgid,comm | grep -e dd -e cat -e ' sh' -e dropbear
check_kill() {
  p=$1
  case "$p" in ''|*[!0-9]*|0|1) echo "REFUSED bad pid $p"; return;; esac
  [ -r /proc/$p/stat ] || { echo "gone $p"; return; }
  set -- $(cat /proc/$p/stat)
  if [ "$6" != "$SID" ] || [ "$p" = "$$" ] || echo "$DROP" | grep -q " $p "; then echo "REFUSED $p: $1 $2 pgrp $5 session $6"; return; fi
  echo "kill $p $2 pgrp $5 session $6"; kill "$p"
}
for p in "$@"; do check_kill "$p"; done
sleep 2
for st in /proc/[0-9]*/stat; do set -- $(cat $st 2>/dev/null); [ "$6" = "$SID" ] && check_kill "$1"; done
sleep 1
left=""; for st in /proc/[0-9]*/stat; do set -- $(cat $st 2>/dev/null); [ "$6" = "$SID" ] && left="$left $1"; done
echo "left in load session:${left:- none}"
"""

with (Path.home() / 'kit-status/kitA-STATUS.md').open('r') as claim:
    fcntl.flock(claim, fcntl.LOCK_SH)
    heading = [l for l in claim.read().splitlines() if l.startswith('## ')][-1]
    assert 'KIT A IN USE: Claude' in heading and 'zbl-lat' in heading, 'claim not ours; no kit contact'

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
# The shared config selects kit A (target dev).
target = iso._load_private_target(Path.home() / '.config/fogcast/config.toml', TID)
home = iso.prepare_private_home(out, target)
load_pids = []
load_sid = None
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
        # The load runs in its own session (setsid), apart from dropbear's, which also holds every ssh shell.
        kit("cat > /tmp/zbl-latload.sh <<'EOS'\n#!/bin/sh\n# zbl-latload: HPS CPU and memory load for the fes.ramtest latency check (Claude zbl-lat)\n"
            "echo \"sid $$\" > /tmp/zbl-latload.pids\n"
            "dd if=/dev/zero of=/tmp/zbl-latload.bin bs=1M count=64 2>/dev/null\n"
            "dd if=/dev/zero of=/dev/null bs=8M 2>/dev/null & echo \"pid $!\" >> /tmp/zbl-latload.pids\n"
            "dd if=/dev/zero of=/dev/null bs=8M 2>/dev/null & echo \"pid $!\" >> /tmp/zbl-latload.pids\n"
            "( while :; do cat /tmp/zbl-latload.bin > /dev/null; done ) & echo \"pid $!\" >> /tmp/zbl-latload.pids\n"
            "EOS\nsetsid sh /tmp/zbl-latload.sh </dev/null >/dev/null 2>&1 & sleep 5; cat /tmp/zbl-latload.pids; "
            "echo; for d in $(pidof dropbear); do echo \"dropbear $(cut -d' ' -f1-6 /proc/$d/stat)\"; done; "
            "for p in $(sed -n 's/^pid //p' /tmp/zbl-latload.pids); do echo \"load $(cut -d' ' -f1-6 /proc/$p/stat)\"; done; "
            "ps -o pid,pgid,comm | grep -e dd -e cat -e ' sh' -e dropbear", 'load-start')
        lines = (out / 'load-start.txt').read_text().splitlines()
        sids = [int(l.split()[1]) for l in lines if re.fullmatch(r'sid [0-9]+', l)]
        load_pids = [int(l.split()[1]) for l in lines if re.fullmatch(r'pid [0-9]+', l)]
        load_pids = [p for p in load_pids if p > 1]
        log('load session', sids, 'pids', load_pids)
        assert len(sids) == 1 and sids[0] > 1 and len(load_pids) == 3, (sids, load_pids)
        load_sid = sids[0]
        time.sleep(15)
        kit('cat /proc/loadavg; top -b -n 1 | head -n 15', 'load-top')
        R['load'] = series('20-load', 24, 4)
        kit('cat /proc/loadavg; top -b -n 1 | head -n 15', 'load-top-end')
    finally:
        load_pids = [p for p in load_pids if type(p) is int and p > 1]
        if load_pids and type(load_sid) is int and load_sid > 1:
            kit("cat > /tmp/zbl-latstop.sh <<'EOS'\n" + STOP_SCRIPT + "EOS\nsh /tmp/zbl-latstop.sh %d %s; "
                "rm -f /tmp/zbl-latstop.sh /tmp/zbl-latload.sh /tmp/zbl-latload.bin /tmp/zbl-latload.pids; "
                "ls /tmp | grep zbl; cat /proc/loadavg" % (load_sid, ' '.join(map(str, load_pids))), 'load-stop')
            stop_text = (out / 'load-stop.txt').read_text()
            R['load_stopped'] = ('left in load session: none' in stop_text and 'REFUSED' not in stop_text
                                 and stop_text.rstrip().endswith('exit 0'))
            # A fresh connection proves dropbear is still listening.
            check = kit('echo sshd-ok; for d in $(pidof dropbear); do cut -d" " -f1-6 /proc/$d/stat; done', 'sshd-check')
            R['sshd_ok'] = 'sshd-ok' in check
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
