# ZX81 full Zon X AY — Kit 2 diagnostic

Kit 2 passed the selected full-AY cart diagnostic on 2026-10-01. The ordinary
private-host library path imported the exact shell, expansion and open 8192-byte
ROM, selected the `machine-rom` and cart, and launched the returned game ID.
Two launches retained identical package, BUILD_ID, composition and programmed
ROM-link identities with generations 1 and 2. The ASUS HDMI audio capture
contains the isolated A/B/C tones, mixed tones, noise and envelope phases.
Repeated-R13 restart was subsequently identified as a gap in this original
firmware; it is covered independently in simulation and the follow-up capture.
Hold and both Stops settle to silence within the capture's filtering limits.

Kit 1 was left undisturbed. The user designated Kit 2; its independently
recorded target ID matched and its lease was free/idle before service changes.
The private host acquired and renewed the existing target lease during the run.
Original services were restored, both on-disk and live executable hashes match,
the boot and installed-image identities are unchanged, and the final lease is
free. The temporary container, private credential and target overlays were removed.

## Frozen selection and platform

The shell/cart/compiler identities and full-CRAM containment are in the
[build record](2026-10-01-zx81-zonx-ay.md). No compiler or physical socket
boundary changed during this run. No shell/cart rebuild followed the kit change.

| Observed item | Identity |
| --- | --- |
| Kit 2 target | `67c5f4e2-d288-49bb-9049-39ecf39cf6f6`, `192.168.10.85` |
| Boot | `da6bd708-6c4a-45ca-b600-d58a94d72b27` |
| Installed image | `f449886fc026dbf678e7ab22ac14dd6d54c924485658e1015835f8fd3c9a127e` |
| Original agent/runtime revision | `d7e13eaa01c062f924a7a287c03261f6021b671e` |
| Diagnostic host/agent/runtime source | `977cab8ea266dce3a4f66e6977f2f80614892912` |
| Diagnostic dynamic runtime SHA256 | `38be527f48bccd1e1cd0c9ca0ea53c77ac01623a9bc560eb88466c978a4d7f19` |
| Diagnostic agent SHA256 | `19af131b46a3c4fc06c89b233a7de2b7aedd026c101a500bb5bff1abb4a23ebc` |
| Diagnostic host SHA256 | `cdb2170afdc9ab9abf2332cfc55b76bdbaf6bd8b398cf55f0c69e21e9e95f8c3` |
| Composition | `83f72896120826e165574f493e69f96133f0fac8c629b3cc923cd308ea09944c` |
| ROM programmed RBF | `9d38a8c112c29aa388ccfc31f42decfbc0db930f388918a8e4d2942bce131b47` |

The runtime and agent were temporary bind mounts over `/usr/sbin` executables,
started/stopped through the existing init supervisors with SIGTERM. No immutable
image bytes or agent configuration changed. The platform artifact record still
reports the installed runtime commit and installed executable digest during an
overlay; that record is not evidence of the diagnostic executable. The separate
[build receipt](zx81-zonx-2026-10-01/dynamic-build-receipt.json), frozen binary
hashes and live process/mount records distinguish these identities explicitly.

The first static runtime aborted at startup before FPGA programming or any
lease claim. Restoring original services succeeded; a dynamic binary from the
same frozen source started and completed the run. The static executable is not
qualified. Logs and failed preparations remain in the task's ignored `out/`.
This was an ad hoc cross-build problem, not a cart or compiler-routing failure.

## Audio, Hold and lifecycle

The open firmware uses original CF/0F writes and repeats twelve phases:
mute, A, B, C, mixed A/B/C, noise, decay, rise, triangle, hold-high,
a fresh shape-0 decay labeled `retrigger`, and final mute. That historical
phase does not establish unchanged-R13 restart behavior; the corrected
generator writes shape 0, delays, then writes shape 0 again. It generates no display file, so the active frame
is intentionally blank. CPU firmware simulation independently validates this
sequence; the hardware capture exercises the independently sealed implementation.

The ASUS 4KPRO (`802B003090700329`, ALSA `hw:CARD=D4KPRO,DEV=0`) filters
and rescales audio. Captures are 48 kHz stereo FLAC. Mute has a small DC offset
and sub-LSB variation; it is not a bit-exact copy of the FPGA PCM bus.
Frequency and changing envelope amplitude are observable, but digital DAC
levels, bit-exact zero and an analog Zon X board are not established here.
Steady Hold variation falls below 0.5 signed-16-bit LSB per channel after the
filter transient; the offset itself is retained in the evidence.

Independent [capture analysis](zx81-zonx-2026-10-01/independent-audio-analysis.json)
reports these stable tone windows; the common discrepancy is below 0.004%
and consistent with capture sample-rate scale.

| Channel | Nominal Hz at 1.625 MHz AY | Active Hz | Relaunch Hz |
| --- | --- | --- | --- |
| A, period 254 | 399.852 | 399.866 | 399.866 |
| B, period 169 | 600.962 | 600.982 | 600.982 |
| C, period 127 | 799.705 | 799.733 | 799.732 |

