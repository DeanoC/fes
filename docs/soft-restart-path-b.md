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

## What a zero-filled idle file does

`OpenRBFArtifact` (`sources/libmister-runtime/src/native/artifacts.cpp`,
`ValidateAndAdopt`) accepts any non-empty regular file up to 32 MiB that has
one readable byte. A 4 KiB regular file of zeros passes that check.
`NativeHardware::LoadIdle` then runs HDMI quiesce, driver quiesce, and
`fpga_.Program`. `TestIdlePreflightFailureCallsNeitherFpgaNorVideo` is the
other failure: an opener error returns `io_failed` with
`mutation_attempted` false, and neither the FPGA nor video bring-up is
called. `Runtime::Stop` wraps every `LoadIdle` error, including that
preflight error, with `IdleFailure` and publishes `reboot_required` only
after `LoadIdle` returns.

`reboot_required` is therefore not produced by a bad bitstream. It is
produced when `LoadIdle` returns. A file that passes the open check is
programmed before that return can happen.

## Why the B3/B4 arm did not set `reboot_required`

These observations are from tip #122 (`f15536f1`), recorded outside this
repository. They are evidence, not instructions.

Path A on that tip was Stop to idle, a supervise restart, the same boot, and
a free lease. The Path B attempt that reached Stop was an active development
session (`load_development_rbf` of the real idle bitstream, `state=active`,
`development=true`, `recovery` null), then a 4 KiB zero file in place of
`idle.rbf`, then `POST /v1/stop`.

The recorded agent result was `stop.error = MISTER_UNAVAILABLE` with message
`target runtime is unavailable`, then `status.state=failed`,
`development=true`, `last_error=MISTER_UNAVAILABLE`, and `recovery` absent.
`POST /v1/development/reboot` was not called. The runtime log for that Stop
reached `starting`, `hdmi_quiesce`, and `driver_quiesce`. ADV7513 address
`0x39` was gone afterward. A later service stop/start with the ADV already
missing needed another hard power cycle. Skipping that restart when `0x39`
was already missing was the stop condition used on the following attempt.

That agent body is what `Coordinator.stopLocked` stores when Stop returns an
error and an empty recovery string. `stopCorePackage` leaves recovery empty
unless the reply state is `reboot_required`. A lost Stop whose follow-up
status is still running, and a Stop reply that is still `starting` with
`program_failed`, both become `MISTER_UNAVAILABLE` and development stays
true because the session was development. `stopHandler` writes only the
error envelope, so the Stop body has no `recovery` field. The stored state
is `failed`, not `stopping`, so the reboot route answers `development reboot
was not requested`.

Hypothesis, from the log stopping at driver quiesce and from `0x39`
disappearing: `fpga_.Program` of the zero file did not return an
`idle_failed` reply before the agent gave up. This note does not add a kit
retry. Treating every unavailable Stop as `reboot_required` would start
`/sbin/reboot` after that program, which is the outcome this arm is avoiding.

Two other misses are real and are not the B3/B4 body. Stop while the runtime
is already `idle` never calls `LoadIdle`. A reconciled startup
`reboot_required` is `failed` with `development` false, and `StopReady` is
false, so Stop's error also omits `recovery`. B3/B4 was neither of those:
the session was active development, and Stop entered `LoadIdle`.

Service and overlay restarts are a separate ADV failure. They are not how
`reboot_required` is set. Path A never enters the reboot branch.

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
- `TestPathBUnclassifiedStopDoesNotArmRecovery` injects the B3/B4 shape.
  One case returns `starting` / `program_failed`. The other loses the Stop
  socket while status stays running. Both leave `failed` + `development` +
  `MISTER_UNAVAILABLE`, omit `recovery`, do not start the script, and leave
  the reboot route `not requested`.

The preflight failure that skips FPGA program is already
`TestIdlePreflightFailureCallsNeitherFpgaNorVideo`. The runtime tests
`TestFailedStartAndStopNeverPerformSecondCleanup` and
`TestRecoverIdleFailureStaysRebootRequired` show `Runtime::Stop` /
`RecoverIdle` publishing `reboot_required` from an injected `LoadIdle`
error, with no bitstream. No production flag was added.

## HOLD-FOR-KIT-GO

**Do not run a kit check from this note.** There is no command, no idle-file
replacement, and no service restart. The B3/B4 zero-file Stop is a closed
result: it programmed a junk image, returned `MISTER_UNAVAILABLE` with no
`recovery`, and dropped the ADV7513. Do not repeat it. Do not bind-mount or
overwrite the idle bitstream. Do not restart the runtime, agent, or launcher
to force `reboot_required`. If `0x39` is already missing, do not cycle those
services; the recovery that brought the board back was a hard power-supply
cycle with someone at the board. A front-panel reset does not replace that
cycle.

Path B has no accepted on-kit arm yet. The failure that avoids FPGA program
is an `OpenRBFArtifact` rejection before HDMI quiesce. Using that on a kit
would still be a live edit of the idle file followed by Stop and, if it
armed, `/sbin/reboot`. That sequence is not authorized here.

After an explicit GO from Deano at the board, the only acceptable new arm is
one that makes `LoadIdle` return `idle_failed` before `hdmi_quiesce` and
`fpga_.Program`. Until that arm exists, Path A remains the only restart to
use, and the proof of Path B is the host injection tests above.
