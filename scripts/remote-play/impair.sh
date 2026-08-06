#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
bin=${REMOTE_PLAY_IMPAIR_BIN:-$repo_dir/bin/remote-play-impair}

if [ "$#" -eq 0 ]; then
  printf '%s\n' "usage: $0 --listen LISTEN --forward DESTINATION [--drop-every N] [--reorder-window N]" >&2
  exit 2
fi

exec "$bin" "$@"