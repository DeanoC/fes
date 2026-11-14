# HIL harness guards: three failures of 14 November 2026, and what each guard prevents

Issue [#1849](https://github.com/DeanoC/fes/issues/1849). On one day, three
session harnesses failed in ways that cost more than a retry. None of them was
a kit defect; all three were in the session's own wrapper or driver, which are
per-session copies that live on Powerboat and not in this repository, so no
test could hold them. This record says what each failure was, what the guard
against it refuses, and which of the things the incidents implied turned out
not to be true.

## The three failures

| # | Incident | What it cost |
| --- | --- | --- |
| 1 | The #1368 session's command file had no final `{"op": "quit"}`, so the driver sat in its silent 30-minute idle wait after the last command. Read as a dead wrapper. | Nothing by itself — but it led straight to 2. |
| 2 | Reading the wrapper as dead, the session ran the wrapper's post-run tail by hand while the pc386 core was still loaded. The original wrapper had been alive throughout. | The agent's stop/close returned HTTP 500 and the lease went `blocked` with "kit cleanup failed or agent shutting down; operator recovery required". Second Kit B operator recovery that day. |
| 3 | The i386 x87 slice 2 run packed its `.fcore` with a plain `tar cf`. The kit refused it at admission (HTTP 422 `INVALID_ARCHIVE`), but the driver recorded the error and ran the rest of the command script. | A void kit run that read as a pass: plausible frames, `rc=0`, nothing of the new core ever loaded. Only `runs/<run>/result.json` showed it. |

The first Kit B recovery that day was the #1494 session's read-only ADV7513
register poll overlapping the runtime's HDMI quiesce during a core stop; it is
recorded in
[the Serpent Isle black capture record](../2026-11-14-pc386-si-black-capture/summary.md)
and is not one of these three. Its cause is open and it is out of this PR's
scope: it is a contention between two readers of the same bus, not a harness
sequencing mistake, and the runtime uses the same `/dev/i2c` SMBus path, so the
I²C-arbitration attribution it was first given does not hold.

The two records that are not on `main` are cited by PR, since their paths are
on their own branches: the #1368 U6/U7 reference record on
[PR #1838](https://github.com/DeanoC/fes/pull/1838) and the x87 slice 2 record
on [PR #1821](https://github.com/DeanoC/fes/pull/1821).

## The guards, and what each refuses

All four live in `sources/misteross/scripts/kit_hil.py`, beside the lease
client `kit.py` the session drivers already import, and are checked in
`sources/misteross/tests/test_kit_hil.py`.

1. **End of run.** `drain` takes a mode, because the two kinds of run want
   different answers. `mode='batch'` — a command file complete before the run
   starts — ends the run when the file is exhausted, so a missing `quit` costs
   nothing; the `quit` op still works and stays the habit. `mode='live'` — the
   console whose ops are appended while the run watches frames — still waits,
   because that wait *is* the design, but it is bounded at five minutes instead
   of thirty and says every heartbeat which command it is waiting for, that the
   command file has no `quit`, that the driver is alive and holds the kit, and
   when it will end the run by itself. Exit-on-exhaustion as the only behaviour
   was considered and rejected: it would end every live run immediately after
   its last queued command.
   *Prevents:* a session concluding from silence that its wrapper is dead.

2. **The post-run tail.** `cleanup_gate`, and `kit_hil.py guard-idle` for the
   shell wrappers, is called before a tail removes anything. It fails closed:
   an unreachable agent, an agent that is not `idle`, a lease that is not
   `free`, and any loaded core each refuse, and the refusal names the loaded
   core (`core_id` and `package_id`) and the lease's owner, purpose and reason.
   A `revoking` lease — what a clean close leaves for a few seconds — is waited
   out for 30 s rather than refused. The guard is a separate pre-check and not
   something the agent enforces, because the removal is `ssh rm -rf` against
   the kit's FogCast data directories: the agent never sees it.
   *Prevents:* a data-tree removal under a loaded core, which is what blocked
   the lease.

3. **The run's verdict.** `drain` stops the command script at the first command
   whose result carries an error, so nothing runs over a core that never
   loaded, and reports it in the run's `reasons` rather than through the
   driver's `halted` key — `halted` makes the driver skip `session.close()`,
   which leaves the kit `IN USE` and is itself an operator recovery.
   `verdict`, and `kit_hil.py verdict runs/RUN/result.json`, reads the error
   back out of `result.json` (a refused command, a `halted`, a `close_error`,
   an error nested in a reply) and exits non-zero. The invariant: no run
   reports success with an error in its `result.json`.
   *Prevents:* a refused admission reading as a pass.

4. **Liveness.** `run_lock` is a per-run lock file, `kit-hil-run.lock`, that
   the driver flocks for the whole run with its pid and command line inside;
   `lock_holder` and `kit_hil.py alive` answer "is my driver alive?" with a
   non-blocking `flock` attempt plus `/proc/<pid>/cmdline`. `program_pids` is
   the `pgrep -x` form, which never returns this process or its parent, for
   what a lock cannot cover. The kit lock cannot serve as the run lock: the
   Lander queue takes `~/kit-status/kitA.lock` and
   `~/fes-hil/locks/lander-kitB.lock` too.
   *Prevents:* the `pgrep -af run-u6ref.sh` self-match that started failure 1.

## Evidence: the tests fail before the guards

The guarded loop is not in the repository, so it was brought in first as a
behaviour-for-behaviour extraction of the session drivers' loop and liveness
check, with the 22 incident cases red against it:
commit `ddc1e3c21`, 1 failure and 21 errors. On that extraction the batch run
with no `quit` exits through `no command for 30 minutes`; the refused `load`
leaves the rest of the script running (`['load', 'grab', 'grab']` instead of
`['load']`); and the cleanup gate, the verdict and the run lock do not exist.
The guards are commit `2fc81f231`, after which all 22 pass:

    cd sources/misteross && python3 -m unittest discover -s tests -p 'test_kit*.py'

The cleanup gate is also exercised against a real local HTTP agent replying in
`protocol.Status`'s own shape (`state`, `game_id`, `core_package.package_id`,
`core_package.core_id`), not only against a fake client, so a wire-shape change
breaks the test rather than the next kit run.

## What the incidents implied that turned out not to be true

- **"The x87 session's fix covers the driver."** It does not. The fix is in
  that session's own wrapper (`~/fes-scratch/x87s2/kit/run-x87.sh`, which
  greps `result.json` for `"error"` and sets `rc=99`). Its driver,
  `hil_x87.py`, is byte-identical to `hil_games.py` in the loop that swallows a
  command error and carries on, and neither `kit.py` nor anything else in the
  repository changed. Nothing here duplicates that work; this PR is the first
  repository-side guard.
- **"The post-run tail had no idea the kit might be busy."** The standalone
  housekeeping script `clean-r.sh` does check that the status file logs the kit
  FREE and that the agent is idle with a free lease before it removes anything.
  The hand-run tail `post-g1.sh` goes straight to `ssh rm -rf` and probes the
  lease only afterwards. So the guard existed in one copy of the pair and not
  the other, which is exactly why it belongs in a shared, tested place. Neither
  copy checks for a loaded core specifically; the new gate does.
- **"A blocked lease needs an operator because it is persisted."** The lease
  state is in memory only: `kitlease.New` starts at `State: "free"`
  (`sources/FogCast/internal/kitlease/manager.go:79`) and nothing in that
  package reads a persisted lease status back, so restarting the agent clears a
  blocked lease. Maintenance reservations are the exception — they are
  persisted by `maintenance_store.go`, and `idle_maintenance.go` requires the
  caller to reinstall intent after a restart. The reason strings distinguish
  the two cases: `manager.go:468` for cleanup failure against `:441` and
  `:459` for maintenance. Verified, as the brief asked.
- **Not implied by the incidents, found while fixing them:** a new file under
  `sources/misteross/scripts/` falls to the `sources/misteross/scripts/**`
  rule and asks for an `OVERLAY core:ALL` HIL — an overlay run of every core —
  for a change that deploys nothing. `hil_plan.py` now classifies
  `kit_hil.py` as `none`, with a test. The lease client `kit.py` beside it
  keeps its overlay classification: the import graph reaches it from
  `pc386_wfw_kit_acceptance.py`, so that one is not a misclassification.

## Reach, and what is not done

The drivers and wrappers on Powerboat are per-session copies that import
`kit.py` by absolute path from whichever worktree made them. These guards
therefore protect a run only once its driver and wrapper call them; this PR
changes no file under `~/fes-scratch`, and the live `u6ref` session's tree was
not touched. Re-pointing the existing copies, and making the next session's
driver a thin caller of `drain` instead of a 566-line copy, is follow-up work.

No kit was used. All four guards are provable against a fake agent and a local
HTTP server, which is why they are unit tests rather than a kit run; the
`cleanup_gate` wire-shape case is the one that would otherwise have needed a
kit, and it is covered by replying as the agent replies.
