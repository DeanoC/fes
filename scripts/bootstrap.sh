#!/usr/bin/env bash
set -euo pipefail

# Build the immutable OSS toolchain in the repository.  This script deliberately
# has no package-manager path: --check-prereqs only reports what a host needs.

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
OPEN_MISTER_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd -P)"
LOCKFILE="$OPEN_MISTER_ROOT/scripts/lockfile.py"
PYTHON="${PYTHON:-python3}"
TOOLCHAIN_ROOT="$OPEN_MISTER_ROOT/build/toolchain"
SRC_ROOT="$TOOLCHAIN_ROOT/src"
BUILD_ROOT="$TOOLCHAIN_ROOT/build"
INSTALL_ROOT="$TOOLCHAIN_ROOT/install"
JOBS="${JOBS:-$(command -v nproc >/dev/null 2>&1 && nproc || printf '2')}"

readonly SCRIPT_DIR OPEN_MISTER_ROOT LOCKFILE PYTHON TOOLCHAIN_ROOT SRC_ROOT BUILD_ROOT INSTALL_ROOT JOBS

TOOLS=(yosys mistral nextpnr verilator openfpgaloader)
declare -A REPO COMMIT ORDER

die() {
    printf 'bootstrap: %s\n' "$*" >&2
    exit 1
}

lock_get() {
    "$PYTHON" "$LOCKFILE" get "$1" "$2"
}

load_lock() {
    local tool
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
    load_lock
    while IFS= read -r tool; do
        printf 'tool: %s\n' "$tool"
        printf 'repo: %s\n' "${REPO[$tool]}"
        printf 'commit: %s\n' "${COMMIT[$tool]}"
        printf 'source: %s\n' "$SRC_ROOT/$tool"
        printf 'build: %s\n' "$BUILD_ROOT/$tool"
        printf 'install: %s\n\n' "$INSTALL_ROOT"
    done < <(ordered_tools)
}

declare -a MISSING_COMMANDS=()
declare -a MISSING_HEADERS=()

check_command() {
    local command_name="$1"
    local package_name="$2"
    if command -v "$command_name" >/dev/null 2>&1; then
        printf '  [ok]      %-18s %s\n' "$command_name" "$(command -v "$command_name")"
    else
        printf '  [missing] %-18s (%s)\n' "$command_name" "$package_name"
        MISSING_COMMANDS+=("$command_name")
    fi
}

