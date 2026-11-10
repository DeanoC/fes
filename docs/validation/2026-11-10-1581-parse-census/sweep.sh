#!/usr/bin/env bash
# Per-file parse sweep under Quartus 17.0.2 Lite at SV2005 (quartus_map --analyze_file). Pro has no
# --analyze_file (error 23024), so Pro reads whole blocks through elab.sh instead.
# Usage: [PKGS="pkg.sv ..."] sweep.sh <out-dir> <file>...   Each file is parsed alone, with the
# package files in PKGS put in the project first. Quartus stops at its first errors in a file, so a
# file's log names only its first failing construct(s). Writes <out-dir>/lite/<file>/{log,rc}.
# A file is a result when Quartus said "was successful" or gave an error id; anything else
# (timeout, crash, a tool that did not run) makes the sweep exit 1 and is marked in <file>/notrun.
set -u
OUT=$(mkdir -p "$1" && cd "$1" && pwd) || exit 1; shift
LITE=${LITE:-$HOME/intelFPGA_lite/17.0/quartus/bin}
[ -x "$LITE/quartus_map" ] || { echo "not executable: $LITE/quartus_map" >&2; exit 1; }
pkgs=(); for p in ${PKGS:-}; do pkgs+=("$(realpath -e "$p")") || { echo "missing package $p" >&2; exit 1; }; done
files=(); for f in "$@"; do files+=("$(realpath -e "$f")") || { echo "missing input $f" >&2; exit 1; }; done
failed=0
for f in "${files[@]}"; do
  W=$OUT/lite/$(basename "$f" .sv); rm -rf "$W"; mkdir -p "$W"
  ( cd "$W" || exit 1
    { echo 'set_global_assignment -name FAMILY "Cyclone V"'; echo 'set_global_assignment -name DEVICE 5CSEBA6U23I7'
      echo "set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005"
      echo "set_global_assignment -name NUM_PARALLEL_PROCESSORS 2"
      for p in ${pkgs[@]+"${pkgs[@]}"}; do echo "set_global_assignment -name SYSTEMVERILOG_FILE $p"; done
      echo "set_global_assignment -name SYSTEMVERILOG_FILE $f"; } > af.qsf
    echo 'PROJECT_REVISION = "af"' > af.qpf
    timeout 900 "$LITE/quartus_map" af --analyze_file="$f" > log 2>&1; echo $? > rc
    rm -rf db incremental_db )
  rc=$(cat "$W/rc")
  if ! grep -q "was successful" "$W/log" && ! { [ "$rc" -ne 0 ] && [ "$rc" -lt 124 ] && grep -qE "^Error \([0-9]+\)" "$W/log"; }; then
    echo "rc $rc without a Quartus verdict" > "$W/notrun"; echo "not a result: $f (rc $rc)" >&2; failed=1
  fi
done
exit $failed
