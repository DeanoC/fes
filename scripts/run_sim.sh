#!/usr/bin/env bash
# Simulate one closed experiment with the repository-local Verilator.

set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)"
PYTHON="${PYTHON:-python3}"
EXP="${EXP:-}"

usage() {
    printf '%s\n' "usage: ${0##*/} --experiment EXP"
}

fail() {
    printf '%s\n' "$*" >&2
    exit 2
}

while (( $# > 0 )); do
    case "$1" in
        --experiment)
            (( $# >= 2 )) || fail "--experiment requires a value"
            EXP=$2
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            fail "unknown option: $1"
            ;;
    esac
done

[[ "$EXP" =~ ^[0-9][0-9][0-9]_[a-z0-9_]+$ ]] || fail "invalid EXP: $EXP"

cd -- "$ROOT"

if [[ -z "${OPEN_MISTER_ROOT:-}" || -z "${TOOLCHAIN_INSTALL:-}" ]]; then
    # shellcheck source=scripts/env.sh
    source "$ROOT/scripts/env.sh"
fi

policy_tool="$ROOT/scripts/experiment_policy.py"
run_logged="$ROOT/scripts/run_logged.sh"
doctor="$ROOT/scripts/doctor.py"
verilator="${TOOLCHAIN_INSTALL}/bin/verilator"
sim_dir="$ROOT/build/sim/$EXP"

[[ -f "$policy_tool" ]] || fail "missing experiment policy: $policy_tool"
[[ -f "$run_logged" ]] || fail "missing command logger: $run_logged"
[[ -f "$doctor" ]] || fail "missing doctor: $doctor"

if ! "$PYTHON" "$policy_tool" --experiment "$EXP" --format json >/dev/null; then
    fail "unknown simulation experiment: $EXP"
fi

"$PYTHON" "$ROOT/scripts/doctor.py" --check-tool verilator
[[ -x "$verilator" ]] || fail "missing repository-local verilator: $verilator"

mkdir -p -- "$sim_dir"

jobs_json="$("$PYTHON" "$policy_tool" --experiment "$EXP" --format json)"
job_count="$("$PYTHON" -c 'import json,sys; print(len(json.loads(sys.argv[1]).get("sim_jobs") or []))' "$jobs_json")"
(( job_count > 0 )) || fail "unknown simulation experiment: $EXP"

job_index=0
while (( job_index < job_count )); do
    eval "$("$PYTHON" -c '
import json, shlex, sys
job = json.loads(sys.argv[1])["sim_jobs"][int(sys.argv[2])]
assignments = {
    "job_name": job["name"],
    "job_top": job["top"],
    "job_tb": job["tb"],
    "job_lint": "1" if job["lint"] else "0",
}
print("job_name=" + shlex.quote(assignments["job_name"]))
print("job_top=" + shlex.quote(assignments["job_top"]))
print("job_tb=" + shlex.quote(assignments["job_tb"]))
print("job_lint=" + assignments["job_lint"])
print("job_sources=( " + " ".join(shlex.quote(path) for path in job["sources"]) + " )")
print("job_cflags=( " + " ".join(shlex.quote(flag) for flag in job["cflags"]) + " )")
print("job_params=( " + " ".join(shlex.quote(f"{name}={value}") for name, value in job["parameters"].items()) + " )")
' "$jobs_json" "$job_index")"

    source_args=()
    for source_rel in "${job_sources[@]}"; do
        [[ -f "$ROOT/$source_rel" ]] || fail "missing simulation source: $source_rel"
        source_args+=("$ROOT/$source_rel")
    done
    [[ -f "$ROOT/$job_tb" ]] || fail "missing simulation testbench: $job_tb"

    param_args=()
    for parameter in "${job_params[@]}"; do
        param_args+=("-G${parameter}")
    done
    cflag_args=()
    if ((${#job_cflags[@]} > 0)); then
        cflag_args+=(-CFLAGS "${job_cflags[*]}")
    fi

    if [[ "$job_name" == "main" ]]; then
        build_mdir="$sim_dir/obj_dir"
        build_log="$sim_dir/verilator-build.log"
        run_log="$sim_dir/simulation.log"
        binary="$build_mdir/V${job_top}"
    else
        build_mdir="$sim_dir/${job_name}_obj_dir"
        build_log="$sim_dir/verilator-${job_name}-build.log"
        run_log="$sim_dir/${job_name}-simulation.log"
        binary="$build_mdir/V${job_top}"
    fi

    if [[ "$job_lint" == "1" ]]; then
        "$run_logged" "$sim_dir/verilator-lint.log" \
            "$verilator" --lint-only --top-module "$job_top" --Mdir "$sim_dir/lint" \
            "${source_args[@]}"
    fi

    "$run_logged" "$build_log" \
        "$verilator" --cc --exe --build --public --top-module "$job_top" \
        "${param_args[@]}" --Mdir "$build_mdir" \
        "${source_args[@]}" "$ROOT/$job_tb" \
        "${cflag_args[@]}"

    "$run_logged" "$run_log" "$binary"
    cat "$run_log"

    job_index=$((job_index + 1))
done
