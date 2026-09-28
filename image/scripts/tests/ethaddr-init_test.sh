#!/bin/sh
# Exercise S15fes-ethaddr against private fixture paths (no kit, no real eth0).
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
script=$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S15fes-ethaddr
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fes-ethaddr.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

test -x "$script"
sh -n "$script"
# The appliance shell is BusyBox ash; reject bash-only syntax where possible.
if command -v dash >/dev/null 2>&1; then dash -n "$script"; fi

cid_fixture=035344534336344780deadbeef0145a1
cid_expected=02:46:43:a9:6a:37

fail() {
  printf 'ethaddr-init_test: %s\n' "$1" >&2
  exit 1
}

# new_card NAME: fresh FAT, sysfs and command stubs under $fixture/NAME.
new_card() {
  card=$fixture/$1
  mkdir -p "$card/fat/linux" "$card/sys/eth0" "$card/run" "$card/bin"
  printf '%s\n' "$cid_fixture" > "$card/cid"
  printf '%s\n' '02:03:04:05:06:07' > "$card/sys/eth0/address"
  printf '%s\n' '0x1002' > "$card/sys/eth0/flags"
  : > "$card/ip.log"
  : > "$card/mount.log"
  : > "$card/mounts"
  cat > "$card/bin/ip" <<SCRIPT
#!/bin/sh
printf '%s\n' "\$*" >> "$card/ip.log"
SCRIPT
  cat > "$card/bin/mount" <<SCRIPT
#!/bin/sh
printf '%s\n' "\$*" >> "$card/mount.log"
SCRIPT
  cat > "$card/bin/logger" <<SCRIPT
#!/bin/sh
printf '%s\n' "\$*" >> "$card/logger.log"
SCRIPT
  chmod 755 "$card/bin/ip" "$card/bin/mount" "$card/bin/logger"
}

run_card() {
  FES_ETHADDR_UBOOT_TXT=$card/fat/linux/u-boot.txt \
  FES_ETHADDR_CID=$card/cid \
  FES_ETHADDR_STATUS=$card/run/fes-ethaddr \
  FES_ETHADDR_NET_DIR=$card/sys/eth0 \
  FES_ETHADDR_MOUNTS=$card/mounts \
  FES_ETHADDR_FAT=$card/fat \
  FES_ETHADDR_IP=$card/bin/ip \
  FES_ETHADDR_MOUNT=$card/bin/mount \
  FES_ETHADDR_LOGGER=$card/bin/logger \
  FES_ETHADDR_URANDOM=${urandom:-/dev/urandom} \
  FES_ETHADDR_WAIT=0 \
    sh "$script" start > "$card/stdout"
}

persisted_mac() {
  sed -n 's/^ethaddr=//p' "$card/fat/linux/u-boot.txt"
}

check_mac_format() {
  printf '%s\n' "$1" | grep -Eq '^02:46:43(:[0-9a-f]{2}){3}$' || fail "bad MAC format: $1"
  first=$(printf '%s' "$1" | cut -c1-2)
  # Locally administered (bit 1 set) and unicast (bit 0 clear).
  [ $((0x$first & 2)) -eq 2 ] || fail "not locally administered: $1"
  [ $((0x$first & 1)) -eq 0 ] || fail "not unicast: $1"
}

# 1. Existing ethaddr (the manual .84 value) is untouched, eth0 not changed.
new_card existing
printf 'bootargs=quiet\nethaddr=02:46:43:00:00:84\n' > "$card/fat/linux/u-boot.txt"
cp "$card/fat/linux/u-boot.txt" "$card/before"
run_card
cmp "$card/before" "$card/fat/linux/u-boot.txt" || fail 'existing u-boot.txt changed'
[ ! -s "$card/ip.log" ] || fail 'existing ethaddr touched eth0'
grep -Fqx 'mac=02:46:43:00:00:84 source=existing' "$card/run/fes-ethaddr" || fail 'existing status'
grep -Fq 'source=existing' "$card/stdout" || fail 'existing console log'
grep -Fq 'source=existing' "$card/logger.log" || fail 'existing syslog'

