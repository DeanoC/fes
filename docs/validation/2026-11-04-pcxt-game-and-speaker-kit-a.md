# PC/XT DOS, floppy, game and speaker diagnostic on Kit A

On 4 November 2026, a claimed Kit A ran the source-bound PC/XT application
diagnostic using the already-active, promoted Kit A software. The test used
producer commit `6974817dfecc1e86b89e04429738790af73e0285` and diagnostic tool
commit `fb18b230486e6d457b097a2b16a8ef5d6c2cf053`. This is a hardware diagnostic
of that producer and the named artifacts, not hardware acceptance of the PR
head or of later changes on main.

| Artifact | SHA-256 or ID |
| --- | --- |
| PC/XT package ID | `af68cf5e9c290ebee8e1b4def79b82ab1c2c705e43db8b7b22be7c31bcd11155` |
| FPGA RBF | `205afdb024c0b81eec5e7089a28d6398a990938c5a4d9e10bcde71ea7f4d6559` |
| Kit software bundle | `d695e970c1b455ed483e287f560f9e0c40abd4ea2d64402b09548a70b0feda0c` |
| Kit image | `f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4` |
| Combined receipt | `d64618b20d197d69d8822b8a2a19aee9454d4f8696fa4063cd54d7f480f6e1d6` |

The retained private evidence is under
`/home/deano/.local/state/fes/pcxt-main-697-reuse-20261104/private/game-run-1/`.
It includes the combined receipt, individual phase receipts and captures. The
game executable is not distributed in this repository.

Independent review of the captures and receipts found:

- FreeDOS booted to `A:\>`. The COM program displayed `READY (PRESS K)`,
  accepted `K`, displayed `PASS`, and returned to the DOS prompt. Its receipt
  SHA-256 is `94f2618efe4c65bd94c70ff04a494e833a913a420ae02471519b9e65ac2e52d1`.
- The guest copied the known marker to `FESSAVE.TXT` and saved the floppy. A
  separate flight and input session, with core generation changing from 1 to 2,
  read the same marker after Stop and relaunch. The write and verify receipt
  hashes are `8c5363ac0136745bfa161c394820db62acffb3aaa627ed43be36585b446f2194`
  and `c538a4423a7a60c84115aa2eea2db8304218430ba36e7fec26e62abebb45c493`.
  This establishes one guest-file save and readback across relaunch, not
  power-cycle durability or whole-image byte identity.
- A live Digger playfield and player were visible. In the retained recording,
  the player's orientation changed from right to left, then right, then left
  after the ordered directional inputs. Frames 1373, 1425, 1479 and 1536 show
  those respective orientations. This establishes the bounded facing-response
  observation; it does not establish sustained travel. A later collision and
  death occurred after those comparisons. The video SHA-256 is
  `5b9d8cfea7d617bb26cc8e145ce117dbab3aaa5cd90613752313b6ece1bfa3ad`;
  the contiguous frame-metadata SHA-256 is
  `6fd613c99f1aba1aa08077603a5c74940a7376dcd7e6f44648913037e912436e`.
- The Kit A HDMI audio recording shows a measurable signal, digital silence,
  then signal in the planned F9 observation intervals. The user listened to the
  recording and confirmed that the audio sounded right. Its SHA-256 is
  `43b9a5bd13090a3d1e0af763b7b509989e74bfe2ec90567d524f353d14b79ab8`.
  FFmpeg progress associates the samples with the host-observed phases, but
  audio/video clocks and device buffering were not calibrated. Exact key-to-sound
  timing and guest delivery are not established by that association.

On completion, the owner stopped the guest and private host, verified idle and a
free lease, confirmed the same boot and image and the promoted bundle active and
selected, and released the kit. The local release audit found no remaining owner
or host process and no holder of the relevant kit or history locks. The earlier
failed attempt remains in the history. The later game recording and sound
evidence do not qualify whole-board timing, general DOS compatibility, or an
uninterrupted game session.
