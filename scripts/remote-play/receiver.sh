#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
bin=${REMOTE_PLAY_RECEIVER_BIN:-$repo_dir/bin/remote-play-receiver}

if [ "$#" -eq 0 ]; then
  printf '%s\n' "usage: $0 --session SESSION --token TOKEN [receiver flags...]" >&2
  exit 2
fi

exec "$bin" "$@"
