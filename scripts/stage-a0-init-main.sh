#!/bin/sh
set -eu
# shellcheck disable=SC1007 # Required POSIX empty-CDPATH command environment.
tool=${STAGE_A0_TOOL:-"$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)/bin/stage-a0"}
exec "$tool" init-main "$@"
