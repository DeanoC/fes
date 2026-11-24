#!/usr/bin/env bash
set -euo pipefail

# Install and verify the pinned TamaGo Go compiler from toolchain.lock.toml.
lockfile=${TAMAGO_LOCKFILE:-platform/embedded/tamago/toolchain.lock.toml}
raw_install=${1:?usage: ci_tamago.sh INSTALL_DIR [WORK_DIR]}
raw_work=${2:-${raw_install}.work}

install_dir=$(python3 -c 'import os, sys; print(os.path.abspath(sys.argv[1]))' "$raw_install")
work_dir=$(python3 -c 'import os, sys; print(os.path.abspath(sys.argv[1]))' "$raw_work")

read_lock_field() {
    python3 - "$lockfile" "$1" <<'PYCODE'
import re, sys
path, key = sys.argv[1], sys.argv[2]
try:
    import tomllib
    with open(path, 'rb') as stream:
        value = tomllib.load(stream)['compiler'][key]
except ModuleNotFoundError:
    section = None
    value = None
    with open(path, 'r', encoding='utf-8') as stream:
        for raw in stream:
            line = raw.split('#', 1)[0].strip()
            if not line:
                continue
            if line.startswith('[') and line.endswith(']'):
                section = line[1:-1].strip()
                continue
            if section == 'compiler':
                match = re.fullmatch(r'([A-Za-z0-9_-]+)\s*=\s*"([^"]*)"', line)
                if match and match.group(1) == key:
                    value = match.group(2)
                    break
if not isinstance(value, str) or not value.strip():
    raise SystemExit(f'invalid compiler.{key} in {path}')
print(value)
PYCODE
}

expected_version=$(read_lock_field version)
url=$(read_lock_field url)
expected_sha=$(read_lock_field sha256)
compiler_bin="$install_dir/usr/local/tamago-go/bin/go"
stamp="$install_dir/toolchain-sha256"

verify_compiler() {
    local got_version
    got_version=$(GOTOOLCHAIN=local GOENV=off "$1" version)
    printf '%s\n' "$got_version"
    case "$got_version" in
        *"$expected_version "*) return 0 ;;
        *)
            echo "TamaGo compiler version mismatch: got '$got_version', want '$expected_version'" >&2
            return 1
            ;;
    esac
}

if [[ -f "$stamp" && -x "$compiler_bin" ]]; then
    if [[ $(cat "$stamp") == "$expected_sha" ]]; then
        verify_compiler "$compiler_bin"
        exit 0
    fi
    echo "Cached TamaGo compiler digest differs from $lockfile; refreshing installation." >&2
    rm -rf "$install_dir/usr" "$stamp"
fi

rm -rf "$work_dir"
mkdir -p "$work_dir/extract" "$install_dir"
trap 'rm -rf "$work_dir"' EXIT

archive="$work_dir/tamago-go.tar.gz"
curl --fail --silent --show-error --location --retry 3 --connect-timeout 30 --max-time 300 \
    --output "$archive" "$url"

python3 - "$archive" "$expected_sha" <<'PYCODE'
import hashlib, sys
h = hashlib.sha256()
with open(sys.argv[1], 'rb') as stream:
    for chunk in iter(lambda: stream.read(1 << 20), b''):
        h.update(chunk)
got = h.hexdigest()
if got != sys.argv[2]:
    raise SystemExit(f'TamaGo archive sha256 mismatch: got {got}, want {sys.argv[2]}')
PYCODE

tar -xzf "$archive" -C "$work_dir/extract"
if [[ ! -x "$work_dir/extract/usr/local/tamago-go/bin/go" ]]; then
    echo "Extracted TamaGo archive missing usr/local/tamago-go/bin/go" >&2
    exit 1
fi
verify_compiler "$work_dir/extract/usr/local/tamago-go/bin/go"

rm -rf "$install_dir/usr" "$stamp"
mv "$work_dir/extract/usr" "$install_dir/usr"
printf '%s\n' "$expected_sha" > "$stamp"
