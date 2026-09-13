# ColecoVision compiler and native lifecycle diagnostic

The FES integration branch selects misteross
`a5b208539aac01de4c5ea9484cacd6bca175ccf5` from parent base
`31e45c0822e82fbd950a4c6dc6d9beb0c22c8230`. Implementation is in
`b60e1aaccc5ec0c6f93d654c5cb2e6caf9f3e873`; the follow-up records its evidence.
The parent pin commit is `cff77c5`. No shared contracts or image package
selection were changed. Coleco remains a development package.

## Final producer artifacts

Both clean recipes passed for `a5b208539aac01de4c5ea9484cacd6bca175ccf5`.
Artifacts reside in `sources/misteross/build/packages/` in the integration
worktree `out/dev/fes-coleco/fes-integration`.

| Lane | Package ID | RBF SHA-256 | RBF bytes |
| --- | --- | --- | --- |
| OSS | `910f666aa60e54af08b5b55f4de934f84091350ba95da2ff0a39b160379a3f26` | `8fa6ada740fb2370d4bf914073a048fe20fcb7791e74220a98602dcb9ec44568` | 2485653 |
| Quartus | `e23ec7972131b9b4e3ee0eb507a1f39452982386fad6429b98550748797ff20c` | `fae982f2e652f7ef45aedf1e96369cafce987a50139d4149d7d008b86eb14d1e` | 2298188 |

OSS achieved 63.7918 MHz system and 89.2299 MHz pixel against 52/74.25 MHz,
with no unrouted nets and 133 M10Ks (85 TDP plus 48 regular at synthesis).
Its build ID is `d0155fd3a302222149b86328497ff9be`.
Quartus 17.0.2 completed with 0 errors and 35 warnings. Required worst-case
slacks were setup 3.683 ns, hold 0.166 ns, recovery 15.130 ns, removal
0.584 ns and pulse width 0.961 ns. These checks do not establish fully
constrained external I/O or physical video acceptance.

Parent `make check`, `make test` (209 tests, 36 skips plus image shell tests),
Coleco default/OSS Verilator simulations and 10 recipe unit tests passed.

## Hardware diagnostic

The final OSS archive above was loaded through the existing host API at
`127.0.0.1:8787`, using its target-agent lease on the designated
`192.168.10.84:8182` kit. The target confirmed that exact package and build ID,
generation 2, ABI `fes.simple-computer@1.0`, and keyboard, media blob and fixed
720p60 interfaces. Persistence was volatile. Host API Stop completed and
subsequent observations confirmed idle and a free kit lease, without reboot.

The running host reported revision `560364a2ca61e848084a3f683ab89eac5ddf3de6`;
the target reported runtime `a729acc593ec772fa5ecd5f802e2dee9758bd4dc` and image
`d1733d3fdcc97c4669f07576c3f449ba72f05e6ed77fbcf3b0a029830d98d760`.
This is an exact-package lifecycle diagnostic on the existing appliance,
not acceptance of a newly assembled parent image.

No cartridge was delivered. The previous producer's capture was black and
had MJPEG decode warnings; it establishes neither a rendering failure nor
working gameplay. No capture or cartridge/input acceptance is claimed for
this final archive. The Quartus archive has compiler evidence only.

## Next integration step

The selected runtime exposes `load_media` and `FesGpCoreDriver::LoadMedia`,
but the selected FogCast tree has no `load_media`/`LoadMedia` client or agent
route. Add bounded cartridge delivery through FogCast's existing leased
session path, including the reset/commit/release sequence required by Coleco.
Then deliver an open diagnostic cartridge that initializes the VDP and draws
a known pattern, and compare the final OSS and Quartus packages on HDMI.
Do not bypass the target lease with direct runtime-socket programming.

Compiler workarounds remain documented in the selected misteross
`cores/fes-coleco/README.md` and `docs/architecture.md`: registered M10K RAM,
three coherent VDP copies, media latency handling, explicit framebuffer RAM,
initialization formats, TV80 frontend, I2C placement and supported constraints.
