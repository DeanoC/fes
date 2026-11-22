# Slice 4d: kernel owner window, KUnit

The DE25 DMA owner's KUnit suite, run under QEMU (`linux,dummy-virt`) with
the locked kernel, SDK and builder (`scripts/de25_kernel_test.py`), at
`5b61791ce`. No hardware is reached and no image is built.

```
python3 scripts/de25_kernel_test.py run --cache <cache> --image-output <dir> --output <new> --jobs 12
```

The image directory holds the locked SDK archive (sha256 `7912076c…`, from a
cached copy on this host) and the vendor `Image` (`874f75d8…`).
`kunit-inputs.json` and `kunit-checks.json` are the runner's own records.

**Result:** 66 of 66 cases pass, and the runner's kernel-log check is clean
(no WARNING, BUG or Oops). The four window cases:

- `window_publishes_grant_and_maps_offsets`
  - The grant is written while not granted, then `CommandGrant` is sent
    once. INFO reports the span, generation, contract, status and build.
  - A second window, `OWNER_IDLE` and an overlay change are all refused
    while the window lives.
  - Two maps at different offsets see the same page. Maps past the span or
    private are refused.
  - `REVOKE` zaps the maps and sends `CommandClear` once. A later map is
    refused, and `REAP` frees the storage.
- `window_refuses_a_block_it_cannot_grant` covers six blocks: wrong
  identity, already granted, faulted, not held, not drained, and a zero
  build. Each is refused before any register write, and the lease is free
  again afterwards.
- `window_fault_or_undrained_port_pins_storage`
  - An undrained port: revoke returns -EAGAIN and keeps the grant.
  - A port-1 fault: closing the client and `REAP` free nothing, and the
    owner stays busy and refuses overlays.
  - Once the record is cleared, `REAP` frees the storage.
- `window_needs_its_block`
  - An owner without the window `reg` refuses the window ABI and ALLOC.
  - With the window `reg`, the scanout and the window are separate leases.
  - ABI 3 still answers 3.

The register model is `fes_de25_hps_window_control.v`'s write rules. It
fails the test on any write the RTL answers with SLVERR.

**Mutations, each run through the same runner on a local commit:**

- Publish without its precondition read: `window_refuses_a_block_it_cannot_grant`
  fails (65/66).
- Revoke frees without `CommandClear`: three window cases fail (63/66).

**Runner.** Its required-case list adds the four window cases. It drops
`delayed_drain_releases_exposure` and
`retry_after_clear_accepts_pristine_snapshot`, which #1312 deleted from the
suite. Before that, the runner failed every run at an unchanged driver
(62/62 PASS, exit 1; #2122).

`.kunitconfig` raises the test kernel's CMA from 32 to 320 MiB for the
256 MiB window.
