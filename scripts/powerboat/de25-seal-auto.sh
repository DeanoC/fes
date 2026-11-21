#!/bin/bash
# Exact-head DE25 producers. Quartus Pro 26.3 only; no OSS or local fallback.
#   de25-seal-auto.sh PR HEAD SELECTION [KIND]
# SELECTION is de25:auto, de25:all or de25:<core_id>[@model][,...]. The exact
# head's scripts/de25_seal_plan.py turns it into one job per registered DE25
# recipe and model; anything that is not a registered DE25 Quartus Pro producer
# is refused. Each admitted package is one line of core-build.result:
#   DONE <fcore> <sha256> <slug>
set -euo pipefail
PR=$1 HEAD=$2 SELECTION=$3 KIND=${4:-core}
Q=${SEAL_Q:-/home/deano/fes-hil-work/hilq}
R=${SEAL_R:-/home/deano/fes-hil-work/fes}
B=$Q/builds/$PR-${HEAD:0:8} W=$Q/builds/$PR-${HEAD:0:8}/wt-seal BW=$Q/builds/$PR-${HEAD:0:8}/wt-base
PRODUCER_TIME_CAP=10800
mkdir -p "$B/logs" "$B/package-out" "$Q/heartbeats"
fail(){ echo "FAIL $*" > "$B/seal.result"; echo "FAIL $*" > "$B/core-build.result"; [[ "$KIND" == core || "$KIND" == de25-core ]] && echo "FAIL $*" > "$B/build.result"; exit 1; }
[[ "$HEAD" =~ ^[0-9a-f]{40}$ ]] || fail "full exact HEAD required"
case "$SELECTION" in de25:?*) SELECT=${SELECTION#de25:} ;; *) fail "not a DE25 selection: $SELECTION" ;; esac
[[ "$SELECT" =~ ^[A-Za-z0-9@.,_-]+$ ]] || fail "invalid DE25 selection $SELECTION"
[ -n "${QUARTUS_ROOTDIR:-}" ] && [ -x "$QUARTUS_ROOTDIR/bin/quartus_sh" ] || fail "Quartus Pro 26.3 installation missing"
"$QUARTUS_ROOTDIR/bin/quartus_sh" --version > "$B/logs/quartus-version.txt" 2>&1 || fail "Quartus version query failed"
grep -q '26.3' "$B/logs/quartus-version.txt" || fail "Quartus Pro 26.3 required"
free=$(df --output=avail -BG /home/deano | tail -1 | tr -dc 0-9); [ "$free" -ge 30 ] || fail "low disk ${free}G (minimum 30G)"
if [ ! -d "$W" ]; then
  if ! git -C "$R" cat-file -e "$HEAD^{commit}" 2>/dev/null; then
    if [[ "$PR" == baseline-* ]]; then
      git -C "$R" fetch -q origin main || fail fetch_main
      if ! git -C "$R" cat-file -e "$HEAD^{commit}" 2>/dev/null; then
        git -C "$R" fetch -q origin "$HEAD" || fail fetch_exact_head
      fi
    else
      git -C "$R" fetch -q origin "pull/$PR/head" || fail fetch_pr
    fi
  fi
  git -C "$R" worktree add -q --detach "$W" "$HEAD" || fail worktree
fi
[ "$(git -C "$W" rev-parse HEAD)" = "$HEAD" ] && [ -z "$(git -C "$W" status --porcelain)" ] || fail "worktree not clean at exact head"
[ -f "$W/scripts/de25_seal_plan.py" ] || fail "exact head has no scripts/de25_seal_plan.py"
# auto compares the head's producer identities with the merge base's.
BASE_ARGS=()
if [ "$SELECT" = auto ]; then
  git -C "$R" fetch -q origin main || fail fetch_main
  BASE=$(git -C "$R" merge-base FETCH_HEAD "$HEAD") || fail "no merge base with origin/main"
  [ -d "$BW" ] && git -C "$R" worktree remove --force "$BW"
  git -C "$R" worktree add -q --detach "$BW" "$BASE" || fail base_worktree
  BASE_ARGS=(--base-root "$BW")
  printf 'merge-base %s\n' "$BASE" > "$B/logs/de25-plan-base.txt"
fi
set +e
python3 -I "$W/scripts/de25_seal_plan.py" --select "$SELECT" "${BASE_ARGS[@]}" --output-dir "$B" > "$B/logs/de25-plan.log" 2>&1
rc=$?
set -e
[ -d "$BW" ] && git -C "$R" worktree remove --force "$BW"
[ "$rc" -eq 0 ] && [ -s "$B/de25-jobs.tsv" ] || fail "DE25 producer plan failed: $(tail -n 1 "$B/logs/de25-plan.log")"
printf 'RUNNING\n' > "$B/seal.result"
(
  while :; do date -Is > "$Q/heartbeats/de25-$PR-${HEAD:0:8}.heartbeat"; sleep 30; done
) & HEARTBEAT=$!
trap 'kill "$HEARTBEAT" 2>/dev/null || true' EXIT
cd "$W/sources/misteross"
RESULTS=()
while IFS=$'\t' read -r SLUG SRC MODEL CORE; do
  [[ "$SLUG" =~ ^fes\.[a-z0-9][a-z0-9._-]*$ && "$SRC" == boards/de25nano/producers/*.py && -f "$SRC" ]] || fail "invalid DE25 plan row: $SLUG $SRC"
  OUT=$W/sources/misteross/build/hilq-de25/$SLUG
  PKG_OUT=$B/package-out/$SLUG
  L=$B/logs/de25-quartus-$SLUG.log
  rm -rf "$OUT" "$PKG_OUT"; mkdir -p "$PKG_OUT" "$(dirname "$OUT")"
  MODEL_ARGS=(); [ "$MODEL" = - ] || MODEL_ARGS=(--model "$MODEL")
  printf 'started %s %s %s model=%s\n' "$(date -Is)" "$SLUG" "$SRC" "$MODEL" >> "$B/logs/de25-seal.log"
  set +e
  timeout --signal=TERM --kill-after=60s "$PRODUCER_TIME_CAP" python3 "$SRC" \
    --root "$W/sources/misteross" --quartus-root "$QUARTUS_ROOTDIR" \
    --cache-root "${SEAL_TOOLROOT:-$R/out/cache/misteross-toolchains}" \
    --output "$OUT" --package-output "$PKG_OUT" "${MODEL_ARGS[@]}" < /dev/null > "$L" 2>&1
  rc=$?
  set -e
  if [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; then
    fail "producer $SRC ($SLUG) exceeded ${PRODUCER_TIME_CAP}s time cap (rc=$rc)"
  fi
  # Admission is the producer's own summary and the exported package, not its exit code.
  if ! python3 -I - "$OUT/build-summary.json" "$HEAD" "$MODEL" > "$B/logs/de25-admission-$SLUG.txt" 2>&1 <<'PY'
import json, sys
summary = json.load(open(sys.argv[1]))
head, model = sys.argv[2], sys.argv[3]
assert summary.get('revision') == head, 'summary revision differs from exact head'
assert str(summary.get('classification', '')).endswith('-package-candidate'), 'not a package candidate'
assert summary.get('package_timing_admission') is True, 'package timing not admitted'
assert model == '-' or summary.get('model') == model, 'summary model differs from plan'
print('admitted', summary['build_id'])
PY
  then
    fail "producer $SRC ($SLUG) rc=$rc: $(tail -n 1 "$B/logs/de25-admission-$SLUG.txt")"
  fi
  mapfile -t FCORES < <(find "$PKG_OUT" -maxdepth 1 -type f -name '*.fcore')
  [ "${#FCORES[@]}" -eq 1 ] || fail "producer $SRC ($SLUG) rc=$rc: expected one .fcore, found ${#FCORES[@]}"
  F=${FCORES[0]}
  SHA=$(sha256sum "$F" | cut -d' ' -f1)
  RESULTS+=("DONE $F $SHA $SLUG")
  printf 'finished %s DONE %s %s %s (producer rc=%s)\n' "$(date -Is)" "$F" "$SHA" "$SLUG" "$rc" >> "$B/logs/de25-seal.log"
done < "$B/de25-jobs.tsv"
[ "${#RESULTS[@]}" -ge 1 ] || fail "DE25 plan selected no packages"
printf '%s\n' "${RESULTS[@]}" > "$B/seal.result"
printf '%s\n' "${RESULTS[@]}" > "$B/core-build.result"
[[ "$KIND" != core && "$KIND" != de25-core ]] || printf '%s\n' "${RESULTS[@]}" > "$B/build.result"
