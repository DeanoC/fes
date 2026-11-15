# Native package admission against platform drivers

Implementation: `81aef659a58646b1ac6d2e17fb2c0a5f985916c9`, based on
`7711a051b7b2ca49f8628b973aa52f6ffed6e12c`. This is software evidence:
host race tests, firmware/FPGA models and an AArch64 bare-metal QEMU image.
There were no hardware operations, FPGA builds or new Nano profile registrations.
The [result](result.json) binds the tested source bytes to that implementation.

The retained-package checker previously described the general FES contract but
did not independently describe the particular platform's implemented drivers.
`RuntimeCompatibility` now requires both an immutable profile registry and a
separate, deeply copied driver inventory. It checks the exact profile, ABI minor
and every required interface version. Capabilities cannot be pooled across ABI
minors. Matching driver declarations cannot override semantic dependencies or a
diagnostic-only registry entry. Unknown optional interfaces remain optional.
There is no per-package decision list in this checker.

The current contained `ProgramLoader` uses the shared `DriverSupport` inventory
before SD containment. The constructor supplies embedded control and, only for
the explicit ROM personality, held-ROM support. A selected package cannot grant
video, ROM or another interface by declaring it. Integration cases verify that
missing required support produces no firmware call, storage transition or partial
plan. This diagnostic loader still does not admit the ordinary FES ABIs.

Validation:

- Focused Go race tests pass for admission, native loading and the diagnostic
  harness. The final retained corpus additionally passes all 19 admission cases
  and all three ROM cases on the host.
- All eight affected checks pass, including 1,568 parent tests (three existing
  skips), generated-consumer consistency, the actual patched TF-A C/Verilated
  endpoint integration, native loading and Tenfoot model paths, and 657 paired
  C++/Go semantic decisions.
- The AArch64 QEMU image passes 19 admission cases and three retained-ROM cases:
  format-three firmware/cartridge and format-four firmware plus cartridge.
  Its ELF SHA-256 is
  `a2b6e648c4de1d9510587891358ff2bf309ba8081380e262698aeff4bc460faf`.
  The guest compiles the admission checker; the separate host/RTL integration
  compiles and exercises the modified native loader. This is not a new physical
  Nano application build or hardware result.

The first QEMU run exposed stale fixture expectations for the 4/8 KiB Nano ROM
packages: their wrong target is rejected before the raw-ROM missing-link guard.
A Go overlay restoring the previous three admission-path files reproduced the
same failure against the exact failed guest fixtures. The fixture generator now
preserves the C++ rejection unless an otherwise compatible raw ROM needs the
model's missing-link result. Exactly two expected codes changed; package inputs,
policy and the guest ELF hash are unchanged. The failed run, previous-source
control and corrected run are retained.

The eight affected checks ran before that fixture-only correction. Independent
source comparison finds no changed Go input; the only changed input in their
source manifests is the fixture generator. The fresh final QEMU run and host
corpus cover that correction, including the three retained-ROM paths. Successful
unrelated checks were not repeated.

Working evidence is under `out/nano-runtime-drivers-20261106/`, with the complete
integration and compatibility outputs named by `result.json`. The durable archive
is `/home/deano/fes-retained/de25-runtime-drivers-20261106-r1`, using the preceding
retained publication `7711a051b7b2ca49f8628b973aa52f6ffed6e12c` as its source-bundle
prerequisite.

The next integration needs an ordinary Nano producer/profile and the standard
ABI drivers it actually implements, followed by enforcement of this combined
check before its native lifecycle. Its trusted boot and firmware scope must be
bound to the selected package, and the exact rebuilt artifacts need physical
validation. Native Ethernet, physical video/input acceptance and normal kit
behavior remain open. The preceding physical launch/return PASS belongs to its
older compiled source. After the reported power interruption, only USB
enumeration has been checked; running state must be revalidated before any next
hardware operation. The top kit's continuing claim and spare card are retained;
no operation was performed on the bottom kit.
