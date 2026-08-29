# FPGA Development Loader Fail-Stop Amendment

Status: Candidate for operator review

Date: 2026-08-28

Supersedes: the hostile-root, exhaustive same-boot cleanup, and signed-resource-evidence requirements of `2026-08-27-linux-mailbox-dev-loader-design.md`. All mailbox protocol, FPGA register/address, artifact compatibility, ownership, result, and HIL requirements not explicitly changed here remain in force.

## Decision

The MiSTer Pi is a private, disposable development target operated by one trusted developer. Reboot or power-cycle is an acceptable recovery boundary. The development loader therefore uses a fail-stop design: it prevents ordinary conflicting ownership and malformed inputs, but it does not attempt production-grade recovery from malicious root activity, hostile concurrent file replacement, or every possible same-boot process race.

When state becomes ambiguous after installation or FPGA mutation begins, the tool fences further development admission, requests a reboot, and stops. If Linux cannot request the reboot, the operator power-cycles the target. It does not adopt, scan for, or repair ambiguous hardware owners on the same boot.

## Threat and operating model

In scope:

- accidental double invocation;
- malformed or mismatched packages, manifests, RBFs, and configuration;
- ordinary process failure, timeout, disconnect, interrupted installation, or power loss;
- stale state left by a previous boot;
- incorrect FPGA ownership sequencing;
- incorrect MMIO addresses, register values, or mailbox ordering;
- a package transferred incompletely or with the wrong SHA-256;
- recovery on the next kernel boot.

Out of scope:

- a malicious or compromised root process;
- hostile replacement of protected files or directories during a checked operation;
- an attacker deliberately reusing PIDs, inodes, Unix peers, or signing material;
- concurrent manual mutation of FogCast files while its root command is running;
- uninterrupted service or same-boot recovery after an ambiguous post-mutation failure;
- production multi-user or production-fleet deployment.

The code must remain fail-closed for malformed input and observable accidental conflicts. Out-of-scope attacks must not drive implementation or acceptance complexity.

## Ownership and runtime lifecycle

The durable hardware-owner record and global install-then-owner lock order remain. Only the development supervisor may start the compatibility Main and development agent. The supervisor retains their child handles, starts Main once, verifies readiness, starts the agent once, receives its readiness receipt, and then publishes a boot-local ready record.

The boot-local ready record is `/run/fogcast/fpgadev-ready-v3.json`. It is one
compact JSON object plus one newline, at most 4096 bytes, with these exact keys
in this exact order:

```text
schema,boot_id,journal_sha256,owner_session,owner_generation,profile_sha256,
capabilities,supervisor_pid,supervisor_start_time,main_pid,main_start_time,
agent_pid,agent_start_time
```

`schema` is JSON integer `3`. Generation, PIDs, and start times are positive
JSON integers. Both SHA-256 values are exactly 64 lowercase hexadecimal
characters. Session and boot ID retain the existing canonical grammars.
`capabilities` is the existing exact ordered five-string development array.
Unknown, missing, duplicate, reordered, wrongly typed, trailing, or oversized
input is rejected.

Executable inode/hash binding, resolved device inode inventories, complete process scans, and adversarial PID-reuse defenses are not required for normal admission. PID plus kernel start time is sufficient for accidental stale-process detection in this threat model. The supervisor publishes the record atomically while holding the owner lock, after Main readiness and the agent receipt. It removes the record while holding the owner lock before fencing a detected failure. Normal hardware admission checks the current boot, terminal journal digest, owner tuple, profile hash, capability array, and live supervisor/Main/agent PID/start-time tuples while holding install then owner locks.

The supervisor starts children with parent-death signaling where supported. If Main or the agent exits unexpectedly, the live supervisor removes the ready record, writes `recovery_required` on a best-effort basis, requests reboot, and exits. If the supervisor itself is replaced or a new supervisor encounters same-boot ambiguous state, it does not scan, signal, adopt, or restart children; it requests reboot. The next boot performs reset and starts a fresh supervisor/Main/agent set.

No `CleanupAuthoritative` pidfd cleanup protocol, same-boot orphan adoption, exhaustive extra-Main scan, or same-boot proof remint is required.

## FPGA programming safety

The following remain mandatory because failures can invalidate the experiment or touch the wrong hardware:

