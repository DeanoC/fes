#!/usr/bin/env bash
set -euo pipefail

# Build the immutable OSS toolchain in the repository.  This script deliberately
# has no package-manager path: --check-prereqs only reports what a host needs.

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
OPEN_MISTER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd -P)"
LOCKFILE="$OPEN_MISTER_ROOT/scripts/lockfile.py"
PYTHON="${PYTHON:-python3}"
resolve_repo_path() {
    local path="$1"
    if [[ "$path" == /* ]]; then
        printf '%s\n' "$path"
    else
        printf '%s/%s\n' "$OPEN_MISTER_ROOT" "$path"
    fi
}

# Capture caller-provided local roots before assigning the legacy defaults
# below.  Shared mode deliberately rejects those overrides, while the local
# lane keeps its existing precedence.
INHERITED_TOOLCHAIN_ROOT="${TOOLCHAIN_ROOT:-}"
INHERITED_TOOLCHAIN_INSTALL="${TOOLCHAIN_INSTALL:-}"
INHERITED_TOOLCHAIN_BUILD="${TOOLCHAIN_BUILD:-}"
INHERITED_ROCM_PATH="${ROCM_PATH:-}"
INHERITED_HIPCC="${HIPCC:-}"
INHERITED_CUDA_HOME="${CUDA_HOME:-}"
INHERITED_CUDACXX="${CUDACXX:-}"
INHERITED_CUDA_PATH="${CUDA_PATH:-}"

TOOLCHAIN_ROOT="$(resolve_repo_path "${FES_TOOLCHAIN_ROOT:-build/toolchain}")"
TOOLCHAIN_LOCK_PATH="$(resolve_repo_path "${FES_TOOLCHAIN_LOCKFILE:-toolchain.lock}")"
SRC_ROOT="$TOOLCHAIN_ROOT/src"
BUILD_ROOT="$TOOLCHAIN_ROOT/build"
INSTALL_ROOT="$TOOLCHAIN_ROOT/install"
JOBS="${JOBS:-$(command -v nproc >/dev/null 2>&1 && nproc || printf '2')}"
GPU_ROUTER="${FES_TOOLCHAIN_GPU_ROUTER:-OFF}"
HIP_ARCHITECTURES="${FES_TOOLCHAIN_HIP_ARCHITECTURES:-gfx1100;gfx1201}"
# Native sm_86 code avoids PTX JIT, which fails when the NVIDIA driver is
# older than the CUDA toolkit.
CUDA_ARCHITECTURES="${FES_TOOLCHAIN_CUDA_ARCHITECTURES:-86}"
CACHE_HELPER="$SCRIPT_DIR/toolchain_cache.py"
CACHE_ENABLED=0
CACHE_ROOT_REQUEST="${FES_TOOLCHAIN_CACHE_ROOT:-}"
# The LRU size cap. prepare_shared_build_environment scrubs the variable, so
# publish receives it as --max-gb and no build child ever sees it.
CACHE_MAX_GB_REQUEST="${FES_TOOLCHAIN_CACHE_MAX_GB:-}"
CACHE_LOCK_FD=""
# Per-tool install plan from toolchain_cache.py. Unset first: an inherited
# variable of the same name would otherwise block the associative declare.
unset CACHE_TOOL_KEY CACHE_TOOL_DIRECTORY CACHE_TOOL_LOCK
declare -A CACHE_TOOL_KEY=() CACHE_TOOL_DIRECTORY=() CACHE_TOOL_LOCK=()

die() {
    printf 'bootstrap: %s\n' "$*" >&2
    exit 1
}

# Prerequisite reporting is intentionally independent of cache identity.  A
# missing host capability must be listed by --check-prereqs rather than being
# converted into an opaque compiler-inventory failure while planning a slot.
if [[ -n "$CACHE_ROOT_REQUEST" && "${1:-}" != "--check-prereqs" ]]; then
    [[ -f "$CACHE_HELPER" ]] || die "shared cache helper is missing: $CACHE_HELPER"
    command -v "$PYTHON" >/dev/null 2>&1 || die "shared cache requires Python: $PYTHON"
    [[ -z "$INHERITED_TOOLCHAIN_ROOT" && -z "$INHERITED_TOOLCHAIN_INSTALL" && -z "$INHERITED_TOOLCHAIN_BUILD" ]] ||
        die "shared cache cannot combine explicit local TOOLCHAIN_ROOT/TOOLCHAIN_INSTALL/TOOLCHAIN_BUILD"
    cache_plan_lines="$($PYTHON "$CACHE_HELPER" \
        --root "$OPEN_MISTER_ROOT" \
        --lock "$TOOLCHAIN_LOCK_PATH" \
        --cache "$CACHE_ROOT_REQUEST" \
        --gpu-router "$GPU_ROUTER" \
        --hip-architectures "$HIP_ARCHITECTURES" \
        --cuda-architectures "$CUDA_ARCHITECTURES" \
        plan --format lines)" || die "cannot derive shared toolchain cache identity"
    while IFS=$'\t' read -r cache_tool cache_key cache_directory cache_lock; do
        case "$cache_tool" in
            yosys|mistral|nextpnr|verilator|openfpgaloader) ;;
            *) die "shared cache helper returned an unknown tool: $cache_tool" ;;
        esac
        [[ "$cache_key" =~ ^[0-9a-f]{64}$ && "$cache_directory" == /* && "$cache_lock" == /* ]] ||
            die "shared cache helper returned an invalid plan for $cache_tool"
        CACHE_TOOL_KEY["$cache_tool"]=$cache_key
        CACHE_TOOL_DIRECTORY["$cache_tool"]=$cache_directory
        CACHE_TOOL_LOCK["$cache_tool"]=$cache_lock
    done <<<"$cache_plan_lines"
    ((${#CACHE_TOOL_KEY[@]} == 5)) || die "shared cache helper returned an incomplete plan"
    CACHE_ENABLED=1
fi

# SRC_ROOT, BUILD_ROOT and INSTALL_ROOT stay writable: the shared cache points
# them at each tool's own install directory in turn.
readonly SCRIPT_DIR OPEN_MISTER_ROOT LOCKFILE PYTHON TOOLCHAIN_ROOT TOOLCHAIN_LOCK_PATH JOBS GPU_ROUTER HIP_ARCHITECTURES CUDA_ARCHITECTURES CACHE_HELPER CACHE_ENABLED CACHE_ROOT_REQUEST CACHE_MAX_GB_REQUEST CACHE_TOOL_KEY CACHE_TOOL_DIRECTORY CACHE_TOOL_LOCK INHERITED_TOOLCHAIN_ROOT INHERITED_TOOLCHAIN_INSTALL INHERITED_TOOLCHAIN_BUILD INHERITED_ROCM_PATH INHERITED_HIPCC INHERITED_CUDA_HOME INHERITED_CUDACXX INHERITED_CUDA_PATH

TOOLS=(yosys mistral nextpnr verilator openfpgaloader)
declare -A REPO COMMIT ORDER

lock_get() {
    FES_TOOLCHAIN_LOCKFILE="$TOOLCHAIN_LOCK_PATH" "$PYTHON" "$LOCKFILE" get "$1" "$2"
}

load_lock() {
    local tool
    # `declare -A REPO` keeps the export attribute of an inherited REPO
    # (bash 5.2).  prepare_shared_build_environment then unsets every
    # exported name outside its allowlist, which deletes that array.
    # `REPO["yosys"]=...` on a missing array is an arithmetic subscript,
    # and `set -u` exits with "yosys: unbound variable" before a fresh
    # slot can be published.  The same collision applies to COMMIT, ORDER
    # and TOOLS.  Rebind them as private globals here: a `declare` inside
    # this function would be local and would not survive the assignment.
    if [[ "$(declare -p TOOLS 2>/dev/null || true)" != declare\ -a* ]]; then
        TOOLS=(yosys mistral nextpnr verilator openfpgaloader)
    fi
    unset REPO COMMIT ORDER
    declare -gA REPO COMMIT ORDER
    for tool in "${TOOLS[@]}"; do
        REPO["$tool"]="$(lock_get "$tool" repo)"
        COMMIT["$tool"]="$(lock_get "$tool" commit)"
        ORDER["$tool"]="$(lock_get "$tool" order)"
    done
}

ordered_tools() {
    local tool
    for tool in "${TOOLS[@]}"; do
        printf '%s\t%s\n' "${ORDER[$tool]}" "$tool"
    done | sort -n -k1,1 | cut -f2-
}

print_plan() {
    local tool
    local root
    load_lock
    while IFS= read -r tool; do
        root="$TOOLCHAIN_ROOT"
        [[ "$CACHE_ENABLED" -eq 0 ]] || root="${CACHE_TOOL_DIRECTORY[$tool]}"
        printf 'tool: %s\n' "$tool"
        printf 'repo: %s\n' "${REPO[$tool]}"
        printf 'commit: %s\n' "${COMMIT[$tool]}"
        printf 'source: %s\n' "$root/src/$tool"
        printf 'build: %s\n' "$root/build/$tool"
        printf 'install: %s\n\n' "$root/install"
    done < <(ordered_tools)
    printf 'lock: %s\n' "$TOOLCHAIN_LOCK_PATH"
    printf 'gpu-router: %s\n' "$GPU_ROUTER"
    if [[ "$GPU_ROUTER" == "HIP" ]]; then
        printf 'hip-architectures: %s\n' "$HIP_ARCHITECTURES"
    elif [[ "$GPU_ROUTER" == "CUDA" ]]; then
        printf 'cuda-architectures: %s\n' "$CUDA_ARCHITECTURES"
    fi
}

run_cache_helper() {
    "$PYTHON" "$CACHE_HELPER" \
        --root "$OPEN_MISTER_ROOT" --lock "$TOOLCHAIN_LOCK_PATH" \
        --cache "$CACHE_ROOT_REQUEST" --gpu-router "$GPU_ROUTER" \
        --hip-architectures "$HIP_ARCHITECTURES" \
        --cuda-architectures "$CUDA_ARCHITECTURES" "$@"
}

shared_cache_release() {
    if [[ -n "${CACHE_LOCK_FD:-}" ]]; then
        flock -u "$CACHE_LOCK_FD" 2>/dev/null || true
        # exec makes every redirection it carries permanent, so the group
        # scopes 2>/dev/null: later diagnostics and eviction logs still show.
        { exec {CACHE_LOCK_FD}>&-; } 2>/dev/null || true
        CACHE_LOCK_FD=""
    fi
}

shared_cache_verify_hit() {
    local tool="$1"
    run_cache_helper --tool "$tool" --expect-key "${CACHE_TOOL_KEY[$tool]}" verify >/dev/null ||
        die "shared $tool install is invalid: ${CACHE_TOOL_DIRECTORY[$tool]}/ready.json"
    printf '==> shared %s install hit: %s\n' "$tool" "${CACHE_TOOL_KEY[$tool]}"
}

shared_cache_reclaim_leftovers() {
    # publish() writes ready.json and only then removes src/ and build/. A run
    # interrupted between the two leaves both trees behind, and every later run
    # takes the hit path above and returns, so nothing would ever remove them.
    # They are not in the manifest and nothing reads them, but a stranded Yosys
    # or nextpnr build tree is gigabytes against the 30 GiB floor.
    local tool="$1" directory="$2"
    [[ -d "$directory/src" || -d "$directory/build" ]] || return 0
    # The exclusive lock, so no concurrent bootstrap is building here. If it is
    # not free, leaving the trees is the safe answer; the next run retries.
    if ! flock -xn "$CACHE_LOCK_FD"; then
        printf '==> leaving %s build leftovers: the install is in use\n' "$tool"
        return 0
    fi
    printf '==> removing %s build leftovers from an interrupted publish\n' "$tool"
    # Directories only. A build tree can hold hardlinks into the published
    # install, and chmod follows the link to the shared inode, which breaks the
    # install's own closure. rmtree needs write on directories, not files.
    find "$directory/src" "$directory/build" -type d -exec chmod u+w {} + 2>/dev/null || true
    rm -rf -- "$directory/src" "$directory/build"
    flock -s "$CACHE_LOCK_FD" || die "cannot restore the shared cache lock: $directory"
}

shared_cache_tool() {
    local tool="$1"
    local key="${CACHE_TOOL_KEY[$tool]}"
    local directory="${CACHE_TOOL_DIRECTORY[$tool]}"
    local lock="${CACHE_TOOL_LOCK[$tool]}"

    # toolchain_cache.py init created the lock file. A shared lock is enough
    # to verify a ready install, and consumers hold the same shared lock, so a
    # hit never waits for a running producer. Only a build takes it
    # exclusively; a prune skips any lock it cannot take exclusively.
    [[ -f "$lock" && ! -L "$lock" ]] || die "shared cache lock path is not a regular non-symlink file: $lock"
    exec {CACHE_LOCK_FD}>>"$lock"
    flock -s "$CACHE_LOCK_FD" || die "cannot acquire shared cache lock: $lock"
    touch -c -- "$lock"
    if [[ -e "$directory/ready.json" || -L "$directory/ready.json" ]]; then
        shared_cache_verify_hit "$tool"
        shared_cache_reclaim_leftovers "$tool" "$directory"
        shared_cache_release
        return 0
    fi
    flock -x "$CACHE_LOCK_FD" || die "cannot acquire shared cache lock: $lock"
    if [[ -e "$directory/ready.json" || -L "$directory/ready.json" ]]; then
        # Another bootstrap published it while this one waited.
        shared_cache_verify_hit "$tool"
        shared_cache_reclaim_leftovers "$tool" "$directory"
        shared_cache_release
        return 0
    fi
    if [[ -e "$directory" || -L "$directory" ]]; then
        # Holding the exclusive lock with no ready.json means an earlier build
        # of this install failed. Nothing reads it, so start again.
        [[ ! -L "$directory" ]] || die "shared $tool install path is a symlink: $directory"
        printf '==> removing failed %s build: %s\n' "$tool" "$directory"
        chmod -R u+w -- "$directory"
        rm -rf -- "$directory"
    fi

    mkdir -m 700 "$directory" "$directory/src" "$directory/build" "$directory/install" "$directory/evidence"
    "$SCRIPT_DIR/bootstrap-build.py" --shared-build-tool "$tool" "$key" "$CACHE_LOCK_FD" ||
        die "shared $tool build driver failed"
    # The driver has already frozen and published the built install under the
    # held lock. Reverify the consumer closure before returning a cache hit.
    run_cache_helper --tool "$tool" --expect-key "$key" verify >/dev/null ||
        die "shared $tool freshly published install failed verification"
    # ready.json keeps the evidence digests authentication checks; the
    # checkout and the build tree are not part of the install.
    rm -rf -- "$directory/src" "$directory/build"
    printf '==> shared %s install published: %s\n' "$tool" "$key"
    shared_cache_release
}

shared_cache_build() {
    local tool
    run_cache_helper init >/dev/null || die "cannot initialize shared toolchain cache root"
    command -v flock >/dev/null 2>&1 || die "shared cache requires the flock utility"
    trap shared_cache_release EXIT
    load_lock
    while IFS= read -r tool; do
        shared_cache_tool "$tool"
    done < <(ordered_tools)
}

main() {
    case "${1:-}" in
        '')
            if [[ "$CACHE_ENABLED" -eq 1 ]]; then
                shared_cache_build
            else
                exec "$SCRIPT_DIR/bootstrap-build.sh"
            fi
            ;;
        --check-prereqs|--print-plan|-h|--help)
            exec "$SCRIPT_DIR/bootstrap-build.sh" "$@"
            ;;
        *) exec "$SCRIPT_DIR/bootstrap-build.sh" "$@" ;;
    esac
}

main "$@"
