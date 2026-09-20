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
    printf '%s\n' "$game_id" > "$FOGCAST_ACTIVE_GAME"
    printf '{"state":"active","game_id":"%s","core_package":{"package_id":"%s"}}\n' "$game_id" "$package_id"
    ;;
  */api/v1/session)
    if [ -f "$FOGCAST_ACTIVE_GAME" ]; then
      game_id=$(cat "$FOGCAST_ACTIVE_GAME")
      case "$game_id" in
        pong-game) package_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ;;
        zx81-game) package_id=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
        coleco-game) package_id=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc ;;
      esac
      printf '{"state":"active","game_id":"%s","core_package":{"package_id":"%s"}}\n' "$game_id" "$package_id"
    else
      printf '%s\n' '{"state":"idle"}'
    fi
    ;;
  */api/v1/session/stop)
    rm -f "$FOGCAST_ACTIVE_GAME"
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

if FES_PACKAGE_EXPECTED_IDS='fes.pong=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,fes.zx81=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb' \
  FOGCAST_POLL_ATTEMPTS=1 PATH="$fake_bin:$PATH" sh "$smoke" >/dev/null 2>&1; then
  printf '%s\n' 'package smoke accepted an incomplete package set' >&2
  exit 1
fi

printf '%s\n' 'package runtime smoke tests passed'
