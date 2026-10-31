# TRWU8CBL bridge diagnostic: firmware error with matching resets

Physical trial: 2026-10-31. Diagnostic source:
`1009bb61fc2af604c4a09bc1946f7ce4b382cfe4`; preparation:
`cf0d345f9aeaa6457a4c41ac5870b2f015683923`. The matched candidate
`25c14e5b3f609e57` booted and distinguished the earlier combined rejection:
the firmware call returned error `4` while all required reset bits matched
before and after. The loader rejected and parked before native execution.
Factory Linux was subsequently restored, verified and the top kit released.

## Refreshed media and bounded attempt

Only the identified spare card was refreshed: reader serial `000000000819`,
3,980,394,496 bytes, resolved to `/dev/sda` and unmounted before writing.
The overwritten prefix was backed up and verified privately; the earlier
original-prefix backup was preserved separately. Raw readback of the new
150,994,944-byte image matched SHA-256
`9f2b21aad34eff23eea8b0f821e3368f99528cb51e6af2e61159d57b1ef72ed0`.
The original factory card was retained after authenticated baseline capture
and clean Linux poweroff. The user confirmed both physical card swaps.

The exact cable `DE25-Nano [3-11.1-iface0]`, FPGA ID `4362C0DD` and UART
interface `if02` at 115200 were used on TRWU8CBL only. Volatile programming
loaded the [prepared candidate](../2026-10-31-de25-bridge-diagnostics/README.md),
SOF SHA-256 `4a5f3d094356c4b26e984851b4356b4bceb42e1963bc14409c7c99a27ed044a0`.
The capture required fresh matching SPL and full U-Boot banners and the
observed `SOCFPGA_AGILEX5 # ` prompt before issuing `fesboot` exactly once.
The host checked complete serial banner lines to tolerate split reads and
reject an unexpected build. Diagnostic parsing was checked offline for all
flag combinations before use.

The existing bounds were retained: 90 seconds for volatile programming,
45 seconds for matching boot, and 45 seconds for the single launch attempt.
The attempt finished within those bounds and was not retried after rejection.

## Observed bridge result

The decisive serial records were:

```text
FES_NANO_BRIDGE BEGIN phase=before-load mask=0000000f before=0000000f
FES_NANO_BRIDGE RESULT phase=before-load smc=4 smc_hex=00000004 mask=0000000f before=0000000f after=0000000f smc_error=1 reset_mismatch=0
FES_NANO_HANDOFF REJECT bridge phase=before-load smc_error=1 reset_mismatch=0
### ERROR ### Please RESET the board ###
```

This establishes a returned firmware error, not a reset-bit mismatch. The
required bits were already set before the call and remained set. In the
pinned TF-A source, status `4` is `INTEL_SIP_SMC_STATUS_ERROR`; the bridge
service maps a negative result from the underlying bridge routine to that
generic status. This interface does not identify which poll/handshake failed.
The matching reset register does not prove traffic drained or global
containment completed and does not justify accepting the failed service.

The initial memory/EL and CPU/counter/watchdog checks did not reject. The
first bridge check stopped execution before peripheral resets, entry file
read/hash, `after-remove` handoff or native entry. None of the four native
handoff/entry/Go success markers appeared. Native peripheral/FPGA activation
and tenfoot remain unqualified.

The next firmware diagnostic should preserve and report the first failed
bridge stage and its status registers, including the already-reset starting
state. Keep first-error propagation and the existing rejection/park behavior;
the observed reset bits alone are not grounds for skipping the service or
ignoring its error. No firmware repair or extra hardware attempt is included
in this trial.

## Recovery and evidence

After the user restored the original card, the exact vendor reference SOF
SHA-256 `e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09`
restored factory Linux through volatile JTAG without a physical power cycle.
Authenticated inspection matched kernel `6.12.11-gd7d192a9ddd9`, root
`/dev/mmcblk0p2 ext3`, original card CID/capacity and Nano model. Boot UUID
changed from `dca09536-3152-4968-8951-941ce37c22f9` to
`fc82bf95-030e-4fb9-beba-4692fba0100f`.

The session logged out and closed, owned programmer clients finished, the
exact USB node returned to `root:root 0664`, and the kit claim was released.
The shared user JTAG broker was left running. Flash was not programmed and
the factory card was not reimaged; normal Linux login/shutdown can update
accounting. Autonomous QSPI boot after a physical power cycle was not retested.
Other boards and harnesses were untouched.

[evidence.json](evidence.json) binds the source/artifacts, raw diagnostic,
media write and restoration receipts. Complete serial/programmer logs and
one-off audit scripts are retained under
`/home/deano/fes-retained/de25-nano-bridge-trial-20261031/` with their hash index.
Private credentials and card-prefix backups are excluded. Retained card-swap
confirmations describe this completed trial and do not authorize a future run.
