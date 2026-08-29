# M2 Linux Mailbox Development Loader Design

## Status

Originally approved by the operator on 2026-08-27; amended after implementation-
plan review to add explicit session/mode/high-water ownership, a restricted
development profile, durable install recovery, and fault-control contracts. The amendment is pending operator
re-approval. This document does not claim implementation, software verification,
reproducibility, hardware observation, or acceptance.

## Purpose

The first `misteross` hardware experiment compiled and loaded a bare Cyclone V
counter, but its DE10-Nano LED output is not physically exposed on the MiSTer
Pi. M2 replaces that board-specific observation with a deterministic message
sent from FPGA fabric to Linux through the Cyclone V HPS general-purpose
registers.

The milestone must answer this narrow question:

> Can the pinned open-source FPGA toolchain build an RBF that runs on the
> designated MiSTer Pi, handshakes with Linux, and delivers the exact line
> `OSS FPGA OK\n` under FogCast-owned development lifecycle control?

This is a private development slice. It does not add a production updater,
public raw-RBF API, general-purpose FPGA console, HDMI behavior, or a new
`Main_MiSTer` contract.

## Governing boundaries

- FogCast's current architecture and ADRs remain authoritative. The target
  hardware has one owner at a time, target-private paths and devices stay out
  of the public protocol, and evidence status is limited to checks actually
  performed.
- `DeanoC/FogCast-POC` owns development loading, target lifecycle, Linux MMIO,
  message observation, and recovery orchestration.
- `misteross` owns RTL, simulation, constraints, open-source and Quartus build
  lanes, artifact manifests, host transport, and cross-lane comparison.
- `DeanoC/Main_MiSTer` is unchanged. Its command FIFO is a bounded compatibility
  adapter used only to perform the initial volatile RBF load.
- The existing public FogCast host/target API is unchanged. The development
  operation is available only through the explicitly selected SSH-enabled
  development profile.
- This focused design qualifies one offline-maintenance owner within FogCast's
  existing single-coordinator architecture. It does not authorize an
  independent hardware state machine: the development command, agent, Main
  supervisor, and agent supervisor share one durable admission fence and one
  hardware-owner record. Normal admission remains closed for every owner state
  other than `normal_main`.
- No code is copied from `Main_MiSTer`. The FogCast implementation uses its own
  narrow Go platform interfaces and the documented Linux register interface.
- The exact privately designated disposable kit is governed by ADR 0002.
  Hardware type, hostname, IP address, MAC address, or discovery alone never
  establishes authorization.

At design time, the source baselines are FogCast commit
`3f27741a931e5932429137cad0cc7ae0b3fafc38` and `misteross` commit
`6f58c27`. Implementation may move a baseline only through an explicit
reviewed rebase record; evidence binds immutable final commits and artifact
hashes, never branch names.

## Approaches considered

### FogCast-owned development tool with a compatibility load handoff — selected

A target-side FogCast command validates and loads the development artifact,
durably fences normal admission and restart sources, waits for compatibility
Main's complete process set to exit, then receives a durable MMIO lease from
the same FogCast ownership record. This reuses the existing MiSTer command
boundary without reusing its asynchronous writer, does not modify Main, and
makes the lifecycle limitation explicit.

The first slice uses a bounded target reboot for recovery because the retained
Main path cannot prove clean in-process release after an incompatible bare RBF.
It must not report a clean stop that did not occur.

### Modify `Main_MiSTer` to recognize a development core — rejected

This would keep Main alive, but would place FogCast-specific development
behavior inside the compatibility source and couple a small toolchain proof to
the larger GPL runtime migration. The operator explicitly selected FogCast as
the owner instead.

### Implement a complete MiSTer core protocol or Avalon bridge first — deferred

A normal MiSTer core or lightweight Avalon slave could support a long-lived
runtime, but either introduces substantially more RTL, protocol, and ownership
surface than is required to prove FPGA-to-Linux communication. Those are later
milestones after the dedicated general-purpose-register path is demonstrated.

### Continue with HDMI as the first observable — deferred

HDMI remains valuable, but it adds raster timing, transmitter state, pin
constraints, and possibly PLL behavior before the basic FPGA/HPS development
loop is known to work.

## System boundary

```text
misteross host workspace
  build + manifest + hash
  private SCP staging
  remote FogCast development invocation
                 |
                 v
FogCast target development profile
  validate designation, target, manifest, hash, and ownership
  persist recovering intent; fence agent and restart sources
  dispatch one existing Main `load_core` command
  observe stable absence of the complete Main process set
  commit no-owner checkpoint, then transfer MMIO lease
  map FPGA manager GPI/GPO registers
  decode, acknowledge, and print mailbox line
  request bounded reboot recovery
                 |
                 v
020_linux_mailbox RBF
  50 MHz fabric state machine
  cyclonev_hps_interface_mpu_general_purpose
  HELLO -> START -> DATA/ACK -> END/ACK -> DONE
```

The development tool is not a daemon and exposes no network, public, or
persistent listening socket. The development-only armed fault checkpoint may
briefly expose the root-only local Unix endpoint specified below; it is absent
from production builds and removed on exit/boot recovery. The tool runs as root
on the target because `/dev/mem`, the Main command FIFO, and reboot are
privileged operations. SSH is transport for the selected development profile,
not a runtime dependency or production feature.

## FPGA experiment

`misteross` adds `experiments/020_linux_mailbox`. Its production top-level has
only the 50 MHz clock input and one instance of
`cyclonev_hps_interface_mpu_general_purpose`. It contains no LED, HDMI, SDRAM,
BRAM/M10K, LUTRAM, DSP, PLL, or external GPIO dependency.

The RTL repeatedly exposes one finite message transaction after each FPGA
configuration:

```text
OSS FPGA OK\n
```

After the terminal handshake it holds a completed state and does not restart
the message until the FPGA is configured again. The counter and protocol state
have deterministic initialization supported by both synthesis lanes.

The current OSS stack already contains explicit support for the HPS general
purpose primitive in Yosys's Intel ALM black-box library and nextpnr-mistral's
Cyclone V architecture. M2 changes the build policy from “no hard blocks” to an
experiment-specific allowlist: `020_linux_mailbox` must contain exactly one
general-purpose HPS interface and zero other unexpected hard blocks. The
Quartus oracle applies the same semantic requirement.

Simulation supplies a model of the primitive boundary and drives HPS GPO from
the testbench. Production synthesis never consumes the simulation model.

The register and primitive assumptions are bound to source, not recollection:

