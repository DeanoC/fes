#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-target-smoke.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

smoke=$repo/scripts/target-smoke.sh
test -x "$smoke"

fake_bin=$fixture/bin
mkdir -p "$fake_bin"
curl_log=$fixture/curl.log
cat > "$fake_bin/curl" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FOGCAST_CURL_LOG"
printf '%s\n' '{"ready":true,"state":"active"}'
EOF
cat > "$fake_bin/sshpass" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FOGCAST_SSH_LOG"
count=0
if [ -f "$FOGCAST_SSH_COUNT" ]; then
  count=$(cat "$FOGCAST_SSH_COUNT")
fi
count=$((count + 1))
printf '%s\n' "$count" > "$FOGCAST_SSH_COUNT"
case "$*" in
  *'/tmp/CORENAME'*)
    if [ "$count" -eq 1 ]; then
      printf '%s\n' "$FOGCAST_EXPECTED_CORE"
    else
      printf '%s\n' MENU
    fi
    ;;
  *'load_core'*) : ;;
  *) printf '%s\n' MENU ;;
esac
EOF
chmod 0755 "$fake_bin/curl" "$fake_bin/sshpass"

FOGCAST_CURL_LOG=$curl_log \
FOGCAST_SSH_LOG=$fixture/ssh.log \
FOGCAST_SSH_COUNT=$fixture/ssh.count \
FOGCAST_EXPECTED_CORE=MEGA \
FOGCAST_HOST_API=http://host.test \
FOGCAST_TARGET_API=http://target.test:8182 \
FOGCAST_TARGET_HOST=target.test \
FOGCAST_POLL_ATTEMPTS=2 \
FOGCAST_POLL_INTERVAL=0 \
PATH="$fake_bin:$PATH" \
  sh "$smoke" sonic-2 MEGA

grep -Fq -- 'http://host.test/api/v1/health' "$curl_log"
grep -Fq -- 'http://target.test:8182/v1/health' "$curl_log"
grep -Fq -- 'http://host.test/api/v1/session/launch' "$curl_log"
grep -Fq -- '--data {"game_id":"sonic-2"}' "$curl_log"
grep -Fq -- 'http://host.test/api/v1/session/stop' "$curl_log"
grep -Fq -- '/tmp/CORENAME' "$fixture/ssh.log"
test "$(cat "$fixture/ssh.count")" -eq 2

if PATH="$fake_bin:$PATH" sh "$smoke" bad/game MEGA >/dev/null 2>&1; then
  echo 'target smoke accepted an invalid game ID' >&2
  exit 1
fi

echo 'target smoke tests passed'
