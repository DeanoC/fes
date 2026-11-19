#!/bin/bash
# D12 condition 3: the PR's own build passes the core's normal timing and seal
# gates, run at the head that will merge.
#
# The merge-forward clause carries the *comparison* (conditions 1 and 2); it
# does not carry this one, because condition 3 is about the identity that
# actually ships, and a merge that moves the production BUILD_ID invalidates a
# recorded gated build. It is therefore re-run here rather than reused.
#
# Each producer's own CLI is used, with no arguments that relax a gate.
set -u
WHAT="$1"                       # artefact key
D=/data/fes1861-netlist
TREE="$D/identity/c3head/sources/misteross"
CACHE="$D/cache"
OUT="$D/cond3"; LOGS="$D/cond3-logs"
mkdir -p "$OUT" "$LOGS"
log="$LOGS/$WHAT.log"
if [ -f "$OUT/$WHAT.done" ]; then echo "SKIP $WHAT"; exit 0; fi
cd "$TREE" || exit 2
export QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus
case "$WHAT" in
  menu)      set -- python3 scripts/build_fes_menu_package.py --cache-root "$CACHE" ;;
  splash)    set -- python3 scripts/build_fes_splash.py ;;
  c64)       set -- python3 scripts/build_fes_c64_oss.py --cache-root "$CACHE" ;;
  spectrum)  set -- python3 scripts/build_fes_spectrum_oss.py --cache-root "$CACHE" --model 48 ;;
  spectrum128)      set -- python3 scripts/build_fes_spectrum_oss.py --cache-root "$CACHE" --model 128 ;;
  spectrum-plus3)   set -- python3 scripts/build_fes_spectrum_oss.py --cache-root "$CACHE" --model plus3 ;;
  pentagon)         set -- python3 scripts/build_fes_spectrum_oss.py --cache-root "$CACHE" --model pentagon ;;
  zx81|zx80|ts1000|ts1500)
    set -- python3 scripts/build_fes_zx81_quartus.py --model "$WHAT" --cache-root "$CACHE" ;;
  *) echo "unknown artefact: $WHAT"; exit 2 ;;
esac
echo "RUN  cond3 $WHAT: $*"
"$@" > "$log" 2>&1
rc=$?
if [ $rc -eq 0 ]; then
  touch "$OUT/$WHAT.done"
  echo "PASS cond3 $WHAT"
else
  echo "FAIL cond3 $WHAT rc=$rc -- see $log"
fi
exit $rc
