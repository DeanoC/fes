#!/bin/sh
set -eu

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
bin=${REMOTE_PLAY_BIN:-$repo_dir/bin/remote-play-spike}

printf '%s\n' "The sender spike does not include a receiver; start an independent RFC 6184 H.264 receiver on the requested UDP port." >&2
printf '%s\n' "Use: $bin probe" >&2
exit 2