# Any value counts, even one this script would never produce.
new_card existing-other
printf '  ethaddr=aa:bb:cc:dd:ee:ff' > "$card/fat/linux/u-boot.txt"
cp "$card/fat/linux/u-boot.txt" "$card/before"
run_card
cmp "$card/before" "$card/fat/linux/u-boot.txt" || fail 'unterminated existing line changed'

# 2. CID yields a deterministic MAC; the file is created when missing.
new_card cid-a
rmdir "$card/fat/linux"
run_card
[ "$(persisted_mac)" = "$cid_expected" ] || fail "cid MAC $(persisted_mac) != $cid_expected"
check_mac_format "$(persisted_mac)"
[ "$(cat "$card/fat/linux/u-boot.txt")" = "ethaddr=$cid_expected" ] || fail 'created file content'
grep -Fq "mac=$cid_expected source=cid persisted=yes eth0-set" "$card/run/fes-ethaddr" || fail 'cid status'
printf 'link set dev eth0 down\nlink set dev eth0 address %s\nlink set dev eth0 up\n' \
  "$cid_expected" | cmp - "$card/ip.log" || fail 'first boot did not set eth0 before DHCP'
[ -f "$card/fat/linux/u-boot.txt" ] || fail 'u-boot.txt missing'
[ "$(find "$card/fat/linux" -mindepth 1 | wc -l | tr -d ' ')" -eq 1 ] || fail 'temporary file left behind'
# Re-run: the persisted line is now "existing" and nothing changes.
cp "$card/fat/linux/u-boot.txt" "$card/before"
: > "$card/ip.log"
run_card
cmp "$card/before" "$card/fat/linux/u-boot.txt" || fail 'cid re-run changed file'
grep -Fqx "mac=$cid_expected source=existing" "$card/run/fes-ethaddr" || fail 'cid re-run status'
[ ! -s "$card/ip.log" ] || fail 'cid re-run touched eth0'

# Same CID on another (reimaged) card, with surrounding whitespace: same MAC.
new_card cid-b
printf '  %s \r\n' "$cid_fixture" > "$card/cid"
printf 'bootargs=quiet\n' > "$card/fat/linux/u-boot.txt"
run_card
[ "$(persisted_mac)" = "$cid_expected" ] || fail 'cid MAC not deterministic'
printf 'bootargs=quiet\nethaddr=%s\n' "$cid_expected" | cmp - "$card/fat/linux/u-boot.txt" ||
  fail 'existing content not preserved'

# eth0 already up: persist for next boot but never bounce a live link.
new_card eth0-up
printf '%s\n' '0x1003' > "$card/sys/eth0/flags"
run_card
[ "$(persisted_mac)" = "$cid_expected" ] || fail 'eth0-up persist'
[ ! -s "$card/ip.log" ] || fail 'live eth0 was reconfigured'
grep -Fq 'eth0-up-next-boot' "$card/run/fes-ethaddr" || fail 'eth0-up status'

# eth0 already carries the MAC (U-Boot applied it): no ip calls.
new_card eth0-same
printf '%s\n' "$cid_expected" | tr 'a-f' 'A-F' > "$card/sys/eth0/address"
run_card
[ ! -s "$card/ip.log" ] || fail 'matching eth0 was reconfigured'
grep -Fq 'eth0-already' "$card/run/fes-ethaddr" || fail 'eth0-same status'

# No eth0 at all: still persisted, boot not blocked.
new_card eth0-missing
rm -rf "$card/sys/eth0"
run_card
[ "$(persisted_mac)" = "$cid_expected" ] || fail 'eth0-missing persist'
grep -Fq 'eth0-missing' "$card/run/fes-ethaddr" || fail 'eth0-missing status'

