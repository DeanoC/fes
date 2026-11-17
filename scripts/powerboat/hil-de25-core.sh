#!/bin/bash
# Exact-head J8G HIL: claim both D25 locks, isolate the host DB, smoke the package,
# and release FREE only after scripts/de25_restore.py passes.
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
D25_SW_BASE=/data/fes-de25-pr1312-hil-19cca-20261112/de25/native-probe-rootfs.img
D25_SW_IMAGE=/data/fes-de25-main-integration-20261105/partial-start-investigation-20261108/first-fault-integration-53009e4ab/hdmi-hotplug-9c/release-13/kernel-93131814a2a2-v2/base
mkdir -p "$D/logs" "$D/out"
SOURCE_WORKTREE=; HOSTPID=
cleanup(){
  [ -n "$HOSTPID" ] && { kill "$HOSTPID" 2>/dev/null || true; wait "$HOSTPID" 2>/dev/null || true; }
  [ -z "$SOURCE_WORKTREE" ] || git -C "$SOURCE_BASE" worktree remove --force "$SOURCE_WORKTREE" >/dev/null 2>&1 || true
}
trap cleanup EXIT
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
  --base "$D25_SW_BASE" --base-sha256 8e4b57d125cf29c312c00ade7db355d7703199268612d8446af0e1d1f9b0b49e \
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
mkdir -p "$SWAP_HOME/.config/fogcast" "$SWAP_HOME/.local/share" "$SWAP_HOME/bin"
cp -a /home/deano/.local/share/fogcast "$SWAP_HOME/.local/share/"
CHECKED_OUT_HEAD=$(git -C "$FES_SOURCE" rev-parse HEAD)
[ "$CHECKED_OUT_HEAD" = "$HEAD" ] || { fail "private host source is not exact head ($CHECKED_OUT_HEAD)"; echo NOTSTARTED > "$D/verdict"; exit 3; }
if ! (cd "$FES_SOURCE/sources/FogCast" && GOFLAGS=-mod=readonly timeout 900 go build -o "$SWAP_HOME/bin/fogcast-api" ./cmd/fogcast-api && GOFLAGS=-mod=readonly timeout 900 go build -o "$SWAP_HOME/bin/fogcast" ./cmd/fogcast) >"$D/logs/private-host-build.log" 2>&1; then
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
src='selected_target = "de25"\nrequest_timeout_seconds = 5\nupload_timeout_seconds = 60\n\n[[targets]]\nname = "de25"\nenabled = true\naddress = "http://192.168.10.119:8182"\ntarget_id = "e2a51a4f-bae2-4041-a4e4-51f6774d4e45"\nagent = ' + quote(token) + '\n'
p=Path(sys.argv[1]); p.write_text(src); p.chmod(0o600)
PY
unset AGENT_TOKEN
env HOME="$SWAP_HOME" XDG_CONFIG_HOME="$SWAP_HOME/.config" XDG_DATA_HOME="$SWAP_HOME/.local/share" \
  "$PRIVATE_API" --config "$SWAP_HOME/.config/fogcast/config.toml" --listen "$API" --headless >"$D/logs/private-host.log" 2>&1 & HOSTPID=$!
for i in $(seq 1 60); do curl --noproxy '*' -fsS "http://$API/api/v1/health" -o "$D/out/host-health-before.json" && break; sleep 1; done
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
# Verify exact build result and package SHA before any session mutation.
LINE=$(cat "$B/core-build.result" 2>/dev/null || true)
read -r STATE PKG PKG_SHA <<< "$LINE"
[ "$STATE" = DONE ] && [ -f "$PKG" ] || { fail "exact-head sealed package missing"; echo NOTSTARTED > "$D/verdict"; exit 3; }
ACTUAL=$(sha256sum "$PKG" | cut -d' ' -f1)
[ "$ACTUAL" = "$PKG_SHA" ] || { fail "sealed package digest mismatch"; echo NOTSTARTED > "$D/verdict"; exit 3; }
# MENU is a volatile development-core load, not an installable playable library entry.
# Other DE25 packages use the existing package-install/launch smoke engine.
CORE_ID=$(python3 - "$PKG" <<'PY'
import sys,tarfile,tomllib
with tarfile.open(sys.argv[1], 'r:*') as archive:
    member=archive.extractfile('manifest.toml')
    if member is None: raise SystemExit('package manifest missing')
    print(tomllib.loads(member.read().decode())['core']['id'])
PY
)
set +e
if [ "$SW_READY" != 1 ]; then
  echo "smoke skipped because exact-head kit software was not verified" > "$D/logs/smoke.log"
  SMOKE_RC=1
elif [ "$CORE_ID" = fes.menu ]; then
  C_PR="$PR" C_HEAD="$HEAD" C_PKG="$PKG" C_OUT="$D/out" C_VID=/dev/v4l/by-id/fes-ms2109-hil C_FOGCAST="$PRIVATE_FOGCAST" C_API="http://$API" \
    timeout 1800 python3 "$Q/hil-de25-menu.py" >"$D/logs/smoke.log" 2>&1
  SMOKE_RC=$?
