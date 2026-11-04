# 2026-11-04: the supervised kit host on Kit A (#863 slice 5)

Kit check of the slice 5 image and bundle. Evidence class: hardware
diagnostic on Kit A, with image changes Deano approved on 4 November. Run i1
was partial because of driver bugs; run i2, with the fixed driver, covers the
rest.

## Method

[`build-863s5-image.py`](build-863s5-image.py) built the image, release, bundle
and host at `83399b86a` with Lander's baseline recipe (`make build`, `verify`,
`release`, `host`, `kit-software`). The resulting artifacts:

| Artifact | Value |
| --- | --- |
| Image | `b00539bd…` |
| Bundle | `d2ace9c2…`, which advertises `kit-host 1.0`; `bin/fogcast-kit` is 18.2 MB |
| `software-base.json` | Byte-identical to image `f904961e`'s, so baseline bundles stay accepted |

[`k863s5-run.py`](k863s5-run.py) drove Kit A, starting from baseline software
`d695e970` on image `f904961e`:

1. `fes-update` the image, keeping the baseline software.
2. Install and activate the slice 5 bundle.
3. Re-activate the baseline bundle and then the slice 5 bundle, to time each.
4. Launch an ST through the kit host and kill the host.
5. Restore the baseline software, then `fes-update rollback`.

The copy here has the fixes listed under "Not shown", made after the run.

## Run i2

The same image, bundle and steps as i1, with the fixed driver. Its record is
[`i2-results.json`](i2-results.json).

| Check | Result |
| --- | --- |
| New image, baseline bundle | Three services: runtime, agent, tenfoot. No host |
| New image, slice 5 bundle | Four direct children: the three services plus `fogcast-kit` (the host) |
| Agent `GET /v1/core-packages`, longest wait | 21.2 s and 22.3 s with the host; 0.08 s without |
| Host's package list | The ST and Coleco packages in the durable store; no menu |
| ST title and diskless launch through the host | Launched in 43.6 s |
| Host killed by PID mid-game | The host restarted alone (new PID 4462); the runtime, agent and launcher kept their PIDs |
| Same kill, the running game | **Ended**: the agent went from `active` to `stopping` to `idle` within 2 s |
| Rollback to `f904961e` | Confirmed; #1469 happened again, and the documented acknowledge repaired it |

**A host restart ends the running game.** The host stops the session it owns
when it shuts down, so a supervisor restart of the host stops the core. Decision
4 forbids this, and it is the open item for slice 6: the agent should hold the
lease across a host restart, and the host should not stop a session on exit.

**#1469 reproduces.** Both rollbacks left the stock supervisor's reactivation
blocked. The driver's guarded repair ran `fes-kit-software acknowledge` only
because the three baseline services were running and the launcher was ready.
It completed, and Kit A was verified clean and logged FREE.

## Run i1

| Step | Result |
| --- | --- |
| Image update to `b00539bd` | Confirmed in 231 s; baseline `d695e970` stayed selected |
| Slice 5 bundle activation | Accepted; activation took 81.2 s and 76.0 s, against 77.2 s for the baseline bundle |
| Host config at `/run/fogcast/fesdata3/fogcast-host` | Kit A's own target ID, the agent's token, `http://127.0.0.1:8182`, `[core_packages] kit = true` |
| Host log | Reached the agent after 50 ms; store open after 22.4 s |
| Rollback to `f904961e` | Confirmed; baseline `d695e970` active |

**The host locks the agent out at every activation.** The driver timed the
agent's `GET /v1/core-packages` once a second for 70 s after each activation:

| Bundle | Longest response |
| --- | --- |
| Slice 5 bundle (four processes) | 18.6 s, and 21.0 s on the second activation |
| Baseline bundle (three processes) | 0.07 s |

The host's store open holds the lock the agent needs. This is the open item
from #1462, now measured on the supervised path.

## Not shown in i1 (covered by i2)

- **Three versus four processes, and the host restarting alone.** The driver
  listed processes with `/proc/<pid>/task/*/children`, which the kit kernel
  lacks, so every listing was empty. The copy here matches the parent PID
  instead.
- **Launching an ST and killing the host mid-game.** Powerboat's local port
  18787 was already taken by another lane's `fogcast-api` (a de25 test host),
  so the driver's `ssh -L` failed silently. Its read of `/api/v1/core-packages`
  reached that host, which listed a ZX81 package and a menu. Nothing was
  written there. The copy here uses `ExitOnForwardFailure=yes` and a free
  local port, and skips the launch if no ST package is listed.

## Recovery after the rollback

After the confirmed rollback, the old supervisor's reactivation of `d695e970`
missed its 60 s readiness window twice. Those failures left the kit in this
state:

- the operation `584e6bd1…` blocked as "candidate failed (context deadline
  exceeded); recovery failed";
- the agent lease `recovery-required`.

[`i1-supervisor-after-rollback.log`](i1-supervisor-after-rollback.log) shows
the order:

1. The rolled-back image booted as a trial, so FESDATA3 was not bound.
2. After the confirm, the supervisor restarted the group at uptime 00:01:41,
   and FESDATA3 was bound at 00:01:46.
3. That attempt was stopped at 00:02:48, and the second failed at 00:03:48.

The three services were running and ready on `d695e970`. The documented
repair, `fes-kit-software acknowledge` with the observed operation, generation,
bundle, base and boot, completed ([`i1-acknowledge.json`](i1-acknowledge.json)).

**State afterwards:**

- the lease is free and no operation is pending;
- `d695e970` is active on `f904961e`, which is good and not a trial;
- three services are running;
- the host state is removed.

Two changes to Kit A remain:

- its `previous` image slot is now the test image `b00539bd`;
- the ST package installed in the slice 3 check is still in its durable store.

The failing path is the stock image and bundle's post-trial reconcile, not
slice 5 code. The durable library adds to it: Kit A holds a 21 MB ST package,
which the agent verifies at start-up. The same package went through three
successful activations minutes earlier.
