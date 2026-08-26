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
quartus_version_line="$("$PYTHON" -c '
import re
import sys

matches = [
    line
    for line in sys.argv[1].splitlines()
    if re.search(r"(?<![0-9])17[.]0[.]2(?![0-9])", line)
]
if len(matches) != 1:
    raise SystemExit(1)
print(matches[0])
' "$version_output")" \
    || fail "Quartus oracle requires exactly one line containing exact version 17.0.2"
quartus_version_sha256="$("$PYTHON" -c 'import hashlib,sys; print(hashlib.sha256(sys.argv[1].encode()).hexdigest())' "$version_output")"

compile_cmd=("$QUARTUS_SH" --flow compile top)
if (( PRINT_COMMANDS )); then
    printf 'experiment: %s\n' "$EXP"
    printf 'target: %s\n' "$TARGET"
    printf 'quartus_version: %s\n' "$quartus_version_line"
    printf 'project: experiments/%s/oracle/top.qpf\n' "$EXP"
    printf 'shared_rtl: %s\n' "$rtl_rel"
    printf 'shared_qsf: %s\n' "$qsf_rel"
    printf 'shared_sdc: %s\n' "$sdc_rel"
    printf 'output: build/oracle/%s\n' "$EXP"
    print_command "$run_logged" "build/oracle/$EXP/quartus.log" "${compile_cmd[@]}"
    exit 0
fi

clean_project_output() {
    [[ "$project_output_dir" == "$oracle_dir/project/output_files" ]] \
        || fail "unsafe staged Quartus output path: $project_output_dir"
    if [[ -e "$project_output_dir" || -L "$project_output_dir" ]]; then
        [[ -d "$project_output_dir" && ! -L "$project_output_dir" ]] \
            || fail "staged Quartus output is not a regular directory: $project_output_dir"
        path_has_symlink_component "$project_output_dir" \
            && fail "staged Quartus output path contains a symlink component: $project_output_dir"
        local nested_symlink output_real expected_real
        nested_symlink="$(find -P "$project_output_dir" -type l -print -quit)"
        [[ -z "$nested_symlink" ]] \
            || fail "staged Quartus output contains a symlink: $nested_symlink"
        output_real="$(CDPATH= cd -- "$project_output_dir" && pwd -P)"
        expected_real="$oracle_dir/project/output_files"
        [[ "$output_real" == "$expected_real" ]] \
            || fail "staged Quartus output resolves outside the oracle project: $project_output_dir"
        # This is the only cleanup in the wrapper.  The path and every nested
        # entry were validated above, so stale reports cannot be attested and
        # no symlink can redirect removal outside build/oracle.
        rm -rf -- "$project_output_dir"
    fi
    mkdir -p -- "$project_output_dir"
    printf 'cleaned staged Quartus output: %s\n' "$project_output_dir"
}

clean_project_output

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

rbf_source="$project_output_dir/top.rbf"
fit_source="$project_output_dir/top.fit.rpt"
timing_source="$project_output_dir/top.sta.rpt"
require_regular "$rbf_source" "Quartus RBF"
require_regular "$fit_source" "Quartus fitter report"
require_regular "$timing_source" "Quartus timing report"
[[ -s "$rbf_source" ]] || fail "Quartus compile produced an empty required RBF artifact"
[[ -s "$fit_source" ]] || fail "Quartus compile produced an empty required fitter report"
[[ -s "$timing_source" ]] || fail "Quartus compile produced an empty required timing report"
cp -- "$rbf_source" "$rbf"
cp -- "$fit_source" "$fit_report"
cp -- "$timing_source" "$timing_report"

