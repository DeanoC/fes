#!/bin/sh
# Install USB Bluetooth modules only when their vermagic matches the target kernel.
# BusyBox insmod cannot decompress .ko.xz, so modules are stored uncompressed.
set -eu

target=${1:?TARGET_DIR is required}
archive=${2:?module archive is required}
[ -d "$target" ] && [ ! -L "$target" ] || {
  printf 'install-bluetooth-modules: target is not a directory: %s\n' "$target" >&2
  exit 2
}
[ -f "$archive" ] || {
  printf 'install-bluetooth-modules: archive is missing: %s\n' "$archive" >&2
  exit 1
}

/usr/bin/python3 - "$target" "$archive" <<'PY'
import lzma
import os
import sys
import tarfile

target, archive_path = sys.argv[1:]
release = "5.15.1-MiSTer"
dest_rel = f"lib/modules/{release}/kernel/drivers/bluetooth"
required = ("btintel.ko", "btbcm.ko", "btrtl.ko", "btusb.ko")
optional = ("ath3k.ko",)
wanted = set(required) | set(optional)
limit = 8 * 1024 * 1024


def fail(reason):
    print(f"install-bluetooth-modules: {reason}", file=sys.stderr)
    raise SystemExit(1)


def unsafe(name):
    if name.startswith("/"):
        return True
    parts = [part for part in name.split("/") if part != "."]
    return any(part in {"", ".."} for part in parts)


def module_name(base):
    if base.endswith(".ko.xz"):
        return base[:-3]
    if base.endswith(".ko"):
        return base
    return None


def in_target_release(name):
    parts = [part for part in name.split("/") if part not in {"", "."}]
    return len(parts) >= 3 and parts[:3] == ["lib", "modules", release]


def module_vermagic(raw, key):
    marker = b"vermagic="
    values = []
    offset = 0
    while True:
        index = raw.find(marker, offset)
        if index < 0:
            break
        if index == 0 or raw[index - 1] == 0:
            value = raw[index + len(marker):].split(b"\0", 1)[0]
            values.append(value)
        offset = index + len(marker)
    if len(values) != 1:
        fail(f"missing or ambiguous vermagic for {key}")
    try:
        version = values[0].decode("ascii").split()[0]
    except (UnicodeDecodeError, IndexError):
        fail(f"invalid vermagic for {key}")
    if version != release:
        fail(f"wrong vermagic for {key}: {version} (expected {release})")


try:
    archive = tarfile.open(archive_path, "r:*")
except (OSError, tarfile.TarError) as error:
    fail(f"invalid module archive: {error}")

chosen = {}
with archive:
    for member in archive.getmembers():
        base = member.name.rstrip("/").split("/")[-1]
        key = module_name(base)
        if key not in wanted:
            continue
        if unsafe(member.name) or member.issym() or member.islnk() or not member.isfile():
            fail(f"refusing module member {member.name}")
        if not in_target_release(member.name):
            continue
        previous = chosen.get(key)
        if previous is None:
            chosen[key] = member
    missing = [name for name in required if name not in chosen]
    if missing:
        fail("missing " + " ".join(missing))
    root = os.path.realpath(target)
    for key, member in chosen.items():
        extracted = archive.extractfile(member)
        if extracted is None:
            fail(f"unreadable module {member.name}")
        raw = extracted.read()
        if member.name.endswith(".ko.xz"):
            try:
                raw = lzma.decompress(raw)
            except lzma.LZMAError as error:
                fail(f"cannot decompress {member.name}: {error}")
        if not raw or len(raw) > limit:
            fail(f"unexpected size for {key}")
        module_vermagic(raw, key)
        destination = os.path.join(root, dest_rel, key)
        parent = os.path.realpath(os.path.dirname(destination))
        if parent != root and not parent.startswith(root + os.sep):
            fail(f"destination escapes the target: {destination}")
        os.makedirs(parent, exist_ok=True)
        with open(destination, "wb") as handle:
            handle.write(raw)
        os.chmod(destination, 0o644)
PY
