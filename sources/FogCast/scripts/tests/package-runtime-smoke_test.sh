#!/bin/sh
set -eu

repo=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/fogcast-package-runtime-smoke-test.XXXXXX")
trap 'rm -rf "$fixture"' EXIT INT TERM

smoke=$repo/scripts/package-runtime-smoke.sh
[ -x "$smoke" ] || {
  printf '%s\n' 'package runtime smoke runner is missing' >&2
  exit 1
}

fake_bin=$fixture/bin
mkdir -p "$fake_bin"
cat > "$fake_bin/curl" <<'EOF'
#!/bin/sh
set -eu

printf '%s\n' "$*" >> "$FOGCAST_CURL_LOG"
url=
body=
for argument do
  url=$argument
done
previous=
for argument do
  if [ "$previous" = '--data' ]; then
    body=$argument
  fi
  previous=$argument
done

case "$url" in
  */api/v1/health)
    printf '%s\n' '{"ready":true,"host":{"revision":"rev-fes"},"target":{"reachable":true,"ready":true,"artifacts":{"agent_revision":"rev-fes"}}}'
    ;;
  */api/v1/core-packages)
    printf '%s\n' '{"packages":[
      {"package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","descriptor":{"core":{"id":"fes.pong"}},"entries":[{"game_id":"pong-game","core_id":"fes.pong","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},
      {"package_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","descriptor":{"core":{"id":"fes.zx81"}},"entries":[{"game_id":"zx81-game","core_id":"fes.zx81","package_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},
      {"package_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","descriptor":{"core":{"id":"fes.coleco"}},"entries":[{"game_id":"coleco-game","core_id":"fes.coleco","package_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}]}
    ]}'
    ;;
  */api/v1/library/core-entries)
    printf '%s\n' '{"entries":[
      {"game_id":"pong-game","core_id":"fes.pong","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
      {"game_id":"zx81-game","core_id":"fes.zx81","package_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
      {"game_id":"coleco-game","core_id":"fes.coleco","package_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
    ]}'
    ;;
  */api/v1/session/launch)
    case "$body" in
      *pong-game*) game_id=pong-game; package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
      *zx81-game*) game_id=zx81-game; package_id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
      *coleco-game*) game_id=coleco-game; package_id=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc ;;
      *) exit 22 ;;
    esac
    if [ "$game_id" = zx81-game ] && [ "${FOGCAST_TIMEOUT_ZX81:-0}" = 1 ]; then
      if [ "${FOGCAST_FOREIGN_ON_TIMEOUT:-0}" = 1 ]; then
        printf '%s\n' foreign-game > "$FOGCAST_ACTIVE_GAME"
      elif [ "${FOGCAST_IDLE_ON_TIMEOUT:-0}" = 1 ]; then
        : > "$FOGCAST_HELD_LEASE"
      else
        printf '%s\n' "$game_id" > "$FOGCAST_PENDING_GAME"
      fi
      exit 28
    fi
    printf '%s\n' "$game_id" > "$FOGCAST_ACTIVE_GAME"
    if [ "$game_id" = zx81-game ] && [ "${FOGCAST_SLOW_ZX81:-0}" = 1 ]; then
      max_time=0
      previous=
      for argument do
        if [ "$previous" = '--max-time' ]; then
          max_time=$argument
        fi
        previous=$argument
      done
      if [ "$max_time" -lt 35 ]; then
        exit 28
      fi
    fi
    printf '{"state":"active","game_id":"%s","core_package":{"package_id":"%s"}}\n' "$game_id" "$package_id"
    ;;
  */api/v1/session)
    if [ -n "${FOGCAST_PENDING_GAME:-}" ] && [ -f "$FOGCAST_PENDING_GAME" ]; then
      polls=0
      if [ -f "$FOGCAST_SESSION_POLL_COUNT" ]; then
        polls=$(cat "$FOGCAST_SESSION_POLL_COUNT")
      fi
      polls=$((polls + 1))
      printf '%s\n' "$polls" > "$FOGCAST_SESSION_POLL_COUNT"
      if [ "$polls" -ge 2 ]; then
        mv "$FOGCAST_PENDING_GAME" "$FOGCAST_ACTIVE_GAME"
      fi
    fi
    if [ -f "$FOGCAST_ACTIVE_GAME" ]; then
      game_id=$(cat "$FOGCAST_ACTIVE_GAME")
      case "$game_id" in
        pong-game) package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
        zx81-game) package_id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
        coleco-game) package_id=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc ;;
        foreign-game) package_id=dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd ;;
      esac
      if [ "$game_id" = zx81-game ] && [ "${FOGCAST_TIMEOUT_ZX81:-0}" = 1 ]; then
        # A cancelled launch can activate without a library game_id in status.
        printf '{"state":"active","core_package":{"package_id":"%s"}}\n' "$package_id"
      else
        printf '{"state":"active","game_id":"%s","core_package":{"package_id":"%s"}}\n' "$game_id" "$package_id"
      fi
    else
      printf '%s\n' '{"state":"idle"}'
    fi
    ;;
  */api/v1/session/stop)
    rm -f "$FOGCAST_ACTIVE_GAME"
    if [ -n "${FOGCAST_HELD_LEASE:-}" ]; then
      rm -f "$FOGCAST_HELD_LEASE"
    fi
    printf '%s\n' '{"state":"idle"}'
    ;;
  *) exit 22 ;;
esac
EOF
chmod 0755 "$fake_bin/curl"