probe_header() {
    local label="$1"
    local compiler="$2"
    local language="$3"
    local header="$4"
    local package_name=""
    if (($# >= 5)); then
        package_name="$5"
        shift 5
    else
        shift "$#"
    fi
    local -a extra_flags=("$@")
    local -a pkg_flags=()
    local -a python_flags=()

    if ! command -v "$compiler" >/dev/null 2>&1; then
        printf '  [missing] %-18s (compiler %s)\n' "$label" "$compiler"
        MISSING_HEADERS+=("$label")
        return
    fi

    if [[ -n "$package_name" ]]; then
        if ! command -v pkg-config >/dev/null 2>&1 || ! pkg-config --exists "$package_name"; then
            printf '  [missing] %-18s (development package %s)\n' "$label" "$package_name"
            MISSING_HEADERS+=("$label")
            return
        fi
        read -r -a pkg_flags <<<"$(pkg-config --cflags "$package_name" 2>/dev/null || true)"
    fi

    if [[ "$label" == "Python development headers" ]]; then
        local python_config="${PYTHON_CONFIG:-${PYTHON}-config}"
        if ! command -v "$python_config" >/dev/null 2>&1; then
            printf '  [missing] %-18s (python3-dev)\n' "$label"
            MISSING_HEADERS+=("$label")
            return
        fi
        read -r -a python_flags <<<"$($python_config --includes 2>/dev/null || true)"
    fi

    if printf '#include <%s>\nint main(void) { return 0; }\n' "$header" |
        "$compiler" "${extra_flags[@]}" "${pkg_flags[@]}" "${python_flags[@]}" \
            -x "$language" -fsyntax-only - >/dev/null 2>&1; then
        printf '  [ok]      %-18s (%s)\n' "$label" "$header"
    else
        printf '  [missing] %-18s (header <%s>)\n' "$label" "$header"
        MISSING_HEADERS+=("$label")
    fi
}

probe_eigen_cmake() {
    if ! command -v cmake >/dev/null 2>&1; then
        printf '  [missing] %-18s (cmake)\n' 'Eigen3 CMake'
        MISSING_HEADERS+=('Eigen3 CMake')
        return
    fi

    local probe_root
    probe_root="$(mktemp -d "${TMPDIR:-/tmp}/open-mister-eigen.XXXXXX")"
    printf '%s\n' \
        'cmake_minimum_required(VERSION 3.16)' \
        'project(eigen_probe LANGUAGES CXX)' \
        'find_package(Eigen3 REQUIRED NO_MODULE)' \
        'add_executable(eigen_probe main.cpp)' \
        'target_link_libraries(eigen_probe PRIVATE Eigen3::Eigen)' \
        >"$probe_root/CMakeLists.txt"
    printf '%s\n' \
        '#include <Eigen/Core>' \
        'int main() { Eigen::Vector3f value; return static_cast<int>(value.size()); }' \
        >"$probe_root/main.cpp"

    if cmake -S "$probe_root" -B "$probe_root/build" -G Ninja >/dev/null 2>&1; then
        printf '  [ok]      %-18s (find_package(Eigen3 REQUIRED NO_MODULE))\n' 'Eigen3 CMake'
        rm -rf -- "$probe_root"
        return 0
    fi
    printf '  [missing] %-18s (find_package(Eigen3 REQUIRED NO_MODULE))\n' 'Eigen3 CMake'
    MISSING_HEADERS+=('Eigen3 CMake')
    rm -rf -- "$probe_root"
    return 0
}

probe_boost_components() {
    if ! command -v cmake >/dev/null 2>&1; then
        printf '  [missing] %-18s (cmake)\n' 'Boost components'
        MISSING_HEADERS+=('Boost components')
        return
    fi

    local probe_root
    probe_root="$(mktemp -d "${TMPDIR:-/tmp}/open-mister-boost.XXXXXX")"
    printf '%s\n' \
        'cmake_minimum_required(VERSION 3.16)' \
        'project(boost_probe LANGUAGES CXX)' \
        'find_package(Boost REQUIRED COMPONENTS program_options iostreams thread)' \
        'add_executable(boost_probe main.cpp)' \
        'target_link_libraries(boost_probe PRIVATE Boost::program_options Boost::iostreams Boost::thread)' \
        >"$probe_root/CMakeLists.txt"
    printf '%s\n' \
        '#include <boost/iostreams/device/array.hpp>' \
        '#include <boost/program_options.hpp>' \
        '#include <boost/thread.hpp>' \
        'int main() {' \
        '    boost::program_options::options_description options("probe");' \
        '    boost::thread worker([]() {});' \
        '    worker.join();' \
        '    return static_cast<int>(options.options().size());' \
        '}' \
        >"$probe_root/main.cpp"

    if cmake -S "$probe_root" -B "$probe_root/build" -G Ninja >/dev/null 2>&1; then
        printf '  [ok]      %-18s (find_package(Boost REQUIRED COMPONENTS program_options iostreams thread))\n' 'Boost components'
        rm -rf -- "$probe_root"
        return 0
    fi
    printf '  [missing] %-18s (find_package(Boost REQUIRED COMPONENTS program_options iostreams thread))\n' 'Boost components'
    MISSING_HEADERS+=('Boost components')
    rm -rf -- "$probe_root"
    return 0
}

check_prereqs() {
    MISSING_COMMANDS=()
    MISSING_HEADERS=()

    printf 'Required build commands:\n'
    check_command git git
    check_command cc build-essential
    check_command c++ build-essential
    check_command cmake cmake
    check_command ninja ninja-build
    check_command make make
    check_command perl perl
    check_command "$PYTHON" python3
    check_command "${PYTHON_CONFIG:-${PYTHON}-config}" python3-dev
    check_command pkg-config pkg-config
    check_command autoconf autoconf
    check_command flex flex
    check_command bison bison
    check_command help2man help2man

    printf '\nRequired development headers:\n'
    probe_header 'Python development headers' cc c Python.h
    probe_header Boost c++ c++ boost/version.hpp
    probe_boost_components
    probe_eigen_cmake
    probe_header libffi cc c ffi.h libffi
    probe_header readline cc c readline/readline.h readline
    probe_header Tcl cc c tcl.h tcl
    probe_header zlib cc c zlib.h
    probe_header liblzma cc c lzma.h liblzma
    probe_header libusb-1.0 cc c libusb-1.0/libusb.h libusb-1.0
    probe_header libftdi1 cc c libftdi1/ftdi.h libftdi1

    if ((${#MISSING_COMMANDS[@]} == 0 && ${#MISSING_HEADERS[@]} == 0)); then
        printf '\nPrerequisites: all required commands and headers are available.\n'
        return 0
    fi

    printf '\nPrerequisites: missing capabilities detected; no package manager was run.\n'
    printf 'Debian/Ubuntu suggestion (review before running):\n'
    printf '  sudo apt-get install build-essential git cmake ninja-build python3-dev '
    printf 'libboost-dev libboost-program-options-dev libboost-iostreams-dev libboost-thread-dev '
    printf 'libeigen3-dev libffi-dev libreadline-dev tcl-dev zlib1g-dev '
    printf 'liblzma-dev libusb-1.0-0-dev libftdi1-dev pkg-config autoconf flex bison help2man perl\n'
    return 1
}

run_logged() {
    local tool="$1"
    shift
    local log_file="$BUILD_ROOT/$tool/build.log"
    mkdir -p "$BUILD_ROOT/$tool"
    {
        printf '\n$'
        printf ' %q' "$@"
        printf '\n'
    } >>"$log_file"
    "$@" 2>&1 | tee -a "$log_file"
}

checkout_pin() {
    local tool="$1"
    local source_dir="$SRC_ROOT/$tool"
    local commit="${COMMIT[$tool]}"

    mkdir -p "$SRC_ROOT"
    if [[ ! -e "$source_dir" ]]; then
        printf '==> cloning %s at %s\n' "$tool" "$commit"
        git clone --filter=blob:none --no-checkout "${REPO[$tool]}" "$source_dir"
        git -C "$source_dir" fetch --no-tags origin "$commit"
        git -C "$source_dir" checkout --detach "$commit"
    else
        [[ -d "$source_dir/.git" || -f "$source_dir/.git" ]] ||
            die "source path exists but is not a Git checkout: $source_dir"
        local dirty
        dirty="$(git -C "$source_dir" status --porcelain=v1 --untracked-files=all)"
        [[ -z "$dirty" ]] || die "source checkout is dirty; clean it manually before bootstrap: $source_dir"
        local head
        head="$(git -C "$source_dir" rev-parse HEAD)"
        [[ "$head" == "$commit" ]] ||
            die "source checkout $tool is at $head, expected locked commit $commit; move it manually"
    fi

    if [[ -f "$source_dir/.gitmodules" ]]; then
        git -C "$source_dir" submodule sync --recursive
        git -C "$source_dir" submodule update --init --recursive
    fi

    local head
    head="$(git -C "$source_dir" rev-parse HEAD)"
    [[ "$head" == "$commit" ]] || die "checkout verification failed for $tool: got $head, expected $commit"
}

identity_binary() {
    case "$1" in
        yosys) printf '%s\n' "$INSTALL_ROOT/bin/yosys" ;;
        mistral) printf '%s\n' "$INSTALL_ROOT/bin/mistral-cv" ;;
        nextpnr) printf '%s\n' "$INSTALL_ROOT/bin/nextpnr-mistral" ;;
        verilator) printf '%s\n' "$INSTALL_ROOT/bin/verilator" ;;
        openfpgaloader) printf '%s\n' "$INSTALL_ROOT/bin/openFPGALoader" ;;
        *) die "unknown tool for identity check: $1" ;;
    esac
}

