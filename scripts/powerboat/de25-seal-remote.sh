#!/bin/bash
# de25-seal-remote.sh PR FULLHEAD SELECTION [KIND]
# HILQ's DE25 seal transport. seal-remote.sh hands every de25:* selection here.
# Runs de25-seal-auto.sh on fes-build-3090 (Quartus Pro 26.3), copies every
# admitted package back, re-verifies each digest, and writes the same
# seal.result/core-build.result/build.result lines here, one per package:
#   DONE <fcore> <sha256> <slug>
# If the 3090's license rejects the DE25 device, the same exact head is retried
# on Powerboat's Quartus Pro under the shared-host lock. Never falls back to
# Quartus Lite or an OSS flow.
set -u
PR=$1; H=$2; SELECTION=$3; K=${4:-core}
Q=/home/deano/lander-hilq; B=$Q/builds/$PR-${H:0:8}
HOST=${SEAL_HOST-fes-build-3090}; RW=/home/deano/fes-hil-work; RB=$RW/hilq/builds/$PR-${H:0:8}
SSH=(ssh -n -o BatchMode=yes -o ConnectTimeout=15 -o ServerAliveInterval=30)
QUARTUS_PRO=/data/altera_pro/26.3/quartus
VENDOR=/data/fes-de25-1297-9b36e4729/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
# One producer may take PRODUCER_TIME_CAP (10800 s) inside de25-seal-auto.sh;
# this bounds a whole multi-package seal. HILQ's unit cap sits above it.
SEAL_CAP=${DE25_SEAL_CAP:-27000}
mkdir -p "$B/logs" "$B/package-out"
fail(){ echo "FAIL $1" > "$B/seal.result"; echo "FAIL $1" > "$B/core-build.result"; [[ "$K" == core || "$K" == de25-core ]] && echo "FAIL $1" > "$B/build.result"; exit 1; }
case "$SELECTION" in de25:?*) ;; *) fail "not a DE25 selection: $SELECTION" ;; esac
[[ "$SELECTION" =~ ^de25:[A-Za-z0-9@.,_-]+$ ]] || fail "invalid DE25 selection $SELECTION"
[[ "$H" =~ ^[0-9a-f]{40}$ ]] || fail "full exact HEAD required"
# Keep the exact-head job visible while it waits for the shared Quartus lock.
mkdir -p "$Q/heartbeats"
HB="$Q/heartbeats/de25-$PR-${H:0:8}.heartbeat"
(while :; do date -Is > "$HB"; sleep 30; done) & HB_PID=$!
trap 'kill "$HB_PID" 2>/dev/null || true' EXIT
if [ -z "$HOST" ] || ! "${SSH[@]}" "$HOST" true 2>/dev/null; then
  fail "DE25 requires fes-build-3090 Quartus Pro 26.3; no local fallback"
fi
echo RUNNING > "$B/seal.result"
rm -f "$B/core-build.result" "$B/build.result"
"${SSH[@]}" "$HOST" "mkdir -p $RW/hilq && rm -f $RB/seal.result $RB/core-build.result $RB/build.result" || fail "remote prep"
rsync -a "$Q/de25-seal-auto.sh" "$HOST:$RW/hilq/de25-seal-auto.sh" </dev/null || fail "remote runner copy"
"${SSH[@]}" "$HOST" "cd $RW/hilq && QUARTUS_ROOTDIR=$QUARTUS_PRO FES_DE25_VENDOR_ARCHIVE=$VENDOR SEAL_Q=$RW/hilq SEAL_R=$RW/fes SEAL_TOOLROOT=$RW/toolchains timeout --signal=TERM --kill-after=60 $SEAL_CAP flock -w 2400 /tmp/lander-quartus-3090.lock bash ./de25-seal-auto.sh $PR $H $SELECTION $K"; rrc=$?
rsync -a "$HOST:$RB/logs/" "$B/logs/remote/" </dev/null
rsync -a "$HOST:$RB/de25-plan.json" "$HOST:$RB/de25-jobs.tsv" "$B/" </dev/null 2>/dev/null
r=$("${SSH[@]}" "$HOST" "cat $RB/seal.result" 2>/dev/null)
LOCAL=0
if [[ "$r" != DONE* ]] && grep -qs 'Error (119013): Current license file does not support the A5EB013BB23BE4SCS device' "$B"/logs/remote/de25-quartus-*.log; then
  echo "DE25 Pro 3090 license rejected A5EB013BB23BE4SCS; retrying same exact head on Powerboat Pro under shared-host lock" | tee -a "$B/logs/seal.log"
  cp "$B/seal.result" "$B/logs/remote-pro-license-failure.result"
  (
    exec 7>/home/deano/fes-hil/locks/lander-sharedhost.lock
    flock -w 600 7 || exit 93
    QUARTUS_ROOTDIR=$QUARTUS_PRO FES_DE25_VENDOR_ARCHIVE=$VENDOR \
    SEAL_Q="$Q" SEAL_R=/home/deano/codex-jobs/de25-kit-parity-p3-20261116/wt \
    SEAL_TOOLROOT="$Q/toolchains" \
    timeout --signal=TERM --kill-after=60 "$SEAL_CAP" flock -w 2400 /tmp/lander-quartus-powerboat-pro.lock \
    bash "$Q/de25-seal-auto.sh" "$PR" "$H" "$SELECTION" "$K"
  ); rrc=$?
  r=$(cat "$B/seal.result" 2>/dev/null || true)
  LOCAL=1
  echo "Powerboat DE25 Pro retry rc=$rrc result=${r:-missing}" | tee -a "$B/logs/seal.log"
fi
case "$r" in DONE\ *) ;; *) fail "DE25 Quartus Pro build failed on $HOST (rc=$rrc): $(printf '%s' "${r:-no seal.result}" | head -n 1)" ;; esac
LINES=()
while read -r STATE RF SH SLUG EXTRA; do
  [ "$STATE" = DONE ] && [[ "$SH" =~ ^[0-9a-f]{64}$ ]] && [[ "$SLUG" =~ ^fes\.[a-z0-9][a-z0-9._-]*$ ]] && [ -z "$EXTRA" ] || fail "malformed DE25 seal line: $STATE $RF $SH $SLUG"
  if [ "$LOCAL" = 1 ]; then
    O=$RF
  else
    mkdir -p "$B/package-out/$SLUG"
    O=$B/package-out/$SLUG/$(basename "$RF")
    rsync -a "$HOST:$RF" "$O" </dev/null || fail "copy back $RF"
  fi
  [ -f "$O" ] && [ "$(sha256sum "$O" | cut -c1-64)" = "$SH" ] || fail "sha mismatch after copy-back ($SLUG)"
  LINES+=("DONE $O $SH $SLUG")
done <<< "$r"
[ "${#LINES[@]}" -ge 1 ] || fail "DE25 seal reported no packages"
printf '%s\n' "${LINES[@]}" > "$B/core-build.result"
[[ "$K" != core && "$K" != de25-core ]] || printf '%s\n' "${LINES[@]}" > "$B/build.result"
printf '%s\n' "${LINES[@]}" > "$B/seal.result"
printf 'DE25 Quartus Pro 26.3 seal (%s, %s): %s\n' "$SELECTION" "$([ "$LOCAL" = 1 ] && echo Powerboat || echo "$HOST")" "${LINES[*]}" >> "$B/logs/seal.log"
exit 0
