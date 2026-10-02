#!/bin/sh
# Fixture test for the read-only kit diagnostics collector and its ssh wrapper.
# Does not contact a kit. FES_KIT_DIAG_ROOT points at a fake /proc and df.txt.
set -eu

root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
collector=$root/scripts/kit-diagnostics.sh
wrapper=$root/scripts/kit-diagnostics-ssh.sh

fail() {
  printf '%s\n' "$*" >&2
  if [ -s "${err:-}" ]; then
    printf '%s\n' '--- stderr ---' >&2
    cat "$err" >&2
  fi
  exit 1
}

[ -f "$collector" ] || fail 'collector is missing'
[ -f "$wrapper" ] || fail 'wrapper is missing'

sh -n "$collector"
sh -n "$wrapper"

tr_bin=$(command -v tr) || fail 'tr is missing'
sed_bin=$(command -v sed) || fail 'sed is missing'
sh_bin=$(command -v sh) || fail 'sh is missing'

work=$(mktemp -d "${TMPDIR:-/tmp}/kit-diagnostics-test.XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM

tools=$work/tools
bindir=$work/bin
mkdir -p "$tools" "$bindir"
ln -s "$tr_bin" "$tools/tr"
ln -s "$sed_bin" "$tools/sed"

fx=$work/root

write_stat() {
  printf '%s (%s) S 1 1 1 0 -1 0 0 0 0 0 %s %s\n' "$2" "$3" "$4" "$5" > "$1"
}

build_fx() {
  rm -rf "$fx"
  mkdir -p "$fx/proc/12" "$fx/proc/34" "$fx/proc/50" "$fx/proc/60" \
    "$fx/proc/7" "$fx/proc/77" "$fx/proc/88" \
    "$fx/etc" "$fx/usr/share/mister-runtime"
  printf '%s\n' 'c41e58c1' > "$fx/etc/fes-image-id"
  printf '%s\n' 'BUILD_ID=should-not-win' 'IMAGE_ID="also-not"' > "$fx/etc/os-release"
  printf '%s\n' 'Linux version 5.15.11-MiSTer (build@host) #1 SMP PREEMPT deadbeef' \
    > "$fx/proc/version"
  printf '%s\n' '12345.67 999.00' > "$fx/proc/uptime"
  printf '%s\n' '0.01 0.05 0.15 1/100 200' > "$fx/proc/loadavg"
  printf '%s\n' \
    'processor	: 0' \
    'model name	: ARMv7 Processor rev 1 (v7l)' \
    '' \
    'processor	: 1' \
    'model name	: ARMv7 Processor rev 1 (v7l)' \
    > "$fx/proc/cpuinfo"
  printf '%s\n' \
    'MemTotal:        503808 kB' \
    'MemAvailable:    419840 kB' \
    'SwapTotal:            0 kB' \
    'SwapFree:             0 kB' \
    'Buffers:           1024 kB' \
    > "$fx/proc/meminfo"
  printf '%s\n' \
    'cpu  1000 0 500 3000000000 0 0 0 0 0 0' \
    'cpu0 10 0 10 10 0 0 0 0 0 0' \
    'cpu1 10 0 10 10 0 0 0 0 0 0' \
    > "$fx/proc/stat"
  printf '%s\n' \
    'cpu  1006 0 504 3000000190 0 0 0 0 0 0' \
    'cpu0 99 0 99 99 0 0 0 0 0 0' \
    'cpu1 99 0 99 99 0 0 0 0 0 0' \
    > "$fx/proc/stat.sample2"
  printf '%s\n' \
    'Filesystem     1024-blocks      Used Available Capacity Mounted on' \
    '/dev/root          131072     80000     51072      62% /' \
    '/dev/mmcblk0p1    1032192    778240    253952      75% /media/fat' \
    'tmpfs              251904       512    251392       1% /tmp' \
    > "$fx/df.txt"

  printf '%s\n' fogcast-kit > "$fx/proc/12/comm"
  printf '%s\n' 'Name:	fogcast-kit' 'VmRSS:	   20000 kB' > "$fx/proc/12/status"
  printf '%s\0%s\0' /usr/sbin/fogcast-kit --menu-display > "$fx/proc/12/cmdline"
  write_stat "$fx/proc/12/stat" 12 fogcast-kit 1000 0
  write_stat "$fx/proc/12/stat.sample2" 12 fogcast-kit 1045 0

  printf '%s\n' fogcast-tenfoo > "$fx/proc/34/comm"
  printf '%s\n' 'Name:	fogcast-tenfoo' 'VmRSS:	   12345 kB' > "$fx/proc/34/status"
  printf '%s\0%s\0%s\0' /usr/sbin/fogcast-tenfoot -gfx menu-display > "$fx/proc/34/cmdline"
  write_stat "$fx/proc/34/stat" 34 fogcast-tenfoo 3000000000 5
  write_stat "$fx/proc/34/stat.sample2" 34 fogcast-tenfoo 3000000002 5

  printf '%s\n' fogcast-api > "$fx/proc/50/comm"
  printf '%s\n' 'Name:	fogcast-api' 'VmRSS:	    8000 kB' > "$fx/proc/50/status"
  printf '%s\0' fogcast-api > "$fx/proc/50/cmdline"
  write_stat "$fx/proc/50/stat" 50 fogcast-api 8 0
  write_stat "$fx/proc/50/stat.sample2" 50 fogcast-api 8 0

  printf '%s\n' fogcast-zzz > "$fx/proc/60/comm"
  printf '%s\n' 'Name:	fogcast-zzz' 'VmRSS:	    1000 kB' > "$fx/proc/60/status"
  printf '%s\0' /usr/sbin/fogcast-zzz > "$fx/proc/60/cmdline"
  write_stat "$fx/proc/60/stat" 60 fogcast-zzz 3 0
  write_stat "$fx/proc/60/stat.sample2" 60 fogcast-zzz 3 0

  printf '%s\n' mister-agent > "$fx/proc/7/comm"
  printf '%s\n' 'Name:	mister-agent' 'VmRSS:	   99999 kB' > "$fx/proc/7/status"
  printf '%s\0' /usr/sbin/mister-agent > "$fx/proc/7/cmdline"
  write_stat "$fx/proc/7/stat" 7 mister-agent 1 0
  write_stat "$fx/proc/7/stat.sample2" 7 mister-agent 5000 0

  printf '%s\n' fogcast-onlylater > "$fx/proc/77/comm"
  printf '%s\n' 'Name:	fogcast-onlylater' 'VmRSS:	     100 kB' > "$fx/proc/77/status"
  printf '%s\0' /usr/sbin/fogcast-onlylater > "$fx/proc/77/cmdline"
  write_stat "$fx/proc/77/stat.sample2" 77 fogcast-onlylater 50 0

  printf '%s\n' fogcast-onlyearly > "$fx/proc/88/comm"
  printf '%s\n' 'Name:	fogcast-onlyearly' 'VmRSS:	     100 kB' > "$fx/proc/88/status"
  printf '%s\0' /usr/sbin/fogcast-onlyearly > "$fx/proc/88/cmdline"
  write_stat "$fx/proc/88/stat" 88 fogcast-onlyearly 1 0
}

fingerprint() {
  (cd "$1" && find . -type f | sort | while IFS= read -r path; do
    cksum "$path"
  done)
}

run_collector() {
  shell=$1
  (
    unset FES_KIT_DIAG_SAMPLE_SECONDS FES_KIT_DIAG_SSH || true
    PATH=$tools
    FES_KIT_DIAG_ROOT=$fx
    export PATH FES_KIT_DIAG_ROOT
    "$shell" "$collector"
  )
}

assert_eq() {
  got=$1
  want=$2
  label=$3
  if [ "$got" != "$want" ]; then
    printf '%s\n' "$want" > "$work/want"
    printf '%s\n' "$got" > "$work/got"
    printf '%s\n' "mismatch: $label" >&2
    diff -u "$work/want" "$work/got" >&2 || true
    exit 1
  fi
}

value_of() {
  text=$1
  key=$2
  found=
  rest=$text
  nl='
'
  while [ -n "$rest" ]; do
    case $rest in
      *"$nl"*)
        line=${rest%%"$nl"*}
        rest=${rest#*"$nl"}
        ;;
      *)
        line=$rest
        rest=
        ;;
    esac
    case $line in
      "$key"=*)
        found=${line#"$key"=}
        ;;
    esac
  done
  printf '%s\n' "$found"
}

