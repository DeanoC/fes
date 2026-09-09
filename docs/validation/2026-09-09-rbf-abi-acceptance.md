# RBF ABI hardware validation — 2026-09-09

Status: **gameplay and recovery diagnostics passed; updated integrated image pending**.
The assembled development image passed structural verification, but standalone
FES Pong initially failed HDMI initialization, followed by faulty ball motion.
Hardware diagnostics now verify a nextpnr correction with the original Pong
RTL: smooth motion, both paddle bounces, and return to centre after a point.
The final pinned package passed its separate gameplay diagnostic. The final
image now passes two-pass reproducibility, structural and QEMU checks and boots
its own matching launcher. The user confirmed both catalog Pong and standalone
FES Pong play and return to the menu. Negative recovery acceptance remains
pending; see the checkpoints below.

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
that primitive and board wiring subsequently produced the second diagnostic
package below. The first package remains failed hardware evidence.

## HDMI I2C isolation

The second package used misteross `c3f348417ea9845933c90b7114f0f89429c8f330`,
Yosys `fca8ca0a5354e52ce0e158bc6e1eed481e590ed8` and nextpnr
`69556b7e58eae3dda9b38df438267481c951863d`. Its package ID was
`e53f52103d972b6f65865bc22ad516022fae8ac22987b118da91916129d5eccf`,
payload SHA-256 `30db8543e388145e2cc48e94c9ecd6deff84378660315526c5755f7a3c7c713c`,
and embedded build ID `4c468eec77a9d4adafe06e985798b2e9`.
It passed routing checks and reported 77.51 MHz timing, but physical HDMI
initialization still failed. This test retained the development image above,
using the rebuilt host and temporary launcher from FogCast
`50afe95f4efb3b177d1cc0d9cef806a303cdad9e`.

Two temporary RBFs isolated the hardware configuration through the explicit raw
diagnostic path. Neither was exported as an accepted package:

| Diagnostic payload SHA-256 | Isolated change | Physical result |
| --- | --- | --- |
| `610a4f72efc7396d21aa5f4c17a9a20678fe3524e83dc4af97ffbc73842a885f` | Invert HPS SCL feedback; one CRAM bit. | I2C read timed out. The working Quartus core uses a different feedback route, so its inversion setting does not transfer to this design. |
| `abc5f896ea8de6dde93bd23590e6691e9f6add317ff7a000b541c0d8874aeed4` | Restore the two bidirectional GPIO input settings from `IOCSR_STD=DIS` to their database defaults; four PRAM bits, routes and inversions unchanged. | During the raw probe, HDMI registers read successfully: power `0x50` while quiesced and status `0xf8`. Automatic recovery after the expected MiSTer probe failure restored MENU without reboot; power returned to `0x10`. Explicit Stop then returned idle. |

Target payload hashes were verified. Runtime log snapshots immediately before
and after the successful reads showed programming completed and no recovery yet.
The second result isolates nextpnr's
output-only input-buffer setting being applied to bidirectional pins. A reviewed
source fix, rebuilt package and complete video/input test are still required.
The unchanged FAT launcher and configuration were restored, the original UI host
remained running, and the diagnostic lease was observed free.

## Remaining acceptance

The rebuilt package `ce656471f730590e30489864ab3c592454ed583db75b8523402c7df71dd311d7`
uses misteross `6568162086167a35b923b14778706312950671c2` and the reviewed nextpnr
input-buffer fix `cb0dab2da29f50e869327534c90215e569d7430f`. Its payload SHA-256 is
`f7a62b60a7bc31ad9379a874baf95e217c8e9ea50a0d1828b210dbadb8e8c7c1`, embedded
build ID `aaa8596c943c16bb0f752e92021db397`, and reported timing 83.94 MHz.
The runtime confirmed the live identity and HDMI link; capture showed the Pong
playfield. The user confirmed paddle movement and Select+Start return to a
usable launcher, but reported flickering and incorrect ball motion after Start.
A recorded repeat showed the ball at the top edge and unexpected motion.
This package therefore remains a failed gameplay diagnostic. The first formal
image assembly was stopped before completion during the investigation.

### Constant register-input defect

The fault is in nextpnr's `MISTRAL_FF.DATAIN` handling. `PINSTYLE_INP` was
`0x001`, advertising a hard constant-low input that the physical register does
not provide. Packing discarded three constant-zero coordinate-register inputs;
the registers then sampled local logic when enabled. Telemetry exposed the
first X update as 667 instead of 155, and the subsequent reset as 670 instead
of 158: bit 9 was incorrectly set in both cases.

Two independent hardware controls verified the cause without changing Pong RTL:

