# Coleco MegaCart two-ROM kit diagnostic

On 2026-09-26, the designated MiSTer Pi passed normal FogCast library launches
of an original synthetic 8 KiB BIOS and 128 KiB MegaCart cartridge, with and
without the exact-shell SGM expansion. Both variants passed Stop/relaunch;
SGM also passed after restarting the private host with its retained selections.
This is exact-artifact hardware diagnostic acceptance. The factory format-2
recipe remains selected; no retail mapper, proprietary BIOS, appliance image,
controller, audio, or persistence qualification is added.

## Selected artifacts

The FES base is `d7545210de07f0c46c64931d1d3d05c38110fe1f`.
Clean-source FPGA producers selected implementation commit
`097577067e3c0f549cb9045877963143185f0273`. The tested host, target agent,
kit service and optimized runtime were built from
`40c30b09b6af0f5b9c4af444ff519bc0e56220bf`.

- Shell package: `198244ecf48081418c41809f175b730b091d0103e551a38e83c90a6b350efe5a`.
- Shell RBF: `382a10e0b140c98c04a565ede5581f39af2b6af11787a8045920385e1e4e562b`.
- SGM expansion: `3eec177cfc1457655249990d0c187f300e21770917b79b4868572f750fc21352`.
- Map: `b97dbf2265abd50798774c42bf4719d68348df3b8f4e3fc3e4340ed48b5d1240`.
- Synthetic BIOS: `f108c82da759bd9ab299e646feb5131a2b1f8af66b11ed843940249eb4269f03`.
- Plain programmed RBF: `97fe379bad063df25e47e705ecc3c3bcc03111b4620fb237dab7b81584494218`, 2,686,561 bytes.
- SGM programmed RBF: `b93470630d99803c9c174bfa64d290bcebfd7dab38679eadf2e40b8dafb7d545`, 2,700,570 bytes.

The [machine-readable evidence](coleco-megacart-2026-09-26/evidence.json)
records source and executable hashes, both cartridge hashes, every returned
ROM receipt, SGM composition, frame checks, and restoration results.

Authenticated nextpnr routing on GPU 0 closed system/pixel/audio at
53.124 / 92.022 / 169.952 MHz against 52.225 / 74.250 / 12.288 MHz constraints.
The shell's reserved socket was vacant. The matching SGM changed 38,434 CRAM
bits inside its socket and zero outside. The independent Python and Go linkers
produced identical plain and SGM bitstreams for the sealed golden sources;
this remained true after the staging optimization.

## Observations

The probe checks the BIOS sentinel, reset bank zero, all eight bank selectors
and their selecting-read return bytes, banked reads, fixed final bank, and
writes that must not select a bank. The SGM variant additionally enables upper
RAM, writes and reads `0x2000`, then disables the overlay. Failures loop before
video setup; successful checks jump to the original Graphics I checkerboard.
The integrated T80/SGM simulation reached the pass marker for both exact ROMs.

All five physical launches returned active with the exact ordered BIOS/cart
receipts and expected optional composition. Every captured frame matched all
664 tile-center, border and outside reference samples. Missing both ROMs and
missing BIOS were rejected with HTTP 400. Every Stop returned idle.

- [Plain MegaCart pass frame](coleco-megacart-2026-09-26/plain-launch-0.png).
- [SGM pass frame after host restart](coleco-megacart-2026-09-26/sgm-after-host-restart.png).

The final run used the normal 30-second host request timeout, 60-second upload
timeout and unchanged 60-second target activation budget. Plain activation
requests took about 43 seconds; the recorded final SGM requests took about
48 seconds. No timeout override was required.

Earlier attempts failed before FPGA programming. Unoptimized native hashing
took 12.543 seconds for 32 MiB on the kit versus 1.568 seconds with `-O2`, with
identical digests. Commit `665ae387` enables optimized default builds and fixes
a missing production-adapter forwarding method for two-source attachment.
Its regression failed before the fix and passed afterward. Commit `40c30b09`
reduces repeated map parsing within two-ROM staging from four parses to two,
without caching validation across requests. Receipt comparison, package
publication and restart adoption retain independent validation.

## Restoration and validation

Kit `73dc9f5f-1a12-4a95-a820-a9b4e600769a` retained boot ID
`948d6338-1a12-4476-a435-d0ea7d4a6c7e` and installed image
`4352d991a2bba5a2a09e4fb09f0a0225d7390896ae2aefb71feb3031ebc60e35`.
The lease-controlled operator temporarily bound verified candidate binaries,
then restored original on-disk and live executable hashes for all three target
services. Target configuration and boot were unchanged. The target finished
ready, idle and free; private containers, staged binaries and credentials were
removed. This did not install a new system image.

The original 32 changed-file lanes passed before source sealing. Follow-up
validation passed the full native runtime suite and build guards, Go race
suites for corepackage/runtime client/agent/session/HTTP, generated-consumer
checks, parent consistency, the sealed Python/Go comparison, and independent
review of the staging validation refactor. Retail images remain private and
untested in this slice; Time Pilot's actual mapper remains unverified.
