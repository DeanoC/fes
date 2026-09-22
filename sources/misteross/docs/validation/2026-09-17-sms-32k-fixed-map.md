# fes.sms 32 KiB fixed map and required blob-stream 1.0

Status: host simulation and package-recipe **GREEN**. Not kit HIL.

## Identity

- Core: `fes.sms` (FogCast `SystemSMS` / library SMS)
- Branch: `feat/fes-sms-32k-fixed-map` from `c43ef45`
- mister-packages pin: `c8c8dfd1fcb0503ac92baf6b365d93e8d26a0854`
  (Herd PR https://github.com/DeanoC/mister-packages/pull/9 — not dual merge-asked)
- Canonical contract: `docs/contracts/media-stream-1.0.md`
  sha256 `aed93f66983d6edf09d4f1ea926027ebc700e95af266ae3872a2840aec9a79cb`
- Consumer fixtures: `cores/fes-sms/generated/stream-exchanges.json`
  sha256 `3b186ea15c6cbed8c682f09c7a17824a03afaefae12d264ae17282461d8e854f`
- Additive ABI header: `cores/*/generated/fes_simple_computer.vh`
  sha256 `9e835b325085596908b2e28ed695e0841f0910a39d70a123ef4177f8bc2e2860`

## What the slice does

The SMS cartridge map is `0x0000–0x7fff`. `0x8000–0xbfff` stays unmapped and
reads `0xff`. Legacy blob 1.0 opcodes 4..6 still admit 1–16384 bytes. Stream
1.0 opcodes 7..12 admit 1–32768 bytes, advertise Info min=1 / max=32768 /
chunk=512, and use CRC-32/IEEE. After a successful commit of length N, every
mapped address N..0x7fff reads `0xff`, including when a shorter image replaces
a longer non-`0xff` image. SMS format-2 recipes declare
`fes.media.blob-stream` 1.0 required next to blob 1.0, keyboard 1.0 and
fixed-video 1.0.

Coleco and SG-1000 keep `ENABLE_MEDIA_STREAM=0` and the original 16 KiB mailbox
ports. Wire opcodes are the published constants; this tree does not invent
header semantics.

## Evidence

- `make sim-fes-sms` consumes the published stream fixture, a 32768-byte
  success path, advertised-limit rejection, a full 256-word chunk, and the
  machine long-then-short `0xff` tail.
- `make sim-fes-sms-oss` repeats those checks on the registered media/VDP
  branches.
- `python3 -m unittest tests.test_build_fes_sms` checks the pin hashes, the
  required package interface, and the 32 KiB map.
- Kit HIL is out of scope (`kit_hil=no`). Runtime/host remain Herd.

A new sealed RBF is not produced in this job. The Quartus and OSS producers
will emit the required stream interface on the next clean seal.

The 32 KiB CPU-execution diagnostic and HoldReset legacy-abort follow-on live
in `docs/validation/2026-09-17-sms-32k-p2-diagnostic.md`.
