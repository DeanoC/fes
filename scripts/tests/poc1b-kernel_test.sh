#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-poc1b-kernel.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

build_script=$repo/scripts/build-poc1b-kernel.sh
verify_script=$repo/scripts/verify-poc1b-kernel.sh
test -x "$build_script"
test -x "$verify_script"

grep -Fq 'd7adb20b4ca595838289406c083fff78f004a8c3' "$build_script"
grep -Fq 'ab8d809efa286413bfc78c5209f63495b36c92449767f0a8d13b1f7747b4dc9d' \
  "$repo/build/poc1b-kernel-defconfig.sha256"
grep -Fq 'MiSTer_defconfig' "$build_script"
grep -Fq 'socfpga_cyclone5_de10_nano.dtb' "$build_script"
grep -Fq 'KBUILD_BUILD_TIMESTAMP' "$build_script"
grep -Fq 'localversion.mister' "$build_script"
grep -Fq 'accepted-modules-tree' "$build_script"
grep -Fq '"/poc1b-output/kernel-work"' "$build_script"
if grep -Fq '"/poc1b-output/kernel-work-$run"' "$build_script"; then
  echo 'clean builds use different canonical build paths' >&2
  exit 1
fi
if grep -Fq 'LOCALVERSION=' "$build_script"; then
  echo 'kernel build overrides LOCALVERSION' >&2
  exit 1
fi

fake_build=$fixture/fake-kernel-build
cat > "$fake_build" <<'EOF'
#!/bin/sh
set -eu
output=$1
epoch=$2
mkdir -p "$output/modules/lib/modules/5.15.1-MiSTer/kernel"
printf '%s\n' kernel > "$output/zImage"
case "$output:${POC1B_KERNEL_FAKE_DIFFER:-0}" in
  *kernel-work-2*:1) printf '%s\n' different >> "$output/zImage" ;;
esac
printf '%s\n' dtb > "$output/MiSTer.dtb"
cat "$output/zImage" "$output/MiSTer.dtb" > "$output/zImage_dtb"
printf '%s\n' module > "$output/modules/lib/modules/5.15.1-MiSTer/kernel/test.ko"
find "$output/modules" -exec touch -t 202507020910.12 {} +
COPYFILE_DISABLE=1 LC_ALL=C tar -cf "$output/modules.tar" -C "$output/modules" .
gzip -n "$output/modules.tar"
cat > "$output/config" <<'CONFIG'
CONFIG_ARCH_INTEL_SOCFPGA=y
CONFIG_STMMAC_ETH=y
CONFIG_DWMAC_SOCFPGA=y
CONFIG_USB_DWC2=y
CONFIG_USB_DWC2_HOST=y
CONFIG_USB_HID=y
CONFIG_INPUT_EVDEV=y
CONFIG_DEVTMPFS=y
CONFIG_DEVTMPFS_MOUNT=y
CONFIG_BLK_DEV_LOOP=y
CONFIG_EXT4_FS=y
CONFIG_VFAT_FS=y
CONFIG_EXFAT_FS=y
CONFIG_TMPFS=y
CONFIG_FPGA=y
CONFIG_FPGA_MGR_SOCFPGA=y
CONFIG_SOCFPGA_FPGA_BRIDGE=y
CONFIG_FPGA_REGION=y
CONFIG
printf '%s\n' '5.15.1-MiSTer' > "$output/release"
printf '%s\n' 'fixture gcc' > "$output/compiler.txt"
printf '%s\n' "$epoch" > "$output/epoch"
EOF
chmod 0755 "$fake_build"

output_root=$fixture/output
POC1B_TEST_MODE=1 \
POC1B_KERNEL_BUILD_ONCE=$fake_build \
POC1B_KERNEL_OUTPUT_ROOT=$output_root \
  sh "$build_script"

for artifact in zImage MiSTer.dtb zImage_dtb modules.tar.gz config manifest.toml; do
  test -f "$output_root/kernel/$artifact"
