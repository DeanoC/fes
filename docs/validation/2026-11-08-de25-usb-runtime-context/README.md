# Restricted USB source verification

Source `fd74604df1382bf48dc184e7b27d53e82960305e` fixes the runtime rejection seen in the consumed USB probe.
The USB caller now uses a distinct inspection type with only identity, source
verification and pin observation. Its underlying control/ROM/program methods
stay private. Canonical source bytes, full native ABI checks, live read-only
owner/digest observations and the generic verifier restrictions remain.
The actual USB caller is explicitly typed to this restricted result.

Two focused Go race tests cover four source profiles, the public method set,
malformed profiles and all four lost-reply positions. Seven affected checks
pass, including 1,719 parent tests (three existing skips), native firmware/FPGA
integration and committed-source consistency (52 consumers/37 fixtures).

A fresh matched SDK/entry/BL31/U-Boot/FIT/SPL/SOF chain is built and checked:
boot identity `0d772a49dead9616`, 2,249 file/source digests,
embedded SPL verified and unchanged FPGA core bytes. The shared Quartus lock
covered composition. Source byte closures match this commit; build-time Go VCS
metadata remains separate and is not a clean-HEAD provenance claim.

No hardware acceptance is claimed here. The old `5cbd398052060507` image remains
consumed. Next is newly frozen admission and a one-attempt USB Ready/Stop test
on the rebuilt bytes. This does not complete ordinary core-load acceptance.
