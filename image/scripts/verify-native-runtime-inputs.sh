#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
native_mode=${NATIVE_RUNTIME_MODE:-package-only}

if [ "$native_mode" = package-only ]; then
  [ "$#" -eq 3 ] || {
    printf '%s\n' 'usage: verify-native-runtime-inputs.sh LOCK RUNTIME_SOURCE IDLE_FILE (package-only)' >&2
    exit 2
  }
  lock=$1
  runtime_source=$2
  idle_file=$3
  [ -f "$lock" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: lock is not a regular file' >&2
    exit 2
  }

  read_package_lock_value() {
    package_section=$1
    package_key=$2
    awk -v wanted_section="$package_section" -v wanted_key="$package_key" '
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
        exit
      }
    ' "$lock"
  }

  format=$(read_package_lock_value '' format)
  expected_commit=$(read_package_lock_value mister_runtime commit)
  mount_path=$(read_package_lock_value mister_runtime mount_path)
  repository=$(read_package_lock_value idle_rbf repository)
  idle_commit=$(read_package_lock_value idle_rbf commit)
  idle_path=$(read_package_lock_value idle_rbf path)
  expected_sha=$(read_package_lock_value idle_rbf sha256)
  expected_size=$(read_package_lock_value idle_rbf size)
  install_path=$(read_package_lock_value idle_rbf install_path)
  [ "$format" = 1 ] || {
    printf '%s\n' 'verify-native-runtime-inputs: lock format must be 1' >&2
    exit 2
  }
  [ "$repository" = https://github.com/MiSTer-devel/Distribution_MiSTer ] || {
    printf '%s\n' 'verify-native-runtime-inputs: idle repository does not match the fixed source' >&2
    exit 2
  }
  [ "$idle_commit" = f7bde4becb452ca28f604ad9802bbed5c6b58e01 ] || {
    printf '%s\n' 'verify-native-runtime-inputs: idle commit does not match the fixed source' >&2
    exit 2
  }
  [ "$idle_path" = menu.rbf ] || {
    printf '%s\n' 'verify-native-runtime-inputs: idle path does not match the fixed source' >&2
    exit 2
  }
  printf '%s\n' "$expected_commit" | grep -Eq '^[0-9a-f]{40}$' || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime commit is invalid' >&2
    exit 2
  }
  [ "$mount_path" = /runtime-source ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime mount path must be /runtime-source' >&2
    exit 2
  }
  printf '%s\n' "$expected_sha" | grep -Eq '^[0-9a-f]{64}$' || {
    printf '%s\n' 'verify-native-runtime-inputs: idle SHA-256 is invalid' >&2
    exit 2
  }
  printf '%s\n' "$expected_size" | grep -Eq '^[1-9][0-9]*$' || {
    printf '%s\n' 'verify-native-runtime-inputs: idle size is invalid' >&2
    exit 2
  }
  [ "$install_path" = /usr/share/mister-runtime/idle.rbf ] || {
    printf '%s\n' 'verify-native-runtime-inputs: idle install path must be absolute and fixed' >&2
    exit 2
  }
  case "$runtime_source" in
    /*) : ;;
    *)
      printf '%s\n' 'verify-native-runtime-inputs: runtime source must be absolute' >&2
      exit 2
      ;;
  esac
  [ -d "$runtime_source" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime source is not a directory' >&2
    exit 1
  }
  runtime_top=$(git -C "$runtime_source" rev-parse --show-toplevel 2>/dev/null) || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime source is not a Git checkout' >&2
    exit 1
  }
  runtime_top=$(CDPATH='' cd -- "$runtime_top" && pwd -P)
  actual_root=$(CDPATH='' cd -- "$runtime_source" && pwd -P)
  [ "$runtime_top" = "$actual_root" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime source is not the checkout root' >&2
    exit 1
  }
  actual_commit=$(git -C "$runtime_source" rev-parse --verify HEAD 2>/dev/null) || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime HEAD is unavailable' >&2
    exit 1
  }
  [ "$actual_commit" = "$expected_commit" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime HEAD does not match the lock' >&2
    exit 1
  }
  [ -z "$(git -C "$runtime_source" status --porcelain --untracked-files=all)" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: runtime checkout is dirty' >&2
    exit 1
  }
  [ -f "$idle_file" ] && [ ! -L "$idle_file" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: idle input is not a regular file' >&2
    exit 1
  }
  actual_sha=$(sha256sum "$idle_file" | awk '{print $1}')
  [ "$actual_sha" = "$expected_sha" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: idle SHA-256 does not match the lock' >&2
    exit 1
  }
  actual_size=$(wc -c <"$idle_file" | tr -d ' ')
  [ "$actual_size" = "$expected_size" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: idle size does not match the lock' >&2
    exit 1
  }
  idle_mode=$(stat -c %a "$idle_file" 2>/dev/null || true)
  printf '%s\n' "$idle_mode" | grep -Eq '^[0145]{3,4}$' || {
    printf '%s\n' 'verify-native-runtime-inputs: idle RBF must not be writable' >&2
    exit 1
  }
  "$repo_root/scripts/native-extra-cores.sh" verify \
    "${NATIVE_RUNTIME_CACHE:-$repo_root/build/cache/target-image/native}"
  printf 'runtime_commit=%s\nidle_sha256=%s\n' "$actual_commit" "$actual_sha"
  exit 0
fi

[ "$#" -eq 4 ] || [ "$#" -eq 5 ] || {
  printf '%s\n' 'usage: verify-native-runtime-inputs.sh LOCK RUNTIME_SOURCE IDLE_FILE MEGADRIVE_FILE [SELECTION_FILE]' >&2
  exit 2
}

lock=$1
runtime_source=$2
idle_file=$3
megadrive_file=$4
selection_file=${5:-${NATIVE_RUNTIME_MEGADRIVE_SELECTION_FILE:-}}

[ -n "$selection_file" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive selection record is required' >&2
  exit 2
}

[ -f "$lock" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: lock is not a regular file' >&2
  exit 2
}

read_lock_value() {
  read_section=$1
  read_key=$2
  awk -v wanted_section="$read_section" -v wanted_key="$read_key" '
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
  awk -v wanted="$selection_key" '
    /^[[:space:]]*(#|$)/ { next }
    $0 ~ "^[[:space:]]*" wanted "[[:space:]]*=" {
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
  ' "$selection_file"
}

validate_selection_shape() {
  selection_mode=$1
  awk '
    /^[[:space:]]*(#|$)/ { next }
    /^[[:space:]]*[A-Za-z_][A-Za-z0-9_]*[[:space:]]*=/ {
      key=$0
      sub(/^[[:space:]]*/, "", key)
      sub(/[[:space:]]*=.*$/, "", key)
      if (!(key == "format" || key == "origin" || key == "abi" ||
            key == "system" || key == "repository" || key == "revision" ||
            key == "artifact" || key == "sha256" || key == "size" ||
            key == "install_path" || key == "recipe" ||
            key == "recipe_sha256" || key == "toolchain" || key == "label")) {
        bad=1
      }
      count[key]++
      next
    }
    { bad=1 }
    END {
      for (key in count) if (count[key] != 1) bad=1
      exit bad ? 1 : 0
    }
  ' "$selection_file" || {
    printf '%s\n' 'verify-native-runtime-inputs: selection record is not a closed normalized record' >&2
    exit 2
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
  [ "$selection_format" = 1 ] || {
    printf '%s\n' 'verify-native-runtime-inputs: selection format must be 1' >&2
    exit 2
  }
  [ "$selection_origin" = "$selection_mode" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: selection origin is invalid' >&2
    exit 2
  }
  [ "$selection_abi" = mister ] && [ "$selection_system" = megadrive ] || {
    printf '%s\n' 'verify-native-runtime-inputs: selection ABI/system must be mister/megadrive' >&2
    exit 2
  }
  for value in "$selection_origin" "$selection_abi" "$selection_system" \
    "$selection_repository" "$selection_revision" "$selection_artifact" \
    "$selection_sha" "$selection_size" "$selection_install_path" \
    "$(read_selection_value recipe)" "$(read_selection_value recipe_sha256)" \
    "$(read_selection_value toolchain)" "$(read_selection_value label)"; do
    printf '%s' "$value" | LC_ALL=C grep -q '[[:cntrl:]]' && {
      printf '%s\n' 'verify-native-runtime-inputs: selection contains a control character' >&2
      exit 2
    }
  done
  [ -n "$selection_repository" ] || {
    printf '%s\n' 'verify-native-runtime-inputs: selection repository is empty' >&2
    exit 2
  }
  printf '%s\n' "$selection_repository" | grep -Eq '^https://[^[:space:]]+$' || {
    printf '%s\n' 'verify-native-runtime-inputs: selection repository is invalid' >&2
    exit 2
  }
  printf '%s\n' "$selection_revision" | grep -Eq '^[0-9a-f]{40}$' || {
    printf '%s\n' 'verify-native-runtime-inputs: selection revision is invalid' >&2
    exit 2
  }
  printf '%s\n' "$selection_sha" | grep -Eq '^[0-9a-f]{64}$' || {
    printf '%s\n' 'verify-native-runtime-inputs: selection SHA-256 is invalid' >&2
    exit 2
  }
  printf '%s\n' "$selection_size" | grep -Eq '^[1-9][0-9]*$' || {
    printf '%s\n' 'verify-native-runtime-inputs: selection size is invalid' >&2
    exit 2
  }
  [ "$selection_install_path" = /usr/share/mister-runtime/cores/megadrive.rbf ] || {
    printf '%s\n' 'verify-native-runtime-inputs: selection install path is invalid' >&2
    exit 2
  }
  if [ "$selection_mode" = source-built ]; then
    [ "$selection_artifact" = megadrive.rbf ] || {
      printf '%s\n' 'verify-native-runtime-inputs: source-built selection artifact is invalid' >&2
      exit 2
    }
    selection_recipe=$(read_selection_value recipe)
    selection_recipe_sha=$(read_selection_value recipe_sha256)
    selection_toolchain=$(read_selection_value toolchain)
    [ "$selection_recipe" = scripts/rebuild_core.py ] || {
      printf '%s\n' 'verify-native-runtime-inputs: source-built recipe is invalid' >&2
      exit 2
    }
    printf '%s\n' "$selection_recipe_sha" | grep -Eq '^[0-9a-f]{64}$' || {
      printf '%s\n' 'verify-native-runtime-inputs: source-built recipe SHA-256 is invalid' >&2
      exit 2
    }
    [ -n "$selection_toolchain" ] || {
      printf '%s\n' 'verify-native-runtime-inputs: source-built toolchain is empty' >&2
      exit 2
    }
  else
    for forbidden_key in recipe recipe_sha256 toolchain label; do
      if [ -n "$(read_selection_value "$forbidden_key")" ]; then
        printf 'verify-native-runtime-inputs: upstream selection contains source-built field: %s\n' "$forbidden_key" >&2
        exit 2
      fi
    done
    [ "$selection_repository" = "$megadrive_repository" ] || {
      printf '%s\n' 'verify-native-runtime-inputs: upstream selection repository differs from lock' >&2
      exit 2
    }
    [ "$selection_revision" = "$megadrive_commit" ] || {
      printf '%s\n' 'verify-native-runtime-inputs: upstream selection revision differs from lock' >&2
      exit 2
    }
    [ "$selection_artifact" = "$megadrive_path" ] || {
      printf '%s\n' 'verify-native-runtime-inputs: upstream selection artifact differs from lock' >&2
      exit 2
    }
    [ "$selection_sha" = "$megadrive_sha" ] && [ "$selection_size" = "$megadrive_size" ] || {
      printf '%s\n' 'verify-native-runtime-inputs: upstream selection identity differs from lock' >&2
      exit 2
    }
  fi
}

