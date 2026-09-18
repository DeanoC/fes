# Computer media streaming

This runtime consumes `fes.media.blob-stream` 1.0 from mister-packages
`fdc4ece2e1fa87035ddca8cd147c621e7edcce3b`. Its authoritative wire definition
is that repository's `docs/media-stream.md` and `packages/abi/fes_simple_computer.yaml`.
The generated C++ header and copied `tests/fixtures/fes-media-stream-v1/exchanges.json`
must match that source. This extends ABI 1.0; it does not widen legacy blob 1.0.

## Discovery and admission

The compiled interface registry advertises driver support separately from live
evidence. After matching identity/build, the driver queries all five Info words
only when the package declares the exact supported stream interface and live
capability bit 3 is present. Info must report min=1, max=32768..33554432 and
chunk=512 exactly. A missing required bit rejects activation. An absent optional
stream bit does not advertise stream support; an undeclared/unknown optional
interface does not impose Info requirements on a legacy descriptor.

`capabilities.media_stream` is omitted without a verified active stream session.
When present its shape is:

```json
{"interface":{"id":"fes.media.blob-stream","major":1,"minor":0},"min_bytes":1,"max_bytes":32768,"chunk_bytes":512}
```

Protocol 2 adds this request without changing the top-level response envelope:

```json
{"protocol":2,"operation":"load_media_stream","path":"/absolute/media.bin","expected_package_id":"<64 lowercase hex characters>","expected_generation":1,"size":32768}
```

Generation is a positive uint64; size is a positive uint32 bounded by 32 MiB
and the actual endpoint maximum. The runtime checks exact active package and
generation under the existing busy boundary before hardware access, and holds
that ownership through snapshot, transfer and cleanup. The retained file's exact
size must equal the request. The first SMS endpoint's 32768 maximum is a fixed-map
capacity, not a mapper-support promise. Existing required keyboard, fixed video
and blob 1.0 requirements remain unchanged.

## Transfer and failures

Fresh stream activation initializes video and neutral keyboard rows, then
explicitly holds reset without attempting execution release. `load_core`
success establishes the owned package/generation and observed capacity, not
executing media. Only a subsequent successful media Commit permits release.
This behavior requires verified stream support; legacy and undeclared or absent
optional-stream sessions retain their previous startup behavior. Keyboard and
hold-reset failures fail activation without releasing execution.

Before holding reset or sending Begin, the runtime opens a regular, non-symlink
file and snapshots it into a private 0600 temporary file unlinked immediately.
It uses a 512-byte buffer and computes reflected CRC32/IEEE over exact payload
bytes. Growth, truncation and deadline expiry reject the snapshot. The snapshot
is independent of later pathname replacement or source mutation.
Snapshot admission failures carry the `request` phase, including file-open and
temporary-file I/O failures; they occur before hold-reset or transfer commands.

Under reset, the driver sends sequential Begin words, contiguous Chunk headers,
ordered low-byte-first data words (zero odd padding), then Commit. Only an
acknowledged successful Commit permits execution release. Neither errors nor
deadline expiry cause command replay. A post-hold failure attempts one Abort
using a separate cleanup deadline. A poisoned mailbox refuses even Abort writes.
Failed/ambiguous cleanup retains package and generation in `reboot_required`;
the existing terminal recovery policy forbids further mutations. A synchronized
Abort leaves reset held and permits a later explicit transfer or ordinary Stop.
Abort never restores the previous image. After a lost response, status reconciles
ownership and lifecycle only: the same package and generation do not prove that
media committed. Clients must not automatically retry an ambiguous request.

The default absolute budget is **120 seconds shared by snapshot and transfer**,
plus **10 seconds separately for cleanup**. The host adapter uses a 135-second
request timeout. GP exchanges retain their 100ms individual bound. Read/write
loops check deadlines on every iteration, including EINTR retries; this does
not make a blocking regular-file syscall cancellable. There is no separate
cancellation API or background transfer worker.

Tests cover canonical wire fixtures, CRC vectors, 32 KiB transfer, all 256 word
ordinals, odd tails, capacity and binding rejection, snapshot mutation/deadlines,
Abort/poison handling and Stop/relaunch. These are software evidence only.
No physical SMS, mapper, throughput or target-execution acceptance is claimed.
