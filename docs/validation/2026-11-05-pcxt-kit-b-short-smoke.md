# PC/XT exact-package Kit B short smoke, 5 November 2026

Kit B booted FreeDOS to `A:\>`, passed a keyboard-driven COM test and saved
and read back a guest file after Stop and relaunch. In a separate Digger flight,
the captured player changed facing after one guarded left input. Captured audio
showed signal, near-silence and signal across three F9 windows. This is a
bounded game and speaker smoke; the player was essentially stationary and
sustained gameplay was not established. Deano listened to an excerpt and found
the sound identifiable and acceptable, but did not know the original audio, so
this was not a fidelity comparison.

The sealed `fes.pcxt-dos` package was produced from main
`bb87b9b657bf9f1826dd1b9e001736e7bd8484ee` and selected by the normal
cache-only resolver for then-current main
`6f49758f050342d7e2aaa5755673c2fb257ab5fe`. Those are distinct identities;
selection does not change the producer commit. The acceptance tool was at
`2e7ee3165891f5d268edbcaf8cfc45f5dc0ce9c3`. The kit used its existing
software bundle produced from `bb51f4454eb72048cefeea16aa16b28eb2aadaad` and
base image `f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4`.
No software activation, image change or reboot was part of the run.

| Identity | SHA-256 |
| --- | --- |
| Package ID | `87fd8149c09a7e319092c56802b891039a118dfe0378d302a6543bcf841659fc` |
| Archive | `19fd34ec5d193e2e8c6b7723ea86b02bb9e8f76bd4febf2a77c88a0759b224d8` |
| RBF | `94b3c222bee23d24fd027a8c908d90aa9f53eae9b01db648e655f8b1d7ac0222` |
| Prepared selection receipt | `984bc5dd479f8d01275df67be23e191b00acba03dff28fb032d5ec6555bbe06b` |
| Kit software bundle | `a8cd30c49afd79eb7668af7ab7ae8a8e2ed2a90a31b25f164e28128b446274ee` |
| Combined receipt | `ff25cc13999f405b049f3f22e482ead76820dec23d7ce826a28064a993d7431a` |
| COM receipt | `e2ed0bd612ae00b85fc36f646cf61bd249ced2b660e9f3c06d6969b0bba04902` |
| Floppy write receipt | `ca2d2d2b6d85aff0a9228d7ad2f276b54c1aca8b6028f2785a38ef3fecaee61d` |
| Floppy readback receipt | `17c753f06ea55a0611f83a92f9dbd03ada464dfdae7860f9dbad9f6d00c327ee` |
| Game receipt | `16d9fae6834fe69cad7dba22d0960c96f9bea9e3daf2d841f06afa52334d46e6` |

The file test used an explicit library Save and a subsequent relaunch; it did
not test persistence across a power cycle. Independent inspection of the game
capture found a right-facing player at encoded PTS 128.9 seconds and a
left-facing player around 131.2 seconds after the recorded left input. The
three recorded F9 windows had left-channel, DC-subtracted RMS values of about
1467, 0.11 and 1463 signed 16-bit counts, respectively. These observations
show a captured speaker response, not original-audio fidelity. The audio
and video capture clocks and buffering were not calibrated for target timing.
External IO, HPS and reset timing remain separate from this smoke.

The run's local evidence is retained in
`/data/deano-pcxt-short-smoke-b-v2/combined-evidence/`; independent stills and
an audio excerpt are in its sibling `visual-review/` and
`audio-review-f9.wav`. The owner outcome and baseline verification are in
`/data/deano-pcxt-short-smoke-b-v2/owner-evidence/`. The kit was left idle,
lease-free and at its as-found software and image. The earlier
[partial attempt](2026-11-05-pcxt-kit-b-partial.md) remains recorded as failed.
