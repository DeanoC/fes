# Nano debug readback and console control

The retained trace could not be read through HPS debug, and one changed console
control also emitted no UART bytes. Neither result establishes CPU execution,
park, cold ownership, DDR, native boot or core loading. The native admission
contract is unchanged. See [machine-readable evidence](evidence.json).

The later [seed-configuration control](../2026-11-01-de25-seed-prefix-control/README.md)
restored console/register output and confirmed unconditional park. It replaced
the image described here; this record preserves the earlier failed comparison.

## HPS readback

The original `15a3809e5598e76c` trace remained loaded during one bounded
OpenOCD attempt. The intended path was the documented ADIv6 AXI system-bus AP
at `0x4000`, reading the 304-byte OCRAM record without a CPU target, halt,
reset or memory-write command. The exact top cable exposed only the FPGA TAP
`4362C0DD` and its three SLD nodes. The expected HPS DAP `4ba06477` was absent.
OpenOCD tried the configured chain after the mismatch and then failed DAP
initialization; this was not a pre-initialization fail-closed inventory gate.
No trace or reset-vector dump was produced. The temporary cable clock changed
from 15 MHz to 6 MHz for this attempt and was restored to 15 MHz, along with
USB permissions. No image reload occurred during the readback attempt.

The reference and combined SOF metadata explicitly report `HPS debug access
port (DAP): DISABLED`. This explains why that debug path was unavailable; it
has not been established as the cause of UART silence. The official
[OpenOCD guide](https://altera-fpga.github.io/rel-26.1.1/embedded-designs/agilex-5/e-series/premium-065b/openocd/ug-openocd-debug-agx5e-premium-065b/)
describes the system-bus path. Enabling the DAP mux is a configuration change
under [HPS_DAP_SPLIT_MODE](https://docs.altera.com/r/docs/683296/25.3/quartus-prime-pro-edition-settings-file-reference-manual/hps_dap_split_mode),
not proof that debug memory access is available in this retained image.

## Changed console control

Frozen source: `6a2bb09ac0fcb1857d9cea29c7530d3b60ef248d`.
Diagnostic ID: `12d8fdacd1c23b7d`.

| Artifact | SHA-256 |
| --- | --- |
| Volatile SOF | `aaba7d33e1098c0b497bf40be39dbe3b7755cb5fdb3e56126c1d9bb74c9ad34e` |
| SPL hex | `bef8bc65f6994b41ec9c158200f806c2a140fbf5aa48b0de3db1f7e4f5621d96` |
| SPL ELF | `c934f81e58c20b10e7ed7f9da075c53d7d93e43094e81eb3f90c27eddd96bcf7` |

This separate diagnostic removed the pre-console cold-proof call and omitted
`fes_nano_cold_begin` from the linked image. It retained the trace instrumentation
and the diagnostic's disabled storage, DDR and watchdog options. If console
initialization returned, it would print six register reads with before/after
markers and then park unconditionally. It could not proceed into native boot,
and later samples could not supply the missing initial proof. The three actual
startup objects' text matched the seed byte for byte. Four focused tests
(including 24 compiled C paths) and all six affected checks passed before
programming. The build receipt names the previous HEAD plus exact source hashes;
those bytes were committed as the frozen source above before the hardware trial.

The one attempt ran from `2026-11-01T08:49:26.523462+00:00` to
`2026-11-01T08:50:14.602210+00:00`. Exact-cable JTAG identity and volatile programming
passed. UART was empty through the 40-second observation deadline: no banner,
checkpoint, register sample or park confirmation. Skipping the cold-state sample
did not restore console output and does not isolate a failing instruction.
There was no unchanged candidate retry, native boot command or SD write.

## End state and next comparison

The user made TRWU8CBL exclusively available and permits omitting factory restore.
The diagnostic remains loaded, the original factory card remains inserted and
the existing claim is held. A subsequent read-only query still reports
`4362C0DD`. UART/programmer clients are closed, and exact top USB permissions
are restored. No factory recovery, power cycle, flash/card reimage or other-kit
operation was performed. The latest prior authenticated factory baseline and
clean shutdown are retained in the original trace trial; they are not a fresh
post-control factory verification.

The next comparison preserves the seed driver configuration and removes all
pre-console trace instrumentation while retaining the hard stop before DDR,
SD loading and firmware entry. That is a changed diagnostic, not a relaxation
of the native CPU/reset gate. The earlier startup attribution remains corrected:
this Nano builds `lowlevel_init_agilex5.S`; the generic ATF-related startup
hypothesis was not supported by the compiled objects.

Full receipts, tools, artifacts and a source bundle are retained under
`/home/deano/fes-retained/de25-nano-console-control-20261101/`.
