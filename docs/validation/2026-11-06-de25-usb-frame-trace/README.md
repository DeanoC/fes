# Nano periodic USB command timing trace

Implementation: `3149fb25e7d6cb5920e124851cdf23fa16112aec`.
Base: `013b69864cc0a39e1bcca68cd53256472686f2fe`.

This matched diagnostic adds observations around direct periodic USB arm and
continuation commands. It preserves the frame parity calculation, every written
channel value, original HCTSIZ/PID/buffer/deadline, queue/spacing/ownership
exclusions and terminal failure/containment rules. The preceding
[independent timing model](../2026-11-06-de25-usb-frame-model/README.md)
distinguishes synthetic delayed writes from caller polling gaps; neither that
model nor the [last physical failure](../2026-11-06-de25-usb-toggle-trial/README.md)
establishes the Nano frame-overrun's cause.

Each selected direct periodic command retains the full original HFNUM value
used to choose parity and two extra non-destructive frame reads immediately
around the unchanged write. Three extra clock samples bracket selection and
the surrounding command window. The fixed eight-command history distinguishes
these sequential observations from the written value. The timing window includes
frame reads and bookkeeping; it is not an exact bus commit timestamp or proof
of the physical wire frame. These observations add bounded overhead and could
affect timing. Split/nonperiodic/retirement/containment commands retain the
original basic recording path.

Packet errors also retain the last and maximum gaps between step clock samples.
This reuses the original deadline clock read without adding a service clock
sample. The first step has no prior gap sample. The new trace never masks a
frame-overrun, supplies payload/completion, changes a deadline or releases an
unhalted source. It adds no production API, normal package admission or hardware
readiness claim.

Validation:

- Three new methods check exact ordered read/write scope and cached formatting,
  a delayed command crossing frame wrap followed by terminal overrun, and
  service-gap measurement using only the original deadline clock. Reports stay
  private and PID/deadline/failed-host ownership remain retained.
- Full DWC2, USB ownership and native runtime race suites pass, including the
  existing 27 ownership/completion exclusions and all seven independent timing
  model methods. Its 32-case synthetic sweep still yields 28 completed reports
  and four terminal overruns. All six affected-check steps pass: 1,568 parent
  tests, three skips, and generated-consumer consistency.
- Removing only the diagnostic statements recovers the preceding continuation,
  arm and deadline source. The basic write helper remains byte-exact. The
  actual rebuilt ARM64 image links both periodic callers to the timing wrapper,
  which has four indirect read/clock calls and one basic channel-write call.
  The original drain snapshot still precedes failure containment.
- Fresh native app/entry has four changed compiled sources among 2,099 combined
  inputs, with 2,095 unchanged. All forty entry-producer records, CPU mappings,
  FPGA/ROM/configuration contracts and compiled firmware exclusion remain exact.
  Twenty secure device objects and both vendor C startup objects are identical.
  The actual new BL31/loader, initialized per-CPU DDR, both 2 MiB no-map DTB
  reservations, original cache operation and matched file composition pass their
  audits. Five ARM model cases pass with their cache/UART modeling limits.
- The first source-inverse audit retained one duplicate newline while removing
  a formatter block. Its failed log and exact script are retained. The corrected
  audit passes; no production or compiled input changed for that correction.

Only the final native SDK/entry, BL31, U-Boot and file-composition outputs under
`out/nano-usb-frame-trace-20261106` are selected. FIT size: 8198704 bytes.
Matched chain identity: `f581c3aad9bfeccfd835f0dc12b6b1023f39f260eb3429ae6f3dd6c559dffdc2`. Composition held the shared Quartus lock
as normal user; it did not program the board.

There have been zero physical attempts on this compiled input. TRWU8CBL's
continuing claim and spare card are retained. Following Deano's reported power
interruption, its exact USB identity is visible again, but boot state and card
contents have not been reverified. Its next physical operation must perform a
fresh exact-device/claim/recovery/SD preflight. The previous failed candidate
remains consumed. The factory card and bottom kit were untouched.

Next: prepare a separately frozen operator for one bounded matched trace using
the original boot, upload/readback, USB/drain, launch/return and cleanup bounds.
Use its frame/write/service observations to choose a repair; do not replay the
preceding failed input. Ordinary core admission, physical HDMI, native network
and complete launch/return acceptance remain open.

Evidence, source snapshots and an incremental source bundle are retained in
`/home/deano/fes-retained/de25-usb-frame-trace-20261106-r1`, with the preceding
frame-model archive as prerequisite. Every archive member is read back and its
hash checked, and the imported source pack is verified.
