#!/bin/sh
set -eu
case "$1:$2" in *[!0-9:]*|0:*|*:0) echo 'builder UID/GID must be nonzero integers' >&2; exit 1;; esac
# Numeric identity matches the caller without installing user-management tools.
printf 'builder:x:%s:%s:Media builder:/home/builder:/bin/sh\n' "$1" "$2" >> /etc/passwd
printf 'builder:x:%s:\n' "$2" >> /etc/group
mkdir -p /home/builder /work
chown "$1:$2" /home/builder /work
