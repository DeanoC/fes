#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
cleanup_manifest_tmp=
cleanup_library_tmp=
cleanup_inspect_root=

cleanup() {
  [ -z "$cleanup_manifest_tmp" ] || /bin/rm -f "$cleanup_manifest_tmp"
  [ -z "$cleanup_library_tmp" ] || /bin/rm -f "$cleanup_library_tmp"
  [ -z "$cleanup_inspect_root" ] || /bin/rm -rf "$cleanup_inspect_root"
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
  printf 'usage: verify-target-image.sh prod|dev IMAGE MANIFEST LIBRARY_REPORT | --inside VARIANT IMAGE MANIFEST LIBRARY_REPORT | --root-fixture VARIANT ROOT MANIFEST LIBRARY_REPORT\n' >&2
  exit 2
}

validate_variant() {
  case "$1" in
    prod|dev) : ;;
    *) usage ;;
  esac
}

verify_root() {
  variant=$1
  root=$2
  manifest=$3
  library_report=$4
  validate_variant "$variant"
  root=$(CDPATH='' cd -- "$root" && pwd -P)

  for required in \
    /sbin/init \
    /usr/bin/busybox \
    /usr/sbin/mister-agent \
    /etc/init.d/S20mister-network \
    /etc/init.d/S40mister-main \
    /etc/init.d/S49fogcast-target-smoke \
    /etc/init.d/S50mister-agent; do
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

  if find "$root" -type f \( \
    -iname '*.rom' -o -iname '*.sfc' -o -iname '*.smc' -o \
    -iname '*.md' -o -iname '*.gen' -o -iname '*.zip' -o \
    -iname '*.bin' -o -iname '*.rbf' -o -iname '*.map' -o -name 'agent.toml' \
    -o -name '*-gdb.py' \
  \) -print -quit | grep -q .; then
    printf '%s\n' 'verify-target-image: forbidden game, runtime, or debug payload found' >&2
    exit 1
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
    [ "$#" -eq 5 ] || usage
    test "${TARGET_IMAGE_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'verify-target-image: root fixtures require test mode' >&2
      exit 2
    }
    verify_root "$2" "$3" "$4" "$5"
    exit
    ;;
  --inside)
    [ "$#" -eq 5 ] || usage
    variant=$2
    image=$3
    manifest=$4
    library_report=$5
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
    verify_root "$variant" "$inspect_root" "$manifest" "$library_report"
    /bin/rm -rf "$inspect_root"
    cleanup_inspect_root=
    exit
    ;;
  prod|dev)
    [ "$#" -eq 4 ] || usage
    variant=$1
    image=$2
    manifest=$3
    library_report=$4
    exec "$repo/scripts/target-image-container.sh" run \
      /work/scripts/verify-target-image.sh --inside \
      "$variant" "$image" "$manifest" "$library_report"
    ;;
  *) usage ;;
esac
