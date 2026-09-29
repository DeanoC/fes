#!/bin/sh
set -eu
repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT INT TERM
mkdir -p "$fixture/run" "$fixture/share/selections" "$fixture/share/core-packages"
cat >"$fixture/supervise" <<'EOF'
#!/bin/sh
printf '%s\n' "$@" >"$MENU_INIT_ARGS"
EOF
chmod +x "$fixture/supervise"
sed -e "s|/run/|$fixture/run/|g" \
  -e "s|/usr/sbin/mister-supervise|$fixture/supervise|g" \
  -e "s|/usr/share/mister-runtime|$fixture/share|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S40mister-runtime" >"$fixture/runtime"
sed -e "s|/run/|$fixture/run/|g" \
  -e "s|/usr/sbin/mister-supervise|$fixture/supervise|g" \
  -e "s|/usr/share/mister-runtime|$fixture/share|g" \
  -e "s|/usr/sbin/fogcast-tenfoot|$fixture/fogcast-tenfoot|g" \
  -e "s|/usr/sbin/fogcast-kit|$fixture/fogcast-kit|g" \
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S60fogcast-kit" >"$fixture/kit"
chmod +x "$fixture/runtime" "$fixture/kit"
cat >"$fixture/fogcast-kit" <<'EOF'
#!/bin/sh
mode=grid
if [ -n "${KIT_UI_FILE:-}" ] && [ -f "$KIT_UI_FILE" ]; then
  mode=$(cat "$KIT_UI_FILE")
fi
if [ "$1" = --config ] && [ "${3:-}" = --print-kit-ui ]; then
  printf '%s\n' print >>"${KIT_UI_LOG:?}"
  printf '%s\n' "$mode"
  exit 0
fi
exit 0
EOF
chmod +x "$fixture/fogcast-kit"
export MENU_INIT_ARGS=$fixture/args
export KIT_UI_FILE=$fixture/kit-ui
export KIT_UI_LOG=$fixture/kit-ui.log
: >"$KIT_UI_LOG"

assert_args() {
  got=$(cat "$MENU_INIT_ARGS")
  if [ "$got" != "$1" ]; then
    printf 'menu init args:\n%s\nwant:\n%s\n' "$got" "$1" >&2
    exit 1
  fi
}

# macOS /bin/sh can leave the background supervisor runnable but not yet
# executing. Wait until it has written its argv and exited so the next
# start is not a no-op and the fixture is still present.
wait_supervise() {
  pidfile=$1
  i=0
  while [ "$i" -lt 100 ]; do
    if [ -s "$MENU_INIT_ARGS" ]; then
      if [ ! -f "$pidfile" ]; then
        return 0
      fi
      IFS= read -r pid <"$pidfile" || pid=
      if [ -z "${pid:-}" ] || ! kill -0 "$pid" 2>/dev/null; then
        return 0
      fi
    fi
    i=$((i + 1))
    sleep 0.05
  done
  printf 'timed out waiting for supervise\n' >&2
  exit 1
}

rm -f "$MENU_INIT_ARGS"
"$fixture/runtime" start
wait_supervise "$fixture/run/mister-runtime-supervisor.pid"
test "$(wc -l <"$MENU_INIT_ARGS")" -eq 2
test "$(tail -1 "$MENU_INIT_ARGS")" = /usr/sbin/mister-runtime
id=$(printf '%064d' 0 | tr 0 a)
mkdir "$fixture/share/core-packages/$id"
printf "core_id = 'fes.menu'\npackage_id = '%s'\n" "$id" >"$fixture/share/selections/fes-menu.package.toml"
rm -f "$MENU_INIT_ARGS"
"$fixture/runtime" start
wait_supervise "$fixture/run/mister-runtime-supervisor.pid"
test "$(tail -3 "$MENU_INIT_ARGS" | head -1)" = --menu-package
test "$(tail -2 "$MENU_INIT_ARGS" | head -1)" = "$fixture/share/core-packages/$id"
test "$(tail -1 "$MENU_INIT_ARGS")" = "$id"

grid_menu=$(printf '%s\n' fogcast-kit "$fixture/fogcast-kit" --config /media/fat/fogcast/launcher.json --menu-display)
plain_grid=$(printf '%s\n' fogcast-kit "$fixture/fogcast-kit" --config /media/fat/fogcast/launcher.json)
tenfoot_menu=$(printf '%s\n' fogcast-kit "$fixture/fogcast-tenfoot" -gfx menu-display -menu-socket "$fixture/run/mister-runtime.sock" -input auto -home rooms)

# Default grid with the menu selection: today's --menu-display command.
rm -f "$MENU_INIT_ARGS"
"$fixture/kit" start
wait_supervise "$fixture/run/fogcast-kit-supervisor.pid"
assert_args "$grid_menu"

# kit_ui tenfoot and an executable renderer.
printf '%s\n' tenfoot >"$KIT_UI_FILE"
printf '%s\n' tenfoot >"$fixture/fogcast-tenfoot"
chmod +x "$fixture/fogcast-tenfoot"
rm -f "$MENU_INIT_ARGS"
"$fixture/kit" start
wait_supervise "$fixture/run/fogcast-kit-supervisor.pid"
assert_args "$tenfoot_menu"

# tenfoot selected but the binary is missing: stay on the grid menu.
rm -f "$fixture/fogcast-tenfoot"
rm -f "$MENU_INIT_ARGS"
"$fixture/kit" start
wait_supervise "$fixture/run/fogcast-kit-supervisor.pid"
assert_args "$grid_menu"

# tenfoot selected without a menu selection: plain grid, and do not ask.
prints=$(wc -l <"$KIT_UI_LOG" | tr -d ' ')
rm -f "$fixture/share/selections/fes-menu.package.toml"
printf '%s\n' tenfoot >"$fixture/fogcast-tenfoot"
chmod +x "$fixture/fogcast-tenfoot"
rm -f "$MENU_INIT_ARGS"
"$fixture/kit" start
wait_supervise "$fixture/run/fogcast-kit-supervisor.pid"
assert_args "$plain_grid"
test "$(wc -l <"$KIT_UI_LOG" | tr -d ' ')" -eq "$prints"

printf "package_id = 'bad'\n" >"$fixture/share/selections/fes-menu.package.toml"
if "$fixture/runtime" start >/dev/null 2>&1; then
  echo 'runtime init accepted an invalid menu selection' >&2
  exit 1
fi
