#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)"
PYTHON="${PYTHON:-python3}"
EXP="${EXP:-010_blinky}"
TOOLCHAIN_INSTALL="${TOOLCHAIN_INSTALL:-$ROOT/build/toolchain/install}"
TOOLCHAIN_BUILD="${TOOLCHAIN_BUILD:-$ROOT/build/toolchain/build}"
TARGET="5CSEBA6U23I7"
PRINT_COMMANDS=0
POSITIONAL_SET=0

usage() {
    printf '%s\n' \
        "usage: ${0##*/} [--print-commands] [--experiment EXP]" \
        "       ${0##*/} [--toolchain-install DIR] [--toolchain-build DIR]"
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
        --toolchain-install)
            (( $# >= 2 )) || fail "--toolchain-install requires a value"
            TOOLCHAIN_INSTALL=$2
            shift 2
            ;;
        --toolchain-build)
            (( $# >= 2 )) || fail "--toolchain-build requires a value"
            TOOLCHAIN_BUILD=$2
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

if [[ -n "${FES_TOOLCHAIN_CACHE_ROOT:-}" ]]; then
    fail "generic OSS does not yet support shared toolchain cache; unset FES_TOOLCHAIN_CACHE_ROOT for the local lane or use FES Python recipes"
fi

cd -- "$ROOT"

out_rel="build/oss/$EXP"
out_dir="$ROOT/$out_rel"
out_synth="$out_dir/synth.json"
out_routed="$out_dir/routed.json"
out_rbf="$out_dir/top.rbf"
out_timing_json="$out_dir/timing.json"
out_timing_txt="$out_dir/timing.txt"
out_summary="$out_dir/build-summary.json"
yosys_log="$out_dir/yosys.log"
nextpnr_help_log="$out_dir/nextpnr-help.log"
nextpnr_log="$out_dir/nextpnr.log"
summary_log="$out_dir/summary.log"
manifest_log="$out_dir/manifest-collect.log"
manifest="$out_dir/manifest.json"
run_logged="$ROOT/scripts/run_logged.sh"
collector="$ROOT/scripts/collect_manifest.py"
lockfile="$ROOT/scripts/lockfile.py"
policy_tool="$ROOT/scripts/experiment_policy.py"
summary_tool="$ROOT/scripts/oss_summary.py"
rtl_rel=""
qsf_rel=""
sdc_rel=""
policy_name=""
policy_top=""
policy_clock=""
policy_clock_mhz=""
policy_artifact=""
policy_sources_json=""
policy_nobram=""
policy_nolutram=""
policy_nodsp=""
policy_yosys_post_synth=""
policy_nextpnr_router=""
policy_synth_only=""
rtl=""
qsf=""
sdc=""
yosys="$TOOLCHAIN_INSTALL/bin/yosys"
nextpnr="$TOOLCHAIN_INSTALL/bin/nextpnr-mistral"
YOSYS_COMMIT=""
YOSYS_DIGEST=""
NEXTPNR_COMMIT=""
NEXTPNR_DIGEST=""

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

require_file() {
    local path=$1
    [[ -f "$path" ]] || fail "missing required file: $path"
}

validate_output_tree() {
    local build_root="$ROOT/build"
    local existing="$out_dir"
    local canonical_build existing_real

    [[ "$out_dir" == "$build_root"/* ]] \
        || fail "unsafe OSS output path outside $build_root: $out_dir"
    if path_has_symlink_component "$out_dir"; then
        fail "OSS output path contains a symlink component: $out_dir"
    fi
    if [[ -e "$out_dir" && ! -d "$out_dir" ]]; then
        fail "OSS output path is not a directory: $out_dir"
    fi

    if [[ -e "$build_root" ]]; then
        [[ -d "$build_root" ]] || fail "build root is not a directory: $build_root"
        canonical_build="$(cd -- "$build_root" && pwd -P)"
        [[ "$canonical_build" == "$build_root" ]] \
            || fail "build root resolves outside repository: $build_root"
    fi

    while [[ ! -e "$existing" && ! -L "$existing" ]]; do
        existing=$(dirname -- "$existing")
    done
    [[ -d "$existing" ]] || fail "OSS output parent is not a directory: $existing"
    existing_real="$(cd -- "$existing" && pwd -P)"
    if [[ -n "${canonical_build:-}" ]]; then
        [[ "$existing_real" == "$canonical_build" || "$existing_real" == "$canonical_build"/* ]] \
            || fail "OSS output resolves outside repository build root: $out_dir"
    else
        [[ "$existing_real" == "$ROOT" || "$existing_real" == "$ROOT"/* ]] \
            || fail "OSS output resolves outside repository: $out_dir"
    fi

    local output_leaf
    for output_leaf in "$out_synth" "$out_routed" "$out_rbf" "$out_timing_json" \
        "$out_timing_txt" "$yosys_log" "$nextpnr_help_log" "$nextpnr_log" \
        "$out_summary" "$summary_log" "$manifest_log" "$manifest"; do
        [[ ! -L "$output_leaf" ]] \
            || fail "OSS output file is a symlink: $output_leaf"
    done
}

validate_real_toolchain_roots() {
    local expected_install="$ROOT/build/toolchain/install"
    local expected_build="$ROOT/build/toolchain/build"
    [[ "$TOOLCHAIN_INSTALL" == "$expected_install" ]] \
        || fail "real OSS builds require canonical toolchain install root: $expected_install"
    [[ "$TOOLCHAIN_BUILD" == "$expected_build" ]] \
        || fail "real OSS builds require canonical toolchain build root: $expected_build"
    local root
    for root in "$expected_install" "$expected_build"; do
        if path_has_symlink_component "$root"; then
            fail "toolchain root contains a symlink component: $root"
        fi
        if [[ -e "$root" && ! -d "$root" ]]; then
            fail "toolchain root is not a directory: $root"
        fi
    done
    local nested_path
    for nested_path in \
        "$expected_install/bin" \
        "$expected_install/bin/yosys" \
        "$expected_install/bin/nextpnr-mistral" \
        "$expected_build/yosys" \
        "$expected_build/nextpnr"; do
        if path_has_symlink_component "$nested_path"; then
            fail "toolchain path contains a symlink component: $nested_path"
        fi
    done
}

validate_output_tree

require_file "$policy_tool"

policy_output="$(
    "$PYTHON" "$policy_tool" \
        --experiment "$EXP" \
        --format shell \
        --check-sources \
        --repo-root "$ROOT"
)" || fail "cannot load closed experiment policy: $EXP"

# Parse the policy's fixed-key output without eval or shell interpolation.
# Every value is selected by a literal key and originates from the closed table.
while IFS='=' read -r policy_key policy_value; do
    case "$policy_key" in
        name) policy_name=$policy_value ;;
        source) rtl_rel=$policy_value ;;
        sources) policy_sources_json=$policy_value ;;
        top) policy_top=$policy_value ;;
        clock) policy_clock=$policy_value ;;
        clock_mhz) policy_clock_mhz=$policy_value ;;
        qsf) qsf_rel=$policy_value ;;
        sdc) sdc_rel=$policy_value ;;
        artifact) policy_artifact=$policy_value ;;
        nobram) policy_nobram=$policy_value ;;
        nolutram) policy_nolutram=$policy_value ;;
        nodsp) policy_nodsp=$policy_value ;;
        yosys_post_synth) policy_yosys_post_synth=$policy_value ;;
        nextpnr_router) policy_nextpnr_router=$policy_value ;;
        synth_only) policy_synth_only=$policy_value ;;
        allowed_hard_blocks) : ;; # Consumed by Python summary validation.
        "") : ;;
        *) fail "closed experiment policy emitted an unknown field: $policy_key" ;;
    esac
done <<< "$policy_output"

[[ "$policy_name" == "$EXP" ]] || fail "closed experiment policy name mismatch"
[[ -n "$rtl_rel" && -n "$policy_sources_json" && -n "$policy_top" && -n "$policy_clock" ]] \
    || fail "closed experiment policy is missing source/top/clock"
case "$policy_clock_mhz" in
    25|50|100) ;;
    *) fail "closed experiment policy must constrain 25, 50 or 100 MHz" ;;
esac
[[ "$policy_artifact" == "top.rbf" ]] || fail "closed experiment policy must emit top.rbf"
[[ "$policy_nobram" == "0" || "$policy_nobram" == "1" ]] \
    || fail "closed experiment policy nobram must be 0 or 1"
[[ "$policy_nolutram" == "0" || "$policy_nolutram" == "1" ]] \
    || fail "closed experiment policy nolutram must be 0 or 1"
[[ "$policy_nodsp" == "0" || "$policy_nodsp" == "1" ]] \
    || fail "closed experiment policy nodsp must be 0 or 1"
[[ "$policy_synth_only" == "0" || "$policy_synth_only" == "1" ]] \
    || fail "closed experiment policy synth_only must be 0 or 1"

rtl="$ROOT/$rtl_rel"
qsf="$ROOT/$qsf_rel"
sdc="$ROOT/$sdc_rel"

if (( ! PRINT_COMMANDS )); then
    validate_real_toolchain_roots
fi

require_file "$rtl"
require_file "$qsf"
require_file "$sdc"
require_file "$run_logged"
require_file "$collector"
require_file "$lockfile"
require_file "$summary_tool"

mapfile -t policy_source_list < <(
    "$PYTHON" -c 'import json,sys; print("\n".join(json.loads(sys.argv[1])))' \
        "$policy_sources_json"
) || fail "closed experiment policy sources are not valid JSON"
(( ${#policy_source_list[@]} > 0 )) || fail "closed experiment policy sources are empty"
[[ "${policy_source_list[0]}" == "$rtl_rel" ]] \
    || fail "closed experiment policy source list does not start with the primary source"

read_verilog_cmds=""
for source_rel in "${policy_source_list[@]}"; do
    [[ -n "$source_rel" ]] || fail "closed experiment policy contains an empty source path"
    require_file "$ROOT/$source_rel"
    read_verilog_cmds+="read_verilog ${source_rel}; "
done

synth_flags=""
[[ "$policy_nobram" == "1" ]] && synth_flags+=" -nobram"
[[ "$policy_nolutram" == "1" ]] && synth_flags+=" -nolutram"
[[ "$policy_nodsp" == "1" ]] && synth_flags+=" -nodsp"

yosys_post=""
if [[ -n "${policy_yosys_post_synth:-}" ]]; then
    yosys_post="${policy_yosys_post_synth}; "
fi
yosys_program="${read_verilog_cmds}synth_intel_alm${synth_flags} -top ${policy_top}; ${yosys_post}stat; write_json ${out_rel}/synth.json"
yosys_cmd=("$yosys" -p "$yosys_program")
nextpnr_help_cmd=("$nextpnr" --help)
nextpnr_cmd=(
    "$nextpnr"
    --json "$out_rel/synth.json"
    --device "$TARGET"
    --qsf "$qsf_rel"
    --sdc "$sdc_rel"
    --freq "$policy_clock_mhz"
    --rbf "$out_rel/$policy_artifact"
    --compress-rbf
    --write "$out_rel/routed.json"
    --report "$out_rel/timing.json"
    --detailed-timing-report
)
if [[ -n "$policy_nextpnr_router" ]]; then
    [[ "$policy_nextpnr_router" == "router1" ]] \
        || fail "closed experiment policy nextpnr_router must be empty or router1"
    nextpnr_cmd+=(--router "$policy_nextpnr_router")
fi

print_cmd() {
    printf 'command:'
    printf ' %q' "$@"
    printf '\n'
}

if (( PRINT_COMMANDS )); then
    printf 'experiment: %s\n' "$EXP"
    printf 'target: %s\n' "$TARGET"
    printf 'synthesis: %s\n' "$yosys_program"
    print_cmd "$run_logged" "$out_rel/yosys.log" "${yosys_cmd[@]}"
    if [[ "$policy_synth_only" != "1" ]]; then
        print_cmd "$run_logged" "$out_rel/nextpnr-help.log" "${nextpnr_help_cmd[@]}"
        print_cmd "$run_logged" "$out_rel/nextpnr.log" "${nextpnr_cmd[@]}"
    fi
    exit 0
fi

authenticate_tool() {
    local lock_name=$1
    local tool_name=$2
    local binary="$TOOLCHAIN_INSTALL/bin/$tool_name"
    local commit stamp digest expected actual stamp_value

    if path_has_symlink_component "$binary"; then
        fail "repository-local executable path contains a symlink component: $binary"
    fi
    [[ -f "$binary" && ! -L "$binary" && -x "$binary" ]] \
        || fail "repository-local executable is not authenticated: $binary"
    commit="$($PYTHON "$lockfile" get "$lock_name" commit)" \
        || fail "cannot read lock entry: $lock_name"
    stamp="$TOOLCHAIN_BUILD/$lock_name/.built-$commit"
    digest="$TOOLCHAIN_BUILD/$lock_name/.digest-$commit.sha256"
    if path_has_symlink_component "$stamp" || path_has_symlink_component "$digest"; then
        fail "toolchain evidence path contains a symlink component: $TOOLCHAIN_BUILD/$lock_name"
    fi
    [[ -f "$stamp" && ! -L "$stamp" ]] \
        || fail "missing build stamp: $stamp"
    stamp_value="$(<"$stamp")"
    [[ "$stamp_value" == "commit=$commit" ]] \
        || fail "build stamp does not authenticate $tool_name"
    [[ -f "$digest" && ! -L "$digest" ]] \
        || fail "missing digest evidence: $digest"
    expected="$(<"$digest")"
    [[ "$expected" =~ ^[0-9a-f]{64}$ ]] \
        || fail "invalid digest evidence: $digest"
    actual="$(sha256sum -- "$binary")"
    actual="${actual%% *}"
    [[ "$actual" == "$expected" ]] \
        || fail "binary digest does not match lock evidence: $binary"
    if [[ "$tool_name" == "yosys" ]]; then
        YOSYS_COMMIT=$commit
        YOSYS_DIGEST=$actual
    elif [[ "$tool_name" == "nextpnr-mistral" ]]; then
        NEXTPNR_COMMIT=$commit
        NEXTPNR_DIGEST=$actual
    fi
}

authenticate_tool yosys yosys
if [[ "$policy_synth_only" != "1" ]]; then
    authenticate_tool nextpnr nextpnr-mistral
fi

mkdir -p -- "$out_dir"

previous_rbf_sha256=""
if [[ -s "$out_rbf" ]]; then
    previous_rbf_sha256="$(sha256sum -- "$out_rbf")"
    previous_rbf_sha256="${previous_rbf_sha256%% *}"
fi

: > "$out_synth"
: > "$out_routed"
: > "$out_rbf"
: > "$out_timing_json"
: > "$out_timing_txt"
: > "$out_summary"

"$run_logged" "$yosys_log" "${yosys_cmd[@]}"
[[ -s "$out_synth" ]] || fail "synthesis did not produce a nonempty JSON design: $out_synth"
"$PYTHON" "$policy_tool" --experiment "$EXP" --fix-synth-json "$out_synth" >/dev/null \
    || fail "cannot apply synth json port directions: $EXP"
"$PYTHON" "$policy_tool" --experiment "$EXP" --check-synth-json "$out_synth" >/dev/null \
    || fail "synth json does not satisfy closed experiment policy: $EXP"

if [[ "$policy_synth_only" == "1" ]]; then
    printf 'synth-only experiment %s wrote %s\n' "$EXP" "$out_synth"
    exit 0
fi

"$run_logged" "$nextpnr_help_log" "${nextpnr_help_cmd[@]}"
required_flags=(--json --device --qsf --sdc --freq --rbf --compress-rbf --write --report --detailed-timing-report)
if [[ "$policy_nextpnr_router" == "router1" ]]; then
    required_flags+=(--router)
fi
for required_flag in "${required_flags[@]}"; do
    flag_found=0
    while IFS= read -r help_line; do
        if [[ "$help_line" == *"$required_flag"* ]]; then
            flag_found=1
            break
        fi
    done < "$nextpnr_help_log"
    (( flag_found == 1 )) || fail "local nextpnr help does not provide required flag: $required_flag"
done

"$run_logged" "$nextpnr_log" "${nextpnr_cmd[@]}"
[[ -s "$out_routed" ]] || fail "place-and-route did not produce a nonempty routed JSON: $out_routed"
"$PYTHON" "$policy_tool" --experiment "$EXP" --check-routed-json "$out_routed" >/dev/null \
    || fail "routed json does not satisfy closed experiment policy: $EXP"
[[ -s "$out_rbf" ]] || fail "place-and-route did not produce a nonempty RBF: $out_rbf"
[[ -s "$out_timing_json" ]] || fail "place-and-route did not produce a nonempty timing report: $out_timing_json"

summary_cmd=(
    "$PYTHON" "$summary_tool"
    --timing-json "$out_timing_json"
    --route-log "$nextpnr_log"
    --rbf "$out_rbf"
    --requested-mhz "$policy_clock_mhz"
    --clock-prefix "$policy_clock"
    --output "$out_summary"
    --timing-output "$out_timing_txt"
    --target "$TARGET"
    --lane oss
    --experiment "$EXP"
    --authenticated-tool "yosys=$YOSYS_COMMIT:$YOSYS_DIGEST"
    --authenticated-tool "nextpnr-mistral=$NEXTPNR_COMMIT:$NEXTPNR_DIGEST"
    --tool-pin "yosys=$YOSYS_COMMIT"
    --tool-pin "nextpnr=$NEXTPNR_COMMIT"
)
for source_spec in \
    "$rtl_rel" \
    "$qsf_rel" \
    "$sdc_rel" \
    "scripts/build_oss.sh" \
    "scripts/run_logged.sh" \
    "scripts/collect_manifest.py" \
    "scripts/experiment_policy.py" \
    "scripts/oss_summary.py" \
    "toolchain.lock"; do
    source_digest="$(sha256sum -- "$ROOT/$source_spec")"
    source_digest="${source_digest%% *}"
    summary_cmd+=(--source-hash "$source_spec=$source_digest")
done
if [[ -n "$previous_rbf_sha256" ]]; then
    summary_cmd+=(--previous-rbf-sha256 "$previous_rbf_sha256")
fi
if [[ -s "$manifest" ]]; then
    summary_cmd+=(--previous-manifest "$manifest")
fi
"$run_logged" "$summary_log" "${summary_cmd[@]}"
[[ -s "$out_summary" ]] || fail "build summary was not produced: $out_summary"
[[ -s "$out_timing_txt" ]] || fail "timing summary is empty: $out_timing_txt"

manifest_cmd=(
    "$PYTHON" "$collector"
    --output-dir "$out_dir"
    --experiment "$EXP"
    --lane oss
    --target "$TARGET"
    --repo-root "$ROOT"
    --build-root "$ROOT/build"
    --source "$rtl"
    --source "$qsf"
    --source "$sdc"
    --source "$ROOT/scripts/build_oss.sh"
    --source "$run_logged"
    --source "$collector"
    --source "$policy_tool"
    --source "$summary_tool"
    --source "$ROOT/toolchain.lock"
    --command-log "$yosys_log"
    --command-log "$nextpnr_help_log"
    --command-log "$nextpnr_log"
    --command-log "$summary_log"
    --artifact "$out_synth"
    --artifact "$out_routed"
    --artifact "$out_rbf"
    --artifact "$out_timing_json"
    --artifact "$out_timing_txt"
    --artifact "$out_summary"
    --build-summary "$out_summary"
    --manifest "$manifest"
)

"$run_logged" "$manifest_log" "${manifest_cmd[@]}"
[[ -s "$manifest" ]] || fail "manifest was not produced: $manifest"

printf 'OSS build complete: %s\n' "$out_dir"
