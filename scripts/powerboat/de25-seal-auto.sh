#!/bin/bash
# Exact-head DE25 producer. Quartus Pro 26.3 only; no OSS or local fallback.
set -euo pipefail
PR=$1 HEAD=$2 PRODUCER=$3 KIND=${4:-core}
Q=${SEAL_Q:-/home/deano/fes-hil-work/hilq}
R=${SEAL_R:-/home/deano/fes-hil-work/fes}
B=$Q/builds/$PR-${HEAD:0:8} W=$Q/builds/$PR-${HEAD:0:8}/wt-seal
L=$Q/builds/$PR-${HEAD:0:8}/logs/de25-quartus.log
mkdir -p "$B/logs" "$B/package-out" "$Q/heartbeats"
fail(){ echo "FAIL $*" > "$B/seal.result"; echo "FAIL $*" > "$B/core-build.result"; [[ "$KIND" == core || "$KIND" == de25-core ]] && echo "FAIL $*" > "$B/build.result"; exit 1; }
[ "$PRODUCER" = build_menu_package.py ] || [ "$PRODUCER" = build_zx81_package.py ] || fail "unsupported DE25 producer $PRODUCER"
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
printf 'RUNNING\n' > "$B/seal.result"
(
  while :; do date -Is > "$Q/heartbeats/de25-$PR-${HEAD:0:8}.heartbeat"; sleep 30; done
) & HEARTBEAT=$!
trap 'kill "$HEARTBEAT" 2>/dev/null || true' EXIT
case "$PRODUCER" in
  build_menu_package.py) SRC=boards/de25nano/producers/build_menu_package.py ;;
  build_zx81_package.py) SRC=boards/de25nano/producers/build_zx81_package.py ;;
esac
cd "$W/sources/misteross"
set +e
python3 "$SRC" --root "$W/sources/misteross" --quartus-root "$QUARTUS_ROOTDIR" \
  --cache-root "${SEAL_TOOLROOT:-$R/out/cache/misteross-toolchains}" \
  --package-output "$B/package-out" > "$L" 2>&1
rc=$?
set -e
F=$(find "$B/package-out" -maxdepth 2 -type f -name '*.fcore' -print -quit)
[ "$rc" -eq 0 ] && [ -n "$F" ] || fail "producer $PRODUCER rc=$rc"
SHA=$(sha256sum "$F" | cut -d' ' -f1)
RESULT="DONE $F $SHA"
printf '%s\n' "$RESULT" > "$B/seal.result"
printf '%s\n' "$RESULT" > "$B/core-build.result"
[[ "$KIND" != core && "$KIND" != de25-core ]] || printf '%s\n' "$RESULT" > "$B/build.result"
printf 'finished %s %s\n' "$(date -Is)" "$RESULT" >> "$L"
