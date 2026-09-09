# Core package library validation — 2026-09-09

Status: final integrated image acceptance passed.

The milestone adds host installation and explicit library selection for ROM-less
FPGA packages. It reuses the existing package activation/input/Stop lifecycle.
The UI renderer, runtime, FPGA sources and compiler pins are unchanged.

## Selected sources

| Component | Revision |
| --- | --- |
| FES build base | `86280c4926b5a29df1f1572071176e141654a960` |
| FogCast | `19dbbec50beebe43c814d849989e243db53171bb` |
| libmister-runtime | `04b20509a5501c1fdf6400e21a8dd6567d6c5e33` |
| misteross | `11c3ee1fbb4d0324a5fd8b3168a7be89a9ecea26` |
| mister-packages | `a5c97eb94b5cad68568368b07a4321fe0b4c5623` |

The FES base had the reviewed FogCast gitlink selected during assembly. The
subsequent parent documentation/pin commit does not change image recipes.

## Software checks

- Full FogCast Go suite and `go vet ./...` passed.
- Affected store/catalog, transport, service, host API and CLI race suites passed.
- Independent component and integration reviews completed. Regressions cover
  recovery status/timeout retention, contradictory identity cleanup, malformed
  request bodies, full CLI descriptor binding and inaccessible installed storage.
- `make check` passed: shared definitions, generated consumers, fixtures and pins.
- `make host` passed.
- Parent `make test`: 192 tests run, 36 skipped, no failures.

## Diagnostic hardware

The designated kit was leased for agent/launcher maintenance. An isolated host
used a separate catalog, archive store and launcher pairing. The UI team's
ordinary host process and catalog remained in place. Temporary bind mounts
selected the diagnostic agent and pairing; both were removed afterward and the
ordinary services and free lease were verified.

The diagnostic agent SHA-256 was
`8ab634fce14624d1b5db05fda2087ec169d534abc3abcd104a0acd74eba7061a`.
The underlying prior image SHA-256 was
`aa500c82fcc449f62e073d7db89088bf04172dc0df051d3b9e36846f3801d986`.
These results are diagnostic, separate from final-image acceptance.

| Check | Observed result |
| --- | --- |
| Import and duplicate import | Immutable archive installed; duplicate returned the same ID without replacement. |
| Ordinary library launch | Stable game ID reported with `system: fpga`, exact package identity and generation. |
| Launcher input and HDMI | Launcher source attached, frames delivered, standalone Pong visible in capture. |
| Version selection during play | Next-launch selection changed; running package and input session remained unchanged. |
| Stale selection | HTTP 409; existing selection preserved. |
| Unsupported ABI | Inspection incompatible; selection rejected; selected package and active input preserved. |
| Version launch and rollback | Both selected package IDs launched; selecting the original restored it on next launch. |
| Stop | Each tested launch returned to idle. |
| Host restart | All three installed archives and the final original selection persisted. |

Original package:
`356d38e50aa0db49f01abccae28d745d305a0f9e98ba634998d68172c9d5d023`.
The metadata-only test version (`1.0.1`) was exported through the authenticated
producer as
`f12f6ede054657b7f1b31942d0ad4c26e6d94ef193091f9ee98dc860e70feaf8`.
Both used RBF SHA-256
`18c3aae94a3d470474955591daf4ba9b5e4ba22b7c92b314c37a043543766135`
and build ID `60ba707329b4e7c8c86d4389e6fa510a`.
The version fixture is diagnostic evidence, not a newly released FPGA build.

Physical play/controller confirmation belongs to the prior RBF ABI milestone.
This test observes input attachment/delivery and video; it does not claim a new
physical button confirmation or automatic boot-fallback test.

Local detailed evidence and bounded runners are retained under
`.superpowers/sdd/2026-09-09-core-package-library/` in the integration worktree.
Private pairing credentials are stored separately and are not published.

## Exact integrated image

Both clean image passes produced SHA-256
`c82c87b6f5e31e00e4f56b9245815b9db77cf9d2c8cc06e829dff88fca8051f5`.
`make verify` passed structural checks, two-pass reproducibility and the QEMU
packaging boot check. The QEMU log SHA-256 was
`c7d599c8e6dead7235ec4320e0ec5d9630e5f0eac92706477994d0f199c6fd03`.

After a leased installation and controlled reboot, the checker verified a
changed boot ID, the root loop's canonical image backing path, the image hash,
and exact agent, runtime, launcher and installed build-input hashes against
the receipted image. The image launcher was running without diagnostic binds.
The previous image was retained as `/media/fat/linux/fsold3.img`.

The exact parent-built host then reused the retained isolated catalog/store.
The normal FPGA platform browse exposed the launchable entry. Original and
metadata-version packages launched with their selected identities; checked
selection, stale-CAS rejection, incompatible-selection/input preservation,
rollback and Stop passed again. Launcher input attached and delivered frames,
and capture showed standalone Pong on this exact image.

A further unsupported package load through the development admission route was
rejected in the compatibility phase while a library game was active. Its game
association, package generation and launcher input session remained unchanged;
Stop then returned idle. This exercises the shared admission path independently
of the selection rejection.

The ordinary launcher configuration was restored, the isolated host stopped,
and the kit lease was observed free. The ordinary host reported the configured
kit reachable and ready after reboot; capture showed its normal menu. The exact
new image remains installed, with the previous image preserved.
