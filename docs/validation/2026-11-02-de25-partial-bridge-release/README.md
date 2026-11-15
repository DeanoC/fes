# Explicit Nano data paths and partial bridge closure

This Linux-free prototype now selects the required data bridges through the
existing native platform hooks and controlled C firmware service. DDR-only
selection leaves the unused paths in reset. This is software/model evidence;
no hardware access, new FPGA artifact, data-bridge release on a board, physical
DDR drain or runtime admission occurred in this step.

The source base is `59ddb433e1d9805586d2c907edf07c549c62a640`.
The production change is `425cfcaf14b8657e1ebbfb43b56cc19b85feeb42`.
Final tested source is `7a30ebdc72079212c330cd0befc76a261266aaf3`.
The four-file final delta contains only host test fixtures/scenarios, recorded
in [source-delta.json](source-delta.json). Production sources, shared contracts
and generated consumers are unchanged across that delta.

## Behavior

`NewCPUHooks` requires a nonzero platform data mask in addition to the explicit
CPU aperture and transport. The mask must fit the observed data capabilities;
package metadata cannot select or qualify it. Control still opens exactly bit
`2`. Data release can select any nonempty subset of mask `13`, subject to the
trusted platform callback. Unknown initial partial reset states still reject.

| Transition for DDR-only mask `8` | Reset mask | Evidence required |
| --- | --- | --- |
| Trusted never-enabled boot | `15` | Closed ownership and all masters contained |
| Control-only open | `13` | Same owner, next generation and exact readback |
| Data release | `5` | Explicit mask `8`, same owner and exact readback |
| Active close | `15` | Stop every producer, close active mask `10`, hardware result `8` |

A close requires fresh hardware evidence only for `12 & ~before.reset_mask`.
It rejects an invented acknowledgement for an unopened path as well as a
missing acknowledgement for an active one. All-master stop/quiescence remains
mandatory, including every fabric producer. Wrong ownership, generations,
readback or uncertain callbacks retire authority without replay. Control-only
recovery retains its separate held-data receipt with zero fresh data ACKs.

The shared [bridge channel](../../../sources/mister-packages/docs/embedded-bridges.md)
is now 1.8. Release encodes `3 | (mask << 16)`; DDR-only uses `0x00080003` and
all-data uses `0x000d0003`. Bare operation `3`, control bits in the data mask and
reserved request bits reject. The YAML, oracle and generated C/Go consumers
were updated together using `make generate`.

The physical cold provider still denies generic bridge mutations, including
both encoded releases. Its fixed reader diagnostic retains control-only
access. These changes do not install a physical programming profile or qualify
an arbitrary package.

## Validation

All checks below passed. [summary.json](summary.json) records the evidence class.

| Check | Result and source scope |
| --- | --- |
| Full affected software plan | 12 commands on `425cfcaf1`; [plan](software-plan.json), [results](software-result.json) |
| Parent regressions | 1,265 tests, 2 skipped, within that plan |
| FPGA producer regressions | 1,576 tests, 75 skipped, within that plan; no synthesis or hardware |
| Direct C service and wire | 62 service cases and standalone codec/oracle checks; [service](service.log), [wire](wire.log) |
| Focused native Go race checks | Native fabric and model backend; [log](go-hooks.log) |
| Joined controlled C/Go/RTL integration | 28 lifecycle cases plus SiP gates and 10 Tenfoot actor cases on final source; [report](native-integration.json), [native](native-tests.log), [Tenfoot](tenfoot-tests.log) |
| Actual pinned vendor driver | 42 injected register cases inside the joined run; [log](vendor-driver.log) |
| Strong cold provider denial | Five Python tests across cold profiles, including encoded releases; [log](cold-denial.log) |
| Combined SD/configuration/reader provider | Three Python tests, including actual C and Avalon RTL; [log](strong-provider.log) |
| Final `make check` | 34 generated consumers and 37 fixture copies match; [log](check-final.log) |

Native and actor cases cover masks `8` and `13`, launch/return, identity
recovery, missing hardware/fabric outcomes, incomplete producer quiescence,
ownership changes, failed opens, bad readback and lost transfer completion.
The missing-FPGA-master case rejects before any bridge/reset write. The driver
fixture rejects polls for unopened data paths during partial close.

The joined adapter needed its SD diagnostic reporter, and its opening-error
injections now target the requested transition even when the pinned driver
skips polling on a reset bridge. The stale invalid-operation test now uses an
undefined operation rather than the already-defined context query. The
non-owned native RPC path preprocesses to identical bytes before and after the
adapter change: [comparison](non-owned-rpc-comparison.json). Final-source
checks cover the changed host paths; the full software campaign is reported
only against its actual tested head. Earlier failures and their logs remain in
the retained local session.

## Remaining physical gate

The pinned FPGA-to-DDR disable branch waits for `F2SDRAM0ACK` to clear;
FPGA-to-CPU disable waits for its flush acknowledgement to assert. Injecting
those expected values checks vendor control flow, not independent physical
DDR drain. The cold data-release restriction remains in place.

The [previously generated DDR project](../2026-11-02-de25-ddr-reader-wiring/README.md)
has three incoming managers at the DDR adapter: the embedded reader, the
existing DDR probe, and the maintenance/debug path. The read-only
[inventory](ddr-master-inventory.json) binds those connections to the generated
file hash; it supplies no stop/drain qualification.

The next integration needs an owned CPU/FPGA DDR reservation and qualified
stop/drain callbacks for all three managers plus the relevant HPS producers.
Then rebuild the matched sealed boot chain and run a bounded board test.
Repeated native core loads, ordinary runtime admission, physical launch/return
and peripheral/UI acceptance remain open.