| Control | Payload SHA-256 | Timing | Result |
| --- | --- | --- | --- |
| Original synthesized netlist with the three zero inputs explicitly routed from a zero LUT | `157c46e3ebef7ad1dce74d4f33c445ad224afc9afbd5bad8a186d4569c0ff9f0` | 84.51 MHz | Correct initial motion, both paddle bounces, centre restored after a point. |
| Unmodified synthesized netlist with nextpnr `PINSTYLE_INP=0x010` | `d254047ab00c23cb7700147237278318d50c8c4c92854ca117451141235c1229` | 79.09 MHz | Same successful gameplay checks; Stop returned idle and the lease was released. |

The second control used provisional compiler binary SHA-256
`9558a77387112d5f156f1b7708e2ba80ff408229a1f62fc37c9052bb9f880f0f`,
built from `cb0dab2d` with the one-line pin-style correction. Its package ID was
`3ecc996f96bee2d3e38f1ed29144b61b8f2762894311c25f280064f1431e1433`.
These are diagnostic artifacts, not a sealed release from a final compiler pin.
The paired launcher was restored and target readiness and a free lease verified.

The reviewed fix is committed as nextpnr
`5e31bf41f47c0b2403f77c6fb679ca306e9b2cd0`, selected by misteross
`11c3ee1fbb4d0324a5fd8b3168a7be89a9ecea26`. Its regression fails with the
previous compiler and passes with the correction, checking real constant
sources, routes to the registers, and decoded register-input selection.
The misteross suite passes 552 tests with one skip; FES consistency checks pass.

The sealed rebuild produces package
`356d38e50aa0db49f01abccae28d745d305a0f9e98ba634998d68172c9d5d023`,
archive SHA-256 `099eb49fabfe015ee17e9d723be9146ddc7c223b4cc49f257bd020384283ba63`,
payload SHA-256 `18c3aae94a3d470474955591daf4ba9b5e4ba22b7c92b314c37a043543766135`,
and build ID `60ba707329b4e7c8c86d4389e6fa510a`. It reports 78.25 MHz and binds
the reviewed source and compiler revisions. Its exact-package hardware run
passed on boot `a1383a40-cf3b-4c0b-ae9a-2cc7dc85bf63`: package loading and
input attachment succeeded, recorded motion followed the expected initial
vector and speed, both paddles reversed the ball, and a point restored the
ball to the centre. The capture contains repeated/skipped frames; displacement
and elapsed-time checks account for those sampling gaps. Stop returned idle,
and subsequent checks confirmed agent readiness and a free lease. The user
confirmed the preceding reboots came from another test.

The user subsequently reported that the visible launcher had not returned.
Runtime idle and agent readiness did not establish launcher readiness: the
reboot had removed the temporary launcher bind mount, and the immutable image's
older binary crashed on the current `classic` theme configuration. Under a
fresh maintenance lease, the existing FAT launcher was bound back onto
`/usr/sbin/fogcast-kit` and restarted. HDMI capture then confirmed the visible
platform menu and the launcher process remained running. This temporary
binding does not survive reboot; final image integration must pair the launcher
with its configuration and verify visible menu restoration explicitly.

A separate startup race caused the agent to report unavailable when the runtime
socket was not yet accepting requests. FogCast's reviewed local fix retries
read-only protocol 2 status within the existing startup deadline. Its unit tests
passed; it has not yet been tested in a rebuilt image.

Acceptance still requires the corrected package and exact final image: visible
standalone video, physical controller movement, Select+Start return to a
responsive launcher, relaunch/Stop, explicit raw diagnostic loading, intentional
live-identity mismatch recovery, and final lease cleanup. The stabilized image
must also pass the planned two-pass and QEMU checks before formal acceptance.

## Final-image checkpoint

FES `a8492dced1003baf6b5253338932add47f60ef68` selects FogCast
`8e4870caaaaadd4c46b1ac53345124c6a197eb04`, libmister-runtime
`475b060bfb8a7c5c5f7340895623ab382c9933fb`, misteross
`11c3ee1fbb4d0324a5fd8b3168a7be89a9ecea26`, and mister-packages
`a5c97eb94b5cad68568368b07a4321fe0b4c5623`. FogCast merges the reviewed
current launcher UI with the described-core branch, preserving input generation
fencing and adding compatibility with the existing Classic configuration.

Both independent image passes produced SHA-256
`866d43a6915b2d6c7fbbddb109ab307d5be9b3d72ed616a9826dac42872cfcbf`.
Structural and QEMU packaging checks pass; QEMU log SHA-256 is
`12f61d5e1b4f098a54a5818de8c2fde8481584093abd287ccd436ab814cf4b2c`.
The parent suite passed 190 tests with 36 skips, the native runtime and shared
package suites passed, and the merged FogCast full Go suite passed. One initial
FogCast recovery test was transiently flaky; its focused 20-repeat run, package
rerun and full rerun passed.

