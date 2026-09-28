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
  "$repo/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S60fogcast-kit" >"$fixture/kit"
chmod +x "$fixture/runtime" "$fixture/kit"
export MENU_INIT_ARGS=$fixture/args

"$fixture/runtime" start
sleep 0.1
test "$(wc -l <"$MENU_INIT_ARGS")" -eq 2
test "$(tail -1 "$MENU_INIT_ARGS")" = /usr/sbin/mister-runtime
id=$(printf '%064d' 0 | tr 0 a)
mkdir "$fixture/share/core-packages/$id"
printf "core_id = 'fes.menu'\npackage_id = '%s'\n" "$id" >"$fixture/share/selections/fes-menu.package.toml"
"$fixture/runtime" start
sleep 0.1
test "$(tail -3 "$MENU_INIT_ARGS" | head -1)" = --menu-package
test "$(tail -2 "$MENU_INIT_ARGS" | head -1)" = "$fixture/share/core-packages/$id"
test "$(tail -1 "$MENU_INIT_ARGS")" = "$id"

"$fixture/kit" start
sleep 0.1
test "$(tail -1 "$MENU_INIT_ARGS")" = --menu-display

printf "package_id = 'bad'\n" >"$fixture/share/selections/fes-menu.package.toml"
if "$fixture/runtime" start >/dev/null 2>&1; then
  echo 'runtime init accepted an invalid menu selection' >&2
  exit 1
fi
