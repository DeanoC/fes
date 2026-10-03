package rebootguard

// fallbackScript is the detached delayed `reboot -f` helper. It ignores the
// TERM/HUP/INT that init and the stop scripts send, so it survives everything
// until init's final kill(-1, SIGKILL). It forces a reboot only when shutdown
// has visibly stalled: no change in block-device I/O counters (mmcblk*/loop*
// lines of diskstats, which move while services, sync or unmount still write
// to the card) for STALL seconds. While I/O keeps progressing it waits, up to
// the stated hard DEADLINE. The hardware watchdog (timeout > DEADLINE) is the
// final backstop for stalls after kill-all and inside the kernel.
//
// Before forcing, it runs an explicit sync for at most SYNCWAIT seconds (a sync
// that is itself stuck must not hold the fallback). BusyBox `reboot -f` then
// syncs again unless given -n, which is never passed here.
//
// Budget (#429): the stop scripts' no-I/O waits are capped at 5 s each
// (S60fogcast-kit, S50mister-agent, S40mister-runtime, S20mister-network),
// 20 s in total, below the 30 s STALL; mister-supervise's own 5+2 s wait runs
// inside its stop script's 5 s window. image/scripts/tests/shutdown-budget_test.sh
// enforces this.
//
// Arguments: STALL DEADLINE POLL PROC REBOOT SYNCWAIT.
const fallbackScript = `trap '' TERM HUP INT
stall=$1 deadline=$2 poll=$3 proc=$4 reboot=$5 syncwait=$6
progress() { grep -E ' (mmcblk|loop)[0-9]' "$proc/diskstats" 2>/dev/null; }
last=$(progress)
still=0
elapsed=0
while [ "$elapsed" -lt "$deadline" ]; do
  sleep "$poll"
  elapsed=$((elapsed + poll))
  now=$(progress)
  if [ "$now" = "$last" ]; then
    still=$((still + poll))
    [ "$still" -lt "$stall" ] || break
  else
    still=0
    last=$now
  fi
done
sync &
sync_pid=$!
waited=0
while [ "$waited" -lt "$syncwait" ] && kill -0 "$sync_pid" 2>/dev/null; do
  sleep 1
  waited=$((waited + 1))
done
exec "$reboot" -f
`