golden=$(cat <<'EOF'
schema=fogcast.kit-diagnostics.v1
image_id=c41e58c1
image_id_source=/etc/fes-image-id
source_revision=
uptime_seconds=12345.67
load_1=0.01
load_5=0.05
load_15=0.15
cpu_count=2
cpu_idle_percent=95
cpu_sample_seconds=0
mem_total_kib=503808
mem_available_kib=419840
swap_total_kib=0
swap_free_kib=0
cma_present=0
cma_total_kib=0
cma_free_kib=0
fs.root.present=1
fs.root.size_kib=131072
fs.root.used_kib=80000
fs.root.free_kib=51072
fs.root.used_percent=62
fs.media_fat.present=1
fs.media_fat.size_kib=1032192
fs.media_fat.used_kib=778240
fs.media_fat.free_kib=253952
fs.media_fat.used_percent=75
fs.tmp.present=1
fs.tmp.size_kib=251904
fs.tmp.used_kib=512
fs.tmp.free_kib=251392
fs.tmp.used_percent=1
process_count=4
process.1.pid=12
process.1.name=fogcast-kit
process.1.cpu_percent=45
process.1.rss_kib=20000
process.2.pid=34
process.2.name=fogcast-tenfoot
process.2.cpu_percent=2
process.2.rss_kib=12345
process.3.pid=50
process.3.name=fogcast-api
process.3.cpu_percent=0
process.3.rss_kib=8000
process.4.pid=60
process.4.name=fogcast-zzz
process.4.cpu_percent=0
process.4.rss_kib=1000
EOF
)

