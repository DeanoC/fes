#!/usr/bin/env bash

# Build the optional Quartus experiment lane.  This file and rebuild_core.py
# are the wrappers that know about the proprietary compiler.  In particular,
# they never search PATH: callers must opt in with QUARTUS_ROOTDIR.
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)"
PYTHON="${PYTHON:-python3}"
EXP="${EXP:-010_blinky}"
TARGET="5CSEBA6U23I7"
PRINT_COMMANDS=0
EXPERIMENT_OPTION_SET=0

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
            (( EXPERIMENT_OPTION_SET == 0 )) || fail "--experiment may be specified only once"
            EXP=$2
            EXPERIMENT_OPTION_SET=1
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
            fail "unexpected positional selector: $1"
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
policy_tool="$ROOT/scripts/experiment_policy.py"

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

require_regular "$policy_tool" "closed experiment policy"

# Consume the same closed policy as the OSS lane before invoking Quartus.
# Besides rejecting unknown experiment selectors, --check-sources rejects
# external includes and forbidden tokens and checks the required HPS GP marker.
policy_output="$(
    "$PYTHON" "$policy_tool" \
        --experiment "$EXP" \
        --format shell \
        --check-sources \
        --repo-root "$ROOT"
)" || fail "cannot load closed experiment policy: $EXP"

policy_name=
policy_source=
policy_top=
policy_clock=
policy_clock_mhz=
policy_qsf=
policy_sdc=
policy_artifact=
while IFS='=' read -r policy_key policy_value; do
    case "$policy_key" in
        name) policy_name=$policy_value ;;
        source) policy_source=$policy_value ;;
        sources) : ;; # OSS lane consumes the full production list.
        top) policy_top=$policy_value ;;
        clock) policy_clock=$policy_value ;;
        clock_mhz) policy_clock_mhz=$policy_value ;;
        qsf) policy_qsf=$policy_value ;;
        sdc) policy_sdc=$policy_value ;;
        artifact) policy_artifact=$policy_value ;;
        nobram|nolutram|nodsp|yosys_post_synth) : ;; # OSS synthesis knobs.
        allowed_hard_blocks) : ;;
        "") : ;;
        *) fail "closed experiment policy emitted an unknown field: $policy_key" ;;
    esac
done <<< "$policy_output"

[[ "$policy_name" == "$EXP" ]] || fail "closed experiment policy name mismatch"
[[ "$policy_source" == "$rtl_rel" ]] || fail "closed experiment policy source mismatch"
[[ "$policy_top" == "top" ]] || fail "closed experiment policy top mismatch"
[[ "$policy_clock" == "FPGA_CLK1_50" && "$policy_clock_mhz" == "50" ]] \
    || fail "closed experiment policy clock mismatch"
[[ "$policy_qsf" == "$qsf_rel" && "$policy_sdc" == "$sdc_rel" ]] \
    || fail "closed experiment policy constraint mismatch"
[[ "$policy_artifact" == "top.rbf" ]] || fail "closed experiment policy artifact mismatch"

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
import re
import sys

source, destination, rtl, sdc, pins = map(Path, sys.argv[1:])
text = source.read_text(encoding="utf-8")
for assignment, value in (("VERILOG_FILE", rtl), ("SDC_FILE", sdc)):
    pattern = rf"(?m)^(\s*set_global_assignment\s+-name\s+{assignment}\s+)[^\s#]+"
    text, replacements = re.subn(
        pattern,
        lambda match: match.group(1) + json.dumps(str(value)),
        text,
    )
    if replacements != 1:
        raise SystemExit(f"oracle QSF must contain exactly one {assignment} assignment")
# Keep compatibility with the original blinky template if it declares the
# shared board pins file.  Mailbox deliberately has no output-pin assignment.
text = text.replace('"../../../boards/de10nano/pins.qsf"', json.dumps(str(pins)))
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
    print_command "$PYTHON" - "$fit_report" "$timing_report" "$summary" "$rbf" "$EXP" "$TARGET" "$rtl" "$sdc" "$qsf" "$oracle_qpf" "$oracle_qsf" "$QUARTUS_SH" "$quartus_version_line" "$quartus_version_sha256" "$version_log" "$quartus_log"
    "$PYTHON" - "$fit_report" "$timing_report" "$summary" "$rbf" "$EXP" "$TARGET" "$rtl" "$sdc" "$qsf" "$oracle_qpf" "$oracle_qsf" "$QUARTUS_SH" "$quartus_version_line" "$quartus_version_sha256" "$version_log" "$quartus_log" <<'PY'
from __future__ import annotations

import hashlib
import json
import re
import sys
from pathlib import Path
from typing import Mapping

