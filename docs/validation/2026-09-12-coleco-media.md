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

## Clean FPGA builds

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

The new packages have compiler and simulation evidence at this point; their
cartridge/HDMI hardware diagnostic has not yet run. The prior installed image
`d1733d3fdcc97c4669f07576c3f449ba72f05e6ed77fbcf3b0a029830d98d760`
has been copied and byte-verified locally for preparation of a disposable
diagnostic derivative. Neither this preparation nor the prior package's load/
Stop test establishes graphics acceptance for these new artifacts.