else
  cp "$Q/hilcore.py" "$Q/hil-de25-core.py"
  python3 - "$Q/hil-de25-core.py" <<'PY' || { fail "D25 smoke patch generation failed"; echo FAIL > "$D/verdict"; exit 1; }
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
if old_rom not in s: raise SystemExit('HIL smoke ROM selection declaration changed')
s=s.replace(old_rom,new_rom,1)
old_idle = """    rc, lc = kit("curl -s -m5 --unix-socket /run/fogcast/local-control.sock http://local-control/v1/local/status")
    check("idle_after_stop", '"phase":"idle"' in lc, lc[:160])"""
new_idle = "    idle = req('/api/v1/session', 'session-after-stop')\n    check('idle_after_stop', idle.get('state') == 'idle', str(idle.get('state')))"
if old_idle not in s: raise SystemExit('HIL smoke idle probe declaration changed')
s=s.replace(old_idle,new_idle,1)
compile(s, str(p), 'exec')
if 'C_API' not in s or 'session-after-stop' not in s: raise SystemExit('D25 smoke patch verification failed')
p.write_text(s)
PY
  C_KIT=D25 C_PR="$PR" C_TARGET=de25 C_HEAD="$HEAD" C_PKG="$PKG" C_OUT="$D/out" C_VID=/dev/v4l/by-id/fes-ms2109-hil C_FOGCAST="$PRIVATE_FOGCAST" C_API="http://$API" \
    timeout 1800 python3 "$Q/hil-de25-core.py" >"$D/logs/smoke.log" 2>&1
  SMOKE_RC=$?
fi
set -e
[ "$SMOKE_RC" -eq 0 ] && PASS=1 || fail "package lifecycle smoke rc=$SMOKE_RC"
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
if python3 "$RESTORE_TOOL" --baseline "$BASE" --before "$D/out/baseline-before.json" --after "$D/out/baseline-after.json" > "$D/out/restore-verification.json"; then RESTORE=1; else fail "de25_restore.py failed; ledger remains claimed"; fi
if [ "$RESTORE" != 1 ]; then
  SERIAL_TOOL="$FES_SOURCE/scripts/de25_serial_recovery.py"
  [ -f "$SERIAL_TOOL" ] || SERIAL_TOOL="$ROOT/wt/scripts/de25_serial_recovery.py"
  if [ -f "$SERIAL_TOOL" ]; then
    echo "network restore failed; attempting serial-checked J8G reboot" | tee -a "$D/logs/driver.log"
    cp -f "$D/out/baseline-after.json" "$D/out/baseline-after-before-serial.json" 2>/dev/null || true
    rm -f "$D/out/baseline-after.json"
    if python3 "$SERIAL_TOOL" --reboot --serial TRWU8J8G --output "$D/out/serial-reboot.json"; then
      # Wait for the same authenticated J8G endpoint to return, then capture
      # a fresh state. Never select another board or serial port as fallback.
      for attempt in $(seq 1 60); do
        if python3 "$D/snapshot.py" "http://$API" D25 "$D/out/baseline-after-recovered.tmp.json" >/dev/null 2>&1; then
          mv "$D/out/baseline-after-recovered.tmp.json" "$D/out/baseline-after.json"
          break
        fi
        rm -f "$D/out/baseline-after-recovered.tmp.json"
        sleep 5
      done
      if [ -s "$D/out/baseline-after.json" ]; then
        python3 "$SERIAL_TOOL" --receipt-from "$D/out/serial-reboot.json" --serial TRWU8J8G \
          --before-boot-id "$(jq -r '.boot_id' "$D/out/baseline-before.json")" \
          --after-boot-id "$(jq -r '.boot_id' "$D/out/baseline-after.json")" \
          --target-id "$(jq -r '.target_id' "$D/out/baseline-before.json")" \
          --output "$D/out/serial-recovery-receipt.json" > "$D/out/serial-recovery-receipt-summary.json" &&
        python3 "$RESTORE_TOOL" --baseline "$BASE" --before "$D/out/baseline-before.json" \
          --after "$D/out/baseline-after.json" --serial-recovery "$D/out/serial-recovery-receipt.json" \
          > "$D/out/restore-verification.json" && RESTORE=1
      fi
    fi
  fi
  [ "$RESTORE" = 1 ] || fail "network restore and serial-checked recovery failed; ledger remains claimed"
fi
if [ "$RESTORE" = 1 ]; then
  SUM="exact-head=$HEAD package=$(basename "$PKG") sha256=$ACTUAL smoke=$([ "$PASS" = 1 ] && echo PASS || echo FAIL) restore=PASS"
  printf '\n## KIT D25 FREE (baseline verified) — de25-j8g; %s; released %s. Evidence %s\n' "$SUM" "$(date -Is)" "$D" >> "$STATUS"
  echo "restore verified; J8G released FREE (baseline verified)" | tee -a "$D/logs/driver.log"
else
  printf '\n## KIT D25 RESTORE_INCOMPLETE — PR %s head %s; exact-head runner could not verify restore. Keep D25 out of dispatch. Evidence %s\n' "$PR" "$HEAD" "$D" >> "$STATUS"
fi
if [ "$RESTORE" = 1 ] && [ "$PASS" = 1 ]; then echo PASS > "$D/verdict"; exit 0; fi
echo FAIL > "$D/verdict"; exit 1
