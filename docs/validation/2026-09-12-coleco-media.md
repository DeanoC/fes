# Coleco cartridge delivery and graphics diagnostic

Follow-up to [the initial lifecycle diagnostic](2026-09-12-coleco-bringup.md).
This work remains a reduced BIOS-free Coleco-compatible development slice, not
retail-game or full TMS9918 compatibility.

## Source changes

The integration base is `59419fb45bb7c74201239a006f86b0128351984a`.

- FogCast `c761cff0d9e7878d90eb3dee24ba96010acb46de`, based on
  `12608992afd003d54cd3019ea4dd7c373bf6ff31`: `core-media PATH` calls the
  running host's `POST /api/v1/session/development-media`, then the existing
  target lease and coordinator, private staging and runtime `load_media`.
  Uploads bind session, target, package and generation. Keyboard delivery is
  serialized with media, and held-key restoration is package/generation-bound.
- libmister-runtime `2629c6e1a896663b3e06688462624c3fac67ba67`, based on
  `a729acc593ec772fa5ecd5f802e2dee9758bd4dc`: bounded filename-independent
  1..16384-byte regular-file snapshot, hold/transfer/commit/release ordering,
  and no release after a failed transfer. Final symlinks and FIFOs are rejected.
- misteross `5f239c92b4e787db173c5af710029793b4144d26`, based on
  `a5b208539aac01de4c5ea9484cacd6bca175ccf5`: CPU/VDP reset stays held until the
  committed cartridge copy finishes, including the OSS final write. Held TV80
  OUT cycles now deliver one VDP byte rather than duplicate writes.
- Final misteross selection `69c58237c0a1cc933140f95ebfff32626c9f455a`
  follows `5f239c9` with the Quartus RAM-latency fixes and vendor-model
  regression probes described below.

The reset/copy and duplicate-OUT bugs are functional fixes, not compiler
workarounds. The existing registered M10K wrappers, three coherent VDP RAM
copies, media latency handling, explicit framebuffer RAM, initialization
formats and supported constraints remain documented for the Yosys/nextpnr/
Mistral owner in misteross `cores/fes-coleco/README.md` and
`docs/architecture.md`. No shared ABI or wire definition changed.

## Open cartridge and software evidence

`make coleco-diagnostic` in the selected misteross produces original
MIT-licensed Z80 code, with no downloaded cartridge or proprietary BIOS.
The 989-byte ROM SHA-256 is
`9f9fa280b141e0538a571bb66f1e2447f691eecb853ae05547720f2ccc20783c`;
the 16384-byte padded variant is
`846e85b33edbe907f1710ecafd691b91c7b2fede3b1b1132493aea708ba74a16`.
The ROM initializes all VRAM and draws a centered green border around
alternating green/orange squares with black gaps.

Default and OSS board simulations transfer the exact 989 → 16384 → 989-byte
cartridges through GP, immediately release reset, verify copied bytes and
one-write-per-OUT behavior, and check complete CPU-generated HDMI frames:
921600 active pixels and 122688 nonblack pixels. The generator and compiler
recipe unit tests pass (13 tests). The runtime full `make test`, focused
artifact/GP tests, ARM cross-build and independent source reviews passed.
The affected FogCast suites pass, including CLI→host→target→Unix-socket
coverage, wrong-target/generation rejection and keyboard/transfer races.
The worker also ran race-enabled suites and vet. Parent `make check` passes:
14 generated consumers, 11 fixture copies and four copied source pins match.

## Initial clean FPGA builds (`5f239c9`)

Both packages were built from clean selected misteross `5f239c9` for
`5CSEBA6U23I7`. Archives are under integration
`sources/misteross/build/packages/`.

| Lane | Package ID | RBF SHA-256 | Bytes |
| --- | --- | --- | --- |
| OSS | `42b34f9a0e2056860c5838bc530646cae40a4ad704110b9c8bf2b8b85cbf97b6` | `ee1f4f5e1daa20520200abdd4a52053a85238093a9e3cf53ee23788576edf662` | 2482681 |
| Quartus | `71f23c8626765cd84548d62753dba48150c1e22d671eabc093d7cc817f634e42` | `fa0bd97c7ca1f820149176b6771178c95ab3aca9fce4d480b73d666e40bef446` | 2292928 |

OSS build ID `8b473a8f3d5cb26ed711b2d6c1d98cda`: 61.4062 MHz system and
87.8117 MHz pixel against 52/74.25 MHz, no unrouted nets, 133 M10Ks,
2597 combinational cells and 638 FFs. Tool revisions remain Yosys
`da6373c0d7565f36036051efc7895fb0d9ac13c3`, nextpnr
`fd862a2c59db7f0406e32831f2e57b3cfe034251`, Mistral
`b28e30a36b5139aaed5a5d361a30b542e6b7c758`; seed 7, router1, timing rip-up.

