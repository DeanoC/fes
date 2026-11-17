# Nano acknowledgment wiring and already-reset bridge investigation

Base: `6924a6d9d801c1d309c214245218f57598ca55c0`. This is an off-board
investigation of the [stage-3 timeout](../2026-10-31-de25-firmware-first-failure/README.md).
No kit, card or device was accessed. Firmware, bitstreams, admission checks and
production compatibility remain unchanged.

The reference source has its optional warm-reset handshake disabled and no
explicit responder wired at its top-level system instance. The pinned Agilex5
firmware nevertheless requests that handshake unconditionally, including when
all requested bridges are already reset. The actual-C model reproduces the
observed stage-3 failure in that state. This is a source-level explanation
consistent with the board trace; it is not a decoded-netlist proof of the
prebuilt SOF's tie-offs.

## Reference source and signal path

The Terasic resource archive SHA-256 is
`f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`.
Its `Demonstration/SoC_FPGA/GHRD/output_files/golden_top_hps.sof` matches the
actual FPGA input used by the serial candidates and factory recovery:
`e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09`.

The source audit establishes:

- `hps_subsys/ip/hps_subsys/agilex_hps.ip` sets `Rst_hps_warm_en=false`.
- `golden_top.v`'s `qsys_top soc_inst` has no warm-handshake request or
  acknowledgment connections.
- The subsystem and HPS wrappers pass the named conduit through to
  `f2h_pending_rst_ack_n`, which reaches the hard primitive's
  `f2s_pending_rst_ack` port. This traces the available signal path; it does
  not establish a final electrical tie-off or interface polarity behavior.
- The archived HPS generation report identifies Quartus 25.1.1 build 125 and
  contains project/IP-variant mismatch warnings. The package does not provide
  a source-to-prebuilt-netlist identity proof. The installed 26.3 tools were
  not used to silently regenerate or upgrade that reference.

Altera's [HPS reset-interface documentation](https://docs.altera.com/r/docs/813752/26.1.1/hard-processor-system-component-reference-manual-agilex-5-socs/reset-signals?contentId=eb6Y1gV34nuWNo9TlCdUfw)
describes enabling the warm-reset option as exposing the request/ack conduit.
That supports the source interpretation, but does not replace inspection of
the actual generated design and its producer identity.

## Optional fabric acknowledgment and required hardware drain

The [Agilex 5 F2H reset sequence](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/f2h-bridge-reset-sequence?contentId=c_gmrOZ2YnB81JpJzHT2bg)
separates an optional warning/quiescence handshake with FPGA logic from the
hardware fence-and-drain request and acknowledgment. After drain, the bridge
is reset. Omitting the optional fabric warning does not supply a hardware
drain acknowledgment or establish that producers are inactive.
The [F2SDRAM sequence](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/f2sdram-bridge-reset-sequence?contentId=prXQhRfZNXRlsNYuatfXnQ)
makes the same distinction for that path.

The existing strict profile deliberately returns the first failed poll. It
currently treats every reported failure as fatal, including the optional
fabric handshake that this reference source does not implement. Merely
ignoring that timeout would not repair or qualify the hardware drain path.
The prior trace records only the first failure, so later poll outcomes remain
unknown on the board.

## Already-reset behavior: actual code and model

The pinned Terasic `socfpga_bridges_disable()` Agilex5 branch does not inspect
the initial bridge-reset bits before issuing its six polls. Its generic
comment about a bridge that cannot be reset twice is inside the other-family
`#else` branch. That shortcut also tests any selected reset bit, not all of
them; it cannot be transplanted into this multi-bridge admission path.

The comparison source from upstream ARM TF-A commit
[`9799b799fb51edef36607bcd534b5510aa4156bc`](https://github.com/ARM-software/arm-trusted-firmware/blob/9799b799fb51edef36607bcd534b5510aa4156bc/plat/intel/soc/common/soc/socfpga_reset_manager.c)
still has the unconditional Agilex5 flow. It is retained for comparison only;
the selected firmware pin was not changed.

Thirty-two host cases compile and execute the original and currently selected
strict C routine with undefined-behavior checks. Initial reset masks are
`0`, `4`, `7` and `15`. Fabric response and hardware-drain response are injected
independently. All calls still perform six polls and four reset writes; reset
state does not select a different flow. For initial reset mask `0x0f`:

| Fabric responder | Hardware drain response | Strict result | First failure |
| --- | --- | --- | --- |
| Absent | Absent | `-60` | Stage 3, FPGA acknowledgment |
| Absent | Present | `-60` | Stage 3, FPGA acknowledgment |
| Present | Absent | `-60` | Stage 4, hardware flush acknowledgment |
| Present | Present | `0` | None |

The missing-fabric-response cases capture ACK `0`, request `4` and reset `15`,
matching the relevant first-failure fields observed on TRWU8CBL. The original
vendor routine returns zero in these modeled failure cases because later
successful clear polls overwrite its return variable. That includes the
modeled hardware-drain failure, not just the optional fabric warning.

This executes the actual C flow, not a replacement bridge policy. The model
injects register/poll outcomes; it does not simulate physical FIFOs, pending
transactions, CDC, acknowledgment latching or timing. It is not drain proof.

## Proposed contract for an idempotent close

The cited reset sequence does not establish a raw-reset-bit shortcut. The
following is the proposed ownership contract, not implemented admission:

| Starting state | Required evidence/behavior |
| --- | --- |
| Cold, never enabled | Establish from the selected boot chain that no master or bridge was enabled and no transaction could be issued; verify closed state without inventing a prior drain |
| Previously closed by this owner | Reuse a completed, generation-bound containment result only while reset state and ownership remain unchanged |
| Active bridge | Stop all producers, use the fabric handshake when supported, require hardware fence/drain completion, then assert and verify reset |
| Unknown or partially reset | Keep rejection; current reset bits alone do not establish past drain or quiescence |

Any such behavior should extend the existing platform/ownership hooks and
sealed producer identity. It should not add a second compatibility or
lifecycle registry. A source grep found bridge enabling in TF-A BL2, but that
file is not compiled into the selected BL31 build; it is not proof that this
boot chain enabled a bridge. Conversely, one closed snapshot is not a complete
never-enabled boot-history proof.

The next implementation needs an explicit board capability/ownership contract
and paired cold-closed versus active-draining verification. If a fabric
responder is added instead, it must acknowledge real quiescence of every
relevant master. A constant acknowledgment or a request echo does not provide
that proof. The existing timeout rejection remains in force.

## Evidence and replay

[evidence.json](evidence.json) binds source selection, upstream snapshot,
audit/model results and the retained investigation files. The source audit,
32-case C model, consistency check and affected-change checks passed. The
archive's `SHA256SUMS` also covers its report and commit-preservation records.
Evidence is preserved under
`/home/deano/fes-retained/de25-nano-ack-investigation-20261031/`. To replay,
copy the retained files into a fresh directory directly under this FES
checkout's `out/`, restore the pinned vendor checkout and selected BL31 receipt
referenced by the scripts, then run `audit_reference.py` and
`exercise_closed_bridge.py`. Retained scripts do not program hardware.

The user also clarified that the earlier USB reconnect was an accidental cable
pull during the SD swap. The failure trace preceded it and factory restoration
was independently verified afterward. That explains the recovery capture gap;
it is not a new firmware fault or independent proof of a board power cycle.