fit_path = Path(sys.argv[1])
timing_path = Path(sys.argv[2])
summary_path = Path(sys.argv[3])
rbf_path = Path(sys.argv[4])
experiment = sys.argv[5]
hps_gp = experiment in {
    "020_linux_mailbox",
    "040_mlab_ram",
    "050_lut_mul",
    "060_dsp_mul",
    "070_mixed_mem",
    "080_dsp_mem",
    "100_dsp_rom",
}
measure_mlab = experiment in {"040_mlab_ram", "070_mixed_mem", "080_dsp_mem"}
measure_dsp = experiment in {"060_dsp_mul", "080_dsp_mem", "100_dsp_rom"}
measure_m10k = experiment in {"070_mixed_mem", "080_dsp_mem", "100_dsp_rom"}
target = sys.argv[6]
rtl_path = Path(sys.argv[7])
sdc_path = Path(sys.argv[8])
pins_path = Path(sys.argv[9])
oracle_qpf_path = Path(sys.argv[10])
oracle_qsf_path = Path(sys.argv[11])
quartus_path = Path(sys.argv[12])
quartus_version = sys.argv[13]
quartus_version_output_sha256 = sys.argv[14]
version_log_path = Path(sys.argv[15])
quartus_log_path = Path(sys.argv[16])
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
        if len(values) == 1:
            return int(float(values[0].replace(",", ""))), None
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
    if len(values) == 1:
        return int(float(values[0].replace(",", ""))), None
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
    "block_memory_bits": re.compile(r"\b(?:total\s+)?block\s+memory\s+bits?\b", re.I),
    "lutram_bits": re.compile(r"\b(?:total\s+)?(?:mlab|lutram)\s+memory\s+bits?\b", re.I),
    "sdram_interfaces": re.compile(r"\b(?:total\s+)?sdram\s+(?:interfaces?|ports?)\b", re.I),
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
    current_name: str | None = None
    sections: dict[str, list[tuple[str, str, str, list[str]]]] = {}
    for line in text.splitlines():
        row = table_row(line)
        if row is None:
            continue
        delimiter, label, value_cells = row
        normalized = re.sub(r"\s+", " ", label).strip().casefold()
        if normalized in summary_names:
            active = True
            current_name = normalized
            sections.setdefault(normalized, [])
            continue
        if normalized in end_names:
            active = False
            current_name = None
            continue
        if active and current_name is not None:
            sections[current_name].append((line, delimiter, label, value_cells))
    # The resource-usage table is the canonical physical fitted-resource
    # section.  Prefer it when present so the same physical quantity is not
    # counted once in Fitter Summary and again in the detailed table.  Tiny
    # fixtures and older Quartus exports may only contain Fitter Summary, so
    # retain that as a strict fallback.
    return sections.get("fitter resource usage summary") or sections.get("fitter summary", [])


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
    if not candidates:
        return None, f"{name}: fitter evidence is missing"
    if len(candidates) != 1:
        return None, f"{name}: fitter evidence is ambiguous"
    used, available = candidates[0]
    record: dict[str, object] = {"used": used, "available": available}
    record["utilization_percent"] = round(used * 100.0 / available, 6) if available else None
    expected = 0
    if measure_dsp and name == "DSP":
        expected = 1
    if measure_m10k and name == "BRAM/M10K":
        expected = 1
    if used != expected:
        return record, f"{name}: unexpected hard resource usage ({used})"
    return record, None


def measured_report_record(
    name: str,
    patterns: tuple[re.Pattern[str], ...],
    *,
    require_zero: bool = True,
    allow_missing_available: bool = False,
) -> tuple[dict[str, object] | None, str | None]:
    """Read exactly one canonical resource row from the selected report table."""

    candidates: list[tuple[int, int]] = []
    malformed = False
    for line, _delimiter, label, _value_cells in fitted_rows:
        if not any(pattern.fullmatch(label) for pattern in patterns):
            continue
        used, available = count_on_line(line)
        if used is None or (available is None and not allow_missing_available):
            malformed = True
            continue
        candidates.append((used, available))
    if malformed:
        return None, f"{name}: fitter evidence is unrecognized"
    if not candidates:
        return None, f"{name}: fitter evidence is missing"
    if len(candidates) != 1:
        return None, f"{name}: fitter evidence is ambiguous"
    used, available = candidates[0]
    record: dict[str, object] = {
        "used": used,
        "available": available,
        "utilization_percent": round(used * 100.0 / available, 6) if available else None,
        "evidence_kind": "fitter_summary",
        "measured": True,
    }
    if require_zero and used != 0:
        return record, f"{name}: unexpected forbidden resource usage ({used})"
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


