# DE25 ZX81 board visibility, 2 November 2026

Producer source: `7656e1954f7e27d0dc3634f60e869f97cf14301d`.
This is an off-board reset/visibility milestone. It does not admit either
package or establish native kit, physical HDMI or DDR acceptance.

The ZX81 board now qualifies configuration reset and PLL lock on the existing
50 MHz reference clock. The separate board pixel reset chain is removed.
A retained outstanding token requires a pixel-side reply confirming allow-low
and a closed visible line before the reference controller permits rearming.
Repeated faults keep that token pending. Video resumes only after a later
complete horizontal sync pulse outside vertical sync. RGB/DE blank immediately
on lock loss; active sync pulses are preserved. PLL loss also restarts HDMI
configuration. The renderer, frame ownership and peripheral reset stay unchanged.

The four new registered-data crossings each have two synchronizer stages.
Their source-to-first-stage D paths are cut; interstage and downstream paths
remain timed. Raw board reset ingress reduces from four clear pins to two.
Pin assignments, IO voltage settings, clock frequencies and HDMI output delay
assumptions are unchanged. No additional reviewed signoff exception or waiver
is introduced. Only the previously approved two HDMI asynchronous inputs are
reviewed exceptions, with fresh fitted evidence.

| Candidate | BUILD_ID | Compile | Reported timing | Raw findings | Package admission |
| --- | --- | --- | --- | --- | --- |
| ZX81 | `9fea2f1e44d09a8d98794704bc0fd9ee` | PASS, 628.0 s | 41 checks PASS | output30, input2; RDC0 | closed |
| MENU | `9e172e513f826cb8449f855f3e51e767` | PASS, 595.5 s | 69 checks PASS | output30, input2, RDC2 | closed |

Both reports contain all 85 rules and zero waived findings. The reviewed input
findings are exactly `HDMI_I2C_SDA` and `HDMI_TX_INT`. MENU's recorded input
closure includes the changed board files, so it was rebuilt with a fresh
identity even though its RTL is unchanged. Its two reset coordination groups
remain open. The parent API's actual records match all 577 ZX81 and 377 MENU
producer inputs at the producer source above.

The private fitted audit preserves the original compiled project byte for
byte. Across four operating corners it checks 24 system reset paths, 136
shared renderer reset/data paths and 104 new visibility interstage/consumer
paths. All 264 pass. The four new chains are recognized as first/second stages,
and every first stage drives only its own second stage. The smallest measured
visibility slack is 0.099 ns. These are internal fitted checks; the output
consumers explicitly remain unqualified in the fanout inventory.

The committed board bench passes nine nominal 50/74.25 MHz clock-stop cases
and 96 deterministic fault schedules (393,216 event steps), preserving whole
HS/VS pulses, immediate blanking and eventual recovery. Removing only the
outstanding-token guard compiles but fails the same bench's recovery check.
The focused 26 build guards, four producer checks and consistency check pass.

All 20 internal and 14 combined client/RTL cases pass on the host and under
QEMU AArch64 Cortex-A76. Internal models use the actual canonical ZX81 BUILD_ID.
The combined RAM-pack proof uses the declared `001122...` model identity and
behavioral memory/persona selection; it does not establish physical partial
reconfiguration or DDR behavior. Both architectures complete BASIC LOAD, RUN,
SAVE and reload twice, print 7, return to the editor 24 times and verify reload
removes the intervening edit. All four 923-byte SAVE files have SHA-256
`b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.
The branch-local combined proof fixture is copied verbatim from the actual
14-case report. At software source `96116fb8de6d36809ec25898b5ce3254b50fe79d`,
all 1,154 parent cases and 62 planner cases pass. The full FPGA aggregate has
1,787 cases: 1,760 pass, 19 skip and eight fail. The DE25 audit verifies that
these are exactly the previously user-excluded DE10/Cyclone V cases (seven
M10K mapper checks and one PC/XT-specific elaborator), whose seven source files
are unchanged. Every other executed case passes. The generic aggregate remains
failed; no test-selection, warning or signoff gate is changed. The private
Verilator 5.032 copy is hash-checked before and after use. The separate
consistency check passes with 40 consumers, 42 fixture copies and no copied pins.

The 6,839-file compiled/model archive (1,349,025,146 bytes) has SHA-256
`9c9d6165b9b9e122512f0275bf0b0cb6ec27ca15be905d5e99931b9d645bc761`.
It contains standalone source history, both complete compiled candidates,
actual model binaries/SAVEs, reports, helpers and original failed wrappers.
A fresh archive and source extraction verifies every byte and the safely
filtered file modes, then passes the public source/topology/timing/client/ELF
readers in 21.0 seconds. The source bundle verifies from an empty Git repository
without alternates or older worktrees. The archive is also copied to Powerboat
and its full SHA-256 verified. A separate focused/software archive restores
and verifies the exact generic failure set and negative controls.

The native owner-kernel card described in the [image preparation record](../2026-11-02-de25-native-owner-image/README.md)
is written and fully read-back verified. No new native boot is observed here;
the physical swap remains pending. No FPGA, QSPI or positive DMA operation is
performed in this milestone. External HDMI timing, MENU reset coordination,
firmware pairing, eligible independent identical-RBF verification and the full
native Linux/MENU/catalog/ZX81 launch/stop/relaunch/recovery lifecycle remain
required before treating the DE25 as a normal managed kit.
