# Tenfoot software-model backend (`modelbackend/`)

`modelbackend.Backend` is the **QEMU / host off-board counterpart** to
[nativefabric](../nativefabric/README.md). It implements `http.RoundTripper`
so `FogCast/ui/tenfoot` and `gfx.Software` exercise real Play/Stop/ROM-linking
and frame-grant lifecycles against [fabricmodel](../fabricmodel/README.md)
(or an injected `nativefabric` endpoint via `NewWithEndpoint`) without touching
physical FPGA hardware. See [../../ARCHITECTURE.md](../../ARCHITECTURE.md).

- **Called by:** `cmd/tenfoot` (QEMU `run.py play`) and host `-race` tests.
- **Delegates to:** `FogCast/corepackage` (archive retention & ROM/part
  linking), [runtimeadmission](../runtimeadmission/README.md), and
  [fabricmodel](../fabricmodel/README.md) (or injected `nativefabric` endpoint).

## Actor and ownership model

- **Single cooperative actor:** HTTP workers submit bounded catalog/session
  requests; one preparation worker validates archives, ROMs, and CPU/video
  `PartInput` compositions against expected digests and receipts before
  retiring the previous owner.
- **Frame and input generation tokens:** Before each Tenfoot draw, the platform
  acquires an opaque frame grant and confirms completion after synchronous
  `ramfb` copy (`FinishFrame`). Transitions block new grants and wait for the
  active frame before display drain/programming; stale input/Stop/frame events
  are rejected by generation token.
- **Failure & cancellation semantics:**
  - Known failed Save retains the active core generation with input retired so
    the UI can retry Back.
  - Programming or identity failures run bounded idle recovery before failing
    launch; uncertain outcomes enter `reboot_required`.
  - `Backend.Close` waits for `App.Stop` cleanup, refuses to steal an active
    renderer frame, and drains the model cleanly.

## Verification

Run the 11-case QEMU Play/Return suite (see
[../README.md#tenfoot-play-and-return-through-the-model](../README.md#tenfoot-play-and-return-through-the-model))
or run host `-race` tests against its generated fixtures:

```sh
FES_MODEL_FIXTURES="$PWD/out/native-tenfoot-play/package-fixtures" \
  GOWORK=off go -C platform/embedded/tamago test -race ./modelbackend
```