# Unlike PLL/RAM/DSP, the mailbox intentionally consumes one exact HPS
# general-purpose primitive.  Quartus releases spell this aggregate either as
# the primitive identifier or as a human-readable ``HPS ... general purpose``
# row, so accept only those closed aliases and retain the fitter measurement.
mailbox_hps_patterns = (
    re.compile(
        r"^(?:total[ _-]+)?cyclonev[ _-]+hps[ _-]+interface[ _-]+"
        r"mpu[ _-]+general[ _-]+purpose(?:[ _-]+interfaces?)?$",
        re.I,
    ),
    re.compile(
        r"^(?:total[ _-]+)?hps(?:[ _-]+interface)?(?:[ _-]+mpu)?"
        r"[ _-]+general[ _-]+purpose(?:[ _-]+(?:interfaces?|i/o))?$",
        re.I,
    ),
    re.compile(r"^mpu[ _-]+general[ _-]+purpose$", re.I),
)
if hps_gp:
    hps_candidates: list[tuple[int, int]] = []
    hps_malformed = False
    for line, _delimiter, label, _value_cells in fitted_rows:
        if not any(pattern.fullmatch(label) for pattern in mailbox_hps_patterns):
            continue
        used, available = count_on_line(line)
        if used is None or available is None:
            hps_malformed = True
            continue
        hps_candidates.append((used, available))
    if hps_malformed:
        hard_errors.append(
            "cyclonev_hps_interface_mpu_general_purpose: fitter evidence is unrecognized"
        )
    if len(hps_candidates) != 1:
        hard_errors.append(
            "cyclonev_hps_interface_mpu_general_purpose: fitter evidence is missing or ambiguous"
        )
    else:
        used, available = hps_candidates[0]
        hps_record: dict[str, object] = {
            "used": used,
            "available": available,
            "utilization_percent": round(used * 100.0 / available, 6) if available else None,
            "evidence_kind": "fitter_summary",
            "measured": True,
        }
        hard_blocks["cyclonev_hps_interface_mpu_general_purpose"] = hps_record
        if used != 1:
            hard_errors.append(
                "cyclonev_hps_interface_mpu_general_purpose: expected exactly one, got "
                + str(used)
            )


