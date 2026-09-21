#!/bin/sh
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
# Current diagnostic contract; historical hardware acceptance does not qualify
# new contained-protocol artifacts.
grep -Fq 'development-contained-v1' "$root/internal/misterruntime/protocol_v2.go"
grep -Fq 'load_development_rbf' "$root/internal/misterruntime/protocol_v2.go"
grep -Fq '/tmp/fogcast-development/core.rbf' "$root/cmd/mister-agent/main.go"
grep -Fq 'native-dev' "$root/../../image/scripts/build-target-image.sh"
for retired in internal/mister internal/core scripts/target-smoke.sh scripts/native-runtime-smoke.sh scripts/deploy-target-image.sh; do
  if [ -e "$root/$retired" ]; then
    printf 'retired product path remains: %s\n' "$retired" >&2
    exit 1
  fi
done
if grep -Eq '^target-(image-deploy|smoke|native-smoke):' "$root/Makefile"; then
  echo 'retired target entrypoint remains' >&2
  exit 1
fi
printf '%s\n' 'contained development diagnostic contract passed'
