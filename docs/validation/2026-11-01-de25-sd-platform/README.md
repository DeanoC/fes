# Controlled native Nano SD platform diagnostic

Scope: connect the retained cold boot owner to actual Nano reset, clock,
pin routing and ComboPHY callbacks, then bind native Go storage through the
owned SD service. This is an exploratory read-only diagnostic. It grants no
FPGA programming authority, active bridge access or secondary CPU release.

Implementation starts from `67b023808b24dfaeb4f60a453b6d40dbee026a94`.
The actual C callback tests cover two leases and retained reset, clock, PHY
and drain failures. Strong early-provider tests cover both disabled serial
and controlled SD profiles, with no controller reads during secure setup.
Profile/export tests reject unknown profiles and stale selected native sources.
The serial observer checks the independent recovery manifest hash, same-owner
generations 3/4/6/7, and final containment; partial, duplicate and failed
sequences cannot pass. Host models and cross-builds do not establish SD operation.

`nano/sd-profile.json` is exact schema 8. SD remains reset at CPU entry and is
granted later through the secure service. The direct static-readiness path is
removed. The existing serial profile keeps SD disabled. SD is restricted to
one-bit PIO at <=400 kHz, using a checked 200 MHz input and the exact pinned
Terasic Nano timing; clock and pin routes are observed, not reprogrammed.
No card writes or automatic failed-lease recovery are implemented.

The matched candidate was built under
`out/nano-sd-platform-20261101/`:

| Artifact | SHA-256 |
| --- | --- |
| Native storage ELF | `8e224b0dbad34afdbaba8b32bb6bbc26fa2fb9b43dc49843d78553bd5e5fc139` |
| Entry image | `0186607a37601dae3223ad05b8b2e56e60ba133527eb9692669495ecebda405c` |
| Secure firmware | `dac990cecc39e464ab54c1d4213a3e970f1a1b62a528e07dc8f3bf523a4218ed` |
| Sealed FIT | `92b85f06253e1b18a403f7a8e0db90b42e5d603a1876c5249695365c29ac7f01` |
| Matched SOF | `07c378becf955fb81a2b3d6c06a3c5de95abe3d7b60fda77384bec4b1a2a57fa` |

Build ID: `19410f6a83a571e2`. The producer independently decoded the embedded
SPL and verified the sealed FIT components. The first entry build stopped
because the isolated cross assembler lacked its shared-library search path;
the retained second build uses the previously recorded tool environment.

The bounded TRWU8CBL procedure first boots the exact recovery loader, verifies
the spare card and protected boot files, uploads only `FES/UPLOAD.BIN` over
Ethernet and independently reads it back. Only then may the matched native
SOF boot and one `fesboot` command run. Native Go must read the unchanged
1,098-byte `FES/RECOVERY.JSON`, contain SD, resume and reread it, then finish
contained. Its expected SHA-256 is
`1a345ddfefc37e1fcfc5cebd17c2cb33dea8b2b70c921b32aac36aec9fe93035`.
The original top-cable 15 MHz rate is restored after the temporary 6 MHz trial.
The user permits retaining the diagnostic and spare card in this exclusive kit.

Hardware outcome is recorded separately after the bounded trial. This candidate
does not yet establish writable SD, USB, native Ethernet, display, FPGA/core
loading, or full launch and return behavior on hardware.

Software checks completed: 1210 parent regressions, 1571 FPGA build-tool tests, generated consistency, uncached C/Go SD race checks, native firmware/FPGA/tenfoot model integration, producer integration and RTL control tests. All eleven selected affected stages passed.

## First hardware attempt and clock-status correction

Candidate `19410f6a83a571e2`, source `1d0f8c044`, passed fresh recovery, Ethernet upload, SD readback and protected-file checks. Native SPL passed its initial scope, DDR, sealed FIT and SD-stop handoff. Secure setup then rejected Clock Manager STAT `0x02030300` before granting native SD access; no `fesboot` command was sent. The captured trace ends during the failure report; a later passive one-second read returned no bytes. No prompt or native SD acceptance is inferred. Original 15 MHz and USB attributes were restored.

