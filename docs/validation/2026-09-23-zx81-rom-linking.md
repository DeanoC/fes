# ZX81 ROM and expansion linking diagnostic — 2026-09-23

This is a development-only, temporary exact-kit diagnostic of the uncommitted
ROM-linking work rebased onto FES `d1743b0c`. It is not native-image, release,
or persistent hardware acceptance. No block device was written.

The producer source came from development snapshot
`121727d0ea833823146ad30ba1bfae3475ae3dcb` (base `d1743b0c`, captured
tree `c607672e9346de39329234317aeffed080959ec6`). The ZX81 OSS producer
sealed format-3 package
`40b0c126e3a687555cb10ce71a12d655d4e0e5f5fca674bf47da3c904082509c`
after meeting timing with seed 7 and placement weight 300. Its RAM validation
cart sealed as
`e362b4ab4cdff6820df61d2cd31496a0b39578f04fa25e7474d4a34366321ab5`.
The temporary target agent included the separate 60-second core-package
observation budget; the raw-RBF diagnostic budget stayed at 10 seconds.

On the designated MiSTer Pi, under the target agent's kit lease, the ARMv7 Go
linker produced the same plain RBF SHA-256 as independent Mistral INIT
decompile/compile/readback:
`a5238b33820d27c38229ed427f2f68d50fce70586daba839bed4a6bd0cfdb82c`.
Five ARM link runs took 3.23–3.26 seconds each. The expansion followed by ROM
linking produced Mistral's
`66dee6c3fef6f0a46189b1039e56501df4fd77ad4e8e8b1032ac81f64a5a23b2`.
The host and target reported the sealed package, map, 8 KiB ROM source,
expansion, and programmed identities exactly.

The private-host library sequence passed: plain 1 KiB launch (33.53 s),
expanded 16 KiB launch (39.44 s), expanded relaunch (39.48 s), and expanded
launch after host restart. Keyboard input and Stop succeeded each time. HDMI
capture showed BASIC RAMTOP `68` without the cart and `128` with it, with the
`0/0` tape indicator. The ROM and expansion selections survived host restart;
the active programmed hash remained the expanded Mistral hash.

The temporary agent and runtime were removed. Their original executable
digests and one-process-per-service state were checked against the baseline;
the kit remained on boot `483d4b1e-d049-4ff9-90de-7191344e4aeb` and was
ready, idle, and unleased. Private host configuration was removed. Local
diagnostic artifacts and receipts are in ignored
`out/rom-main-full-hardware/` of the integration worktree.

The first attempt at this exact package stopped before launch because the
diagnostic oracle supplied logical ROM words to a helper requiring encoded
Mistral RAM mux words. Correcting that diagnostic input made the Mistral and
Go outputs match byte for byte; no production code changed for that mismatch.
An earlier sealed shell also passed the same full sequence with the current
agent/runtime. Neither diagnostic implies acceptance of a flashable image.

The rebased integration source was frozen separately as development snapshot
`821defd56ced90b279eb868b38e4849bf0f5b3b5`; `make host` passed there.
The full FogCast Go suite, expansion Go suite, 298 browser tests, libmister-runtime
host unit/archive audit, the full FES Python suite (569 tests, 37 skipped), and
`scripts/generate.py --check` passed. FES `make check` intentionally rejects
both dirty feature trees and development snapshots; it awaits a reviewed,
committed source selection. Cold image/release checks were not claimed.

The same integration snapshot completed `make dev` with
`FES_CACHE_ROOT=/home/deano/fes/out/cache` after its three factory packages
sealed. Structural validation accepted the 64 MiB diagnostic `linux.img`
(SHA-256 `7368a8b7aa358853282508f6707fc456e2bc69ee8f8f719783fe138ff47b8384`).
Its closed receipt includes ZX81 `rom-map.json` with SHA-256
`ae2b9b311c8b73a61575405c7a2f5b6b32acf9281ea2aa798750066adea8fdd5`
and base RBF with SHA-256
`d5e4bec62ec03793ac84e21f72c61f6829f60ce3dd0bd932b35f3129d4e03688`.
The image's package ID is
`c1b06b9aa0227a3b357a04c5116768cb354faef4341721bdd694c9aed2cbe8df`,
different from the kit-tested package because its source-provenance revision
differs. This image was not deployed or hardware accepted.

After selecting the feature source as a commit, the FES `make check` consistency
gate passed: package YAML, 12 generated consumers, and 24 fixture copies match.
