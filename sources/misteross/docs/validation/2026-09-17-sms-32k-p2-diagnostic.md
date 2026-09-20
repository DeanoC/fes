# fes.sms P2 32 KiB diagnostic and HoldReset legacy abort

Status: host simulation **GREEN**. Package-only / off kit. Not kit HIL.

## Identity

- Core: `fes.sms`
- Branch: `feat/fes-sms-32k-fixed-map` from `7028b84`
- Bounded diagnostic only. No mapper or retail claim.

## P2-A — Upper-16 KiB CPU execution

Reset enters `0x0000` and jumps to code at `0x4000`. Upper-half code paints
the Graphics I border/checkerboard and a plus-shaped tile whose pattern lives
only above `0x4000`, writes `A5` at `C000`, captures port `DC` at `C001`, and
stores distinctive upper-half byte `0x18` at `C002`.

Split artifacts from `make sms-diagnostic`:

| File | Role | Size | sha256 |
| --- | --- | ---: | --- |
| `build/diagnostics/fes-sms/graphics-i.rom` | Sim regression (signature + HALT) | 17394 | `92ef4fb5cb968c11dd3862b354aec00e2df57285a2d2eefc67760abb4b471eca` |
| `build/diagnostics/fes-sms/graphics-i-32k.rom` | Same, padded to 32 KiB | 32768 | `4ebd8312861d4a13e9dae8c1a14a1bc63eb3043b7a199dd50c91e4d6534e6e4f` |
| `build/diagnostics/fes-sms/graphics-i-hil.rom` | HIL: visible plus tile + `DC`/`DD` poll loop, no forever HALT | 17466 | `8226290bebcc86438ce070f0500b0a0289552ef49ca72c0ce975fc79e68f988e` |
| `build/diagnostics/fes-sms/graphics-i-hil-32k.rom` | HIL image padded to 32 KiB | 32768 | `411c33162658bf0bba55f5745565ee023c6bb6f5190a57f9a3b3ea5e2c484835` |

`--pad-to` maximum is 32768. `make sim-fes-sms` and `make sim-fes-sms-oss`
run the HALT image to completion on the CPU (not reset-only peeks) and check
the RAM signature plus that `cpu_addr_debug` entered `0x4000–0x7fff`.

Kit HIL is out of scope until Deano GO. The HIL ROM is for a later
display+USB Stop/relaunch check.

## P2-B — HoldReset aborts incomplete legacy blob

When `ENABLE_MEDIA_STREAM=1`, HoldReset still clears `media_open`/`media_ptr`
if a legacy blob 1.0 transfer is open, so a following data word is
`invalid state` (`0xf5c00004`), not success. Stream header/transfer staging
is left in place across Hold. Coleco/SG-1000 keep `ENABLE_MEDIA_STREAM=0` and
the original always-clear HoldReset path.

Reproducer: `/Users/clawzai/tmp/fes-stream-review-NIbcxK/legacy_hold.cpp`.
`make sim-fes-sms` includes that sequence plus stream-staging preservation
and abort-then-stream interlock.

## Evidence

- `python3 -m unittest tests.test_build_fes_sms`
- `make sim-fes-sms`
- `make sim-fes-sms-oss`
