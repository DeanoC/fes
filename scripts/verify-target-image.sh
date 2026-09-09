#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
cleanup_manifest_tmp=
cleanup_library_tmp=
cleanup_inspect_root=
cleanup_native_inputs_tmp=
canonical_native_input_lock=$repo/build/native-runtime.inputs.lock.toml
native_input_lock=$canonical_native_input_lock
native_selection_file=$repo/build/cache/target-image/native/megadrive.selection.toml

cleanup() {
  [ -z "$cleanup_manifest_tmp" ] || /bin/rm -f "$cleanup_manifest_tmp"
  [ -z "$cleanup_library_tmp" ] || /bin/rm -f "$cleanup_library_tmp"
  [ -z "$cleanup_inspect_root" ] || /bin/rm -rf "$cleanup_inspect_root"
  [ -z "$cleanup_native_inputs_tmp" ] || /bin/rm -f "$cleanup_native_inputs_tmp"
}
trap cleanup EXIT INT TERM

required_libraries() {
  printf '%s\n' \
    /lib/ld-linux-armhf.so.3 \
    /lib/libImlib2.so.1 \
    /lib/libbluetooth.so.3 \
    /lib/libbz2.so.1.0 \
    /lib/libc.so.6 \
    /lib/libdl.so.2 \
    /lib/libfreetype.so.6 \
    /lib/libgcc_s.so.1 \
    /lib/libm.so.6 \
    /lib/libpng16.so.16 \
    /lib/libpthread.so.0 \
    /lib/librt.so.1 \
    /lib/libstdc++.so.6 \
    /lib/libz.so.1
}

usage() {
  printf 'usage: verify-target-image.sh prod|dev|native-dev IMAGE MANIFEST LIBRARY_REPORT [SELECTION] | --inside VARIANT IMAGE MANIFEST LIBRARY_REPORT [SELECTION] | --root-fixture VARIANT ROOT MANIFEST LIBRARY_REPORT [SELECTION]\n' >&2
  exit 2
}

validate_variant() {
  case "$1" in
    prod|dev|native-dev) : ;;
    *) usage ;;
  esac
}

reject_native_input_lock_override() {
  [ -z "${TARGET_IMAGE_ROOT_FIXTURE_NATIVE_INPUT_LOCK:-}" ] || {
    printf '%s\n' 'verify-target-image: native input lock override is only permitted with --root-fixture' >&2
    exit 2
  }
}

read_native_lock_value() {
  lock_section=$1
  lock_key=$2
  awk -v wanted_section="$lock_section" -v wanted_key="$lock_key" '
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
  ' "$native_input_lock"
}

read_native_selection_value() {
  selection_key=$1
  awk -v wanted_key="$selection_key" '
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
  ' "$native_selection_file"
}