identity_file() {
    printf '%s\n' "$BUILD_ROOT/$1/.identity-${COMMIT[$1]}.txt"
}

digest_file() {
    printf '%s\n' "$BUILD_ROOT/$1/.digest-${COMMIT[$1]}.sha256"
}

binary_digest() {
    local tool="$1"
    local binary
    binary="$(identity_binary "$tool")"
    command -v sha256sum >/dev/null 2>&1 || die 'sha256sum is required for tool identity authentication'
    sha256sum -- "$binary" | cut -d ' ' -f1
}

run_identity() {
    local tool="$1"
    local binary
    binary="$(identity_binary "$tool")"
    [[ -x "$binary" ]] || return 1
    case "$tool" in
        mistral) "$binary" ;;
        *) "$binary" --version ;;
    esac
}

record_identity() {
    local tool="$1"
    local output
    output="$(run_identity "$tool" 2>&1)" || return 1
    [[ -n "$output" ]] || return 1
    printf '%s\n' "$output" >"$(identity_file "$tool")"
    binary_digest "$tool" >"$(digest_file "$tool")"
}

artifact_verified() {
    local tool="$1"
    local expected actual
    local digest_path
    local output

    output="$(run_identity "$tool" 2>&1)" || return 1
    [[ -n "$output" ]] || return 1
    digest_path="$(digest_file "$tool")"
    [[ -f "$digest_path" ]] || return 1
    expected="$(tr -d '[:space:]' <"$digest_path")"
    [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || return 1
    actual="$(binary_digest "$tool")" || return 1
    [[ "$expected" == "$actual" ]]
}

build_yosys() {
    local source_dir="$SRC_ROOT/yosys"
    if [[ -f "$source_dir/Makefile" ]]; then
        run_logged yosys make -C "$source_dir" PREFIX="$INSTALL_ROOT" -j"$JOBS"
        run_logged yosys make -C "$source_dir" PREFIX="$INSTALL_ROOT" install
    else
        # The locked 2026-08-26 Yosys commit has moved to its CMake build
        # surface and ships no Makefile. Keep the upstream Makefile path for
        # older pins, but use the commit's own documented CMake flow here.
        printf 'Yosys pin has no Makefile; using its CMake build surface\n' | tee -a "$BUILD_ROOT/yosys/build.log"
        run_logged yosys cmake -S "$source_dir" -B "$BUILD_ROOT/yosys" -G Ninja \
            -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$INSTALL_ROOT" \
            -DYOSYS_ENABLE_UNIT_TESTS=OFF -DYOSYS_ENABLE_FUNCTIONAL_TESTS=OFF
        run_logged yosys ninja -C "$BUILD_ROOT/yosys" -j"$JOBS"
        run_logged yosys cmake --install "$BUILD_ROOT/yosys"
    fi
}

build_mistral() {
    local build_dir="$BUILD_ROOT/mistral"
    run_logged mistral cmake -S "$SRC_ROOT/mistral" -B "$build_dir" -G Ninja \
        -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$INSTALL_ROOT"
    run_logged mistral ninja -C "$build_dir" -j"$JOBS"
    run_logged mistral ninja -C "$build_dir" install
}

build_nextpnr() {
    local build_dir="$BUILD_ROOT/nextpnr"
    run_logged nextpnr cmake -S "$SRC_ROOT/nextpnr" -B "$build_dir" -G Ninja \
        -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$INSTALL_ROOT" \
        -DARCH=mistral -DMISTRAL_ROOT="$SRC_ROOT/mistral" -DBUILD_PYTHON=OFF \
        -DBUILD_GUI=OFF -DBUILD_TESTS=OFF -DUSE_IPO=OFF
    run_logged nextpnr ninja -C "$build_dir" nextpnr-mistral -j"$JOBS"
    run_logged nextpnr ninja -C "$build_dir" install
}

build_verilator() {
    local source_dir="$SRC_ROOT/verilator"
    (cd "$source_dir" && run_logged verilator autoconf)
    (cd "$source_dir" && run_logged verilator ./configure --prefix="$INSTALL_ROOT" --disable-tcmalloc)
    run_logged verilator make -C "$source_dir" -j"$JOBS"
    run_logged verilator make -C "$source_dir" install
}

build_openfpgaloader() {
    local build_dir="$BUILD_ROOT/openfpgaloader"
    run_logged openfpgaloader cmake -S "$SRC_ROOT/openfpgaloader" -B "$build_dir" -G Ninja \
        -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$INSTALL_ROOT"
    run_logged openfpgaloader ninja -C "$build_dir" -j"$JOBS"
    run_logged openfpgaloader ninja -C "$build_dir" install
}

build_tool() {
    local tool="$1"
    local build_dir="$BUILD_ROOT/$tool"
    local stamp="$build_dir/.built-${COMMIT[$tool]}"
    mkdir -p "$build_dir" "$INSTALL_ROOT/bin"
    checkout_pin "$tool"

    if [[ -f "$stamp" ]] && artifact_verified "$tool"; then
        printf '==> %s already built at %s (identity verified)\n' "$tool" "${COMMIT[$tool]}"
        return 0
    fi
    printf '==> building %s at %s\n' "$tool" "${COMMIT[$tool]}"
    case "$tool" in
        yosys) build_yosys ;;
        mistral) build_mistral ;;
        nextpnr) build_nextpnr ;;
        verilator) build_verilator ;;
        openfpgaloader) build_openfpgaloader ;;
        *) die "unknown tool: $tool" ;;
    esac
    record_identity "$tool" || die "installed $tool failed its version/help identity check"
    printf 'commit=%s\n' "${COMMIT[$tool]}" >"$stamp"
    printf '==> %s installed; stamp %s\n' "$tool" "$stamp"
}