mailbox_zero_fitted_rows = (
    (
        "HPS boot from FPGA",
        re.compile(r"^(?:hps[ _-]+)?boot[ _-]+from[ _-]+fpga$", re.I),
    ),
    (
        "HPS clock resets",
        re.compile(r"^(?:hps[ _-]+)?clock[ _-]+resets$", re.I),
    ),
    (
        "HPS cross trigger",
        re.compile(r"^(?:hps[ _-]+)?cross[ _-]+trigger$", re.I),
    ),
    (
        "HPS S2F AXI",
        re.compile(r"^(?:hps[ _-]+)?s2f[ _-]+axi$", re.I),
    ),
    (
        "HPS F2S AXI",
        re.compile(r"^(?:hps[ _-]+)?f2s[ _-]+axi$", re.I),
    ),
    (
        "HPS AXI Lightweight",
        re.compile(r"^(?:hps[ _-]+)?axi[ _-]+lightweight$", re.I),
    ),
    (
        "HPS interrupts",
        re.compile(r"^(?:hps[ _-]+)?interrupts?$", re.I),
    ),
    (
        "HPS JTAG",
        re.compile(r"^(?:hps[ _-]+)?jtag$", re.I),
    ),
    (
        "HPS loan I/O",
        re.compile(r"^(?:hps[ _-]+)?loan[ _-]+i/o$", re.I),
    ),
    (
        "HPS MPU event standby",
        re.compile(r"^(?:hps[ _-]+)?mpu[ _-]+event[ _-]+standby$", re.I),
    ),
    (
        "HPS STM event",
        re.compile(r"^(?:hps[ _-]+)?stm[ _-]+event$", re.I),
    ),
    (
        "HPS TPIU trace",
        re.compile(r"^(?:hps[ _-]+)?tpiu[ _-]+trace$", re.I),
    ),
    (
        "HPS DMA",
        re.compile(r"^(?:hps[ _-]+)?dma$", re.I),
    ),
    (
        "HPS CAN",
        re.compile(r"^(?:hps[ _-]+)?can$", re.I),
    ),
    (
        "HPS EMAC",
        re.compile(r"^(?:hps[ _-]+)?emac$", re.I),
    ),
    (
        "HPS I2C",
        re.compile(r"^(?:hps[ _-]+)?i2c$", re.I),
    ),
    (
        "HPS NAND Flash",
        re.compile(r"^(?:hps[ _-]+)?nand[ _-]+flash$", re.I),
    ),
    (
        "HPS QSPI",
        re.compile(r"^(?:hps[ _-]+)?qspi$", re.I),
    ),
    (
        "HPS SDMMC",
        re.compile(r"^(?:hps[ _-]+)?sdmmc$", re.I),
    ),
    (
        "HPS SPI Master",
        re.compile(r"^(?:hps[ _-]+)?spi[ _-]+master$", re.I),
    ),
    (
        "HPS SPI Slave",
        re.compile(r"^(?:hps[ _-]+)?spi[ _-]+slave$", re.I),
    ),
    (
        "HPS UART",
        re.compile(r"^(?:hps[ _-]+)?uart$", re.I),
    ),
    (
        "HPS USB",
        re.compile(r"^(?:hps[ _-]+)?usb$", re.I),
    ),
    (
        "HSSI RX PCS",
        re.compile(r"^(?:total[ _-]+)?hssi[ _-]+rx[ _-]+pcs(?:s)?$", re.I),
    ),
    (
        "HSSI PMA RX Deserializers",
        re.compile(
            r"^(?:total[ _-]+)?hssi[ _-]+pma[ _-]+rx[ _-]+deserializers?$",
            re.I,
        ),
    ),
    (
        "HSSI TX PCS",
        re.compile(r"^(?:total[ _-]+)?hssi[ _-]+tx[ _-]+pcs(?:s)?$", re.I),
    ),
    (
        "HSSI PMA TX Serializers",
        re.compile(
            r"^(?:total[ _-]+)?hssi[ _-]+pma[ _-]+tx[ _-]+serializers?$",
            re.I,
        ),
    ),
    (
        "DLL",
        re.compile(r"^(?:total[ _-]+)?dlls?$", re.I),
    ),
    (
        "SERDES transmitters",
        re.compile(r"^(?:total[ _-]+)?serdes[ _-]+transmitters?$", re.I),
    ),
    (
        "SERDES receivers",
        re.compile(r"^(?:total[ _-]+)?serdes[ _-]+receivers?$", re.I),
    ),
    (
        "JTAG blocks",
        re.compile(
            r"^(?:total[ _-]+)?(?:jtags|jtag[ _-]+blocks?)$",
            re.I,
        ),
    ),
    (
        "ASMI blocks",
        re.compile(r"^(?:total[ _-]+)?asmi[ _-]+blocks?$", re.I),
    ),
    (
        "CRC blocks",
        re.compile(r"^(?:total[ _-]+)?crc[ _-]+blocks?$", re.I),
    ),
    (
        "remote update blocks",
        re.compile(r"^(?:total[ _-]+)?remote[ _-]+update[ _-]+blocks?$", re.I),
    ),
    (
        "oscillator blocks",
        re.compile(r"^(?:total[ _-]+)?oscillator[ _-]+blocks?$", re.I),
    ),
    (
        "impedance control blocks",
        re.compile(r"^(?:total[ _-]+)?impedance[ _-]+control[ _-]+blocks?$", re.I),
    ),
    (
        "hard memory controllers",
        re.compile(r"^(?:total[ _-]+)?hard[ _-]+memory[ _-]+controllers?$", re.I),
    ),
    (
        "block memory implementation bits",
        re.compile(
            r"^(?:total[ _-]+)?block[ _-]+memory[ _-]+implementation[ _-]+bits?$",
            re.I,
        ),
    ),
)


def optional_zero_record(
    name: str,
    pattern: re.Pattern[str],
) -> tuple[dict[str, object] | None, str | None]:
    """Validate a hard-resource row when a Quartus release emits it."""

    candidates: list[tuple[int, int]] = []
    malformed = False
    for line, _delimiter, label, _value_cells in fitted_rows:
        if pattern.fullmatch(label) is None:
            continue
        used, available = count_on_line(line)
        if used is None or available is None:
            malformed = True
            continue
        candidates.append((used, available))
    if malformed:
        return None, f"{name}: fitter evidence is unrecognized"
    if not candidates:
        return None, None
    if len(candidates) != 1:
        return None, f"{name}: fitter evidence is ambiguous"
    used, available = candidates[0]
    record: dict[str, object] = {
        "used": used,
        "available": available,
        "utilization_percent": round(used * 100.0 / available, 6) if available else None,
        "evidence_kind": "fitter_summary",
        "measured": True,
    }
    if used != 0:
        return record, f"{name}: unexpected forbidden resource usage ({used})"
    return record, None


if hps_gp:
    for name, pattern in mailbox_zero_fitted_rows:
        if measure_m10k and name == "block memory implementation bits":
            continue
        _record, error = optional_zero_record(name, pattern)
        if error is not None:
            hard_errors.append(error)

if measure_m10k:
    impl_record, impl_error = measured_report_record(
        "block memory implementation bits",
        (
            re.compile(
                r"^(?:total\s+)?block\s+memory\s+implementation\s+bits?$",
                re.I,
            ),
        ),
        require_zero=False,
    )
    if impl_error is not None:
        hard_errors.append(impl_error)
    elif impl_record is None or impl_record.get("used") != 10240:
        hard_errors.append(
            "block memory implementation bits: expected 10240, got "
            + str(None if impl_record is None else impl_record.get("used"))
        )


