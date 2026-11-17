# TRWU8CBL matched boot and first native handoff trial

Source: `8539d23287de3b5af75ea8c6594aa2e7e4977345`, branch
`codex/de25-embedded-os`. Physical test date: 2026-10-31. The matched boot chain
booted on TRWU8CBL, but the first bridge containment check rejected the handoff.
The native entry and Go probe did not run. Factory Linux was subsequently
restored and verified, and the board was released.

## Boot chain and spare media

The prepared [candidate](../2026-10-31-de25-nano-handoff/README.md) was used
without rebuilding or relaxing its gates. Its build ID is `82a6d9421d08e0e3`;
the programmed SOF SHA-256 is
`a1f778ea42ca395ab0994fe0e185e2ce0dfc8d03ecdc1a0b8c6b4d60806e93b4`.
Only the top board's exact cable `DE25-Nano [3-11.1-iface0]`, FPGA ID `4362C0DD`,
and UART interface `if02` at 115200 were used. The lower board was not accessed.

The user explicitly authorized wiping the spare 4 GB card. Its reader serial
was `000000000819` and its actual capacity was 3,980,394,496 bytes. The overwritten
150,994,944-byte prefix was backed up privately and verified before writing.
Raw readback of the written image matched SHA-256
`150a59128c68662d273b4e5a94477cfa637364f1399ad79e39b5446050ec0ea0`.
The original factory card was kept aside after authenticated baseline capture
and confirmed clean Linux poweroff. No flash programming or factory-card
reimaging was performed; normal Linux login/shutdown can update accounting.

Volatile JTAG programming succeeded. The serial capture shows the exact
expected SPL and U-Boot suffixes, the selected contained BL31 build label,
successful DDR calibration and 1 GiB of RAM. The loader stopped at
`SOCFPGA_AGILEX5 # `. The first host runner incorrectly expected `=> ` and
timed out without sending `fesboot`. The existing boot was preserved; a
read-only `version` command rechecked the live build and vendor prompt before
`fesboot` was sent exactly once. No second programming or containment attempt
was made during the probe.

## Handoff result

The complete command output is retained. Its decisive portion is:

```text
FES_NANO_BOOT id=82a6d9421d08e0e3
FES_NANO_MEMORY ram_top=c0000000 sp=bfb00800 malloc=bfb09000..bff0b000 reloc=bff0b000 fdt=bfb02ed0
FES_NANO_HANDOFF REJECT bridge service/readback
### ERROR ### Please RESET the board ###
```

The live LMB map reported DDR `0x80000000..0xbfffffff`, with reservations at
`0..0xffffff` and `0xbeb00ff0..0xbfffffff`. The memory/EL and initial
CPU/counter/watchdog checks did not reject. Execution stopped inside the first
`contain()` call, before its peripheral-reset sequence, payload file read,
entry hashing, late handoff or native execution.

The rejection combines SMC failure and bridge reset-bit mismatch. This capture
does not distinguish them or record the actual service return/reset register
values. It does not prove bridge drain or containment completed. The loader
parked and was not retried or bypassed. None of the four required handoff/entry/
Go success markers appeared. Native SD, USB, network, display, FPGA activation
and tenfoot remain unqualified.

The next diagnostic build should record the SMC return and bridge reset
register before/after the call, then correlate any failing firmware handshake.
Keep the current rejection and park behavior. The pinned vendor routine's
already-reset handling is a source-review lead, not a demonstrated root cause.

## Restoration and retained evidence

The user reinserted the original factory card. The exact vendor reference SOF
SHA-256 `e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09`
was loaded through volatile JTAG. Factory Linux reached its login prompt
without a physical power cycle. Authenticated inspection matched the original
kernel `6.12.11-gd7d192a9ddd9`, root `/dev/mmcblk0p2 ext3`, card CID/capacity,
and Nano board model. Boot UUID changed from
`34322962-bd96-4fed-a4d5-424327fffebf` to
`dca09536-3152-4968-8951-941ce37c22f9`.

This verifies recovery of the factory Linux baseline through the reference
JTAG image; autonomous QSPI boot after a power cycle was not retested. The UART
session was logged out and closed, owned programmer processes finished, and
the exact USB node's ownership/mode were restored to `root:root 0664`. The
shared user JTAG broker was left running. The top-board claim was released.

[evidence.json](evidence.json) identifies artifacts, phases, recovery and all
retained file digests. The immutable candidate remains under
`/home/deano/fes-retained/de25-nano-handoff-20261031/`. The new serial logs,
programmer logs, receipts and one-off audit scripts are retained under
`/home/deano/fes-retained/de25-nano-serial-test-20261031/` with a separate
manifest and SHA-256 index. Audit scripts and physical confirmations describe
this completed trial and are not reusable board authorization. Private login
files, manual login excerpts and the original spare-card backup are excluded.
