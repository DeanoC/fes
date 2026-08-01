#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo 'install-poc1a-target: archive path required' >&2
  exit 2
fi

archive=$1
root=${MISTER_REMOTE_ROOT:-}
fat=$root/media/fat
base=$fat/mister-remote
linux=$fat/linux
work=$root/tmp/mister-remote-poc1a-install.$$

cleanup() {
  rm -f "$work/mister-remote/MiSTer.ini.fragment" \
    "$work/mister-remote/agent.toml" \
    "$work/mister-remote/mister-agent" \
    "$work/mister-remote/start-agent.sh" \
    "$work/user-startup.sh" "$work/MiSTer.ini"
  rmdir "$work/mister-remote" "$work" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

expected='mister-remote/MiSTer.ini.fragment
mister-remote/agent.toml
mister-remote/mister-agent
mister-remote/start-agent.sh'
actual=$(tar -tzf "$archive")
if [ "$actual" != "$expected" ]; then
  echo 'install-poc1a-target: archive does not contain the exact deployment allowlist' >&2
  exit 1
fi

mkdir -p "$root/tmp" "$work" "$base" "$linux" "$fat/games/SNES"
tar -xzf "$archive" -C "$work"

startup=$linux/user-startup.sh
startup_backup=$startup.pre-mister-remote
ini=$fat/MiSTer.ini
ini_backup=$ini.pre-mister-remote
if [ -f "$startup" ] && [ ! -e "$startup_backup" ]; then
  cp "$startup" "$startup_backup"
fi
if [ -f "$ini" ] && [ ! -e "$ini_backup" ]; then
  cp "$ini" "$ini_backup"
fi

cp "$work/mister-remote/MiSTer.ini.fragment" "$base/MiSTer.ini.fragment"
cp "$work/mister-remote/agent.toml" "$base/agent.toml"
cp "$work/mister-remote/mister-agent" "$base/mister-agent"
cp "$work/mister-remote/start-agent.sh" "$base/start-agent.sh"
chmod 0600 "$base/MiSTer.ini.fragment" "$base/agent.toml"
chmod 0755 "$base/mister-agent" "$base/start-agent.sh"

if [ ! -f "$startup" ]; then
  printf '%s\n' '#!/bin/sh' > "$startup"
fi
awk '
  $0 == "# BEGIN mister-remote" { dropping = 1; next }
  $0 == "# END mister-remote" { dropping = 0; next }
  !dropping { print }
' "$startup" > "$work/user-startup.sh"
printf '%s\n' \
  '# BEGIN mister-remote' \
  '/media/fat/mister-remote/start-agent.sh &' \
  '# END mister-remote' >> "$work/user-startup.sh"
cp "$work/user-startup.sh" "$startup"
chmod 0755 "$startup"

if [ ! -f "$ini" ]; then
  printf '%s\n' '[MiSTer]' > "$ini"
fi
awk '
  function settings() {
    print "logo=0"
    print "fb_terminal=0"
    print "osd_timeout=5"
    print "video_off=1"
    print "video_off_logo=0"
  }
  /^\[MiSTer\][[:space:]]*$/ { print; in_mister = 1; seen = 1; next }
  /^\[/ {
    if (in_mister && !written) { settings(); written = 1 }
    in_mister = 0
    print
    next
  }
  in_mister && /^(logo|fb_terminal|osd_timeout|video_off|video_off_logo)[[:space:]]*=/ { next }
  { print }
  END {
    if (in_mister && !written) settings()
    if (!seen) { print "[MiSTer]"; settings() }
  }
' "$ini" > "$work/MiSTer.ini"
cp "$work/MiSTer.ini" "$ini"

printf '%s\n' 'Intentional invalid-extension sentinel; this is not a game ROM.' > \
  "$fat/games/SNES/.mister-remote-invalid.txt"

if [ -z "${MISTER_REMOTE_SKIP_START:-}" ]; then
  "$base/start-agent.sh" >/tmp/mister-agent-supervisor-launch.log 2>&1 &
fi