shells=$sh_bin
if dash_bin=$(command -v dash); then
  shells="$sh_bin $dash_bin"
fi

for shell in $shells; do
  build_fx
  before=$(fingerprint "$fx")
  err=$work/err
  got=$(run_collector "$shell" 2>"$err") || fail "$shell collector failed"
  [ ! -s "$err" ] || fail "$shell collector wrote to stderr: $(cat "$err")"
  assert_eq "$got" "$golden" "$shell baseline"
  after=$(fingerprint "$fx")
  assert_eq "$after" "$before" "$shell fixture bytes changed"

  printf '%s\n' 'CmaTotal:         16384 kB' 'CmaFree:          8192 kB' >> "$fx/proc/meminfo"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell cma run failed"
  [ "$(value_of "$got" cma_present)" = 1 ] || fail "$shell cma_present"
  [ "$(value_of "$got" cma_total_kib)" = 16384 ] || fail "$shell cma_total"
  [ "$(value_of "$got" cma_free_kib)" = 8192 ] || fail "$shell cma_free"
  [ "$(value_of "$got" image_id)" = c41e58c1 ] || fail "$shell image id changed"

  build_fx
  rm -f "$fx/etc/fes-image-id"
  printf '%s\n' 'FES_IMAGE_ID="f449886f"' > "$fx/etc/os-release"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell os-release run failed"
  [ "$(value_of "$got" image_id)" = f449886f ] || fail "$shell os-release id"
  [ "$(value_of "$got" image_id_source)" = /etc/os-release ] || fail "$shell os-release source"

  build_fx
  rm -f "$fx/etc/fes-image-id" "$fx/etc/os-release"
  mkdir -p "$fx/etc/fes"
  printf '%s\n' \
    '{' \
    '  "fogcast_revision": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",' \
    '  "image_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",' \
    '  "fes_revision": "0123456789abcdef0123456789abcdef01234567"' \
    '}' > "$fx/etc/fes/factory.json"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell factory run failed"
  [ "$(value_of "$got" image_id)" = bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ] || fail "$shell factory id"
  [ "$(value_of "$got" image_id_source)" = /etc/fes/factory.json ] || fail "$shell factory source"
  [ "$(value_of "$got" source_revision)" = 0123456789abcdef0123456789abcdef01234567 ] || fail "$shell factory revision"

  printf '%s\n' 'c41e58c1' > "$fx/etc/fes-image-id"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell stamp-and-factory run failed"
  [ "$(value_of "$got" image_id)" = c41e58c1 ] || fail "$shell stamp still wins"
  [ "$(value_of "$got" image_id_source)" = /etc/fes-image-id ] || fail "$shell stamp source"
  [ "$(value_of "$got" source_revision)" = 0123456789abcdef0123456789abcdef01234567 ] || fail "$shell revision beside stamp"

  build_fx
  rm -f "$fx/etc/fes-image-id" "$fx/etc/os-release"
  mkdir -p "$fx/.fes-bootstrap/etc/fes"
  printf '%s\n' '{"image_sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","fes_revision":"abcdefabcdefabcdefabcdefabcdefabcdefabcd"}' \
    > "$fx/.fes-bootstrap/etc/fes/factory.json"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell bootstrap factory run failed"
  [ "$(value_of "$got" image_id)" = cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc ] || fail "$shell bootstrap id"
  [ "$(value_of "$got" image_id_source)" = /.fes-bootstrap/etc/fes/factory.json ] || fail "$shell bootstrap source"
  [ "$(value_of "$got" source_revision)" = abcdefabcdefabcdefabcdefabcdefabcdefabcd ] || fail "$shell bootstrap revision"

  printf '%s\n' '{"fes_revision":"abcdefabcdefabcdefabcdefabcdefabcdefabcd"}' \
    > "$fx/.fes-bootstrap/etc/fes/factory.json"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell revision-only run failed"
  [ "$(value_of "$got" image_id)" = deadbeef ] || fail "$shell revision used as image id"
  [ "$(value_of "$got" image_id_source)" = /proc/version ] || fail "$shell revision-only source"
  [ "$(value_of "$got" source_revision)" = abcdefabcdefabcdefabcdefabcdefabcdefabcd ] || fail "$shell revision-only value"

  build_fx
  rm -f "$fx/etc/fes-image-id" "$fx/etc/os-release"
  printf '%s\n' 'Linux version 5.15.11-MiSTer (build@host) #1 SMP PREEMPT f449886f' \
    > "$fx/proc/version"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell version run failed"
  [ "$(value_of "$got" image_id)" = f449886f ] || fail "$shell version id"
  [ "$(value_of "$got" image_id_source)" = /proc/version ] || fail "$shell version source"

  build_fx
  rm -f "$fx/etc/fes-image-id" "$fx/etc/os-release"
  printf '%s\n' 'Linux version 5.15.11-MiSTer (build@host) #1 SMP' > "$fx/proc/version"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell unknown image run failed"
  [ "$(value_of "$got" image_id)" = '' ] || fail "$shell unknown image id"
  [ "$(value_of "$got" image_id_source)" = none ] || fail "$shell unknown image source"

  build_fx
  printf '%s\n' \
    'cpu  0 0 0 0 0 0 0 0 0 0' \
    > "$fx/proc/stat"
  printf '%s\n' \
    'cpu  1 0 0 2 0 0 0 0 0 0' \
    > "$fx/proc/stat.sample2"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell rounding run failed"
  [ "$(value_of "$got" cpu_idle_percent)" = 67 ] || fail "$shell idle rounding"

  build_fx
  printf '%s\n' \
    'Filesystem     1024-blocks      Used Available Capacity Mounted on' \
    '/dev/root          131072     80000     51072      62% /' \
    'tmpfs              251904       512    251392       1% /tmp' \
    > "$fx/df.txt"
  got=$(run_collector "$shell" 2>"$err") || fail "$shell missing fat run failed"
  [ "$(value_of "$got" fs.media_fat.present)" = 0 ] || fail "$shell fat present"
  [ "$(value_of "$got" fs.media_fat.size_kib)" = '' ] || fail "$shell fat size"
  [ "$(value_of "$got" fs.root.present)" = 1 ] || fail "$shell root present"
  [ "$(value_of "$got" fs.tmp.free_kib)" = 251392 ] || fail "$shell tmp free"

  build_fx
  printf '%s\n' 'cpu  10 0 0 10 0 0 0 0 0 0' > "$fx/proc/stat.sample2"
  if run_collector "$shell" >"$work/out" 2>"$err"; then
    fail "$shell backwards counters should fail"
  fi
  grep -q 'backwards' "$err" || fail "$shell backwards stderr"

  build_fx
  rm -f "$fx/proc/meminfo"
  if run_collector "$shell" >"$work/out" 2>"$err"; then
    fail "$shell missing meminfo should fail"
  fi
  grep -q 'meminfo' "$err" || fail "$shell meminfo stderr"

  build_fx
  rm -f "$fx/df.txt"
  before=$(fingerprint "$fx")
  if run_collector "$shell" >"$work/out" 2>"$err"; then
    fail "$shell missing df should fail"
  fi
  after=$(fingerprint "$fx")
  assert_eq "$after" "$before" "$shell failure wrote the fixture"
