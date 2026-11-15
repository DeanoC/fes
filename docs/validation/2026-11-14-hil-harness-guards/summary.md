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
| 2 | Reading the wrapper as dead, the session ran the wrapper's post-run tail by hand while the pc386 core was still loaded. The original wrapper had been alive throughout. | The agent's own cleanup then failed with HTTP 500 and the lease went `blocked` with "kit cleanup failed or agent shutting down; operator recovery required". Second Kit B operator recovery that day. The [#1368 record](https://github.com/DeanoC/fes/pull/1838) calls the early removal the likely cause of that HTTP 500 and says it is not shown; this record keeps that distinction. |
| 3 | The i386 x87 slice 2 run packed its `.fcore` with a plain `tar cf`. The kit refused it at admission (HTTP 422 `INVALID_ARCHIVE`), but the driver recorded the error and ran the rest of the command script. | A void kit run that read as a pass: plausible frames, `rc=0`, nothing of the new core ever loaded. Only `runs/<run>/result.json` showed it. |

The first Kit B recovery that day is not one of these three, and the shape it
was first given does not survive its own record. The `b3` run's last `stop`
reported `recovery: reboot_required` and the ADV7513 became unreadable, with
that session's register poll running around it — but
[the Serpent Isle black capture record](../2026-11-14-pc386-si-black-capture/summary.md)
establishes that the poll cannot be the other master (both it and the runtime
go through `/dev/i2c` SMBus ioctls, which the kernel serialises per adapter),
and that the `lost arbitration` line *precedes* the poll's first read, which
succeeded. The cause is **not classified** there, and nothing in it is a
harness sequencing mistake, so no guard here addresses it. The one harness-side
effect that record does name — 43 consecutive 4 s read timeouts holding the
adapter lock for about three minutes — it offers as prudence about polls during
load and stop windows, explicitly not as a lesson derived from the failure.

The two records that are not on `main` are cited by PR, since their paths are
on their own branches, both verified with `git branch -r --contains`:
`docs/validation/2026-11-14-pc386-u6-u7-reference/` on
`origin/deano/blissful-maxwell-b9ef1e`
([PR #1838](https://github.com/DeanoC/fes/pull/1838)) and
`docs/validation/2026-11-14-i386-x87-s2/summary.md` on
`origin/deano/i386-x87-s2`
([PR #1821](https://github.com/DeanoC/fes/pull/1821)).

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
   `guard-idle` reads the kit's state and claims nothing, so it is an
   observation and not an enforcement — see the correction below. The only
   caller that removes anything is `kit_hil.py clean`, which holds the lease
   across its whole listing and removal, and which
   takes the step the wrappers each hand-rolled: it reads the run's own
   `kit-new-tops.txt` and `kit-new-paths.txt`, gates, removes exactly those
   trees, checks the `find` diff is exactly them with nothing appeared, and
   gates again. It refuses a path the run did not create, a path outside
   FogCast's three data directories, a data root itself, a path that would not
   survive shell quoting (`'`, `$`, `*`, a `..` segment), a `--keep` path such
   as the shared scrub image, an empty list, a tree already gone, and a diff
   bigger than the listed trees. Without `--apply` it removes nothing. These
   are the checks `clean-r.sh` grew by hand and the wrapper tails never got,
   in one tested place; 19 cases cover them, each red before the command
   existed.
   *Prevents:* a data-tree removal under a loaded core, the step that preceded
   the blocked lease — and, now, the removal of anything the run did not
   make. The gate also refuses a kit whose status carries a
   `recovery` (a `stop` that answered `reboot_required`, as run `b3`'s did),
   because that kit already needs attention. It does not refuse on
   `last_error`: whether the run passed is the verdict's question, not the
   gate's.

3. **The run's verdict.** `drain` stops the command script at the first command
   whose result carries an error, so nothing runs over a core that never
   loaded, and reports it in the run's `reasons` rather than through the
   driver's `halted` key — `halted` makes the driver skip `session.close()`,
   which leaves the kit `IN USE` and is itself an operator recovery.
   `verdict`, and `kit_hil.py verdict runs/RUN/result.json`, reads the error
   back out of `result.json` (a refused command, a `halted`, a `close_error`,
   an error nested in a reply) and exits non-zero. The invariant: no run
   reports success with an error in its `result.json`.
   `drain` catches what the ops raise as well as what they return: `call`
   turns any HTTP status over 300 into a `KitError`
   (`hil_games.py:193`) and an unknown op raises `SystemExit`, so a driver can
   hand its own `run` straight in. A `stop` that answers
   `{"recovery": "reboot_required"}` reports no error at all, and `halting`
   ends the run on that too, which is what the drivers' own `halted` key did.
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
(The extraction catches `Exception` where the drivers catch
`(Exception, SystemExit)`, so the unknown-command case errors there rather than
failing; both are red.) The guards are commit `2fc81f231`, and the cases added
after checking them against the real data are red on the extraction too. All of
them pass on the branch head:

    cd sources/misteross && python3 -m unittest discover -s tests -p 'test_kit*.py'
    # 77 tests, OK (62 of them this module's)

The cleanup gate is also exercised against a real local HTTP agent replying in
`protocol.Status`'s own shape (`state`, `game_id`, `core_package.package_id`,
`core_package.core_id`), not only against a fake client, so a wire-shape change
breaks the test rather than the next kit run.

## The verdict against every kit run on this host

`kit_hil.py verdict` was run over all 129 `result.json` files under
`~/fes-scratch/*/runs/` and `~/fes-scratch/*/kit/runs/`. 120 exit 0 and 9 exit
non-zero, and every one of the 9 names a real failed command, a halt or a
failed release — no false positive from `error_in`'s depth search (an
`audio_stop` reporting `{'levels': {'error': 'no wav'}}` is the kind of nested
error it is meant to catch, and no run tripped on anything else).

The three incident runs are among them: `x87s2/kit/runs/x5b` (the
`INVALID_ARCHIVE` admission refusal), `u6ref/kit/runs/g4`
(`close_error: target rejected request (HTTP 500)`) and `si1494/kit/runs/b3`
(`halted: stop reported reboot_required`).

The other six are the finding: **seven of the nine runs with a real command
error were logged as clean runs with `HIL rc=0` and the kit released.**

| run | what `verdict` reports | what the wrapper logged |
| --- | --- | --- |
| `x87s2/.../x5b` | command 2 `load`: HTTP 422 `INVALID_ARCHIVE` | `HIL rc=0`, RELEASED |
| `p5dbg/.../k2` | command 63 `load`: `BrokenPipeError` | `HIL rc=0`, RELEASED |
| `p5dbg/.../k3` | commands 3 and 33 `load`: `BrokenPipeError` | `HIL rc=0`, RELEASED |
| `p5dbg/.../k4` | command 3 `load`: `BrokenPipeError` | `HIL rc=0`, RELEASED |
| `p5v86/.../k2` | command 63 `load`: `BrokenPipeError` | `HIL rc=0`, RELEASED |
| `ring3/.../r1` | commands 1 and 28 `load`: `BrokenPipeError` | `HIL rc=0`, RELEASED |
| `si1494/.../t1` | command 3 `load`: HTTP 400 `BAD_REQUEST` (hard-disk media) | `HIL rc=0`, RELEASED |
| `si1494/.../b3` | `halted: reboot_required` | `HIL rc=0`, then HALTED |
| `u6ref/.../g4` | `close_error`: HTTP 500 | `HIL rc=0`, then HALTED |

`p5dbg/k2` is typical: a `load` failed at command 63 of 91, 28 further commands
ran, and the wrapper released the kit on `rc=0`. Whether each of those runs'
conclusions depended on the failed load is for its own session to say; what
this shows is that the driver's exit code never carried the information, on any
of them, which is what guard 3 changes.

## What the incidents implied that turned out not to be true

- **"The x87 session's fix covers the driver."** It does not. The fix is in
  that session's own wrapper (`~/fes-scratch/x87s2/kit/run-x87.sh`, which
  greps `result.json` for `"error"` and sets `rc=99`). Its driver,
  `hil_x87.py`, is byte-identical to `hil_games.py` in the loop that swallows a
  command error and carries on, and the `scripts/` diff of
  `origin/deano/i386-x87-s2` against `main` touches no HIL driver or lease
  client. Nothing here duplicates that work; this PR is the first
  repository-side guard.
- **"Only the hand-run tail was unguarded."** It is worse than the incident
  implies. The standalone housekeeping script `clean-r.sh` does check that the
  status file logs the kit FREE and that the agent is idle with a free lease
  before it removes anything. But the tail *inside* the wrappers is not
  guarded either: `run-u6ref.sh:42-51` and `run-x87.sh` both run
  `ssh rm -rf` on the run's new trees as soon as the driver exits — before any
  lease or agent probe, and whatever the driver's exit code was. The hand-run
  `post-g1.sh` is a copy of that same unguarded tail, so running it by hand
  was not what made it unguarded. (`run-games.sh`'s tail only lists.) Neither
  copy checks for a loaded core; the new gate does, which is why guard 2
  belongs in the wrapper tail and not only in the hand-run scripts.
- **"Restarting the agent clears a blocked lease."** True for this kind of
  block, and now verified in the field rather than read off the code: the
  #1368 session cleared Kit B's `blocked` lease at 17:44 EET with a warm
  `reboot`, no power cycle, and the lease came back `free` with a fresh
  generation and no owner. The qualification is the cleanup, not the
  persistence: The state is indeed in memory only: `kitlease.New` starts at
  `State: "free"` (`sources/FogCast/internal/kitlease/manager.go:79`) and
  nothing in that package reads a persisted lease status back. But `New` then
  calls `revokeLocked("agent startup reconciliation")` (`:405`), which runs the
  same `cleanup` callback, and on failure sets the same `blocked` state and the
  same reason at `:468`. So a restart re-runs the reconciliation rather than
  skipping it: it clears a lease blocked by a cleanup that now succeeds, and
  re-blocks one whose cleanup still fails. Maintenance reservations are the
  separate, persisted case (`maintenance_store.go`), and
  `idle_maintenance.go:8` requires the caller to reinstall intent after a
  restart; their reasons are `:441` and `:459`, distinct from `:468`. **The
  reason string is the discriminator**, so a record should say which kind it
  means: the in-memory "kit cleanup failed or agent shutting down" clears on
  any agent restart, a maintenance block does not.
- **There is no way to stop out of a blocked lease, which is why these are
  guards and not a recovery path.** The agent refuses
  `POST /v1/kit/takeover` on a blocked lease with HTTP 409, so no token exists
  to send a `stop` with — #1368's recovery tried that first. A wrapper
  therefore cannot recover itself once the lease is blocked; it can only avoid
  blocking it, which is what guard 2 does.
  `fes-kit-software restart-service fogcast-kit` would be the narrower fix than
  a reboot and **remains untested**: it is a destructive action on a shared kit
  needing Deano's explicit OK, this work gave no occasion to try it, and
  manufacturing a blocked lease to find out is not something to do to a shared
  kit.
- **Not implied by the incidents, found while fixing them:** a new file under
  `sources/misteross/scripts/` falls to the `sources/misteross/scripts/**`
  rule and asks for an `OVERLAY core:ALL` HIL — an overlay run of every core —
  for a change that deploys nothing. `hil_plan.py` now classifies
  `kit_hil.py` as `none`, with a test. The lease client `kit.py` beside it
  keeps its overlay classification: the import graph reaches it from
  `pc386_wfw_kit_acceptance.py`, so that one is not a misclassification.

## The invariant guard 2 rests on

**The kit lease is held for the whole interval in which the cleanup reads or
removes anything, and the command's exit code tells the truth about whether it
was.**

Everything else about guard 2 is mechanism. Three rounds of review each found
the same gap in a different place, which is why the invariant is stated here
rather than only implemented:

1. The gate read the kit's state and the caller then removed files, so another
   session could claim the kit in between — "safe when it looked, not when it
   acted".
2. Claiming first fixed the start of the interval but not its length: a claim
   excludes others only while it is live, `kit.Session` renews in the
   background and merely sets `failed` when a renewal is lost, and one ssh
   command could run 180 s against a 90 s lease
   (`kitlease.New(90*time.Second, ...)`,
   `sources/FogCast/cmd/mister-agent/main.go:511`). So a slow `find` or `rm`
   could outlive the grant and keep deleting under a kit someone else had
   claimed.
3. And an unconfirmed `/v1/kit/release` was only logged, so `clean` exited 0
   and the documented `clean … || exit 5` tail proceeded with the lease
   possibly still held — the exit code not telling the truth.

What holds it now: `hold_for_cleanup` claims before it checks;
`require_lease` — `kit.Session.mutate`'s own guard, applied to the steps the
agent never sees — runs before the listing, before each `rm -r` and before the
verification listing, and aborts with nothing further removed; `KitShell`'s
per-command timeout is below the lease's own expiry so one command cannot
outlast it; and an unconfirmed release raises, after any original failure has
been preserved, so the command cannot report success over a lease that may
still be held.

If a fourth round finds another instance of this family, the question stops
being which check to add and becomes whether a host-side script should hold a
kit lease at all, or whether this belongs behind an agent-side operation that
owns the lease itself — a design question for Deano, not another patch.

## A claim this record made, and why it was wrong

**The earlier version of this record said of guard 2: "Fails closed. An
unreachable agent, an agent that is not `idle`, a lease that is not `free`, a
loaded core and a pending recovery all refuse, because each of them means a
removal would race the runtime." It said the gate *enforces* that nothing is
removed under a loaded core. That was wrong, and a Codex P1 on PR #1850
([finding](https://github.com/DeanoC/fes/pull/1850#discussion_r4574628833))
caught it.**

What the gate did was *read* the kit's state and then let the caller remove
files. It required the lease to be `free` — which is to say, claimed by
nobody — and then listed and `rm -r`'d while it stayed free, so another session
could claim the kit between the check and the delete and load a core into it.
The second gate, after the removal, could only report that this had happened.
The guard proved the state was safe at the moment it looked, not that it was
safe when it acted.

That is the #1368 incident's own shape: a sequencing assumption that happened
to be wrong. A record asserting a safety property the code does not have is
worse than a missing guard, because the next session reads the record instead
of the code.

**What it does now.** `hold_for_cleanup` claims the kit lease *first* — a claim
succeeds only while the lease is free, so it is the exclusion itself — and
checks the state second, under its own held lease; `clean` releases it in a
`finally`. Check and removal are one held interval instead of two ordered
events. `guard-idle` is unchanged and still read-only: it is an observation,
and the record no longer calls it more than that. The order is asserted
directly in `test_the_lease_is_claimed_before_the_kit_is_listed`, which fails
if anything is read or removed before the claim.

A second round on the claim-first head found two more of the same family, both
right: the lost-renewal P1 and the unconfirmed-release P2 above. Five of their
six cases are red on `3145d8cd3`; the sixth, that an unconfirmed release does
not mask an original refusal, already passed there and is kept as a regression
guard for the new ordering rather than claimed as a fix.

The other four findings from the first round were defects rather than
corrections: `verdict` ignored `drain`'s own `reasons`, so a live run that
ended with no `quit` exited 0; a malformed command line raised out of `drain`
instead of ending the run with a reason; a `result.json` holding `null` or `[]`
was treated as an empty object and passed; and one `find` over all the roots
accepted a partial listing when a single root was unreadable. Each has a case
that is red on `b35a9cf3f`.

On the last of those, the finding's literal remedy — refuse on any nonzero
`find` exit — is not what was implemented, and the reason is worth recording:
`find` also exits nonzero when a root merely does not exist, and
`/media/fat/fogcast/saves` need not exist on a kit that has never saved, so
that rule would refuse every cleanup on such a kit. The evidence here is
inconclusive rather than reassuring: of the three runs checked, the two that
listed three roots found `saves` and the third never asked, because
`run-games.sh` lists only two roots. Each root is therefore probed on its own:
a root that exists must list cleanly, and a root that is absent contributes
nothing and is named in the log.

## The gate against the real agents

Kit B was released by the #1368 session at 17:44 EET, so `guard-idle` was run
against both kits' live agents. This is a read-only pair of GETs — the same
probe class the wrappers run *before* they claim — so no kit was claimed and
nothing on either kit changed:

    $ kit_hil.py guard-idle --config ~/.config/fogcast/config.toml --target kit2
    {"state": "idle", "lease": "free", "target": "kit2", "host": "fes-kit-b"}   # rc 0
    $ kit_hil.py guard-idle --config ~/.config/fogcast/config.toml --target dev
    {"state": "idle", "lease": "free", "target": "dev", "host": "fes-kit-a"}    # rc 0

So the pass path holds against the real wire, both kits are reachable by name
from one unmodified host TOML (its `sha256` unchanged, `selected_target = 'dev'`
still its only one), and each answer says which kit it is about. The refusal
paths stay unit-tested: provoking them would mean loading a core to remove
trees under, which is the thing the guard exists to prevent.

## Repository gates run here

| gate | result |
| --- | --- |
| `sources/misteross`: `unittest discover -s tests -p 'test_kit*.py'` | 77 tests, OK |
| `tests/test_hil_plan.py` | 39 tests, OK |
| `tests/test_affected.py`, `tests/test_test_changed.py` | 43 and 33 tests, OK |
| `scripts/check_diff.py --base <main> --head HEAD` | exit 0 |
| `scripts/check_doc_links.py` | exit 0 |
| `scripts/parent_tests.py --video true --media false` | exit 0 |
| `hil_plan classify --repo . --base-image-commit <main> --head <head>` | `NO_DEPLOY_CHANGE` |

The fpga lane's per-core RTL simulations are left to CI: any
`sources/misteross/scripts/` change selects all of them, as a `kit.py` change
already did.

## Adoption, as far as it can go here

The guards only help a run whose own driver and wrapper call them, and the
copies on Powerboat belong to live sessions, so nothing under `~/fes-scratch`
is touched here. What this PR can do instead is make the call a single tested
command rather than a block each wrapper reimplements: with `clean` and
`verdict`, a wrapper's tail is

    python3 "$KH" clean --ssh "$SSH" --run-dir "$OUT" --config "$CFG" --target "$T" --apply || exit 5
    python3 "$KH" verdict "$OUT/result.json" || rc=99

in place of the `find`/`comm`/`ssh rm -rf` block and the `grep '"error"'` only
`run-x87.sh` ever wrote. The per-session wiring, and the question of whether a
shared thin driver should replace the 566-line copies at all, stay in issue
#1852: a driver needs a kit's capture device, audio device and ssh helper, and
proposing its shape without a kit run to validate it would be guesswork.

## Reach, and what is not done

The drivers and wrappers on Powerboat are per-session copies that import
`kit.py` by absolute path from whichever worktree made them. These guards
therefore protect a run only once its driver and wrapper call them; this PR
changes no file under `~/fes-scratch`, and the live `u6ref` session's tree was
not touched. Re-pointing the existing copies, and making the next session's
driver a thin caller of `drain` instead of a 566-line copy, is follow-up work.

`guard-idle` prints which target answered and names it in every refusal,
because a gate that passes for one kit while the tail removes on another is
worse than no gate. A named `--target` wins over `FOGCAST_BASE_URL` and
`FOGCAST_TOKEN`; the environment can only fill in what that target omits.
The live mode's five-minute wait is the `idle_limit` parameter, not a ceiling:
a console session that will think for longer passes a larger one.

No kit was used. All four guards are provable against a fake agent and a local
HTTP server, which is why they are unit tests rather than a kit run; the
`cleanup_gate` wire-shape case is the one that would otherwise have needed a
kit, and it is covered by replying as the agent replies.
