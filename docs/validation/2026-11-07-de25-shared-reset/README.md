# DE25 shared capture reset ownership

The native-computer producer now removes two redundant capture reset-release
chains. Actual fitted register inventories change from six to five chains on
the system clock and three to two on the pixel clock. Intra-clock false-path
reset-synchronizer findings fall from three to two. Four signoff rule classes
remain, with zero waivers; this is not complete timing or hardware acceptance.

Implementation: `d2aa3e41cedb586363a394fd1a3be913e9a219c6`.
Constraint/source follow-up and exact compiled source: `bb350b361cda882917d32bbcff9131208668558b`.
Package: `d2b280fb954639335eb66d77b2d4bb73518ad937fe21d32012911fea29acedf9`.
Functional build ID: `3450f3d47031c3b81bede281d1ae0f99`.

The computer's qualified 100 MHz Platform Designer reset directly resets the
writer/token owner, removing its extra two release cycles. The existing local
pixel release now resets both readiness and scanout/token ownership. Both
owners still assert reset with clocks stopped. Execution Hold does not reclaim
a published bank, and a later transport reset retires the epoch instead of
reopening an unloaded core.

The first build stopped during planning because the captured timing file still
required the removed reset registers. Its 1,055 source/report files and logs
are preserved and verified. The owned timing file removes only those obsolete
ingress cuts, rejects their return, and keeps shared-reset recovery/removal
checks. Byte comparison proves all clock and metadata bounds outside that
section unchanged. No broader cut, clock group or timing waiver was added.

The fresh Quartus Pro 26.3 build ran on the 3090 host with eight workers under
the shared lock. All ten normal compile phases passed their unchanged 120 s
limits. The sealed package, 195 source inputs,
four generated JTAG SDC repairs and generated handoff/timing files independently
verify. This package has no new matched native boot chain or board trial yet.

| Reported multicorner minimum, ns | Previous | Shared reset |
| --- | ---: | ---: |
| Setup | 1.969 | 1.881 |
| Hold | 0.000 | 0.000 |
| Recovery | 7.827 | 7.259 |
| Removal | 0.049 | 0.084 |
| Minimum pulse width | 0.220 | 0.220 |

All reported TNS values are zero and the clock inventory is unchanged. These
are the tool's rounded reported values; hold rounds to 0.000 ns and supplies no
reported margin. These are not external-pin or whole-board timing
qualification. The builds used different hosts/sources; this is not a paired
QoR benchmark. Missing input/output delays remain two/30 ports.

Validation on the frozen source:

- Writer-control induction passes with arbitrary qualified inputs; its broken
  capture condition produces a counterexample. The full video RTL comparison
  passes at identical qualified resets through Hold, clock loss, publication,
  scanout, RAM writes and pixels. This does not prove enclosing reset latency,
  physical metastability or timing.
- Actual ROM firmware/secure-provider and ZX80/ZX81 board models pass launch,
  held return and failure paths. Stopped clocks cannot defeat asynchronous
  containment or release the pixel reset. A broken connection bypassing the
  shared pixel release fails the actual token-owner monitor.
- 67 focused DE25 tests pass (one unavailable default Yosys test skipped; the
  pinned Yosys proof runs separately). Native context/ROM/composition tests pass.
- The final affected run passes 1,425 parent tests (two skips), 1,585 FPGA
  tests (75 skips), generated consistency and the actual Avalon/native model.
- The actual new package and separately retained ROM go through the real shared
  linker and native loader preflight with one choice and zero firmware calls.
  This checks admission before I/O, not an SD write, native boot or core load.

The remaining system reset bridges discard the qualified input's clock
association. A future correction must check the generated reset-request path
for OCM/interconnect. Vendor JTAG clock-sense resets and the outer HDMI
PLL-qualified reset also have distinct owners. Do not combine them merely to
reduce rule counts, or reset one bank owner while its peer retains a token.
The HDMI/I2C pin contract still needs its proper clock/receiver/board budget.

TRWU8CBL enumerates after the user's power interruption, but its current
CPU/FPGA state is unqualified. No UART opening, JTAG programming, SD write or
flash operation occurred here. The historical recovery inputs still verify.
Before a physical trial, close the remaining contracts, rebuild the exact
producer if changed, prepare its matched native chain, and requalify the kit.
Physical menu/core transitions, media, input and native networking remain
necessary for the requested full DE10-style behavior.

Raw evidence: `out/nano-shared-reset-20261106/`. `result.json` binds the exact
logs, package and model receipts. No PR, push, lane handoff or hardware
acceptance is implied by this record.

Retained evidence and source pack: `/home/deano/fes-retained/de25-shared-reset-20261107-r1`, dependent on the prior timing-repair retention.