verify_root() {
  variant=$1
  root=$2
  manifest=$3
  library_report=$4
  selection_file=${5:-$native_selection_file}
  native_selection_file=$selection_file
  validate_variant "$variant"
  root=$(CDPATH='' cd -- "$root" && pwd -P)

  if [ "$variant" = native-dev ]; then
    required_paths='/sbin/init
/usr/bin/busybox
/usr/bin/readlink
/usr/sbin/mister-runtime
/usr/sbin/fogcast-kit
/usr/sbin/mister-agent
/usr/share/mister-runtime/idle.rbf
/usr/share/mister-runtime/cores/megadrive.rbf
/usr/share/mister-runtime/build-inputs
/etc/init.d/S20mister-network
/etc/init.d/S40mister-runtime
/etc/init.d/S49fogcast-target-smoke
/etc/init.d/S50mister-agent
/etc/init.d/S60fogcast-kit'
  else
    required_paths='/sbin/init
/usr/bin/busybox
/usr/sbin/mister-agent
/usr/sbin/mister-disable-menu-blanking
/etc/init.d/S20mister-network
/etc/init.d/S40mister-main
/etc/init.d/S49fogcast-target-smoke
/etc/init.d/S50mister-agent'
  fi
  printf '%s\n' "$required_paths" | while IFS= read -r required; do
    [ -n "$required" ] || continue
    [ -e "$root$required" ] || {
      printf 'verify-target-image: missing required path: %s\n' "$required" >&2
      exit 1
    }
  done

  library_count=0
  while IFS= read -r library; do
    library_count=$((library_count + 1))
    [ -e "$root$library" ] || {
      printf 'verify-target-image: missing required library: %s\n' "$library" >&2
      exit 1
    }
    resolved=$(readlink -f "$root$library")
    case "$resolved" in
      "$root"/*) : ;;
      *)
        printf 'verify-target-image: library escapes image: %s\n' "$library" >&2
        exit 1
        ;;
    esac
done <<EOF
$(required_libraries)
EOF
  test "$library_count" -eq 14

  fstab=$root/etc/fstab
  grep -Eq '^/dev/root[[:space:]]+/[[:space:]]+ext4[[:space:]]+ro([,[:space:]]|$)' "$fstab" || {
    printf '%s\n' 'verify-target-image: root filesystem is not declared read-only' >&2
    exit 1
  }
  for volatile_mount in /run /tmp /var/log; do
    test -d "$root$volatile_mount" && test ! -L "$root$volatile_mount" || {
      printf 'verify-target-image: %s is not a real mount-point directory\n' "$volatile_mount" >&2
      exit 1
    }
    grep -Eq "^[^#]+[[:space:]]+$volatile_mount[[:space:]]+tmpfs[[:space:]]" "$fstab" || {
      printf 'verify-target-image: %s is not tmpfs\n' "$volatile_mount" >&2
      exit 1
    }
  done

  if [ "$variant" = native-dev ]; then
    [ ! -e "$root/etc/init.d/S40mister-main" ] || {
      printf '%s\n' 'verify-target-image: native image contains the Main init service' >&2
      exit 1
    }
    [ ! -e "$root/usr/sbin/mister-disable-menu-blanking" ] || {
      printf '%s\n' 'verify-target-image: native image contains the legacy Menu configuration helper' >&2
      exit 1
    }
    [ -x "$root/usr/sbin/fogcast-kit" ] && [ -x "$root/etc/init.d/S60fogcast-kit" ] || {
      printf '%s\n' 'verify-target-image: launcher binary and init must be executable' >&2
      exit 1
    }
    cmp "$root/etc/init.d/S60fogcast-kit" \
      "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S60fogcast-kit" || {
      printf '%s\n' 'verify-target-image: launcher init differs from selected service' >&2
      exit 1
    }
    native_runtime_service=$root/etc/init.d/S40mister-runtime
    native_agent_service=$root/etc/init.d/S50mister-agent
    "$repo/scripts/validate-native-init-services.sh" \
      "$native_runtime_service" "$native_agent_service"
    if grep -Eq '/dev/MiSTer_cmd|CORENAME|/media/fat/MiSTer|agent_binary=|killall|pidof|pgrep|/proc/' \
      "$native_agent_service"; then
      printf '%s\n' 'verify-target-image: native agent service depends on legacy Main state' >&2
      exit 1
    fi
    if grep -ERq '/dev/MiSTer_cmd|/tmp/CORENAME|/media/fat/MiSTer|load_core[[:space:]].*\.mgl' \
      "$root/etc/init.d"; then
      printf '%s\n' 'verify-target-image: native init contains conventional Main, MGL, or FIFO startup wiring' >&2
      exit 1
    fi

    [ -f "$selection_file" ] && [ ! -L "$selection_file" ] || {
      printf '%s\n' 'verify-target-image: Mega Drive selection is not a regular non-symlink file' >&2
      exit 1
    }
    selection_mode=$(stat -c %a "$selection_file" 2>/dev/null || true)
    printf '%s\n' "$selection_mode" | grep -Eq '^[0145]{3,4}$' || {
      printf '%s\n' 'verify-target-image: Mega Drive selection must not be writable' >&2
      exit 1
    }
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
              key == "recipe_sha256" || key == "toolchain" || key == "label")) bad=1
        count[key]++
        next
      }
      { bad=1 }
      END { for (key in count) if (count[key] != 1) bad=1; exit bad ? 1 : 0 }
    ' "$selection_file" || {
      printf '%s\n' 'verify-target-image: Mega Drive selection is not a closed normalized record' >&2
      exit 1
    }
    selection_format=$(read_native_selection_value format)
    selection_origin=$(read_native_selection_value origin)
    selection_abi=$(read_native_selection_value abi)
    selection_system=$(read_native_selection_value system)
    selection_repository=$(read_native_selection_value repository)
    selection_revision=$(read_native_selection_value revision)
    selection_artifact=$(read_native_selection_value artifact)
    selection_sha=$(read_native_selection_value sha256)
    selection_size=$(read_native_selection_value size)
    selection_install_path=$(read_native_selection_value install_path)
    selection_recipe=$(read_native_selection_value recipe)
    selection_recipe_sha=$(read_native_selection_value recipe_sha256)
    selection_toolchain=$(read_native_selection_value toolchain)
    selection_label=$(read_native_selection_value label)
    [ "$selection_format" = 1 ] || exit 1
    case "$selection_origin" in source-built|upstream) : ;; *) exit 1 ;; esac
    [ "$selection_abi" = mister ] && [ "$selection_system" = megadrive ] || exit 1
    printf '%s\n' "$selection_revision" | grep -Eq '^[0-9a-f]{40}$' || exit 1
    printf '%s\n' "$selection_sha" | grep -Eq '^[0-9a-f]{64}$' || exit 1
    printf '%s\n' "$selection_size" | grep -Eq '^[1-9][0-9]*$' || exit 1
    printf '%s\n' "$selection_repository" | grep -Eq '^https://[^[:space:]]+$' || exit 1
    [ "$selection_install_path" = /usr/share/mister-runtime/cores/megadrive.rbf ] || exit 1
    for selection_value in "$selection_origin" "$selection_abi" "$selection_system" \
      "$selection_repository" "$selection_revision" "$selection_artifact" \
      "$selection_sha" "$selection_size" "$selection_install_path" \
      "$selection_recipe" "$selection_recipe_sha" "$selection_toolchain" "$selection_label"; do
      printf '%s' "$selection_value" | LC_ALL=C grep -q '[[:cntrl:]]' && exit 1
    done
    case "$selection_origin" in
      source-built)
        [ "$selection_artifact" = megadrive.rbf ] || exit 1
        [ "$selection_recipe" = scripts/rebuild_core.py ] || exit 1
        printf '%s\n' "$selection_recipe_sha" | grep -Eq '^[0-9a-f]{64}$' || exit 1
        [ -n "$selection_toolchain" ] || exit 1
        ;;
      upstream)
        [ "$selection_repository" = "$(read_native_lock_value megadrive_rbf repository)" ] &&
          [ "$selection_revision" = "$(read_native_lock_value megadrive_rbf commit)" ] &&
          [ "$selection_artifact" = "$(read_native_lock_value megadrive_rbf path)" ] &&
          [ "$selection_sha" = "$(read_native_lock_value megadrive_rbf sha256)" ] &&
          [ "$selection_size" = "$(read_native_lock_value megadrive_rbf size)" ] || exit 1
        [ -z "$selection_recipe" ] &&
          [ -z "$selection_recipe_sha" ] &&
          [ -z "$selection_toolchain" ] && [ -z "$selection_label" ] || exit 1
        ;;
    esac

    idle=$root/usr/share/mister-runtime/idle.rbf
    expected_idle_sha=$(read_native_lock_value idle_rbf sha256)
    expected_idle_size=$(read_native_lock_value idle_rbf size)
    [ -f "$idle" ] && [ ! -L "$idle" ] || {
      printf '%s\n' 'verify-target-image: native idle RBF must be a regular non-symlink file' >&2
      exit 1
    }
    [ "$(sha256sum "$idle" | awk '{print $1}')" = "$expected_idle_sha" ] || {
      printf '%s\n' 'verify-target-image: native idle RBF digest differs from the lock' >&2
      exit 1
    }
    [ "$(wc -c < "$idle" | tr -d ' ')" = "$expected_idle_size" ] || {
      printf '%s\n' 'verify-target-image: native idle RBF size differs from the lock' >&2
      exit 1
    }
    megadrive=$root/usr/share/mister-runtime/cores/megadrive.rbf
    expected_megadrive_sha=$selection_sha
    expected_megadrive_size=$selection_size
    [ -f "$megadrive" ] && [ ! -L "$megadrive" ] || {
      printf '%s\n' 'verify-target-image: native Mega Drive RBF must be a regular non-symlink file' >&2
      exit 1
    }
    [ "$(sha256sum "$megadrive" | awk '{print $1}')" = "$expected_megadrive_sha" ] || {
      printf '%s\n' 'verify-target-image: native Mega Drive RBF digest differs from the lock' >&2
      exit 1
    }
    [ "$(wc -c < "$megadrive" | tr -d ' ')" = "$expected_megadrive_size" ] || {
      printf '%s\n' 'verify-target-image: native Mega Drive RBF size differs from the lock' >&2
      exit 1
    }
    "$repo/scripts/native-extra-cores.sh" verify-image "$(dirname "$native_selection_file")" "$root"
    expected_rbf_count=$("$repo/scripts/native-extra-cores.sh" count)
    rbf_count=$(find "$root" -iname '*.rbf' | wc -l | tr -d ' ')
    [ "$rbf_count" -eq "$expected_rbf_count" ] || {
      printf 'verify-target-image: native image RBF count differs from selected systems, found %s\n' "$rbf_count" >&2
      exit 1
    }

    expected_inputs=$(mktemp "${TMPDIR:-/tmp}/fogcast-native-build-inputs.XXXXXX")
    cleanup_native_inputs_tmp=$expected_inputs
    {
      printf 'format=1\n'
      printf 'mister_runtime_commit=%s\n' "$(read_native_lock_value mister_runtime commit)"
      printf 'mister_agent_sha256=%s\n' "$(sha256sum "$root/usr/sbin/mister-agent" | awk '{print $1}')"
      printf 'fogcast_kit_sha256=%s\n' "$(sha256sum "$root/usr/sbin/fogcast-kit" | awk '{print $1}')"
      printf 'idle_repository=%s\n' "$(read_native_lock_value idle_rbf repository)"
      printf 'idle_commit=%s\n' "$(read_native_lock_value idle_rbf commit)"
      printf 'idle_path=%s\n' "$(read_native_lock_value idle_rbf path)"
      printf 'idle_sha256=%s\n' "$expected_idle_sha"
      printf 'idle_size=%s\n' "$expected_idle_size"
      printf 'idle_install_path=%s\n' "$(read_native_lock_value idle_rbf install_path)"
      printf 'megadrive_origin=%s\n' "$selection_origin"
      printf 'megadrive_abi=%s\n' "$selection_abi"
      printf 'megadrive_system=%s\n' "$selection_system"
      printf 'megadrive_repository=%s\n' "$selection_repository"
      printf 'megadrive_revision=%s\n' "$selection_revision"
      printf 'megadrive_artifact=%s\n' "$selection_artifact"
      printf 'megadrive_sha256=%s\n' "$expected_megadrive_sha"
      printf 'megadrive_size=%s\n' "$expected_megadrive_size"
      printf 'megadrive_install_path=%s\n' "$selection_install_path"
      if [ "$selection_origin" = source-built ]; then
        printf 'megadrive_recipe=%s\n' "$selection_recipe"
        printf 'megadrive_recipe_sha256=%s\n' "$selection_recipe_sha"
        printf 'megadrive_toolchain=%s\n' "$selection_toolchain"
        selection_label=$(read_native_selection_value label)
        [ -z "$selection_label" ] || printf 'megadrive_label=%s\n' "$selection_label"
      fi
      "$repo/scripts/native-extra-cores.sh" build-inputs "$(dirname "$native_selection_file")" "$root"
    } > "$expected_inputs"
    cmp "$expected_inputs" "$root/usr/share/mister-runtime/build-inputs" >/dev/null 2>&1 || {
      printf '%s\n' 'verify-target-image: native build-input record differs from the selection' >&2
      exit 1
    }
    /bin/rm -f "$expected_inputs"
    cleanup_native_inputs_tmp=
  fi

  server_resolved=$(find "$root" \( -type f -o -type l \) \
    \( -name dropbear -o -name dropbearmulti -o -name sshd \) \
    -exec sh -c '
      for candidate do
        [ -x "$candidate" ] || continue
        readlink -f "$candidate"
      done
    ' sh {} + | LC_ALL=C sort -u)
  if [ -n "$server_resolved" ]; then
    server_count=$(printf '%s\n' "$server_resolved" | wc -l | tr -d ' ')
  else
    server_count=0
  fi
  while IFS= read -r server; do
    [ -z "$server" ] && continue
    case "$server" in
      "$root"/*) : ;;
      *)
        printf 'verify-target-image: SSH server escapes image: %s\n' "$server" >&2
        exit 1
        ;;
    esac
  done <<EOF
$server_resolved
EOF
  if [ "$variant" = prod ]; then
    test "$server_count" -eq 0 || {
      printf '%s\n' 'verify-target-image: production contains an SSH server' >&2
      exit 1
    }
  else
    test "$server_count" -eq 1 && test -x "$root/usr/sbin/dropbear" || {
      printf '%s\n' 'verify-target-image: development must contain exactly one Dropbear server' >&2
      exit 1
    }
  fi

  if [ "$variant" = native-dev ]; then
    if find "$root" -type f \( \
      -iname '*.rom' -o -iname '*.sfc' -o -iname '*.smc' -o \
      -iname '*.md' -o -iname '*.gen' -o -iname '*.zip' -o \
      -iname '*.bin' -o -iname '*.mgl' -o -iname '*.map' -o -name 'agent.toml' \
      -o -name '*-gdb.py' \
    \) -print -quit | grep -q .; then
      printf '%s\n' 'verify-target-image: forbidden game, runtime, or debug payload found' >&2
      exit 1
    fi
  else
    if find "$root" -type f \( \
      -iname '*.rom' -o -iname '*.sfc' -o -iname '*.smc' -o \
      -iname '*.md' -o -iname '*.gen' -o -iname '*.zip' -o \
      -iname '*.bin' -o -iname '*.rbf' -o -iname '*.map' -o -name 'agent.toml' \
      -o -name '*-gdb.py' \
    \) -print -quit | grep -q .; then
      printf '%s\n' 'verify-target-image: forbidden game, runtime, or debug payload found' >&2
      exit 1
    fi
  fi
  "$repo/scripts/scan-target-image-secrets.sh" "$root"
  for forbidden_tool in \
    /usr/bin/cc /usr/bin/gcc /usr/bin/g++ /usr/bin/c++ /usr/bin/make \
    /usr/bin/apk /usr/bin/dpkg /usr/bin/opkg /usr/bin/rpm; do
    [ ! -e "$root$forbidden_tool" ] || {
      printf 'verify-target-image: forbidden target tool found: %s\n' "$forbidden_tool" >&2
      exit 1
    }
  done

  agent=$root/usr/sbin/mister-agent
  agent_type=$(file "$agent")
  printf '%s\n' "$agent_type" | grep -Eq 'ELF 32-bit.*ARM.*EABI5'
  printf '%s\n' "$agent_type" | grep -Fq 'statically linked'
  printf '%s\n' "$agent_type" | grep -Fq 'stripped'
  agent_header=$(readelf -h "$agent")
  printf '%s\n' "$agent_header" | grep -Eq 'Class:[[:space:]]+ELF32'
  printf '%s\n' "$agent_header" | grep -Eq 'Machine:[[:space:]]+ARM'
  if strings -a "$agent" | grep -Eiq '(^|[[:space:]])(token|secret|bearer|api[_-]?key)[[:space:]]*(=|:)'; then
    printf '%s\n' 'verify-target-image: agent binary contains a token assignment' >&2
    exit 1
  fi

  if [ "$variant" = native-dev ]; then
    launcher_type=$(file "$root/usr/sbin/fogcast-kit")
    printf '%s\n' "$launcher_type" | grep -Eq 'ELF 32-bit.*ARM.*EABI5'
    printf '%s\n' "$launcher_type" | grep -Fq 'statically linked'
    printf '%s\n' "$launcher_type" | grep -Fq 'stripped'
    launcher_header=$(readelf -h "$root/usr/sbin/fogcast-kit")
    printf '%s\n' "$launcher_header" | grep -Eq 'Class:[[:space:]]+ELF32'
    printf '%s\n' "$launcher_header" | grep -Eq 'Machine:[[:space:]]+ARM'
    runtime=$root/usr/sbin/mister-runtime
    runtime_type=$(file "$runtime")
    printf '%s\n' "$runtime_type" | grep -Eq 'ELF 32-bit.*ARM.*EABI5'
    runtime_header=$(readelf -h "$runtime")
    printf '%s\n' "$runtime_header" | grep -Eq 'Class:[[:space:]]+ELF32'
    printf '%s\n' "$runtime_header" | grep -Eq 'Machine:[[:space:]]+ARM'
    runtime_needed=$(readelf -d "$runtime" | awk '
      /\(NEEDED\)/ {
        value=$0
        sub(/^.*\[/, "", value)
        sub(/\].*$/, "", value)
        print value
      }
    ')
    [ -n "$runtime_needed" ] || {
      printf '%s\n' 'verify-target-image: native runtime has no inspectable NEEDED closure' >&2
      exit 1
    }
    printf '%s\n' "$runtime_needed" | while IFS= read -r needed; do
      case "$needed" in
        ''|*/*)
          printf 'verify-target-image: invalid runtime NEEDED entry: %s\n' "$needed" >&2
          exit 1
          ;;
      esac
      required_path=$(required_libraries | awk -F/ -v wanted="$needed" '$NF == wanted { print; exit }')
      [ -n "$required_path" ] && [ -e "$root$required_path" ] || {
        printf 'verify-target-image: runtime NEEDED library is absent from the verified closure: %s\n' "$needed" >&2
        exit 1
      }
    done
  fi

  /bin/mkdir -p "$(dirname "$manifest")" "$(dirname "$library_report")"
  manifest_tmp=$manifest.new.$$
  library_tmp=$library_report.new.$$
  cleanup_manifest_tmp=$manifest_tmp
  cleanup_library_tmp=$library_tmp
  (
    while IFS= read -r library; do
      resolved=$(readlink -f "$root$library")
      resolved_path=${resolved#"$root"}
      digest=$(sha256sum "$resolved" | awk '{print $1}')
      printf '%s\t%s\t%s\n' "$library" "$resolved_path" "$digest"
    done <<EOF
$(required_libraries)
EOF
  ) | LC_ALL=C sort > "$library_tmp"
  test "$(wc -l < "$library_tmp" | tr -d ' ')" -eq 14
  LC_ALL=C sort -c "$library_tmp"

  (
    cd "$root"
    find . \( -type f -o -type l \) -print | LC_ALL=C sort | while IFS= read -r relative; do
      path=${relative#./}
      if [ -L "$relative" ]; then
        link=$(readlink "$relative")
        digest=$(printf '%s' "$link" | sha256sum | awk '{print $1}')
        printf '%s\tsymlink\t%s\n' "$path" "$digest"
      else
        digest=$(sha256sum "$relative" | awk '{print $1}')
        printf '%s\tfile\t%s\n' "$path" "$digest"
      fi
    done
  ) > "$manifest_tmp"
  LC_ALL=C sort -c "$manifest_tmp"
  /bin/mv "$library_tmp" "$library_report"
  cleanup_library_tmp=
  /bin/mv "$manifest_tmp" "$manifest"
  cleanup_manifest_tmp=
}

case "${1:-}" in
  --root-fixture)
    [ "$#" -eq 5 ] || [ "$#" -eq 6 ] || usage
    test "${TARGET_IMAGE_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'verify-target-image: root fixtures require test mode' >&2
      exit 2
    }
    if [ -n "${TARGET_IMAGE_ROOT_FIXTURE_NATIVE_INPUT_LOCK:-}" ]; then
      [ -f "$TARGET_IMAGE_ROOT_FIXTURE_NATIVE_INPUT_LOCK" ] || {
        printf '%s\n' 'verify-target-image: root-fixture native input lock does not exist' >&2
        exit 2
      }
      native_input_lock=$TARGET_IMAGE_ROOT_FIXTURE_NATIVE_INPUT_LOCK
    fi
    if [ "$#" -eq 6 ]; then
      native_selection_file=$6
    else
      native_selection_file=${TARGET_IMAGE_ROOT_FIXTURE_NATIVE_MEGA_DRIVE_SELECTION:-$native_selection_file}
    fi
    verify_root "$2" "$3" "$4" "$5" "$native_selection_file"
    exit
    ;;
  --inside)
    [ "$#" -eq 5 ] || [ "$#" -eq 6 ] || usage
    reject_native_input_lock_override
    variant=$2
    image=$3
    manifest=$4
    library_report=$5
    if [ "$#" -eq 6 ]; then
      native_selection_file=$6
    fi
    validate_variant "$variant"
    test -f "$image"
    image=$(readlink -f "$image")
    size=$(/usr/bin/stat -c %s "$image")
    test "$size" -le 67108864 || {
      printf 'verify-target-image: image exceeds 64 MiB: %s\n' "$size" >&2
      exit 1
    }
    file "$image" | grep -Eq 'ext[234] filesystem data'
    inspect_root=/target-image-output/inspect-$variant.$$
    case "$inspect_root" in
      /target-image-output/*) : ;;
      *)
        printf '%s\n' 'verify-target-image: unsafe inspection path' >&2
        exit 2
        ;;
    esac
    /bin/rm -rf "$inspect_root"
    /bin/mkdir -p "$inspect_root"
    cleanup_inspect_root=$inspect_root
    /usr/sbin/debugfs -R "rdump / $inspect_root" "$image" >/dev/null
    verify_root "$variant" "$inspect_root" "$manifest" "$library_report" "$native_selection_file"
    /bin/rm -rf "$inspect_root"
    cleanup_inspect_root=
    exit
    ;;
  prod|dev|native-dev)
    [ "$#" -eq 4 ] || [ "$#" -eq 5 ] || usage
    reject_native_input_lock_override
    variant=$1
    image=$2
    manifest=$3
    library_report=$4
    if [ "$variant" = native-dev ]; then
      if [ "$#" -eq 5 ]; then
        native_selection_file=$5
      else
        # Normal invocations run the verifier inside a container. The final
        # selection copy is under the repository mount, unlike the host cache
        # default used by the build steps.
        native_selection_file=build/output/target-image/native-dev/megadrive.selection.toml
      fi
    fi
    exec "$repo/scripts/target-image-container.sh" run \
      /work/scripts/verify-target-image.sh --inside \
      "$variant" "$image" "$manifest" "$library_report" "$native_selection_file"
    ;;
  *) usage ;;
esac