# 3. All-zero CID -> random fallback, persisted once and stable on re-run.
new_card zero
printf '%s\n' 00000000000000000000000000000000 > "$card/cid"
printf '\253\315\357' > "$card/urandom"
urandom=$card/urandom run_card
[ "$(persisted_mac)" = 02:46:43:ab:cd:ef ] || fail "random MAC $(persisted_mac)"
grep -Fq 'source=random persisted=yes' "$card/run/fes-ethaddr" || fail 'random status'
cp "$card/fat/linux/u-boot.txt" "$card/before"
printf '\001\002\003' > "$card/urandom"
urandom=$card/urandom run_card
cmp "$card/before" "$card/fat/linux/u-boot.txt" || fail 'random MAC not stable'
grep -Fqx 'mac=02:46:43:ab:cd:ef source=existing' "$card/run/fes-ethaddr" || fail 'random re-run'

# Missing CID with the real random source.
new_card missing
rm -f "$card/cid"
run_card
mac=$(persisted_mac)
check_mac_format "$mac"
grep -Fq "mac=$mac source=random" "$card/run/fes-ethaddr" || fail 'missing-cid status'
run_card
[ "$(persisted_mac)" = "$mac" ] || fail 'missing-cid MAC not stable'
[ "$(grep -c '^ethaddr=' "$card/fat/linux/u-boot.txt")" -eq 1 ] || fail 'duplicate ethaddr'

# Empty and non-hex CIDs also fall back.
for bad in empty garbage; do
  new_card "cid-$bad"
  if [ "$bad" = empty ]; then : > "$card/cid"; else printf 'not-a-cid\n' > "$card/cid"; fi
  run_card
  grep -Fq 'source=random' "$card/run/fes-ethaddr" || fail "$bad CID did not fall back"
done

# 4. Trailing-newline handling.
new_card no-newline
printf 'bootargs=quiet' > "$card/fat/linux/u-boot.txt"
run_card
printf 'bootargs=quiet\nethaddr=%s\n' "$cid_expected" | cmp - "$card/fat/linux/u-boot.txt" ||
  fail 'missing trailing newline not repaired'
new_card empty-file
: > "$card/fat/linux/u-boot.txt"
run_card
printf 'ethaddr=%s\n' "$cid_expected" | cmp - "$card/fat/linux/u-boot.txt" || fail 'empty file'
new_card commented
printf '#ethaddr=02:00:00:00:00:01\n' > "$card/fat/linux/u-boot.txt"
run_card
printf '#ethaddr=02:00:00:00:00:01\nethaddr=%s\n' "$cid_expected" |
  cmp - "$card/fat/linux/u-boot.txt" || fail 'commented ethaddr treated as set'

# 5. Read-only FAT is remounted rw for the write and restored ro.
new_card readonly
printf '/dev/root %s exfat ro,sync 0 0\n' "$card/fat" > "$card/mounts"
run_card
[ "$(persisted_mac)" = "$cid_expected" ] || fail 'read-only persist'
printf -- '-o remount,rw %s\n-o remount,ro %s\n' "$card/fat" "$card/fat" |
  cmp - "$card/mount.log" || fail 'read-only remount sequence'
new_card writable
printf '/dev/root %s exfat rw,sync 0 0\n' "$card/fat" > "$card/mounts"
run_card
[ ! -s "$card/mount.log" ] || fail 'writable FAT was remounted'

# Unpersistable FAT: boot continues, this boot still gets the MAC.
new_card unwritable
rm -rf "$card/fat"
printf 'not a directory\n' > "$card/fat"
run_card 2>/dev/null
grep -Fq "mac=$cid_expected source=cid persisted=no eth0-set" "$card/run/fes-ethaddr" ||
  fail 'unpersistable status'

# stop is a no-op and unknown verbs are rejected.
sh "$script" stop
if sh "$script" bogus 2>/dev/null; then fail 'bogus verb accepted'; fi

printf '%s\n' 'ethaddr-init_test: ok'
