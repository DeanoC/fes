#!/bin/sh
set -eu
LC_ALL=C
export LC_ALL

escape_toml() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

emit_artifact() {
  artifact_name=$1
  artifact_path=$2
  artifact_source=$3
  artifact_digest=$(sha256sum "$artifact_path" | awk '{print $1}')
  artifact_size=$(stat -c '%s' "$artifact_path")
  printf '[[artifacts]]\nname = "%s"\npath = "%s"\nsha256 = "%s"\nsize = %s\nsource = "%s"\n\n' \
    "$(escape_toml "$artifact_name")" "$(escape_toml "$artifact_path")" "$artifact_digest" "$artifact_size" "$(escape_toml "$artifact_source")"
}

resolve_one() {
  resolve_pattern=$1
  # The deliberate unquoted expansion resolves the caller's singleton glob.
  # shellcheck disable=SC2086
  set -- $resolve_pattern
  if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
    printf 'expected exactly one regular file matching %s\n' "$resolve_pattern" >&2
    exit 1
  fi
  printf '%s\n' "$1"
}

megadrive_core=$(resolve_one '/media/fat/_Console/MegaDrive_*.rbf')
snes_core=$(resolve_one '/media/fat/_Console/SNES_*.rbf')

printf 'format = 1\n\n'
emit_artifact main_mister /media/fat/MiSTer https://github.com/MiSTer-devel/Main_MiSTer
emit_artifact menu /media/fat/menu.rbf https://github.com/MiSTer-devel/Menu_MiSTer
emit_artifact kernel /media/fat/linux/zImage_dtb https://github.com/MiSTer-devel/Linux-Kernel_MiSTer
emit_artifact megadrive_core "$megadrive_core" https://github.com/MiSTer-devel/MegaDrive_MiSTer
emit_artifact snes_core "$snes_core" https://github.com/MiSTer-devel/SNES_MiSTer

set -- /media/fat/config/input_*_v3.map
[ "$#" -ge 1 ] && [ -f "$1" ] || {
  printf 'no controller maps found\n' >&2
  exit 1
}
controller_maps=$(printf '%s\n' "$@" | sort)
printf '%s\n' "$controller_maps" | while IFS= read -r controller_map; do
  [ -f "$controller_map" ] || {
    printf 'controller map is not a regular file: %s\n' "$controller_map" >&2
    exit 1
  }
  controller_name="controller_$(basename "$controller_map")"
  emit_artifact "$controller_name" "$controller_map" 'local MiSTer controller mapping'
done

printf '[runtime]\nkernel_release = "%s"\n\n' "$(escape_toml "$(uname -r)")"

ldd_output=$(ldd /media/fat/MiSTer) || {
  printf 'ldd failed for Main_MiSTer\n' >&2
  exit 1
}
library_paths=$(printf '%s\n' "$ldd_output" | awk '{ for (i = 1; i <= NF; i++) if ($i ~ /^\//) print $i }' | sort -u)
[ -n "$library_paths" ] || {
  printf 'no Main_MiSTer runtime libraries found\n' >&2
  exit 1
}
printf '%s\n' "$library_paths" | while IFS= read -r library_path; do
  [ -f "$library_path" ] || {
    printf 'library is not a regular file: %s\n' "$library_path" >&2
    exit 1
  }
  library_digest=$(sha256sum "$library_path" | awk '{print $1}')
  library_size=$(stat -c '%s' "$library_path")
  printf '[[libraries]]\npath = "%s"\nsha256 = "%s"\nsize = %s\n\n' \
    "$(escape_toml "$library_path")" "$library_digest" "$library_size"
done
