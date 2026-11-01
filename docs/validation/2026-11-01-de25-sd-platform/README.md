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
