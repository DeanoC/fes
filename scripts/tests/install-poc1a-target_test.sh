#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/mister-remote-install-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

root=$fixture/root
stage=$fixture/stage
mkdir -p "$root/media/fat/linux" "$root/media/fat/games/SNES" "$stage/mister-remote"
original_startup='#!/bin/sh
echo stock
'
original_ini='[MiSTer]
logo=1
fb_terminal=1
osd_timeout=30
video_off=0
video_off_logo=1

[Other]
logo=preserve
'
printf '%s' "$original_startup" > "$root/media/fat/linux/user-startup.sh"
printf '%s' "$original_ini" > "$root/media/fat/MiSTer.ini"

printf '%s\n' '[MiSTer]' > "$stage/mister-remote/MiSTer.ini.fragment"
printf '%s\n' 'token = "test"' > "$stage/mister-remote/agent.toml"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$stage/mister-remote/mister-agent"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$stage/mister-remote/start-agent.sh"
archive=$fixture/package.tar.gz
tar -czf "$archive" -C "$stage" \
  mister-remote/MiSTer.ini.fragment \
  mister-remote/agent.toml \
  mister-remote/mister-agent \
  mister-remote/start-agent.sh

MISTER_REMOTE_ROOT=$root MISTER_REMOTE_SKIP_START=1 sh "$repo/scripts/install-poc1a-target.sh" "$archive"
MISTER_REMOTE_ROOT=$root MISTER_REMOTE_SKIP_START=1 sh "$repo/scripts/install-poc1a-target.sh" "$archive"

test "$(cat "$root/media/fat/linux/user-startup.sh.pre-mister-remote")" = "$(printf '%s' "$original_startup")"
test "$(cat "$root/media/fat/MiSTer.ini.pre-mister-remote")" = "$(printf '%s' "$original_ini")"
test "$(grep -c '^# BEGIN mister-remote$' "$root/media/fat/linux/user-startup.sh")" -eq 1
test "$(grep -c '^# END mister-remote$' "$root/media/fat/linux/user-startup.sh")" -eq 1
test "$(grep -c '^logo=0$' "$root/media/fat/MiSTer.ini")" -eq 1
test "$(grep -c '^fb_terminal=0$' "$root/media/fat/MiSTer.ini")" -eq 1
test "$(grep -c '^osd_timeout=5$' "$root/media/fat/MiSTer.ini")" -eq 1
test "$(grep -c '^video_off=1$' "$root/media/fat/MiSTer.ini")" -eq 1
test "$(grep -c '^video_off_logo=0$' "$root/media/fat/MiSTer.ini")" -eq 1
grep -q '^logo=preserve$' "$root/media/fat/MiSTer.ini"
test -f "$root/media/fat/games/SNES/.mister-remote-invalid.txt"
test "$(stat -f '%Lp' "$root/media/fat/mister-remote/mister-agent")" = 755
test "$(stat -f '%Lp' "$root/media/fat/mister-remote/agent.toml")" = 600
