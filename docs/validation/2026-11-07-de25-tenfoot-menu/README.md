# DE25 native Tenfoot menu ownership

The paired native frontend now presents the actual Tenfoot software renderer
through a privately owned MenuSession and crosses menu -> computer -> menu through
ProgramService. The boot verifier remains the original computer identity.

Implementation: `e89ef9781c32e4f43405effef5180de2f9806b6c`; native video-poll pacing
fix: `a19002ecc` (same millisecond wait as the existing native menu presenter).
Recovery upload extension: `5246bae7e`.
Base: `95ef748cf06d537448472c1dea630209cf44f6a1`.
Stage: `out/nano-tenfoot-menu-20261107`.

## Behavior

- Paired Ready owns a displayed menu and contained SD. Construction presents an
  initial frame; ProgramMenuPlatform then presents the real App framebuffer.
- Launch closes and independently drains the menu, resumes its original store,
  retains the selected computer/ROM, and carries that particular menu return
  through the existing owned configuration and computer Run/fetch proof.
- Return closes the computer, resumes SD, freshly retains/configures the menu
  and presents the cached launcher frame before publishing Ready.
- Rejected content restores the menu without consuming the logical generation.
  The request context remains cancellable without expiring the restored display.
- Loading/running frames perform no menu I/O or cache replacement. The 8 MiB
  two-slot arena remains reserved once. Uncertain transitions stop the service;
  known sessions close once, with no retry of failed grants or publication.
- ParkMenu closes the displayed menu independently and leaves SD contained.
  Terminal frontend errors retire known sessions before the native panic path.
- The paired SDK exports MENU.FCP, MENU.JSN and MENUSEL.JSN with byte hashes and
  sizes. Startup reads its single menu selection before taking SD ownership.

## Verification

All seven selected checks passed, including 1,648 parent tests with three skips,
generated-consumer consistency, and the native patched-C/Verilated-RTL integration
suite with Go race detection. The new menu phase includes repeated cycles,
content rejection, cancelled load requests, stale/busy calls, invalid frames,
pre-START arena corruption, configuration/identity/closure failures and frontend
retirement. The actual App/backend/service test compares every rendered pixel
with the modeled scanout bytes and exercises launch, return, restore, display
and serial failures. Only the platform responses are injected.

The final native executable cross-builds. The pre-pacing executable/entry and
interrupted firmware build are retained as superseded controls; none was used
on hardware. The final SDK, entry, secure BL31 and native U-Boot/SPL all built. An independent
proof rechecks their actual bytes, linked menu service/presenter/park code,
generated headers, full union reservation, boot anchor and unchanged limits.
The final FIT is 8280624 bytes with 107984 bytes remaining
under the existing 8 MiB cap; loaded OCRAM ends at `0x5de00`
within `0x60000`. Full identities are in [result.json](result.json). Their complete recorded
source sets match the implementation bytes. Actual retained ZX81, ZX80 and menu
packages pass the combined loader preflight with zero firmware calls or SD
containment; this optional case was explicitly enabled, rather than skipped.
The newly exported menu archive and metadata match those admitted real inputs.
`card-files/FES` contains the two-computer catalog and matched menu sidecars; no
card was written. The machine-readable result records matched-chain compilation
separately from software and physical evidence.

## Remaining integration and acceptance

This is software and compilation evidence. No FPGA conversion, board programming,
SD writes, HDMI capture or physical launch/return occurred in this milestone.
Whole-system and DE10-equivalent acceptance remain open.

Before a hardware cycle:

1. Exercise the actual strong cold USB composition with shared confirmed PIO USB
   while the menu DDR path is active, through menu/core/menu and failures. The
   current mixed-cold provider test compiles without controlled/shared USB, so
   its success does not establish this composition. `shared_valid` permits the
   confirmed menu reset state, but that structural observation is not a test.
2. Add source-bound menu presentation/return observations and teach the bounded
   trace/operator to admit the checked companion's union mapping and actual
   display marker. Current serial-only trace assumptions must reject this new
   mode until explicitly extended; never globally allow 8 MiB for ABI41.
3. Recovery uploads now accept the three fixed menu targets with existing
   limits/readback; all 36 uploader tests pass, including shared repeated
   uploads, size rejection and retirement on changed files. Perform actual menu
   file upload/readbacks on the separately booted recovery loader before trial.
4. Revalidate the changed source/artifact set, compose the matched HPS boot image
   under the shared Quartus lock, freshly claim/reidentify TRWU8CBL, then run one
   bounded visible menu -> selected core -> restored menu trial with controller
   input and exact identities. The bottom kit is outside this work.

Broader cores, media, audio, network service behavior and appliance qualification
still belong to the overall goal; these checks do not substitute for them.

Evidence is retained with readback/import verification under
`/home/deano/fes-retained/de25-tenfoot-menu-20261107-r1`. This retains the exact
final artifacts, prepared files, source delta and superseded pre-pacing controls.
