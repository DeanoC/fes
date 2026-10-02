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
mkdir -p "$fixture/run" "$fixture/log" "$fixture/share/selections"
sed -e "s|/run/|$fixture/run/|g" -e "s|/var/log/|$fixture/log/|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/usr/sbin/mister-supervise" > "$fixture/supervise"
sed -e "s|/run/|$fixture/run/|g" \
  -e "s|/usr/sbin/mister-supervise|$fixture/supervise|g" \
  -e "s|/usr/sbin/fogcast-tenfoot|$fixture/tenfoot|g" \
  -e "s|/usr/sbin/fogcast-kit|$fixture/launcher|g" \
  -e "s|/usr/share/mister-runtime|$fixture/share|g" \
  -e "s|/media/fat/fogcast/launcher.json|$fixture/config|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S60fogcast-kit" > "$fixture/service"
cat > "$fixture/launcher" <<'SCRIPT'
#!/bin/sh
# Foreground helper. Must exit; the supervised child is the other branch.
if [ "$1" = --config ] && [ "${3:-}" = --print-kit-ui ]; then
  mode=grid
  if [ -n "${KIT_UI_FILE:-}" ] && [ -f "$KIT_UI_FILE" ]; then
    mode=$(cat "$KIT_UI_FILE")
  fi
  printf '%s\n' "$mode"
  exit 0
fi
[ "$1" = --config ] || exit 9
[ -f "$2" ] || exit 3
# A stubborn child proves Stop remains bounded even when TERM is ignored.
trap '' TERM
while :; do sleep 1; done
SCRIPT
cat > "$fixture/tenfoot" <<'SCRIPT'
#!/bin/sh
if [ -n "${TENFOOT_ARGS_FILE:-}" ]; then
  printf '%s\n' "$@" > "$TENFOOT_ARGS_FILE"
fi
while :; do sleep 1; done
SCRIPT
chmod 755 "$fixture/service" "$fixture/supervise" "$fixture/launcher" "$fixture/tenfoot"
export KIT_UI_FILE=$fixture/kit-ui
export TENFOOT_ARGS_FILE=$fixture/tenfoot-args
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
# fes.menu plus kit_ui=tenfoot. --print-kit-ui must return, not hang as the child.
printf "core_id = 'fes.menu'\npackage_id = 'abc'\n" > "$fixture/share/selections/fes-menu.package.toml"
printf '%s\n' tenfoot > "$KIT_UI_FILE"
: > "$fixture/config"
rm -f "$TENFOOT_ARGS_FILE"
timeout 2 "$fixture/service" start
i=0
while [ ! -s "$TENFOOT_ARGS_FILE" ] && [ "$i" -lt 50 ]; do
  i=$((i + 1))
  sleep 0.05
done
[ -s "$TENFOOT_ARGS_FILE" ]
got=$(cat "$TENFOOT_ARGS_FILE")
want=$(printf '%s\n' -config "$fixture/config" -catalog-config /usr/share/fogcast/config.toml -gfx menu-display -menu-socket "$fixture/run/mister-runtime.sock" -input auto -home rooms)
if [ "$got" != "$want" ]; then
  printf 'tenfoot args:\n%s\nwant:\n%s\n' "$got" "$want" >&2
  exit 1
fi
timeout 8 "$fixture/service" stop
