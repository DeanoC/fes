# PC/XT runtime floppy FreeDOS boot on Kit B

On 31 October 2026, Kit B booted the locked FreeDOS 1.4 XT floppy to `A:\>`
from the runtime-loaded SDRAM buffer. The [result](result.json) binds the
package, disk, bundle, session, captures, and restore. The FPGA package was
built from FES `0fdae134bc1d5938bcafe3fa0d8d5abcc98bd62a`; the reviewed
capture checker used `b7ddd3f4bd97105a66b501b6056cdc75d58659ba`. Only the
parent checker and its tests changed between those commits.

The Quartus 17.0.2 package passed its three seed audits and a byte-identical
independent rebuild. The worst reported internal setup/hold slacks across the
four analyzed corners were +1.192/+0.121 ns, +1.419/+0.122 ns and
+1.016/+0.124 ns for seeds 1, 2 and 3 respectively.

The full host simulation sent the image through the runtime mailbox and
SDRAM, booted the real BIOS and DOS, typed `DIR`, and checked the directory
listing and 720p glyphs after 2,882,804,525 fabric clocks. That simulation
uses injected firmware and keyboard stimulus; the hardware run below exercises
the packaged firmware loader and keyboard self-test responder.

After an owned launch, the media unit reported empty and the [empty capture](empty.png)
had no prompt. The session then accepted the complete 368,640-byte image and
reported the same generation and a ready disk. The recorded frames show
[POST](boot-001.png), [the FreeDOS kernel](boot-002.png), and
[FreeCOM and `A:\>`](boot-003.png). A fresh owned launch/Stop capture check
also identified the Kit B channel: [idle](mapping-idle.png),
[active](mapping-active.png), and [stopped](mapping-stopped.png). The session
was stopped and the kit restored
its recorded baseline bundle, with its image and boot unchanged, idle RBF
verified, no pending operation and no remaining lease.

Recheck the captured frames from the repository root:

```sh
python3 scripts/pcxt_dos_hil_acceptance.py \
  docs/validation/2026-10-31-pcxt-dos-boot/empty.png \
  docs/validation/2026-10-31-pcxt-dos-boot/boot-001.png \
  docs/validation/2026-10-31-pcxt-dos-boot/boot-002.png \
  docs/validation/2026-10-31-pcxt-dos-boot/boot-003.png
```

A first run also displayed the prompt, but the original checker rejected the
real capture's color variation. That run was stopped and retained as failure
rather than accepted. The revised checker was independently reviewed, including
near-identical wrong-glyph controls, before the repeat above.

This is an exact-package DOS boot diagnostic. It does not establish independent
readback of every target-side disk byte, floppy writes or persistence, normal
host keyboard input, external SDRAM/HDMI/HPS/reset timing, or whole-board
qualification. Media-ready is the runtime transfer acknowledgement.
