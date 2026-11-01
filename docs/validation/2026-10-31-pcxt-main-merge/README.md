# PC/XT FreeDOS boot after the main ABI merge

On 31 October 2026, Kit B booted the locked FreeDOS 360 KiB floppy to `A:\>`
from the runtime-loaded SDRAM buffer using the post-merge package. The
[result](result.json) binds the exact package, source, disk, runtime bundle,
session and capture hashes. This run supersedes the earlier bit-16 PC/XT boot
for the merged bit-20 interface; it does not change the earlier artifact's
identity or its historical record.

The package and matching software bundle were built from merge commit
`72febda79dc96d0bb87c72ea96d53f83366a523e`. At the time of this run, the
subsequent commits through `e02a5273c32d39e74aaae5e58007dc23f498aca8` changed
documentation and tests, not the deployed implementation. That statement does
not extend to later PR source trees, which add sealed Quartus QSF synchronizer
assignments and change the host upload path. This record proves the named
package only; current-source timing and exact-artifact Kit B acceptance remain
open. Quartus passed three seed audits and an independent
byte-identical rebuild for the named package. Across the four reported internal corners,
the worst setup/hold slacks were +1.012/+0.120 ns, +0.827/+0.134 ns and
+1.421/+0.120 ns for seeds 1, 2 and 3.

A private host captured the kit [idle](idle.png), launched the exact package
and recorded its [empty drive](empty.png), then uploaded the locked floppy.
The same session reported the medium ready and displayed [BIOS POST](boot-001.png),
the [FreeDOS kernel](boot-002.png), and [FreeCOM with `A:\>`](boot-003.png).
After owned Stop, the [idle display returned](stopped.png). These launch and
Stop transitions verify the capture channel for this owned session. Kit restore
and lease status are recorded separately in the result.

From the repository root, recheck the boot frames with:

```sh
python3 scripts/pcxt_dos_hil_acceptance.py \
  docs/validation/2026-10-31-pcxt-main-merge/empty.png \
  docs/validation/2026-10-31-pcxt-main-merge/boot-001.png \
  docs/validation/2026-10-31-pcxt-main-merge/boot-002.png \
  docs/validation/2026-10-31-pcxt-main-merge/boot-003.png
```

The first attempt was stopped and retained as a failure after the test driver
incorrectly required a full session field in the reduced upload response. The
repeat validated the response's target, session, package and generation and
reread the full active session before accepting it. This is an exact-package
boot diagnostic, not independent full-media readback, floppy write/persistence,
host keyboard, external timing or whole-board qualification.
