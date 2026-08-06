#!/bin/sh
# Keep the disposable MiSTer development tunnel alive. Never use this for production.
set -eu

TARGET=${MISTER_TARGET_HOST:-192.168.10.245}
TARGET_USER=${MISTER_TARGET_USER:-root}
LOCAL_PORT=${MISTER_LOCAL_TUNNEL_PORT:-18182}
TARGET_PORT=${MISTER_TARGET_API_PORT:-8182}
PASSWORD_FILE=${MISTER_SSH_PASSWORD_FILE:-}
LOCK_DIR=${TMPDIR:-/tmp}/fogcast-dev-target-tunnel.lock

case "$TARGET" in
  ''|-*) printf '%s\n' 'dev-target-tunnel: invalid target host' >&2; exit 2 ;;
esac
case "$LOCAL_PORT:$TARGET_PORT" in
  *[!0-9:]*|*:*:*) printf '%s\n' 'dev-target-tunnel: invalid port' >&2; exit 2 ;;
esac

if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  lock_pid=''
  if [ -r "$LOCK_DIR/pid" ]; then
    IFS= read -r lock_pid < "$LOCK_DIR/pid" || true
  fi
  if [ -n "$lock_pid" ] && kill -0 "$lock_pid" 2>/dev/null; then
    printf '%s\n' "dev-target-tunnel: another tunnel instance owns $LOCAL_PORT" >&2
    exit 1
  fi
  printf '%s\n' 'dev-target-tunnel: removing stale tunnel lock' >&2
  rm -rf "$LOCK_DIR"
  mkdir "$LOCK_DIR"
fi
printf '%s\n' "$$" > "$LOCK_DIR/pid"
cleanup_lock() { rm -rf "$LOCK_DIR" 2>/dev/null || true; }
trap cleanup_lock EXIT INT TERM

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
  rm -rf "$LOCK_DIR" 2>/dev/null || true
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

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
  # ssh exits normally when the target reboots; keep the supervisor loop alive.
  expect <<'EXPECT' || true
set timeout 30
log_user 0
set target $env(TARGET)
set user $env(TARGET_USER)
set local_port $env(LOCAL_PORT)
set target_port $env(TARGET_PORT)
set password_file $env(PASSWORD_FILE)
if {![file readable $password_file]} { exit 1 }
set password_handle [open $password_file r]
set password [string trimright [read $password_handle] "\n"]
close $password_handle
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
  timeout { exp_continue }
}
EXPECT

  printf '%s\n' 'dev-target-tunnel: tunnel exited; retrying' >&2
  sleep 2
done
