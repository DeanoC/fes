# Both A55 RAS records and detection-control coverage

Implementation b42b86e14 replaces the current-record diagnostic with RAS2. It
keeps the original exception and extends the fixed inventory to43 reads (seven
mandatory CPU and36MMIO), adding the DSU/OCRAM detection and interrupt controls
as read-only observations. The primary RC28 register map is pinned and retained.

Optional RAS accesses require PFR0.RAS1, reviewed MIDR412fd050 and ERRIDR.NUM2.
Only an original selector0/1 permits writes. RAS2 selects constant0 then1,
reads each FR/CTLR/status/MISC0/status and confirms the selector after each
record, then restores the saved selector. Each selector write follows a fully
drained before marker, then ISB and readback before selected-register access.
Exactly three added system-register writes target ERRSELR. No status, detection,
interrupt or error-injection control is written. ADDR/MISC1 remain unobserved.

The decoder retains at most20 actions and records uncertainty from the first
selector-write marker. A matching restore readback proves restoration; END also
requires both records. Partial access or wrong readback cannot promise a restored
selector and remains terminal under park-only fallback. Unsupported feature,
CPU, record count and selector have explicit checked SKIP outcomes. Neither END
nor SKIP grants native boot, ownership, package admission or recovery.

85 focused tests,79 actual-ARM fixed-inventory/vector/failure cases and66 new
bank/selector cases pass. The latter execute actual MSR/ISB/MRS using TPIDR_EL3
as a QEMU surrogate selector; register values are injected. Tests check actual
selector state at the park endpoint for both successful restores, every read
failure, all three write failures, ignored/incorrect writes and changed selection.
Two unmodified QEMU CPUs skip unsupported RAS0/2 before optional accesses. No
emulation result proves the physical ERRSELR or selected banks work on Nano.
The initial emulation link overlap is retained: the larger test text crossed
its old fixture address; v3 moved only test memory and all79 cases then passed.

All affected integration steps pass, including1532 parent tests/two skips for
macOS-only layout and missing cached upstream U-Boot. The shared ROM linker test
ran with Go1.26.5. Actual BL31/SPL/U-Boot builds and linked binary/source audits
pass. The audit resolves all43 canonical reads, both fixed selectors, saved0/1
restore, barriers/readbacks and exact skip-message branch targets. The1428-byte
early guard, NOBITS allocation, vendor cache operations and all runtime limits
are unchanged. The matched file-only composition retains the1658880-byte sealed
FPGA payload; native31d SDK/entry bytes and2098 selected source hashes match.

This validation performs no board operation or SD write and grants no ordinary
admission, native startup, ROM completion or DE10-equivalent core-load acceptance.
Next freeze a fresh one-attempt operator, independently admit the exact claimed
TRWU8CBL, and collect the63-read/three-selector-write diagnostic without retry.
Do not replay stopped1e8d, enable error detection before initialization, clear
status, bypass cache maintenance, scrub live OCRAM or widen time/ROM bounds.

Primary references: [Cortex-A55 TRM](https://documentation-service.arm.com/static/5e7e09f6a3736a0d2e862d2f)
B2.40-B2.50/B3.3 and [Arm RAS specification](https://documentation-service.arm.com/static/5f8db40c4966cd7c95fd58e3)
4.4.3-4.4.9 define record selection and distinguish it from error status/control.
[Altera CS/R1 register map](https://docs.altera.com/v/u/resources/864455/agilex-5-cs-and-r1-suffix-opn-hps-register-map)
provides the four read-only detection/interrupt observations. Their live values
and the shared-bank diagnostic remain unverified until the bounded board test.
