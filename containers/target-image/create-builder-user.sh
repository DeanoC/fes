#!/bin/sh
set -eu

: "${HOST_UID:?HOST_UID is required}"
: "${HOST_GID:?HOST_GID is required}"

if ! getent group "$HOST_GID" >/dev/null; then
  groupadd --gid "$HOST_GID" builder
fi

useradd --uid "$HOST_UID" --gid "$HOST_GID" --create-home builder
