#!/bin/sh
# #429 shutdown budget: the sysrq b / reboot -nf fallback must never cut off a shutdown
# that is still making progress. Its I/O-stall trigger (FallbackStall) must be
# longer than the worst-case no-I/O time of the capped rcK stop waits, and its
# shortest deadline plus bounded sync plus margin must fit inside any watchdog
# timeout the agent keeps.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root=$here/../../..
initd=$root/image/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d
guard=$root/sources/FogCast/internal/rebootguard/guard_linux.go

fail() { printf 'shutdown-budget: %s\n' "$1" >&2; exit 1; }

# Each stop path's wait is a `stop_wait=N` countdown of 1 s sleeps; scripts
# without one must not sleep in stop at all.
total=0
for script in "$initd"/S*; do
  name=${script##*/}
  waits=$(awk -F= '/^[[:space:]]*stop_wait=[0-9]+[[:space:]]*$/ {print $2}' "$script")
  first=1
  for w in $waits; do
    # Only the first countdown is the stop_service wait; later ones (e.g. the
    # supervisor's own) are not in these scripts. Enforce the 5 s cap.
    [ "$w" -le 5 ] || fail "$name stop wait $w s exceeds the 5 s cap"
    if [ "$first" = 1 ]; then total=$((total + w)); first=0; fi
  done
done
supervise=$initd/../../usr/sbin/mister-supervise
grep -q '/bin/sleep 5' "$supervise" || fail 'mister-supervise escalator is not 5 s'
grep -q '/bin/sleep 2' "$supervise" || fail 'mister-supervise unkillable window is not 2 s'

value() { # value NAME -> seconds of `c.NAME = N * time.Second` in guard_linux.go
  awk -v n="$1" '$0 ~ "c\\." n " = [0-9]+ \\* time.Second" {gsub(/[^0-9]/, "", $3); print $3; exit}' "$guard"
}
stall=$(value FallbackStall) deadline=$(value FallbackDeadline) min=$(value FallbackMinDeadline)
sync=$(value FallbackSyncWait) margin=$(value WatchdogMargin) wd=$(value WatchdogTimeout)
for v in stall deadline min sync margin wd; do
  eval "x=\$$v"; [ -n "$x" ] || fail "could not read $v from guard_linux.go"
done
[ "$total" -lt "$stall" ] || fail "capped stop waits ${total}s are not below the ${stall}s I/O-stall trigger"
[ "$min" -gt "$total" ] || fail "minimum fallback deadline ${min}s does not exceed stop waits ${total}s"
[ $((deadline + sync + margin)) -le "$wd" ] || fail "fallback ${deadline}+${sync}+${margin}s does not fit the ${wd}s watchdog"
printf 'shutdown budget ok: stop waits %ss < stall %ss; fallback %s..%ss + sync %ss + margin %ss <= watchdog %ss\n' \
  "$total" "$stall" "$min" "$deadline" "$sync" "$margin" "$wd"
