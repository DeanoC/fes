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
release = "5.15.1-MiSTer"


def module_bytes(name, version=release):
    return b"\x7fELF\0vermagic=" + version.encode() + b" SMP preempt mod_unload ARMv7 p2v8\0" + name.encode()


btusb = module_bytes("btusb")


def add_bytes(archive, name, data):
    member = tarfile.TarInfo(name)
    member.size = len(data)
    archive.addfile(member, io.BytesIO(data))


def add_required(archive, btusb_name, btusb_data):
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btintel.ko", module_bytes("intel"))
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btbcm.ko", module_bytes("bcm"))
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btrtl.ko", module_bytes("rtl"))
    add_bytes(archive, btusb_name, btusb_data)


with tarfile.open(fixture / "happy.tar", "w") as archive:
    add_bytes(archive, "extra/btusb.ko", module_bytes("wrong-btusb", "6.1.0"))
    add_required(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btusb.ko.xz", lzma.compress(btusb))
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/ath3k.ko", module_bytes("ath"))
    link = tarfile.TarInfo(f"lib/modules/{release}/build")
    link.type = tarfile.SYMTYPE
    link.linkname = "/tmp"
    archive.addfile(link)

with tarfile.open(fixture / "missing.tar", "w") as archive:
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btintel.ko", module_bytes("intel"))
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btbcm.ko", module_bytes("bcm"))
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btrtl.ko", module_bytes("rtl"))

with tarfile.open(fixture / "escape.tar", "w") as archive:
    add_required(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btusb.ko", module_bytes("btusb"))
    add_bytes(archive, "../btusb.ko", b"escape")

with tarfile.open(fixture / "symlink.tar", "w") as archive:
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btintel.ko", module_bytes("intel"))
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btbcm.ko", module_bytes("bcm"))
    add_bytes(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btrtl.ko", module_bytes("rtl"))
    link = tarfile.TarInfo(f"lib/modules/{release}/kernel/drivers/bluetooth/btusb.ko")
    link.type = tarfile.SYMTYPE
    link.linkname = "/tmp/btusb.ko"
    archive.addfile(link)

with tarfile.open(fixture / "absolute.tar", "w") as archive:
    add_required(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btusb.ko", module_bytes("btusb"))
    add_bytes(archive, f"/lib/modules/{release}/kernel/drivers/bluetooth/btintel.ko", module_bytes("absolute"))

with tarfile.open(fixture / "wrong-vermagic.tar", "w") as archive:
    add_required(
        archive,
        f"lib/modules/{release}/kernel/drivers/bluetooth/btusb.ko",
        module_bytes("btusb", "6.1.0"),
    )

with tarfile.open(fixture / "missing-vermagic.tar", "w") as archive:
    add_required(archive, f"lib/modules/{release}/kernel/drivers/bluetooth/btusb.ko", b"btusb")
PY

target=$fixture/target
mkdir "$target"
"$installer" "$target" "$fixture/happy.tar"
python3 - "$fixture/expected-btusb" <<'PY'
import sys
from pathlib import Path

Path(sys.argv[1]).write_bytes(b"\x7fELF\0vermagic=5.15.1-MiSTer SMP preempt mod_unload ARMv7 p2v8\0btusb")
PY
cmp "$fixture/expected-btusb" "$target/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btusb.ko"
python3 - "$fixture/expected-intel" <<'PY'
import sys
from pathlib import Path

Path(sys.argv[1]).write_bytes(b"\x7fELF\0vermagic=5.15.1-MiSTer SMP preempt mod_unload ARMv7 p2v8\0intel")
PY
cmp "$fixture/expected-intel" "$target/lib/modules/5.15.1-MiSTer/kernel/drivers/bluetooth/btintel.ko"
python3 - "$fixture/expected-ath" <<'PY'
import sys
from pathlib import Path

Path(sys.argv[1]).write_bytes(b"\x7fELF\0vermagic=5.15.1-MiSTer SMP preempt mod_unload ARMv7 p2v8\0ath")
PY
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
expect_rejection 'a module built for another kernel' "$fixture/wrong-vermagic.tar" 'wrong vermagic for btusb.ko: 6.1.0 (expected 5.15.1-MiSTer)'
expect_rejection 'a module without vermagic' "$fixture/missing-vermagic.tar" 'missing or ambiguous vermagic for btusb.ko'
