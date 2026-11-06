# DE25 capture control and generated constraint repair

A fresh sealed standard Nano computer package clears two of the six previously
reported signoff rules: the obsolete JTAG filters (`TMC-20026`) and opposite
reset polarity inferred from capture control (`LNT-30023`). Four rules remain
open. No timing or Design Assistant waiver was added, and no hardware was
programmed. This is progress toward normal native core loading, not timing,
boot, physical display/input or DE10 feature-parity acceptance.

Source base: `3160327a847604a3d521b3adcda65b9ea6226a96`.
Implementation/proof: `a90d2e2f4480eb3aa2fabbd4457003714bd41626`.
Compiled source, including compiler-fixture correction:
`66c852f9c7f63c3535d86766ea451bb5a93200b3`.
Native source-inventory follow-up:
`78e1a9e8030a597f708e462952262aad52c977bc`.
Ownership remains `lane:deano`; no PR handoff or merge occurred.

## What changed and what proves it

The external-ROM variant now selects the prototype's `video.v`, with its
identity bound into the pre-synthesis source record, installed source and
independent generated-source verification. The redundant `armed` flag is
removed. Resetting blank history low preserves the requirement to observe a
real blank interval before starting capture after Hold/release. The predecessor
could select a different *inactive* writer bank after release; bank selection
is compared when capturing and write addresses/data only when valid.

The actual writer-control blocks are extracted from the candidate and the
captured `79d616e85e19d65f0d645b67a85ffe7e142dcdfc` predecessor. Yosys induction
proves observable control, write-enable/address and publication equivalence
with arbitrary reset, Hold, CE, blanking and returned-bank-token inputs. An
inverted capture condition must produce a proof counterexample. This proof
abstracts the returned token and does not prove memory initialisation, physical
reset timing or metastability.

A separate full dual-clock RTL comparison checks both implementations' pixels,
valid RAM writes, published rows and bank/token state. It covers release during
active video, random input histories, maximum-height frames, individual clock
stops and reset assertion while both clocks stop. The final source-bound run
passes 40,204,224 ticks, 55,148 byte writes, five publications, four
acknowledgements and four raster boundaries. The real C ROM/secure-provider and
ZX80/ZX81 RTL suite also passes using the selected new video source.

A read of an isolated copy of the preceding routed candidate finds zero
registers matching either `sink_data_buffer` or `src_data` filter. The original
QSF, QPF, SOF and timing-report hashes remain unchanged. The producer removes
only the exact obsolete type-qualified `set_false_path` command from four
generated JTAG SDC files. It preserves their other bytes, adds no broader
selection or replacement exception, and records reconstructable before/after
hashes. Additional active commands, source changes, missing files or receipt
changes reject. Compilation and independent package verification check the
complete repair. Deleting an exception preserves or strengthens constraints.

The native handoff source inventory now also includes `timing_sources.py`, so
that verifier cannot change outside future SDK/firmware source snapshots.
Thirty-seven existing native source/ROM/composition tests pass after that
follow-up. The qualified recovery image and its historical validator still
pass their unchanged pin checks; this did not access the board.

## Actual build and timing scope

The first fresh 3090 build stopped at the unchanged 120-second placement bound
(total 396.05 seconds). Its files/reports are preserved with 1,057 verified
members. A post-failure check found no remaining process in that owned project;
the 16-CPU host still had a load near 16. No other work was stopped.

A separate local Powerboat build used eight workers, the same pinned vendor
archive/Quartus Pro 26.3 and the existing normal-user shared lock. All ten
standard tasks completed within the same 120-second per-task bound; placement
took 88.25 seconds and total work 498.24 seconds. The changed host, worker
setting and source revision are recorded. This is not a paired QoR benchmark.
No clocks, I/O standards, timing requirements or bounds were relaxed.

| Multicorner reported minimum, ns | Previous candidate | Fresh candidate |
| --- | ---: | ---: |
| Setup | 2.002 | 1.969 |
| Hold | 0.002 | 0.000 |
| Recovery | 7.070 | 7.827 |
| Removal | 0.021 | 0.049 |
| Minimum pulse width | 0.220 | 0.220 |

All reported design-wide TNS values are zero and the clock inventory is
unchanged. Hold rounds to zero and provides no reported margin. This table
covers constrained paths; it does not cover the still-unconstrained pins.

The fresh package is
`dae4d85197e72feb06715915842d5cf4bd779748792a18112220179aeb042547`,
with build ID `60ca6b4faa763d56e6232eed59a95e9e`. Independent checks bind all
194 source inputs, generated sources/SDCs, both endpoint IDs and the package
contract. The actual new package, separate firmware ROM, resource maps and
private activation receipt pass through the real retained native store and
normal ABI/driver loader with no firmware call or SD containment. This is one
actual artifact selection, not a physical load/return cycle. A native boot chain
matched to this new package has not been built; the preceding chain remains
evidence only for its earlier inputs.

## Software checks and open work

The affected run completed 1,425 parent tests (two skips), generated-consumer
consistency (48 consumers), native/Avalon integration and all 1,584 FPGA cases
(75 skips). Its only two errors were old fake-compiler fixtures that lacked the
now-required generated JTAG SDC. Those fixtures were corrected, and all six
compiler-runner cases pass separately. The raw aggregate remains recorded as
failed; it was not relabelled green. The previously observed Coleco golden
failure did not recur in this run; no Coleco code or golden was changed.
Nine focused source/proof tests and the ROM/model suite also pass. Early query
and proof-driver errors are retained with the corrected final evidence.

Remaining signoff rules are missing output delay (30 ports), missing input
delay (two ports), multiple reset-release chains in two clock domains and
three intra-clock false paths into reset synchronizers. The output list
includes the asynchronous LED[7], 27 HDMI pixel/control signals and two I2C
outputs; the inputs are I2C SDA and HDMI interrupt. An asynchronous LED needs
an explicit endpoint classification, not an invented synchronous receiver.

The selected initializer writes ADV7513 `0xBA=0x60` (no clock delay). The
published video input setup/hold minima are 1.8/1.3 ns, but a complete pin
contract also needs the actual forwarded clock, selected transmitter phase
and board flight-time budget. No board delays were fabricated from that
component-only specification. See the [ADV7513 data sheet, Table 1](https://www.analog.com/media/en/technical-documentation/data-sheets/ADV7513.pdf).

Independent reset chains may release on different cycles. The remaining
fan-out/reconvergence must be consolidated or otherwise proved while retaining
asynchronous assertion with stopped clocks; this repair does not waive those
findings. See [Altera RDC-50003](https://resources.altera.com/quartushelp/25.1/da_rules/rdc_50003.htm).

Next: close those reset and external-pin contracts, rebuild/revalidate the
exact producer if it changes, prepare a fresh matched native chain/operator,
then requalify TRWU8CBL after its power interruption before a bounded boot/load/
ROM/input/return trial. Physical menu/core display transitions, media/input and
native networking still need completion toward the full DE10 behavior.

Raw evidence is in `out/nano-timing-audit-20261106/`; `result.json` binds its
receipts. Retention is at
`/home/deano/fes-retained/de25-timing-repair-20261106-r1`, with the preceding
native-runtime retention archive as the source prerequisite. Archive integrity
is separate from candidate qualification.