done

cat > "$bindir/ssh" <<'EOF'
#!/bin/sh
set -eu
log=${FES_KIT_DIAG_FAKE_SSH_LOG:?}
: > "$log"
printf '%s\n' "${0##*/}" >> "$log"
for arg do
  printf '%s\n' "$arg" >> "$log"
  case $arg in
    *reboot*|*scp*|*'u-boot.txt'*|*'>>'*|*';'*) exit 9 ;;
  esac
done
prev3=
prev2=
prev1=
for arg do
  prev3=$prev2
  prev2=$prev1
  prev1=$arg
done
[ "$prev3" = root@192.0.2.10 ] || exit 9
[ "$prev2" = sh ] || exit 9
[ "$prev1" = -s ] || exit 9
export FES_KIT_DIAG_ROOT=${FES_KIT_DIAG_FAKE_ROOT:?}
export PATH=${FES_KIT_DIAG_FAKE_PATH:?}
unset FES_KIT_DIAG_SAMPLE_SECONDS || true
exec "${FES_KIT_DIAG_FAKE_SH:?}" -s
EOF
cp "$bindir/ssh" "$bindir/ssh-prefix"
cat > "$bindir/sshpass" <<'EOF'
#!/bin/sh
set -eu
[ "${1:-}" = -p ] || exit 9
[ "${2:-}" = 1 ] || exit 9
shift 2
exec "$@"
EOF
chmod 755 "$bindir/ssh" "$bindir/ssh-prefix" "$bindir/sshpass"