def static_exclusion(
    name: str,
    report_pattern: re.Pattern[str],
    source_patterns: tuple[str, ...],
) -> tuple[dict[str, object], str | None]:
    static_basis = (
        "static source/project/command exclusion"
        if experiment == "020_linux_mailbox"
        else "static source/project exclusion"
    )
    proof_commands = (
        [
            {
                "path": f"build/oracle/{experiment}/{path.name}",
                "sha256": sha256_file(path),
            }
            for path in (version_log_path, quartus_log_path)
        ]
        if experiment == "020_linux_mailbox"
        else []
    )
    report_binding = (
        {
            "path": f"build/oracle/{experiment}/top.fit.rpt",
            "sha256": sha256_file(fit_path),
        }
        if experiment == "020_linux_mailbox"
        else None
    )

    def excluded_record(source_records: list[dict[str, str]]) -> dict[str, object]:
        exclusion: dict[str, object] = {
            "basis": static_basis,
            "patterns": list(source_patterns),
            "sources": source_records,
        }
        if experiment == "020_linux_mailbox":
            exclusion["commands"] = proof_commands
            exclusion["report"] = report_binding
        return {
            # No aggregate fitted class count exists for these classes in the
            # normal Cyclone V summary.  Keep that fact distinct from a
            # measured zero even when capability rows are present.
            "used": None,
            "available": None,
            "status": "excluded",
            "evidence_kind": "static_exclusion",
            "measured": False,
            "exclusion": exclusion,
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

    source_inputs = [
        (f"experiments/{experiment}/rtl/top.v", rtl_path),
        ("boards/de10nano/pins.qsf", pins_path),
        ("boards/de10nano/clocks.sdc", sdc_path),
    ]
    if experiment == "020_linux_mailbox":
        source_inputs.append(
            (f"experiments/{experiment}/oracle/top.qpf", oracle_qpf_path)
        )
    source_inputs.append((f"experiments/{experiment}/oracle/top.qsf", oracle_qsf_path))
    source_records: list[dict[str, str]] = []
    for relative, path in source_inputs:
        text = path.read_text(encoding="utf-8", errors="replace")
        for pattern in source_patterns:
            if re.search(pattern, text, re.I | re.M):
                return excluded_record(source_records), f"{name}: source/project exclusion matched {pattern!r} in {relative}"
        source_records.append({"path": relative, "sha256": sha256_file(path)})
    return excluded_record(source_records), None


static_hard_contracts: dict[str, tuple[re.Pattern[str], tuple[str, ...]]] = {}
if not measure_mlab:
    mlab_source_patterns = [
        r"\bmlab(?:s)?\b",
        r"\blutram\b",
        r"\b(?:altsyncram|lpm_ram|mlab_cell)\b",
    ]
    # Initialized block memory is a Verilog array. Keep that shape out of the
    # MLAB exclusion when this experiment measures M10K.
    if not measure_m10k:
        mlab_source_patterns.append(
            r"\b(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\["
        )
    static_hard_contracts["MLAB/LUTRAM"] = (
        re.compile(r"^(?:total\s+)?mlabs?$|^(?:total\s+)?mlab/lutram\s+blocks?$", re.I),
        tuple(mlab_source_patterns),
    )
if experiment == "010_blinky":
    static_hard_contracts["HPS"] = (
        re.compile(r"^(?:total\s+)?(?:hps|hard\s+processor\s+system)\s+blocks?$", re.I),
        (
            r"\bhps\b",
            r"\bhard[_ ]processor",
            r"\b(?:altera|cyclonev)[_ ]hps\b",
            r"\b(?:hps_component|soc_system|soc_id|arm)\b",
            r"\bsoc\b",
        ),
    )
for name, (report_pattern, source_patterns) in static_hard_contracts.items():
    record, error = static_exclusion(name, report_pattern, source_patterns)
    hard_blocks[name] = record
    if error is not None:
        hard_errors.append(error)

if measure_mlab:
    mlab_record, mlab_error = measured_report_record(
        "MLAB/LUTRAM",
        (
            re.compile(r"^(?:total\s+)?(?:mlab|lutram)\s+memory\s+bits?$", re.I),
            re.compile(r"^(?:total\s+)?lutram\s+bits?$", re.I),
        ),
        require_zero=False,
        allow_missing_available=True,
    )
    if mlab_record is not None:
        hard_blocks["MLAB/LUTRAM"] = mlab_record
        used = mlab_record.get("used")
        if type(used) is not int or used != 256:
            hard_errors.append(
                f"MLAB/LUTRAM: expected 256 lutram bits, got {used}"
            )
    if mlab_error is not None:
        hard_errors.append(mlab_error)

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
    re.compile(r"^resource$", re.I),
    re.compile(
        r"^(?:hard\s+processor\s+system|hps)?\s*peripheral\s+utilization$",
        re.I,
    ),
    re.compile(
        r"^(?:total\s+)?(?:block\s+)?memory\s+bits?$",
        re.I,
    ),
    re.compile(
        r"^(?:total\s+)?block\s+memory\s+implementation\s+bits?$",
        re.I,
    ),
    re.compile(r"^(?:total\s+)?(?:mlab|lutram)\s+memory\s+bits?$", re.I),
    re.compile(r"^memory alut usage$", re.I),
    re.compile(r"^(?:64|32)-address deep$", re.I),
    re.compile(r"^(?:total\s+)?sdram(?:\s+(?:interfaces?|ports?))?$", re.I),
    *(
        pattern
        for _name, pattern in mailbox_zero_fitted_rows
        if hps_gp
    ),
    *(mailbox_hps_patterns if hps_gp else ()),
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
) + (
    (
        "hard",
        "resource",
        "hps",
        "mpu",
        "arm",
        "sdram",
        "oscillator",
        "controller",
        "hssi",
        "serdes",
        "asmi",
        "crc",
        "remote update",
        "jtag",
        "dll",
    )
    if hps_gp
    else ()
)
ignored_prose = ("capability", "peripheral", "entity", "pin", "compilation", "diagnostic")


