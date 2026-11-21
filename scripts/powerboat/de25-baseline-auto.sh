#!/bin/bash
# Independent D25 main promotion. A/B promotion scripts and records are untouched.
set -Eeuo pipefail
Q=/home/deano/lander-hilq
ROOT=/home/deano/codex-jobs/de25-kit-parity-p3-20261116
REPO=$ROOT/wt
BASE=/home/deano/fes-hil/baselines/current-restore-de25.json
LOCK=/home/deano/fes-hil/hilq-state/de25-baseline-promote.lock
E=/home/deano/fes-hil/de25-promotion

# Return 0 if $1 records a verified MENU+ZX81 promotion of the 40-char SHA in $2.
d25_already_promoted() {
  local answer
  answer=$(python3 -c 'import json, re, sys
path, head = sys.argv[1], sys.argv[2]
if not re.fullmatch(r"[0-9a-f]{40}", head):
    print("no"); raise SystemExit(0)
try:
    data = json.loads(open(path, encoding="utf-8").read())
except (OSError, json.JSONDecodeError):
    print("no"); raise SystemExit(0)
if not isinstance(data, dict):
    print("no"); raise SystemExit(0)
source = data.get("source") or ""
evidence = data.get("verified_restore_evidence") or ""
verified_at = data.get("verified_at_utc") or ""
match = re.search(r"fetched origin/main ([0-9a-f]{40})", source)
if not match or match.group(1) != head or not verified_at:
    print("no"); raise SystemExit(0)
parts = [part for part in evidence.split(";") if part]
has_menu = any("baseline-menu-" in part for part in parts)
has_zx81 = any("baseline-zx81-" in part for part in parts)
print("yes" if has_menu and has_zx81 and len(parts) >= 2 else "no")
' "$1" "$2" || true)
  [[ "${answer:-no}" == "yes" ]]
}

d25_resolve_main_head() {
  if [[ -n "${D25_DRY_HEAD:-}" ]]; then
    HEAD=$D25_DRY_HEAD
    MAIN_HEAD=$D25_DRY_HEAD
    [[ "$HEAD" =~ ^[0-9a-f]{40}$ ]] || { echo "FAIL invalid fetched main head"; exit 1; }
    return 0
  fi
  git -C "$REPO" fetch origin main
  MAIN_HEAD=$(git -C "$REPO" rev-parse FETCH_HEAD)
  if [[ -n "${D25_PROMOTION_HEAD:-}" ]]; then
    [[ "$D25_PROMOTION_HEAD" =~ ^[0-9a-f]{40}$ ]] || { echo "FAIL requested D25 promotion head is not a full SHA"; exit 1; }
    git -C "$REPO" fetch origin "$D25_PROMOTION_HEAD"
    HEAD=$(git -C "$REPO" rev-parse FETCH_HEAD)
    [[ "$HEAD" = "$D25_PROMOTION_HEAD" ]] || { echo "FAIL requested D25 promotion head resolved differently"; exit 1; }
  else
    HEAD=$MAIN_HEAD
  fi
  [[ "$HEAD" =~ ^[0-9a-f]{40}$ ]] || { echo "FAIL invalid fetched main head"; exit 1; }
  [[ "$HEAD" = "$MAIN_HEAD" ]] || { echo "FAIL requested D25 promotion head is not fetched origin/main ($MAIN_HEAD)"; exit 1; }
}

if [[ "${DRY_RUN:-}" == "1" || "${1:-}" == "--dry-run" ]]; then
  [[ -n "${D25_DRY_BASE:-}" ]] && BASE=$D25_DRY_BASE
  d25_resolve_main_head
  if d25_already_promoted "$BASE" "$HEAD"; then
    echo "SKIP already promoted $HEAD"
  else
    echo "DRY_RUN would seal and HIL $HEAD"
  fi
  exit 0
fi

mkdir -p "$E"
exec 9>"$LOCK"
flock -n 9 || { echo "AUTO_WAIT D25 promotion lock busy"; exit 0; }
ledger=/home/deano/kit-status/de25-TRWU8J8G-STATUS.md
last=$(awk '/^## /{line=$0} END{print line}' "$ledger")
if [[ "$last" != *"KIT D25 FREE (baseline verified)"* ]] || ! flock -n /home/deano/kit-status/de25-TRWU8J8G-STATUS.lock -c true; then
  echo "AUTO_WAIT J8G not FREE"; exit 0
fi
free=$(df --output=avail -BG /home/deano | tail -1 | tr -dc 0-9)
[ "$free" -ge 30 ] || { echo "FAIL disk below 30G: ${free}G"; exit 1; }
d25_resolve_main_head
if d25_already_promoted "$BASE" "$HEAD"; then
  echo "SKIP already promoted $HEAD"
  exit 0
fi
mkdir -p "$Q/heartbeats"
HEARTBEAT="$Q/heartbeats/de25-promotion-${HEAD:0:8}.heartbeat"
(while :; do date -Is > "$HEARTBEAT"; sleep 30; done) &
HEARTBEAT_PID=$!
trap 'kill "$HEARTBEAT_PID" 2>/dev/null || true' EXIT
cp -p "$BASE" "$E/current-restore-de25.before-$HEAD.json"
for spec in menu:fes.menu zx81:fes.zx81; do
  name=${spec%%:*}; core=${spec#*:}; pr="baseline-$name-$(date +%Y%m%d-%H%M%S)"; build="$Q/builds/$pr-${HEAD:0:8}"
  bash "$Q/seal-remote.sh" "$pr" "$HEAD" "de25:$core" core
  grep -q '^DONE ' "$build/core-build.result"
  run="$Q/runs/baseline-$name-${HEAD:0:8}-D25-$(date +%Y%m%d-%H%M%S)"
  LW_REPO=DeanoC/fes LW_PR=0 LW_HEAD="$HEAD" \
    /home/deano/lander-watch/bin/lw-start "d25-baseline-$name-${HEAD:0:8}" 10800 -- \
    env /bin/bash "$Q/hil-de25-core.sh" D25 "$pr" "$HEAD" "$build" "$run" > "$E/$name-watcher-dir.txt"
  watcher=$(cat "$E/$name-watcher-dir.txt")
  run="$Q/runs/baseline-$name-${HEAD:0:8}-D25-*"
  while [ ! -f "$watcher/result.txt" ]; do sleep 5; done
  run=$(find "$Q/runs" -maxdepth 1 -type d -name "baseline-$name-${HEAD:0:8}-D25-*" -printf '%T@ %p\n' | sort -nr | head -1 | cut -d' ' -f2-)
  printf '%s\n' "$run" > "$E/$name-run-dir.txt"
  cat "$watcher/result.txt" > "$E/$name-watcher-result.txt"
  [ "$(cat "$run/verdict" 2>/dev/null || true)" = PASS ] || { echo "FAIL $name exact-main HIL"; exit 1; }
done
VALIDATOR="$E/de25-promotion-$HEAD.py"
git -C "$REPO" show "$HEAD:scripts/de25_promotion.py" > "$VALIDATOR"
python3 "$VALIDATOR" --baseline "$BASE" --head "$HEAD" \
  --menu-run "$(cat "$E/menu-run-dir.txt")" --zx81-run "$(cat "$E/zx81-run-dir.txt")"
