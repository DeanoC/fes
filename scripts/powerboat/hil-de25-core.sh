#!/bin/bash
# Exact-head J8G HIL: claim both D25 locks, isolate the host DB, smoke the package,
# and release FREE only after native restore and visible MENU verification.
set -Eeuo pipefail
KIT=$1 PR=$2 HEAD=$3 B=$4 D=$5
[ "$KIT" = D25 ] || { echo "D25 runner refuses kit $KIT"; exit 2; }
[[ "$HEAD" =~ ^[0-9a-f]{40}$ ]] || { echo "full exact HEAD required"; exit 2; }
Q=/home/deano/lander-hilq; KS=/home/deano/kit-status
STATUS=$KS/de25-TRWU8J8G-STATUS.md
BASE=/home/deano/fes-hil/baselines/current-restore-de25.json
LOCK1=$KS/de25-TRWU8J8G-STATUS.lock; LOCK2=$KS/de25-TRWU8J8G-STATUS.md.lock
TARGET=de25; TARGET_ID=e2a51a4f-bae2-4041-a4e4-51f6774d4e45; SERIAL=TRWU8J8G; ADDR=192.168.10.119
ROOT=/home/deano/codex-jobs/de25-kit-parity-p3-20261116
SOURCE_BASE=$ROOT/wt
D25_SW_BASE=/home/deano/hil-assets/de25-kit-software-base/native-probe-rootfs.img
D25_SW_IMAGE=/data/fes-de25-main-integration-20261105/partial-start-investigation-20261108/first-fault-integration-53009e4ab/hdmi-hotplug-9c/release-13/kernel-93131814a2a2-v2/base
mkdir -p "$D/logs" "$D/out"
if ! command -v ffmpeg >/dev/null || ! command -v tesseract >/dev/null; then
  echo NOTSTARTED > "$D/verdict"
  echo "DE25 restore capture requires ffmpeg and tesseract"
  exit 3
fi
SOURCE_WORKTREE=; HOSTPID=; TMPDIR_PATH=
cleanup(){
  [ -n "$HOSTPID" ] && { kill "$HOSTPID" 2>/dev/null || true; wait "$HOSTPID" 2>/dev/null || true; }
  [ -z "$SOURCE_WORKTREE" ] || git -C "$SOURCE_BASE" worktree remove --force "$SOURCE_WORKTREE" >/dev/null 2>&1 || true
  [ -z "$TMPDIR_PATH" ] || rm -rf "$TMPDIR_PATH"
}
trap cleanup EXIT
# Keep Go's temporary paths short and avoid VCS stamping from ambient parent
# directories (for example a stray /home/deano/.git).
TMPDIR_PATH=$(mktemp -d /tmp/lXXXXXX)
export TMPDIR="$TMPDIR_PATH"
# Prepare the managed software from this exact revision before taking J8G.
FES_SOURCE=$ROOT/wt
if [ "$(git -C "$FES_SOURCE" rev-parse HEAD 2>/dev/null || true)" != "$HEAD" ]; then
  FES_SOURCE=$D/fes-source
  git -C "$SOURCE_BASE" fetch origin "$HEAD" >"$D/logs/source-fetch.log" 2>&1 || true
  if ! git -C "$SOURCE_BASE" cat-file -e "$HEAD^{commit}" 2>/dev/null || \
     ! git -C "$SOURCE_BASE" worktree add --detach "$FES_SOURCE" "$HEAD" >"$D/logs/source-worktree.log" 2>&1; then
    echo "NOTSTARTED" > "$D/verdict"; echo "could not prepare exact-head source $HEAD"; exit 3
  fi
  SOURCE_WORKTREE=$FES_SOURCE
fi
[ "$(git -C "$FES_SOURCE" rev-parse HEAD)" = "$HEAD" ] || { echo NOTSTARTED > "$D/verdict"; exit 3; }
SW_OUT=$D/kit-software
if ! python3 "$FES_SOURCE/scripts/kit_software.py" \
  --base "$D25_SW_BASE" --base-sha256 f6630dea5e0a4c9274a7787091dd306f06fddfb48beb3e2f047e4bf08fdd3a0b \
  --version "de25-hilq-${HEAD:0:12}" --output "$SW_OUT" --board de25-nano \
  --de25-static-expansion 0 --de25-image "$D25_SW_IMAGE" >"$D/logs/kit-software-build.log" 2>&1; then
  echo NOTSTARTED > "$D/verdict"; echo "exact-head managed DE25 kit-software build failed"; exit 3
