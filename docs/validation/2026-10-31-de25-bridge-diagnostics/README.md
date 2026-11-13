# Nano bridge diagnostics and next bounded test candidate

Hardware follow-up: the [bounded trial](../2026-10-31-de25-bridge-trial/README.md)
returned firmware error `4` with matching reset bits before/after. Native
execution was rejected; factory Linux was restored and the kit released.
The preparation record below describes the earlier off-board work.

Base: `7c5b8d834`. Diagnostic implementation: `1009bb61f`. This follows the
[first physical trial](../2026-10-31-de25-nano-serial-test/README.md), which booted
the matched chain but stopped at `REJECT bridge service/readback`. The board
was restored and released after that trial. This preparation accesses no UART,
JTAG, physical card or target and does not inherit that candidate's hardware
observations.

The new U-Boot adapter separates the service return from bridge reset
readback. It records `BEGIN` before the SMC and `RESULT` after it returns,
including the signed and hexadecimal service return, requested reset mask,
full reset register before/after, and independent `smc_error` and
`reset_mismatch` flags. Readback occurs even when the call returns an error.
Both `before-load` and `after-remove` handoff phases identify themselves.

The gate still requires a zero SMC return and all requested reset bits set.
Any service failure, missing reset bit or both observations rejects and parks
before continuing to peripheral resets. There is one SMC call per gate and no
retry, bypass or firmware/reset-sequence repair. `BEGIN` without `RESULT`
means the call/readback sequence did not complete; a host deadline cannot
preempt an unreturned firmware/MMIO operation. Register observations do not
establish global drain or identify a failing handshake's root cause.

## Validation

Seven focused host tests pass. The new tests compile and execute the actual
adapter's bridge function with injected SMC/MMIO replies under undefined
behavior sanitization. They cover 29 cases: both phases, successful and
positive/negative error returns, every required reset bit missing, extra
unrelated bits, and an already-closed bridge. They verify the before/call/after
ordering, one call, unconditional readback, both independent error flags and
unchanged acceptance. Existing memory, CPU/device policy and export tests pass.

The full Nano SPL and U-Boot cross-build passes against the pinned Terasic
source. The extracted U-Boot bytes contain the new diagnostic strings.
The previously selected contained BL31 and native entry remain byte-identical.
The prior loader is rejected by the package producer before output creation
because its recorded adapter source differs. Parent consistency and
generated-consumer checks and `make test-changed` pass. Exact job receipts and
retained file digests are recorded in [evidence.json](evidence.json).

The first affected-test invocation lacked `jsonschema` and
`rfc3986_validator`; its log is retained. The repository's pinned test
requirements were installed only under this task's ignored `out/` directory.
The affected tests then use the prior task head `7c5b8d834` as their base.

## Prepared artifacts and test bounds

Expected SPL/full U-Boot build ID: **`25c14e5b3f609e57`**.

| Artifact | SHA-256 |
| --- | --- |
| `native-probe.sof` | `4a5f3d094356c4b26e984851b4356b4bceb42e1963bc14409c7c99a27ed044a0` |
| `u-boot.itb` | `422e555500f96e4d63b6766d03bc190f8fbb1d20378802092438457060f71889` |
| `u-boot-spl-dtb.hex` | `dfb4848c6c1220779a8e0de2ecf19a797920deb92485d7e1935f2253d0af7f42` |
| `nano-probe-sd.img` | `9f2b21aad34eff23eea8b0f821e3368f99528cb51e6af2e61159d57b1ef72ed0` |

The image is 150,994,944 bytes (144 MiB); the authorized 4 GB spare is large
enough. It contains the exact new FIT at raw sector `0x200` and in FAT32, plus
the same native entry. Packaging verifies raw FIT bytes, FAT file readbacks,
MBR/BPB/backup geometry, source identities and configuration. The SOF combines
the exact new SPL with the unchanged vendor reference; PFG confirms the Nano
device and FPGA-first order. Embedded SPL bytes are not independently decoded.

The matched package and detailed `NEXT-TEST.md` are retained under
`/home/deano/fes-retained/de25-nano-bridge-diagnostics-20261031/test-kit/`.
Build receipts, selected exported sources, source patch and validation logs
are retained beside it with a SHA-256 index. The pinned compiler, vendor Git
archives and prior native payload are retained in the preceding archives;
the build receipts identify their exact selected paths and bytes.

The next trial requires refreshing the identified spare with this new image,
recording a fresh factory baseline and clean shutdown, and claiming only
TRWU8CBL. Use its exact cable/FPGA ID and UART interface. Bound volatile
programming to 90 seconds, matching-loader capture to 45 seconds and the single
`fesboot` attempt to 45 seconds. Require both matching banners and the observed
`SOCFPGA_AGILEX5 # ` prompt. Capture both diagnostic phases if reached. Any
rejection or missing result stops the attempt and triggers factory restoration.

A complete native pass still needs the four existing handoff/entry/Go markers.
Restore the original factory card and exact vendor-reference SOF, then verify
kernel/root/card/model and a fresh boot UUID before releasing the board. The
previous trial proved reference-SOF JTAG recovery without a physical power
cycle; repeat recovery verification for the next trial. Native peripherals,
FPGA runtime activation and tenfoot remain subsequent tests.
