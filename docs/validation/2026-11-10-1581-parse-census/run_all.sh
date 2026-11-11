#!/usr/bin/env bash
# Run every form through every reader, three jobs at a time, into OUT.
#   run_all.sh OUT                          the whole matrix; writes OUT/ALL_DONE.all when complete
#   READERS=... FORMS=... TAG=x run_all.sh OUT   a subset; needs its own TAG, writes OUT/ALL_DONE.x
# Every old ALL_DONE.* in OUT is removed first. The tools must be the ones this record used: the three
# Yosys binaries by sha256, Verilator 5.032, Quartus Lite 17.0.2 and Pro 26.3.0; their identities are
# written to OUT/tools.txt. The run is complete only when summarize.py --check finds every selected
# cell to be a result (see summarize.py); otherwise this names what is missing and exits 1.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
OUT=$(mkdir -p "$1" && cd "$1" && pwd) || exit 1
rm -f "$OUT"/ALL_DONE.*
if [ -n "${READERS:-}${FORMS:-}" ] && { [ -z "${TAG:-}" ] || [ "$TAG" = all ]; }; then
  echo "a READERS or FORMS subset needs its own TAG (not 'all')" >&2; exit 1
fi
TAG=${TAG:-all}
FORMS=${FORMS:-$(cd "$HERE/forms" && ls f*.sv | sed 's/\.sv$//')}
READERS=${READERS:-yosys-pc386 yosys-i386 yosys-shared verilator lite2005 pro2005 pro2009}
for f in $FORMS; do [ -f "$HERE/forms/$f.sv" ] || { echo "no such form: $f" >&2; exit 1; }; done
# The control form f08_no_carry, which every reader accepts, runs in every census, subset or not:
# summarize.py --check uses it to tell a reader that works from one with a set-up or licence fault.
case " $FORMS " in *" f08_no_carry "*) ;; *) FORMS="$FORMS f08_no_carry" ;; esac

LITE=${LITE:-$HOME/intelFPGA_lite/17.0/quartus/bin}; PRO=${PRO:-/data/altera_pro/26.3/quartus/bin}
VERILATOR=$(command -v "${VERILATOR:-verilator}") || VERILATOR=
export LITE PRO VERILATOR
pin() {  # name path want-sha256
  [ -n "$2" ] && [ -x "$2" ] || { echo "$1: not executable: ${2:-(unset)}" >&2; exit 1; }
  got=$(sha256sum "$2" | cut -d' ' -f1)
  [ "$got" = "$3" ] || { echo "$1: $2 has sha256 $got, not the record's $3" >&2; exit 1; }
  echo "$1 $2 sha256 $got"
}
banner() {  # name tool want
  [ -n "$2" ] && [ -x "$2" ] || { echo "$1: not executable: ${2:-(unset)}" >&2; exit 1; }
  got=$("$2" --version 2>&1 | grep -m1 -E "Version|Verilator") || true
  case $got in *"$3"*) echo "$1 $2 $got" ;; *) echo "$1: $2 reports '$got', not $3" >&2; exit 1 ;; esac
}
{
  for r in $READERS; do
    case $r in
      yosys-pc386) pin $r "${YOSYS_PC386:-}" b6cd1fd2f56e22e21a82b3ed31b0c2068df3b0951c8a9b613861d589c74fbf5f ;;
      yosys-i386) pin $r "${YOSYS_I386:-}" bbce80f217ce338b5613a4ea961fd7155e456813c1d2b559ed4a171416abe053 ;;
      yosys-shared) pin $r "${YOSYS_SHARED:-}" 6747da54a9f759f56d078ee30b434094c2aa79ec8aef4fd2c7f613ad47131efa ;;
      verilator) banner $r "$VERILATOR" "Verilator 5.032" ;;
      lite2005) banner $r "$LITE/quartus_sh" "17.0.2 Build 602"; [ -x "$LITE/quartus_map" ] || exit 1 ;;
      pro2005|pro2009) banner $r "$PRO/quartus_sh" "26.3.0 Build 114"; [ -x "$PRO/quartus_syn" ] || exit 1 ;;
      *) echo "unknown reader: $r" >&2; exit 1 ;;
    esac
  done
} > "$OUT/tools.txt" || exit 1

for r in $READERS; do for f in $FORMS; do echo "$r $f"; done; done |
  xargs -P 3 -L 1 "$HERE/census.sh" "$OUT"
status=$?
READERS="$READERS" FORMS="$FORMS" python3 "$HERE/summarize.py" --check "$OUT"
complete=$?
if [ "$status" -ne 0 ] || [ "$complete" -ne 0 ]; then
  echo "census incomplete (workers' status $status)" >&2
  exit 1
fi
{ echo "readers: $READERS"; echo "forms: $(echo $FORMS)"; } > "$OUT/ALL_DONE.$TAG"
