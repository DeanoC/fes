#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
bin=${REMOTE_PLAY_BIN:-$repo_dir/bin/remote-play-sender}

if [ "$#" -eq 0 ]; then
  printf '%s\n' "usage: $0 --capture-device DEVICE [sender flags... ]" >&2
  exit 2
fi

exec "$bin" sender "$@"
