# M1 two-node integration evidence (#363)

**Historical record (22 October 2026):** these tests covered the former host-local RetroArch executor and paired-kit path. Both paths have been removed. The named tests and implementation are historical; this evidence does not transfer to the kit profile. The kit is now its own FogCast host ([#863](https://github.com/DeanoC/fes/issues/863)); see [mesh under kit-as-host](mesh-kit-host.md).

M1 had two execution nodes: an FPGA kit target running its agent and kit
launcher, and the host-local RetroArch `host_only` executor. Kit mutations use
the target's existing lease, per-kit bearer and `X-FogCast-Target-ID`. The
RetroArch process is owned by the root host session coordinator. Remote mesh
software-runner admission is outside this topology.

This matrix records the #363 two-node regression coverage at the time. The
named tests ran from `sources/FogCast`; some have since been removed.

| #363 area | Proving tests | Limits |
|---|---|---|
| Simultaneous play and display ownership | `fogcast:TestTwoNodeSimultaneousPlay`, `fogcast:TestTwoNodeHostFirstThenKitKeepsBoth`, `fogcast:TestTwoNodeCastModeKitLaunchStopsHostOnly`, `fogcast:TestTwoNodeHostMediaOnSameKitReplacesKitPlay` | Non-cast mode (media disabled, MJPEG preview, or ffplay playback) keeps host-only and kit plays independent in both launch directions. Cast mode (managed sender plus target cast) gives the kit display one owner and replaces the other play in both directions. |
| Scoped Stop | `fogcast:TestTwoNodeScopedStop` | Uses fake kit and host executors; no hardware is exercised. |
| Launch race and BUSY | `fogcast:TestTwoNodeHostOnlyBusyKeepsKit` | Includes sequential BUSY admission and two concurrent public launch calls. |
| Busy kit lease | `fogcast:TestTwoNodeKitBusyLeaseRefusesOnlyKit` | Models the foreign lease through the service's observed connection state. |
| Unavailable host executor | `fogcast:TestTwoNodeHostExecutorUnavailableKeepsKit` | Uses the fake host executor's unavailable error. |
| Kit unreachable and reconnect | `fogcast:TestTwoNodeKitUnavailableKeepsHostOnly` | Simulates a failed kit status read, then restores status; it does not restart a process. |
| Coordinator (host) loss and restart | Existing: `fogcast:TestServiceDevelopmentSessionStateReconstructsNativeGameAfterHostRestart`, `internal/hostapi:TestSessionStatusDoesNotReplaceEligibleInputWhileItReconnects` | Kit-side reconstruction after a host restart is covered by the existing tests. No new two-node restart test: the host-only RetroArch process is a child of the host and does not outlive it. |
| Incompatible version or capabilities | Existing: `internal/meshplace:TestMeshMajorMismatchFailsClosed`, `internal/meshplace:TestWrongABIDoesNotSelect`, `internal/hostexec:TestRetroArchAdapterRejectsMissingOrMismatchedCore`, plus the new `fogcast:TestTwoNodeHostExecutorUnavailableKeepsKit` | A rejected backend reports unavailable and leaves the other node's play alone. |
| Scoped input | Existing: `host:TestRemoteInputAttachSendDetachOwnsSessionAndRelease`, `internal/hostapi:TestLauncherStopIsScopedToPairedKit` | Kit remote input stays with kit sessions. Remote input to `host_only` is an M1 non-goal (see below). |

Ownership contract: `Service.plays[target]` is the sole source of truth for
each kit's game, development load, rejected-package state, and recovery
admission. Root `activeExecution` and its game identity belong only to the
host-local `host_only` executor. Kit load success or failure never stops or
replaces that host game in non-cast mode. Cast mode still makes the host sender
claim the kit display, so a cast host launch and a kit launch replace one another. Scoped Stop, status projection,
and recovery use the selected kit record; unscoped Stop addresses host-only
play when it is active.

## Source and fixture provenance

Implementation identities on this base: #288/#375 `d0305225`; #289/#372
`38701008`; #359/#385 `8199e908`; #360/#384 `0acdef71`; #361/#380
`272239a8`; #362/#405 `b1207189`. These are merge commits in FES history, not
binary identities. The listed host tests use synthetic in-process fakes,
temporary ROM/core bytes and protocol-shaped fixtures; no sealed package or
exact artifact is claimed.

## Hardware evidence limit

The host tests did not establish hardware acceptance for those artifacts.

## M1 input limit

Remote controller input routed to `host_only` is a non-goal for M1. Software
input is attached to the runner through RetroArch's local joypad/udev input.
The host must not route kit remote input into the local executor.