- correct Cyclone V MMIO base addresses, offsets, masks, barriers, and register order;
- exact MisterPi/DE10-Nano-compatible RBF identity and SHA-256 binding;
- FPGA-manager USERMODE and released-drive checks;
- bridge/reset and GPO-zero ordering;
- one cumulative bounded readiness interval;
- mailbox signature/version/opcode/sequence/payload validation;
- bounded timeout and result recording;
- reboot after every attempted development load, successful or failed.

Programming-process and mapping checks may use direct observable process/mapping checks for known development tools. They need not prove global absence against every root process or defend against hostile process-table races. If the simple observation is unavailable or contradictory, the command aborts before MMIO mutation.

## Package and resource provenance

The trusted host produces two deliberately separate deterministic bundles:

1. a versioned software-install archive containing the fixed ARMv7 executables,
   recovery trampoline, and configuration/template files, bound by its archive
   SHA-256 and internal `manifest.sha256`; and
2. one per-run artifact bundle containing `top.rbf`, the nine-field FogCast
   artifact `manifest.json`, unsigned `resource_evidence.json`, and a sorted
   `bundle.sha256` that binds those three payload members.

Software installation is infrequent; artifact bundles are disposable inputs to
individual development runs. They are not combined into one archive. The
target independently verifies the software archive/member hashes during
installation and the artifact bundle member hashes plus expected
board/lane/source/artifact/resource tuple before each run.

Ed25519 signing keys, embedded public keys, fixture-key rejection, signature interoperability vectors, and signature verification are removed. They do not protect against a trusted host/root operator error beyond the existing hash and tuple checks, and they add a second key-distribution system that this development environment does not need.

`resource_evidence.json` uses unsigned schema 2, superseding the signed schema 1
object. It is one compact JSON object plus one newline, at most 2048 bytes,
with these exact keys in this exact order:

```text
schema,experiment,board,build_lane,source_commit,artifact_sha256,
synthesis_report_sha256,clock_inputs,external_input_ports,
external_output_ports,bidirectional_ports,hps_general_purpose_interfaces,
pll_blocks,dsp_blocks,block_memory_bits,lutram_bits,sdram_interfaces
```

`schema` is JSON integer `2`. Resource counts are JSON integers in
`0..4294967295`; they are never strings, floats, exponent forms, negative, or
null. Commit and SHA-256 fields retain their existing lowercase hexadecimal
grammars; the experiment, board, and lane use their existing fixed values.
Unknown, missing, duplicate, reordered, trailing, or oversized input is
rejected. There are no signing-key or signature fields.

The object is derived from the build report. The trusted host producer opens
and verifies that report and binds its SHA-256 into the object. The target does
not receive or independently re-parse the synthesis report; it verifies the
evidence member through `bundle.sha256`, checks report-hash grammar, matches the
experiment/board/lane/source/artifact tuple to the artifact manifest and opened
RBF, and rejects mismatched or forbidden counts. It is trusted build evidence,
not a cryptographic statement from an independent authority.

## Installation and boot recovery

`install-profile`, `recover-install`, and `uninstall-profile` remain root-only and serialized by the install lock. Installation uses a unique private transfer directory, verifies the software archive SHA-256 and member manifest, and copies the verified staged recovery executable plus required install members into a protected persistent stage under `/var/lib/fogcast/fpgadev-staging/<software-manifest-sha256>/`. That directory and every member are durable before boot dispatch changes. It remains available through `terminal` and is removed only after `restored`. Installation then writes fixed target paths and keeps byte-identical backups of changed launch sources.

Before writing the first nonterminal journal, installation durably installs a
small recovery trampoline bound to the persistent staged recovery executable
and atomically makes it the earliest boot dispatcher. The trampoline never
depends on a not-yet-installed `/usr/bin` member.
The original dispatcher is already present in the durable backup set. With no
journal, or with a completed `restored` journal, the trampoline chains to the
original dispatcher. With a nonterminal journal, it acquires install then
owner, starts no Main or agent, idempotently completes or rolls back the
recorded transition, and reboots. No inventoried legacy starter runs before
this decision.

The coarse durable journal phases are:

1. `prepared` — package and existing sources validated; backups durable;
2. `installed` — development binaries/configuration and recovery launcher durable;
3. `terminal` — legacy launch sources disabled and reboot required;
4. `uninstalling` — uninstall is durably authorized; the recovery trampoline remains earliest, and disabling the supervisor plus restoring original sources are idempotent pending actions.
5. `restored` — every non-dispatcher source is restored and restoration of the original earliest dispatcher is durably authorized.

