#!/bin/sh
set -eu
exec /usr/bin/python3 /src/image/de25-nano/managed_base.py _post-build "${1:?TARGET_DIR is required}"
