#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
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

cd -- "$ROOT"

rtl_rel="experiments/$EXP/rtl/top.v"
qsf_rel="boards/de10nano/pins.qsf"
sdc_rel="boards/de10nano/clocks.sdc"
out_rel="build/oss/$EXP"
out_dir="$ROOT/$out_rel"
run_logged="$ROOT/scripts/run_logged.sh"
collector="$ROOT/scripts/collect_manifest.py"
lockfile="$ROOT/scripts/lockfile.py"

rtl="$ROOT/$rtl_rel"
qsf="$ROOT/$qsf_rel"
sdc="$ROOT/$sdc_rel"
yosys="$TOOLCHAIN_INSTALL/bin/yosys"
nextpnr="$TOOLCHAIN_INSTALL/bin/nextpnr-mistral"

require_file() {
    local path=$1
    [[ -f "$path" ]] || fail "missing required file: $path"
}

require_file "$rtl"
require_file "$qsf"
require_file "$sdc"
require_file "$run_logged"
require_file "$collector"
require_file "$lockfile"

out_synth="$out_dir/synth.json"
out_routed="$out_dir/routed.json"
out_rbf="$out_dir/top.rbf"
out_timing_json="$out_dir/timing.json"
out_timing_txt="$out_dir/timing.txt"
yosys_log="$out_dir/yosys.log"
nextpnr_help_log="$out_dir/nextpnr-help.log"
nextpnr_log="$out_dir/nextpnr.log"
manifest_log="$out_dir/manifest-collect.log"
manifest="$out_dir/manifest.json"

yosys_program="read_verilog $rtl_rel; synth_intel_alm -nobram -nolutram -nodsp -top top; cd top; rename LED \\LED[0]; stat; write_json $out_rel/synth.json"
yosys_cmd=("$yosys" -p "$yosys_program")
nextpnr_help_cmd=("$nextpnr" --help)
nextpnr_cmd=(
    "$nextpnr"
    --json "$out_rel/synth.json"
    --device "$TARGET"
    --qsf "$qsf_rel"
    --sdc "$sdc_rel"
    --freq 50
    --rbf "$out_rel/top.rbf"
    --write "$out_rel/routed.json"
    --report "$out_rel/timing.json"
    --detailed-timing-report
)

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
    print_cmd "$run_logged" "$out_rel/nextpnr-help.log" "${nextpnr_help_cmd[@]}"
    print_cmd "$run_logged" "$out_rel/nextpnr.log" "${nextpnr_cmd[@]}"
    exit 0
fi

authenticate_tool() {
    local lock_name=$1
    local tool_name=$2
    local binary="$TOOLCHAIN_INSTALL/bin/$tool_name"
    local commit stamp digest expected actual stamp_value

    [[ -f "$binary" && ! -L "$binary" && -x "$binary" ]] \
        || fail "repository-local executable is not authenticated: $binary"
    commit="$($PYTHON "$lockfile" get "$lock_name" commit)" \
        || fail "cannot read lock entry: $lock_name"
    stamp="$TOOLCHAIN_BUILD/$lock_name/.built-$commit"
    digest="$TOOLCHAIN_BUILD/$lock_name/.digest-$commit.sha256"
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
}

authenticate_tool yosys yosys
authenticate_tool nextpnr nextpnr-mistral

mkdir -p -- "$out_dir"

: > "$out_synth"
: > "$out_routed"
: > "$out_rbf"
: > "$out_timing_json"
: > "$out_timing_txt"

"$run_logged" "$yosys_log" "${yosys_cmd[@]}"
[[ -s "$out_synth" ]] || fail "synthesis did not produce a nonempty JSON design: $out_synth"

"$run_logged" "$nextpnr_help_log" "${nextpnr_help_cmd[@]}"
for required_flag in --json --device --qsf --sdc --freq --rbf --write --report --detailed-timing-report; do
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
[[ -s "$out_rbf" ]] || fail "place-and-route did not produce a nonempty RBF: $out_rbf"
[[ -s "$out_timing_json" ]] || fail "place-and-route did not produce a nonempty timing report: $out_timing_json"

timing_pass=0
rbf_size=$(wc -c < "$out_rbf")
rbf_hash=$(sha256sum -- "$out_rbf")
rbf_hash=${rbf_hash%% *}
{
    printf 'target: %s\n' "$TARGET"
    printf 'constraint: 50 MHz (20.000 ns)\n'
    printf 'rbf_size_bytes: %s\n' "$rbf_size"
    printf 'rbf_sha256: %s\n' "$rbf_hash"
    while IFS= read -r route_line; do
        route_lower=${route_line,,}
        if [[ "$route_lower" == *unrouted* ]]; then
            fail "unrouted marker found in route log: $nextpnr_log"
        fi
        if [[ "$route_line" == *"PASS at 50.00 MHz"* || "$route_line" == *"PASS at 50 MHz"* ]]; then
            timing_pass=1
        fi
        case "$route_line" in
            *"Max frequency"*|*"MISTRAL_"*|*"Critical path"*|*"Slack"*)
                printf '%s\n' "$route_line"
                ;;
        esac
    done < "$nextpnr_log"
} > "$out_timing_txt"
(( timing_pass == 1 )) || fail "50 MHz timing requirement was not met; see $nextpnr_log"
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
    --source "$ROOT/toolchain.lock"
    --command-log "$yosys_log"
    --command-log "$nextpnr_help_log"
    --command-log "$nextpnr_log"
    --artifact "$out_synth"
    --artifact "$out_routed"
    --artifact "$out_rbf"
    --artifact "$out_timing_json"
    --artifact "$out_timing_txt"
    --manifest "$manifest"
)

"$run_logged" "$manifest_log" "${manifest_cmd[@]}"
[[ -s "$manifest" ]] || fail "manifest was not produced: $manifest"

printf 'OSS build complete: %s\n' "$out_dir"
