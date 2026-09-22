# Soft-restart Path B

Path A is the restart that stays on the current boot: Stop reaches idle, the
kit lease is free, and `mister-supervise` restarts the runtime and the agent.
That path is the procedure in FogCast
[development restart](../sources/FogCast/docs/DEVELOPMENT.md#restarting-target-services).

Path B is the other branch: a development session is already
`reboot_required`, `recover_idle` does not program idle, and the agent starts
a board reboot. This note is the arming map for that branch. The kit check
at the bottom is **HOLD-FOR-KIT-GO**. It is not a procedure and it has no
commands.

## What arms `reboot_required`

The runtime is the only component that decides a physical idle load failed.
`IdleFailure` in `sources/libmister-runtime/src/runtime.cpp` rewrites that
failure as `idle_failed` with phase `recovery`. These `Runtime` methods then
publish state `reboot_required`:

| Method | Condition | What it does not do |
| --- | --- | --- |
| `Start` | the startup `LoadIdle` fails | |
| `Stop` | state was `running_development` and the cleanup `LoadIdle` fails | `Stop` from `idle` returns success and does not call `LoadIdle`. `Stop` from `reboot_required` returns the existing error and does not call hardware. |
| `RecoverIdle` | state was `reboot_required` and the second `LoadIdle` fails | Success publishes `idle` and does not reboot the board. `running_development` is delegated to `Stop`. `idle` is a no-op success. |
| `FinishLaunchFailure` | a launch mutated hardware and the cleanup `LoadIdle` fails | A failed launch whose cleanup reaches idle stays `idle` and keeps the original error. |
| `DrainFaults` | input-fault cleanup `LoadIdle` fails | |
| `RestoreAfterSaveFailure` | `RestoreInput` fails after a save failure | Persistent mode keeps package and generation metadata. |
| `LoadComputerMediaStream` | the hardware error phase is already `recovery` | |

`sources/libmister-runtime/src/daemon/controller.cpp` exposes the second
attempt as protocol-2 operation `recover_idle`. That operation calls
`Runtime::RecoverIdle` only. It does not reboot the board.

## What the agent publishes

`misterruntime.Runtime` in `sources/FogCast/internal/misterruntime/runtime.go`
and `stopCorePackage` in `core_data.go` translate the daemon reply:

| Observation | Agent result |
| --- | --- |
| `Stop` reply state `reboot_required` and no active package | recovery `reboot_required`, no API error |
| `Stop` reply state `reboot_required` with an active package | recovery `reboot_required` plus `MISTER_UNAVAILABLE` phase `recovery` (core-data recovery, not the development-reboot arm) |
| `Stop` reply failed with any other state | `mapProtocol2Error`, recovery empty |
| Reconcile sees `reboot_required` without an active package | public state `failed`, recovery `reboot_required`, `development` false |
| `RecoverIdle` reply is clean idle | idle programmed, no board reboot |
| `RecoverIdle` reply is `reboot_required` / `idle_failed` | false, and the error keeps the daemon phase |
| `RecoverIdle` reply is `invalid_request` or `unknown_operation` | `UNSUPPORTED_OPERATION` |
| `RecoverIdle` transport, busy, or any other failure | `MISTER_UNAVAILABLE` with no phase |
| `StopReady` | false for `reboot_required`. True for idle, running development, or a described active package. |

`Coordinator.stopLocked` in `sources/FogCast/internal/agent/coordinator.go`
arms the development reboot only in two cases:

1. Native session. `Stop` returns no error, the session is already
   `development`, and the runtime recovery string is `reboot_required`.
   The published status is `stopping` + `development` + `reboot_required`.
2. Legacy session. The runtime does not implement `ownedDevelopmentRuntime`.
   Production `misterruntime.Runtime` implements that interface, so the
   native image does not take this branch.

Every other `Stop` result stays off that arm:

- Already idle: return idle. The runtime is not called.
- Already `stopping` + `development` + `reboot_required`: return it unchanged.
- `StopReady` is false: return the current status plus
  `MISTER_UNAVAILABLE`. `stopHandler` writes only the error envelope, so the
  HTTP body has no `recovery` field even when the stored status has one.
- Recovery is set but the session is not development, or `Stop` returned an
  error: publish `failed` (or pass the error through). That is not `stopping`.

`Coordinator.RebootDevelopment` rejects anything except
`stopping` + `development` + `reboot_required` with `BAD_REQUEST` and the
message `development reboot was not requested`. When the state matches, it
calls `RecoverIdle` first:

| `RecoverIdle` result | Next step |
| --- | --- |
| Idle programmed | Publish idle. Do not call `RecoverDevelopment`. |
| `UNSUPPORTED_OPERATION`, or `MISTER_UNAVAILABLE` with phase `recovery` | Call `RecoverDevelopment`. |
| Any other error, including transport failure | Publish `failed`. Do not reboot. |

`RecoverDevelopment` on the native runtime starts the configured executable
and returns. Production sets that path in `cmd/mister-agent/main.go` as
`rebootCommand = "/sbin/reboot"`. A missing or non-executable path returns
`MISTER_UNAVAILABLE` and starts nothing. Tests replace the path with
`misterruntime.WithRebootCommand`. The production constant is unchanged.

The host in `Service.stopLocked` (`sources/FogCast/fogcast/service.go`) calls
`RebootDevelopment` only when the agent Stop body is HTTP 200 with
`stopping`, `development`, and `recovery=reboot_required`. An agent
`APIError` is returned immediately, so the host does not wait for a new boot
id. A clean idle reply is accepted on the same boot. Any other success waits
until health is ready with a different boot id and status is clean idle.

## Why Path B did not arm (hypothesis)

This is a reading of the code above against the observed kit results. It is
not a new hardware trace.

Poisoning the idle bitstream while the runtime is already `idle` cannot arm
Path B. `Runtime::Stop` and `Coordinator.stopLocked` both return idle without
`LoadIdle`, so the agent never stores `reboot_required`.
`POST /v1/development/reboot` then answers `development reboot was not
requested`.

A startup `LoadIdle` failure, or a later reconcile of `reboot_required`, is
also not the arm. Reconcile publishes `failed` with `development` false.
`StopReady` is false, so Stop's HTTP body is `MISTER_UNAVAILABLE` and the
error envelope omits `recovery`. The stored state remains `failed`, not
`stopping`, so the reboot route stays `not requested`. That matches Stop
returning `MISTER_UNAVAILABLE` with `recovery` null.

Service and overlay restarts are a different event. They are not how the
state machine sets `reboot_required`, and repeating them while the FPGA or
the ADV7513 path is live can drop the ADV7513 from I2C. Path A never enters
the reboot branch, which is why Stop → idle → supervise stayed on the same
boot.

## Offline proof

`TestPathB*` in `sources/FogCast/fogcast/path_b_recovery_test.go` drives the
real host service, target HTTP agent, coordinator, and native runtime adapter.
The daemon is a fake protocol-2 control. The board reboot is a temp script
installed with `WithRebootCommand`. The test does not call `/sbin/reboot`.

The covered rows are:

- Development Stop reports `reboot_required`, `recover_idle` returns
  `idle_failed` phase `recovery`, and only then the test script runs. The
  host accepts the new boot id and idle.
- Development Stop reports idle. The script does not run, and
  `POST /v1/development/reboot` is `BAD_REQUEST`.
- Reconciled `reboot_required` makes Stop `MISTER_UNAVAILABLE` with no
  `recovery` field, does not call runtime Stop or `recover_idle`, and leaves
  the reboot route `not requested`. Status still shows `failed` and
  `recovery=reboot_required`.
- `recover_idle` returning `idle_failed` with a phase other than `recovery`
  fails closed and does not start the script.

No production flag was added. An arming hook on the kit would be another way
to program the FPGA, which is the risk this note is avoiding.

## HOLD-FOR-KIT-GO

**Do not run this section.** It is an acceptance list for Deano, physically
at the board, after an explicit GO. It is not a Caster task. There is no
command to copy. A development reboot, an init-script restart loop, or an
idle-bitstream poison can drop the ADV7513 from I2C and take Ethernet down.
The recovery for that is a hard power-supply cycle with someone at the
board. A front-panel reset does not replace that cycle. Do not loop runtime
or agent restarts to force the failure.

After that GO, the observations that would accept Path B on a kit are:

1. The session is already development, and a real cleanup `LoadIdle` failure
   makes agent Stop return HTTP 200 `stopping` + `development` +
   `recovery=reboot_required`. Idle and a reconciled startup failure must
   still refuse the reboot route.
2. The host then calls the development reboot route. The agent issues
   `recover_idle` once. The board reboot runs only when that second
   `LoadIdle` returns `idle_failed` phase `recovery`, or the daemon reports
   the operation unknown.
3. Either the boot id changes and the session is idle, or the kit becomes
   unreachable. If it becomes unreachable, stop. Hard-cycle the supply.
   Do not start another restart loop.

Until that GO, Path A remains the only restart to use.
