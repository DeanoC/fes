# ZX81 in-session HDMI controls

Issue [#370](https://github.com/DeanoC/fes/issues/370) adds an opaque launcher
plane inside the running ZX81 shell. The workbench and cassette picker use
that plane while CPU execution, RAM, ROM and the expansion remain active.
Closing it drains physical scanout before machine input resumes. Display and
picker recovery do not load MENU, invoke Stop, reset the CPU or relaunch.

Software checks and a sealed package passed. A leased designated-kit smoke
test observed ROM-backed BASIC, visible picker actions and preserved program
pixels through swap/eject. Input was injected through Linux evdev: these are
real FPGA/HDMI observations, with physical operator button acceptance pending.

## Frozen selection

Base: `d0305225f8cfca939c7058ddae9a797a9711e26a`. The implementation is
`75d734f40c5ed7068ba9685c9d93b8a41564abe8`, followed by captured-target
guards in `6c0974b726b81784dd27ab2022c8d2da840b79e3` and transient presenter
recovery in `551b9d1b964593198a66d0e6ef7bf09bc0a1caab`, live cassette footer
status in `8ef4f259a8ba0a59332d825a26fee2bf0cea9408`, and paired first-read
identity in `5a58053229b5e1772defe5f9bdc34c72f659db71`. The final image and
host select all four module sources at `5a58053229b5e1772defe5f9bdc34c72f659db71`.
The FPGA source closure is unchanged by those software commits. Subsequent
test/provenance and validation documentation commits do not replace that frozen
build selection.

| Artifact | Identity |
| --- | --- |
| ZX81 package, version 1.5.0 | `f04f058c1a1b38de1c4f2f65f8191ac1e0fac63664de816300482b184411574f` |
| BUILD_ID | `7671359e7ef165e07820dacd5d7df8fd` |
| Base shell RBF SHA256 | `4c4efd299f6228fc38fe9d1d1917ced030a4ede188776f83e8705a9d934a00f0` |
| `.fcore` SHA256 | `24efd207383ad8f32059e16982dad2cc32ce05e6229f61ace2f5ff04862cb1be` |
| Sealed ROM map SHA256 | `0c0fc00252eee9dfff62eb6b02820eb6d28fa89b91af61f24dc77165cb83d2ee` |
| Household 8192-byte BASIC ROM SHA256 | `14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa` |
| Actually programmed ROM-linked RBF SHA256 | `a47763fddfe00ca181aa7da3cbfc31a4296495d155b58bccb0f606f546e6a8f7` |
| Idle MENU package | `6ecb849a8a35215d2cc58fb0c09a12b2faa3708b1f7de49feaeee4b609b11e40` |

| Software artifact | SHA256 |
| --- | --- |
| Final system image (128 MiB) | `5032c2d2282da26e79e6f7efc47f9f2a1aa9abca4aad4ca6f19817e82d18ce4a` |
| Release manifest, `0.2.0-dev.370.2` | `cf5c5141684077f79c58a0d87eb0a2c1e86213b82761b0385d1e1814c82da0f0` |
| Installed `mister-runtime` | `69e5c6476620db8cba638d2b93675d99d34981eaa30e9027482c7ef061f7f5cc` |
| Installed `mister-agent` | `dd91591c8eb6a9728bb1036cbeaba4025e52de5c747402c2c980bdb046986754` |
| Installed `fogcast-kit` | `586b18a5bcaf338c99a55974d9062c65aaaf107bbb44fb2abeeea9a14c4e95b2` |
| Matching amd64 `fogcast-api` | `e41c1f1794a143474c4b2a54f43fb7c628238c40bfce175c0d6957fa1887f07e` |

The [image receipt](zx81-session-display-2026-10-02/image-5a580.json),
[host receipt](zx81-session-display-2026-10-02/host-5a580.json),
[release manifest](zx81-session-display-2026-10-02/release.json),
[target binary receipt](zx81-session-display-2026-10-02/target-binaries.json)
and [selection provenance](zx81-session-display-2026-10-02/fes-zx81.package-selection.provenance-5a580.json)
bind these bytes. The producer manifest records `75d734f40`, while selection
records `5a5805322` and the same functional source digest. This is authenticated
reuse of that original FPGA build, not a fresh compilation from the later commit.

The producer uses the existing ZX81 scoped compiler lock, including its bounded
HPS DDR atom declaration. The sealed producer passes all three clock gates and
ROM/socket policy. Historical Zon X/cart audio evidence does not qualify this
new shell. The interface is `fes.simple-computer` 1.0 with observed HPS DDR and
`fes.video.session-display` 1.0; there is no ABI version bump.

## Software evidence

- Parent `make check` and `make test` passed: 605 Python tests with 39
  environmental/documented skips, plus image/platform packaging fixtures.
- Generated consistency passed for 15 consumers and 32 fixtures. Full shared
  package and runtime tests passed, including protocol, expected-generation,
  memfd frame, input-neutralization and display lifecycle checks.
- FogCast `go test ./...`, `go vet ./...` and focused race suites passed.
  The browser suite passed 312 tests; two Chrome integration tests were skipped
  because Chrome was unavailable. Native ARMv7 tenfoot compiled without CGo.
- ZX81 BASIC, tape, ROM/socket, GP and video simulations passed, alongside the
  new session-display simulation, the 186-test producer suite, MENU regressions,
  and accepted/queued/skid/return-pipeline DDR drain checks. Verilator:
  `5.032 2025-01-01 (Debian 5.032-1+b2)`.
- Two-kit host regressions reject another target's captured session for open,
  close, replace and eject. Failed/ambiguous close keeps UI input focus and
  permits retry. Busy/unavailable media tests preserve the session; they do
  not authorize Stop or recovery programming.
- Hardware smoke exposed a transient frame `Busy` during cassette loading.
  The presenter now clears its error/backoff on a healthy binding under the
  same mutex as the epoch check. Tenfoot clears only the associated display
  notice and preserves tape status and unrelated identity notices. Both
  recovery regressions, races and the ARM build passed at `551b9d1b9`.

At clean `5a5805322`, `make build` completed two independent cold assemblies.
Both image digests were `5032c2d2282da26e79e6f7efc47f9f2a1aa9abca4aad4ca6f19817e82d18ce4a`.
`make verify` passed structural inspection and the vexpress-a9 QEMU packaging
smoke (not FPGA emulation). The
[verification receipt](zx81-session-display-2026-10-02/verification-5a580.json)
records QEMU log SHA256
`2d5083e322d988e72c64b39bf552491ed38d09633c96284c1ae095ccd1f5cd55`.
`make release RELEASE_VERSION=0.2.0-dev.370.2` exported immutable verified bytes.
Its [assembly evidence](zx81-session-display-2026-10-02/release-evidence.json)
keeps `hardware: not-run`; the following dated diagnostic is separate evidence.
Full build/test logs and immutable release files remain in the task worktree's
ignored `out/zx81-session/` and `out/native-integration-dev/appliance/releases/`.

CI first found a stale fixed checksum for the regenerated SMS shared header.
The correction records the current generated header and its FES provenance;
all 92 previous definitions and the published stream contract/exchange fixtures
remain unchanged. The complete 186-test producer suite passed after correction.
These test/provenance-only changes are outside selected production inputs.

The footer distinguishes an active reachable session from readiness for a new
launch, so it retains live arm/eject results. A fresh paired read projects the
foreground game identity before a browser status poll has saved a target record;
enrichment requires the observed package and core generation to match. The
regression reproduced the former nil game ID, and the fixed host passed fresh
launch → first paired read → visible Home controls without a public status read.

## HDMI observations

The authorized target is `dev`, MiSTer Pi `192.168.10.84`, kit ID
`73dc9f5f-1a12-4a95-a820-a9b4e600769a`. Capture used only
`/dev/v4l/by-id/usb-GENKI_ShadowCast_3_KT044001-video-index0` on Powerboat.
One temporary normal host used the existing paired launcher and renewable
target lease. The normal user service remained disabled. Kit 2 was untouched.

The task created its own `ZX81 HDMI controls #370` library entry and bound the
already imported private BASIC ROM. Existing entries were unchanged. Next-start
cassette selection remained empty throughout all live arm/eject operations.
File import used a temporary local copy of the licensed bundled Character
Display tape; no private ROM bytes are published.

The official leased updater installed the final image and confirmed boot
`9805b4e2-db0a-486d-ae37-75a384e0f4ff`, `trial: false` and native idle. The
[confirmed update status](zx81-session-display-2026-10-02/final-update-status.json)
records the exact image and boot. SSH hashes of all three installed executables
matched the final rootfs. These observations use the final verified image and
matching `5a5805322` host, rather than a derived rootfs.

| Operation | Observation and evidence |
| --- | --- |
| Fresh library launch, first paired read, Home | The [launch](zx81-session-display-2026-10-02/final-active.json) and [first paired result](zx81-session-display-2026-10-02/final-first-paired.json) matched session, target, game, flight and full package identity before any public status read. [Workbench](zx81-session-display-2026-10-02/final-first-home.png) and [picker](zx81-session-display-2026-10-02/final-picker.png) appeared on HDMI. |
| Starter cassette, manual BASIC input | The visible picker armed Guess the Number and showed [Tape armed](zx81-session-display-2026-10-02/final-guess-armed.png). Manual `LOAD ""` followed by `LIST` showed [the complete program through line 110](zx81-session-display-2026-10-02/final-program-before.png), ending in `STOP`. |
| Swap without LOAD or reset | Character Display arm showed [Tape armed](zx81-session-display-2026-10-02/final-character-armed.png). Returning with 9 held preserved the [same program pixels](zx81-session-display-2026-10-02/final-program-after-swap-held.png). |
| Visible eject | The room showed [Tape ejected](zx81-session-display-2026-10-02/final-visible-eject.png). Escape returned to the [unchanged listing](zx81-session-display-2026-10-02/final-program-after-eject.png). |
| File import and failed path | Typing `/tmp/370.p` through the path entry armed the licensed Character Display copy and showed [success](zx81-session-display-2026-10-02/final-import-success.png). `/tmp/0.p` produced a [visible missing-file error](zx81-session-display-2026-10-02/final-missing-path.png). Returning preserved the [same listing](zx81-session-display-2026-10-02/final-program-after-import.png). |
| Controller and keyboard focus | Injected USB controller Select opened the [visible room](zx81-session-display-2026-10-02/final-controller-select.png); B returned to the [unchanged listing](zx81-session-display-2026-10-02/final-program-after-controller.png). After held-key release, a [fresh 9 press](zx81-session-display-2026-10-02/final-fresh-key.png) typed into BASIC. |
| Busy and unavailable operations | Eight concurrent idempotent opens returned [one success and seven BUSY responses](zx81-session-display-2026-10-02/final-busy-display.json), retaining the exact session/package. A missing media digest returned [404](zx81-session-display-2026-10-02/final-missing-media.json); a direct unleased display request returned [403](zx81-session-display-2026-10-02/final-unowned-display.json). |
| Stop, relaunch and stale bindings | Explicit [Stop](zx81-session-display-2026-10-02/final-first-stop.json) restored [idle MENU rendering](zx81-session-display-2026-10-02/final-idle-hdmi.png). [Relaunch](zx81-session-display-2026-10-02/final-relaunched.json) advanced core generation from 1 to 2. Old generation-1 [display](zx81-session-display-2026-10-02/final-replaced-stale-display.json) and [eject](zx81-session-display-2026-10-02/final-replaced-stale-eject.json) requests returned 409. Held Select+Start returned [idle](zx81-session-display-2026-10-02/final-chord-session.json); final explicit Stop released ownership. |

The five before/after program captures share SHA256
`a7a0cf58c71409b069153a59b49c6e88a62abacb051853497b98c60c50020d84`.
[Runtime observations and integrity checks](zx81-session-display-2026-10-02/final-integrity.json)
record unchanged package, ROM-linked RBF and core generation 1 through the
live operations. Display generation 2 was distinct from core generation 1;
close revoked it to zero. All recorded underflow counts were zero. The
[own-entry library projection](zx81-session-display-2026-10-02/final-library.json)
kept next-start cassette `No cassette`.

Keyboard input used an ephemeral uinput device. Controller events were written
to the actual USB gamepad's evdev node; the
[device record](zx81-session-display-2026-10-02/final-input-devices.txt)
identifies `081f:e401`, `event0`. Neither method proves physical button presses.
The bare ROM fixture has no optional composition record: the existing room
policy therefore reports `Running: Hardware unavailable` and disables its Stop
button. Live tape controls, Return, host Stop and controller Stop chord passed;
this diagnostic does not claim composed expansion-state or room Stop-button
acceptance.

Earlier diagnostic smoke used verified image
`11627fa4e200e820b95dbe843bcb484ceb56908d90fbac10a3fc8625d0d18090`
at `75d734f40`, boot `8dcfb989-7986-4636-9f77-008cdbfde942`, with host
`6c0974b72`. Guess the Number was loaded with manual `LOAD ""` and listed.
Character Display arm, held-key return, eject and file import each preserved
the same listing PNG SHA256
`a7a0cf58c71409b069153a59b49c6e88a62abacb051853497b98c60c50020d84`.
Actual core generations 1 and 2 remained unchanged within their runs, with
separate nonzero display generations and zero observed underflows. Stale
generation-1 requests were rejected after the generation-2 launch.

Backspace during one early diagnostic correctly invoked the existing computer
Stop shortcut. Captures from that attempt are excluded from eject evidence;
the successful repetition loaded/listed the tape again and performed the
visible eject without entering a delete shortcut.

The UI diagnostic used a disposable copy of verified image
`a8ded2215f80fe36b9c04980ca547b85f2ad08d51dc5413074107fa1b6f3179e`
with only tenfoot replaced from `8ef4f259a`. Its image was
`b4281ca738203b96db216ec56805524448a6bb971c6998e6ed9b6cc700ed0741`;
runtime and agent remained at `551b9d1b9`. Visible arm/eject footer results and
the subsequent host identity fix passed on this derived image. Those results
are diagnostic only and do not establish image reproducibility or release
acceptance. The original verified image was untouched.

## Classification and cleanup

Classification: exact-artifact hardware diagnostic for the final verified
image, package and binaries, with synthetic keyboard/controller events. It is
not complete physical-operator or expansion/audio acceptance.

Final [Stop](zx81-session-display-2026-10-02/final-stop.json) and
[runtime status](zx81-session-display-2026-10-02/final-idle-runtime.json)
confirmed idle. The [final lease](zx81-session-display-2026-10-02/final-released.json)
was free. Both SSH input helpers closed; the ephemeral keyboard and task files
were removed. The temporary host stopped, and the
[normal user service](zx81-session-display-2026-10-02/final-host-services.txt)
remained disabled/inactive. Target native services remained running.
Published observations have [checksums](zx81-session-display-2026-10-02/SHA256SUMS);
private ROM bytes, credentials, rootfs images and image backups remain local.

Staging initially lacked space for another 128 MiB system image. Under an owned
renewable maintenance lease, two unselected and unmounted old images/manifests
were copied to private local archives and verified against their image digests
before removing those exact target files. Factory, current/good, previous,
pending/trial and every loop-backed image were protected. The private verified
archives remain available. After final boot confirmation, this task's superseded
`11627fa4…` image was also archived with its verified manifest under a renewed
maintenance lease. It was unselected and unmounted; current/good, previous,
factory and loop-backed images remained protected. This restored 248.2 MiB free
on FAT for future staging. No household media or library entries were removed.

Physical keyboard/controller button presses remain for an operator. Controlled
transport/clock faults and ambiguous-close recovery were exercised in software
and RTL tests, not by disrupting the kit's clocks or services. The hardware
session used the vacant expansion socket; expansion/audio acceptance of 1.5.0
remains separate. A permanently stopped pixel clock cannot acknowledge a
normal boundary close; input stays focused on the UI until physical close is
confirmed or the existing Stop/fault lifecycle completes.
