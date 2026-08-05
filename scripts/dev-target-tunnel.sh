#!/bin/sh
# Keep the disposable MiSTer development tunnel alive. Never use this for production.
set -eu

TARGET=${MISTER_TARGET_HOST:-192.168.10.245}
TARGET_USER=${MISTER_TARGET_USER:-root}
LOCAL_PORT=${MISTER_LOCAL_TUNNEL_PORT:-18182}
TARGET_PORT=${MISTER_TARGET_API_PORT:-8182}
PASSWORD_FILE=${MISTER_SSH_PASSWORD_FILE:-}

case "$TARGET" in
  ''|-*) printf '%s\n' 'dev-target-tunnel: invalid target host' >&2; exit 2 ;;
esac
case "$LOCAL_PORT:$TARGET_PORT" in
  *[!0-9:]*|*:*:*) printf '%s\n' 'dev-target-tunnel: invalid port' >&2; exit 2 ;;
esac

if ! command -v expect >/dev/null 2>&1; then
  printf '%s\n' 'dev-target-tunnel: expect is required for password-authenticated reconnects' >&2
  exit 1
fi

if [ -n "$PASSWORD_FILE" ] && [ ! -r "$PASSWORD_FILE" ]; then
  printf '%s\n' 'dev-target-tunnel: password file is not readable' >&2
  exit 1
fi

TEMP_PASSWORD=''
cleanup() {
  if [ -n "$TEMP_PASSWORD" ]; then
    rm -f "$TEMP_PASSWORD"
  fi
}
trap cleanup EXIT INT TERM

if [ -z "$PASSWORD_FILE" ]; then
  TEMP_PASSWORD=$(mktemp "${TMPDIR:-/tmp}/fogcast-ssh-password.XXXXXX")
  chmod 600 "$TEMP_PASSWORD"
  printf 'MiSTer SSH password (not saved after this process): ' >&2
  stty -echo < /dev/tty
  IFS= read -r PASSWORD < /dev/tty
  stty echo < /dev/tty
  printf '\n' >&2
  umask 077
  printf '%s' "$PASSWORD" > "$TEMP_PASSWORD"
  unset PASSWORD
  PASSWORD_FILE=$TEMP_PASSWORD
fi

while :; do
  if ! nc -G 3 -z "$TARGET" 22 >/dev/null 2>&1; then
    printf '%s\n' "dev-target-tunnel: waiting for $TARGET:22" >&2
    sleep 3
    continue
  fi

  export TARGET TARGET_USER LOCAL_PORT TARGET_PORT PASSWORD_FILE
  expect <<'EXPECT'
set timeout 30
set target $env(TARGET)
set user $env(TARGET_USER)
set local_port $env(LOCAL_PORT)
set target_port $env(TARGET_PORT)
set password_file $env(PASSWORD_FILE)
set password [string trimright [read [open $password_file r]] "\n"]
spawn ssh -N \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=5 \
  -o ServerAliveCountMax=2 \
  -L 127.0.0.1:$local_port:127.0.0.1:$target_port \
  $user@$target
expect {
  -re {(?i)(password|passphrase):} { send -- "$password\r"; exp_continue }
  eof { exit 1 }
  timeout { interact }
}
EXPECT

  printf '%s\n' 'dev-target-tunnel: tunnel exited; retrying' >&2
  sleep 2
done
