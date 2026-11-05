# Native ROM ownership-cost accounting

Base: `180cbde2823a8b9fc7be9026d87c624515de47e1`. The preceding physical
trial reached a verified written counter of 5,888/8,192 bytes before the
unchanged one-second ROM deadline. Ownership brackets consumed 94.4877% of
elapsed time, but the aggregate could not distinguish full observations from
the per-access guards inside a bounded step.

The strong firmware now classifies each bracket using the private step state
at entry, before common CPU/binding/configuration checks. Its existing next
timer sample updates the selected completed count, total and maximum as well
as the aggregate. No timer or device read was added. These are diagnostic
values; ownership, callback identity, live register checks, transfer commands,
PID, 16-byte framing and the one-second/64-poll limits are unchanged. Accounting
adds instructions and memory traffic, so physical overhead still needs measurement.

The existing `fast_checks` counts actual entry to the inner guard. The new
`fast_samples` includes completed brackets classified by step state at entry;
an earlier ownership failure may leave one pending measurement. These names
must not be interpreted as interchangeable. Valid completed full/step totals
sum to the aggregate. Clock regression, total or per-class overflow, sample
overflow and invalid classification mark the diagnostic invalid without
changing the returned timestamp or transfer decision. The failure report uses
cached values after the existing containment callback and adds no reads.

The 17 targeted tests passed, including the real C ownership/step code joined
to the trusted boot/USB/master model. The 4/8 KiB driver cases now exercise
separate known-duration brackets, idle samples, regression, both class
overflows, sample overflows and invalid classification. Register access counts
stay unchanged. Successful transfers check classification and sum consistency;
early failures retain the pending distinction. Both actual C/RTL transfer paths
passed all 23 phases. The unshared fixture still measures all ownership work
in the full class and verifies one-time reporting without additional I/O.

Exact source hashes and logs are in [the test receipt](result.json), with job
logs under `out/nano-rom-cost-split-20261105`. This is host/RTL evidence. Next,
build the matched image and run one bounded physical trace to determine which
ownership path needs optimization. USB failure diagnosis, complete ROM loading,
ordinary package admission and DE10-equivalent core readiness remain open.

## Matched image and hardware stop

Source `9fd40a21b334fb0f1b63dc3a1eabac2bd2065ea9` now has a complete matched
SDK/entry/BL31/U-Boot/composition build. Machine-code checks retain exactly one
counter read in `computer_rom_now` and all six new reporting fields in the
linked firmware. The native application ELF is byte-identical to the preceding
physical trial; only the firmware diagnostic changed. The 2,075 native inputs
were archived and read back, and both real ROM choices preflight successfully.

The broader selected checks completed after a bounded resume. The original
600-second wrapper expired in the final FPGA Python suite's PC/XT device test,
after ten earlier check commands had completed successfully. Its complete log
and timeout receipt remain preserved. The resume verified coverage of the
1,129 preceding component tests, then reran the interrupted module and the
remaining 452 tests, which passed with 59 optional-tool skips. It did not waive
or change any test, timing or seal check. Two initial resume-wrapper coverage
checks refused multiline unittest descriptions before running tests; the
corrected matcher retains exact test IDs, includes those descriptions, and
checks that no failure verdict occurred in the preserved prefix.

The single physical attempt stopped while programming the known recovery SOF.
Quartus reported a broken chain and rescan error, followed by a
configuration-succeeded line, but its overall exit was unsuccessful. The strict
programmer gate rejected it. The saved UART reached the known recovery U-Boot
prompt; that does not override the failed programming receipt. No upload, SD
write, native candidate programming, `fesboot`, ROM-cost measurement, CPU Run or
return cycle followed. The original attempt remains fused and was not retried.

Independent cleanup verified exact USB permissions, the recorded 15 MHz
readback, the private JTAG server's exit and a free UART. The host kernel log
contained no USB disconnect during the attempt window, and a subsequent full
descriptor inventory found no process holding the top USB adapter. These
observations do not prove an electrical or server cause.

A separate bounded idle-link check at 15 MHz obtained the expected device ID,
then a broken-chain response at the next sample. It made no programming, reset,
serial or SD request. A subsequent comparison temporarily selected a 6 MHz
programming link and obtained three consecutive expected-ID scans, then restored
and read back the original 15 MHz setting. Each operation had a separate
successful cleanup audit. CPU and FPGA operating clocks were unchanged. This
small comparison does not establish the cause or qualify programming/boot at
6 MHz. The original card and other kit remain untouched; the top claim and test
card are retained. Physical CPU or USB halt is not independently proved.

The [hardware receipt](hardware-result.json) references all three operations.
Their evidence is under `out/nano-rom-cost-split-board-20261105`,
`out/nano-rom-cost-split-chain-observe-20261105` and
`out/nano-rom-cost-split-chain-rate-20261105`.

A host-only alternate-rate lifecycle prototype is prepared under
`out/nano-rom-cost-split-20261105/alternate-rate`. Nine tests cover exact target
and restore commands, original/target mismatch, a setter throwing after applying
the rate, lost ownership, uncertain restoration, consumed operations and missing
callbacks. Every read/write requires the injected ownership guard; failed or
partial starts still require restoration. A failed restore remains uncertain and
cannot automatically retry. This prototype is not yet wired into a new native
operator. Next, integrate it with explicit prior-failure accounting and fresh
operator fixtures/preflight before a separate bounded 6 MHz programming test.
The cost-split firmware's physical measurements remain unavailable.