The mixed phase contains all three peaks. Noise has absolute autocorrelation
below 0.028 at nominal tone lags, compared with above 0.9938 for isolated
tones. Phase-aligned early/middle/late envelope windows show decay
(AC RMS 0.0749 → 0.00469 → 0.000569), triangle reversal
(0.0748 → 0.00391 → 0.00617), held high output
(0.00356 → 0.0499 → 0.15390), and the fresh shape-0 decay
(0.0722 → 0.00471 → 0.000690). Rise and relaunch trajectories are retained
in the JSON. These are filtered capture observations, not an exact
register-level envelope comparison; all sixteen shapes have separate
independent simulation coverage.

Hold was a bounded GP mailbox diagnostic under the private host's ownership,
using the existing execution opcode rather than adding a public launch API.
The operator sent Hold, captured five seconds, and sent Release while preserving
the original request-toggle parity. The [acknowledgements](zx81-zonx-2026-10-01/hold-gp.json)
were `f5000000` and `f5800000`. Normal host Stop and game-ID relaunch followed;
this does not claim a new host pause feature.

The [active](zx81-zonx-2026-10-01/active.json) and
[relaunch](zx81-zonx-2026-10-01/relaunch-active.json) receipts bind the same
shell/cart and open ROM to runtime generations 1 and 2. Lossless evidence:
[active](zx81-zonx-2026-10-01/active.flac),
[relaunch](zx81-zonx-2026-10-01/relaunch.flac),
[Hold](zx81-zonx-2026-10-01/hold.flac),
[first Stop](zx81-zonx-2026-10-01/idle.flac), and
[final Stop](zx81-zonx-2026-10-01/final-idle.flac).

[hardware-result.json](zx81-zonx-2026-10-01/hardware-result.json) freezes capture
hashes, software/platform identities, unchanged tuple checks and restoration.
The [restored health](zx81-zonx-2026-10-01/restored-health.json) equals the
initial health, and [lease status](zx81-zonx-2026-10-01/restored-lease.json)
is free. The retained [restored frame](zx81-zonx-2026-10-01/restored-idle-later.png)
shows Kit 2's existing menu. Full scripts, API outcomes, mount/process observations
and failed static preparation remain in `out/hardware/zx81-zonx-kit2-20261001-*`.

This qualifies these exact shell/cart/open-ROM bytes through the filtered
Kit 2 HDMI capture and owned lifecycle. Factory-image acceptance, proprietary
music compatibility, speakers, physical original-card phase/analog filtering,
and a bit-exact capture on another device remain separate.

## Repeated-R13 follow-up

Review identified that the original phase named `retrigger` wrote shape 0 only
once after shape 13. Its capture showed a fresh decay and did not establish
unchanged-R13 restart. The corrected generator at `00c46c1ed` starts shape 0,
waits approximately 0.524 seconds for it to advance, then writes shape 0 again.
The CPU simulation observes both actual OUT writes with a substantial gap,
alongside the existing independently specified engine restart tests.

The corrected 8192-byte ROM SHA256 is
`c063e16f1bbf652195bed14556efdc3820c9435f76ab2f9012e9787094f5e3ec`.
A separate private-library run used the same frozen shell/cart and diagnostic
software from this record, changing only the explicitly selected ROM. Its
programmed payload SHA256 is
`cb81f272730e49e96ccee8cfe244e316425eb5a97c7a5f0f89122c6cdba7ba22`.
The [active receipt](zx81-zonx-2026-10-01/repeated-r13/active.json) and
[relaunch receipt](zx81-zonx-2026-10-01/repeated-r13/relaunch-active.json)
retain the same composition and ROM tuple with generations 1 and 2.

Independent [analysis](zx81-zonx-2026-10-01/repeated-r13/independent-analysis.json)
of the new [active](zx81-zonx-2026-10-01/repeated-r13/active.flac) and
[relaunch](zx81-zonx-2026-10-01/repeated-r13/relaunch.flac) captures finds
paired decay attacks separated by 0.520–0.525 seconds in 5 ms analysis bins,
consistent with the programmed half-delay. Active restart events at 10.705
and 23.815 seconds raise normalized AC RMS from 0.00182 to 0.154, then repeat
the decay; paired decay profiles correlate above 0.999. Relaunch shows the
same restart pattern. Capture filtering and amplitude limitations still apply.

The [result](zx81-zonx-2026-10-01/repeated-r13/result.json) retains exact
identities and hashes. Original target health and live executable hashes were
again restored, boot/image unchanged, private credential/overlays/container
removed, and [the lease released](zx81-zonx-2026-10-01/repeated-r13/restored-lease.json).
This follow-up qualifies repeated unchanged-R13 behavior for these exact bytes;
it does not replace the original ROM's frozen identities or extend factory
acceptance. It uses the existing library launch path; workbench bus 2 readiness
is separately tested through the real host admission/selection path.
