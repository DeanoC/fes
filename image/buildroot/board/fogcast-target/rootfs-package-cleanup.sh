#!/bin/sh
set -eu

[ "$#" -eq 1 ] || {
  printf '%s\n' 'rootfs-package-cleanup: expected target directory' >&2
  exit 1
}
target=$1
[ -d "$target" ] && [ ! -L "$target" ] || {
  printf '%s\n' 'rootfs-package-cleanup: target must be a non-symlink directory' >&2
  exit 1
}

# Check each retained path component before looking below it. Tests such as
# -d follow symlinks, so the explicit -L rejection must accompany every step.
path=$target
for component in usr share mister-runtime core-packages; do
  path=$path/$component
  if [ ! -e "$path" ] && [ ! -L "$path" ]; then
    exit 0
  fi
  [ -d "$path" ] && [ ! -L "$path" ] || {
    printf '%s\n' 'rootfs-package-cleanup: package path components must be non-symlink directories' >&2
    exit 1
  }
done
root=$path

build_inputs=$target/usr/share/mister-runtime/build-inputs
[ -f "$build_inputs" ] && [ ! -L "$build_inputs" ] || {
  printf '%s\n' 'rootfs-package-cleanup: package identity record is unavailable' >&2
  exit 1
}
identities=
identity_keys=
identity_count=0
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    fes_*_package_id=*)
      key=${line%%=*}
      identity=${line#*=}
      case " $identity_keys " in
        *" $key "*)
          printf '%s\n' 'rootfs-package-cleanup: package identity record is ambiguous' >&2
          exit 1
          ;;
      esac
      [ "${#identity}" -eq 64 ] || {
        printf '%s\n' 'rootfs-package-cleanup: package identity is invalid' >&2
        exit 1
      }
      case "$identity" in
        *[!0-9a-f]*)
          printf '%s\n' 'rootfs-package-cleanup: package identity is invalid' >&2
          exit 1
          ;;
      esac
      case " $identities " in
        *" $identity "*)
          printf '%s\n' 'rootfs-package-cleanup: package identity record is ambiguous' >&2
          exit 1
          ;;
      esac
      identity_keys="$identity_keys $key"
      identities="$identities $identity"
      identity_count=$((identity_count + 1))
      ;;
  esac
done < "$build_inputs"
[ "$identity_count" -gt 0 ] || {
  printf '%s\n' 'rootfs-package-cleanup: package identity record is ambiguous' >&2
  exit 1
}

for identity in $identities; do
  directory=$root/$identity
  [ -d "$directory" ] && [ ! -L "$directory" ] || {
    printf '%s\n' 'rootfs-package-cleanup: selected package must be a non-symlink directory' >&2
    exit 1
  }
done
[ "$(find "$root" -mindepth 1 -maxdepth 1 -print | wc -l | tr -d ' ')" -eq "$identity_count" ] || {
  printf '%s\n' 'rootfs-package-cleanup: package root differs from selected identity' >&2
  exit 1
}

# Unlink permission belongs to directories. Keep manifest and RBF at 0444 so
# the filesystem image and the copied tree retain the same sealed file modes.
video_root=$target/usr/share/mister-runtime/core-video-parts
video_ids=
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    factory_video_*_package_id=*)
      identity=${line#*=}
      [ "${#identity}" -eq 64 ] || exit 1
      case "$identity" in *[!0-9a-f]*) exit 1 ;; esac
      case " $video_ids " in *" $identity "*) : ;; *) video_ids="$video_ids $identity" ;; esac
      ;;
  esac
done < "$build_inputs"
if [ -e "$video_root" ] || [ -L "$video_root" ]; then
  [ -n "$video_ids" ] && [ -d "$video_root" ] && [ ! -L "$video_root" ] || {
    echo 'rootfs-package-cleanup: video tree differs from selected identity' >&2; exit 1;
  }
  [ -f "$video_root/index.json" ] && [ ! -L "$video_root/index.json" ] || exit 1
  video_count=1
  for identity in $video_ids; do
    [ -d "$video_root/$identity" ] && [ ! -L "$video_root/$identity" ] || exit 1
    video_count=$((video_count + 1))
  done
  [ "$(find -P "$video_root" -type d | wc -l | tr -d ' ')" -eq "$video_count" ] || exit 1
  [ "$(find -P "$video_root" -type l | wc -l | tr -d ' ')" -eq 0 ] || exit 1
elif [ -n "$video_ids" ]; then
  echo 'rootfs-package-cleanup: selected video tree is missing' >&2; exit 1
fi
for identity in $identities; do
  chmod u+w "$root/$identity"
done
chmod u+w "$root"
if [ -n "$video_ids" ]; then
  for identity in $video_ids; do chmod u+w "$video_root/$identity"; done
  chmod u+w "$video_root"
fi
