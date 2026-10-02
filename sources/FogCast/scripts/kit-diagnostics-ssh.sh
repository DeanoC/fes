#!/bin/sh
# Pipe kit-diagnostics.sh to root@<kit-ip> as `ssh host sh -s`.
# The kit does not receive a copied file. Remote argv is only `sh -s`.
# Nothing is written to known_hosts, /media/fat, or u-boot.txt, and no
# service is restarted.
#
# Usage: kit-diagnostics-ssh.sh <kit-ip>
# Stock kit login (see docs/DEVELOPMENT.md):
#   FES_KIT_DIAG_SSH='sshpass -p 1 ssh' kit-diagnostics-ssh.sh 192.168.10.84
#
# FES_KIT_DIAG_SSH is an optional client prefix, split on spaces. The
# collector's fixture variables are unset before exec so a client SendEnv
# rule cannot point the kit at a workstation path.

set -eu

usage() {
  printf '%s\n' 'usage: kit-diagnostics-ssh.sh kit-ip' >&2
}

if [ "$#" -ne 1 ]; then
  usage
  exit 2
fi

host=$1
case $host in
  ''|-*|*[!0-9A-Za-z.-]*|*..*)
    usage
    exit 2
    ;;
esac
if [ "${#host}" -gt 253 ]; then
  usage
  exit 2
fi

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
collector=$script_dir/kit-diagnostics.sh
[ -f "$collector" ] || {
  printf '%s\n' 'kit-diagnostics-ssh: collector is missing' >&2
  exit 1
}

set -- \
  -o ConnectTimeout=8 \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  "root@${host}" \
  sh -s

if [ -n "${FES_KIT_DIAG_SSH:-}" ]; then
  set -f
  # Intentional split of the operator-supplied client prefix.
  # shellcheck disable=SC2086
  set -- ${FES_KIT_DIAG_SSH} "$@"
  set +f
else
  set -- ssh -o BatchMode=yes "$@"
fi

unset FES_KIT_DIAG_ROOT FES_KIT_DIAG_SAMPLE_SECONDS
exec "$@" < "$collector"