build_fx
export FES_KIT_DIAG_FAKE_ROOT="$fx"
export FES_KIT_DIAG_FAKE_PATH="$tools"
export FES_KIT_DIAG_FAKE_SSH_LOG="$work/ssh.log"
export FES_KIT_DIAG_FAKE_SH="$sh_bin"
before=$(fingerprint "$fx")

got=$(PATH="$bindir:/usr/bin:/bin" sh "$wrapper" 192.0.2.10 2>"$work/err") || fail 'wrapper failed'
[ ! -s "$work/err" ] || fail "wrapper stderr: $(cat "$work/err")"
assert_eq "$got" "$golden" 'wrapper output'
assert_eq "$(cat "$work/ssh.log")" "$(cat <<'EOF'
ssh
-o
BatchMode=yes
-o
ConnectTimeout=8
-o
StrictHostKeyChecking=no
-o
UserKnownHostsFile=/dev/null
root@192.0.2.10
sh
-s
EOF
)" 'default ssh argv'
after=$(fingerprint "$fx")
assert_eq "$after" "$before" 'wrapper wrote the fixture'

got=$(FES_KIT_DIAG_SSH="$bindir/ssh-prefix" PATH="/usr/bin:/bin" sh "$wrapper" 192.0.2.10 2>"$work/err") \
  || fail 'wrapper prefix failed'
assert_eq "$got" "$golden" 'wrapper prefix output'
assert_eq "$(cat "$work/ssh.log")" "$(printf '%s\n' \
  ssh-prefix \
  -o ConnectTimeout=8 \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  root@192.0.2.10 \
  sh -s)" 'prefix ssh argv'

got=$(FES_KIT_DIAG_SSH="$bindir/sshpass -p 1 $bindir/ssh" PATH="/usr/bin:/bin" \
  sh "$wrapper" 192.0.2.10 2>"$work/err") || fail 'wrapper sshpass prefix failed'
assert_eq "$got" "$golden" 'wrapper sshpass output'
assert_eq "$(cat "$work/ssh.log")" "$(printf '%s\n' \
  ssh \
  -o ConnectTimeout=8 \
  -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null \
  root@192.0.2.10 \
  sh -s)" 'sshpass prefix argv'

unset FES_KIT_DIAG_SSH || true
rm -f "$work/ssh.log"
for bad in '' '-oProxyCommand=touch' '192.168.10.84;reboot' 'root@192.0.2.10' 'bad host' '..' ; do
  if PATH="$bindir:/usr/bin:/bin" sh "$wrapper" "$bad" >"$work/out" 2>"$work/err"; then
    fail "wrapper accepted bad host: $bad"
  fi
  [ ! -f "$work/ssh.log" ] || fail "wrapper called ssh for bad host: $bad"
done

audit() {
  file=$1
  while IFS= read -r line || [ -n "$line" ]; do
    trim=$line
    trim=${trim#"${trim%%[![:space:]]*}"}
    case $trim in
      ''|'#'*) continue ;;
    esac
    case $trim in
      *'>>'*|*'scp '*|*'reboot'*|*'mktemp'*|*'u-boot.txt'*|*'tee '*)
        fail "forbidden text in $file: $trim"
        ;;
    esac
    rest=$trim
    while [ -n "$rest" ]; do
      case $rest in
        *'>'*)
          rest=${rest#*>}
          case $rest in
            '&'*) ;;
            *) fail "redirection in $file: $trim" ;;
          esac
          ;;
        *) break ;;
      esac
    done
  done < "$file"
}

: > "$work/err"
audit "$collector"
audit "$wrapper"

printf '%s\n' 'kit-diagnostics fixture tests passed'
