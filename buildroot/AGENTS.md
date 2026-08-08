# Target image and Buildroot safeguards

Read `../AGENTS.md` first. This tree can affect target artifacts; local builds
and tests are allowed. The standing grant in
`docs/adr/0002-disposable-local-development-target.md` allows full project
access, deploy, reboot, software/image/configuration and credential replacement,
wipe, and rebuild of the designated local disposable MiSTer Pi development kit
without repeated confirmation, the rollback lock/runbook, or physical-state
preservation. Before destructive action, resolve it through operator-controlled
private configuration and verify exact target identity; never infer designation
from device type, hostname, IP address, or discovery, and never record private
identity or secrets. Other and production targets require explicit operator
authorization and the applicable rollback/hardening controls. Preserve all
lifecycle/reconciliation, evidence, and artifact-provenance gates.

Keep locks and manifests reproducible and auditable: pin source/toolchain and
cross-repository inputs immutably; record provenance, license/configured
features, and safe artifact hashes. Luna may routinely regenerate locks,
hashes, and generated artifacts within an approved gate when provenance, tests,
and review are recorded. Escalate to Sol and obtain an approved decision only
for changes to gate semantics, dependency/source authority, image architecture,
security/rollback/hardware invariants, or for an unexplained reproducibility
failure. For a Reproducible claim or a gated artifact, perform
clean independent builds in separate output trees with pinned environment,
locale, timezone, and `SOURCE_DATE_EPOCH`; unexplained byte differences fail
the reproducibility gate. Compare generated maps, dependency closure, compiler
settings, and image manifests with reviewed locked baselines when that gate
requires it.

For other development and production targets, preserve the applicable verified
rollback path and separate development from production.
Production images contain no SSH server or interactive development service;
development access is an explicitly selected profile and never a runtime
dependency. Do not place secrets or interactive credentials in configs, locks,
manifests, build logs, images, or source control. Automated target updates stay
disabled until the approved signing, trust-root, revocation, downgrade,
atomic-activation, power-loss, and rollback design exists.

The pinned POC1B production-named profile contains historical shared credential
material. Handle or copy it only for exact pinned historical reproduction or an
explicitly authorized rollback. Never copy its credential or profile into a new
profile, reuse it elsewhere, promote it, or treat it as a destination security
baseline. Future production profiles remove interactive credentials and define
their recovery channel explicitly.

Build/test success does not establish physical behavior. FPGA, HDMI, audio,
input, saves, lifecycle recovery, and rollback assertions require separately
recorded HIL evidence with artifact provenance. Routine lock, hash, and
generated-artifact updates remain Luna work. Escalate only changes to gate
semantics, dependency/source authority, image architecture, or a
security/rollback/hardware invariant, and unexplained reproducibility failures,
to Sol. Require independent Vega review.