fi
SW_BUNDLE=$SW_OUT/bundle.tar
SW_RUNTIME_SHA=$(python3 "$FES_SOURCE/scripts/de25_kit_software.py" runtime-digest --bundle "$SW_BUNDLE" --head "$HEAD") || {
  echo NOTSTARTED > "$D/verdict"; echo "exact-head software bundle validation failed"; exit 3;
}
SW_BUNDLE_ID=$(python3 - "$SW_OUT/receipt.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1])); print(r.get('bundle',{}).get('id') or r.get('id') or '')
PY
)
[[ "$SW_BUNDLE_ID" =~ ^[0-9a-f]{64}$ ]] || { echo NOTSTARTED > "$D/verdict"; echo "invalid software bundle ID"; exit 3; }
# Verify every exact sealed package before claiming J8G or activating candidate software.
# core-build.result holds one line per package: DONE <fcore> <sha256> [<slug>].
[ -s "$B/core-build.result" ] || { echo NOTSTARTED > "$D/verdict"; echo "exact-head sealed package missing"; exit 3; }
mapfile -t PACKAGE_LINES < "$B/core-build.result"
PKGS=(); PKG_SHAS=(); CORE_IDS=(); SLUGS=()
for LINE in "${PACKAGE_LINES[@]}"; do
  read -r STATE PKG PKG_SHA SLUG EXTRA <<< "$LINE"
  [ "$STATE" = DONE ] && [ -f "$PKG" ] && [ -z "$EXTRA" ] || { echo NOTSTARTED > "$D/verdict"; echo "exact-head sealed package missing"; exit 3; }
  ACTUAL=$(sha256sum "$PKG" | cut -d' ' -f1)
  [ "$ACTUAL" = "$PKG_SHA" ] || { echo NOTSTARTED > "$D/verdict"; echo "sealed package digest mismatch"; exit 3; }
  CORE_ID=$(python3 - "$PKG" <<'PY' || { echo NOTSTARTED > "$D/verdict"; echo "sealed package manifest invalid"; exit 3; }
import sys,tarfile,tomllib
with tarfile.open(sys.argv[1], 'r:*') as archive:
    member=archive.extractfile('manifest.toml')
    if member is None: raise SystemExit('package manifest missing')
    print(tomllib.loads(member.read().decode())['core']['id'])
PY
)
  SLUG=${SLUG:-$CORE_ID}
  [[ "$SLUG" =~ ^fes\.[a-z0-9][a-z0-9._-]*$ ]] || { echo NOTSTARTED > "$D/verdict"; echo "invalid package slug $SLUG"; exit 3; }
  for SEEN in "${SLUGS[@]}"; do [ "$SEEN" != "$SLUG" ] || { echo NOTSTARTED > "$D/verdict"; echo "package $SLUG listed twice"; exit 3; }; done
  PKGS+=("$PKG"); PKG_SHAS+=("$ACTUAL"); CORE_IDS+=("$CORE_ID"); SLUGS+=("$SLUG")
