#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
kernel_commit=d7adb20b4ca595838289406c083fff78f004a8c3
defconfig_sha256=ab8d809efa286413bfc78c5209f63495b36c92449767f0a8d13b1f7747b4dc9d
kernel_release=5.15.1-MiSTer
dtb_target=socfpga_cyclone5_de10_nano.dtb

usage() {
  printf 'usage: verify-target-kernel.sh DIR | --inside DIR | --fixture DIR\n' >&2
  exit 2
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    /usr/bin/shasum -a 256 "$1" | /usr/bin/awk '{print $1}'
  fi
}

file_size() {
  if stat -c %s "$1" >/dev/null 2>&1; then
    stat -c %s "$1"
  else
    /usr/bin/stat -f %z "$1"
  fi
}

manifest_value() {
  manifest=$1
  key=$2
  awk -v wanted="$key" '
    $0 ~ "^" wanted "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      if (substr(value, 1, 1) == "\"") {
        sub(/^\"/, "", value)
        sub(/\"[[:space:]]*$/, "", value)
      }
      print value
      exit
    }
  ' "$manifest"
}

artifact_value() {
  manifest=$1
  wanted_name=$2
  wanted_field=$3
  awk -v wanted_name="$wanted_name" -v wanted_field="$wanted_field" '
    /^\[\[artifacts\]\]$/ { in_artifact=1; name=""; next }
    in_artifact && /^name[[:space:]]*=/ {
      name=$0
      sub(/^[^=]*=[[:space:]]*\"/, "", name)
      sub(/\"[[:space:]]*$/, "", name)
      next
    }
    in_artifact && name == wanted_name && $0 ~ "^" wanted_field "[[:space:]]*=" {
      value=$0
      sub(/^[^=]*=[[:space:]]*/, "", value)
      gsub(/^\"|\"[[:space:]]*$/, "", value)
      print value
      exit
    }
  ' "$manifest"
}

verify_module_archive() {
  archive=$1
  python3 - "$archive" "$kernel_release" <<'PY'
import sys
import tarfile


def reject(reason):
    print(
        f"verify-target-kernel: module archive rejected: {reason}",
        file=sys.stderr,
    )
    raise SystemExit(1)


archive_path, release = sys.argv[1:]
modules_root = f"lib/modules/{release}"
root_directories = {"lib", "lib/modules", modules_root}
has_module = False

try:
    with tarfile.open(archive_path, "r:gz") as module_archive:
        members = module_archive.getmembers()
except (OSError, tarfile.TarError) as error:
    reject(f"invalid gzip tar: {error}")

if not members:
    reject("archive is empty")

for member in members:
    name = member.name
    if name.startswith("/"):
        reject(f"absolute member path: {name}")
    while name.startswith("./"):
        name = name[2:]
    if name in {"", "."}:
        if not member.isdir():
            reject("archive root is not a directory")
        continue

    normalized = name[:-1] if name.endswith("/") else name
    parts = normalized.split("/")
    if any(part in {"", ".", ".."} for part in parts):
        reject(f"unsafe member path: {member.name}")
    if member.issym() or member.islnk():
        reject(f"link member is not allowed: {member.name}")
    if normalized in root_directories:
        if not member.isdir():
            reject(f"module-tree parent is not a directory: {member.name}")
        continue
    if not normalized.startswith(modules_root + "/"):
        reject(f"member is outside {modules_root}: {member.name}")
    if member.isdir():
        continue
    if not member.isfile():
        reject(f"special member is not allowed: {member.name}")
    if normalized.endswith((".ko", ".ko.xz")):
        has_module = True

if not has_module:
    reject(f"no kernel module exists beneath {modules_root}")
PY
}

verify_kernel() {
  dir=$1
  test -d "$dir"
  dir=$(CDPATH='' cd -- "$dir" && pwd -P)
  for artifact in zImage MiSTer.dtb zImage_dtb modules.tar.gz config manifest.toml; do
    test -s "$dir/$artifact" || {
      printf 'verify-target-kernel: missing or empty artifact: %s\n' "$artifact" >&2
      exit 1
    }
  done

  combined=$(mktemp "${TMPDIR:-/tmp}/fogcast-target-zimage-dtb.XXXXXX")
  names=$(mktemp "${TMPDIR:-/tmp}/fogcast-target-kernel-names.XXXXXX")
  trap 'rm -f "$combined" "$names"' EXIT INT TERM
  cat "$dir/zImage" "$dir/MiSTer.dtb" > "$combined"
  cmp "$combined" "$dir/zImage_dtb" || {
    printf '%s\n' 'verify-target-kernel: DTB is not appended exactly once' >&2
    exit 1
  }
  dtc -I dtb -O dts "$dir/MiSTer.dtb" >/dev/null
  verify_module_archive "$dir/modules.tar.gz"

  for symbol in \
    CONFIG_ARCH_INTEL_SOCFPGA \
    CONFIG_STMMAC_ETH \
    CONFIG_DWMAC_SOCFPGA \
    CONFIG_USB_DWC2 \
    CONFIG_USB_DWC2_HOST \
    CONFIG_USB_HID \
    CONFIG_INPUT_EVDEV \
    CONFIG_DEVTMPFS \
    CONFIG_DEVTMPFS_MOUNT \
    CONFIG_BLK_DEV_LOOP \
    CONFIG_EXT4_FS \
    CONFIG_VFAT_FS \
    CONFIG_EXFAT_FS \
    CONFIG_TMPFS \
    CONFIG_FPGA \
    CONFIG_FPGA_MGR_SOCFPGA \
    CONFIG_SOCFPGA_FPGA_BRIDGE \
    CONFIG_FPGA_REGION; do
    grep -Fxq "$symbol=y" "$dir/config" || {
      printf 'verify-target-kernel: required config is absent: %s=y\n' "$symbol" >&2
      exit 1
    }
  done

  manifest=$dir/manifest.toml
  test "$(manifest_value "$manifest" format)" = 1
  test "$(manifest_value "$manifest" source_date_epoch)" = 1751459412
  test "$(manifest_value "$manifest" kernel_commit)" = "$kernel_commit"
  test "$(manifest_value "$manifest" defconfig)" = MiSTer_defconfig
  test "$(manifest_value "$manifest" defconfig_sha256)" = "$defconfig_sha256"
  test "$(manifest_value "$manifest" dtb)" = "$dtb_target"
  test "$(manifest_value "$manifest" release)" = "$kernel_release"
  test "$(manifest_value "$manifest" release_suffix)" = -MiSTer
  test "$(manifest_value "$manifest" release_suffix_source)" = accepted-modules-tree
  test -n "$(manifest_value "$manifest" compiler)"

  awk '
    /^\[\[artifacts\]\]$/ { in_artifact=1; next }
    in_artifact && /^name[[:space:]]*=/ {
      value=$0
      sub(/^[^=]*=[[:space:]]*\"/, "", value)
      sub(/\"[[:space:]]*$/, "", value)
      print value
    }
  ' "$manifest" | LC_ALL=C sort > "$names"
  expected_names='MiSTer.dtb
config
modules.tar.gz
zImage
zImage_dtb'
  test "$(cat "$names")" = "$expected_names" || {
    printf '%s\n' 'verify-target-kernel: manifest artifact set is not exact' >&2
    exit 1
  }
  for artifact in zImage MiSTer.dtb zImage_dtb modules.tar.gz config; do
    recorded_sha=$(artifact_value "$manifest" "$artifact" sha256)
    recorded_size=$(artifact_value "$manifest" "$artifact" size)
    test "$recorded_sha" = "$(sha256_file "$dir/$artifact")" || {
      printf 'verify-target-kernel: digest mismatch: %s\n' "$artifact" >&2
      exit 1
    }
    test "$recorded_size" = "$(file_size "$dir/$artifact")" || {
      printf 'verify-target-kernel: size mismatch: %s\n' "$artifact" >&2
      exit 1
    }
  done
  rm -f "$combined" "$names"
  trap - EXIT INT TERM
  printf 'target image kernel verified: %s\n' "$(sha256_file "$dir/zImage_dtb")"
}

case "${1:-}" in
  --fixture)
    [ "$#" -eq 2 ] || usage
    test "${TARGET_IMAGE_TEST_MODE:-0}" = 1 || {
      printf '%s\n' 'verify-target-kernel: fixtures require test mode' >&2
      exit 2
    }
    verify_kernel "$2"
    ;;
  --inside)
    [ "$#" -eq 2 ] || usage
    test "$2" = /work/build/output/target-image/kernel || {
      printf '%s\n' 'verify-target-kernel: unsafe container artifact path' >&2
      exit 2
    }
    verify_kernel "$2"
    ;;
  *)
    [ "$#" -eq 1 ] || usage
    expected=$repo/build/output/target-image/kernel
    requested=$(CDPATH='' cd -- "$1" 2>/dev/null && pwd -P) || {
      printf 'verify-target-kernel: artifact directory does not exist: %s\n' "$1" >&2
      exit 1
    }
    test "$requested" = "$expected" || {
      printf '%s\n' 'verify-target-kernel: only the canonical artifact directory may be verified' >&2
      exit 2
    }
    exec "$repo/scripts/target-image-container.sh" run \
      /work/scripts/verify-target-kernel.sh --inside \
      /work/build/output/target-image/kernel
    ;;
esac
