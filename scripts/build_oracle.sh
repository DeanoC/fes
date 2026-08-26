#!/usr/bin/env bash

# Build the optional Quartus reference lane.  This file is intentionally the
# only wrapper that knows about the proprietary compiler.  In particular, it
# never searches PATH: callers must opt in with QUARTUS_ROOTDIR.
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)"
PYTHON="${PYTHON:-python3}"
EXP="${EXP:-010_blinky}"
TARGET="5CSEBA6U23I7"
PRINT_COMMANDS=0
POSITIONAL_SET=0

usage() {
    printf '%s\n' \
        "usage: ${0##*/} [--print-commands] [--experiment EXP]"
}

fail() {
    printf '%s\n' "$*" >&2
    exit 2
}

while (( $# > 0 )); do
    case "$1" in
        --print-commands)
            PRINT_COMMANDS=1
            shift
            ;;
        --experiment)
            (( $# >= 2 )) || fail "--experiment requires a value"
            EXP=$2
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            (( $# == 0 )) || fail "unexpected argument: $1"
            ;;
        -* )
            fail "unknown option: $1"
            ;;
        *)
            (( POSITIONAL_SET == 0 )) || fail "unexpected argument: $1"
            EXP=$1
            POSITIONAL_SET=1
            shift
            ;;
    esac
done

if [[ ! "$EXP" =~ ^[0-9][0-9][0-9]_[a-z0-9_]+$ ]]; then
    fail "invalid EXP: $EXP"
fi

# Do this check before touching build/oracle.  The unavailable message is a
# supported interface: OSS and simulation must remain usable without Quartus.
if [[ -z "${QUARTUS_ROOTDIR:-}" ]]; then
    fail "Quartus oracle unavailable; OSS and simulation remain usable (set QUARTUS_ROOTDIR to an installed Quartus 17.0.2 tree)"
fi

cd -- "$ROOT"

rtl_rel="experiments/$EXP/rtl/top.v"
oracle_dir="$ROOT/build/oracle/$EXP"
project_dir="$oracle_dir/project"
project_output_dir="$project_dir/output_files"
oracle_qpf="$ROOT/experiments/$EXP/oracle/top.qpf"
oracle_qsf="$ROOT/experiments/$EXP/oracle/top.qsf"
qsf_rel="boards/de10nano/pins.qsf"
sdc_rel="boards/de10nano/clocks.sdc"
rtl="$ROOT/$rtl_rel"
qsf="$ROOT/$qsf_rel"
sdc="$ROOT/$sdc_rel"
run_logged="$ROOT/scripts/run_logged.sh"
collector="$ROOT/scripts/collect_manifest.py"

rbf="$oracle_dir/top.rbf"
fit_report="$oracle_dir/top.fit.rpt"
timing_report="$oracle_dir/top.sta.rpt"
summary="$oracle_dir/build-summary.json"
timing_summary="$oracle_dir/timing.txt"
version_log="$oracle_dir/quartus-version.log"
quartus_log="$oracle_dir/quartus.log"
summary_log="$oracle_dir/summary.log"
manifest_log="$oracle_dir/manifest-collect.log"
manifest="$oracle_dir/manifest.json"

path_has_symlink_component() {
    local path=$1
    local current component
    local -a components
    case "$path" in
        /*)
            current=/
            path=${path#/}
            ;;
        *)
            current=$PWD
            ;;
    esac
    IFS='/' read -r -a components <<< "$path"
    for component in "${components[@]}"; do
        [[ -z "$component" || "$component" == "." ]] && continue
        if [[ "$component" == ".." ]]; then
            current=${current%/*}
            [[ -n "$current" ]] || current=/
            continue
        fi
        if [[ "$current" == "/" ]]; then
            current="/$component"
        else
            current="$current/$component"
        fi
        [[ -L "$current" ]] && return 0
    done
    return 1
}

require_regular() {
    local path=$1
    local label=$2
    [[ -f "$path" && ! -L "$path" ]] || fail "missing regular $label: $path"
    if path_has_symlink_component "$path"; then
        fail "$label path contains a symlink component: $path"
    fi
}

validate_output_tree() {
    local build_root="$ROOT/build"
    local canonical_build existing existing_real
    [[ "$oracle_dir" == "$build_root/oracle/$EXP" ]] \
        || fail "unsafe oracle output path: $oracle_dir"
    path_has_symlink_component "$oracle_dir" \
        && fail "oracle output path contains a symlink component: $oracle_dir"
    if [[ -e "$build_root" ]]; then
        [[ -d "$build_root" && ! -L "$build_root" ]] \
            || fail "build root is not a regular directory: $build_root"
        canonical_build="$(CDPATH= cd -- "$build_root" && pwd -P)"
        [[ "$canonical_build" == "$build_root" ]] \
            || fail "build root resolves outside repository: $build_root"
    fi

    existing="$oracle_dir"
    while [[ ! -e "$existing" && ! -L "$existing" ]]; do
        existing=$(dirname -- "$existing")
    done
    [[ -d "$existing" && ! -L "$existing" ]] \
        || fail "oracle output parent is not a directory: $existing"
    existing_real="$(CDPATH= cd -- "$existing" && pwd -P)"
    if [[ -n "${canonical_build:-}" ]]; then
        [[ "$existing_real" == "$canonical_build" || "$existing_real" == "$canonical_build"/* ]] \
            || fail "oracle output resolves outside repository build root: $oracle_dir"
    else
        [[ "$existing_real" == "$ROOT" || "$existing_real" == "$ROOT"/* ]] \
            || fail "oracle output resolves outside repository: $oracle_dir"
    fi

    local path
    for path in "$oracle_dir" "$project_dir" "$project_output_dir" \
        "$project_dir/top.qpf" "$project_dir/top.qsf" \
        "$rbf" "$fit_report" "$timing_report" "$summary" "$timing_summary" \
        "$version_log" "$quartus_log" "$summary_log" "$manifest_log" "$manifest"; do
        [[ ! -L "$path" ]] || fail "oracle output path is a symlink: $path"
    done

    # Quartus creates several nested files below the staged project.  Reject
    # every pre-existing symlink before any copy/compile operation so a leaf or
    # directory cannot redirect writes outside build/oracle.
    if [[ -d "$oracle_dir" ]]; then
        local nested_symlink
        nested_symlink="$(find -P "$oracle_dir" -type l -print -quit)"
        [[ -z "$nested_symlink" ]] \
            || fail "oracle output tree contains a symlink: $nested_symlink"
    fi
}

validate_quartus_root() {
    local root="$QUARTUS_ROOTDIR"
    [[ "$root" == /* ]] || root="$ROOT/$root"
    [[ "$root" != *[[:space:]]* ]] \
        || fail "Quartus oracle unavailable; QUARTUS_ROOTDIR must not contain spaces: $root"
    path_has_symlink_component "$root" \
        && fail "Quartus oracle unavailable; QUARTUS_ROOTDIR contains a symlink component: $root"
    [[ -d "$root" && ! -L "$root" ]] \
        || fail "Quartus oracle unavailable; QUARTUS_ROOTDIR is not a directory: $root"

    local candidate
    for candidate in "$root/quartus/bin/quartus_sh" "$root/bin/quartus_sh"; do
        if [[ -f "$candidate" && ! -L "$candidate" && -x "$candidate" ]] \
            && ! path_has_symlink_component "$candidate"; then
            QUARTUS_ROOTDIR="$root"
            QUARTUS_SH="$candidate"
            export QUARTUS_ROOTDIR
            return 0
        fi
    done
    fail "Quartus oracle unavailable; expected quartus_sh below QUARTUS_ROOTDIR/bin or QUARTUS_ROOTDIR/quartus/bin"
}

print_command() {
    printf 'command:'
    printf ' %q' "$@"
    printf '\n'
}

validate_output_tree
validate_quartus_root

require_regular "$rtl" "shared RTL"
require_regular "$qsf" "shared pin constraints"
require_regular "$sdc" "shared timing constraints"
require_regular "$oracle_qpf" "oracle project file"
require_regular "$oracle_qsf" "oracle settings file"
require_regular "$run_logged" "logged command runner"
require_regular "$collector" "manifest collector"

version_output="$("$QUARTUS_SH" --version 2>&1)" \
    || fail "Quartus oracle unavailable; could not execute $QUARTUS_SH --version"
if ! grep -Eq '(^|[^0-9])17\.0\.2([^0-9]|$)' <<< "$version_output"; then
    first_version_line="${version_output%%$'\n'*}"
    fail "Quartus oracle requires exact version 17.0.2; detected: ${first_version_line:-no version output}"
fi

compile_cmd=("$QUARTUS_SH" --flow compile top)
if (( PRINT_COMMANDS )); then
    printf 'experiment: %s\n' "$EXP"
    printf 'target: %s\n' "$TARGET"
    printf 'quartus_version: %s\n' "${version_output%%$'\n'*}"
    printf 'project: experiments/%s/oracle/top.qpf\n' "$EXP"
    printf 'shared_rtl: %s\n' "$rtl_rel"
    printf 'shared_qsf: %s\n' "$qsf_rel"
    printf 'shared_sdc: %s\n' "$sdc_rel"
    printf 'output: build/oracle/%s\n' "$EXP"
    print_command "$run_logged" "build/oracle/$EXP/quartus.log" "${compile_cmd[@]}"
    exit 0
fi

mkdir -p -- "$project_output_dir"

# Stage the project below the isolated output directory.  Relative paths in a
# checked-in QSF are useful to a human opening the project, but would point at
# the wrong tree once staged, so only the generated copy receives absolute
# paths to the already-validated shared inputs.
cp -- "$oracle_qpf" "$project_dir/top.qpf"
"$PYTHON" - "$oracle_qsf" "$project_dir/top.qsf" "$rtl" "$sdc" "$qsf" <<'PY'
from pathlib import Path
import json
import sys

source, destination, rtl, sdc, pins = map(Path, sys.argv[1:])
text = source.read_text(encoding="utf-8")
replacements = {
    '"../../../experiments/010_blinky/rtl/top.v"': json.dumps(str(rtl)),
    '"../../../boards/de10nano/clocks.sdc"': json.dumps(str(sdc)),
    '"../../../boards/de10nano/pins.qsf"': json.dumps(str(pins)),
}
for old, new in replacements.items():
    text = text.replace(old, new)
destination.write_text(text, encoding="utf-8")
PY

"$run_logged" "$version_log" "$QUARTUS_SH" --version
(
    cd -- "$project_dir"
    "$run_logged" "$quartus_log" "${compile_cmd[@]}"
)

validate_output_tree

[[ -d "$project_output_dir" && ! -L "$project_output_dir" ]] \
    || fail "Quartus compile did not produce an isolated output_files directory"

find_report() {
    local pattern=$1
    local candidate
    # Avoid ``find | head`` here: with pipefail, a large report set can make
    # find exit on SIGPIPE even though a valid first report was found.
    while IFS= read -r candidate; do
        printf '%s\n' "$candidate"
        return 0
    done < <(find "$project_output_dir" -maxdepth 1 -type f -name "$pattern" -print | sort)
    return 0
}

rbf_source="$(find_report '*.rbf')"
fit_source="$(find_report '*.fit.rpt')"
timing_source="$(find_report '*.sta.rpt')"
[[ -n "$rbf_source" ]] || fail "Quartus compile did not produce a required RBF artifact"
[[ -n "$fit_source" ]] || fail "Quartus compile did not produce a required fitter report"
[[ -n "$timing_source" ]] || fail "Quartus compile did not produce a required timing report"

require_regular "$rbf_source" "Quartus RBF"
require_regular "$fit_source" "Quartus fitter report"
require_regular "$timing_source" "Quartus timing report"
cp -- "$rbf_source" "$rbf"
cp -- "$fit_source" "$fit_report"
cp -- "$timing_source" "$timing_report"

# Normalize the two text reports into the same schema-2 build object consumed
# by compare_builds.py.  The parser is deliberately conservative: an absent
# count stays absent, while an observed zero remains an explicit zero.
{
    print_command "$PYTHON" - "$fit_report" "$timing_report" "$summary" "$rbf" "$EXP" "$TARGET"
    "$PYTHON" - "$fit_report" "$timing_report" "$summary" "$rbf" "$EXP" "$TARGET" <<'PY'
from __future__ import annotations

import hashlib
import json
import re
import sys
from pathlib import Path

fit_path, timing_path, summary_path, rbf_path, experiment, target = map(Path, sys.argv[1:])
experiment = str(experiment)
target = str(target)
fit_text = fit_path.read_text(encoding="utf-8", errors="replace")
timing_text = timing_path.read_text(encoding="utf-8", errors="replace")

number = r"([0-9][0-9,]*(?:\.[0-9]+)?)"


def count_on_line(line: str) -> tuple[int | None, int | None]:
    pairs = re.search(rf"{number}\s*/\s*{number}", line)
    if pairs:
        return int(float(pairs.group(1).replace(",", ""))), int(float(pairs.group(2).replace(",", "")))
    used = re.search(rf"(?:used|utilized|usage)\D{{0,24}}{number}", line, re.I)
    available = re.search(rf"(?:available|total|capacity)\D{{0,24}}{number}", line, re.I)
    return (
        int(float(used.group(1).replace(",", ""))) if used else None,
        int(float(available.group(1).replace(",", ""))) if available else None,
    )


def records_for(labels: tuple[str, ...]) -> dict[str, dict[str, object]]:
    records: dict[str, dict[str, object]] = {}
    for line in fit_text.splitlines():
        lowered = line.lower()
        if not any(label.lower() in lowered for label in labels):
            continue
        used, available = count_on_line(line)
        if used is None and available is None:
            continue
        key = labels[0]
        record: dict[str, object] = {"used": used, "available": available}
        if isinstance(used, int) and isinstance(available, int) and available:
            record["utilization_percent"] = round(used * 100.0 / available, 6)
        else:
            record["utilization_percent"] = None
        records[key] = record
        break
    return records


resources: dict[str, dict[str, object]] = {}
for labels in (("ALM",), ("register",), ("IO",)):
    resources.update(records_for(labels))

hard_blocks: dict[str, dict[str, object]] = {}
for labels in (("M10K", "block memory"), ("DSP",), ("PLL",), ("MLAB",), ("BRAM",)):
    hard_blocks.update(records_for(labels))

fmax_values: list[float] = []
for line in timing_text.splitlines():
    lowered = line.lower()
    if "mhz" not in lowered:
        continue
    if not any(marker in lowered for marker in ("fmax", "frequency", "clock", "period", "slack")):
        continue
    for match in re.finditer(rf"{number}\s*mhz", line, re.I):
        fmax_values.append(float(match.group(1).replace(",", "")))
achieved = max(fmax_values) if fmax_values else None
timing_status = achieved is not None and achieved >= 50.0

rbf_bytes = rbf_path.read_bytes()
summary = {
    "status": "pass" if timing_status else "fail",
    "build_status": "pass" if timing_status else "fail",
    "route": {"status": "pass", "unrouted": False},
    "route_status": "pass",
    "timing": {
        "status": "pass" if timing_status else "fail",
        "clock": "FPGA_CLK1_50",
        "requested_mhz": 50.0,
        "achieved_mhz": achieved,
    },
    "resources": resources,
    "hard_blocks": hard_blocks,
    "resource_classes": {name: "ordinary" for name in resources},
    "unknown_resources": {},
    "hard_block_status": "pass",
    "hard_block_reason": "Quartus fitter report normalized; no OSS hard-block policy applied",
    "authenticated_tools": {},
    "reproducibility": {
        "rbf_size_bytes": len(rbf_bytes),
        "rbf_sha256": hashlib.sha256(rbf_bytes).hexdigest(),
        "previous_rbf_sha256": None,
        "rbf_stability_measured": False,
        "rbf_stable": None,
        "rbf_stability_reason": "oracle rebuild stability is informational",
    },
    "source_hashes": {},
    "tool_pins": {},
    "target": target,
    "lane": "oracle",
    "experiment": experiment,
    "simulation": {"status": "not-run"},
}
summary_path.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n", encoding="utf-8")
timing_path.with_name("timing.txt").write_text(
    "target: " + target + "\n"
    + "constraint: 50 MHz\n"
    + "clock: FPGA_CLK1_50\n"
    + "achieved: " + ("unknown" if achieved is None else f"{achieved:.6f}") + " MHz\n"
    + "status: " + ("pass" if timing_status else "fail") + "\n",
    encoding="utf-8",
)
PY
} > "$summary_log" 2>&1 || fail "Quartus report normalization failed; see $summary_log"

summary_status="$("$PYTHON" -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8")).get("status", "fail"))' "$summary")"
[[ "$summary_status" == pass ]] \
    || fail "Quartus timing report does not satisfy the 50 MHz requirement; see $summary_log"

manifest_cmd=(
    "$PYTHON" "$collector"
    --output-dir "$oracle_dir"
    --experiment "$EXP"
    --lane oracle
    --target "$TARGET"
    --repo-root "$ROOT"
    --build-root "$ROOT/build"
    --source "$rtl"
    --source "$qsf"
    --source "$sdc"
    --source "$oracle_qpf"
    --source "$oracle_qsf"
    --source "$ROOT/scripts/build_oracle.sh"
    --source "$run_logged"
    --source "$collector"
    --source "$ROOT/scripts/compare_builds.py"
    --source "$ROOT/toolchain.lock"
    --command-log "$version_log"
    --command-log "$quartus_log"
    --command-log "$summary_log"
    --artifact "$rbf"
    --artifact "$fit_report"
    --artifact "$timing_report"
    --artifact "$timing_summary"
    --artifact "$summary"
    --build-summary "$summary"
    --manifest "$manifest"
)

"$run_logged" "$manifest_log" "${manifest_cmd[@]}"
[[ -s "$manifest" ]] || fail "oracle manifest was not produced: $manifest"

printf 'Quartus oracle build complete: %s\n' "$oracle_dir"