- the accepted Main baseline defines the FPGA manager base at `0xff706000` in
  [`fpga_base_addr_ac5.h`](https://github.com/DeanoC/Main_MiSTer/blob/d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d/fpga_base_addr_ac5.h);
- its [`fpga_manager.h`](https://github.com/DeanoC/Main_MiSTer/blob/d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d/fpga_manager.h#L11-L19)
  places GPO at offset `0x10` and GPI at offset `0x14`;
- its [`fpga_io.cpp`](https://github.com/DeanoC/Main_MiSTer/blob/d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d/fpga_io.cpp)
  is the authority for the compatibility loader's bridge enable/disable,
  FPGA-manager completion, input/offload teardown, and `app_restart` fork/exec
  behavior, so observing only the original Main PID or process-local cleanup is
  forbidden;
- the Quartus Lite 17.0 Cyclone-V HPS SVD at
  `ip/altera/hps/altera_hps/altera_hps.svd` (locally verified SHA-256
  `d8611ea716d16937dc11294e9785d548264458d62c04ea5bd73c46c019a13238`)
  fixes system-manager FPGA-interface module `0xffd08028`, SDR
  `ctrlgrp_fpgaportrst` `0xffc25080`, reset-manager `brgmodrst`
  `0xffd0501c`, and L3 `remap` `0xff800000`; the last register is explicitly
  write-only and therefore cannot honestly supply an MMIO readback;
- the pinned SoCFPGA Linux bridge drivers identify runtime bridge instances by
  their `name` attribute rather than the dynamic `brN` directory name. M2's
  target inventory is exactly one each of `hps2fpga`, `lwhps2fpga`,
  `fpga2hps`, and `fpga2sdram`; every corresponding `state` must be exactly
  `disabled\n`, and missing, duplicate, or unexpected logical names fail
  closed. The first three views report reset-controller state and
  `fpga2sdram` reports SDR-port-reset state; none is represented as a readback
  of write-only L3 `remap`;
- the accepted FogCast baseline's
  [`internal/mister/runtime.go`](https://github.com/DeanoC/FogCast-POC/blob/3f27741a931e5932429137cad0cc7ae0b3fafc38/internal/mister/runtime.go)
  contains the retained asynchronous FIFO adapter; M2 must not reuse that
  adapter for a mode-changing write because its timed-out goroutine can later
  complete;
- pinned Yosys commit `13b43f8c85ec430a33ee55d058fb4c32b42b6910`
  declares the primitive in
  [`megafunction_bb.v`](https://github.com/YosysHQ/yosys/blob/13b43f8c85ec430a33ee55d058fb4c32b42b6910/techlibs/intel_alm/common/megafunction_bb.v#L712-L714);
  and
- pinned nextpnr commit `7d4f72c0aabc15da932748a54e82a6ff7b41921e`
  registers the primitive as a Cyclone V BEL in
  [`globals.cc`](https://github.com/YosysHQ/nextpnr/blob/7d4f72c0aabc15da932748a54e82a6ff7b41921e/mistral/globals.cc).

Implementation must recheck these exact pinned sources and the target's
machine-observed FPGA-manager state before accepting the assumption.

## Mailbox protocol version 1

The physical registers are the Cyclone V FPGA manager GPO at
`0xff706010` and GPI at `0xff706014`. FogCast contains these Linux-specific
addresses behind a register-I/O interface; they do not enter the public
FogCast protocol.

All words are unsigned 32-bit values. Bit numbering is inclusive.

### FPGA-to-HPS GPI words

| Bits | Meaning |
| --- | --- |
| `31:24` | FPGA signature `0xD3` |
| `23:20` | protocol version `1` |
| `19:16` | opcode: `0=HELLO`, `1=DATA`, `2=END`, `3=DONE` |
| `15:8` | sequence number, starting at zero |
| `7:0` | DATA byte; zero for HELLO and END |

Thus HELLO is exactly `0xD3100000`. DATA has prefix `0xD3110000`, and
END has prefix `0xD3120000`. DONE has prefix `0xD3130000`.

The signature deliberately sets bit 31. An unmodified compatibility Main
therefore follows its existing fail-closed incompatible-core path. No stock
Main process can accidentally interpret this as a normal MiSTer core.

### HPS-to-FPGA GPO words

| Bits | Meaning |
| --- | --- |
| `31:24` | HPS acknowledgement signature `0xAC` |
| `23:20` | protocol version `1` |
| `19:16` | opcode being acknowledged |
| `15:8` | echoed sequence number |
| `7:0` | echoed DATA byte; zero for START/HELLO and END |

START is the acknowledgement of HELLO and is exactly `0xAC100000`. A DATA or
END word is acknowledged by replacing only the signature byte with `0xAC` and
echoing all remaining fields exactly.

### State machine

1. After configuration, FPGA holds HELLO regardless of the inherited GPO
   value.
2. After the durable MMIO lease transfer, FogCast writes zero to GPO, observes
   two identical HELLO reads separated by one poll interval, and writes START.
3. FPGA exposes DATA sequence zero and holds it until the exact corresponding
   acknowledgement is present.
4. FogCast accepts a DATA word only when two reads separated by one poll
   interval are identical and their signature, version, opcode, expected
   sequence, and byte are valid. It appends the byte and writes the exact ACK.
5. FPGA increments the sequence modulo 256 only after the exact ACK. The M2
   message is shorter than 256 bytes, so wrap is not exercised in production
   but remains defined and simulated.
6. After the newline DATA acknowledgement, FPGA exposes END with the next
   sequence and holds it. FogCast requires two identical valid END reads before
   writing the exact END ACK.
7. FPGA consumes the exact END ACK, enters its terminal state, and exposes
   DONE with the END sequence and a zero byte. For this fixed 12-byte message,
   the terminal word is exactly `0xD3130C00`.
8. FogCast accepts success only after observing two identical valid DONE reads
   separated by one poll interval. DONE is not acknowledged and remains stable
   until reconfiguration.

An invalid signature, version, opcode, sequence, byte, early END/DONE,
oversized message, timeout, or change between the two required samples fails
closed. FogCast does not acknowledge an invalid or unstable word. The maximum
accepted payload is 256 bytes, although this experiment requires the exact
12-byte payload above.

The double-read rule plus explicit post-transfer zero write prevents a retained
GPO value from advancing fresh FPGA state before FogCast owns the mailbox and
prevents a transient or changing register word from being accepted.

## FogCast development lifecycle

FogCast adds a target-only command named `mister-fpga-dev` and a focused
internal package. The package separates policy from Linux mechanisms through
interfaces for clock/deadlines, artifact access, durable owner state, Main
command dispatch, process-set observation, register I/O, output, and reboot.
Host tests use fakes; only the Linux adapters open real files, inspect `/proc`,
map physical registers, or reboot.

### Shared durable admission and ownership

The development profile stores one hardware-owner record at
`/var/lib/fogcast/hardware-owner-v1.json` using a root-owned `0700` directory,
`0600` file, atomic replace, file `fsync`, and parent-directory `fsync`. The
shared inter-process lock is the no-symlink regular file
`/run/fogcast/hardware-owner-v1.lock`, in a root-owned `0700` directory with
mode `0600`, held by exclusive `flock`. Process death releases the lock but
never changes the durable record. The
record is versioned and includes durable generation high-water, fresh session
and generation identities, owner modes, run ID, boot ID, state,
quiescing/candidate/active owners, complete lease sets, and first failure. The
canonical schema is:

```json
{
  "schema": 1,
  "state": "recovering_intent",
  "phase": "intent_committed",
  "boot_id": "lowercase Linux boot UUID",
  "run_id": "32 lowercase hexadecimal characters",
  "generation_high_water": 42,
  "active_session": "32 lowercase hexadecimal characters",
  "active_generation": 41,
  "active_mode": "fpga_native",
  "candidate_session": "32 lowercase hexadecimal characters",
  "candidate_generation": 42,
  "candidate_mode": "updating",
  "quiescing_owner": "compat_main",
  "candidate_owner": "fpgadev",
  "active_owner": "compat_main",
  "active_leases": ["command_fifo", "core_input_saves", "core_protocol", "fpga_bridges", "fpga_generation", "fpga_programming", "main_process_set", "native_video_audio"],
  "requested_resources": ["fpga_generation", "fpga_manager_gpi_gpo"],
  "first_failure": ""
}
```

Keys appear in this order and arrays are lexically sorted. `phase` is empty in
`normal_main` and `normal_main_starting`; otherwise it is one of the result
phases defined below and never moves backward. `generation_high_water` is a
nonzero unsigned 64-bit value that never decreases. Every allocation increments
it before use; active/candidate generation is nonzero when present and never
exceeds it, while zero is the canonical absent generation. A session is fresh
32-character lowercase hex for every owner activation; empty is canonical
absence. Modes are `none`, `fpga_native`, or `updating`. `run_id` is empty in
normal states and otherwise is the manifest's validated 32-character value.
Owners are `none`, `compat_main`, or `fpgadev`. `normal_leases`
below means the eight literals in the example `active_leases`; `dev_leases`
means the two literals in `requested_resources`. Requested resources describe
non-owning intent only; they are never a lease or permission to touch hardware.
Only `active_leases` grants ownership after an atomic durable transfer. Each
active lease is the exact tuple `(active_session, active_generation,
active_mode, resource)`; requested resources confer no tuple or ownership.
`first_failure` is empty or one stable code from the result-code set. Unknown
fields, duplicate keys, non-canonical values, unsorted/duplicate resource
arrays, or a state-inconsistent owner/resource set fail closed.

The exact state invariants are:

| State | Active tuple | Quiescing/candidate tuple | Phase and admission |
| --- | --- | --- | --- |
| `normal_main` | `compat_main`, fresh session/current generation, `fpga_native`, `normal_leases` | candidate session/generation empty/zero, mode `none`, owner `none`, no requested resources, empty run ID | empty; allowed |
| `recovering_intent` | `compat_main`, old session/generation, `fpga_native`, `normal_leases` | quiescing `compat_main`; candidate `fpgadev`, fresh session/generation, `updating`, requested `dev_leases`, manifest run ID | intent/load phase; fenced |
| `no_owner` | owner `none`, empty session, generation zero, mode `none`, no active leases | quiescing `none`; candidate `fpgadev` retains its fresh session/generation, `updating`, requested `dev_leases`, manifest run ID | `main_absent`; fenced |
| `fpgadev_active` | `fpgadev`, candidate session/generation, `updating`, `dev_leases` | candidate identity cleared only after the same values become active; manifest run ID | lease/mailbox phase; fenced |
| `recovery_required` | preserves the last conservatively owned active tuple, if any | preserves the last allocated candidate identity until a strictly higher recovery generation is active; preserves run ID and first failure | last phase; fenced |
| `normal_main_starting` | `compat_main`, fresh session/generation allocated by incrementing high-water, `fpga_native`, `normal_leases` | candidate identity absent; empty run ID | empty; fenced |

`recovery_required` never releases an exclusive lease merely because its
process disappeared; reboot reconciliation proves reset before discarding the
preserved tuple. Each row transition is one atomic, file-and-directory-fsynced
record replacement under the shared lock. The owner phase is likewise replaced
durably at each result phase before that phase may be reported or used as a
fault-injection checkpoint.

The development profile does not construct or start cast, presentation, audio,
input, or controller-route facilities. Their public routes return the existing
unavailable response without making a hardware call. This fact is not inferred
from empty configuration fields, marker paths, or a generic `/proc` scan. The
boot supervisor validates an identity-bound readiness receipt from the exact
development agent child and publishes the current-boot quiescence proof defined
below. Development preflight validates that proof while permitting the healthy
attested Main process to retain its expected command-FIFO lease. After Main is
stably absent, no-owner qualification applies the proof's Task-8-owned exact
worker/route inventory and requires every inventoried process and descriptor
absent. The
agent's core launch/stop and the Main and agent supervisors consult this record
under the same inter-process lock. Every state other than `normal_main` blocks
normal hardware admission and prevents any legacy or new supervisor/start hook
from starting or restarting Main or the agent.
The agent may remain alive, but the unchanged public protocol
projects every fenced state as its existing `failed` status with
`mister_unavailable`; the private owner record retains the more precise reason.
It cannot issue a hardware transition. A vanished development process or
released process-local lock never clears the durable fence.

Serialization covers check and use, not only admission. Every permitted agent
core transition holds the shared lock from its owner-state check through its
own durable terminal, teardown, or reconciliation state. A Main or agent
supervisor holds it from
owner-state validation through child creation and durable started/failed-start
recording. The development command cannot commit `recovering_intent` between a
normal transition's admission and dispatch, and a previously admitted normal
transition cannot dispatch after development intent commits.

On boot, one development supervisor owns the shared lock and is the only Main
starter. Before starting Main or the agent it runs recovery. If the record is
anything other than a canonical `normal_main` record for the current boot, it
requires a new kernel boot ID relative to the recorded operation,
verifies the FPGA manager and bridge reset baseline, commits a durable
`no_owner` checkpoint, transfers a fresh complete lease set in
`normal_main_starting`, invokes its injected Main starter exactly once, verifies
exact Main/Menu readiness, and
commits `normal_main`. Only that final commit reopens agent admission. A
same-boot service restart cannot clear an interrupted record. If reset or Main
readiness cannot be proven, the target remains fenced and reports manual
recovery required.

Exact Main/Menu readiness means the expected Main executable identity is
present, the configured command FIFO exists with its expected FIFO type and
root ownership, the FPGA manager reports `operating`, and the core-name file
reports the exact token `MENU` in two reads 10 ms apart. All four observations
must hold within one context-derived 30-second deadline started before the first
filesystem or process observation. Every filesystem, process, FIFO, manager,
and Menu read receives that same bounded context; no adapter may replace it
with `context.Background`. An unknown core or a changing sample is not ready.

The first development-profile installation also starts fenced. It first stops
the pre-existing FogCast agent and proves it absent while leaving the healthy
Main/Menu owner in place. Before launch sources are changed, preflight must
identify one platform boot dispatcher whose ordering is proven to precede
every inventoried Main and agent start source. The installer atomically places
and fsyncs a minimal recovery trampoline in that dispatcher only after a
`prepared` journal has been file-and-directory-fsynced, and before any other
persistent launch change; before the trampoline exists, a crash leaves all old
launch sources unchanged and the inert prepared journal may be safely resumed.
The trampoline consults the root-owned durable
install journal and completes or rolls back it before invoking either the
captured old dispatcher or the new supervisor.

With that recovery executor established, `initialize-owner` observes the
complete pre-existing Main process set and exact Menu readiness, allocates a
fresh session/generation and high-water, and writes `normal_main` under the
shared lock. Installation then records every exact source path, hash, mode, and
backup; disables and fsyncs each old source; and installs and fsyncs the single
supervisor last. Each journal phase and its directory is fsynced before
advancement. Boot always enters the trampoline before any inventoried starter,
so interrupted work cannot bypass journal recovery. Uninstall keeps the
trampoline active, disables the supervisor first, restores every source
byte-for-byte with its recorded metadata, fsyncs it, then atomically restores
the original dispatcher last; it refuses unless the owner is canonical
`normal_main`. An absent record at boot is not
silently initialized. Ambiguity or an absent uninitialized record leaves the
target fenced for manual recovery. Unexpected Main or agent death changes the
record to `recovery_required` and requires reboot reconciliation; the
development supervisor never performs a same-boot restart. Because auxiliary
hardware facilities are absent in this profile, an agent death cannot orphan
one of their routes. Any ambiguity about a core transition remains fenced.

One root-only `install-profile` process holds the shared install lock and then
the shared owner lock while it
prepares the journal/trampoline, performs the internal `initialize-owner`
migration, and advances through the terminal install commit; there is no
between-command lock handoff. Presence of any nonterminal install journal blocks
agent admission, development preflight/run, and supervisor starts; only the
trampoline's root-only `recover-install` process may advance it. The symmetric
root-only `uninstall-profile` process holds both locks in that order through its terminal
restore. Thus a
same-boot crash cannot turn the still-running compatibility Main into an
unfenced development opportunity.

### Development activation and quiescence proof

An installation is not development-active on the installation boot. Its
terminal source-replacement phase requests reboot and remains an admission
fence. On the successor boot the recovery trampoline's `recover-install`
process holds the install lock, validates or repairs only the install journal,
backups, and launch sources, then passes the validated held lock descriptor
across exec to the sole supervisor without starting Main or touching hardware.
The supervisor validates the inherited descriptor, acquires the shared owner
lock in install-then-owner order, and is the sole FPGA reset/owner-recovery executor; it
proves the boot ID changed, completes reset and owner reconciliation, and starts
Main and then the development-profile agent.
The persistent terminal journal is immutable after source replacement; only
that successor-boot path may publish a new boot-local quiescence proof bound to
its digest. First-install HIL and every later development run therefore
start after a kernel reboot; no process, uinput descriptor, or virtual input
device from the replaced compatibility-agent boot can survive.

The supervisor creates a one-use inherited readiness pipe for the exact agent
child. After validating the development profile and constructing the runtime,
the agent writes one canonical readiness receipt through that pipe. The receipt
binds its PID, process start time, executable device/inode/SHA-256, profile
SHA-256, and the exact fixed capability set `cast_unavailable`,
`input_unavailable`, `controller_routes_unavailable`,
`presentation_unavailable`, and `audio_unavailable`. It is
written only after those controllers have not been constructed and their
fallback routes are registered. The supervisor validates the child and pipe,
the retained exact Main identity, the current boot and canonical `normal_main`
owner tuple, and the terminal install-journal digest, then atomically publishes
root-owned mode `0600` `/run/fogcast/fpgadev-boot-v2.json`. The canonical proof
contains schema 2 and the exact supervisor, Main, and agent
PID/start-time/executable tuples defined below. Unknown, missing, duplicate,
reordered, or noncanonical fields fail closed. Main, supervisor, or agent exit
removes the proof and durably transitions ownership to `recovery_required`; a
same-boot replacement cannot mint another proof.

`AdmissionUsable` means the canonical schema-2 proof matches the current boot,
owner session/generation, terminal journal digest, protected profile digest,
capabilities, resolved inventory, and live exact supervisor, Main, and agent
tuples. Normal admission, pre-intent development admission, and the immediate
pre-dispatch revalidation all require `AdmissionUsable`; absent, replaced,
PID-reused, extra, or executable-mismatched Main processes fail closed.
`CleanupAuthoritative` means the canonical durably published schema-2 proof
matches the current boot, owner session/generation, terminal journal digest,
protected profile digest, capabilities, and resolved inventory, and the exact
recorded supervisor tuple is proven absent. After durably fencing
`recovery_required`, a same-boot replacement may open pidfds only for a
still-live Main or agent that independently matches its complete proof tuple.
A proof is stale for cleanup only when those boot/owner/journal/profile/
capability/inventory bindings mismatch; expected absence of the recorded
supervisor is required, not staleness. Absent, malformed, incomplete, or stale
cleanup authority permits no scan-derived signal and requires reboot. PID 1
owns orphan reaping.

The inherited pipe is close-on-exec everywhere except the one child write end.
Agent start plus the complete receipt read share one five-second deadline. The
supervisor reads at most 1024 bytes through EOF, requires exactly one canonical
newline-terminated JSON object and no second line/trailing data, and requires
the child to close the pipe. The exact field order is `schema`, `pid`,
`start_time`, `executable_device`, `executable_inode`, `executable_sha256`,
`profile_sha256`, `capabilities`; schema is 1 and capabilities is the exact
ordered five-string array above. It then matches PID/start time and executable
identity to the child handle and `/proc` under the same deadline.
On timeout, malformed or partial receipt, identity mismatch, or premature child
exit, the supervisor closes both inherited-pipe ends, terminates and reaps the
exact child through its retained process handle, commits `recovery_required`,
and publishes no boot proof.

The boot proof is schema 2, is at most 16 KiB, and has exact canonical field order
`schema`, `boot_id`, `owner_session`, `owner_generation`, `supervisor_pid`,
`supervisor_start_time`, `supervisor_executable_device`,
`supervisor_executable_inode`, `supervisor_executable_sha256`, `main_pid`,
`main_start_time`, `main_executable_device`, `main_executable_inode`,
`main_executable_sha256`, `agent_pid`,
`agent_start_time`, `agent_executable_device`, `agent_executable_inode`,
`agent_executable_sha256`, `profile_sha256`, `journal_sha256`, `capabilities`,
`resolved_inventory`, followed by one newline. `resolved_inventory` is the
current-boot `ResolvedInventoryV1` below. Its parent directory is root-owned mode `0700`; the
file is root-owned mode `0600`, regular, link count one, opened no-follow, and
atomically replaced plus file/directory fsynced. Publication and removal occur
while the supervisor holds the shared owner lock. Admission and cleanup apply
the distinct `AdmissionUsable` and `CleanupAuthoritative` predicates above.
Every path and address string accepted into the journal is at most 255 UTF-8
bytes, `start_sources` contains at most 16 entries, and all other inventory
arrays have the exact fixed cardinality above. Before committing `terminal`,
the installer canonically serializes a worst-case proof using 20-digit unsigned
device/inode/PID/start/generation values and the fully resolved inventory shape;
installation fails unless that byte string including newline is at most 16 KiB.

The sole install journal path is
`/var/lib/fogcast/fpgadev-install-v1.json`; its parent is root-owned mode `0700`
and its root-owned regular link-count-one file is mode `0600`, opened no-follow,
bounded to 1 MiB, strict duplicate/unknown/trailing-data rejecting, and
canonical newline-terminated JSON. Top-level field order is `schema`, `state`,
`install_boot_id`, `package_sha256`, `previous_config_sha256`, `inventory`,
`sources`; schema is 1. State is exactly one of `prepared`,
`trampoline_installed`, `owner_initialized`, `sources_disabled`,
`supervisor_installed`, `terminal`, `uninstalling`, or `restored`. `inventory`
is the exact `InventoryV1` object below. `sources` is a canonical path-sorted,
duplicate-free array of objects with exact order `path`, `kind`, `mode`,
`sha256`, `backup_path`, `backup_sha256`, `disabled_state`,
`disabled_sha256`; `sha256` binds the original bytes, `disabled_state` is exactly
`absent`, `inert_replacement`, or `approved_trampoline`, and `disabled_sha256`
is empty for `absent` or binds the installed replacement. Exactly one
proven-earliest boot-dispatcher source may be `approved_trampoline`; its hash and
fixed argv are package-attested, it may only invoke `recover-install` with the
held-lock handoff, and all other sources are absent or inert. Reboot-volatile device/inode values
are never persisted. Every backup is a
separate protected regular link-count-one mode-`0600` file below the journal's
root-owned mode-`0700` backup directory and is size/hash bound. Every journal
replacement and backup creation is file-and-directory-fsynced. `terminal` is
the immutable installed record used by all later boots; boot-local state never
rewrites it. Only an explicitly authorized uninstall, while holding both locks
in order, may replace it with `uninstalling` and then `restored`; `restored` is
immutable and cannot admit development.

Task 8's immutable terminal install journal owns the exact legacy runtime and
start-source inventory captured from the protected pre-install configuration.
`InventoryV1` has fixed fields: the optional cast executable immutable
expectation `(path,sha256)`, configured input-uinput/framebuffer/native-command/
token-file path expectations `(path,type,rdev)`, canonical input-listen,
cast-RTP, and cast-control `(network,address)` endpoints, the Main command-FIFO
expectation, and the ordered start-source bindings/backups. Only character or
block device expectations carry stable `rdev`; FIFO/runtime device/inode values
are never persisted across boots. The installer derives
these fields only from a strictly parsed protected prior agent configuration
and the inventoried launch sources; any nonempty value in those authoritative
worker, route-path, or endpoint fields that
cannot be represented, resolved, or bound makes installation fail. Executable
matches use `/proc/<pid>/exe` device/inode/hash, path descriptors use opened-fd
device/inode/rdev identity, and endpoint matches join `/proc/<pid>/fd`
`socket:[inode]` values to exact entries in `/proc/net/{tcp,tcp6,udp,udp6}`.
Missing/transient `/proc` evidence fails closed except for a process proven to
have exited during the same bounded scan. The Main FIFO is phase-classified:
only the exact attested Main process may hold it before dispatch, and no holder
is allowed after stable Main absence.

The exact `InventoryV1` canonical field order is `schema`, `main_executable`,
`cast_executable`, `input_uinput`, `cast_framebuffer`, `cast_native_command`,
`cast_token_file`, `input_listen`, `cast_rtp`, `cast_control`, `main_fifo`, and
`start_sources`. Schema is 1. An immutable path expectation uses exact field
order `path`, `kind`, `rdev`, `sha256`; `rdev` is zero unless kind is character
or block device, and `sha256` is empty unless kind is a protected regular
executable/source. Absent optional facilities are the JSON literal `null`,
never an empty or omitted binding. A network binding uses
exact field order `network`, `address`; absent optional endpoints are `null`.
`start_sources` is path-sorted and duplicate-free and each element uses the
same protected source binding stored in the top-level `sources` array. The
closed v1 authority is the pinned Main executable binding plus the strictly
parsed prior `agentconfig.Config`: `CastBinary` is the only external
cast/offload/presentation worker; `InputUInputPath`, `CastFramebuffer`,
`CastNativeCmd`, and `CastTokenFile` are the only target-private route paths;
and `InputListenAddress`, `CastRTPAddress`, and `CastControlAddress` are the only
network endpoints. The inventoried start sources are the only process launch
authority after installation. Package/Main attestations bind the exact binaries
and source commits that establish this closed set. A future profile or binary
introducing another nonempty worker/route field is schema-incompatible and must
fail installation until `InventoryV1` is revised and reapproved. Static Task 6
synthesis evidence, not this dynamic inventory, proves the experiment has no
video/audio/PLL/SDRAM/save/storage/external-output interface; the verifier may
not manufacture those facts from an empty runtime scan.

On every boot, before starting children, the supervisor validates protected
static executable/device expectations and every launch source's terminal
`disabled_state`; it never expects a disabled source to retain its original
pre-install identity/hash. Every path observation uses metadata-only
`O_PATH|O_NOFOLLOW|O_CLOEXEC` beneath the protected root followed by `fstat`;
it never opens a FIFO for reading/writing or a device for I/O. It creates the final
`ResolvedInventoryV1` only after Main readiness and the agent receipt, when the
required Main FIFO exists, and before publishing the boot proof. Its canonical
field names/order mirror `InventoryV1`; each resolved path object has exact
order `path`, `kind`, `state`, `device`, `inode`, `rdev`, `sha256`, and network
endpoints remain the immutable `network,address` pair. Route/object `state` is
exactly `present` or `expected_absent`; start-source `state` is exactly
`disabled_absent`, `disabled_inert`, or `approved_trampoline`. A present object's current type/rdev/hash must
match the immutable expectation, while device/inode are explicitly boot-local.
Only a configured optional runtime route such as the cast token/native path may
resolve as `expected_absent`; its device/inode/rdev are zero and SHA-256 is
empty, and both later path appearance and a descriptor whose link target names
that path fail verification. Required Main FIFO/input/device/executable paths
must be present after readiness. If two logical configured paths resolve to the
same current-boot identity, both fields retain that identity and scanning
deduplicates the object while applying the strictest phase rule: the sole
pre-dispatch exception remains the exact attested Main holding the Main FIFO;
after Main absence no holder is permitted. A `disabled_absent` source must stay
absent. A `disabled_inert` source must no-follow resolve to the exact
terminal-journal replacement hash/type/mode and is never an executable starter.
The sole `approved_trampoline` must resolve to its exact package-attested
hash/type/mode and fixed recover-install argv; it is the only surviving source
allowed to launch, and only into the held-lock supervisor chain. Any
original-hash source reappearance or second launcher fails closed. The entire resolved object is
embedded in the boot proof and therefore bound to current boot, owner,
supervisor, agent, profile, and journal digest. Task 7 scans
against this resolved object and revalidates opened-fd identity where the path
still exists; it also matches deleted descriptor targets by their recorded
path plus boot-local device/inode. Tests recreate the FIFO and devtmpfs fixtures
with different inodes across simulated boots and require the new proof to bind
the new identities while the immutable journal stays byte-identical; they also
cover Main-created FIFO timing, expected-absent route appearance, disabled
source absence/inert replacement/original reappearance, exact sole trampoline
launch and second-launcher rejection, and aliased native-command/Main-FIFO
paths.
Agent start, complete receipt, metadata-only resolution, and durable proof
publication share the one cumulative five-second deadline; timeout at any point
follows the failed-receipt child cleanup/fence path and makes no proof usable.

Task 7 defines the exact semantic boundary:
`MaintenanceGate.Enter(context.Context) (MaintenanceStatus,
MaintenanceUnlock, error)`, where the returned unlock is non-nil only after the
shared install flock has been acquired and `MaintenanceStatus` contains the immutable terminal journal SHA-256 and
`InventoryV1`; and `QuiescenceVerifier.VerifyPreDispatch(context.Context,
MaintenanceStatus, hardwareowner.Record) error`,
`VerifyPostMain(context.Context, MaintenanceStatus, hardwareowner.Record)
(PolicySubsystemProof, error)`, and `VerifyPressedInput(context.Context,
MaintenanceStatus, hardwareowner.Record) error`. Task 8 implements these
interfaces and modifies Task 7 production composition to inject them. Task 7
does not define an interim journal byte grammar. Until Task 8 is integrated,
the Task 7 production constructor is deliberately unavailable and fails before
durable intent; fixture implementations exercise the lifecycle.

The sole install lock is
`/var/lock/fogcast/fpgadev-install.lock`. Its parent is root-owned mode `0700`;
the root-owned regular link-count-one file is mode `0600`, opened with
`O_NOFOLLOW|O_CLOEXEC`, identity-checked after open, and protected by an
exclusive nonblocking-retry `flock` bounded by the caller context. Every
Install, Recover, Uninstall, maintenance read, normal admission, supervisor
boot, development preflight, and development run uses that same lock. The
global acquisition order is install lock then owner lock; release order is
owner then install. No code may acquire them in reverse. Task 7 holds the
install lock from maintenance status through its owner-lock terminal fence and
releases it only after the owner lock; normal admission holds it through its
own existing check/use fence.

At boot, `recover-install` retains the opened install-lock descriptor across
its exec of the supervisor by clearing close-on-exec only for that exact fd and
passing its numeric value through one fixed inherited-fd argument. The
supervisor validates the descriptor's device/inode/type/UID/mode and held-flock
identity before acquiring the owner lock. It releases owner then install only
after recovery, child readiness, and boot-proof publication have reached their
durable terminal. A missing/invalid inherited descriptor fails before reset or
child creation. Thus there is no unlocked gap between journal/source recovery
and owner admission. Tests cover concurrent script commands, development and
normal admission contention, wrong/reused inherited fds, exec failure, crash
while either/both locks are held, and prove kernel fd/flock release permits only
the next correctly ordered recovery process.

Before intent and immediately before dispatch, the quiescence check requires
`AdmissionUsable`, including the one exact live Main and absence of any extra or
replaced Main, and confirms the current owner tuple and terminal journal binding.
It does not classify the attested Main's command FIFO
as an auxiliary route. After the complete Main process set is stably absent,
qualification scans the journal's exact inventory and rejects every surviving
inventoried worker, descriptor, stream, route, or unexpected Main-FIFO holder.
Pressed-input neutrality is proven by the changed boot plus the
identity-bound receipt that the input controller has never been constructed on
that boot. Opening a fresh `/dev/uinput` control descriptor and writing raw
events is forbidden and is not evidence about the prior virtual device.

`/run` is cleared by boot. On every boot, including each reboot requested by a
development run, recovery first reconciles any old-boot owner record, then the
sole supervisor starts Main and the agent and may publish exactly one proof for
that boot. The persistent journal and its digest do not change. If Main or the
agent exits on that same boot, the live supervisor removes the proof and commits
`recovery_required`. If the supervisor exits, the proof's live-supervisor check
fails immediately and a replacement first commits the same-boot fence. When a
valid schema-2 proof exists, it opens identity-bound pidfds for only the recorded
Main and agent, terminates them, and proves their `/proc` identities disappear;
PID 1/service management owns orphan reaping. When the supervisor died before a
valid proof was durably published, the replacement signals no scanned or
unrecorded process and requires a kernel reboot to clear possible orphans. The
live supervisor directly terminates and reaps only children for which it retains
the original process handles. Neither a service restart nor another agent child
may republish it until a later kernel boot ID has been proven and recovery has
completed.

### No-owner qualification after compatibility loading

The compatibility load leaves persistent hardware state after Main exits, so
process absence and USERMODE alone are not a no-owner checkpoint. While the
record still names `compat_main` as the quiescing owner, the FogCast recovery
adapter must release or reset every old exclusive lease and verify all of these
conditions within an independent two-second no-owner qualification deadline:

- `main_process_set` and `command_fifo`: the complete executable-identity and
  PID/start-time process set is stably absent for 250 ms, so the kernel has
  closed its FIFO and device descriptors;
- `fpga_bridges`: apply the pinned Main baseline's disable sequence—FPGA
  interface module zero, SDR port zero, bridge module reset `7`, and L3/NIC-301
  remap `1`. Read back the first three readable registers exactly. Because the
  source-bound L3 `remap` register is write-only, record the exact issued write
  rather than fabricating a readback, and independently enumerate dynamic Linux
  bridge entries by `name`, requiring the exact four-name inventory and exact
  disabled `state` tokens above after the sequence;
- `fpga_programming`: require FPGA-manager USERMODE, `CTRL.EN=0`,
  `CTRL.AXICFGEN=0`, all configuration-pull controls clear, and no programming
  process or mapping;
- `core_input_saves`: after stable Main absence, require every exact
  install-inventoried input/offload process, stream, route, and device
  descriptor absent; validate the successor-boot quiescence proof that no
  FogCast input controller or virtual input device was constructed on this
  boot; and verify the experiment contains no input, SDRAM, save, or storage
  interface. No fresh uinput control descriptor may be used as a release
  capability;
- `native_video_audio`: require the experiment's synthesis allowlist to prove
  no video, audio, PLL, SDRAM, or external-output interface and verify no
  retained Main presentation/offload process remains; and
- `core_protocol`: write GPO zero with readback and require two stable HELLO
  samples, proving the new image neither implements the normal core protocol
  nor advances from inherited state.

M2 makes one focused preparation classification for `fpga_generation`: after
the checks above, the already configured `020_linux_mailbox` image is a
non-owning candidate resource, not an active generation lease. This is valid
only because its hash-bound static policy proves that its sole observable is
the HPS GPI HELLO word, HELLO is held until exact START, GPO is zero, every
bridge is reset, and it has no external or shared-memory dependency. Any other
bitstream, failed readback, or non-HELLO word cannot use this classification
and remains `recovery_required`. The subsequent atomic transfer is the first
grant of the prepared FPGA generation and GPI/GPO access to `fpgadev`.

Only a complete successful qualification permits the durable `no_owner`
commit. Its commit is the receipt that all normal leases are released/reset and
the candidate is inert; a partial qualification preserves the conservative
`compat_main` tuple and requires reboot recovery. For this bounded release path
only, the coordinator's recovery adapter may temporarily map the specified
bridge reset/status and GPO/GPI registers under the quiescing-owner record. It
may only apply the exact reset, source-bound write, readable-register
readback, independent bridge-state, zero, and stable-sample operations
listed above; it must unmap every recovery mapping before the durable
`no_owner` commit.

### Ordered operation

The command performs these phases under the shared inter-process lock:

1. Resolve the private disposable-target designation using existing
   operator-controlled configuration and verify the exact target identity.
2. Require the explicit development profile and root privilege. Through the
   Task 8 semantic fence, require the immutable terminal journal and a
   current-boot `AdmissionUsable` quiescence proof bound to its digest and validate it against a
   canonical `normal_main` owner record. Validate compatibility Main readiness
   within its one cumulative deadline and the absence of a second development
   operation. Task 7 alone fails closed here until Task 8 provides the concrete
   journal and proof adapters.
3. Validate the staged manifest, descriptor-bound canonical synthesis-resource
   evidence, regular-file/no-symlink artifact, declared board, experiment,
   build lane, size, and SHA-256. Only
   `020_linux_mailbox`, board `misterpi`, and filename `top.rbf` are accepted by
   protocol version 1. Require the evidence tuple to match the manifest and its
   observed resource counts to match the fixed experiment allowlist. Retain the
   protected staging-directory descriptor and
   artifact `(device,inode,size,sha256)` binding; immediately before FIFO
   dispatch, reopen beneath that descriptor with no-follow semantics and require
   the exact same binding.
4. Allocate a fresh session and generation above durable high-water, snapshot
   the existing Main process set by executable identity plus PID and process
   start time, durably commit `recovering_intent`, activate the shared
   admission/restart fence, and revalidate the current-boot `AdmissionUsable`
   supervisor/Main/agent proof before FIFO dispatch. Permit the attested Main's expected command-FIFO
   descriptor at this phase; it is the dispatch interface, not an auxiliary
   route.
5. Invoke a new synchronous bounded FIFO writer exactly once with
   `load_core <validated-private-path>\n`. The writer uses no detached goroutine
   and cannot complete after it returns. Its result distinguishes `attempted`
   from `completed`, but every invocation is treated as potentially consumed;
   every error from this point takes the post-attempt recovery path. Immediately
   after the invocation returns, commit owner phase `load_attempted` before any
   further observation.
6. Observe the entire expected Main executable process set, including
   replacements and double-fork descendants, until no matching PID/start-time
   instance exists for a continuous 250 ms while every Main launch source and
   supervisor remains fenced. The five-second Main timeout includes this
   stability interval.
7. Run every bounded no-owner qualification above, including the post-Main
   exact journal-inventory scan and successor-boot pressed-input proof, readable bridge
   reset readbacks, the source-bound write-only L3 write plus independent Linux
   bridge-state observations, programming-drive release, subsystem
   release/inertness checks, GPO clear, and stable HELLO. Then durably commit
   the true `no_owner` checkpoint.
   A failure before this checkpoint preserves Main as the quiescing owner in
   the durable record; it never assumes absence or release.
8. Atomically transfer the complete FPGA-generation and GPI/GPO lease set to
   the development generation by committing `fpgadev_active`. Only after this
   commit may the development owner map or touch GPI/GPO for START, ACK, or any
   other mailbox operation; the earlier recovery-only qualification exception
   grants no development access and has already unmapped.
9. Map the single page containing `0xff706000`; verify GPO `0x10` and GPI
   `0x14` are aligned and within it. Accesses are aligned little-endian 32-bit
   loads/stores with compiler and CPU ordering barriers and GPO readback after
   every write. Execute the mailbox protocol and unmap on every exit path.
   Package-internal progress callbacks durably commit `hello_observed` after
   stable HELLO, `message_partial` after the first accepted DATA byte,
   `end_ack_written` after successful END ACK/readback, and `done_observed`
   only after stable terminal DONE confirmation. DONE validation and terminal
   hold complete while `Observation.TerminalWord` is still zero; the callback
   runs next, and only its success assigns the nonzero terminal word. Hold or
   callback failure therefore preserves the prior phase with terminal word
   `00000000`. Callback failure stops the protocol, preserves the partial
   observation, and cannot fabricate a later phase.
10. Preserve the last successfully durable protocol phase and determine the
    exact terminal primary result. Durably move the owner record to
    `recovery_required` first, release the shared owner lock after that fence
    is durable, then exclusively persist that one final result before
    requesting a bounded reboot on both success and failure. An unlock failure
    is therefore known before result creation and becomes `state_store_failed`
    when it is the first failure. If the
    owner transition fails, the earlier post-intent record remains fenced. If
    that is the first failure, the one result uses `state_store_failed`; an
    earlier primary remains primary and the transition error is retained as a
    later recovery/store failure. The result never claims a phase later than
    the last successful owner-record commit. If result creation itself fails,
    no substitute or partial result is published, the owner remains in its
    already-fenced state, and the command still attempts bounded reboot.

Before `recovering_intent` is committed, a failure releases the lock and leaves
Main and `normal_main` state untouched. At and after that commit, every failure
remains durably fenced and requires reset recovery, even when the FIFO writer reports
that no complete write was observed. If reboot cannot be requested, the target
stays safely fenced with a root-only diagnostic and requires manual recovery.

The command never starts a second Main process, silently resumes a failed
session, clears an interrupted same-boot record, claims clean FPGA release, or
exposes a raw target path through the public FogCast API.

Development builds provide four root-only, run-ID-bound fault interfaces that
production packaging omits. `fault-arm --run-id ID` creates a `0600` marker in
`/run/fogcast`. `inspect --run-id ID` prints exactly one line:

```text
FOGCAST_FPGA_DEV_INSPECT run_id=<run_id> session=<session> generation=<generation> phase=load_attempted pid=<pid> start_time=<start_time> executable_sha256=<sha256>
```

The active command's root-only runtime diagnostic contains those fields plus
the executable device and inode, is written before the `load_attempted` block,
and contains no path or target identity. It also owns a no-symlink, root-only
`0600` Unix `SOCK_SEQPACKET` fault endpoint in `/run/fogcast`. Only at this armed
deterministic checkpoint may the command release the process lock after the
durable fence and diagnostic are file-and-directory-fsynced, and after the
endpoint has been bound, chmoded, lstat-verified, and its parent directory
fsynced. The socket itself is not fsynced. It then blocks without touching
hardware or state. A normal/unarmed run never releases the lock.

`fault-kill --run-id ID` acquires the now-available owner lock, matches
run/session/generation/phase, validates the endpoint metadata, connects, and
uses `SO_PEERCRED` to require that its peer is the exact diagnostic PID whose
start time plus executable device, inode, and hash still match `/proc`. It sends
one fixed request containing the matching in-memory run/session/generation.
The armed development process accepts only a root peer and an exact request,
then calls `SIGKILL` on its own current process ID. The external command never
signals a numeric PID; endpoint closure/process disappearance confirms the
self-kill. Exit, stale socket, peer replacement, and PID-reuse races fail safely
without signaling another process. Success prints exactly
`FOGCAST_FPGA_DEV_FAULT_KILLED run_id=<run_id>`. `recovery-reboot --run-id ID`
requests reboot only for the matching fenced post-intent record. Boot recovery
owns removal of the matching orphaned staging directory and runtime marker;
`/run` reset removes hook state. Hostile run-ID, PID reuse, record, ownership,
mode, or production-profile mismatches fail without mutation.
If a test cancels the armed checkpoint instead of killing it, the active
command must reacquire the lock and revalidate the unchanged owner record and
diagnostic before any further observation; mismatch takes the post-intent
recovery path.

The crash-cycle host transport pins and attests the local OpenSSH client, uses
quiet mode, and supplies the fixed safely quoted remote command beginning
`exec mister-fpga-dev run ...`, so the login shell is replaced by the armed
process. After self-SIGKILL the blocking SSH child must exit 255 with empty
captured stdout and stderr and therefore no result framing. The durable
diagnostic is obtained only through the separate exact `inspect` response and
protected host trace; the separate `fault-kill` response supplies its exact
success line. Any other killed-session exit or output is a failed fault cycle.

## Cross-repository artifact contract

`misteross` stages a private directory containing the RBF and a minimal JSON
development manifest. The target tool accepts schema version 1 with exactly
these required fields:

```json
{
  "schema": 1,
  "run_id": "32 lowercase hexadecimal characters",
  "experiment": "020_linux_mailbox",
  "board": "misterpi",
  "build_lane": "oss",
  "artifact_filename": "top.rbf",
  "artifact_size": 7007204,
  "artifact_sha256": "64 lowercase hexadecimal characters",
  "source_commit": "40 lowercase hexadecimal characters"
}
```

`build_lane` is `oss` or `oracle`. `artifact_size` is an integer from 1 through
16,777,216 inclusive. `run_id` is freshly generated for each invocation and is
an evidence-correlation value, not a credential or target identity. A run ID
whose result path already exists is rejected before intent; old evidence is
never replaced by a later invocation. Unknown fields, duplicate JSON keys,
non-canonical hashes, any `artifact_filename` other than the exact literal
`top.rbf`, symlinks, ownership/mode violations, and content/hash mismatches are
rejected before dispatch.

The same protected staging directory also contains canonical
`resource_evidence.json`. This is synthesis-observation evidence, not an
operator assertion. Its schema version is 1 and its exact field order is
`schema`, `experiment`, `board`, `build_lane`, `source_commit`,
`artifact_sha256`, `synthesis_report_sha256`, `clock_inputs`,
`external_input_ports`, `external_output_ports`, `bidirectional_ports`,
`hps_general_purpose_interfaces`, `pll_blocks`, `dsp_blocks`,
`block_memory_bits`, `lutram_bits`, `sdram_interfaces`,
`signing_key_sha256`, and `signature`. `schema` is JSON integer 1. Every
resource value is a JSON integer in the inclusive range 0 through 4294967295;
negative, fractional, exponent, string, Boolean, or null spellings are rejected.
The experiment, board, lane, source-commit, and artifact-hash strings use the
manifest's exact grammar and literals. Both report and key hashes are exactly 64
lowercase hexadecimal characters; `signature` is exactly 128 lowercase
hexadecimal characters encoding one Ed25519 signature. The complete compact
object plus one newline is at most 2048 bytes.

The signing-key file is exactly the raw 32-byte Ed25519 seed, not PEM, DER,
PKCS#8, or OpenSSH text. It is a no-follow regular link-count-one mode-`0600`
file owned by the invoking host user and named only by
`FOGCAST_DEV_SIGNING_KEY`. The package public-key input is exactly the decoded
raw 32-byte Ed25519 public key with the same protected-file rules, named only by
`FOGCAST_DEV_EVIDENCE_PUBLIC_KEY`. `signing_key_sha256` is SHA-256 over those
decoded 32 public-key bytes, never over a pathname, hex text, or container
encoding. Both variables are environment-only and are absent from argv/logs.

The OSS and oracle report parsers generate the unsigned canonical prefix only
after independently parsing their lane's real synthesis report and enforcing
the experiment policy. `misteross` signs the compact canonical object containing
every field through `signing_key_sha256` (without `signature` and with one
newline) using the operator's host-only development signing key, then emits the
complete canonical object. The private key is never packaged, copied to the
target, committed, or passed in argv. The tagged ARM `mister-fpga-dev` binary
contains the corresponding Ed25519 public key; its package manifest records the
public-key SHA-256, and the integration ledger requires the signer-derived and
binary-embedded public-key hashes to match before target contact.

The target opens the evidence beneath the retained staging-directory descriptor
with the same root-owned, mode-`0600`, regular, link-count-one and no-follow rules
as the manifest; enforces the 2048-byte bound and exact grammar; requires its
lane/source/artifact tuple to equal the validated manifest; requires
`signing_key_sha256` to equal the embedded public-key hash; and verifies the
signature before using any count. Qualification then accepts only one clock
input, zero other external input/output/bidirectional ports, exactly one HPS
general-purpose interface, and zero PLL, DSP, block-memory, LUTRAM, or SDRAM
resources. The source-side lane parser, signer, and target parser/verifier have
independent hostile fixtures. An arbitrary RBF, copied evidence, or
caller-provided Boolean cannot mint an accepted proof without the approved
signing key. Supporting a different resource shape or signing key requires a
new reviewed package/evidence binding and operator approval.

The wire shape is exactly:

```json
{"schema":1,"experiment":"020_linux_mailbox","board":"misterpi","build_lane":"oss","source_commit":"40-lowercase-hex","artifact_sha256":"64-lowercase-hex","synthesis_report_sha256":"64-lowercase-hex","clock_inputs":1,"external_input_ports":0,"external_output_ports":0,"bidirectional_ports":0,"hps_general_purpose_interfaces":1,"pll_blocks":0,"dsp_blocks":0,"block_memory_bits":0,"lutram_bits":0,"sdram_interfaces":0,"signing_key_sha256":"64-lowercase-hex","signature":"128-lowercase-hex"}
```

The descriptive hash placeholders above state grammar and are not literal
accepted values; fixtures substitute canonical lowercase hexadecimal strings.

Both repositories implement this exact interoperability fixture. The raw seed
is `9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60`;
the derived raw public key is
`d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a`;
and its SHA-256 is
`21fe31dfa154a261626bf854046fd2271b7bed4b6abe45aa58877ef47f9721b9`.
The unsigned message is the compact example above truncated after the
`signing_key_sha256` value and closed with `}` plus one newline, with source
commit replaced by forty `1` characters, artifact hash by sixty-four `2`
characters, report hash by sixty-four `3` characters, and the exact public-key
hash above. It is 625 bytes, has SHA-256
`4b8fc9e56d478137f28d85694e4239318bf1c203dc9684e7670c2bba7707a3f5`,
and its exact signature is
`94638b568071dfebbfb18448dd6b5f2740ffca480d33dbdc225e785009535f24da90df6df2f46d3bc18ab253e8e8ad8261347fe1f2b49543b1d2d3bc599cba05`.
FogCast verifies this fixture with Go's standard-library `crypto/ed25519`.
`misteross` vendors the public-domain RFC 8032 reference operations in one
hash-reviewed Python-standard-library-only module and tests this fixture plus
the RFC 8032 empty-message vector; no ambient crypto package or executable is
used.

The interoperability seed/public key is test-only and is forbidden in every
production package, bundle, and integration ledger. FogCast package creation,
`misteross` bundle signing, target verification, and integration preflight each
reject the exact fixture public-key hash
`21fe31dfa154a261626bf854046fd2271b7bed4b6abe45aa58877ef47f9721b9`.
The real development seed is generated once from the host operating system's
cryptographic random source into the protected ignored 32-byte file, is never
printed, and must derive a different public-key hash. Test fixtures may select
the known seed only through an explicit package-local test seam that production
entry points cannot invoke.

`mister-fpga-dev preflight` performs only designation, profile, privilege,
manifest/artifact, result-collision, owner-admission, Main-readiness, and tool-
identity checks. It does not allocate a generation, write owner state, invoke
the FIFO, or load an RBF. Success exits `0`, writes exactly
`FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n` to stdout, and writes nothing to stderr.
A preflight failure exits `2`, writes nothing to stdout, and writes exactly one
newline-terminated, path-free stderr line:

```text
FOGCAST_FPGA_DEV_PREFLIGHT code=<code>
```

`code` is exactly one of `designation_failed`, `profile_disabled`,
`privilege_required`, `manifest_rejected`, `result_conflict`, or
`ownership_conflict`. This grammar contains no run ID when manifest validation
did not establish one. The separate `run` command repeats those checks, then
allocates the generation and commits intent. Any `run` failure before durable
intent exits `2`, writes nothing to stdout or the result store, and writes
exactly one path-free stderr line
`FOGCAST_FPGA_DEV_RUN code=<code>`, where `code` is one of the six values above
plus `generation_failed` or `state_store_failed`. These two values are never
preflight-command outcomes.

### Persistent result schema version 1

After a validated run ID, fresh generation, and durable `recovering_intent`,
every execution that reaches a terminal phase attempts exactly one exclusive
create of the canonical result file at
`/var/lib/fogcast/fpga-dev/results/result-<run_id>.json`. The directory is
root-owned `0700`, the atomically replaced file is `0600`, and `<run_id>` is the
validated manifest value. Only forced process death or a result-create failure
may leave it absent; neither case authorizes a second create or weakens the
owner fence. The JSON contains these fields in this order and ends with one
newline:

```json
{
  "schema": 1,
  "run_id": "32 lowercase hexadecimal characters",
  "generation": 42,
  "session": "32 lowercase hexadecimal characters",
  "mode": "updating",
  "experiment": "020_linux_mailbox",
  "build_lane": "oss",
  "artifact_sha256": "64 lowercase hexadecimal characters",
  "source_commit": "40 lowercase hexadecimal characters",
  "phase": "done_observed",
  "primary_code": "ok",
  "primary_detail": "",
  "payload_hex": "4f53532046504741204f4b0a",
  "payload_length": 12,
  "payload_sha256": "64 lowercase hexadecimal characters",
  "terminal_word": "d3130c00",
  "recovery_request": "pending",
  "elapsed_ms": 125
}
```

`generation` is the nonzero durable owner generation, `session` is the exact
active development session, and `mode` is exactly `updating`. `phase` is exactly one
of `intent_committed`, `load_attempted`, `main_absent`, `lease_active`,
`hello_observed`, `message_partial`, `end_ack_written`, or `done_observed`.
`primary_code` is exactly one of `ok`, `load_dispatch_failed`,
`main_handoff_timeout`, `no_owner_qualification_failed`, `state_store_failed`,
`mmio_failed`, `protocol_violation`, `message_timeout`, or `payload_mismatch`.
`primary_detail` is empty for success and otherwise one stable, bounded,
path-free diagnostic. `payload_hex` is lowercase hex for all bytes accepted
before failure; its SHA-256 and length always describe those exact bytes,
including the standard empty-payload hash. `terminal_word` is eight lowercase
hex digits and is `00000000` unless stable DONE was observed.
`recovery_request` is `pending` or `failed`; target output never uses
`verified`. The terminal owner transition is attempted before result creation
so the exclusive result is never forced to predict a later state-store
outcome. The result is then persisted with `pending` before the reboot request
using exclusive create. If that bounded request returns an error, one
conditional atomic update may change only `recovery_request` from `pending` to
`failed`; it requires the existing file to match the exact run ID, session,
generation, mode, artifact/source binding, and every primary/payload/elapsed
field supplied by the caller. Every other existing file, repeated update, or
field change is a collision and remains byte-for-byte unchanged. A result-
creation failure publishes no file and cannot unwind or weaken the durable
fence. `elapsed_ms` is a nonnegative integer.

Both writers and readers reject unknown fields, duplicate keys, invalid enums,
non-canonical hex, inconsistent payload length/hash, mismatched artifact/source
bindings, a success without the exact payload and terminal word, and any
result filename not derived from its `run_id`.

On success, target stdout contains exactly these two newline-terminated lines:

```text
FPGA> OSS FPGA OK
FOGCAST_FPGA_DEV_RESULT run_id=<run_id> primary=ok
```

On a post-intent failure it emits no `FPGA>` line and, when the exclusive result
create succeeded, writes exactly one result-framing line with the stable
primary code. A result-create failure emits no result-framing line and writes
no stdout, exits `2`, and writes exactly this path-free line to stderr:

```text
FOGCAST_FPGA_DEV_RESULT_UNAVAILABLE code=state_store_failed
```

The in-memory command outcome is `state_store_failed` even if an earlier
protocol failure existed, because no authoritative primary record could be
published; the earlier failure remains in the private diagnostic chain. A
crash after the durable `recovery_required` transition but before result create
has no framing at all. Preflight uses only its separate grammar above. A
persisted result, rather than SSH stdout delivery, is the authoritative
target-side primary record; its absence after a post-intent disconnect is a
failed run that still requires reconnect and recovery verification. Boot
recovery consumes the already-durable fence whether the create failed or the
process crashed in that window.

The staging directory is created under target-private `/tmp` storage with mode
`0700`; contained files use mode `0600`. Neither the manifest nor result record
contains credentials or the private target identity. Generated artifacts and
results remain ignored and are not committed.

`misteross` gains an explicit FogCast development transport rather than
changing the meaning of the existing MiSTer transport. Its preflight retains
the current board and Main-binary attestations, validates the installed
`mister-fpga-dev` identity, stages the bundle, invokes the command without
placing credentials in arguments. Any post-intent disconnect provisionally
requires reconnect and readiness recovery; it is accepted as the expected
successful intermediate event only after a valid result is retrieved. After
reconnect the host retrieves that result by `run_id`, verifies its artifact and
source bindings, completes the readiness observation in the host evidence, and
removes the target-local result only after the host copy is durable. Missing,
duplicate, mismatched, or malformed results fail the run without skipping
recovery/readiness verification.

Regenerated Dropbear keys are handled only within the operator-authorized
private target record. Reconnection must re-resolve the private designation
and re-run exact target/Main readiness attestations; IP, MAC, or a newly
accepted host key alone is insufficient.

## Safety and recovery

- All FPGA programming is volatile. M2 does not write flash, replace the SD
  image, or install an unattended updater.
- The operation is development-profile-only and local/private. Production
  images must not contain or enable this path.
- One command holds the shared inter-process lock through the durable terminal
  `recovery_required` commit, except for the explicitly armed `load_attempted`
  fault checkpoint described above. After that terminal fence is durable it
  releases the owner lock before exclusive result creation and reboot; every
  admission/restart path rejects `recovery_required`. At the armed checkpoint
  the already-durable owner record alone fences admission and restart while the
  command is blocked and touches nothing. Only a failure before durable intent
  may leave `normal_main` unchanged.
- Main owns initial programming through the single command. Ownership transfers
  only through the durable no-owner checkpoint and atomic development-lease
  commit after stable absence of the complete Main process set.
- The reboot is an explicit process-scoped recovery boundary, matching the
  current runtime's inability to prove clean in-process release.
- A timeout exists independently for Main exit, HELLO, each mailbox advance,
  total message, reboot request, reconnect, and readiness verification.
- A successful payload does not erase a cleanup or recovery failure. Primary
  and recovery results are reported separately.
- The existing designated development kit has standing mutation/reboot
  authorization under ADR 0002. Other targets remain blocked without their
  own authorization and rollback prerequisites.

Protocol version 1 fixes a 10 ms mailbox poll interval and these upper bounds:

| Phase | Timeout |
| --- | ---: |
| compatibility Main exit after dispatch | 5 seconds |
| no-owner qualification after stable Main absence | 2 seconds |
| stable HELLO after development lease transfer and GPO clear | 2 seconds |
| each DATA, END, or DONE advance | 1 second |
| complete START-through-stable-DONE transaction | 10 seconds |
| reboot request | 5 seconds |
| target reconnect | 120 seconds |
| post-reconnect FogCast/Main readiness | 30 seconds |

Implementation may wake earlier but may not silently lengthen a bound. A
different bound is a reviewed protocol/lifecycle change, not a deployment
setting.

## Testing strategy

### `misteross` software tests

- Verilator proves HELLO is stable before START.
- Incorrect START and ACK words do not advance state.
- Every byte, sequence, DATA ACK, END ACK, stable DONE, and terminal hold is
  checked.
- A parameterized simulation transaction accepts DATA sequence `255` followed
  by `0`, proving the defined modulo-256 wrap independently of the fixed
  12-byte production message.
- Reset/reconfiguration restarts at HELLO and sequence zero.
- RTL/source policy proves exactly one allowed HPS general-purpose primitive
  and no excluded resource or external-output dependency.
- OSS and oracle manifest/report parsers prove the per-experiment resource
  allowlist, emit canonical artifact-bound `resource_evidence.json`, and reject
  extra or missing primitives, ports, memories, PLLs, or DSPs.
- Transport tests cover canonical bundle generation, remote command formation,
  result parsing, disconnect/reboot classification, and recovery-attestation
  failure.

### FogCast software tests

- Protocol tests cover every valid word and each invalid field independently.
- The decoder requires two identical samples for every word and rejects
  duplicates, skipped sequences, changes between samples, early END/DONE,
  payload overflow, wrong text, and every timeout.
- A protocol-unit fixture accepts sequence `255` followed by `0` before the
  independent 256-byte payload bound rejects a 257th DATA byte.
- Artifact tests reject malformed/duplicate-key JSON, unknown fields,
  non-regular files, symlinks, unsafe names, ownership/mode errors, size bounds,
  hash mismatch, absent or mismatched resource evidence, arbitrary-RBF evidence
  reuse, wrong/unknown signing key, malformed or invalid signature, and every
  forbidden nonzero resource count.
- Lifecycle tests cover every durable owner-state transition, monotonic fresh
  generations, agent/supervisor fencing, process death at every phase,
  same-boot restart/adoption refusal after unexpected Main, agent, or supervisor death, new-boot reconciliation, no-owner persistence,
  atomic lease transfer, and separate preservation of primary/recovery errors.
- Crash-boundary tests use an actual helper process and real fixture store. The
  helper exits without defers immediately before and immediately after each
  successful durable replacement. The parent asserts the surviving canonical
  record, absence of a result/reboot request, closed process resources, and
  same-boot admission refusal for every post-intent boundary. A crash before
  the first intent replacement leaves canonical `normal_main` and permits a
  fresh normal admission; the matrix asserts that distinct pre-intent outcome.
  Injected `Replace` errors are retained as a separate store-failure matrix and
  are not described as crashes.
- Development-profile tests prove cast, presentation, audio, input, and
  controller-route facilities are not constructed, their public routes return
  unavailable without hardware calls, and the inherited-pipe agent receipt and
  supervisor boot proof bind the exact supervisor/child/current boot/owner/journal tuple.
  Phase-aware tests permit only the attested Main FIFO before dispatch and,
  after stable Main absence, detect every individually inventoried auxiliary
  worker, descriptor, stream, or route. Serialization race tests pause an agent after admission and a supervisor
  after validation, prove development intent cannot interleave before their
  durable terminal/started records, then prove neither can dispatch after
  development intent wins the lock.
- FIFO tests prove the synchronous writer cannot write after return, distinguish
  attempted/completed observations, and send every invocation error through
  recovery. Process tests cover original-PID exit, double-fork replacement,
  PID reuse/start-time mismatch, transient absence, and supervisor restart
  attempts.
- Linux register tests cover USERMODE gating, page/offset/alignment bounds,
  ordered 32-bit access, the three exact readable bridge-disable readbacks, the
  exact source-bound write-only L3 write, the exact four-name independent Linux
  bridge-state inventory,
  programming-drive release, GPO clear, write readback, and unmap on every path
  without touching real hardware; the platform adapter receives focused
  compile/static review.
- No-owner qualification tests fail each lease release/readback independently,
  preserve the conservative Main tuple, and prove that only the exact
  hash-bound, bridge-isolated, stable-HELLO experiment can receive the focused
  inert-preparation classification.
- Result tests lock the exact schema, field order, session/mode/generation bindings, enums, payload/hash
  consistency, filename derivation, canonical framing, and malformed/duplicate
  rejection.
- Relevant Go tests, formatting, vet/static checks, and race tests must pass in
  the implementation environment. The implementation handoff records any
  inherited baseline failure separately and never attributes it to M2.

### Build and comparison

- Verilator simulation passes first.
- The pinned OSS lane builds, routes, meets the 50 MHz requirement, and emits a
  hash-bound RBF and manifest.
- Quartus Lite 17.0.2 builds the identical RTL/protocol intent as an explicit
  oracle and emits its own hash-bound RBF and reports.
- Comparison checks protocol/source hashes, target device, clock intent,
  allowed hard block, unexpected hard blocks, timing status, and artifact
  identity without expecting the two RBF byte streams to match.

### Hardware-in-the-loop matrix

Before HIL, bind exact FogCast, `misteross`, toolchain, target image, Main,
development-tool, OSS RBF, oracle RBF, and configuration hashes. Resolve and
verify the privately designated target without recording its private identity.

Run exactly:

- three consecutive OSS load/mailbox/reboot/readiness cycles; and
- one Quartus-oracle load/mailbox/reboot/readiness cycle; and
- one OSS fault-injection cycle in which the host observes the durable
  owner-record phase `load_attempted`, invokes the run-ID-bound `fault-kill`
  command before its no-owner checkpoint, verifies that same-boot Main/agent
  admission and restart remain fenced, then requests reboot and verifies
  reconciled readiness. A development-build-only test hook blocks immediately
  after that phase commit so this kill point is deterministic; production
  packaging omits the hook.

Each successful cycle records separately:

1. artifact and target preflight;
2. volatile load dispatch;
3. observed Main exit;
4. FPGA manager operating state;
5. exact decoded payload and payload SHA-256;
6. complete mailbox acknowledgement through END and stable DONE observation;
7. reboot request;
8. reconnect; and
9. normal FogCast/Main readiness after recovery.

The fault-injection cycle additionally records the pre-kill durable generation,
owner state, and `load_attempted` phase, missing primary result as expected,
same-boot admission
refusal, supervisor non-restart, reboot/reset reconciliation, cleared fence,
and restored exact readiness. It must leave no stale owner record, lock,
process, mapping, staging directory, or result.

The Linux register/message observation on physical hardware may support
HIL-observed status for this narrow FPGA-to-HPS behavior. It does not establish
HDMI, audio, input, save, latency, broader MiSTer compatibility, production
security, reproducibility, or full migration-stage acceptance.

## Failure handling

Every phase produces a stable error code and bounded diagnostic. Expected
operator-facing categories are designation/identity failure, development
profile failure, ownership conflict, artifact/manifest rejection, load
dispatch failure, Main handoff timeout, MMIO failure, protocol violation,
message timeout, payload mismatch, reboot-request failure, reconnect failure,
and readiness failure.

The first failure remains the primary result. A terminal owner-store, result-
store, or unlock failure becomes `state_store_failed` when no earlier primary
failure exists, so a successful mailbox cannot hide it. If an earlier primary
already makes the run failed, later recovery/store errors are joined to the
private diagnostic chain but do not require a second durable error journal.
Reboot-request failure is represented by `recovery_request=failed` when the
result exists; if its conditional update also fails, the still-running command
reports failure and the host treats a missing disconnect or ambiguous result as
failed. Recovery is required at or after durable `recovering_intent`, including
a crash before or during FIFO dispatch. A successful reboot/readiness check
does not rewrite a failed primary as success. Reconnect and post-reconnect
readiness are the authoritative recovery evidence and are host outcomes, not
target `primary_code` values.

Complete logs remain on the development host. Target-local diagnostics contain
no credentials, private target identity, or private paths and use root-only
development state storage. Interrupted or ambiguous operations reconcile as
requiring recovery; they never assume the target is idle.

## Deliverables

### FogCast repository

- focused `internal/fpgadev` policy/protocol/platform boundary;
- target-only `cmd/mister-fpga-dev` command;
- durable hardware-owner store and shared agent/Main-supervisor admission fence;
- synchronous bounded FIFO adapter and complete Main process-set observer;
- boot-time interrupted-maintenance reconciliation before normal supervisors;
- tests and development-profile packaging;
- private runbook and evidence schema updates; and
- immutable handoff recording implementation identity and verification.

### `misteross` repository

- `020_linux_mailbox` RTL, constraints, simulation, and expectations;
- generalized experiment-aware simulation/build/oracle policy;
- development bundle manifest support;
- explicit FogCast development transport;
- build/comparison/transport tests; and
- updated bring-up and HIL evidence.

## Exit criteria

M2 is complete only when:

1. both repositories' required software checks and independent reviews pass;
2. the OSS and Quartus artifacts satisfy their semantic and timing gates;
3. all four successful HIL cycles deliver exactly `OSS FPGA OK\n`, consume the
   END ACK, and expose stable DONE;
4. all four successful cycles recover through reboot and return to verified normal
   FogCast/Main readiness;
5. the fixed post-attempt kill cycle proves the durable fence blocks same-boot
   admission/restart and reboot reconciliation restores exact readiness;
6. no unexplained owner, process, register mapping, staging directory, or
   target-local diagnostic remains; and
7. the evidence handoff states the exact commits, hashes, commands, observed
   results, unobserved behavior, and remaining risks.

Completion of M2 does not complete FogCast Stage B or C and does not replace
the later HDMI test-pattern milestone.