The image is installed on boot `f6e7be48-24e5-4475-9436-2d8a3805253a`.
The installed package is `356d38e5` (full identity above), and the running
image-owned launcher SHA-256 is
`af4156babd75beac8e33d540e9b56b9b15c186970210c913ac35c7a796e6a5ac`.
The installed source records match, the agent is ready, and no launcher bind
mount is present. HDMI capture shows the launcher drawing its idle attract
screen. Physical navigation, game input/Stop, raw loading and intentional live
identity mismatch recovery on this exact image remain pending.

The first deployment attempt exposed an exFAT filename alias on this kit:
`launcher.json` and arbitrary long suffix paths resolved to the same inode.
Staging with a suffixed filename replaced the active configuration, and the
backup precondition stopped the image swap. The ordinary pairing was
reconstructed from the owner-local paired configuration and the observed
Classic theme; exact pre-attempt remote configuration bytes were not retained.
The corrected installation backs up configuration off-card before uploading,
uses distinct short basenames, and checks actual directory entries and inode
inequality before replacement. It retains the previous image as
`/media/fat/linux/fesold.img` and the configuration as
`/media/fat/fogcast/fcold.json`, with an additional owner-only local copy.

Published review dependencies are mister-packages #6, libmister-runtime #19,
misteross #22, FogCast #200, Yosys #8 and nextpnr #41. FES #12 remains a draft
until the outstanding exact-image hardware checks finish.


## Interactive acceptance and recovery follow-up

On image `866d43a6915b2d6c7fbbddb109ab307d5be9b3d72ed616a9826dac42872cfcbf`,
the user confirmed catalog Pong launch, play, exit and subsequent menu navigation.
The user separately confirmed standalone FES Pong play and return to the menu.
The timed standalone video capture preceded the user's play and does not by
itself establish the ball's motion during this confirmation.

An unsupported ABI package was rejected during compatibility without replacing
the active standalone package or its input session. A package with the same
payload but a deliberately different declared build ID was correctly rejected
by live identity verification, but automatic MENU recovery failed. An operator
reboot restored the ordinary launcher; this is not a successful automatic
recovery result. The runtime cleanup correction remains pending verification.

A separate raw-load failure occurred after explicit Stop: the host redundantly
attempted Stop after releasing its lease. FogCast
`e74caf212471bc67befb10df5304fb093e614541` corrects this. A diagnostic host built
from that commit passed catalog Pong launch, explicit Stop, raw MiSTer RBF load,
and Stop back to idle against the image above. This verifies the host correction
on hardware, but does not establish an integrated image containing that commit.
Evidence: `out/acceptance/20260909-raw-stop-fix-e74caf2`.


Runtime `04b20509a5501c1fdf6400e21a8dd6567d6c5e33` was subsequently tested
through a temporary executable bind mount against the same installed image.
Its ARM diagnostic SHA-256 is
`5d4d9b630d7a9731e1a98909f1cd7eade1246822dc522bf79dcb5c29eed83c9a`.
The mismatch cleanup now quiesces the verified FES GP driver, probes MENU, and
verifies HDMI successfully. The installed target agent still publishes failed
state for an attempted package error, causing the host to report a recovery
error. Explicit Stop restores idle without reboot. Target/host reconciliation
requires correction before this negative test can pass end to end.


## Reviewed recovery diagnostic passed

The kit passed the repeated identity-mismatch test with runtime
`04b20509a5501c1fdf6400e21a8dd6567d6c5e33` and matching FogCast host/target
`9444091d3a4f3eea7002599a61fa00ab5ea7af3b`. Both attempts returned
`UNRECOGNIZED_CORE` in the identity phase and left the session idle. The direct
HTTP response retained expected build ID `70ba707329b4e7c8c86d4389e6fa510a`
and observed ID `60ba707329b4e7c8c86d4389e6fa510a`. A subsequent 007 launch
and Stop returned idle without reboot, followed by a successful raw MiSTer RBF
load and Stop.

The image launcher was paired to the diagnostic host for this passing run.
Earlier runs left it paired to the ordinary UI host and encountered native
launch/Stop errors despite successful runtime MENU recovery; those attempts
are not passing acceptance evidence. The CLI's compact error projection does
not include build-ID fields, so the repeated request verifies those fields
through the HTTP API.

These were temporary executable/configuration bind mounts over the existing
image, not a rebuilt integrated image. Diagnostic host SHA-256:
`3aff8fbcdc2fb82538a82a1591e7af72786374effad788d9e45d2a372b76c0e0`;
target agent SHA-256:
`e3f6310954128debcb0b9111f05a587b4a314184e680cd886d228b05176effe3`.
Evidence: `out/acceptance/20260909-package-recovery-04b2050`.