Quartus 17.0.2 build ID `4a0d278b451eaffce3314b9c495aa3e5`: setup 3.105 ns,
hold 0.167 ns, recovery 12.500 ns, removal 1.001 ns, pulse 0.961 ns; all
reported TNS zero. These gates do not claim fully constrained external I/O.

## Hardware classification

The first disposable image
`6c3420176541a140f762579a9adf1ff189381fd6f6c3a8ba886f478640029f37`
booted but its statically linked diagnostic runtime aborted during startup.
An ARM `std::thread` create/join probe reproduced an abort with `-static` and
passed with normal dynamic linking on the same kit and toolchain. This is a
diagnostic link-mode finding, not an FPGA compiler workaround or a media-source
regression. The first image was never confirmed; watchdog fallback restored
`d1733d3f...d98d760`, boot `fdd69c10-5573-46b8-9bfc-3a26e8a2c1e2`, with raw
idle ready and no pending/corrupt update state. Factory/good/previous images
were retained.

The second diagnostic image uses the runtime's normal dynamic linkage and
successfully reached raw native idle and update confirmation, boot
`3b261e31-91d7-4ee7-9dd4-39cb13eacb4f`. Its SHA-256 is
`77dccfd1f6ff2ea013ff186e31d6030bd5cdd566d1bc22985d05652fa3a2dd3e`.
The runtime binary SHA-256 is
`2932d86bdf0b97c609a9338e3ff44ff92f06c85da2719216ba8111af816b0ef3`;
agent `7e6c5d14b13b0b0a2393a272ea42ae70a6126c268fcbddb75080993c580b99a1`;
host `a1ace56e6feaf145bff7eaa98eded182ea671544764c7ae106e25c8e2c520013`.
Its manifest identifies source-selection FES `156c289`; unchanged base packages,
kernel, bootstrap and kit binary come from the preserved previous image. This
is a diagnostic derivative, not a reproducible cold image or release acceptance.

### Graphics results for `5f239c9`

Both sealed packages loaded through the separate host at `127.0.0.1:8797`,
which acquired and renewed the existing target lease on `192.168.10.84:8182`.
The original host at `127.0.0.1:8787` was preserved. Every response matched the
expected package, build ID and `fes.simple-computer` interfaces; media stayed
volatile. No direct runtime-socket or JTAG programming was used.

- OSS generation 1: 989 → 16384 → 989-byte uploads all succeeded. All three
  1280×720 PNG captures are byte-identical, SHA-256
  `140468bf0eb64422b6f559d924689e51e139ad891131de37a101dd353b0b1385`.
  The expected green border and green/orange square pattern is visible.
  A stale-generation upload returned HTTP 409; the subsequent valid reload
  retained generation 1 and the same image. Stop returned idle.
- Quartus generation 2: both 989 and 16384-byte uploads succeeded, but graphics
  **failed**: orange border, shifted geometry and green rather than alternating
  squares. Both captures are identical, SHA-256
  `4ea362fa4b18c048706283fa0294bc6475e68651cabee716a9c2ae21c3f531fb`.
  Stop returned idle and the lease was free. Compiler success is not graphics
  acceptance; the follow-up below resolves this mismatch.

Capture uses uncompressed YUYV 1280×720 at 25 fps and discards the first 25
frames. There are no MJPEG startup/decode errors. Comparing the OSS capture
directly to ideal RGB classes yields 98.6727% whole-frame and 93.7785% logical
agreement. Inspection localizes all disagreements to horizontally filtered
YUYV color transitions (for example black becomes `(0,61,0)` beside green).
Excluding two pixels around expected horizontal color transitions, without
translation/scaling or vertical-edge exclusion, gives 100% agreement over
850528 stable frame pixels, including 129952 logical-region pixels. This is
color/geometry diagnostic evidence, not bit-exact RGB or gameplay acceptance.
The same check rejects the launcher frame and the incorrect Quartus pattern.

Local evidence is retained under `out/dev/fes-coleco/evidence/`, including raw
API results, capture logs, images, the comparison script and both diagnostic
image candidates. The original image and all appliance recovery images remain
retained. Hardware input, audio, commercial cartridges and full system
compatibility are not established by this diagnostic.

## Quartus latency correction and final builds

`69c5823` corrects two wrapper assumptions, not a demonstrated Yosys/nextpnr/
Mistral backend defect. Quartus `altsyncram` registers the media read address
even with unregistered output, so cartridge copy now uses the same one-cycle
priming, delayed write address and final flush as OSS. The framebuffer already
registers its read address; removing its additional output register restores
the one-cycle video latency. OSS media behavior remains unchanged.