format=$(read_lock_value '' format)
expected_commit=$(read_lock_value mister_runtime commit)
mount_path=$(read_lock_value mister_runtime mount_path)
repository=$(read_lock_value idle_rbf repository)
idle_commit=$(read_lock_value idle_rbf commit)
idle_path=$(read_lock_value idle_rbf path)
expected_sha=$(read_lock_value idle_rbf sha256)
expected_size=$(read_lock_value idle_rbf size)
install_path=$(read_lock_value idle_rbf install_path)
megadrive_repository=$(read_lock_value megadrive_rbf repository)
megadrive_commit=$(read_lock_value megadrive_rbf commit)
megadrive_path=$(read_lock_value megadrive_rbf path)
megadrive_sha=$(read_lock_value megadrive_rbf sha256)
megadrive_size=$(read_lock_value megadrive_rbf size)
megadrive_install_path=$(read_lock_value megadrive_rbf install_path)

[ "$format" = 1 ] || {
  printf '%s\n' 'verify-native-runtime-inputs: lock format must be 1' >&2
  exit 2
}
printf '%s\n' "$expected_commit" | grep -Eq '^[0-9a-f]{40}$' || {
  printf '%s\n' 'verify-native-runtime-inputs: runtime commit must be 40 lowercase hexadecimal characters' >&2
  exit 2
}
[ "$mount_path" = /runtime-source ] || {
  printf '%s\n' 'verify-native-runtime-inputs: runtime mount path must be /runtime-source' >&2
  exit 2
}
[ "$repository" = https://github.com/MiSTer-devel/Distribution_MiSTer ] || {
  printf '%s\n' 'verify-native-runtime-inputs: idle repository does not match the fixed source' >&2
  exit 2
}
[ "$idle_commit" = f7bde4becb452ca28f604ad9802bbed5c6b58e01 ] || {
  printf '%s\n' 'verify-native-runtime-inputs: idle commit does not match the fixed source' >&2
  exit 2
}
[ "$idle_path" = menu.rbf ] || {
  printf '%s\n' 'verify-native-runtime-inputs: idle path does not match the fixed source' >&2
  exit 2
}
printf '%s\n' "$expected_sha" | grep -Eq '^[0-9a-f]{64}$' || {
  printf '%s\n' 'verify-native-runtime-inputs: idle SHA-256 is invalid' >&2
  exit 2
}
printf '%s\n' "$expected_size" | grep -Eq '^[1-9][0-9]*$' || {
  printf '%s\n' 'verify-native-runtime-inputs: idle size is invalid' >&2
  exit 2
}
[ "$install_path" = /usr/share/mister-runtime/idle.rbf ] || {
  printf '%s\n' 'verify-native-runtime-inputs: idle install path must be absolute and fixed' >&2
  exit 2
}
[ "$megadrive_repository" = https://github.com/MiSTer-devel/MegaDrive_MiSTer ] || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive repository does not match the fixed source' >&2
  exit 2
}
[ "$megadrive_commit" = 7365a137cfd8fa6f041e964d8b953159c0ec42d9 ] || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive commit does not match the fixed source' >&2
  exit 2
}
[ "$megadrive_path" = releases/MegaDrive_20260603.rbf ] || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive path does not match the fixed source' >&2
  exit 2
}
printf '%s\n' "$megadrive_sha" | grep -Eq '^[0-9a-f]{64}$' || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive SHA-256 is invalid' >&2
  exit 2
}
printf '%s\n' "$megadrive_size" | grep -Eq '^[1-9][0-9]*$' || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive size is invalid' >&2
  exit 2
}
[ "$megadrive_install_path" = /usr/share/mister-runtime/cores/megadrive.rbf ] || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive install path must be image-owned and fixed' >&2
  exit 2
}

