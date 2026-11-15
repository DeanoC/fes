# Strong cold mixed-role callbacks, 2026-11-07

Implementation `a0b014017ba5afa4d2dedf1f1c39f877e59e2832` connects the real cold provider to the trusted
joined-dispatcher activation callback in an explicit firmware selection. It
keeps the original computer boot context and requires the complete8MiB union
reservation. Reader size, firewall coverage, held acknowledgement register and
SD-return predicate follow active role. Capture retains the preceding policy.
Completion validates/consumes its return proof, constructs selected stop policy,
prepares computer ROM or retires completed ROM for menu, publishes the selected
identity/dispatcher and clears stale window/rearm state. Failed ROM or drain
state cannot be reset into a new grant.

The fixture executes actual cold boot acceptance, SiP/configuration, SD platform,
ROM driver, pinned SHA-384 and independent master-stop callbacks. Four five-load
sequences cover alternating roles in both starting orders and repeated loads
of each role. Selected build IDs change; each computer's4096 ROM bytes are
checked at the injected FIFO. Nine failure cases cover invalid role resources
and ABI, CPU drift, bad ROM digest, held acknowledgement mismatch, configuration
corruption, invalid SD route/closure and absent drain acknowledgement. Every
failure refuses further launch/SD I/O. A smaller arena fails compilation.
MMIO and configuration-mailbox replies are injected; these are host tests of
the real callbacks, not physical FPGA or shared-USB qualification.

All 11 selected checks passed. Parent tests: 1642 with
3 skips. Separate overlapping misteross suite: 1595 with
79 skips; the two counts are not additive. Selected checks also
include generated consumers, native C/RTL/Go/Tenfoot integration, producer
identity, DE25 control and firmware identity/drain simulations. Exact source
hashes, results and limits are in [result.json](result.json); full new-case
traces and compile contracts are retained.

No board build enables the combined firmware mode yet. Both sealed role inputs,
all six shared HPS/DDR fingerprints and the complete union8MiB CPU/firmware/SPL
exclusion must be bound by the matched native builder and entry chain. Actual
ProgramService/Tenfoot menu display ownership and combined physical acceptance
remain open. No board or card was touched; full appliance acceptance, broader
core support, shared USB/media/network integration and timing are not established.
