# Full Tenfoot installer navigation candidate

Source `49752d8b21cd1625ab0ee22892a20e628af36b87`, based on
`8e1be9a2fd2c2afeb4c2195e160ef187fd5ba7ea`, completes the ordinary App path
missing from the [preceding staged installer](../2026-11-10-de25-native-package-install/README.md).
Validation head `f2b0e7a3ae68f810dab93de58c9ab1fbfcbc618e` adds a test-only
asynchronous-response barrier. The native executable's selected production
inputs remain unchanged by that test repair.

The new full App test starts with an empty playable catalogue and uses ordinary
commands and displayed snapshots for Settings → Systems → Install → Open
library → launch → return. It runs through the real renderer, native backend,
owned installer, menu presentation and modeled firmware controls. The shared
Systems setup now offers Open library when its source already advertises
configured entries. This refreshes the library without another entry-creation
request. An uncertain install write retires the runtime and advertises no new
playable entry. Arbitrary native entry creation, updates and uninstall remain
outside this preconfigured staged-source implementation.

Explicit writable native images additionally emit bounded
`FES_NATIVE_INSTALLER_VIEW` records. These separately observe Settings/Systems
row kinds, focus, busy/status and cached native owner/generation. Changes are
emitted even when the ordinary App status is unchanged. Ordinary images keep
their previous output. The host decoder takes an explicit installation phase
and completed-install generation origin, preserving the actual counters:
first Install 0→1, launch 1→2 and return 2→3. Its ordinary defaults and rejection
checks remain in place. Navigation records grant no storage or firmware rights.

The [producer/consumer result](app-interoperability-result.json) consumes actual
committed Go App/model output through the Python decoder. It confirms one
successful install/launch/return and separately rejects an uncertain-write
retirement. The test acts on the last presented snapshot: its first draft could
select asynchronously updated controls before presentation. The original model
and consumer failures remain logged. This is framing and App evidence, without
physical boot or firmware trace acceptance.

The recovery uploader adds the single fixed `install-manifest` purpose,
`FES/INSTALL.JSN`, with an 8 KiB limit and existing RAM hash, SD write/readback,
fresh readback and protected-boot checks. Boundary, changed-file and arbitrary
path refusal cases pass. The [ten-file plan](upload-plan.json) includes the
matched experimental FIT and [nine staged files](staged-export.json). Referenced
content and empty bootstrap lists precede the install manifest. The plan is
file-only and has not executed; startup does not install automatically.

All [eight selected software gates](software-results.json) pass: 1,891 parent
cases plus their ancillary check, native C/RTL/runtime integration, full host
Go/UI suites, generated consistency, planner and whitespace. Earlier gates are
explicitly carried forward across the isolated cartridge-test change; the host
suites and committed whitespace rerun at the final head. The initial host race
suite failed a cartridge picker assertion. [Controlled base/current tests](cartridge-control-result.json)
show it depends on whether the swap completes before Select release. The test
now holds its asynchronous reply until the intended menu-owned release is
checked. Twenty repetitions and the full host suite pass. Runtime input behavior
and timeouts are unchanged; the original failure is preserved.

The [matched SDK/entry/OCRAM-MMU BL31/U-Boot/FIT/HPS-first SOF](publication.json)
has boot identity `c491ac67743d3526`. FIT size is 8,374,952 bytes, leaving
13,656 bytes under the unchanged 8 MiB limit. The computer/menu FPGA payloads
are unchanged. A copied checker output-directory name failed before correction;
the final checker derives the directory from the composition command. Both
checker results remain retained. Native Ethernet is still disabled.

The [retention proof](retention-proof.json) verifies 2,546 archived files,
2,273 selected source hashes, archive readback/modes and independent source
restoration/fsck/pack verification against the declared base-plus-delta bundle.
The capsule contains the final candidate, staged inputs, software/model logs
and comparison fixtures. External tools/vendor Git and all live board state
still require their existing admission. No UART, USB/JTAG operation, upload,
physical card access, kit-state change or old-output deletion occurred here.

Next is the bounded hardware controller that joins navigation to existing
menu/USB/firmware ownership proofs, followed by a fresh top-only admission and
Install → launch/return → fresh boot/discovery → launch/return trial. The older
two-title observer cannot stand in for that controller. Stop on the first failed
gate; do not reuse a capture or programming grant. TRWU8CBL's factory card stays
protected, the bottom kit stays excluded and cleanup stays paused. This changed
image has no physical installation, visible-display or keyboard acceptance.
Native networking and DE10-equivalent ordinary core loading remain incomplete.
