# PC/XT Kit B diagnostic, 5 November 2026

This was a **partial diagnostic**, not a complete game/speaker acceptance or a
Tier 1 qualification. On Kit B, FreeDOS booted to `A:\>`, a keyboard-driven
COM test passed, and a file saved from the guest was read back after Stop and a
new session. The Digger playfield was visible, but the active-game observation
failed before the direction-input checks. Audio was recorded but not accepted
as speaker proof. The overall run is recorded as **FAIL**.

The sealed `fes.pcxt-dos` package was produced from main `bb87b9b657bf9f1826dd1b9e001736e7bd8484ee`
and selected by the normal cache-only resolver for the then-current main
`6f49758f050342d7e2aaa5755673c2fb257ab5fe`. Selection does not make the
producer and selected commits identical. The kit ran the separately receipted
`bb51f4454eb72048cefeea16aa16b28eb2aadaad` software bundle on the existing
`f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4` base
image; no software activation, image change or reboot was part of this run.

| Identity | SHA-256 |
| --- | --- |
| Package ID | `87fd8149c09a7e319092c56802b891039a118dfe0378d302a6543bcf841659fc` |
| Archive | `19fd34ec5d193e2e8c6b7723ea86b02bb9e8f76bd4febf2a77c88a0759b224d8` |
| RBF | `94b3c222bee23d24fd027a8c908d90aa9f53eae9b01db648e655f8b1d7ac0222` |
| Prepared selection receipt | `984bc5dd479f8d01275df67be23e191b00acba03dff28fb032d5ec6555bbe06b` |
| Kit software bundle | `a8cd30c49afd79eb7668af7ab7ae8a8e2ed2a90a31b25f164e28128b446274ee` |
| COM receipt | `34f6fbb227adf61a2ef447680e9306b3453dda1c8079172965b589748f667020` |
| Floppy write receipt | `04ab9f47318dbb8fc8a4c8618e78d4b998a6b6d159bdd066a8762cb9d1525f3b` |
| Floppy readback receipt | `45006ccbab023aa1e5be70c6f19a8a696bfa58eb0b2a75abd42d9fc4fabb058e` |
| Game failure receipt | `9be85815eb4a3781afd14b40955534bda44d0c28c959adc6ba462e0f1c0b60ef` |

The write and readback receipts bind the same saved revision, with different
flight IDs and package generations 1 and 2. The COM capture shows
`FES COM READY (PRESS K)`, `FES COM PASS` and return to `A:\>`; the readback
capture shows `type fessave.txt`, the expected file contents and a returned
prompt. This tests an explicit library Save across Stop and relaunch, not
persistence after a power cycle. The capture came from Kit B's 4KPRO loopback;
the local video timestamp is not a target timing measurement.

The local evidence is retained in
`/data/deano-pcxt-normal-owner/combined-evidence/`; the Stop and baseline
recovery evidence is in `/data/deano-pcxt-normal-owner/owner-evidence/recovery/`.
After the failure, the exact owned game session was stopped and the kit was
verified idle, lease-free and returned to its as-found software and image. The
game/speaker test and the full exact-artifact kit acceptance remain pending.