build_all() {
    local tool
    load_lock
    mkdir -p "$SRC_ROOT" "$BUILD_ROOT" "$INSTALL_ROOT/bin"
    if [[ -n "${LD_LIBRARY_PATH:-}" ]]; then
        export LD_LIBRARY_PATH="$INSTALL_ROOT/lib:$LD_LIBRARY_PATH"
    else
        export LD_LIBRARY_PATH="$INSTALL_ROOT/lib"
    fi
    while IFS= read -r tool; do
        build_tool "$tool"
    done < <(ordered_tools)
}

usage() {
    cat <<'EOF'
Usage: scripts/bootstrap.sh [--check-prereqs | --print-plan]
       scripts/bootstrap.sh

Builds the five repositories pinned in toolchain.lock under build/toolchain.
--check-prereqs reports missing host capabilities without installing anything.
--print-plan prints the lock-derived plan without cloning or creating files.
EOF
}

main() {
    case "${1:-}" in
        '') build_all ;;
        --check-prereqs)
            [[ $# -eq 1 ]] || { usage >&2; return 2; }
            check_prereqs
            ;;
        --print-plan)
            [[ $# -eq 1 ]] || { usage >&2; return 2; }
            print_plan
            ;;
        -h|--help)
            [[ $# -eq 1 ]] || { usage >&2; return 2; }
            usage
            ;;
        *) usage >&2; return 2 ;;
    esac
}

main "$@"
