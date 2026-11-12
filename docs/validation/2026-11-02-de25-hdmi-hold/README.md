# DE25 HDMI I2C SDA hold repair

Tested FPGA/software source: `72716153eb7be7b62917ed685d5d7380347a2d7d`.
The parent is `a3a9dd9d5d19b893c555caa012b5505719896732`.
This is a source repair, digital protocol proof and fresh fitted candidate.
External HDMI timing, package admission and native acceptance remain open.

## Failure and repair

The ADV7513 [Rev. B data sheet, Table 1](https://www.analog.com/media/en/technical-documentation/data-sheets/adv7513.pdf)
requires at least 100 ns SDA hold after SCL falls. An independent timing
assertion on the actual retained controller finds **0 ns** over 662 ordinary
master data transitions. The original 4.5-second failed job, simulation log,
executable, private timing bench and input files are retained. That job remains
failed; it is not recast as a passing negative test.

The scanout derivation now delays only the master's open-drain SDA request
through eight 50 MHz reference stages. This gives **160 ns digital hold**.
The controller state, SCL/divider, ACK sampling, 33 initializer values and
interrupt restart behavior are unchanged. The DE10 lane and the standalone
DE25 display/vendor lane are unchanged. No SDC, clock target, pin assignment,
IO standard, kernel or runtime is changed.

The HDL bench measures elapsed time between SCL falling and ordinary master
SDA transitions, and requires the receiver's 100 ns minimum. It excludes the
slave's explicit ACK drive/release and START/STOP boundaries. It checks for
actual samples and retains the existing divider, LUT, retry, delayed-ACK and
interrupt tests. It does not assert the implementation's stage count as a
substitute for measuring the bus.

## Actual protocol and fitted checks

All 29 focused recipe cases pass in 1.300 s (1.4-second job). Real preparation
from the pinned Terasic archive followed by Verilator and controller-only
Yosys equivalence passes in 5.445 s (6.3-second job):

| Case | Writes | Ordinary SDA timing samples | Minimum digital hold |
| --- | ---: | ---: | ---: |
| ACK, then interrupt reconfiguration | 66 | 662 | 160 ns |
| First-byte NACK and retry | 34 | 339 | 160 ns |
| Second-byte NACK and retry | 34 | 339 | 160 ns |
| Third-byte NACK and retry | 34 | 339 | 160 ns |

The controller-only equivalence proves all 40 cells after `async2sync`; it does
not prove the new configuration/pipeline equivalent to its deliberately
different predecessor, asynchronous reset timing or physical IO behavior.
All ten static inputs used by the actual protocol proof match the fresh
canonical compilation. Its separate preparation `inputs.json` is retained
and independently checked, rather than claimed identical to the canonical
package's different identity/profile record.

Fresh Quartus Pro 26.3 generation, synthesis, fit, route and conversion pass in
624.7 s on the 3090 builder:

| Identity | Value |
| --- | --- |
| BUILD_ID | `5952e4a9936363114264247efd5584ee` |
| Linux RBF SHA-256 | `e18324235e32c86b643b24dba18deab344628cd9fe0a41bf9d6a3ea76836fde4` |
| Linux RBF size | 1,425,408 bytes |
| Canonical source inputs authenticated | 374 |
| Reported timing checks | 69, all pass |
| Signoff inventory | 85 rules; zero waived findings |
| Raw failed rules | TMC-20012: 30; TMC-20011: 2; RDC-50003: 2 |
| Unreviewed failed rule groups | 2 |
| Package timing admission | false |

The two previously approved asynchronous input findings are checked again
against this exact source, RBF and fitted database. The review receipt SHA-256
is `2245ea45fe0647566dda0e4c4659b2ea3b68003f86920c89f887a6dbedf4aaf7`.
No further exception is accepted.

A read-only query of a private fitted copy passes in 33.5 s. It finds all eight
SDA stages, from the controller's `SDO` through stages 0 to 7 to the SDA pad,
with no extra consumer. All 64 setup/hold paths pass: eight stages, two checks
and four actual operating conditions. The worst setup slack is 17.611 ns;
the worst hold slack is 0.099 ns. Every report is checked for the final
snapshot, exact endpoints, launch/latch clocks, actual corner and absence
of an SDC exception. Source/RBF/database bindings remain unchanged after
querying. [Fitted and protocol checks](fitted-hold.json) retain the values and
report hashes.

## Remaining external timing boundary

Both the old producer `1c5f24e18` and this new candidate have four-corner
datasheet/raw output reports retained, without adding external constraints.
The old candidate's conditional TCO analysis is retained as an investigation;
its numbers are not transferred to this differently placed RBF or treated as
a board timing contract.

The fitted forwarded HDMI clock uses PIN_DJ24, bank 2A_T and the vendor's
1.1-V assignment. Both selected vendor QSFs agree, while the rev-B manual's
HDMI table lists 3.3 V. The reported default output loads are 0 pF. The resource
ZIP has no PCB traces or detailed schematic establishing the clock path,
level conversion or board loading. No additional board files are available.
The voltage discrepancy, receiver thresholds, load, path delay and jitter
still need a defensible external contract. This record neither changes the
IO voltage nor infers an electrical incompatibility from an inapplicable
parameter row. No board delays or margin are invented.

The 160 ns result is digital master-data hold at the HDL bus. It is not a
physical pad, rise-time or complete I2C timing qualification. HDMI output and
whole-reset signoff still block MENU export; an eligible independent RBF
rebuild, admitted factory/card and native ABI3 MENU/catalog/ZX81/recovery
acceptance remain required. The artifact is unsealed and unprogrammed.

## Validation and retention

All seven commands in the complete current affected plan and `make check`
pass in 1,535.1 s. The routine parent suite passes 1,146 tests with five skips
in 206.060 s; the full FPGA software suite passes 1,762 tests with 76 skips in
1,313.542 s. Extended video/media/race modes are disabled by the current
selection. Generated consistency checks 40 consumers and 42 fixture copies.
[Software checks](software-checks.json) records the actual plan and results.
Unchanged combined-compiler and runtime-test inputs remain authenticated at
59 and 58 respectively; this increment does not rerun the historical full
native/AArch64 BASIC proof.

Independent local source/artifact restoration passes in 7.8 s (7.558 s inside
the job), verifying all 3,738 archived regular files, all 374 canonical source
inputs, all 11 actual protocol inputs and the fitted database bindings.
The 715,555,407-byte archive SHA-256 is
`63311b6ab2f870827adf3b4d075a86464ca98819e1654a9fc536afadbabe98d4`.
It includes the complete fitted candidate, positive and failed protocol
artifacts, raw output/stage queries, locked PDFs, original import failure,
scripts and terminal job records. Duplicate private fitted copies are omitted;
their original compiled inputs are retained. The failed probe's later input
inventory is explicitly retrospective and does not replace its failed receipt.
[Retention](retention.json) binds the source restoration and archive.

No board or installed state is accessed. Bottom TRWU8J8G's claim/baseline and
the top board are unchanged. New QSPI or boot-configuration deployment still
needs fresh approval after a concrete qualified artifact. This repair is
delivered only through the existing second consolidated PR #1312, in
`lane:deano`.
