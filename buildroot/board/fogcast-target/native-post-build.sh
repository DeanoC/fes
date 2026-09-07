#!/bin/sh
set -eu

target=${1:?TARGET_DIR is required}
repo=$(CDPATH='' cd -- "$(dirname "$0")/../../.." && pwd)
launcher=$repo/bin/fogcast-kit-linux-armv7
lock=${NATIVE_RUNTIME_INPUT_LOCK:-/work/build/native-runtime.inputs.lock.toml}
idle_input=${NATIVE_RUNTIME_IDLE_FILE:-/work/build/cache/target-image/native/idle.rbf}
megadrive_input=${NATIVE_RUNTIME_MEGADRIVE_FILE:-/work/build/cache/target-image/native/megadrive.rbf}
selection_input=${NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE:-/work/build/cache/target-image/native/megadrive.selection.toml}

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

read_selection_value() {
  selection_key=$1
  /usr/bin/awk -v wanted_key="$selection_key" '
    /^[[:space:]]*(#|$)/ { next }
    $0 ~ "^[[:space:]]*" wanted_key "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      quote=substr(value, 1, 1)
      if ((quote == "\"" || quote == sprintf("%c", 39)) &&
          substr(value, length(value), 1) == quote) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$selection_input"
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
megadrive_repository=$(read_lock_value megadrive_rbf repository)
megadrive_commit=$(read_lock_value megadrive_rbf commit)
megadrive_path=$(read_lock_value megadrive_rbf path)
megadrive_sha=$(read_lock_value megadrive_rbf sha256)
megadrive_size=$(read_lock_value megadrive_rbf size)
megadrive_install_path=$(read_lock_value megadrive_rbf install_path)

[ -f "$selection_input" ] && [ ! -L "$selection_input" ] || {
  printf '%s\n' 'native-post-build: Mega Drive selection is not a regular non-symlink file' >&2
  exit 1
}
/usr/bin/awk '
  /^[[:space:]]*(#|$)/ { next }
  /^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=/ {
    key=$0
    sub(/^[[:space:]]*/, "", key)
    sub(/[[:space:]]*=.*$/, "", key)
    if (!(key == "format" || key == "origin" || key == "abi" ||
          key == "system" || key == "repository" || key == "revision" ||
          key == "artifact" || key == "sha256" || key == "size" ||
          key == "install_path" || key == "recipe" ||
          key == "recipe_sha256" || key == "toolchain" || key == "label")) bad=1
    count[key]++
    next
  }
  { bad=1 }
  END { for (key in count) if (count[key] != 1) bad=1; exit bad ? 1 : 0 }
' "$selection_input" || {
  printf '%s\n' 'native-post-build: Mega Drive selection is not a closed normalized record' >&2
  exit 1
}
selection_mode=$(stat -c %a "$selection_input" 2>/dev/null || true)
printf '%s\n' "$selection_mode" | grep -Eq '^[0145]{3,4}$' || {
  printf '%s\n' 'native-post-build: Mega Drive selection must not be writable' >&2
  exit 1
}
selection_format=$(read_selection_value format)
selection_origin=$(read_selection_value origin)
selection_abi=$(read_selection_value abi)
selection_system=$(read_selection_value system)
selection_repository=$(read_selection_value repository)
selection_revision=$(read_selection_value revision)
selection_artifact=$(read_selection_value artifact)
selection_sha=$(read_selection_value sha256)
selection_size=$(read_selection_value size)
selection_install_path=$(read_selection_value install_path)
selection_recipe=$(read_selection_value recipe)
selection_recipe_sha=$(read_selection_value recipe_sha256)
selection_toolchain=$(read_selection_value toolchain)
selection_label=$(read_selection_value label)
if [ "$selection_format" != 1 ] ||
  { [ "$selection_origin" != source-built ] && [ "$selection_origin" != upstream ]; } ||
  [ "$selection_abi" != mister ] || [ "$selection_system" != megadrive ]; then
  printf '%s\n' 'native-post-build: Mega Drive selection identity is invalid' >&2
  exit 1
fi
printf '%s\n' "$selection_revision" | grep -Eq '^[0-9a-f]{40}$' || {
  printf '%s\n' 'native-post-build: Mega Drive selection revision is invalid' >&2
  exit 1
}
[ -n "$selection_repository" ] &&
  printf '%s\n' "$selection_repository" | grep -Eq '^https://[^[:space:]]+$' || {
  printf '%s\n' 'native-post-build: Mega Drive selection repository is invalid' >&2
  exit 1
}
for selection_value in "$selection_origin" "$selection_abi" "$selection_system" \
  "$selection_repository" "$selection_revision" "$selection_artifact" \
  "$selection_sha" "$selection_size" "$selection_install_path" \
  "$selection_recipe" "$selection_recipe_sha" "$selection_toolchain" "$selection_label"; do
  printf '%s' "$selection_value" | LC_ALL=C grep -q '[[:cntrl:]]' && {
    printf '%s\n' 'native-post-build: Mega Drive selection contains a control character' >&2
    exit 1
  }
done
printf '%s\n' "$selection_sha" | grep -Eq '^[0-9a-f]{64}$' || {
  printf '%s\n' 'native-post-build: Mega Drive selection SHA-256 is invalid' >&2
  exit 1
}
printf '%s\n' "$selection_size" | grep -Eq '^[1-9][0-9]*$' || {
  printf '%s\n' 'native-post-build: Mega Drive selection size is invalid' >&2
  exit 1
}
[ "$selection_install_path" = /usr/share/mister-runtime/cores/megadrive.rbf ] || {
  printf '%s\n' 'native-post-build: Mega Drive selection install path is invalid' >&2
  exit 1
}
case "$selection_origin" in
  source-built)
    [ "$selection_artifact" = megadrive.rbf ] || exit 1
    [ "$selection_recipe" = scripts/rebuild_core.py ] || exit 1
    printf '%s\n' "$selection_recipe_sha" | grep -Eq '^[0-9a-f]{64}$' || exit 1
    [ -n "$selection_toolchain" ] || exit 1
    ;;
  upstream)
    [ "$selection_repository" = "$megadrive_repository" ] &&
      [ "$selection_revision" = "$megadrive_commit" ] &&
      [ "$selection_artifact" = "$megadrive_path" ] &&
      [ "$selection_sha" = "$megadrive_sha" ] &&
      [ "$selection_size" = "$megadrive_size" ] || {
      printf '%s\n' 'native-post-build: upstream selection differs from the locked release' >&2
      exit 1
    }
    [ -z "$selection_recipe" ] &&
      [ -z "$selection_recipe_sha" ] &&
      [ -z "$selection_toolchain" ] &&
      [ -z "$selection_label" ] || {
      printf '%s\n' 'native-post-build: upstream selection contains source-built provenance' >&2
      exit 1
    }
    ;;
