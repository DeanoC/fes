# DE25 microSD write-protect wiring

Base: `47ee485cb`. The preceding real writer stopped with `block device is
read-only` after VERIFIED. Its failed trace, active generation-3 SD lease,
unknown possible partial write and independent host cleanup remain recorded
separately. The full 2,502-file closure and original source are retained at
`/home/deano/fes-retained/de25-sd-writer-failure-20261110-r1`; no failed gate or
programming grant is resumed by this change.

The [primary-source review](evidence.json) uses the retained Terasic revB
manual, Figure 3-28 and Table 3-23. They show CLK, CMD and DAT0–DAT3 through a
level translator, without a mechanical socket write-protect connection. The
manual is listed on [Terasic's resource page](https://www.terasic.com.cn/cgi-bin/page/archive.pl?CategoryNo=115&Language=English&No=1384&PartNo=4).
Its PDF hash and extracted original figure hash are recorded. The pinned
Altera register header defines WPSL as SRS09 bit 19. The pinned vendor U-Boot
Cadence driver copies an operation table without `get_wp`; its MMC uclass
returns ENOSYS when that callback is absent. Recovery writes on this spare
have passed.

The old native driver sampled WPSL before every writable sector. Software
writability was selected by the expected real-card identity. This points to
the socket-input check as the rejection, but the failed capture did not record
the full raw register value or sector index. It cannot exclude a partial write.

The Nano backend now supplies the explicit trusted board property
`NoSocketWriteProtect` through the firmware-owned SD session. Generic drivers
still sample the socket input by default. Neither wiring choice grants write
permission: normal storage remains read-only, and the writable session still
requires the expected CID/capacity and its exclusive firmware lease.

The driver also checks the card's permanent and temporary CSD protection bits,
independent of socket wiring. It refreshes that protection on every successful
same-card resume; card-reported command errors remain enforced. The
[Linux SD parser](https://raw.githubusercontent.com/torvalds/linux/v6.12/drivers/mmc/core/sd.c)
also treats CSD bit 13 as permanent read-only protection. Default-read-only,
quiescence, identity, transport-mode, clock and failure-containment checks
remain in place. No bus speed, width or DMA selection changes.

Tests model an unwired low socket signal, explicit permission versus default
read-only, protected CSD cards with both wiring policies, temporary protection
changes across resume, retained permanent protection and the real C firmware
lease/write/quiesce/resume path. A first focused run exposed that the newly
decoded card-protection state was not copied from the rediscovered device
during resume. That implementation was repaired; both the failed and passing
logs remain under `out/nano-sd-wp-fix-20261110/`.

This is a software repair informed by the retained manual and failed physical
test. It has not been deployed. The old writer SDK/boot chain no longer match
the changed production source and may not be used as acceptance evidence for
this repair. A fresh read-only recovery inspection must first establish the
tail contents and protected media after the failed write. Only then can a new
reviewed plan and matched writer be prepared for another bounded test. No
native write success, FAT persistence, normal core installation or full
appliance acceptance is claimed.

[All eight selected checks pass](software-result.json): 1,869 parent tests
with three existing skips and one ancillary run, generated consumer checks,
SD firmware/PIO/FAT/store races and the native integration's 42 commands.
The latter includes Tenfoot launch/return and injected-endpoint failures.
Its existing producer-fixture conditional skips are not hardware evidence.

A [fresh matched read-only inspector](prepared-inspector.json), compiled from
`a5d6027e9`, is now cross-built and packaged. Full selected-source/receipt
validation, embedded-SPL decoding and offline FAT readbacks pass. It has no
write plan and enables no native sector writes. This prepares the next card
inspection; no physical operation occurred after the failed writer test.
