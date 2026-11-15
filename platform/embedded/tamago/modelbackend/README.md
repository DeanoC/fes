# Tenfoot software-model sessions

This adapter connects the real tenfoot App, renderer and existing hostclient
Play/Stop requests to retained format-2 packages, ROM linking and the fabric lifecycle model. It
runs on ordinary Go and native AArch64 TamaGo. It never constructs the physical
runtime, programs an FPGA or emulates a game CPU. Its catalog contains synthetic
ROM-link fixtures and is deliberately separate from the real network-host mode.
`NewWithEndpoint` can inject the [contained native firmware/FPGA endpoint](../nativefabric/README.md)
into that same actor for model-only integration tests. The caller retains the
endpoint across failures so uncertain transfer reservations remain owned.
The injected path checks its explicit identity profile and admitted programmed
digest before it retires the old owner; model-only package compatibility does
not qualify a physical Nano producer.

`Backend` implements `http.RoundTripper`; tenfoot uses its existing in-process
transport boundary. Unsupported endpoints fail locally rather than forwarding
to a host or kit. The normal Linux launcher and its host/target APIs are unchanged.
The model transport implements only the catalog/session subset needed here,
not the complete host service, game search, media picker or package installer.

A single cooperative actor owns the controller. HTTP workers submit bounded
requests and wait for a reply or cancellation. One preparation worker reads the
selected archive and ROMs, checks the selected source digests and retains the
package. Format 2 rejects a selected link receipt, composition, ROM sources or parts;
formats 3/4 require the selected receipt and return an opaque linked object.
On TamaGo, its context yields at existing parser/linker cancellation
checkpoints. The actor independently checks package and applicable receipt
admission before retiring any owner. Its event callback must return promptly
and must not call blocking actor methods.

Part archives are read against selected file digests and asset IDs, then retained
and composed with the ROMs. The canonical part tuple and pre-ROM overlay identity
remain separate from the final programmed digest. An active session publishes
its parts composition only after release.

The selected receipt is generated on the host with the same retained-link API,
then checked on the guest; the guest does not trust caller-supplied programmed
bytes. Compatibility still comes from the existing C++ oracle's exact-package
model policy. It remains a trusted runner-owned diagnostic input, not physical
runtime admission or an authenticated receipt.

Before each tenfoot draw, the platform obtains an opaque frame grant. It presents
the private software framebuffer only with that grant and confirms completion
after synchronous RAMFB copying. During transition it can draw privately, but
cannot scan out without a grant. The actor waits for the outstanding frame
before display drain/containment/programming. Frame grants and input requests
carry generation ownership; delayed events reject without affecting the owner.
These are software ownership checks, not physical DMA revocation proofs.

Session publication follows model release: an active response includes the
verified package and fresh generation. Stop carries the displayed session,
package, generation, target and flight identity; a stale Stop cannot select a
replacement. Save precedes display drain and replacement. A known failed save
preserves the core/package generation with input retired, and the existing UI
can retry Back. Programming or identity failure completes bounded idle recovery
before the launch fails. Cancellation before admission preserves the current
owner; observed cancellation after admission fails a contained target phase
and recovers before release. A release that already committed is a live session
and must be stopped with its current identity.

The actor remains alive while `App.Stop` finishes its own asynchronous cleanup,
then `Backend.Close` drains the model. Close refuses an outstanding renderer
frame rather than stealing it. New grants and input are blocked once close is
accepted. No game progress is actually saved: Save/Program/Identify are model
endpoint completions. Sources and receipts are volatile; no durable session or
package installation is created.

Run `run.py play` as described in the [native guide](../README.md#tenfoot-play-and-return-through-the-model).
It drives real QEMU xHCI USB keys and captures the rendered frames. The eleven
cases include normal Play/Back, wrong ROM, wrong receipt, interrupted preparation,
identity/programming mismatch, a failed save followed by retry, a composed
CPU/video launch, and changed/missing/incompatible part rejections.
Stale input/frame events and tenfoot's own Quit are also checked. The complete
FAT32 disk must retain its original hash. The fixture producer needs an installed
standard Go toolchain as well as the existing C++ compiler and TamaGo tools.

Host tests use the same fixture directory:

```sh
FES_MODEL_FIXTURES="$PWD/out/native-tenfoot-play/package-fixtures" \
  GOWORK=off go -C platform/embedded/tamago test -race ./modelbackend
```

They use public App commands and `RunPlatform`, not direct App state edits, and
check both idle and running frame ownership on close. Additional cases hold a
frame across admission, submit a stale Stop after relaunch, cancel a real HTTP
request during preparation, and cancel after admission but before release.
Linux-only tenfoot test dependencies are recorded in this module; they are not
selected into the bare-metal image.

The DE25 sealed-diagnostic verifier additionally drives the real tenfoot
launch/return loop using its format-2 Nano package, explicit model-only policy,
CPU hook adapter and simulated TF-A/RTL endpoint. Identity mismatch and missing
policy/receipt errors are checked before release. The HPS-first diagnostic
also rejects a missing or mismatched simulated boot handoff before retirement.
This is host-model evidence;
it does not enable a physical Nano programming profile.

A native presenter error returns before `FinishFrame`, leaving the outstanding
model preparation held. Close refuses that grant. A completed CPU copy in the
boot-owned scanout backend is still not a physical vblank or DMA drain; the
model's software completions cannot authorize FPGA reconfiguration.