esac

[ "$idle_install_path" = /usr/share/mister-runtime/idle.rbf ] || {
  printf '%s\n' 'native-post-build: idle install path differs from image policy' >&2
  exit 1
}
[ "$megadrive_install_path" = /usr/share/mister-runtime/cores/megadrive.rbf ] || {
  printf '%s\n' 'native-post-build: Mega Drive install path differs from image policy' >&2
  exit 1
}
[ "$(/usr/bin/sha256sum "$idle_input" | /usr/bin/awk '{print $1}')" = "$idle_sha" ] || {
  printf '%s\n' 'native-post-build: idle input digest differs from the lock' >&2
  exit 1
}
[ "$(/usr/bin/wc -c < "$idle_input" | /usr/bin/tr -d ' ')" = "$idle_size" ] || {
  printf '%s\n' 'native-post-build: idle input size differs from the lock' >&2
  exit 1
}
[ "$(/usr/bin/sha256sum "$megadrive_input" | /usr/bin/awk '{print $1}')" = "$selection_sha" ] || {
  printf '%s\n' 'native-post-build: Mega Drive input digest differs from the selection' >&2
  exit 1
}
[ "$(/usr/bin/wc -c < "$megadrive_input" | /usr/bin/tr -d ' ')" = "$selection_size" ] || {
  printf '%s\n' 'native-post-build: Mega Drive input size differs from the selection' >&2
  exit 1
}