done
cat "$output_root/kernel/zImage" "$output_root/kernel/MiSTer.dtb" > "$fixture/expected-zImage_dtb"
cmp "$fixture/expected-zImage_dtb" "$output_root/kernel/zImage_dtb"

fake_bin=$fixture/fake-bin
mkdir -p "$fake_bin"
cat > "$fake_bin/dtc" <<'EOF'
#!/bin/sh
last=
for arg do
  last=$arg
done
test -s "$last"
EOF
chmod 0755 "$fake_bin/dtc"
PATH="$fake_bin:$PATH" POC1B_TEST_MODE=1 \
  sh "$verify_script" --fixture "$output_root/kernel"

cp "$output_root/kernel/config" "$fixture/valid-config"
sed '/^CONFIG_FPGA_REGION=y$/d' "$fixture/valid-config" > \
  "$output_root/kernel/config"
if PATH="$fake_bin:$PATH" POC1B_TEST_MODE=1 \
  sh "$verify_script" --fixture "$output_root/kernel" >/dev/null 2>&1; then
  echo 'kernel verifier accepted a pruned required config' >&2
  exit 1
fi
cp "$fixture/valid-config" "$output_root/kernel/config"

expect_module_rejection() {
  if module_output=$(PATH="$fake_bin:$PATH" POC1B_TEST_MODE=1 \
    sh "$verify_script" --fixture "$output_root/kernel" 2>&1); then
    echo 'kernel verifier accepted an invalid module archive' >&2
    exit 1
  fi
  printf '%s\n' "$module_output" | \
    grep -Fq 'verify-poc1b-kernel: module archive rejected:' || {
      printf '%s\n' 'kernel verifier failed for the wrong reason:' >&2
      printf '%s\n' "$module_output" >&2
      exit 1
    }
}

cp "$output_root/kernel/modules.tar.gz" "$fixture/valid-modules.tar.gz"
mkdir -p "$fixture/unrelated"
printf '%s\n' unrelated > "$fixture/unrelated/readme.txt"
COPYFILE_DISABLE=1 tar -czf "$output_root/kernel/modules.tar.gz" \
  -C "$fixture/unrelated" readme.txt
expect_module_rejection

mkdir -p "$fixture/wrong-release/lib/modules/5.15.0/kernel"
printf '%s\n' module > \
  "$fixture/wrong-release/lib/modules/5.15.0/kernel/test.ko"
COPYFILE_DISABLE=1 tar -czf "$output_root/kernel/modules.tar.gz" \
  -C "$fixture/wrong-release" .
expect_module_rejection

python3 - "$output_root/kernel/modules.tar.gz" <<'PY'
import io
import sys
import tarfile

with tarfile.open(sys.argv[1], "w:gz") as archive:
    member = tarfile.TarInfo("../escape.ko")
    payload = b"module\n"
    member.size = len(payload)
    archive.addfile(member, io.BytesIO(payload))
PY
expect_module_rejection
cp "$fixture/valid-modules.tar.gz" "$output_root/kernel/modules.tar.gz"

printf '%s\n' extra >> "$output_root/kernel/zImage_dtb"
if PATH="$fake_bin:$PATH" POC1B_TEST_MODE=1 \
  sh "$verify_script" --fixture "$output_root/kernel" >/dev/null 2>&1; then
  echo 'kernel verifier accepted a multiply-appended DTB' >&2
  exit 1
fi

POC1B_KERNEL_FAKE_DIFFER=1 \
POC1B_TEST_MODE=1 \
POC1B_KERNEL_BUILD_ONCE=$fake_build \
POC1B_KERNEL_OUTPUT_ROOT=$fixture/different-output \
  sh "$build_script" >/dev/null 2>&1 && {
    echo 'kernel build accepted non-reproducible outputs' >&2
    exit 1
  }
:
