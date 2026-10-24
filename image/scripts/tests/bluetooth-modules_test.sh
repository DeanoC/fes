#!/bin/sh
# The image-creator module archive can contain unrelated symlinks and xz
# modules. Install only the USB Bluetooth modules, uncompressed.
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
installer=$repo/scripts/install-bluetooth-modules.sh
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fes-bluetooth-modules.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

python3 - "$fixture" <<'PY'
import io
import lzma
import sys
import tarfile
from pathlib import Path

fixture = Path(sys.argv[1])
btusb = b"btusb-module-bytes"


def add_bytes(archive, name, data):
    member = tarfile.TarInfo(name)
    member.size = len(data)
    archive.addfile(member, io.BytesIO(data))


def add_required(archive, btusb_name, btusb_data):
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btintel.ko", b"intel")
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btbcm.ko", b"bcm")
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btrtl.ko", b"rtl")
    add_bytes(archive, btusb_name, btusb_data)


with tarfile.open(fixture / "happy.tar", "w") as archive:
    add_bytes(archive, "extra/btusb.ko", b"wrong-btusb")
    add_required(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btusb.ko.xz", lzma.compress(btusb))
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/ath3k.ko", b"ath")
    link = tarfile.TarInfo("lib/modules/5.15.1-MiSTer/build")
    link.type = tarfile.SYMTYPE
    link.linkname = "/tmp"
    archive.addfile(link)

with tarfile.open(fixture / "missing.tar", "w") as archive:
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btintel.ko", b"intel")
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btbcm.ko", b"bcm")
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btrtl.ko", b"rtl")

with tarfile.open(fixture / "escape.tar", "w") as archive:
    add_required(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btusb.ko", b"btusb")
    add_bytes(archive, "../btusb.ko", b"escape")

with tarfile.open(fixture / "symlink.tar", "w") as archive:
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btintel.ko", b"intel")
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btbcm.ko", b"bcm")
    add_bytes(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btrtl.ko", b"rtl")
    link = tarfile.TarInfo("lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btusb.ko")
    link.type = tarfile.SYMTYPE
    link.linkname = "/tmp/btusb.ko"
    archive.addfile(link)

with tarfile.open(fixture / "absolute.tar", "w") as archive:
    add_required(archive, "lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btusb.ko", b"btusb")
    add_bytes(archive, "/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btintel.ko", b"absolute")
PY

target=$fixture/target
mkdir "$target"
"$installer" "$target" "$fixture/happy.tar"
printf '%s' 'btusb-module-bytes' >"$fixture/expected-btusb"
cmp "$fixture/expected-btusb" "$target/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btusb.ko"
printf '%s' intel >"$fixture/expected-intel"
cmp "$fixture/expected-intel" "$target/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btintel.ko"
printf '%s' ath >"$fixture/expected-ath"
cmp "$fixture/expected-ath" "$target/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/ath3k.ko"
test ! -e "$target/lib/modules/5.15.1-MiSTer/build"
test "$(stat -c %a "$target/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btusb.ko")" = 644

expect_rejection() {
  reason=$1
  archive=$2
  needle=$3
  rm -rf "$fixture/reject"
  mkdir "$fixture/reject"
  if "$installer" "$fixture/reject" "$archive" >"$fixture/reject.log" 2>&1; then
    printf 'installer accepted %s\n' "$reason" >&2
    exit 1
  fi
  grep -Fq "$needle" "$fixture/reject.log" || {
    printf 'installer rejected %s for the wrong reason:\n' "$reason" >&2
    cat "$fixture/reject.log" >&2
    exit 1
  }
}

expect_rejection 'a missing btusb module' "$fixture/missing.tar" 'missing btusb.ko'
expect_rejection 'a path escape' "$fixture/escape.tar" 'refusing module member ../btusb.ko'
expect_rejection 'a symlinked module' "$fixture/symlink.tar" 'refusing module member lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btusb.ko'
expect_rejection 'an absolute module path' "$fixture/absolute.tar" 'refusing module member /lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btintel.ko'