done
N_PKGS=${#PKGS[@]}
[ "$N_PKGS" -ge 1 ] || { echo NOTSTARTED > "$D/verdict"; echo "exact-head sealed package missing"; exit 3; }
# Generate the D25 smoke engine before the claim when any package needs it.
NEEDS_CORE_SMOKE=0
for CORE_ID in "${CORE_IDS[@]}"; do [ "$CORE_ID" = fes.menu ] || NEEDS_CORE_SMOKE=1; done
if [ "$NEEDS_CORE_SMOKE" = 1 ]; then
  cp "$Q/hilcore.py" "$Q/hil-de25-core.py"
  python3 - "$Q/hil-de25-core.py" <<'PY' || { echo NOTSTARTED > "$D/verdict"; echo "D25 smoke patch generation failed"; exit 3; }
from pathlib import Path
import sys
p=Path(sys.argv[1]); s=p.read_text(); old='ORIGIN = "http://127.0.0.1:8787"'; new='ORIGIN = os.environ.get("C_API", "http://127.0.0.1:8787")'
if old not in s: raise SystemExit('HIL smoke API declaration changed')
s=s.replace(old,new,1)
old_rom = '    plan = hil_rom.rom_plan(R["descriptor"], entry.get("core_id"), req(f"/api/v1/library/core-entries/{game}/rom", "rom-before"))'
new_rom = """    rom = (R[\"descriptor\"] or {}).get(\"rom\") or {}
    if rom.get(\"id\"):
        current = req(f\"/api/v1/library/core-entries/{game}/rom?rom_id={rom['id']}\", \"rom-before\")
        plan = hil_rom.rom_plan(R[\"descriptor\"], entry.get(\"core_id\"), current)
    else:
        plan = None"""
# The HILQ engine now carries this ROM selection itself; patch only older copies.
if new_rom not in s:
    if old_rom not in s: raise SystemExit('HIL smoke ROM selection declaration changed')
    s=s.replace(old_rom,new_rom,1)
old_idle = """    rc, lc = kit("curl -s -m5 --unix-socket /run/fogcast/local-control.sock http://local-control/v1/local/status")
    check("idle_after_stop", '"phase":"idle"' in lc, lc[:160])"""
new_idle = "    idle = req('/api/v1/session', 'session-after-stop')\n    check('idle_after_stop', idle.get('state') == 'idle', str(idle.get('state')))"
if old_idle not in s: raise SystemExit('HIL smoke idle probe declaration changed')
s=s.replace(old_idle,new_idle,1)
old_frames = '    s1 = grab("10-core", 1); s2 = grab("11-core", 10)'
new_frames = old_frames + """
    tool = os.environ.get("C_BOOT_SCREEN_TOOL", "")
    if tool and os.path.isfile(tool):
        frames = [str(FR / (n + ".png")) for n in ("10-core", "11-core") if (FR / (n + ".png")).is_file()]
        bs = subprocess.run([sys.executable, tool, "--core", os.environ.get("C_CORE_ID", ""), *frames], capture_output=True, text=True, timeout=180)
        (OUT / "boot-screen.json").write_text(bs.stdout + bs.stderr)
        if bs.returncode != 3:
            check("boot_screen", bs.returncode == 0 and len(frames) == 2, bs.stdout.strip()[:400])"""
if old_frames not in s: raise SystemExit('HIL smoke core frame capture changed')
s=s.replace(old_frames,new_frames,1)
old_video = '    check("core_video", ok, f"{s1} / {s2} (bright_frac info only)")'
new_video = old_video + """
    # Conformance steps from config/de25-conformance.json: HID taps through the
    # host's confirmed tap, a relative mouse move, and media changes through
    # the FogCast CLI, each judged on the HDMI capture. Exit 3 means the core
    # has no steps yet. Declared media and mouse interfaces that no step
    # covers are recorded as pending rather than passed.
    tool = os.environ.get("C_CONFORMANCE_TOOL", "")
    if tool and os.path.isfile(tool):
        cf = subprocess.run([sys.executable, tool, "--core", os.environ.get("C_CORE_ID", ""), "--package", str(PKG),
            "--api", ORIGIN, "--fogcast", FOGCAST, "--video", VID, "--out", str(OUT / "conformance"),
            "--kit", "D25", "--package-id", str(pid)],
            capture_output=True, text=True, timeout=1200)
        (OUT / "conformance.log").write_text(cf.stdout + cf.stderr)
        if cf.returncode != 3:
            try:
                report = json.loads(cf.stdout)
            except ValueError:
                report = {}
            R["conformance_pending"] = report.get("pending")
            for name, v in (report.get("checks") or {}).items():
                check(name, v.get("ok") is True, str(v.get("detail", ""))[:400])
            check("conformance", cf.returncode == 0 and report.get("verdict") == "PASS", f"rc={cf.returncode}")"""
if old_video not in s: raise SystemExit('HIL smoke core video check changed')
s=s.replace(old_video,new_video,1)
compile(s, str(p), 'exec')
if 'C_API' not in s or 'session-after-stop' not in s or 'boot-screen.json' not in s or 'C_CONFORMANCE_TOOL' not in s: raise SystemExit('D25 smoke patch verification failed')
p.write_text(s)
PY
fi
exec 8>"$LOCK1" 9>"$LOCK2"
flock -n 8 || { echo "J8G status lock busy"; echo NOTSTARTED > "$D/verdict"; exit 3; }
flock -n 9 || { echo "J8G ledger lock busy"; echo NOTSTARTED > "$D/verdict"; exit 3; }
last=$(awk '/^## /{line=$0} END{print line}' "$STATUS")
[[ "$last" == *"KIT D25 FREE (baseline verified)"* ]] || { echo "J8G ledger is not FREE: $last"; echo NOTSTARTED > "$D/verdict"; exit 3; }
python3 - "$BASE" <<'PY'
import json,sys
b=json.load(open(sys.argv[1]))
assert b['kit_id']=='de25-j8g' and b['serial']=='TRWU8J8G'
assert b['target_id']=='e2a51a4f-bae2-4041-a4e4-51f6774d4e45'
PY
TAG="lander-hilq D25 fes#$PR @ $HEAD"
printf '\n## KIT D25 IN USE (de25-j8g) — owner HILQ, PR %s, head %s, scope exact-head package HIL; evidence %s; started %s.\n' "$PR" "$HEAD" "$D" "$(date -Is)" >> "$STATUS"
echo "START $TAG" | tee -a "$D/logs/driver.log"
PASS=0; RESTORE=0; HOSTPID=; SWAP_HOME=$D/private-home; API=127.0.0.1:19789
fail(){ echo "$*" | tee -a "$D/logs/driver.log"; }
# Verify the authenticated network endpoint's independent hardware identity before mutation.
if ! /home/deano/lander-hilq/kits/kssh D25 "test \"\$(cat /sys/class/net/eth0/address)\" = 02:8e:2a:b1:d3:bd" >"$D/logs/identity.txt" 2>&1; then fail "network MAC identity check failed"; echo NOTSTARTED > "$D/verdict"; exit 3; fi
# Copy the host library to a run-local home and point a private API at J8G.
# Enable remote input: without it the host has no input bridge, so attach,
# tap and event all fail and no typed key reaches the core (fes#2262).
mkdir -p "$SWAP_HOME/.config/fogcast" "$SWAP_HOME/.local/share" "$SWAP_HOME/bin"
cp -a /home/deano/.local/share/fogcast "$SWAP_HOME/.local/share/"
CHECKED_OUT_HEAD=$(git -C "$FES_SOURCE" rev-parse HEAD)
[ "$CHECKED_OUT_HEAD" = "$HEAD" ] || { fail "private host source is not exact head ($CHECKED_OUT_HEAD)"; echo NOTSTARTED > "$D/verdict"; exit 3; }
if ! (cd "$FES_SOURCE/sources/FogCast" && GOFLAGS='-mod=readonly -buildvcs=false' timeout 900 go build -buildvcs=false -o "$SWAP_HOME/bin/fogcast-api" ./cmd/fogcast-api && GOFLAGS='-mod=readonly -buildvcs=false' timeout 900 go build -buildvcs=false -o "$SWAP_HOME/bin/fogcast" ./cmd/fogcast) >"$D/logs/private-host-build.log" 2>&1; then
  fail "exact-head private FogCast API/CLI build failed"; echo NOTSTARTED > "$D/verdict"; exit 3
fi
PRIVATE_API=$SWAP_HOME/bin/fogcast-api; PRIVATE_FOGCAST=$SWAP_HOME/bin/fogcast
AGENT_TOKEN=$(/home/deano/lander-hilq/kits/kssh D25 'grep "^token" /media/fat/fogcast/agent.toml | cut -d= -f2 | xargs')
[[ "$AGENT_TOKEN" =~ ^[A-Za-z0-9._~-]{32,256}$ ]] || { fail "could not read valid J8G API credential"; echo NOTSTARTED > "$D/verdict"; exit 3; }
FOGCAST_AGENT_TOKEN="$AGENT_TOKEN" python3 - "$SWAP_HOME/.config/fogcast/config.toml" <<'PY'
from pathlib import Path
import os,re,sys
token=os.environ['FOGCAST_AGENT_TOKEN']
if not re.fullmatch(r'[A-Za-z0-9._~-]{32,256}', token): raise SystemExit('invalid J8G credential')
def quote(value): return '"' + value.replace('\\','\\\\').replace('"','\\"') + '"'
src='selected_target = "de25"\nrequest_timeout_seconds = 5\nupload_timeout_seconds = 60\n\n[[targets]]\nname = "de25"\nenabled = true\naddress = "http://192.168.10.119:8182"\ntarget_id = "e2a51a4f-bae2-4041-a4e4-51f6774d4e45"\nagent = ' + quote(token) + '\n\n[remote_input]\nenabled = true\n'
p=Path(sys.argv[1]); p.write_text(src); p.chmod(0o600)
PY
unset AGENT_TOKEN
env HOME="$SWAP_HOME" XDG_CONFIG_HOME="$SWAP_HOME/.config" XDG_DATA_HOME="$SWAP_HOME/.local/share" \
  "$PRIVATE_API" --config "$SWAP_HOME/.config/fogcast/config.toml" --listen "$API" --headless >"$D/logs/private-host.log" 2>&1 & HOSTPID=$!
for i in $(seq 1 60); do curl --noproxy '*' -fsS "http://$API/api/v1/health" -o "$D/out/host-health-before.json" 2>/dev/null && break; sleep 1; done
if ! test -s "$D/out/host-health-before.json"; then fail "private D25 host did not become ready"; echo FAIL > "$D/verdict"; exit 1; fi
# Capture serial-bound state before the exact package is installed or launched.
cat > "$D/snapshot.py" <<'PY'
import hashlib,json,subprocess,sys,urllib.request
api,kit,dest=sys.argv[1:]
def get(url):
 with urllib.request.urlopen(url,timeout=15) as r: return json.load(r)
health=get(api+'/api/v1/health')
agent_health=json.loads(subprocess.check_output(['/home/deano/lander-hilq/kits/kssh',kit,'T=$(grep "^token" /media/fat/fogcast/agent.toml | cut -d= -f2 | xargs); curl -fsS -H "Authorization: Bearer $T" http://127.0.0.1:8182/v1/health'],text=True,timeout=20))
agent_status=json.loads(subprocess.check_output(['/home/deano/lander-hilq/kits/kssh',kit,'T=$(grep "^token" /media/fat/fogcast/agent.toml | cut -d= -f2 | xargs); curl -fsS -H "Authorization: Bearer $T" http://127.0.0.1:8182/v1/status'],text=True,timeout=20))
lease=json.loads(subprocess.check_output(['/home/deano/lander-hilq/kits/kssh',kit,'T=$(grep "^token" /media/fat/fogcast/agent.toml | cut -d= -f2 | xargs); curl -fsS -H "Authorization: Bearer $T" http://127.0.0.1:8182/v1/kit/lease'],text=True,timeout=20))
native=subprocess.check_output(['/home/deano/lander-hilq/kits/kssh',kit,"printf 'boot_id='; cat /proc/sys/kernel/random/boot_id; printf '\\nkernel='; uname -r; printf '\\nmac='; cat /sys/class/net/eth0/address; printf '\\nmounts\\n'; awk '$2==\"/\" || $2==\"/media/fat\" || $2==\"/run/fogcast/fesdata3\"' /proc/mounts"],text=True,timeout=20)
conn=(health.get('target') or {}).get('connection') or {}
serial='TRWU8J8G' if kit=='D25' and native.split('mac=',1)[-1].splitlines()[0]=='02:8e:2a:b1:d3:bd' else ''
raw={'kit':'de25-j8g','serial':serial,'target_id':conn.get('target_id'),'boot_id':conn.get('boot_id'),'host_health':health,'agent_health':agent_health,'agent_status':agent_status,'lease':lease,'native_readonly':native,'native_sha256':hashlib.sha256(native.encode()).hexdigest()}
open(dest,'w').write(json.dumps(raw,indent=2,sort_keys=True)+'\n')
if not serial or raw['target_id']!='e2a51a4f-bae2-4041-a4e4-51f6774d4e45': raise SystemExit('J8G identity mismatch')
PY
python3 "$D/snapshot.py" "http://$API" D25 "$D/out/baseline-before.json" || { fail "before snapshot identity/preflight failed"; echo NOTSTARTED > "$D/verdict"; exit 3; }
# Install and activate the exact-head managed software before either smoke.
SOFTWARE_CLI=(go run ./cmd/fes-software --config "$SWAP_HOME/.config/fogcast/config.toml")
SW_READY=0
software_status(){
  local output=$1
  (cd "$FES_SOURCE/sources/FogCast" && GOFLAGS=-mod=readonly timeout 120 "${SOFTWARE_CLI[@]}" --action status) >"$output" 2>"$D/logs/software-status-poll.log"
}
(cd "$FES_SOURCE/sources/FogCast" && GOFLAGS=-mod=readonly timeout 900 "${SOFTWARE_CLI[@]}" --action install --bundle "$SW_BUNDLE") >"$D/out/software-install.json" 2>"$D/logs/software-install.log" || { fail "exact-head kit-software install failed"; echo FAIL > "$D/verdict"; }
if [ "$(cat "$D/verdict" 2>/dev/null || true)" != FAIL ]; then
  (cd "$FES_SOURCE/sources/FogCast" && GOFLAGS=-mod=readonly timeout 900 "${SOFTWARE_CLI[@]}" --action activate --candidate "$SW_BUNDLE_ID" --requester "lander-hilq-$PR") >"$D/out/software-activate.json" 2>"$D/logs/software-activate.log" || fail "exact-head kit-software activation request failed"
  for attempt in $(seq 1 36); do
    python3 "$D/snapshot.py" "http://$API" D25 "$D/out/candidate-active.json" >/dev/null 2>&1 || true
    if software_status "$D/out/software-status-candidate.json" && \
      [ "$(jq -r '.host_health.target.software.active_bundle // empty' "$D/out/candidate-active.json" 2>/dev/null)" = "$SW_BUNDLE_ID" ] && \
      [ "$(jq -r '.selected // empty' "$D/out/software-status-candidate.json" 2>/dev/null)" = "$SW_BUNDLE_ID" ] && \
      [ "$(jq -r 'if .pending == null then "clear" else "pending" end' "$D/out/software-status-candidate.json" 2>/dev/null)" = clear ]; then break; fi
    sleep 5
  done
  if [ "$(jq -r '.host_health.target.software.active_bundle // empty' "$D/out/candidate-active.json" 2>/dev/null)" != "$SW_BUNDLE_ID" ] || \
    [ "$(jq -r '.selected // empty' "$D/out/software-status-candidate.json" 2>/dev/null)" != "$SW_BUNDLE_ID" ] || \
    [ "$(jq -r 'if .pending == null then "clear" else "pending" end' "$D/out/software-status-candidate.json" 2>/dev/null)" != clear ]; then
    fail "exact-head kit-software activation did not settle on the selected bundle"; echo FAIL > "$D/verdict"
  fi
  /home/deano/lander-hilq/kits/kssh D25 "p=\$(pidof mister-runtime); test -n \"\$p\"; sha256sum /proc/\$p/exe | awk '{print \$1}'" >"$D/out/runtime-active.sha256" 2>"$D/logs/runtime-active.log" || fail "could not read running mister-runtime digest"
  if python3 "$FES_SOURCE/scripts/de25_kit_software.py" verify-runtime --expected "$SW_RUNTIME_SHA" --observed-file "$D/out/runtime-active.sha256" >"$D/out/runtime-manifest-verification.txt"; then
    SW_READY=1
  else
    fail "running runtime differs from software manifest"; echo FAIL > "$D/verdict"
  fi
fi
# MENU is a volatile development-core load, not an installable playable library entry.
# Other DE25 packages use the existing package-install/launch smoke engine.
# Every package runs under this one claim and software activation; the run
# passes only if every package passes. A single package keeps the $D/out
# layout; several packages each get $D/out/<slug>.
ALL_PASS=1
for i in "${!PKGS[@]}"; do
  PKG=${PKGS[$i]} CORE_ID=${CORE_IDS[$i]} SLUG=${SLUGS[$i]}
  if [ "$N_PKGS" -eq 1 ]; then
    OUT=$D/out SMOKE_LOG=$D/logs/smoke.log
  else
    OUT=$D/out/$SLUG SMOKE_LOG=$D/logs/smoke-$SLUG.log
    mkdir -p "$OUT"
    echo "package $((i + 1))/$N_PKGS: $SLUG $(basename "$PKG")" | tee -a "$D/logs/driver.log"
  fi
  set +e
  if [ "$SW_READY" != 1 ]; then
    echo "smoke skipped because exact-head kit software was not verified" > "$SMOKE_LOG"
    SMOKE_RC=1
  elif [ "$CORE_ID" = fes.menu ]; then
    C_PR="$PR" C_HEAD="$HEAD" C_PKG="$PKG" C_OUT="$OUT" C_VID=/dev/v4l/by-id/fes-ms2109-hil C_FOGCAST="$PRIVATE_FOGCAST" C_API="http://$API" \
      timeout 1800 python3 "$Q/hil-de25-menu.py" >"$SMOKE_LOG" 2>&1
    SMOKE_RC=$?
  else
    C_CORE_ID="$CORE_ID" C_BOOT_SCREEN_TOOL="$FES_SOURCE/scripts/de25_boot_screen.py" C_CONFORMANCE_TOOL="$FES_SOURCE/scripts/de25_conformance.py" C_KIT=D25 C_PR="$PR" C_TARGET=de25 C_HEAD="$HEAD" C_PKG="$PKG" C_OUT="$OUT" C_VID=/dev/v4l/by-id/fes-ms2109-hil C_FOGCAST="$PRIVATE_FOGCAST" C_API="http://$API" \
      timeout 1800 python3 "$Q/hil-de25-core.py" >"$SMOKE_LOG" 2>&1
    SMOKE_RC=$?
  fi
  set -e
  [ "$SMOKE_RC" -eq 0 ] || { fail "$SLUG package lifecycle smoke rc=$SMOKE_RC"; ALL_PASS=0; }
  # Bind the smoke result to the package that this runner actually verified and
  # loaded. The smoke engines have different result schemas, so normalize these
  # two identity fields here before recording the run verdict.
  if [ "$CORE_ID" = fes.menu ]; then
    RESULT="$OUT/menu-result.json"
  else
    RESULT="$OUT/result.json"
  fi
  if [ -s "$RESULT" ]; then
    C_RESULT="$RESULT" C_CORE_ID="$CORE_ID" C_PACKAGE_ID="$(basename "$PKG" .fcore)" python3 - <<'PY' || { fail "could not bind $SLUG smoke result to its package"; ALL_PASS=0; }
import json
import os
from pathlib import Path
import re

path = Path(os.environ['C_RESULT'])
core_id = os.environ['C_CORE_ID']
package_id = os.environ['C_PACKAGE_ID']
if not re.fullmatch(r'fes\.[a-z0-9.-]+', core_id):
    raise SystemExit('invalid core ID from package manifest')
if not re.fullmatch(r'[0-9a-f]{64}', package_id):
    raise SystemExit('invalid package ID from sealed package filename')
result = json.loads(path.read_text())
if not isinstance(result, dict):
    raise SystemExit('smoke result is not a JSON object')
result['core_id'] = core_id
result['package_id'] = package_id
path.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
PY
  else
    fail "smoke result is missing: $RESULT"
    ALL_PASS=0
  fi
  # Leave the kit idle before the next package.
  curl --noproxy '*' -sS --max-time 30 -H 'Content-Type: application/json' -d '{"target":"de25"}' "http://$API/api/v1/session/stop" > "$OUT/stop-after-smoke.json" 2>/dev/null || true
done
PASS=$ALL_PASS
# Stop is repeated idempotently, then the recorded baseline bundle is reactivated.
curl --noproxy '*' -sS --max-time 30 -H 'Content-Type: application/json' -d '{"target":"de25"}' "http://$API/api/v1/session/stop" > "$D/out/stop-final.json" || true
sleep 2
BASE_SW=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["kit_sw"])' "$BASE")
(cd "$FES_SOURCE/sources/FogCast" && GOFLAGS=-mod=readonly timeout 900 "${SOFTWARE_CLI[@]}" --action activate --candidate "$BASE_SW" --requester "lander-hilq-$PR-restore") >"$D/out/software-restore.json" 2>"$D/logs/software-restore.log" || fail "recorded baseline software reactivation failed"
for attempt in $(seq 1 36); do
  python3 "$D/snapshot.py" "http://$API" D25 "$D/out/baseline-after.json" >/dev/null 2>&1 || true
  if software_status "$D/out/software-status-baseline-restore.json" && \
    [ "$(jq -r '.host_health.target.software.active_bundle // empty' "$D/out/baseline-after.json" 2>/dev/null)" = "$BASE_SW" ] && \
    [ "$(jq -r '.selected // empty' "$D/out/software-status-baseline-restore.json" 2>/dev/null)" = "$BASE_SW" ] && \
    [ "$(jq -r 'if .pending == null then "clear" else "pending" end' "$D/out/software-status-baseline-restore.json" 2>/dev/null)" = clear ]; then break; fi
  sleep 5
done
if [ "$(jq -r '.host_health.target.software.active_bundle // empty' "$D/out/baseline-after.json" 2>/dev/null)" != "$BASE_SW" ] || \
  [ "$(jq -r '.selected // empty' "$D/out/software-status-baseline-restore.json" 2>/dev/null)" != "$BASE_SW" ] || \
  [ "$(jq -r 'if .pending == null then "clear" else "pending" end' "$D/out/software-status-baseline-restore.json" 2>/dev/null)" != clear ]; then
  fail "baseline software activation did not settle on the recorded bundle"
fi
python3 "$D/snapshot.py" "http://$API" D25 "$D/out/baseline-after.json" || fail "after snapshot failed"
RESTORE_TOOL="$FES_SOURCE/scripts/de25_restore.py"
[ -f "$RESTORE_TOOL" ] || RESTORE_TOOL="$ROOT/wt/scripts/de25_restore.py"

capture_restored_menu(){
  local evidence attempt frame ocr screen
  evidence=$(mktemp -d "$D/out/restore-menu.XXXXXX") || return 1
  for attempt in {1..12}; do
    [ "$attempt" = 1 ] || sleep 5
    frame=$evidence/$attempt.png
    ocr=$evidence/$attempt.txt
    if ! timeout -k 3s 15s ffmpeg -nostdin -hide_banner -loglevel error \
      -f v4l2 -i /dev/v4l/by-id/fes-ms2109-hil -vf 'select=gte(n\,10)' \
      -frames:v 1 -threads 1 "$frame" >"$evidence/$attempt.capture.log" 2>&1 || [ ! -s "$frame" ]; then
      continue
    fi
    if ! timeout -k 3s 10s tesseract "$frame" stdout --psm 11 \
      >"$ocr" 2>"$evidence/$attempt.ocr.log"; then
      continue
    fi
    screen=$(<"$ocr")
    # OCR may put the error title on separate lines. Recognize Home, the
    # Library's Home navigation, or Welcome, and reject the room error overlay.
    if grep -Eiq '\bRoom[[:space:]]+failed\b' <<< "${screen//$'\n'/ }"; then
      continue
    fi
    if { grep -iwq FES "$ocr" && grep -Eiq '^[[:space:]]*Home[[:space:]]*$' "$ocr"; } ||
       { grep -Eiq '^[[:space:]]*FES[[:space:]|]+Library[[:space:]]*$' "$ocr" && grep -iwq Home "$ocr"; } ||
       grep -Eiq '\bWelcome[[:space:]]+to[[:space:]]+FES\b' "$ocr"; then
      printf 'restored MENU verified in %s (OCR %s)\n' "$frame" "$ocr" | tee -a "$D/logs/driver.log"
      return 0
    fi
  done
  return 1
}

# Preserve the claim and the evidence if either restore check fails.
if ! python3 "$RESTORE_TOOL" --baseline "$BASE" --before "$D/out/baseline-before.json" \
  --after "$D/out/baseline-after.json" > "$D/out/restore-verification.json"; then
  fail "DE25 restore verification failed; ledger remains claimed for operator recovery"
elif capture_restored_menu; then
  RESTORE=1
else
  fail "DE25 restored MENU not verified; ledger remains claimed for operator recovery"
fi
if [ "$RESTORE" = 1 ]; then
  PKG_NAMES=$(for P in "${PKGS[@]}"; do basename "$P"; done | paste -sd, -)
  PKG_DIGESTS=$(IFS=,; echo "${PKG_SHAS[*]}")
  SUM="exact-head=$HEAD package=$PKG_NAMES sha256=$PKG_DIGESTS smoke=$([ "$PASS" = 1 ] && echo PASS || echo FAIL) restore=PASS"
  printf '\n## KIT D25 FREE (baseline verified) — de25-j8g; %s; released %s. Evidence %s\n' "$SUM" "$(date -Is)" "$D" >> "$STATUS"
  echo "restore verified; J8G released FREE (baseline verified)" | tee -a "$D/logs/driver.log"
else
  printf '\n## KIT D25 RESTORE_INCOMPLETE — PR %s head %s; exact-head runner could not verify restore. Keep D25 out of dispatch. Evidence %s\n' "$PR" "$HEAD" "$D" >> "$STATUS"
fi
if [ "$RESTORE" = 1 ] && [ "$PASS" = 1 ]; then echo PASS > "$D/verdict"; exit 0; fi
echo FAIL > "$D/verdict"; exit 1
