# Merged-source kit software validation

Source `d1a38ecfd1f1f461d207e0b88ea152475d7ac2e3` built the merged userspace
bundle and qualified private host. Software activation, private package/input
cycles and the software-input-to-HDMI diagnostic passed. [Identities and hashes](2026-10-16-merged-kit-software/evidence.json)
keep host checks, software activation and later diagnostics distinct.

## Build and activation

Bundle `eaff152f182b69e1bcd867e4ec0c43931806e7dccf9b73cebe90250a4e560e65`
was built against retained qualified image `32f16cd2…`. The actual running image
was `ed721b84074b039daf66bad2aa24276b88dd8cd713b5cfcdb2c2f25489be4ad2`.
An initial preflight expecting the retained image was rejected without selecting
software. The retained build base and actual running image declare the identical
base contract, recorded in [the contract](2026-10-16-merged-kit-software/running-base-contract.json)
and [bundle receipt](2026-10-16-merged-kit-software/bundle-receipt.json).
The successful run explicitly pinned the actual running image; the target
validated installation against that current base.

The [API diagnostic](2026-10-16-merged-kit-software/activation-result.json)
passed held installation, competing-client rejection, queue/duplicate/conflict/
cancellation checks, retained idle Stop, requester-process exit and activation.
Selection changed from `cab26686…`, generation 10, to the merged bundle,
generation 11, on the same boot `14c05a47-1615-43f0-a084-a1dc8948c119` and base.
Activation recorded ten seconds of stable ready/idle/free state. Successful
selection remained installed. Reboot, recovery, local-busy, input and HDMI stages
were skipped by this particular runner invocation.

Six installed-core archive/member/selection files had identical before/after
hashes through software activation. The [byte comparison](2026-10-16-merged-kit-software/core-persistence-after-activation.json)
is separate from the runner's API inventory check; neither claims core execution.

## Host checks and package input

`make check` passed on the merged source. All 27 focused runner tests passed in
15.993 seconds. The private host SHA-256 and bundle archive/executable digests are
recorded separately in the evidence; no matching-source requirement is inferred
from this run.

The private [Pong package/input diagnostic](2026-10-16-merged-kit-software/package-input-receipt.json)
passed initial and host-restart cycles. Each acknowledged both requested API
events and advanced its input frame counter by three. Both containers exited
zero and were removed cleanly. These counters establish the recorded input path,
not physical controller delivery or hardware consumption. Runtime/agent/base
artifact provenance is retained separately from the active bundle's provenance;
no source equality was required for compatibility.

The first visual helper refused the private target alias before events, captures
or host startup; this is a failed preflight, not passing visual evidence. The
corrected helper checks that the alias resolves to the designated `.84` address.
No ambiguous mutation was retried.

## Software input and captured HDMI

The corrected private probe completed with exit zero, four acknowledged injected
events, clean cleanup and unchanged bundle/base/boot/generation guards. Its
[receipt](2026-10-16-merged-kit-software/visual.json) binds the exact helper and
standard source hashes to the captures.

The [baseline](2026-10-16-merged-kit-software/baseline.png) is black and supplies
no usable pre-input paddle position; its cause is not established. The passing
comparison is between [up](2026-10-16-merged-kit-software/up.png) and
[down](2026-10-16-merged-kit-software/down.png). Independent image review and
[pixel-row measurements](2026-10-16-merged-kit-software/image-analysis.json)
place the left paddle at y=0–143 after up and y=937–1079 after down; the right
paddle stays at y=469–611 in both. This establishes an effect of software-injected
directional input on the captured FPGA HDMI output, not a physical USB-button path.

The [final menu capture](2026-10-16-merged-kit-software/final-menu.png) shows
return to the launcher. It was taken under an owned read-only snapshot lease;
that grant was then released. The subsequent [final state](2026-10-16-merged-kit-software/final-ready.json)
is ready, idle and free, with merged bundle `eaff152f…` selected/active at
generation 11, unchanged base/boot and no pending/unreleased operation or fault.
The [anonymous health check](2026-10-16-merged-kit-software/public-health-check.json)
also confirms that software state is omitted from public health.

All six original core-file hashes still match after activation, both private
package cycles and the visual probe; [final persistence evidence](2026-10-16-merged-kit-software/core-persistence-final.json)
records the exact bytes. This does not claim Coleco execution. Reboot and
failed-candidate recovery were not repeated on this merged bundle.

An evidence-only `make test-changed --base d1a38ecf` run passed its four always
commands and policy tests without execution-source changes. The exact bundle
archive and original receipt remain under
`out/kit-software/kit-2026.10.16-merged-main`; the published evidence records their
identities rather than including the archive.

No raw health dump, software journal, private configuration, credential or operator
command is included in the published evidence.