/bin/rm -f "$target/etc/init.d/S40mister-main" \
  "$target/usr/sbin/mister-disable-menu-blanking"
/bin/mkdir -p "$target/usr/share/mister-runtime"
/usr/bin/install -m 0644 \
  "$idle_input" \
  "$target/usr/share/mister-runtime/idle.rbf"
/bin/mkdir -p "$target/usr/share/mister-runtime/cores"
/usr/bin/install -m 0644 \
  "$megadrive_input" \
  "$target/usr/share/mister-runtime/cores/megadrive.rbf"

installed_idle=$target/usr/share/mister-runtime/idle.rbf
installed_megadrive=$target/usr/share/mister-runtime/cores/megadrive.rbf
[ -f "$installed_idle" ] && [ ! -L "$installed_idle" ] || {
  printf '%s\n' 'native-post-build: installed idle RBF must be a regular non-symlink file' >&2
  exit 1
}
[ -f "$installed_megadrive" ] && [ ! -L "$installed_megadrive" ] || {
  printf '%s\n' 'native-post-build: installed Mega Drive RBF must be a regular non-symlink file' >&2
  exit 1
}
[ "$(/usr/bin/sha256sum "$installed_idle" | /usr/bin/awk '{print $1}')" = "$idle_sha" ]
[ "$(/usr/bin/wc -c < "$installed_idle" | /usr/bin/tr -d ' ')" = "$idle_size" ]
[ "$(/usr/bin/sha256sum "$installed_megadrive" | /usr/bin/awk '{print $1}')" = "$selection_sha" ]
[ "$(/usr/bin/wc -c < "$installed_megadrive" | /usr/bin/tr -d ' ')" = "$selection_size" ]
/bin/chmod 0755 "$target/etc/init.d/S40mister-runtime" \
  "$target/etc/init.d/S50mister-agent" \
  "$target/etc/init.d/S60fogcast-kit"

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
  printf 'megadrive_origin=%s\n' "$selection_origin"
  printf 'megadrive_abi=%s\n' "$selection_abi"
  printf 'megadrive_system=%s\n' "$selection_system"
  printf 'megadrive_repository=%s\n' "$selection_repository"
  printf 'megadrive_revision=%s\n' "$selection_revision"
  printf 'megadrive_artifact=%s\n' "$selection_artifact"
  printf 'megadrive_sha256=%s\n' "$selection_sha"
  printf 'megadrive_size=%s\n' "$selection_size"
  printf 'megadrive_install_path=%s\n' "$selection_install_path"
  if [ "$selection_origin" = source-built ]; then
    printf 'megadrive_recipe=%s\n' "$selection_recipe"
    printf 'megadrive_recipe_sha256=%s\n' "$selection_recipe_sha"
    printf 'megadrive_toolchain=%s\n' "$selection_toolchain"
    [ -z "$selection_label" ] || printf 'megadrive_label=%s\n' "$selection_label"
  fi
} > "$build_inputs"

extra_cores=$(CDPATH='' cd -- "$(dirname "$0")/../../.." && pwd)/scripts/native-extra-cores.sh
"$extra_cores" install "$(dirname "$megadrive_input")" "$target"
expected_rbf_count=$("$extra_cores" count)
rbf_count=$(find "$target" -iname '*.rbf' | /usr/bin/wc -l | /usr/bin/tr -d ' ')
[ "$rbf_count" -eq "$expected_rbf_count" ] || {
  printf 'native-post-build: installed RBF count differs from selected systems, found %s\n' "$rbf_count" >&2
  exit 1
}