def add_unknown_resource(line: str) -> None:
    key = "unrecognized:" + line.strip()[:80]
    unknown_resources[key] = {"evidence": line.strip()}


for line, _delimiter, label, _value_cells in fitted_rows:
    lowered = label.casefold()
    if not any(marker in lowered for marker in unknown_markers):
        continue
    if any(pattern.fullmatch(label) for pattern in (*all_known_hard_patterns, *all_static_hard_patterns, *known_context_patterns)):
        continue
    # The normal HPS peripheral-utilization heading is a context row; an
    # unrecognized HPS/MPU/ARM entity row is still hard-resource evidence.
    if not hps_gp and any(
        word in lowered for word in ignored_prose
    ):
        continue
    # CPU scheduling diagnostics contain ``processor`` but are not fitted
    # physical resources.  A physical HPS aggregate is handled by the static
    # exclusion contract above.
    if (
        not hps_gp
        and "processor" in lowered
        and not re.search(r"\bhps\b|hard\s+processor\s+system", lowered)
    ):
        continue
    if (
        not hps_gp
        and not re.search(
            r"\b(?:total|blocks?|ram|m10k|m20k|bram|dsp|plls?|resources?|units?|count|usage)\b",
            lowered,
        )
    ):
        continue
    used, available = count_on_line(line)
    if used is None and available is None:
        # A malformed aggregate row is still evidence that must not be
        # silently ignored, while one-field capability/prose rows were
        # filtered above.
        add_unknown_resource(line)
        continue
    add_unknown_resource(line)

# Mailbox reports are accepted only against a closed fitted-row vocabulary.
# In addition to the marker scan above, reject any remaining numeric row that
# is not a known ordinary utilization row.  This catches a newly named hard
# resource even if its label does not contain one of today's markers.
if hps_gp:
    # Exact full-label vocabulary for ordinary Cyclone-V fitted resources.
    # Do not accept a row merely because its label contains a word such as
    # "logic", "clock", or "memory": new numeric rows are evidence that the
    # closed policy does not understand and therefore fail below.
    ordinary_fitted_patterns = (
        re.compile(r"^(?:total\s+)?alms?$", re.I),
        re.compile(r"^(?:total\s+)?(?:dedicated\s+logic\s+)?registers?$", re.I),
        re.compile(r"^(?:total\s+)?(?:user\s+)?(?:i\s*/?\s*o\s+)?pins?$", re.I),
        re.compile(r"^logic utilization \(alms needed / total alms on device\)$", re.I),
        re.compile(r"^alms needed \[=a-b\+c\]$", re.I),
        re.compile(r"^\[a\] alms used in final placement \[=a\+b\+c\+d\]$", re.I),
        re.compile(r"^\[[a-d]\] alms used for (?:lut logic(?: and registers)?|registers|memory \(up to half of total alms\))$", re.I),
        re.compile(r"^\[[bc]\] estimate of alms (?:recoverable by dense packing|unavailable \[=a\+b\+c\+d\])$", re.I),
        re.compile(r"^\[[a-d]\] due to (?:location constrained logic|lab-wide signal conflicts|lab input limits|virtual i/os)$", re.I),
        re.compile(r"^total labs: partially or completely used$", re.I),
        re.compile(r"^(?:logic|memory) labs(?: \(up to half of total labs\))?$", re.I),
        re.compile(r"^combinational alut usage for (?:logic|route-throughs)$", re.I),
        re.compile(r"^(?:[4-7]|<=3) input functions$", re.I),
        re.compile(r"^(?:primary|secondary) logic registers$", re.I),
        re.compile(r"^(?:design implementation|routing optimization) registers$", re.I),
        re.compile(r"^virtual pins$", re.I),
        re.compile(r"^(?:clock|dedicated input) pins$", re.I),
        re.compile(r"^global signals$", re.I),
        re.compile(r"^(?:global|quadrant|horizontal periphery) clocks$", re.I),
        re.compile(r"^(?:average|peak) interconnect usage \(total/h/v\)$", re.I),
        re.compile(r"^(?:maximum|highest non-global|total|average) fan-out$", re.I),
    )
    known_fitted_patterns = (
        *all_known_hard_patterns,
        *all_static_hard_patterns,
        *known_context_patterns,
    )
    for line, _delimiter, label, _value_cells in fitted_rows:
        if any(pattern.fullmatch(label) for pattern in known_fitted_patterns):
            continue
        lowered = label.casefold()
        if not hps_gp and any(
            word in lowered for word in ignored_prose
        ):
            continue
        if any(marker in lowered for marker in unknown_markers):
            # Already handled by the marker pass (including malformed rows).
            continue
        used, available = count_on_line(line)
        if used is None and available is None:
            continue
        if any(pattern.fullmatch(label) for pattern in ordinary_fitted_patterns):
            continue
        add_unknown_resource(line)

