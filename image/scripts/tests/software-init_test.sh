#!/bin/sh
# Init delegates the whole group, and a failed stop prevents a second start.
set -eu
image=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT INT TERM
cat >"$fixture/supervisor" <<'INNER'
#!/bin/sh
printf '%s\n' "$1" >> "$CALLS"
if [ "$1" = stop ] && [ "${STOP_FAIL:-}" = 1 ]; then exit 1; fi
INNER
chmod +x "$fixture/supervisor"
sed "s|/usr/sbin/fes-kit-software|$fixture/supervisor|g" "$image/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S40fes-kit-software" > "$fixture/init"
chmod +x "$fixture/init"
export CALLS=$fixture/calls
"$fixture/init" restart
test "$(cat "$CALLS")" = "$(printf 'stop\nstart')"
: > "$CALLS"
if STOP_FAIL=1 "$fixture/init" restart; then echo 'restart ignored failed stop' >&2; exit 1; fi
test "$(cat "$CALLS")" = stop
for old in S40mister-runtime S50mister-agent S60fogcast-kit; do
 test ! -e "$image/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/$old"
done
