#!/bin/sh
# Keep the disposable MiSTer development tunnel alive. Never use this for production.
set -eu

TARGET=${MISTER_TARGET_HOST:-}
TARGET_USER=${MISTER_TARGET_USER:-root}
LOCAL_PORT=${MISTER_LOCAL_TUNNEL_PORT:-18182}
TARGET_PORT=${MISTER_TARGET_API_PORT:-8182}
PASSWORD_FILE=${MISTER_SSH_PASSWORD_FILE:-}
LOCK_DIR=${TMPDIR:-/tmp}/fogcast-dev-target-tunnel.lock
SSH_PID_FILE=$LOCK_DIR/ssh.pid

case "$TARGET" in
  ''|-*) printf '%s\n' 'dev-target-tunnel: invalid target host' >&2; exit 2 ;;
esac
case "$LOCAL_PORT:$TARGET_PORT" in
  *[!0-9:]*|*:*:*) printf '%s\n' 'dev-target-tunnel: invalid port' >&2; exit 2 ;;
esac

if ! mkdir "$LOCK_DIR" 2>/dev/null; then
  if [ ! -r "$LOCK_DIR/pid" ]; then
    printf '%s\n' 'dev-target-tunnel: tunnel lock owner is unknown; refusing recovery' >&2
    exit 1
  fi
  IFS= read -r lock_pid < "$LOCK_DIR/pid" || {
    printf '%s\n' 'dev-target-tunnel: tunnel lock owner is unreadable; refusing recovery' >&2
    exit 1
  }
  case "$lock_pid" in
    ''|*[!0-9]*)
      printf '%s\n' 'dev-target-tunnel: tunnel lock owner is invalid; refusing recovery' >&2
      exit 1
      ;;
  esac
  if kill -0 "$lock_pid" 2>/dev/null; then
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