if unknown_resources:
    hard_errors.append("unrecognized hard-resource evidence: " + ", ".join(sorted(unknown_resources)))

hard_block_status = "pass" if not hard_errors else "fail"
if hard_errors:
    hard_block_reason = "; ".join(hard_errors)
elif measure_mlab:
    hard_block_reason = "fitter summary rows measure RAM Blocks/M10K, DSP Blocks, PLLs, 256 MLAB/LUTRAM bits, and one allowed HPS general-purpose primitive"
elif measure_dsp and measure_m10k:
    hard_block_reason = "fitter summary rows measure one RAM Block/M10K, one DSP block, PLLs, and one allowed HPS general-purpose primitive; MLAB/LUTRAM remains excluded by static source/project evidence (used=null)"
elif measure_dsp:
    hard_block_reason = "fitter summary rows measure RAM Blocks/M10K, one DSP block, PLLs, and one allowed HPS general-purpose primitive; MLAB/LUTRAM remains excluded by static source/project evidence (used=null)"
elif measure_m10k:
    hard_block_reason = "fitter summary rows measure one RAM Block/M10K, 256 MLAB/LUTRAM bits, DSP Blocks, PLLs, and one allowed HPS general-purpose primitive"
elif hps_gp:
    hard_block_reason = "fitter summary rows measure RAM Blocks/M10K, DSP Blocks, PLLs, and one allowed HPS general-purpose primitive; MLAB/LUTRAM remains excluded by static source/project evidence (used=null)"
else:
    hard_block_reason = "fitter summary rows measure RAM Blocks/M10K, DSP Blocks, and PLLs; MLAB/LUTRAM and HPS are excluded by static source/project evidence (used=null)"

clock_name = "FPGA_CLK1_50"


def top_port_evidence(path: Path) -> dict[str, int]:
    """Parse only the production top declaration for semantic port counts."""

    text = path.read_text(encoding="utf-8", errors="replace")
    match = re.search(
        r"\bmodule\s+top(?:\s*#\s*\([^;]*\))?\s*\((?P<ports>.*?)\)\s*;",
        text,
        re.I | re.S,
    )
    if match is None:
        raise ValueError("production top declaration is missing")
    counts = {"input": 0, "output": 0, "inout": 0}
    clock_inputs = 0
    current_direction: str | None = None
    for raw_segment in match.group("ports").split(","):
        segment = re.sub(r"//[^\n]*|/\*.*?\*/", " ", raw_segment, flags=re.S).strip()
        if not segment:
            continue
        direction_match = re.match(r"^(input|output|inout)\b(?P<tail>.*)$", segment, re.I | re.S)
        if direction_match:
            current_direction = direction_match.group(1).lower()
            tail = direction_match.group("tail")
        elif current_direction is not None:
            tail = segment
        else:
            raise ValueError("production top port declaration has an undeclared ANSI segment")
        names = re.findall(r"\b[A-Za-z_][A-Za-z0-9_$]*\b", tail)
        if not names:
            raise ValueError("production top port declaration is malformed")
        name = names[-1]
        counts[current_direction] += 1
        if current_direction == "input" and name == clock_name:
            clock_inputs += 1
    if clock_inputs != 1:
        raise ValueError("production top must expose exactly one FPGA_CLK1_50 input")
    return {
        "clock_inputs": clock_inputs,
        "external_input_ports": counts["input"] - clock_inputs,
        "external_output_ports": counts["output"],
        "bidirectional_ports": counts["inout"],
    }