The optional `sim-fes-coleco-quartus` target uses Icarus and the installed
Quartus `altera_mf.v` (SHA-256
`e7bc6f0200f8236986c4b255a4ce7937596946bdb646a93551057edc1e08ca69`).
Verilator rejected constructs in this vendor model; no vendor source is
copied into the repository. The probes reproduce both failures against
`5f239c9` and pass after the correction: framebuffer address/output latency
and exact 1, 3, 989, 16384, then 989-byte GP cartridge copies with immediate
RELEASE. Default and OSS full-board simulations also pass. This is focused
vendor-model coverage, not a claim of a full vendor-model CPU/HDMI frame test.
Independent source review found no remaining actionable issue.

Both final builds use clean selected `69c5823`, the same device and toolchain
revisions as above, and pass their build/timing gates.

| Lane | Package ID | RBF SHA-256 | Bytes |
| --- | --- | --- | --- |
| OSS | `11f79d4b74216c4ee741e65931b1c37219691545e462e032a5e51fbd9974a134` | `4efdfc0670e757792314ee75ace1c724aa24fafcec7d443e94c9bb79a6c75382` | 2489507 |
| Quartus | `5c9b705cf9e4b2819caa09eecf06e0bd9dee36047ac0fc8e034707bccfb6648f` | `821bc900d619171b13d52f66a7471028c8e5dc77414e5b469428e1d89d482138` | 2315048 |

OSS build ID `1bb56479e12015350188cef89b9b3999`: 59.0772 MHz system,
91.5499 MHz pixel, no unrouted nets, 133 M10Ks, 2590 combinational cells and
638 FFs. Quartus build ID `dc636ae0cf736466a2519d9efe1942ac`: setup 3.566 ns,
hold 0.150 ns, recovery 12.603 ns, removal 1.169 ns, pulse 0.961 ns, all
reported TNS zero. The same external-I/O constraint caveat applies.

### Final exact-artifact hardware results

On the same confirmed diagnostic image and boot, Quartus generation 3 and OSS
generation 4 advertised their exact final package/build IDs and the expected
keyboard, media and fixed-video interfaces. Each passed 989 → 16384 → 989-byte
uploads without changing generation; OSS also passed an initial 989-byte upload
before that captured sequence. All six final captures are byte-identical,
including across compiler lanes, SHA-256
`140468bf0eb64422b6f559d924689e51e139ad891131de37a101dd353b0b1385`.
Each passes the documented stable-interior color/geometry comparison with
100% agreement. The final comparison also rejects the preserved launcher frame.
Both Stop operations returned idle through the owning host session.

Evidence files are `final-{quartus,oss}-*.json`, `final-*.png`, capture logs,
and `final-{quartus,oss}-summary.json` in the evidence directory. The previous
Quartus failure remains preserved rather than overwritten. This establishes
CPU-driven open-cartridge graphics, bounded media loading/reloading and lifecycle
behavior for the exact two RBFs, not retail compatibility or release-image
acceptance. The next functional milestone is controller delivery with an
interactive open diagnostic, followed by broader VDP behavior and audio.

### Restoration and integration handoff

The leased `fes-update --action rollback` completed successfully, confirmed
original image `d1733d3fdcc97c4669f07576c3f449ba72f05e6ed77fbcf3b0a029830d98d760`
on boot `f6c00d7f-be5e-4c7a-8280-69b4270b580c`, and reported raw idle ready,
trial false, no pending image and no corrupt state. The diagnostic image remains
retained as previous; no image, factory, kernel or bootstrap was deleted.
The separate diagnostic host on port 8797 was terminated; the original 8787
host was preserved. After rollback another operator, `misteross@hps-location`,
claimed the kit for `850 HPS I2C QSF HPS_LOCATION`. The original host reported
that owner as busy on the restored boot; we did not stop or displace it.
See `rollback.log`, `final-lease.json` and `restored-host-status.json`.

The integration branch selects the final three component revisions above;
mister-packages remains `4e36ca9e4832588d6b8f1f9fae6cc68eafba8aec` and shared
contracts are unchanged. The main checkout and its pins are not advanced.
Final `make check` and `git diff --check` pass. Integration remains a branch
handoff: no push, main-branch merge or cold release acceptance is implied.

Final regression run: all 209 parent Python tests passed (36 delegated skips;
the real-container drivers ran their suites). The subsequent image shell suite
initially failed because `native-runtime-inputs_test.sh` still asserted the
old runtime commit `a729acc`. Updating only that literal to selected `2629c6e`
made the complete `make -C image test FOGCAST_DIR=.../sources/FogCast` pass.
No image recipe or production code changed for this test correction. The 13
focused Coleco Python tests were also rerun successfully at final `69c5823`.
Logs: `final-parent-tests.log`, `parent-image-test-trace.log` and
`final-image-tests.log`. The initial aggregate `make test` exit was nonzero;
the Python and corrected image suites are reported separately, not hidden as
an uninterrupted successful aggregate run.