Recovery behavior is exact:

| Durable phase | Next-boot action before any Main/agent |
| --- | --- |
| absent | Chain to the original dispatcher. |
| `prepared` | The trampoline first verifies its persistent staged recovery helper against the helper hash embedded when the trampoline was published. If that helper is present and matches, it runs recovery. Recovery completes package installation when every other staged member matches; if another staged payload is missing/mismatched, the verified helper restores backups, advances to `restored`, restores the original dispatcher, and reboots. If the helper itself is absent or mismatched, the trampoline starts no Main, records a diagnostic, and remains fenced for operator replacement of the verified software package plus power-cycle. |
| `installed` | Idempotently disable every inventoried legacy source, advance to `terminal`, and reboot. On any failure, retain the trampoline and reboot/power-cycle for another recovery attempt. |
| `terminal` | Start the development supervisor; the supervisor alone starts Main and agent. |
| `uninstalling` | Keep the trampoline earliest, idempotently disable the supervisor entry, restore every non-dispatcher backup and fsync it, then advance to `restored`. |
| `restored` | Idempotently restore the original earliest dispatcher last, fsync its parent, and chain to normal boot. The completed journal may remain as installation history. |

Atomic rename and directory fsync remain for the journal, backups, and installed executable/configuration files. Fixed protected directories use root ownership with mode `0700`; files use `0600` or executable `0755` as appropriate. Basic no-follow opens are retained where they are already simple. Descriptor continuity across every pathname check, link-count policing on every read, inode pinning across reboots, and a crash test at every individual syscall boundary are not required.

Any nonterminal journal blocks development admission. `recover-install` on the next boot follows the table above, then requests another reboot if needed. It does not attempt live same-boot repair after an ambiguous failure. Uninstall first writes and fsyncs `uninstalling` while retaining the recovery trampoline, then idempotently disables the supervisor entry and restores backups, reaches durable `restored`, and only then restores the original earliest dispatcher. Cleanup of unused development files occurs after `restored` and is not needed for boot correctness.

## Fault handling and diagnostics

The development fault hook remains only where it provides useful HIL coverage: kill the active loader after its durable intent marker and prove that same-boot development admission is fenced until reboot. It need not defend against malicious Unix peers, PID reuse after validation, endpoint replacement, or every packet-framing race. A root-only diagnostic records the run ID and last durable phase. The host judges recovery from the durable record, reboot/disconnect, and post-boot readiness—not from an exact SSH stderr/status grammar.

## Verification and acceptance

Software acceptance requires:

- focused unit tests for valid and malformed software and artifact bundle manifests, RBF, and resource evidence;
- lock contention and owner-state tests;
- Main readiness and agent receipt tests;
- install tests for normal completion, durable persistent staging, `prepared` reboot recovery with no fixed-path command installed and no transfer directory, interruption in each coarse journal phase, interruption immediately before/after trampoline publication, interruption immediately before/after the `uninstalling` fsync and supervisor disablement, interruption midway through multi-source disable/restore, next-boot completion/rollback before any Main starts, and uninstall restoration;
- loader tests for success, protocol failure, timeout, pre-mutation abort, and post-intent reboot fencing;
- tagged and untagged Go tests, affected race tests, vet, formatting, ARMv7 cross-build, deterministic package checks, and untagged development-command absence;
- no unresolved functional Critical or Important review finding within this stated threat model.

The test matrix does not require hostile-root TOCTOU, exhaustive PID/inode reuse, every-fsync crash injection, or cryptographic signing-key tests.

HIL acceptance requires:

- install and reboot into the development profile;
- verify Main/menu and agent readiness;
- three OSS RBF mailbox cycles;
- one Quartus-oracle RBF mailbox cycle;
- one post-intent kill followed by reboot recovery;
- uninstall and verify normal Main/menu recovery.

Reboot or power-cycle is a valid recovery action and is recorded as such. It is not treated as a failure of the development architecture.

## Consequences

This amendment intentionally trades defense-in-depth and same-boot recovery for a smaller, understandable development system. It should produce a working RBF development loop sooner and reduce the risk that untested recovery machinery is more fragile than the loader itself.

The design is not suitable for an untrusted target, shared root access, unattended production service, or fleet deployment. Any move to those environments requires a new threat model and a separate hardening plan; the removed controls are not silently inherited as future requirements.
