#!/bin/sh
set -eu

target=${1:?TARGET_DIR is required}
repo=$(CDPATH='' cd -- "$(dirname "$0")/../../.." && pwd)
fogcast=${FOGCAST_DIR:?FOGCAST_DIR is required}
native_mode=${NATIVE_RUNTIME_MODE:-package-only}
case "$native_mode" in
  package-only) : ;;
  *)
    printf '%s\n' 'native-post-build: native runtime mode must be package-only' >&2
    exit 2
    ;;
esac
launcher=$fogcast/bin/fogcast-kit-linux-armv7
lock=${NATIVE_RUNTIME_INPUT_LOCK:-$fogcast/build/native-runtime.inputs.lock.toml}
idle_input=${NATIVE_RUNTIME_IDLE_FILE:-/work/build/cache/target-image/native/idle.rbf}
extra_cores=$(CDPATH='' cd -- "$(dirname "$0")/../../.." && pwd)/scripts/native-extra-cores.sh

read_lock_value() {
  read_section=$1
  read_key=$2
  /usr/bin/awk -v wanted_section="$read_section" -v wanted_key="$read_key" '
    /^\[/ {
      section=$0
      gsub(/^\[|\]$/, "", section)
      next
    }
    section == wanted_section && $0 ~ "^[[:space:]]*" wanted_key "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
    }
  ' "$lock"
}

case "$target" in
  /*) : ;;
  *)
    printf 'native-post-build: TARGET_DIR must be absolute: %s\n' "$target" >&2
    exit 2
    ;;
esac
target=$(CDPATH='' cd -- "$target" && pwd -P)

[ -x "$target/usr/sbin/mister-runtime" ] || {
  printf '%s\n' 'native-post-build: mister-runtime is missing or not executable' >&2
  exit 1
}
[ -x "$target/usr/sbin/mister-agent" ] || {
  printf '%s\n' 'native-post-build: mister-agent is missing or not executable' >&2
  exit 1
}

[ -f "$launcher" ] && [ ! -L "$launcher" ] && [ -x "$launcher" ] || {
  printf '%s\n' 'native-post-build: run make build-fogcast-kit before image assembly' >&2
  exit 1
}
/usr/bin/install -m 0755 "$launcher" "$target/usr/sbin/fogcast-kit"

runtime_commit=$(read_lock_value mister_runtime commit)
idle_repository=$(read_lock_value idle_rbf repository)
idle_commit=$(read_lock_value idle_rbf commit)
idle_path=$(read_lock_value idle_rbf path)
idle_sha=$(read_lock_value idle_rbf sha256)
idle_size=$(read_lock_value idle_rbf size)
idle_install_path=$(read_lock_value idle_rbf install_path)

/bin/rm -f "$target/etc/init.d/S40mister-main" \
  "$target/usr/sbin/mister-disable-menu-blanking"

if [ "$native_mode" = package-only ]; then
  "$extra_cores" validate
  [ -f "$idle_input" ] && [ ! -L "$idle_input" ] || {
    printf '%s\n' 'native-post-build: idle input is not a regular non-symlink file' >&2
    exit 1
  }
  [ "$idle_install_path" = /usr/share/mister-runtime/idle.rbf ] || {
    printf '%s\n' 'native-post-build: idle install path differs from image policy' >&2
    exit 1
  }
  printf '%s\n' "$idle_sha" | grep -Eq '^[0-9a-f]{64}$' || exit 1
  printf '%s\n' "$idle_size" | grep -Eq '^[1-9][0-9]*$' || exit 1
  [ "$(/usr/bin/sha256sum "$idle_input" | /usr/bin/awk '{print $1}')" = "$idle_sha" ] || {
    printf '%s\n' 'native-post-build: idle input digest differs from the lock' >&2
    exit 1
  }
  [ "$(/usr/bin/wc -c < "$idle_input" | /usr/bin/tr -d ' ')" = "$idle_size" ] || {
    printf '%s\n' 'native-post-build: idle input size differs from the lock' >&2
    exit 1
  }
  idle_mode=$(/usr/bin/stat -c %a "$idle_input" 2>/dev/null || true)
  printf '%s\n' "$idle_mode" | grep -Eq '^[0145]{3,4}$' || {
    printf '%s\n' 'native-post-build: idle input must not be writable' >&2
    exit 1
  }

  for stale_dir in \
    "$target/usr/share/mister-runtime/cores" \
    "$target/usr/share/mister-runtime/selections"; do
    if [ -e "$stale_dir" ] || [ -L "$stale_dir" ]; then
      [ -d "$stale_dir" ] && [ ! -L "$stale_dir" ] || {
        printf 'native-post-build: stale package directory is unsafe: %s\n' "$stale_dir" >&2
        exit 1
      }
      /bin/chmod -R u+rwX "$stale_dir"
      /bin/rm -rf "$stale_dir"
    fi
  done
  /bin/mkdir -p "$target/usr/share/mister-runtime"
  /usr/bin/install -m 0644 "$idle_input" \
    "$target/usr/share/mister-runtime/idle.rbf"
  /bin/chmod 0755 "$target/etc/init.d/S40mister-runtime" \
    "$target/etc/init.d/S50mister-agent" \
    "$target/etc/init.d/S60fogcast-kit"

  # The stable FES bootstrap retains its root here after pivot_root. This must
  # exist in the immutable candidate; boot cannot create it on a read-only image.
  /bin/mkdir -p "$target/.fes-bootstrap"

  "$extra_cores" install "$(dirname "$idle_input")" "$target"
  build_inputs="$target/usr/share/mister-runtime/build-inputs"
  mister_agent_sha=$(/usr/bin/sha256sum "$target/usr/sbin/mister-agent" | /usr/bin/awk '{print $1}')
  {
    printf 'format=1\n'
    printf 'mister_runtime_commit=%s\n' "$runtime_commit"
    printf 'mister_agent_sha256=%s\n' "$mister_agent_sha"
    printf 'fogcast_kit_sha256=%s\n' "$(/usr/bin/sha256sum "$target/usr/sbin/fogcast-kit" | /usr/bin/awk '{print $1}')"
    printf 'idle_repository=%s\n' "$idle_repository"
    printf 'idle_commit=%s\n' "$idle_commit"
    printf 'idle_path=%s\n' "$idle_path"
    printf 'idle_sha256=%s\n' "$idle_sha"
    printf 'idle_size=%s\n' "$idle_size"
    printf 'idle_install_path=%s\n' "$idle_install_path"
    "$extra_cores" build-inputs "$(dirname "$idle_input")" "$target"
  } > "$build_inputs"

  expected_rbf_count=$("$extra_cores" count)
  rbf_count=$(find "$target" -iname '*.rbf' | /usr/bin/wc -l | /usr/bin/tr -d ' ')
  [ "$rbf_count" -eq "$expected_rbf_count" ] || {
    printf 'native-post-build: installed RBF count differs from package-only inputs, found %s\n' "$rbf_count" >&2
    exit 1
  }
  exit 0
fi