The [official CS/R1 RC28 register map](https://docs.altera.com/api/khub/documents/Z5suNak8Ubz6FMY3WjgxBQ/content) defines MAIN_TRANS bit 9 and PERF_TRANS bit 17 as completion bits: 1 is complete, 0 is in transition. The initial implementation had this polarity reversed. The correction requires both bits high, both PLL locks high, BUSY low and boot mode clear. The live 200 MHz derivation and every reset, route, PHY and ownership check remain required. Added failures cover each incomplete transition, BUSY, boot mode and the old locks-only value.

All 44 direct C/platform/profile/export/trace tests passed after the correction. The unchanged broader software checks remain bound to the first candidate; a second full affected run is not claimed. A fresh matched firmware/FIT/SPL/SOF is required before another physical attempt.

## Verified native SD cycle

The corrected candidate `db7c01de309270e7`, firmware source `0b55ff3826e85e0da3ee2d7ddf9c79d54861cf5e`, completed the read-only native SD cycle on TRWU8CBL. Its SOF SHA-256 is `448a6bc8d79540641a5fd5205d4b33cdd095f7be2341098f32511f1ff24f90e6`; FIT SHA-256 is `00e7198b8fc6daa74b754e15437f80a2488110dc89f57fb14dc1f8fcf026bcd6`. The unchanged native storage ELF/entry from the first candidate was reused.

The first recovery-programming attempt for this candidate stopped with a JTAG-chain error, before upload or any SD write. A read-only scan returned the expected device ID. One bounded second recovery attempt succeeded, followed by Ethernet upload/readback with protected boot files unchanged, trusted SD setup, the original one-core EL2/EL1 handoff, native FAT32 reads and firmware-controlled containment.

The [retained complete trace](native-sd-console.log) reports 1,098 bytes and the expected hash for both reads, card capacity 7,774,208 sectors, unchanged owner and generations 3, 4, 6 and 7. The final receipt is contained. Native SD writes were disabled throughout.

The original automatic runner remains recorded as failed: its parser incorrectly expected 16 CID bytes. The actual driver explicitly reports the 15 CID bytes supplied by SDHCI R2, excluding the CRC/end byte. The observer now requires exactly that width and rejects shorter, longer and zero identities. All 15 observer tests, including the immutable physical trace, passed. `hardware-result.json` records successful validation of the saved complete bytes with the corrected observer; hardware was not rerun for this observer-only fix. Original failures and hashes are retained separately.

Read-only post-test checks confirmed JTAG `4362C0DD`, original top-cable 15 MHz, restored USB permissions and no UART owner. The diagnostic, spare card and continuing claim remain on the exclusive top kit under the user's retained-diagnostic policy. Factory restore was not needed. This is narrow native SD diagnostic acceptance. Writable storage, USB, native Ethernet/display, FPGA activation and real core launch/return remain separate work.

## Retention

The artifact archive at `/home/deano/fes-retained/de25-nano-sd-platform-20261101/artifacts.tar.gz` contains 1,854 individually hash-verified members, including selected SDK sources, both matched firmware/bootloader candidates, entry and SOF/FIT bytes, source exports, recovery inputs, raw traces, original failures and bounded trial helpers. Its SHA-256 is `1cb0605f6aec9dd2520b605324610eedd4e85d001c7656deebf68b9817609772`. Claim tokens and factory credentials are excluded.

The adjacent `source.bundle` retains this milestone's commits; it requires base `67b023808b24dfaeb4f60a453b6d40dbee026a94`, already retained by the preceding SD-lease milestone. It is an incremental source bundle, not a standalone repository. Final bundle identity and restore verification are recorded in the retained directory after commit.