semantic_resource_evidence: dict[str, int] | None = None
if hps_gp:
    try:
        semantic_resource_evidence = top_port_evidence(rtl_path)
    except (OSError, ValueError) as exc:
        hard_errors.append(f"semantic port evidence: {exc}")
        semantic_resource_evidence = {
            "clock_inputs": 0,
            "external_input_ports": 0,
            "external_output_ports": 0,
            "bidirectional_ports": 0,
        }

    def measured_used(
        records: Mapping[str, dict[str, object]],
        name: str,
        *,
        required: bool = True,
    ) -> int:
        record = records.get(name)
        used = record.get("used") if isinstance(record, dict) else None
        if record is None and not required:
            return 0
        if type(used) is not int or used < 0:
            hard_errors.append(f"semantic resource evidence is missing or malformed: {name}")
            return 0
        return used

    semantic_rows = {
        "block_memory_bits": (
            re.compile(r"^(?:total\s+)?block\s+memory\s+bits?$", re.I),
        ),
        "lutram_bits": (
            re.compile(r"^(?:total\s+)?(?:mlab|lutram)\s+memory\s+bits?$", re.I),
            re.compile(r"^(?:total\s+)?lutram\s+bits?$", re.I),
        ),
        "sdram_interfaces": (
            re.compile(r"^(?:total\s+)?sdram(?:\s+(?:interfaces?|ports?))?$", re.I),
        ),
    }
    for semantic_name, patterns in semantic_rows.items():
        measured, error = measured_report_record(
            semantic_name,
            patterns,
            require_zero=not (
                (measure_mlab and semantic_name == "lutram_bits")
                or (measure_m10k and semantic_name == "block_memory_bits")
            ),
            allow_missing_available=semantic_name == "lutram_bits",
        )
        if measured is not None:
            resources[semantic_name] = measured
        if error is not None:
            hard_errors.append(error)

    semantic_resource_evidence.update(
        {
            "hps_general_purpose_interfaces": measured_used(
                hard_blocks, "cyclonev_hps_interface_mpu_general_purpose"
            ),
            "pll_blocks": measured_used(hard_blocks, "PLL"),
            "dsp_blocks": measured_used(hard_blocks, "DSP"),
            "block_memory_bits": measured_used(resources, "block_memory_bits"),
            "lutram_bits": measured_used(resources, "lutram_bits"),
            "sdram_interfaces": measured_used(resources, "sdram_interfaces"),
        }
    )
    expected_dsp = 1 if measure_dsp else 0
    if semantic_resource_evidence["dsp_blocks"] != expected_dsp:
        hard_errors.append(
            f"dsp_blocks: unexpected resource usage "
            f"({semantic_resource_evidence['dsp_blocks']}), expected {expected_dsp}"
        )
    if semantic_resource_evidence["pll_blocks"] != 0:
        hard_errors.append(
            f"pll_blocks: unexpected resource usage "
            f"({semantic_resource_evidence['pll_blocks']}), expected 0"
        )
    for semantic_name in ("block_memory_bits", "lutram_bits", "sdram_interfaces"):
        expected_bits = 0
        if measure_mlab and semantic_name == "lutram_bits":
            expected_bits = 256
        elif measure_m10k and semantic_name == "block_memory_bits":
            expected_bits = 2048
        if semantic_resource_evidence[semantic_name] != expected_bits:
            hard_errors.append(
                f"{semantic_name}: unexpected resource usage "
                f"({semantic_resource_evidence[semantic_name]}), expected {expected_bits}"
            )
    hard_block_status = "pass" if not hard_errors else "fail"
    hard_block_reason = "; ".join(hard_errors) if hard_errors else hard_block_reason


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
    if restricted_indexes:
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
synthesis_report = {
    "path": f"build/oracle/{experiment}/top.fit.rpt",
    "sha256": sha256_file(fit_path),
}
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
    "clock_intent": clock_name,
    "allowed_hard_blocks": (
        {
            **(
                {"cyclonev_hps_interface_mpu_general_purpose": 1}
                if hps_gp
                else {}
            ),
            **({"MISTRAL_MUL9X9": 1} if measure_dsp else {}),
            **({"MISTRAL_M10K": 1} if measure_m10k else {}),
        }
    ),
    "resource_evidence": semantic_resource_evidence,
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
    "synthesis_report": synthesis_report,
    "synthesis_report_path": synthesis_report["path"],
    "synthesis_report_sha256": synthesis_report["sha256"],
    "target": target,
    "lane": "oracle",
    "experiment": experiment,
    "simulation": {"status": "not-run"},
}
summary_path.write_text(json.dumps(summary, indent=2, sort_keys=False) + "\n", encoding="utf-8")
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
    --source "$ROOT/scripts/experiment_policy.py"
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
