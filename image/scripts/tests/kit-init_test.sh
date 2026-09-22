#!/bin/sh
# Exercise the actual init and shared supervisor in private fixture paths.
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d)
cleanup() {
  "$fixture/service" stop 2>/dev/null || true
  rm -rf "$fixture"
}
trap cleanup EXIT INT TERM
mkdir "$fixture/run" "$fixture/log"
sed -e "s|/run/|$fixture/run/|g" -e "s|/var/log/|$fixture/log/|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/usr/sbin/mister-supervise" > "$fixture/supervise"
sed -e "s|/run/|$fixture/run/|g" \
  -e "s|/usr/sbin/mister-supervise|$fixture/supervise|g" \
  -e "s|/usr/sbin/fogcast-kit|$fixture/launcher|g" \
  -e "s|/media/fat/fogcast/launcher.json|$fixture/config|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S60fogcast-kit" > "$fixture/service"
cat > "$fixture/launcher" <<'SCRIPT'
#!/bin/sh
[ "$1" = --config ] || exit 9
[ -f "$2" ] || exit 3
# A stubborn child proves Stop remains bounded even when TERM is ignored.
trap '' TERM
while :; do sleep 1; done
SCRIPT
chmod 755 "$fixture/service" "$fixture/supervise" "$fixture/launcher"
timeout 2 "$fixture/service" start
first=$(cat "$fixture/run/fogcast-kit-supervisor.pid")
timeout 2 "$fixture/service" start
[ "$first" = "$(cat "$fixture/run/fogcast-kit-supervisor.pid")" ]
sleep 2
grep -q 'exited status=3' "$fixture/log/fogcast-kit.log"
: > "$fixture/config"
sleep 2
child=$(cat "$fixture/run/fogcast-kit.pid")
kill -0 "$child"
timeout 8 "$fixture/service" stop
[ ! -e "$fixture/run/fogcast-kit-supervisor.pid" ]
[ ! -e "$fixture/run/fogcast-kit.pid" ]
# Killed processes may briefly remain zombies until adopted/reaped.
if kill -0 "$child" 2>/dev/null; then
  [ "$(ps -o stat= -p "$child" | cut -c1)" = Z ]
fi
