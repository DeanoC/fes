#!/bin/sh
set -eu
exec python3 /src/image/de25-nano/managed_base.py _post-build "${1:?TARGET_DIR is required}"