[ -f "$selection_file" ] && [ ! -L "$selection_file" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: selection record is not a regular non-symlink file' >&2
  exit 2
}
selection_mode=$(stat -c %a "$selection_file" 2>/dev/null || true)
printf '%s\n' "$selection_mode" | grep -Eq '^[0145]{3,4}$' || {
  printf '%s\n' 'verify-native-runtime-inputs: selection record must not be writable' >&2
  exit 2
}
selection_origin=$(read_selection_value origin)
[ "$selection_origin" = source-built ] || [ "$selection_origin" = upstream ] || {
  printf '%s\n' 'verify-native-runtime-inputs: selection origin is invalid' >&2
  exit 2
}
validate_selection_shape "$selection_origin"

case "$runtime_source" in
  /*) : ;;
  *)
    printf '%s\n' 'verify-native-runtime-inputs: runtime source must be absolute' >&2
    exit 2
    ;;
esac
[ -d "$runtime_source" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: runtime source is not a directory' >&2
  exit 1
}
runtime_source=$(CDPATH='' cd -- "$runtime_source" && pwd -P)
runtime_top=$(git -C "$runtime_source" rev-parse --show-toplevel 2>/dev/null) || {
  printf '%s\n' 'verify-native-runtime-inputs: runtime source is not a Git checkout' >&2
  exit 1
}
runtime_top=$(CDPATH='' cd -- "$runtime_top" && pwd -P)
[ "$runtime_top" = "$runtime_source" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: runtime source is not the checkout root' >&2
  exit 1
}
actual_commit=$(git -C "$runtime_source" rev-parse --verify HEAD 2>/dev/null) || {
  printf '%s\n' 'verify-native-runtime-inputs: runtime HEAD is unavailable' >&2
  exit 1
}
[ "$actual_commit" = "$expected_commit" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: runtime HEAD does not match the lock' >&2
  exit 1
}
[ -z "$(git -C "$runtime_source" status --porcelain --untracked-files=all)" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: runtime checkout is dirty' >&2
  exit 1
}

[ -f "$idle_file" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: idle input is not a regular file' >&2
  exit 1
}
actual_sha=$(sha256sum "$idle_file" | awk '{print $1}')
[ "$actual_sha" = "$expected_sha" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: idle SHA-256 does not match the lock' >&2
  exit 1
}
actual_size=$(wc -c <"$idle_file" | tr -d ' ')
[ "$actual_size" = "$expected_size" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: idle size does not match the lock' >&2
  exit 1
}

[ -f "$megadrive_file" ] && [ ! -L "$megadrive_file" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive input is not a regular file' >&2
  exit 1
}
actual_megadrive_sha=$(sha256sum "$megadrive_file" | awk '{print $1}')
[ "$actual_megadrive_sha" = "$selection_sha" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive SHA-256 does not match the selection' >&2
  exit 1
}
actual_megadrive_size=$(wc -c <"$megadrive_file" | tr -d ' ')
[ "$actual_megadrive_size" = "$selection_size" ] || {
  printf '%s\n' 'verify-native-runtime-inputs: Mega Drive size does not match the selection' >&2
  exit 1
}

megadrive_mode=$(stat -c %a "$megadrive_file" 2>/dev/null || true)
printf '%s\n' "$megadrive_mode" | grep -Eq '^[0145]{3,4}$' || {
  printf '%s\n' 'verify-native-runtime-inputs: selected Mega Drive RBF must not be writable' >&2
  exit 1
}

printf 'runtime_commit=%s\nidle_sha256=%s\nmegadrive_origin=%s\nmegadrive_sha256=%s\n' \
  "$actual_commit" "$actual_sha" "$selection_origin" "$actual_megadrive_sha"

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
"$repo_root/scripts/native-extra-cores.sh" verify "$(dirname "$megadrive_file")"