FOGCAST_CURL_LOG=$fixture/curl.log \
FOGCAST_ACTIVE_GAME=$fixture/active-game \
FOGCAST_HOST_API=http://host.test \
FES_PACKAGE_EXPECTED_IDS='fes.pong=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,fes.zx81=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb,fes.coleco=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc' \
FOGCAST_POLL_ATTEMPTS=2 \
FOGCAST_POLL_INTERVAL=0 \
PATH="$fake_bin:$PATH" \
  sh "$smoke"

grep -Fq -- 'http://host.test/api/v1/health' "$fixture/curl.log"
grep -Fq -- 'http://host.test/api/v1/core-packages' "$fixture/curl.log"
grep -Fq -- 'http://host.test/api/v1/library/core-entries' "$fixture/curl.log"
test "$(grep -Fc -- 'api/v1/session/launch' "$fixture/curl.log")" -eq 3
test "$(grep -Fc -- 'api/v1/session/stop' "$fixture/curl.log")" -eq 3

rm -f "$fixture/curl.log"
FOGCAST_CURL_LOG=$fixture/curl.log \
FOGCAST_ACTIVE_GAME=$fixture/active-game \
FOGCAST_SLOW_ZX81=1 \
FOGCAST_HOST_API=http://host.test \
FES_PACKAGE_EXPECTED_IDS='fes.pong=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,fes.zx81=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb,fes.coleco=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc' \
FOGCAST_POLL_ATTEMPTS=2 \
FOGCAST_POLL_INTERVAL=0 \
PATH="$fake_bin:$PATH" \
  sh "$smoke"
test ! -f "$fixture/active-game"

rm -f "$fixture/curl.log"
if FOGCAST_CURL_LOG=$fixture/curl.log \
  FOGCAST_ACTIVE_GAME=$fixture/active-game \
  FOGCAST_PENDING_GAME=$fixture/pending-game \
  FOGCAST_SESSION_POLL_COUNT=$fixture/session-polls \
  FOGCAST_TIMEOUT_ZX81=1 \
  FOGCAST_HOST_API=http://host.test \
  FES_PACKAGE_EXPECTED_IDS='fes.pong=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,fes.zx81=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb,fes.coleco=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc' \
  FOGCAST_POLL_ATTEMPTS=3 \
  FOGCAST_POLL_INTERVAL=0 \
  PATH="$fake_bin:$PATH" \
  sh "$smoke" >/dev/null 2>&1; then
  printf '%s\n' 'package smoke accepted a timed-out ZX81 launch' >&2
  exit 1
fi
test ! -f "$fixture/pending-game"
test ! -f "$fixture/active-game"

rm -f "$fixture/curl.log" "$fixture/session-polls"
if FOGCAST_CURL_LOG=$fixture/curl.log \
  FOGCAST_ACTIVE_GAME=$fixture/active-game \
  FOGCAST_PENDING_GAME=$fixture/pending-game \
  FOGCAST_SESSION_POLL_COUNT=$fixture/session-polls \
  FOGCAST_HELD_LEASE=$fixture/held-lease \
  FOGCAST_TIMEOUT_ZX81=1 \
  FOGCAST_IDLE_ON_TIMEOUT=1 \
  FOGCAST_HOST_API=http://host.test \
  FES_PACKAGE_EXPECTED_IDS='fes.pong=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,fes.zx81=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb,fes.coleco=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc' \
  FOGCAST_POLL_ATTEMPTS=2 \
  FOGCAST_POLL_INTERVAL=0 \
  PATH="$fake_bin:$PATH" \
  sh "$smoke" >/dev/null 2>&1; then
  printf '%s\n' 'package smoke accepted a timed-out idle launch' >&2
  exit 1
fi
test ! -f "$fixture/held-lease"
test "$(grep -Fc -- 'api/v1/session/stop' "$fixture/curl.log")" -eq 2

rm -f "$fixture/curl.log" "$fixture/session-polls"
if FOGCAST_CURL_LOG=$fixture/curl.log \
  FOGCAST_ACTIVE_GAME=$fixture/active-game \
  FOGCAST_PENDING_GAME=$fixture/pending-game \
  FOGCAST_SESSION_POLL_COUNT=$fixture/session-polls \
  FOGCAST_TIMEOUT_ZX81=1 \
  FOGCAST_FOREIGN_ON_TIMEOUT=1 \
  FOGCAST_HOST_API=http://host.test \
  FES_PACKAGE_EXPECTED_IDS='fes.pong=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,fes.zx81=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb,fes.coleco=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc' \
  FOGCAST_POLL_ATTEMPTS=3 \
  FOGCAST_POLL_INTERVAL=0 \
  PATH="$fake_bin:$PATH" \
  sh "$smoke" >/dev/null 2>&1; then
  printf '%s\n' 'package smoke accepted a timed-out launch beside a foreign session' >&2
  exit 1
fi
test "$(cat "$fixture/active-game")" = foreign-game
test "$(grep -Fc -- 'api/v1/session/stop' "$fixture/curl.log")" -eq 1

if FES_PACKAGE_EXPECTED_IDS='fes.pong=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,fes.zx81=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \
  FOGCAST_POLL_ATTEMPTS=1 PATH="$fake_bin:$PATH" sh "$smoke" >/dev/null 2>&1; then
  printf '%s\n' 'package smoke accepted an incomplete package set' >&2
  exit 1
fi

printf '%s\n' 'package runtime smoke tests passed'
