# RBF ABI hardware validation — 2026-09-09

Status: **diagnostic validation only; milestone acceptance incomplete**.
The assembled development image passed structural verification, but standalone
FES Pong failed HDMI initialization. Two-pass image reproducibility, QEMU checks,
and acceptance against the final corrected image remain pending.

## Tested source and artifact identity

These revisions describe the image actually tested, rather than later fixes.

| Component | Revision |
| --- | --- |
| FES | `a6024bc39073340b99890a5eb7647ba83eab20ec` |
| FogCast | `9338329c476b88c6a1611ece40913e6672a9f617` |
| libmister-runtime | `475b060bfb8a7c5c5f7340895623ab382c9933fb` |
| misteross | `b167572866a6dc578b0eb5742773f254fbf8a1d0` |
| mister-packages | `a5c97eb94b5cad68568368b07a4321fe0b4c5623` |

- Image SHA-256: `a4899cfd81ae1c48e483e1fe9ee6b106057f472ab80583061e5966926cbc4117`.
- Standalone package ID: `67e1559ba7d080d5a25f9cdec25e66cfa3b5cfc480d73ddc42e4661e24ec1211`.
- RBF SHA-256: `3e868d92136d14699340d9f590ad7d9c7b5185ef70ae346a7e9c577454658f05`.
- Embedded build ID: `b7fe7ca32b44814344568451cc10db9a`.

The standalone build reported 79.95 MHz against its 74.25 MHz requirement.
Timing closure does not establish working HDMI output.

## Observed results

| Check | Result |
| --- | --- |
| Development image assembly | Passed, including four legacy cores and the selected standalone package; all 19 receipted files verified. |
| Deployed image identity | Exact image hash verified on the kit after reboot. |
| Protocol discovery | Native protocol 1 and protocol 2 status responses observed; custom ABI and programming profiles advertised. |
| MiSTer compatibility | Mega Drive 007 launched; host media capture showed the game and the launcher input stream was attached and active. |
| Corrupt package preflight | Rejected during admission; existing game, media, input session and readiness preserved. |
| Unsupported ABI preflight | Rejected during compatibility; existing game, media, input session and readiness preserved. |
| Standalone GP identity | Passed before custom video initialization, as established by the runtime call ordering and failure phase. |
| Standalone HDMI | Failed: `I2C device detection read failed`. |
| Failure reporting | Runtime reported recovery failure and `reboot_required`; it did not claim successful activation. |
| Operator recovery | Controlled reboot restored MENU with successful HDMI link verification. Existing FAT launcher/configuration restored; maintenance lease released and observed free. |

The host test used isolated application state and a private paired launcher.
The existing UI team's host process remained running. The previous image was
retained before replacement; this test did not exercise automatic image fallback.

## Findings and remaining acceptance

The tested standalone design omitted the FPGA route between the HPS I2C
controller and the HDMI transmitter. Working MiSTer cores provide this route
through the HPS peripheral I2C primitive and the board's U10/AA4 pins. Supporting
that primitive in the pinned OSS tools and adding the board wiring are required
before producing a new standalone package. The failed package above must remain
identified as failed hardware evidence.

A separate startup race caused the agent to report unavailable when the runtime
socket was not yet accepting requests. FogCast's reviewed local fix retries
read-only protocol 2 status within the existing startup deadline. Its unit tests
passed; it has not yet been tested in a rebuilt image.

Acceptance still requires the corrected package and exact final image: visible
standalone video, physical controller movement, Select+Start return to a
responsive launcher, relaunch/Stop, explicit raw diagnostic loading, intentional
live-identity mismatch recovery, and final lease cleanup. The stabilized image
must also pass the planned two-pass and QEMU checks before formal acceptance.