# Normalize the two text reports into the same schema-2 build object consumed
# by compare_builds.py.  The parser is deliberately conservative: an absent
# count stays absent, while an observed zero remains an explicit zero.
{
    print_command "$PYTHON" - "$fit_report" "$timing_report" "$summary" "$rbf" "$EXP" "$TARGET" "$rtl" "$sdc" "$qsf" "$oracle_qsf" "$QUARTUS_SH" "$quartus_version_line" "$quartus_version_sha256"
    "$PYTHON" - "$fit_report" "$timing_report" "$summary" "$rbf" "$EXP" "$TARGET" "$rtl" "$sdc" "$qsf" "$oracle_qsf" "$QUARTUS_SH" "$quartus_version_line" "$quartus_version_sha256" <<'PY'
from __future__ import annotations

import hashlib
import json
import re
import sys
from pathlib import Path

fit_path = Path(sys.argv[1])
timing_path = Path(sys.argv[2])
summary_path = Path(sys.argv[3])
rbf_path = Path(sys.argv[4])
experiment = sys.argv[5]
target = sys.argv[6]
rtl_path = Path(sys.argv[7])
sdc_path = Path(sys.argv[8])
pins_path = Path(sys.argv[9])
oracle_qsf_path = Path(sys.argv[10])
quartus_path = Path(sys.argv[11])
quartus_version = sys.argv[12]
quartus_version_output_sha256 = sys.argv[13]
fit_text = fit_path.read_text(encoding="utf-8", errors="replace")
timing_text = timing_path.read_text(encoding="utf-8", errors="replace")

number = r"[0-9][0-9,]*(?:\.[0-9]+)?"
number_token = rf"(?<![A-Za-z0-9]){number}(?![A-Za-z0-9])"


def table_cells(line: str) -> tuple[str, list[str]] | None:
    for delimiter in ("|", ";"):
        if delimiter in line:
            return delimiter, [cell.strip() for cell in line.split(delimiter)]
    return None


def table_row(line: str) -> tuple[str, str, list[str]] | None:
    parsed = table_cells(line)
    if parsed is None:
        return None
    delimiter, cells = parsed
    first_label = next((index for index, cell in enumerate(cells) if cell), None)
    if first_label is None:
        return None
    label = re.sub(r"\s+", " ", cells[first_label]).strip()
    label = re.sub(r"^(?:--\s*)+", "", label).strip()
    return delimiter, label, cells[first_label + 1 :]


def count_on_line(line: str) -> tuple[int | None, int | None]:
    row = table_row(line)
    if row is not None:
        _delimiter, _label, value_cells = row
        value_text = " | ".join(value_cells)
        pairs = re.search(rf"({number})\s*/\s*({number})", value_text)
        if pairs:
            return int(float(pairs.group(1).replace(",", ""))), int(float(pairs.group(2).replace(",", "")))
        used = re.search(rf"(?:used|utilized|usage)\D{{0,24}}({number})", value_text, re.I)
        available = re.search(rf"(?:available|total|capacity)\D{{0,24}}({number})", value_text, re.I)
        if used and available:
            return int(float(used.group(1).replace(",", ""))), int(float(available.group(1).replace(",", "")))
        values: list[str] = []
        for cell in value_cells:
            values.extend(re.findall(number_token, cell))
        if len(values) >= 2:
            return int(float(values[0].replace(",", ""))), int(float(values[1].replace(",", "")))
        return (
            int(float(used.group(1).replace(",", ""))) if used else None,
            int(float(available.group(1).replace(",", ""))) if available else None,
        )

    pairs = re.search(rf"({number})\s*/\s*({number})", line)
    if pairs:
        return int(float(pairs.group(1).replace(",", ""))), int(float(pairs.group(2).replace(",", "")))
    used = re.search(rf"(?:used|utilized|usage)\D{{0,24}}({number})", line, re.I)
    available = re.search(rf"(?:available|total|capacity)\D{{0,24}}({number})", line, re.I)
    if used and available:
        return int(float(used.group(1).replace(",", ""))), int(float(available.group(1).replace(",", "")))
    values = re.findall(number_token, line)
    if len(values) >= 2:
        return int(float(values[0].replace(",", ""))), int(float(values[1].replace(",", "")))
    return (
        int(float(used.group(1).replace(",", ""))) if used else None,
        int(float(available.group(1).replace(",", ""))) if available else None,
    )


resource_patterns = {
    "ALM": re.compile(r"\b(?:total\s+)?(?:logic\s+)?alms?\b", re.I),
    "register": re.compile(r"\b(?:total\s+)?(?:dedicated\s+logic\s+)?registers?\b", re.I),
    # Quartus uses both ``Total pins`` and ``I/O pins``.  Do not search for
    # the substring ``IO``: it occurs in version/build prose and would turn
    # those numbers into a fake resource record.
    "IO": re.compile(r"\b(?:total\s+(?:user\s+)?(?:i\s*/?\s*o\s+)?pins?|i\s*/?\s*o\s+pins?)\b", re.I),
}


def records_for(name: str, pattern: re.Pattern[str]) -> dict[str, dict[str, object]]:
    records: dict[str, dict[str, object]] = {}
    for line in fit_text.splitlines():
        if pattern.search(line) is None:
            continue
        used, available = count_on_line(line)
        if used is None and available is None:
            continue
        record: dict[str, object] = {"used": used, "available": available}
        if isinstance(used, int) and isinstance(available, int) and available:
            record["utilization_percent"] = round(used * 100.0 / available, 6)
        else:
            record["utilization_percent"] = None
        records[name] = record
        break
    return records


resources: dict[str, dict[str, object]] = {}
for name, pattern in resource_patterns.items():
    resources.update(records_for(name, pattern))

def summary_rows(text: str) -> list[tuple[str, str, str, list[str]]]:
    """Return rows from the two fitted-resource summary sections.

    Quartus exports many capability, pin, entity, and diagnostic tables in the
    same report.  Their labels are intentionally not evidence.  Section
    markers are required before any row can be treated as fitted evidence.
    Capability, pin, entity, and diagnostic rows without those markers are
    intentionally ignored by this parser.
    """

    summary_names = {"fitter summary", "fitter resource usage summary"}
    end_names = {
        "fitter settings",
        "parallel compilation",
        "fitter netlist optimizations",
        "fitter partition statistics",
        "fitter resource utilization by entity",
    }
    active = False
    rows: list[tuple[str, str, str, list[str]]] = []
    for line in text.splitlines():
        row = table_row(line)
        if row is None:
            continue
        delimiter, label, value_cells = row
        normalized = re.sub(r"\s+", " ", label).strip().casefold()
        if normalized in summary_names:
            active = True
            continue
        if normalized in end_names:
            active = False
            continue
        if active:
            rows.append((line, delimiter, label, value_cells))
    return rows


all_fit_rows = [
    (line, row[0], row[1], row[2])
    for line in fit_text.splitlines()
    if (row := table_row(line)) is not None
]
summary_section_names = {"fitter summary", "fitter resource usage summary"}
summary_section_present = any(
    re.sub(r"\s+", " ", label).strip().casefold() in summary_section_names
    for _line, _delimiter, label, _value_cells in all_fit_rows
)
fitted_rows = summary_rows(fit_text) if summary_section_present else []


hard_row_patterns = {
    "PLL": (
        re.compile(r"^(?:total|fractional)\s+plls?$", re.I),
    ),
    "BRAM/M10K": (
        re.compile(r"^total\s+ram\s+blocks?$", re.I),
        re.compile(r"^(?:total\s+)?m(?:10|20)k\s+blocks?$", re.I),
    ),
    # A multiplier row can be useful context in a Fitter report, but the
    # acceptance gate is deliberately tied to the physical DSP Blocks row.
    "DSP": (
        re.compile(r"^total\s+dsp\s+blocks?$", re.I),
    ),
}


def hard_record(
    name: str,
    patterns: tuple[re.Pattern[str], ...],
) -> tuple[dict[str, object] | None, str | None]:
    candidates: list[tuple[int, int]] = []
    malformed = False
    for line, _delimiter, label, _value_cells in fitted_rows:
        if not any(pattern.fullmatch(label) for pattern in patterns):
            continue
        used, available = count_on_line(line)
        if used is None or available is None:
            malformed = True
            continue
        candidates.append((used, available))
    if malformed:
        return None, f"{name}: fitter evidence is unrecognized"
    unique = sorted(set(candidates))
    if not unique:
        return None, f"{name}: fitter evidence is missing"
    if len(unique) != 1:
        return None, f"{name}: fitter evidence is ambiguous"
    used, available = unique[0]
    record: dict[str, object] = {"used": used, "available": available}
    record["utilization_percent"] = round(used * 100.0 / available, 6) if available else None
    if used != 0:
        return record, f"{name}: unexpected hard resource usage ({used})"
    return record, None


def sha256_file(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


hard_blocks: dict[str, dict[str, object]] = {}
hard_errors: list[str] = []
for name, patterns in hard_row_patterns.items():
    record, error = hard_record(name, patterns)
    if record is not None:
        record["evidence_kind"] = "fitter_summary"
        record["measured"] = True
        hard_blocks[name] = record
    if error is not None:
        hard_errors.append(error)


def static_exclusion(
    name: str,
    report_pattern: re.Pattern[str],
    source_patterns: tuple[str, ...],
) -> tuple[dict[str, object], str | None]:
    def excluded_record(source_records: list[dict[str, str]]) -> dict[str, object]:
        return {
            # No aggregate fitted class count exists for these classes in the
            # normal Cyclone V summary.  Keep that fact distinct from a
            # measured zero even when capability rows are present.
            "used": None,
            "available": None,
            "status": "excluded",
            "evidence_kind": "static_exclusion",
            "measured": False,
            "exclusion": {
                "basis": "static source/project exclusion",
                "patterns": list(source_patterns),
                "sources": source_records,
            },
        }

    # A normal Cyclone V Fitter Resource Summary can contain MLAB memory-bit
    # and HPS peripheral-capability rows, but it does not provide an aggregate
    # measured class count.  These classes therefore use a separate,
    # explicitly labelled source/project exclusion contract.  If a report
    # does contain an aggregate physical-count row, do not silently
    # reinterpret it as a static zero: the evidence is contradictory and
    # fails closed.
    for line, _delimiter, label, _value_cells in fitted_rows:
        if report_pattern.fullmatch(label) is None:
            continue
        used, available = count_on_line(line)
        if used is not None or available is not None:
            return excluded_record([]), f"{name}: fitter report contains a measured row despite static exclusion"
        return excluded_record([]), f"{name}: fitter report contains an unrecognized static-class row"

    source_inputs = (
        (f"experiments/{experiment}/rtl/top.v", rtl_path),
        ("boards/de10nano/pins.qsf", pins_path),
        ("boards/de10nano/clocks.sdc", sdc_path),
        (f"experiments/{experiment}/oracle/top.qsf", oracle_qsf_path),
    )
    source_records: list[dict[str, str]] = []
    for relative, path in source_inputs:
        text = path.read_text(encoding="utf-8", errors="replace")
        for pattern in source_patterns:
            if re.search(pattern, text, re.I | re.M):
                return excluded_record(source_records), f"{name}: source/project exclusion matched {pattern!r} in {relative}"
        source_records.append({"path": relative, "sha256": sha256_file(path)})
    return excluded_record(source_records), None


static_hard_contracts = {
    "MLAB/LUTRAM": (
        re.compile(r"^(?:total\s+)?mlabs?$|^(?:total\s+)?mlab/lutram\s+blocks?$", re.I),
        (
            r"\bmlab(?:s)?\b",
            r"\blutram\b",
            r"\b(?:altsyncram|lpm_ram|mlab_cell)\b",
            r"\b(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
        ),
    ),
    "HPS": (
        re.compile(r"^(?:total\s+)?(?:hps|hard\s+processor\s+system)\s+blocks?$", re.I),
        (
            r"\bhps\b",
            r"\bhard[_ ]processor",
            r"\b(?:altera|cyclonev)[_ ]hps\b",
            r"\b(?:hps_component|soc_system|soc_id|arm)\b",
            r"\bsoc\b",
        ),
    ),
}
for name, (report_pattern, source_patterns) in static_hard_contracts.items():
    record, error = static_exclusion(name, report_pattern, source_patterns)
    hard_blocks[name] = record
    if error is not None:
        hard_errors.append(error)

# Catch a resource section whose spelling is not covered by the conservative
# aliases above.  This is deliberately an error rather than silently calling
# an unrecognized hard block zero.
unknown_resources: dict[str, dict[str, object]] = {}
all_known_hard_patterns = tuple(
    pattern for patterns in hard_row_patterns.values() for pattern in patterns
)
all_static_hard_patterns = tuple(
    value[0] for value in static_hard_contracts.values()
)
# ``9x9 multipliers`` is a normal companion row in some Quartus reports.  It
# is not itself the direct DSP gate, but it is recognized context when the
# required DSP Blocks row is also present.
known_context_patterns = (
    re.compile(r"^(?:total\s+)?(?:[0-9]+x[0-9]+\s+)?multipliers?$", re.I),
)
unknown_markers = (
    "ram block",
    "embedded memory",
    "hard block",
    "processor",
    "multiplier",
    "pll",
    "dsp",
    "m10k",
    "m20k",
    "bram",
    "mlab",
    "lutram",
)
ignored_prose = ("capability", "peripheral", "entity", "pin", "compilation", "diagnostic")
for line, _delimiter, label, _value_cells in fitted_rows:
    lowered = label.casefold()
    if not any(marker in lowered for marker in unknown_markers):
        continue
    # MLAB memory bits and block-memory bits are capacity/bit totals, not
    # aggregate physical MLAB/LUTRAM fitted counts.
    if re.search(r"(?:memory|block)\s+bits?\b", lowered):
        continue
    if any(pattern.fullmatch(label) for pattern in (*all_known_hard_patterns, *all_static_hard_patterns, *known_context_patterns)):
        continue
    if any(word in lowered for word in ignored_prose):
        continue
    # CPU scheduling diagnostics contain ``processor`` but are not fitted
    # physical resources.  A physical HPS aggregate is handled by the static
    # exclusion contract above.
    if "processor" in lowered and not re.search(r"\bhps\b|hard\s+processor\s+system", lowered):
        continue
    if not re.search(r"\b(?:total|blocks?|ram|m10k|m20k|bram|dsp|plls?|resources?|units?|count|usage)\b", lowered):
        continue
    used, available = count_on_line(line)
    if used is None and available is None:
        # A malformed aggregate row is still evidence that must not be
        # silently ignored, while one-field capability/prose rows were
        # filtered above.
        key = "unrecognized:" + line.strip()[:80]
        unknown_resources[key] = {"evidence": line.strip()}
        continue
    key = "unrecognized:" + line.strip()[:80]
    unknown_resources[key] = {"evidence": line.strip()}
if unknown_resources:
    hard_errors.append("unrecognized hard-resource evidence: " + ", ".join(sorted(unknown_resources)))

hard_block_status = "pass" if not hard_errors else "fail"
hard_block_reason = "; ".join(hard_errors) if hard_errors else "fitter summary rows measure RAM Blocks/M10K, DSP Blocks, and PLLs; MLAB/LUTRAM and HPS are excluded by static source/project evidence (used=null)"

clock_name = "FPGA_CLK1_50"


def fmax_values(cell: str) -> list[float]:
    return [float(value.replace(",", "")) for value in re.findall(rf"({number})\s*mhz", cell, re.I)]


timing_lines = timing_text.splitlines()
headers: list[tuple[int, str, int, int]] = []
malformed_headers = 0
for line_number, line in enumerate(timing_lines):
    parsed = table_cells(line)
    if parsed is None:
        continue
    delimiter, cells = parsed
    clock_indexes = [index for index, cell in enumerate(cells) if re.search(r"\bclock\s+name\b", cell, re.I)]
    restricted_indexes = [index for index, cell in enumerate(cells) if re.search(r"\brestricted\s+fmax\b", cell, re.I)]
    if clock_indexes or restricted_indexes:
        if len(clock_indexes) != 1 or len(restricted_indexes) != 1:
            malformed_headers += 1
            continue
        headers.append((line_number, delimiter, clock_indexes[0], restricted_indexes[0]))

restricted_candidates: list[float] = []
invalid_timing_tables = 0
for header_number, delimiter, clock_index, restricted_index in headers:
    target_rows: list[list[str]] = []
    line_number = header_number + 1
    while line_number < len(timing_lines):
        parsed = table_cells(timing_lines[line_number])
        if parsed is None:
            if re.fullmatch(r"[+\-= ]+", timing_lines[line_number].strip()):
                line_number += 1
                continue
            break
        if parsed[0] != delimiter:
            break
        cells = parsed[1]
        if any(re.search(r"\bclock\s+name\b", cell, re.I) for cell in cells):
            break
        if max(clock_index, restricted_index) < len(cells):
            if re.fullmatch(rf"{re.escape(clock_name)}", cells[clock_index], re.I):
                target_rows.append(cells)
        line_number += 1
    if len(target_rows) != 1:
        invalid_timing_tables += 1
        continue
    cells = target_rows[0]
    values = fmax_values(cells[restricted_index])
    if len(values) != 1:
        invalid_timing_tables += 1
        continue
    restricted_candidates.append(values[0])

if headers and malformed_headers == 0 and invalid_timing_tables == 0 and restricted_candidates:
    achieved = min(restricted_candidates)
else:
    achieved = None
timing_status = achieved is not None and achieved >= 50.0


common_source_hashes = {
    f"experiments/{experiment}/rtl/top.v": sha256_file(rtl_path),
    "boards/de10nano/clocks.sdc": sha256_file(sdc_path),
    "boards/de10nano/pins.qsf": sha256_file(pins_path),
}

rbf_bytes = rbf_path.read_bytes()
provenance = {
    "path": str(quartus_path),
    "executable": str(quartus_path),
    "sha256": sha256_file(quartus_path),
    "executable_sha256": sha256_file(quartus_path),
    "version": quartus_version,
    "required_version": "17.0.2",
    "version_output_sha256": quartus_version_output_sha256,
}

summary = {
    "status": "pass" if timing_status and hard_block_status == "pass" else "fail",
    "build_status": "pass" if timing_status and hard_block_status == "pass" else "fail",
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
    "hard_block_evidence": hard_blocks,
    "resource_classes": {name: "ordinary" for name in resources},
    "unknown_resources": unknown_resources,
    "hard_block_status": hard_block_status,
    "hard_block_reason": hard_block_reason,
    "authenticated_tools": {"quartus_sh": provenance},
    "reproducibility": {
        "rbf_size_bytes": len(rbf_bytes),
        "rbf_sha256": hashlib.sha256(rbf_bytes).hexdigest(),
        "previous_rbf_sha256": None,
        "rbf_stability_measured": False,
        "rbf_stable": None,
        "rbf_stability_reason": "oracle rebuild stability is unmeasured by contract",
    },
    "source_hashes": common_source_hashes,
    "tool_pins": {"quartus": provenance},
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
    || fail "Quartus normalized reports do not satisfy timing or hard-resource evidence requirements; see $summary_log"

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
