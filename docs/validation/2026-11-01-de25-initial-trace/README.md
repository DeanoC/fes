# Nano initial CPU trace and ownership rejection

The bounded TRWU8CBL **trace diagnostic passed**: all 18 checkpoints from first
C entry to unconditional park were captured. First-C MPIDR was `0x81000000`
and EL was 3 throughout. The initial ownership check still **rejected** with
`CPUINRESET=0xf`; the later sample agrees and reset timeouts are zero. This
resolves the silent diagnostic sufficiently to observe the real boot gate.
It does not qualify native boot or core loading. See [evidence](evidence.json).

## Exact implementation and build

Tested source: `d3ec4a3928eac87477b67d7c92d6e63c4a0d1bc7`.
Diagnostic ID: `ec0cdc1da979aca3`.

| Artifact | SHA-256 |
| --- | --- |
| Volatile SOF | `c24f01011c18b87e75dccc47288ced97c15ada63ee97532d18f26fa145d65e1f` |
| SPL hex | `3dc1890123e4875104c17a9cd1fc0bd64d91d8d1a29fc2d32d905cef8889f8d7` |
| SPL ELF | `88b5ac280e505a31b4280e680b12dc96fd6654805ddcc1d6aa691f15ff27479e` |
| UART capture | `c5d16220283cc33cbc9acb3db7c94bc24dfc634014b6729cc68ff37538e886e5` |

The diagnostic now preserves the seed configuration except for the build ID,
including its driver selection. The earlier driver-stripping path is removed.
Both watchdog-start paths are excluded, the first initializer stops at the
report for either admission result, and the later initializer independently
stops on entry. Firmware invocation is independently removed. Startup source
and the three startup objects' text match the seed; the trace/cold records
remain in loaded OCRAM data. No native CPU/reset policy or firmware ABI changed.
Four focused tests, including both initializer stops under enabled watchdog
configuration, and all six affected checks pass. The build binds previous
HEAD plus exact input hashes; those bytes were committed before programming.

| Comparison | Outcome |
| --- | --- |
| Original early trace with stripped drivers | UART silent |
| Cold sample omitted; early trace and stripped drivers retained | UART silent |
| Seed configuration; only post-console trace | Console, six reads and park passed |
| Seed configuration; initial sample and full early trace restored | All 18 checkpoints, first/fresh sample and park passed |

These comparisons establish that the full observation path runs with the
preserved seed configuration. They do not isolate a particular stripped
configuration option as the cause of silence.

## Physical trace

One changed-image attempt ran from `2026-11-01T09:15:14.836743+00:00` to
`2026-11-01T09:15:22.364871+00:00`, within the 30-second query, 90-second programming,
40-second observation and 240-second overall bounds. The exact cable reported
`4362C0DD`; volatile programming and exact SPL/trace identity passed. First and
post-console observations were:

| Field | First sample | Post-console |
| --- | --- | --- |
| MPIDR / EL | `0x81000000` / `3` from first C trace | `0x81000000` / `3`, logical core `0` |
| Reset cause | `0x00000004` | `0x00000004` |
| Peripheral reset 0 | `0xff7fbfff` | not resampled by this report |
| Bridge reset | `0x0000004f` | not resampled by this report |
| CPU reset | `0x0000000f` | `0x0000000f` |
| CPU release | `0x00000000` | `0x00000000` |
| SDM boot kind | `4` | not resampled by this report |
| Initial valid | `0` | never replaces the initial proof |
| Reset timeouts | not sampled | `0x00000000` |

The final checkpoint and line confirm unconditional
`FES_NANO_CPU_DIAG PARK no-ddr no-storage no-bl31`. No DDR, SD/FIT loading,
BL31/native entry, bridge operation or core launch occurred. No native command,
SD/flash write, unchanged-image retry, card swap, reference restore or physical
power cycle was performed.

The observed active CPU contradicts interpreting all four reset bits as a
complete current CPU inventory. The existing native gate remains strict.
Further source-backed CPU-state observations are needed before deciding what
can prove unused cores are stopped; matching this one tuple must not grant
admission. The official [RC28 register map](https://docs.altera.com/v/u/resources/864455/agilex-5-cs-and-r1-suffix-opn-hps-register-map)
confirms offsets/read-only CPU-reset fields
but its descriptions do not resolve this observation. The pinned TF-A power
manager header supplies additional status addresses; their off-state meaning
still needs qualification before use as ownership evidence.

## Board and evidence retention

The user made this top kit exclusive and permits omitting factory restore.
The diagnostic remains loaded and parked with the original factory card
inserted. The continuing claim stays held. A separate read-only query still
reports `4362C0DD`; UART/programmer clients are closed, there is no UART owner
and top USB permissions are restored. Shared JTAG broker/clock, other kits
and harnesses were unchanged. Factory boot was not restored or freshly verified.

Exact source bundle, SOF/SPL/ELF/configuration, transformed sources, startup
comparisons, software results and raw receipts are retained with verified
checksums in `/home/deano/fes-retained/de25-nano-initial-trace-20261101/`.
