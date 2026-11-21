#!/bin/bash
# HILQ adapter: keep its unit alive while lander-watch supervises the D25 runner.
set -euo pipefail
KIT=$1 PR=$2 HEAD=$3 BUILD=$4 RUN=$5
[ "$KIT" = D25 ] || { echo "D25 watcher received kit=$KIT" >&2; exit 2; }
: "${LW_REPO:?LW_REPO required}" "${LW_PR:?LW_PR required}" "${LW_HEAD:?LW_HEAD required}"
[ "$LW_REPO" = DeanoC/fes ] && [ "$LW_PR" = "$PR" ] && [ "$LW_HEAD" = "$HEAD" ] || { echo "watcher identity mismatch" >&2; exit 2; }
mkdir -p "$RUN"
WATCH=$(/home/deano/lander-watch/bin/lw-start "hilq-de25-pr${PR}-${HEAD:0:8}" 10800 -- \
  env HILQ_D25_WATCH_JOB=1 /home/deano/lander-hilq/hil-de25-core.sh "$KIT" "$PR" "$HEAD" "$BUILD" "$RUN")
printf '%s\n' "$WATCH" > "$RUN/lander-watch-job.txt"
while [ ! -f "$WATCH/result.txt" ]; do sleep 2; done
cat "$WATCH/result.txt" > "$RUN/lander-watch-result.txt"
cat "$WATCH/result.txt"
case "$(cat "$RUN/verdict" 2>/dev/null || true)" in PASS) exit 0;; *) exit 1;; esac
