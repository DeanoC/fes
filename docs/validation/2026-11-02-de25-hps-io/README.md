# DE25 native read-only HPS I/O observation

TRWU8CBL passes two exact live samples of the sealed build's 164 HPS pinmux
registers. The native result and independent saved-byte audit pass, with
unchanged cold owner/generation 1 and every bridge held. This is a fixed
read-only hardware diagnostic. Normal runtime package admission, general boot
qualification, clocks/RAM qualification and Tenfoot physical launch/return
remain open.

## Source and artifact identity

| Item | Identity |
| --- | --- |
| Base | `2b3dca63b3d6e6ae04b863dbf8891ad883c0862e` |
| Compiled native/firmware implementation | `4b62ea3234f6d9ae427327d80033594a1100d4b8` |
| Tested checkout and final software checks | `7b05f14719cd5ef5733f9f5a78e69e4f54347c0c` |
| Unchanged sealed FPGA producer | `c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6` |
| Native boot ID | `0f121dd65e2abc81` |
| Compiled handoff SHA-256 | `a17de6f2dbf5f4e93643b50b4c8291a33f930064f9a43bad3ece24e9c3dda04a` |
| I/O map SHA-256 | `03e83b579fc5edd07a52dd6b58ea27481c43733dcd72eb8f2b66422b454640c5` |
| Native ELF SHA-256 | `76b95e0cc99516fa54d7af18cdec6973794f71580c4b4b936754c8017c862c24` |
| 5,386,140-byte FIT SHA-256 | `4662ecb6a0c21831ab51ae4c8292f80566556d7d613af86625d25e715e26cc44` |
| Native SOF SHA-256 | `16735e86b836047ec993dd1f52dab5026923d170c71d8a91a55d58676500fc3c` |
| 31,635-byte serial capture SHA-256 | `ea66a951ee774d47cb1073096963928064c11a1c6879d6289efe2d79527821bb` |

The follow-up commit changes only the SD-only host wire fixture, adding an
unbound optional-provider stub. All 110 selected native Git blobs still match
the compiled commit and current bytes; another 472 pinned module files and
826 compiler SDK sources match their build receipt. The matched entry,
BL31/SPL/FIT and HPS-first composition are reverified without rebuilding
unchanged artifacts. [Selected input audit](selected-inputs.json),
[linked selection/startup audit](selection-proof.json).

## Contract and implementation

`hps_io.py` derives a canonical expected map from the actual compiled
`hps_bootloader_handoff.bin`, after sealed-project verification and an exact
match against `board/hps/build.json`. It covers 48 mux, 48 I/O-control, 20
FPGA-selection and 48 delay registers. The two generic FPGA entries at
relative offsets `0x1c0`/`0x1c4` are excluded exactly as in the pinned Agilex 5
vendor initialization; they never become observer addresses. All accepted
masks are full-width. The checked fixture retains the actual 1,629-byte
handoff and its provenance.

The descriptor and its digest travel through the native SDK, entry receipt,
cold SPL/BL31 chain and HPS-first composition. Shared experimental bridge
contract 1.10 adds operations 12/13 and the separate `FHI1` response tag.
Requests contain only the existing expected owner/generation and cookie,
with no address, pointer or write value. Unbound providers return NOT_READY.

The opt-in `hpsio` secure provider checks cold ownership, the boot CPU, locked
secondary CPU release, EL3 translation/data-cache state, contained masters,
closed bridges and absence of a device lease around every read. Part 0
samples every register twice and compares all values to the compiled map;
only then does it return the first digest half. Part 1 verifies the pending
scope and returns the second half without another read. Failure retires the
observation while preserving its first error. Native Go brackets the pair
with identical verified source-context/bridge observations.

The strict decoder requires all 328 ordered raw register records, both
response boundaries and the matching native result. Partial, duplicated,
changed or reordered observations cannot complete. `HPSContextVerifier`
still does not implement `BootHandoffVerifier`, and this change registers no
Nano runtime programming profile. [Expected map](hps-io.json).

## Hardware result

The single native attempt ran on 2026-11-02 from 22:10:52 to 22:11:17 UTC.
One qualified recovery boot and one owned Ethernet upload session wrote the
new diagnostic FIT, independently read it back from the retained spare card
and freshly checked its hash. Protected recovery/card checks pass. The core
package was not rewritten. The matched native boot then passes the loaded
BL31, cold CPU/reset/ownership, EL3 capture and EL1 entry gates.

Both pinmux samples match, under the same closed generation 1. The native
observation takes **6.722842 seconds**, within the unchanged 45-second bound;
the loader prompt retains its 50-second bound. There are no native SD
writes, phase-two FPGA transfers, bridge openings or data releases. The
original experiment and native controllers both pass. A second independent
decoder invocation on the saved capture agrees with the stored report and
rejects five damaged copies. [Hardware receipt](hardware-evidence.json),
[raw serial capture](native-console.log).

Cleanup reads back the original 15 MHz JTAG clock before stopping only the
owned private top-board service. Exact USB permissions are restored, the
UART is free and the continuing claim remains owned. The spare and completed
diagnostic stay on the exclusive top kit under Deano's retention policy;
its factory card remains safe. No factory restoration, bottom-kit operation
or 3090 Quartus job occurred. Future 3090 jobs must hold the existing shared
`/tmp/lander-quartus-3090.lock` throughout.

## Software evidence and retained failures

All 12 final affected software scopes pass on `7b05f1471`, including 1,314
exact parent tests in three intact-module partitions capped at 300 seconds
each (two environment skips), generated consistency, the SD/PIO lifecycle,
native firmware/FPGA launch integration, producer integration, shared schema
checks and 1,577 FPGA software tests (75 environment skips). `make generate` and `make check` pass
for the definition change. Focused checks exercise 984 read/value/ownership
failure positions, the actual strong-provider dispatcher, native Go digest
and scope validation, and serial rejection. The actual outer controller has
74 injected cases; the upload helper has 10 cases over the real
`UploadSession` and a stateful card/memory model.
[Software summary](software-summary.json).

The first broader run correctly fails because the SD-only host wire fixture
lacks the new optional hook. The unbound fixture stub fixes it, without
changing deployed bytes. The failed run and successful final run are both
retained. An initial composition invocation uses the wrong option name and
stops before creating output; the corrected invocation is retained separately.
An input audit initially assumes ignored module/SDK files are Git blobs; the
corrected audit reports committed and receipt-bound inputs separately.
None of these off-board failures opened hardware or reset a hardware retry
limit. There was one physical attempt for this implementation.

Private build, trial, receipt and failed-run files are under
`out/nano-hps-io-20261102/`; the final complete affected checks are under
`out/nano-hps-io-checks-20261102/`. Full source is independently restored and
checked without shared Git objects in
`/home/deano/fes-retained/de25-hps-io-20261102/`.

The next step is to combine this proven I/O subset with a qualified CPU/RAM
and programming-aperture contract, then bind an explicit Nano platform to
the retained-package runtime. Source hashes or this pinmux receipt alone
cannot supply that authority.