if [ -n "$PASSWORD_FILE" ]; then
  case "$PASSWORD_FILE" in
    /*) : ;;
    *)
      printf '%s\n' 'dev-target-tunnel: password file must be an absolute path' >&2
      exit 1
      ;;
  esac
  if [ ! -f "$PASSWORD_FILE" ] || [ -L "$PASSWORD_FILE" ] || [ ! -r "$PASSWORD_FILE" ]; then
    printf '%s\n' 'dev-target-tunnel: password file must be a readable regular file, not a symlink' >&2
    exit 1
  fi
  password_owner=$(stat -f '%u' "$PASSWORD_FILE" 2>/dev/null || true)
  password_mode=$(stat -f '%Lp' "$PASSWORD_FILE" 2>/dev/null || true)
  if [ "$password_owner" != "$(id -u)" ] || [ "$password_mode" != 600 ]; then
    printf '%s\n' 'dev-target-tunnel: password file must be owned by the current user with mode 0600' >&2
    exit 1
  fi
fi

TEMP_PASSWORD=''
TTY_STATE=''
EXPECT_PID=''
LAUNCHING=0
PENDING_SIGNAL=''
restore_tty() {
  if [ -n "$TTY_STATE" ]; then
    stty "$TTY_STATE" < /dev/tty 2>/dev/null || true
    TTY_STATE=''
  fi
}
stop_expect() {
  if [ -z "$EXPECT_PID" ]; then
    return
  fi
  expect_pid=$EXPECT_PID
  EXPECT_PID=''
  ssh_pid=''
  ssh_start=''
  if [ -r "$SSH_PID_FILE" ]; then
    exec 9< "$SSH_PID_FILE"
    IFS= read -r ssh_pid <&9 || true
    IFS= read -r ssh_start <&9 || true
    exec 9<&-
  fi
  case "$ssh_pid" in
    ''|*[!0-9]*) ssh_pid='' ;;
  esac
  current_ssh_start=''
  if [ -n "$ssh_pid" ]; then
    current_ssh_start=$(ps -p "$ssh_pid" -o lstart= 2>/dev/null || true)
    current_ssh_start=${current_ssh_start#${current_ssh_start%%[![:space:]]*}}
  fi
  if [ -n "$ssh_pid" ] && [ -n "$ssh_start" ] && [ "$current_ssh_start" = "$ssh_start" ]; then
    kill -TERM "$ssh_pid" 2>/dev/null || true
  else
    ssh_pid=''
  fi
  kill -TERM "$expect_pid" 2>/dev/null || true
  attempts=0
  while kill -0 "$expect_pid" 2>/dev/null && [ "$attempts" -lt 20 ]; do
    sleep 0.1
    attempts=$((attempts + 1))
  done
  if kill -0 "$expect_pid" 2>/dev/null; then
    kill -KILL "$expect_pid" 2>/dev/null || true
  fi
  ssh_verify_start=''
  if [ -n "$ssh_pid" ]; then
    ssh_verify_start=$(ps -p "$ssh_pid" -o lstart= 2>/dev/null || true)
    ssh_verify_start=${ssh_verify_start#${ssh_verify_start%%[![:space:]]*}}
  fi
  if [ -n "$ssh_pid" ] && [ "$current_ssh_start" = "$ssh_verify_start" ] && kill -0 "$ssh_pid" 2>/dev/null; then
    kill -KILL "$ssh_pid" 2>/dev/null || true
  fi
  wait "$expect_pid" 2>/dev/null || true
}
cleanup() {
  stop_expect
  restore_tty
  if [ -n "$TEMP_PASSWORD" ]; then
    rm -f "$TEMP_PASSWORD"
  fi
  rm -rf "$LOCK_DIR" 2>/dev/null || true
}
trap cleanup EXIT
on_int() {
  if [ "$LAUNCHING" -eq 1 ]; then PENDING_SIGNAL=INT; return; fi
  cleanup
  exit 130
}
on_term() {
  if [ "$LAUNCHING" -eq 1 ]; then PENDING_SIGNAL=TERM; return; fi
  cleanup
  exit 143
}
trap on_int INT
trap on_term TERM

if [ -z "$PASSWORD_FILE" ]; then
  TEMP_PASSWORD=$(mktemp "${TMPDIR:-/tmp}/fogcast-ssh-password.XXXXXX")
  chmod 600 "$TEMP_PASSWORD"
  TTY_STATE=$(stty -g < /dev/tty)
  stty -echo < /dev/tty
  printf 'MiSTer SSH password (temporary file removed on normal or trapped exit): ' >&2
  IFS= read -r PASSWORD < /dev/tty
  restore_tty
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

  export TARGET TARGET_USER LOCAL_PORT TARGET_PORT PASSWORD_FILE SSH_PID_FILE
  # ssh exits normally when the target reboots; keep the supervisor loop alive.
  LAUNCHING=1
  expect <<'EXPECT' &
set timeout 30
log_user 0
trap {
  if {[info exists ssh_pid]} { catch {exec kill -TERM $ssh_pid} }
  catch {close}
  exit 143
} SIGTERM
trap {
  if {[info exists ssh_pid]} { catch {exec kill -TERM $ssh_pid} }
  catch {close}
  exit 130
} SIGINT
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
set ssh_pid [pid]
set ssh_start [string trim [exec ps -p $ssh_pid -o lstart=]]
set ssh_identity_tmp "$env(SSH_PID_FILE).tmp"
set ssh_identity_handle [open $ssh_identity_tmp w]
puts $ssh_identity_handle $ssh_pid
puts $ssh_identity_handle $ssh_start
close $ssh_identity_handle
file rename -force $ssh_identity_tmp $env(SSH_PID_FILE)
expect {
  -re {(?i)(password|passphrase):} { send -- "$password\r"; exp_continue }
  eof { exit 1 }
  timeout { exp_continue }
}
EXPECT
  EXPECT_PID=$!
  LAUNCHING=0
  case "$PENDING_SIGNAL" in
    INT) PENDING_SIGNAL=''; cleanup; exit 130 ;;
    TERM) PENDING_SIGNAL=''; cleanup; exit 143 ;;
  esac
  wait "$EXPECT_PID" 2>/dev/null || true
  EXPECT_PID=''
  rm -f "$SSH_PID_FILE"

  printf '%s\n' 'dev-target-tunnel: tunnel exited; retrying' >&2
  sleep 2
done
